// Package raft provides Reef v1 Raft-based federation.
// TDD Tests: BoltStore, RaftCommand, ReefFSM, ClientConnPool, LeaderedServer
package raft

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/sipeed/reef/pkg/reef"
	"github.com/sipeed/reef/pkg/reef/evolution"
	bolt "go.etcd.io/bbolt"
	"go.etcd.io/raft/v3/raftpb"
)

func TestBoltStoreOpenClose(t *testing.T) {
	db, err := bolt.Open("testdata/_str.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewBoltStore(db)
	if store == nil {
		t.Fatal("nil store")
	}
	store.InitBuckets()
	if !store.BucketExists("raft_log") || !store.BucketExists("raft_state") || !store.BucketExists("reef_state") {
		t.Error("buckets missing")
	}
}

func TestBoltStoreRaftLog(t *testing.T) {
	db, _ := bolt.Open("testdata/_rl.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	defer db.Close()
	store := NewBoltStore(db)
	store.InitBuckets()
	store.SaveEntries([]raftpb.Entry{
		{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: []byte(`{"type":1}`)},
		{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: []byte(`{"type":2}`)},
		{Index: 3, Term: 2, Type: raftpb.EntryNormal, Data: []byte(`{"type":3}`)},
	})
	loaded, _ := store.LoadEntries(1, 3)
	if len(loaded) != 3 || loaded[0].Index != 1 {
		t.Error("load mismatch")
	}
	store.TruncateEntries(2)
	remaining, _ := store.LoadEntries(1, 3)
	if len(remaining) != 2 {
		t.Error("truncate failed")
	}
}

func TestBoltStoreHardState(t *testing.T) {
	db, _ := bolt.Open("testdata/_hs.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	defer db.Close()
	store := NewBoltStore(db)
	store.InitBuckets()
	store.SaveHardState(raftpb.HardState{Term: 5, Vote: 2, Commit: 100})
	loaded, _ := store.LoadHardState()
	if loaded.Term != 5 || loaded.Vote != 2 || loaded.Commit != 100 {
		t.Error("hard state mismatch")
	}
}

func TestBoltStoreSnapshot(t *testing.T) {
	db, _ := bolt.Open("testdata/_snap.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	defer db.Close()
	store := NewBoltStore(db)
	store.InitBuckets()
	snap := Fsmsnapshot{
		Tasks:   map[string]*reef.Task{"t1": {ID: "t1", Status: reef.TaskRunning}},
		Clients: map[string]*reef.ClientInfo{"c1": {ID: "c1", Role: "coder", State: reef.ClientConnected}},
		Genes:   map[string]*evolution.Gene{},
		Drafts:  map[string]*evolution.SkillDraft{},
	}
	store.SaveSnapshot(snap)
	loaded, _ := store.LoadSnapshot()
	if len(loaded.Tasks) != 1 || loaded.Tasks["t1"].Status != reef.TaskRunning {
		t.Error("snap mismatch")
	}
}

func TestRaftCommandSerialization(t *testing.T) {
	cmd := RaftCommand{Type: CmdTaskEnqueue, Payload: json.RawMessage(`{"id":"x"}`), Timestamp: 1000, Proposer: "n1"}
	data, _ := json.Marshal(cmd)
	var d RaftCommand
	json.Unmarshal(data, &d)
	if d.Type != CmdTaskEnqueue || d.Proposer != "n1" {
		t.Error("serde mismatch")
	}
}

func TestRaftCommandSerde(t *testing.T) {
	// Test NewRaftCommand and round-trip for 5 representative types
	types := map[RaftCommandType]interface{}{
		CmdTaskEnqueue:    &TaskEnqueuePayload{TaskID: "t1", TaskData: json.RawMessage(`{"instruction":"test"}`)},
		CmdClientRegister: &ClientRegisterPayload{ClientID: "c1", Role: "coder", Skills: []string{"go"}, Capacity: 3},
		CmdGeneSubmit:     &GeneSubmitPayload{Gene: json.RawMessage(`{"strategy":"fb"}`)},
		CmdClaimPost:      &ClaimPostPayload{TaskID: "t2", TaskData: json.RawMessage(`{"priority":5}`)},
		CmdDagUpdate:      &DagUpdatePayload{NodeID: "n1", Status: "completed", Output: json.RawMessage(`{"result":"ok"}`), UpdatedAt: 1000},
	}

	for typ, payload := range types {
		cmd, err := NewRaftCommand(typ, payload, "node-1")
		if err != nil {
			t.Fatalf("NewRaftCommand(%d): %v", typ, err)
		}
		if cmd.Type != typ {
			t.Errorf("Type = %d, want %d", cmd.Type, typ)
		}
		if cmd.Proposer != "node-1" {
			t.Errorf("Proposer = %s, want node-1", cmd.Proposer)
		}
		if cmd.Timestamp == 0 {
			t.Error("Timestamp should be non-zero")
		}

		// Serialize + Deserialize
		data, err := cmd.Serialize()
		if err != nil {
			t.Fatalf("Serialize(%d): %v", typ, err)
		}
		restored, err := DeserializeRaftCommand(data)
		if err != nil {
			t.Fatalf("DeserializeRaftCommand(%d): %v", typ, err)
		}
		if restored.Type != cmd.Type || restored.Proposer != cmd.Proposer || restored.Timestamp != cmd.Timestamp {
			t.Errorf("round-trip mismatch for %d", typ)
		}
	}

	// Test UnmarshalPayload
	cmd, _ := NewRaftCommand(CmdTaskAssign, &TaskAssignPayload{TaskID: "t1", ClientID: "c1"}, "n1")
	var payload TaskAssignPayload
	if err := cmd.UnmarshalPayload(&payload); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}
	if payload.TaskID != "t1" || payload.ClientID != "c1" {
		t.Error("UnmarshalPayload mismatch")
	}

	// Test empty payload
	emptyCmd := &RaftCommand{Type: CmdTaskEnqueue, Payload: json.RawMessage(`{}`), Timestamp: 1000, Proposer: "n1"}
	var emptyPayload TaskEnqueuePayload
	if err := emptyCmd.UnmarshalPayload(&emptyPayload); err != nil {
		t.Errorf("empty payload should unmarshal cleanly: %v", err)
	}

	// Test invalid JSON payload
	badCmd := &RaftCommand{Type: CmdTaskEnqueue, Payload: json.RawMessage(`not-json`), Timestamp: 1000, Proposer: "n1"}
	var badPayload TaskEnqueuePayload
	if err := badCmd.UnmarshalPayload(&badPayload); err == nil {
		t.Error("expected error for invalid JSON payload")
	}

	// Test invalid JSON data for DeserializeRaftCommand
	if _, err := DeserializeRaftCommand([]byte(`not-json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestAllRaftCommandTypes(t *testing.T) {
	for _, ct := range []RaftCommandType{
		CmdTaskEnqueue, CmdTaskAssign, CmdTaskStart, CmdTaskComplete,
		CmdTaskFailed, CmdTaskCancel, CmdTaskEscalate, CmdTaskTimedOut,
		CmdClientRegister, CmdClientUnregister, CmdClientStale,
		CmdGeneSubmit, CmdGeneApprove, CmdGeneReject, CmdSkillDraft, CmdSkillApprove, CmdSkillReject,
		CmdClaimPost, CmdClaimAssign, CmdClaimExpire,
		CmdDagUpdate,
	} {
		cmd := RaftCommand{Type: ct, Payload: json.RawMessage(`{}`), Timestamp: 1000, Proposer: "n1"}
		data, _ := json.Marshal(cmd)
		var d RaftCommand
		if err := json.Unmarshal(data, &d); err != nil {
			t.Errorf("type %d: %v", ct, err)
		}
	}
}

func TestCommandTypeEnum(t *testing.T) {
	// Verify constant values
	tests := []struct {
		ct       RaftCommandType
		wantVal  int32
		wantStr  string
		isTask   bool
		isClient bool
		isEvo    bool
		isClaim  bool
		isDag    bool
		domain   string
	}{
		{CmdTaskEnqueue, 1, "CmdTaskEnqueue", true, false, false, false, false, "task"},
		{CmdTaskAssign, 2, "CmdTaskAssign", true, false, false, false, false, "task"},
		{CmdTaskTimedOut, 8, "CmdTaskTimedOut", true, false, false, false, false, "task"},
		{CmdClientRegister, 20, "CmdClientRegister", false, true, false, false, false, "client"},
		{CmdClientStale, 22, "CmdClientStale", false, true, false, false, false, "client"},
		{CmdGeneSubmit, 30, "CmdGeneSubmit", false, false, true, false, false, "evolution"},
		{CmdGeneApprove, 31, "CmdGeneApprove", false, false, true, false, false, "evolution"},
		{CmdSkillReject, 35, "CmdSkillReject", false, false, true, false, false, "evolution"},
		{CmdClaimPost, 40, "CmdClaimPost", false, false, false, true, false, "claim"},
		{CmdClaimExpire, 42, "CmdClaimExpire", false, false, false, true, false, "claim"},
		{CmdDagUpdate, 50, "CmdDagUpdate", false, false, false, false, true, "dag"},
	}

	for _, tt := range tests {
		if int32(tt.ct) != tt.wantVal {
			t.Errorf("%s value = %d, want %d", tt.ct, int32(tt.ct), tt.wantVal)
		}
		if tt.ct.String() != tt.wantStr {
			t.Errorf("%s.String() = %q, want %q", tt.ct, tt.ct.String(), tt.wantStr)
		}
		if tt.ct.IsTaskOp() != tt.isTask {
			t.Errorf("%s.IsTaskOp() = %v, want %v", tt.ct, tt.ct.IsTaskOp(), tt.isTask)
		}
		if tt.ct.IsClientOp() != tt.isClient {
			t.Errorf("%s.IsClientOp() = %v, want %v", tt.ct, tt.ct.IsClientOp(), tt.isClient)
		}
		if tt.ct.IsEvolutionOp() != tt.isEvo {
			t.Errorf("%s.IsEvolutionOp() = %v, want %v", tt.ct, tt.ct.IsEvolutionOp(), tt.isEvo)
		}
		if tt.ct.IsClaimOp() != tt.isClaim {
			t.Errorf("%s.IsClaimOp() = %v, want %v", tt.ct, tt.ct.IsClaimOp(), tt.isClaim)
		}
		if tt.ct.IsDagOp() != tt.isDag {
			t.Errorf("%s.IsDagOp() = %v, want %v", tt.ct, tt.ct.IsDagOp(), tt.isDag)
		}
		if tt.ct.Domain() != tt.domain {
			t.Errorf("%s.Domain() = %q, want %q", tt.ct, tt.ct.Domain(), tt.domain)
		}
	}

	// Edge case: unknown type
	unknown := RaftCommandType(99)
	if unknown.String() != "RaftCommandType(99)" {
		t.Errorf("unknown.String() = %q, want %q", unknown.String(), "RaftCommandType(99)")
	}
	if unknown.IsTaskOp() || unknown.IsClientOp() || unknown.IsEvolutionOp() || unknown.IsClaimOp() || unknown.IsDagOp() {
		t.Error("unknown type should return false for all Is* methods")
	}
	if unknown.Domain() != "unknown" {
		t.Errorf("unknown.Domain() = %q, want %q", unknown.Domain(), "unknown")
	}

	// Edge case: type 0
	zero := RaftCommandType(0)
	if zero.String() != "RaftCommandType(0)" {
		t.Errorf("zero.String() = %q", zero.String())
	}
}

func TestPayloadStructs(t *testing.T) {
	// Task payloads
	taskPayloads := []struct {
		name    string
		payload interface{}
	}{
		{"TaskEnqueuePayload", &TaskEnqueuePayload{TaskID: "t1", TaskData: json.RawMessage(`{"instruction":"test"}`)}},
		{"TaskAssignPayload", &TaskAssignPayload{TaskID: "t1", ClientID: "c1"}},
		{"TaskStartPayload", &TaskStartPayload{TaskID: "t1", ClientID: "c1"}},
		{"TaskCompletePayload", &TaskCompletePayload{TaskID: "t1", ClientID: "c1", Result: json.RawMessage(`{"text":"ok"}`), ExecutionTimeMs: 1234}},
		{"TaskFailedPayload", &TaskFailedPayload{TaskID: "t1", ClientID: "c1", Error: json.RawMessage(`{"type":"E001"}`), AttemptCount: 0}},
		{"TaskCancelPayload", &TaskCancelPayload{TaskID: "t1", Reason: "user requested"}},
		{"TaskEscalatePayload", &TaskEscalatePayload{TaskID: "t1", EscalationLevel: 1}},
		{"TaskTimedOutPayload", &TaskTimedOutPayload{TaskID: "t1", TimeoutAt: 1000}},
	}

	for _, tt := range taskPayloads {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var back map[string]interface{}
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
		})
	}

	// Client payloads
	clientPayloads := []struct {
		name    string
		payload interface{}
	}{
		{"ClientRegisterPayload", &ClientRegisterPayload{ClientID: "c1", Role: "coder", Skills: []string{"go", "rust"}, Capacity: 5}},
		{"ClientUnregisterPayload", &ClientUnregisterPayload{ClientID: "c1"}},
		{"ClientStalePayload", &ClientStalePayload{ClientID: "c1"}},
	}
	for _, tt := range clientPayloads {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := json.Marshal(tt.payload)
			var back map[string]interface{}
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
		})
	}

	// Evolution payloads
	evoPayloads := []struct {
		name    string
		payload interface{}
	}{
		{"GeneSubmitPayload", &GeneSubmitPayload{Gene: json.RawMessage(`{"strategy":"fb"}`)}},
		{"GeneApprovePayload", &GeneApprovePayload{GeneID: "g1", ApproverNode: "n1"}},
		{"GeneRejectPayload", &GeneRejectPayload{GeneID: "g1", Reason: "invalid", RejecterNode: "n1"}},
		{"SkillDraftPayload", &SkillDraftPayload{Draft: json.RawMessage(`{"name":"greet"}`)}},
		{"SkillApprovePayload", &SkillApprovePayload{DraftID: "d1", Approver: "admin"}},
		{"SkillRejectPayload", &SkillRejectPayload{DraftID: "d1", Reason: "bad", Rejecter: "admin"}},
	}
	for _, tt := range evoPayloads {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := json.Marshal(tt.payload)
			var back map[string]interface{}
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
		})
	}

	// Claim payloads
	claimPayloads := []struct {
		name    string
		payload interface{}
	}{
		{"ClaimPostPayload", &ClaimPostPayload{TaskID: "t1", TaskData: json.RawMessage(`{"priority":5}`)}},
		{"ClaimAssignPayload", &ClaimAssignPayload{TaskID: "t1", ClientID: "c1"}},
		{"ClaimExpirePayload", &ClaimExpirePayload{TaskID: "t1", RetryCount: 1}},
	}
	for _, tt := range claimPayloads {
		t.Run(tt.name, func(t *testing.T) {
			data, _ := json.Marshal(tt.payload)
			var back map[string]interface{}
			if err := json.Unmarshal(data, &back); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
		})
	}

	// DAG payload
	t.Run("DagUpdatePayload", func(t *testing.T) {
		p := &DagUpdatePayload{NodeID: "n1", Status: "running", Output: json.RawMessage(`{"progress":50}`), UpdatedAt: 2000}
		data, _ := json.Marshal(p)
		var back DagUpdatePayload
		if err := json.Unmarshal(data, &back); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if back.NodeID != "n1" || back.Status != "running" || back.UpdatedAt != 2000 {
			t.Error("DagUpdatePayload round-trip mismatch")
		}
	})

	// Edge cases: empty slices and nil json.RawMessage
	t.Run("ClientRegisterPayload_empty_skills", func(t *testing.T) {
		p := &ClientRegisterPayload{ClientID: "c1", Role: "coder", Skills: []string{}, Capacity: 0}
		data, _ := json.Marshal(p)
		var back ClientRegisterPayload
		json.Unmarshal(data, &back)
		if back.ClientID != "c1" {
			t.Error("empty skills round-trip mismatch")
		}
	})

	t.Run("GeneSubmitPayload_nil_gene", func(t *testing.T) {
		p := &GeneSubmitPayload{Gene: nil}
		data, _ := json.Marshal(p)
		var back GeneSubmitPayload
		json.Unmarshal(data, &back)
		// JSON null unmarshals to json.RawMessage("null"), not nil
		if back.Gene == nil && string(back.Gene) != "null" {
			t.Error("nil gene: expected null or nil")
		}
	})

	t.Run("TaskFailedPayload_zero_attempts", func(t *testing.T) {
		p := &TaskFailedPayload{TaskID: "t1", ClientID: "c1", AttemptCount: 0}
		data, _ := json.Marshal(p)
		var back TaskFailedPayload
		json.Unmarshal(data, &back)
		if back.AttemptCount != 0 {
			t.Error("zero attempts expected")
		}
	})
}

func TestReefFSMApplyTaskEnqueue(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000, Payload: json.RawMessage(`{"id":"t1","instruction":"test","status":"Created","priority":3}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"] == nil || fsm.Tasks["t1"].Instruction != "test" || fsm.Tasks["t1"].Priority != 3 {
		t.Error("enqueue wrong")
	}
}

func TestReefFSMApplyTaskAssign(t *testing.T) {
	fsm := newFSMWithTask(t, "t1")
	cmd := RaftCommand{Type: CmdTaskAssign, Timestamp: 2000, Payload: json.RawMessage(`{"task_id":"t1","client_id":"cA"}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"].AssignedClient != "cA" || fsm.Tasks["t1"].Status != reef.TaskRunning {
		t.Error("assign wrong")
	}
}

func TestReefFSMApplyTaskComplete(t *testing.T) {
	fsm := newFSMWithRunningTask(t, "t1", "cA")
	cmd := RaftCommand{Type: CmdTaskComplete, Timestamp: 3000, Payload: json.RawMessage(`{"task_id":"t1","execution_time_ms":1234,"result":{"text":"done"}}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 3, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"].Status != reef.TaskCompleted || fsm.Tasks["t1"].Result.Text != "done" {
		t.Error("complete wrong")
	}
}

func TestReefFSMApplyTaskFailed(t *testing.T) {
	fsm := newFSMWithRunningTask(t, "t1", "cA")
	cmd := RaftCommand{Type: CmdTaskFailed, Timestamp: 3000, Payload: json.RawMessage(`{"task_id":"t1","error":{"type":"E001","message":"oops"},"attempt_history":[]}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 3, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"].Status != reef.TaskFailed || fsm.Tasks["t1"].Error.Type != "E001" {
		t.Error("failed wrong")
	}
}

func TestReefFSMApplyClientRegister(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: CmdClientRegister, Timestamp: 1000, Payload: json.RawMessage(`{"client_id":"cA","role":"coder","state":"connected"}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Clients["cA"] == nil || fsm.Clients["cA"].Role != "coder" {
		t.Error("register wrong")
	}
}

func TestReefFSMApplyGeneApprove(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// First submit a gene
	g, _ := json.Marshal(&evolution.Gene{ID: "g1", StrategyName: "fb", Role: "coder", ControlSignal: "go build", Version: 1})
	submitCmd := RaftCommand{Type: CmdGeneSubmit, Timestamp: 1000, Payload: g}
	submitData, _ := json.Marshal(submitCmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: submitData})

	// Then approve it
	approvePayload, _ := json.Marshal(map[string]string{"gene_id": "g1", "approver_node": "n1"})
	approveCmd := RaftCommand{Type: CmdGeneApprove, Timestamp: 2000, Payload: approvePayload}
	approveData, _ := json.Marshal(approveCmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: approveData})

	if fsm.Genes["g1"] == nil || fsm.Genes["g1"].Status != evolution.GeneStatusApproved {
		t.Error("gene approve wrong")
	}
}

func TestReefFSMDeterministic(t *testing.T) {
	inputs := []struct{ ct RaftCommandType; pay string }{
		{CmdTaskEnqueue, `{"id":"t1","instruction":"a","status":"Created"}`},
		{CmdTaskEnqueue, `{"id":"t2","instruction":"b","status":"Created"}`},
		{CmdTaskAssign, `{"task_id":"t1","client_id":"c1"}`},
		{CmdTaskComplete, `{"task_id":"t1","execution_time_ms":100,"result":{"text":"ok"}}`},
		{CmdTaskCancel, `{"task_id":"t2"}`},
	}
	for round := 0; round < 20; round++ {
		a := NewReefFSM(newTestDB(t), nil)
		b := NewReefFSM(newTestDB(t), nil)
		for i, in := range inputs {
			c := RaftCommand{Type: in.ct, Payload: json.RawMessage(in.pay), Timestamp: 1000 + int64(i)}
			d, _ := json.Marshal(c)
			e := &raftpb.Entry{Index: uint64(i) + 1, Term: 1, Type: raftpb.EntryNormal, Data: d}
			a.Apply(e)
			b.Apply(e)
		}
		if !a.Equal(b) {
			t.Fatalf("round %d diverged", round)
		}
	}
}

func TestReefFSMSnapshotRestore(t *testing.T) {
	fsm := newFSMWithTask(t, "t1")
	c := RaftCommand{Type: CmdTaskAssign, Timestamp: 2000, Payload: json.RawMessage(`{"task_id":"t1","client_id":"c1"}`)}
	d, _ := json.Marshal(c)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: d})
	snap, _ := fsm.Snapshot()
	if len(snap) == 0 {
		t.Fatal("empty snap")
	}
	r := NewReefFSM(newTestDB(t), nil)
	r.Restore(snap)
	if !fsm.Equal(r) {
		t.Error("restore mismatch")
	}
}

func TestReefFSMConfChangeNoop(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryConfChange, Data: []byte(`{}`)})
	if len(fsm.Tasks) != 0 {
		t.Error("conf change should not mutate")
	}
}

func TestClientConnPoolNew(t *testing.T) {
	pool := NewClientConnPool(PoolConfig{ServerAddrs: []string{"ws://n1:8080", "ws://n2:8081", "ws://n3:8082"}})
	if pool == nil || len(pool.Servers) != 3 || pool.LeaderIdx != -1 {
		t.Error("pool init wrong")
	}
}

func TestClientConnPoolLeaderDetection(t *testing.T) {
	pool := NewClientConnPool(PoolConfig{ServerAddrs: []string{"ws://n1:8080", "ws://n2:8081"}})
	pool.OnLeaderChange("ws://n1:8080")
	if pool.LeaderIdx != 0 {
		t.Error("leader idx 0")
	}
	pool.OnLeaderChange("ws://n2:8081")
	if pool.LeaderIdx != 1 {
		t.Error("leader idx 1")
	}
}

func TestClientConnPoolSendToLeaderNoLeader(t *testing.T) {
	pool := NewClientConnPool(PoolConfig{ServerAddrs: []string{"ws://n1:8080"}})
	msg, _ := reef.NewMessage(reef.MsgTaskCompleted, "", nil)
	err := pool.SendToLeader(msg)
	if err == nil {
		t.Error("expected error with no leader")
	}
}

func TestClientConnPoolSingleAddr(t *testing.T) {
	pool := NewClientConnPool(PoolConfig{ServerAddrs: []string{"ws://localhost:8080"}})
	if len(pool.Servers) != 1 || pool.Servers[0].Addr != "ws://localhost:8080" {
		t.Error("single addr wrong")
	}
}

func TestLeaderedServerProposeNotLeader(t *testing.T) {
	ls := &LeaderedServer{}
	err := ls.Propose(RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000})
	if err != ErrNotLeader {
		t.Errorf("expected ErrNotLeader, got %v", err)
	}
}

func TestLeaderedServerLifecycleIdempotent(t *testing.T) {
	ls := &LeaderedServer{}
	ls.onBecomeLeader()
	ls.onBecomeLeader()
	ls.onLoseLeadership()
	ls.onLoseLeadership()
	// must not panic
}

// helpers

func TestBoltStoreCompactLog(t *testing.T) {
	db, err := bolt.Open("testdata/_cl.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewBoltStore(db)
	store.InitBuckets()
	store.SaveEntries([]raftpb.Entry{
		{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: []byte(`{"type":1}`)},
		{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: []byte(`{"type":2}`)},
		{Index: 3, Term: 2, Type: raftpb.EntryNormal, Data: []byte(`{"type":3}`)},
		{Index: 4, Term: 2, Type: raftpb.EntryNormal, Data: []byte(`{"type":4}`)},
		{Index: 5, Term: 3, Type: raftpb.EntryNormal, Data: []byte(`{"type":5}`)},
	})
	if err := store.CompactLog(3); err != nil {
		t.Fatal(err)
	}
	remaining, _ := store.LoadEntries(1, 5)
	if len(remaining) != 2 || remaining[0].Index != 4 || remaining[1].Index != 5 {
		t.Errorf("compact failed: got %d entries with indexes %d,%d, expected 4,5",
			len(remaining), remaining[0].Index, remaining[1].Index)
	}
}

func TestBoltStoreCompactLogEmpty(t *testing.T) {
	db, err := bolt.Open("testdata/_cle.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewBoltStore(db)
	store.InitBuckets()
	if err := store.CompactLog(100); err != nil {
		t.Fatal("compact on empty bucket should not error:", err)
	}
}

func TestBoltStoreClose(t *testing.T) {
	db, err := bolt.Open("testdata/_close.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	store := NewBoltStore(db)
	store.InitBuckets()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Second close must be safe
	if err := store.Close(); err != nil {
		t.Fatal("second Close should be safe:", err)
	}
}

func TestBoltStoreDB(t *testing.T) {
	db, err := bolt.Open("testdata/_db.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewBoltStore(db)
	if store.DB() != db {
		t.Error("DB() returned different db instance")
	}
}

func TestBoltStoreClearSnapshot(t *testing.T) {
	db, err := bolt.Open("testdata/_cs.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewBoltStore(db)
	store.InitBuckets()
	snap := Fsmsnapshot{
		Tasks:   map[string]*reef.Task{"t1": {ID: "t1", Status: reef.TaskRunning}},
		Clients: map[string]*reef.ClientInfo{"c1": {ID: "c1", Role: "coder", State: reef.ClientConnected}},
		Genes:   map[string]*evolution.Gene{},
		Drafts:  map[string]*evolution.SkillDraft{},
	}
	if err := store.SaveSnapshot(snap); err != nil {
		t.Fatal("save snapshot:", err)
	}
	loaded, err := store.LoadSnapshot()
	if err != nil {
		t.Fatal("load snapshot:", err)
	}
	if len(loaded.Tasks) != 1 {
		t.Fatal("snapshot not saved correctly")
	}
	if err := store.ClearSnapshot(); err != nil {
		t.Fatal("clear snapshot:", err)
	}
	loadedAfter, err := store.LoadSnapshot()
	if err != nil {
		t.Fatal("load after clear:", err)
	}
	if len(loadedAfter.Tasks) != 0 {
		t.Error("snapshot not cleared")
	}
}

func TestBoltStoreClearSnapshotEmpty(t *testing.T) {
	db, err := bolt.Open("testdata/_cse.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewBoltStore(db)
	store.InitBuckets()
	if err := store.ClearSnapshot(); err != nil {
		t.Fatal("clear on empty should not error:", err)
	}
}

func TestBoltStoreLockLeaseStubs(t *testing.T) {
	db, err := bolt.Open("testdata/_ll.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewBoltStore(db)
	store.InitBuckets()

	ok, err := store.PessimisticLockAcquire("key1", time.Second)
	if ok || err == nil {
		t.Error("PessimisticLockAcquire stub should return error")
	}
	if err2 := store.PessimisticLockRelease("key1"); err2 == nil {
		t.Error("PessimisticLockRelease stub should return error")
	}
	if err3 := store.PessimisticLeaseRenew("key1", time.Second); err3 == nil {
		t.Error("PessimisticLeaseRenew stub should return error")
	}
}

// =====================================================================
// P7-03 tests: Validate, IsConsensus, IsLocal, Dispatch
// =====================================================================

func TestRaftCommandValidate(t *testing.T) {
	// Valid command
	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{TaskID: "t1"}, "n1")
	if err := cmd.Validate(); err != nil {
		t.Errorf("valid command should validate: %v", err)
	}

	// Nil command
	var nilCmd *RaftCommand
	if err := nilCmd.Validate(); err == nil {
		t.Error("nil command should fail validation")
	}

	// Unknown type
	unknown := &RaftCommand{Type: RaftCommandType(99), Payload: json.RawMessage(`{}`), Timestamp: 1000, Proposer: "n1"}
	if err := unknown.Validate(); err == nil {
		t.Error("unknown type should fail validation")
	}

	// Invalid payload JSON
	badPayload := &RaftCommand{Type: CmdTaskEnqueue, Payload: json.RawMessage(`not-json`), Timestamp: 1000, Proposer: "n1"}
	if err := badPayload.Validate(); err == nil {
		t.Error("bad payload JSON should fail validation")
	}

	// Empty payload (valid for commands with no data)
	emptyPayload := &RaftCommand{Type: CmdTaskEnqueue, Payload: json.RawMessage(``), Timestamp: 1000, Proposer: "n1"}
	if err := emptyPayload.Validate(); err != nil {
		t.Errorf("empty payload should validate: %v", err)
	}
}

func TestRaftCommandConsensus(t *testing.T) {
	allTypes := []RaftCommandType{
		CmdTaskEnqueue, CmdTaskAssign, CmdTaskStart, CmdTaskComplete,
		CmdTaskFailed, CmdTaskCancel, CmdTaskEscalate, CmdTaskTimedOut,
		CmdClientRegister, CmdClientUnregister, CmdClientStale,
		CmdGeneSubmit, CmdGeneApprove, CmdGeneReject,
		CmdSkillDraft, CmdSkillApprove, CmdSkillReject,
		CmdClaimPost, CmdClaimAssign, CmdClaimExpire,
		CmdDagUpdate,
	}

	for _, ct := range allTypes {
		cmd := &RaftCommand{Type: ct, Payload: json.RawMessage(`{}`), Timestamp: 1000, Proposer: "n1"}
		if !cmd.IsConsensus() {
			t.Errorf("%s should require consensus", ct)
		}
		if cmd.IsLocal() {
			t.Errorf("%s should not be local", ct)
		}
	}

	// Nil command
	var nilCmd *RaftCommand
	if nilCmd.IsConsensus() {
		t.Error("nil command should not be consensus")
	}
	if !nilCmd.IsLocal() {
		t.Error("nil command should be local")
	}

	// Unknown type
	unknown := &RaftCommand{Type: RaftCommandType(99), Payload: json.RawMessage(`{}`), Timestamp: 1000, Proposer: "n1"}
	if unknown.IsConsensus() {
		t.Error("unknown type should not be consensus")
	}
	if !unknown.IsLocal() {
		t.Error("unknown type should be local")
	}
}

func TestDispatchTable(t *testing.T) {
	// All defined types should map to DispatchConsensus
	allTypes := []RaftCommandType{
		CmdTaskEnqueue, CmdTaskAssign, CmdTaskStart, CmdTaskComplete,
		CmdTaskFailed, CmdTaskCancel, CmdTaskEscalate, CmdTaskTimedOut,
		CmdClientRegister, CmdClientUnregister, CmdClientStale,
		CmdGeneSubmit, CmdGeneApprove, CmdGeneReject,
		CmdSkillDraft, CmdSkillApprove, CmdSkillReject,
		CmdClaimPost, CmdClaimAssign, CmdClaimExpire,
		CmdDagUpdate,
	}

	for _, ct := range allTypes {
		if !DispatchesToConsensus(ct) {
			t.Errorf("%s should dispatch to consensus", ct)
		}
		if GetDispatchTarget(ct) != DispatchConsensus {
			t.Errorf("%s should have DispatchConsensus target", ct)
		}
	}

	// Unknown type defaults to non-consensus
	if GetDispatchTarget(RaftCommandType(99)) != DispatchNonConsensus {
		t.Error("unknown type should dispatch to non-consensus")
	}
	if DispatchesToConsensus(RaftCommandType(99)) {
		t.Error("unknown type should not dispatch to consensus")
	}
}

// =====================================================================
// Protocol tests: raft_leader_change
// =====================================================================

func TestRaftLeaderChangeMessage(t *testing.T) {
	// First election: old addresses are empty
	msg := reef.NewRaftLeaderChangeMessage("ws://n2:8080", "node-2", "", "", 3)
	if msg.MsgType != reef.MsgRaftLeaderChange {
		t.Errorf("MsgType = %s, want %s", msg.MsgType, reef.MsgRaftLeaderChange)
	}

	var payload reef.RaftLeaderChangePayload
	if err := msg.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if payload.NewLeaderAddr != "ws://n2:8080" {
		t.Errorf("NewLeaderAddr = %s", payload.NewLeaderAddr)
	}
	if payload.NewLeaderID != "node-2" {
		t.Errorf("NewLeaderID = %s", payload.NewLeaderID)
	}
	if payload.OldLeaderAddr != "" {
		t.Errorf("OldLeaderAddr should be empty, got %q", payload.OldLeaderAddr)
	}
	if payload.OldLeaderID != "" {
		t.Errorf("OldLeaderID should be empty, got %q", payload.OldLeaderID)
	}
	if payload.Term != 3 {
		t.Errorf("Term = %d, want 3", payload.Term)
	}
	if payload.Timestamp == 0 {
		t.Error("Timestamp should be set")
	}

	// Leader change with old addresses
	msg2 := reef.NewRaftLeaderChangeMessage("ws://n3:8080", "node-3", "ws://n2:8080", "node-2", 4)
	var payload2 reef.RaftLeaderChangePayload
	msg2.DecodePayload(&payload2)
	if payload2.OldLeaderAddr != "ws://n2:8080" {
		t.Errorf("OldLeaderAddr = %s", payload2.OldLeaderAddr)
	}
	if payload2.OldLeaderID != "node-2" {
		t.Errorf("OldLeaderID = %s", payload2.OldLeaderID)
	}

	// Term 0
	msg3 := reef.NewRaftLeaderChangeMessage("ws://n1:8080", "node-1", "", "", 0)
	var payload3 reef.RaftLeaderChangePayload
	msg3.DecodePayload(&payload3)
	if payload3.Term != 0 {
		t.Errorf("Term = %d, want 0", payload3.Term)
	}

	// Message type validity
	if !reef.MsgRaftLeaderChange.IsValid() {
		t.Error("MsgRaftLeaderChange should be valid")
	}

	// Round-trip through JSON
	data, _ := json.Marshal(msg)
	var restored reef.Message
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("JSON round-trip: %v", err)
	}
	if restored.MsgType != reef.MsgRaftLeaderChange {
		t.Error("MsgType mismatch after round-trip")
	}
	var restoredPayload reef.RaftLeaderChangePayload
	if err := restored.DecodePayload(&restoredPayload); err != nil {
		t.Fatalf("decode restored: %v", err)
	}
	if restoredPayload.NewLeaderAddr != "ws://n2:8080" {
		t.Error("NewLeaderAddr mismatch after round-trip")
	}
}

// helpers

func newTestDB(t *testing.T) *bolt.DB {
	t.Helper()
	db, err := bolt.Open("testdata/__fsm.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestStore(t *testing.T) *BoltStore {
	t.Helper()
	db := newTestDB(t)
	store := NewBoltStore(db)
	store.InitBuckets()
	return store
}

func newFSMWithTask(t *testing.T, id string) *ReefFSM {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000, Payload: json.RawMessage(`{"id":"` + id + `","instruction":"test","status":"Created","priority":5}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	return fsm
}

func newFSMWithRunningTask(t *testing.T, id, client string) *ReefFSM {
	fsm := newFSMWithTask(t, id)
	cmd := RaftCommand{Type: CmdTaskAssign, Timestamp: 2000, Payload: json.RawMessage(`{"task_id":"` + id + `","client_id":"` + client + `"}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: data})
	return fsm
}

// =====================================================================
// P7-04 tests: All 21 command handlers, FSM enhancements
// =====================================================================

// --- New Task Handlers ---

func TestReefFSMApplyTaskEscalate(t *testing.T) {
	fsm := newFSMWithTask(t, "t1")
	cmd := RaftCommand{Type: CmdTaskEscalate, Timestamp: 2000,
		Payload: json.RawMessage(`{"task_id":"t1","escalation_level":1}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"].EscalationCount != 1 {
		t.Errorf("escalation count = %d, want 1", fsm.Tasks["t1"].EscalationCount)
	}
	if fsm.Tasks["t1"].Status != reef.TaskFailed {
		t.Errorf("status = %s, want %s", fsm.Tasks["t1"].Status, reef.TaskFailed)
	}
}

func TestReefFSMApplyTaskTimedOut(t *testing.T) {
	fsm := newFSMWithRunningTask(t, "t1", "cA")
	cmd := RaftCommand{Type: CmdTaskTimedOut, Timestamp: 3000,
		Payload: json.RawMessage(`{"task_id":"t1","timeout_at":2500}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 3, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"].Status != reef.TaskFailed {
		t.Errorf("status = %s, want %s", fsm.Tasks["t1"].Status, reef.TaskFailed)
	}
	if fsm.Tasks["t1"].Error == nil || fsm.Tasks["t1"].Error.Type != "TIMEOUT" {
		t.Error("error should be TIMEOUT")
	}
}

func TestReefFSMApplyTaskStart(t *testing.T) {
	fsm := newFSMWithTask(t, "t1")
	cmd := RaftCommand{Type: CmdTaskStart, Timestamp: 1500,
		Payload: json.RawMessage(`{"task_id":"t1","client_id":"cA"}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"].Status != reef.TaskRunning {
		t.Errorf("status = %s, want %s", fsm.Tasks["t1"].Status, reef.TaskRunning)
	}
	if fsm.Tasks["t1"].StartedAt == nil {
		t.Error("StartedAt should be set")
	}
}

// --- New Evolution Handlers ---

func TestReefFSMApplyGeneSubmit(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	g, _ := json.Marshal(&evolution.Gene{ID: "g1", StrategyName: "fb", Role: "coder",
		ControlSignal: "go build", Version: 1})
	cmd := RaftCommand{Type: CmdGeneSubmit, Timestamp: 1000, Payload: g}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Genes["g1"] == nil {
		t.Fatal("gene not stored")
	}
	if fsm.Genes["g1"].StrategyName != "fb" || fsm.Genes["g1"].Role != "coder" {
		t.Error("gene fields wrong")
	}
}

func TestReefFSMApplyGeneReject(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// Submit a gene first
	g, _ := json.Marshal(&evolution.Gene{ID: "g1", StrategyName: "fb", Role: "coder",
		ControlSignal: "go build", Version: 1})
	submitCmd := RaftCommand{Type: CmdGeneSubmit, Timestamp: 1000, Payload: g}
	submitData, _ := json.Marshal(submitCmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: submitData})

	// Reject it
	rejectPayload, _ := json.Marshal(map[string]string{
		"gene_id": "g1", "reason": "bad strategy", "rejecter_node": "n2"})
	rejectCmd := RaftCommand{Type: CmdGeneReject, Timestamp: 2000, Payload: rejectPayload}
	rejectData, _ := json.Marshal(rejectCmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: rejectData})

	if fsm.Genes["g1"].Status != evolution.GeneStatusRejected {
		t.Errorf("status = %s, want %s", fsm.Genes["g1"].Status, evolution.GeneStatusRejected)
	}
}

func TestReefFSMApplySkillDraft(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	d, _ := json.Marshal(&evolution.SkillDraft{
		ID: "d1", Role: "coder", SkillName: "greet", Content: "hello",
		SourceGeneIDs: []string{"g1"}, Status: evolution.SkillDraftPendingReview})
	cmd := RaftCommand{Type: CmdSkillDraft, Timestamp: 1000, Payload: d}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Drafts["d1"] == nil {
		t.Fatal("draft not stored")
	}
	if fsm.Drafts["d1"].SkillName != "greet" {
		t.Error("draft skill name wrong")
	}
}

func TestReefFSMApplySkillApprove(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// Submit a draft first
	d, _ := json.Marshal(&evolution.SkillDraft{
		ID: "d1", Role: "coder", SkillName: "greet", Content: "hello",
		SourceGeneIDs: []string{"g1"}, Status: evolution.SkillDraftPendingReview})
	submitCmd := RaftCommand{Type: CmdSkillDraft, Timestamp: 1000, Payload: d}
	submitData, _ := json.Marshal(submitCmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: submitData})

	// Approve it
	approvePayload, _ := json.Marshal(map[string]string{"draft_id": "d1", "approver": "admin"})
	approveCmd := RaftCommand{Type: CmdSkillApprove, Timestamp: 2000, Payload: approvePayload}
	approveData, _ := json.Marshal(approveCmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: approveData})

	if fsm.Drafts["d1"].Status != evolution.SkillDraftApproved {
		t.Errorf("status = %s, want %s", fsm.Drafts["d1"].Status, evolution.SkillDraftApproved)
	}
	if fsm.Drafts["d1"].ReviewedAt == nil {
		t.Error("ReviewedAt should be set")
	}
}

func TestReefFSMApplySkillReject(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// Submit a draft first
	d, _ := json.Marshal(&evolution.SkillDraft{
		ID: "d1", Role: "coder", SkillName: "greet", Content: "hello",
		SourceGeneIDs: []string{"g1"}, Status: evolution.SkillDraftPendingReview})
	submitCmd := RaftCommand{Type: CmdSkillDraft, Timestamp: 1000, Payload: d}
	submitData, _ := json.Marshal(submitCmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: submitData})

	// Reject it
	rejectPayload, _ := json.Marshal(map[string]string{
		"draft_id": "d1", "reason": "bad content", "rejecter": "admin"})
	rejectCmd := RaftCommand{Type: CmdSkillReject, Timestamp: 2000, Payload: rejectPayload}
	rejectData, _ := json.Marshal(rejectCmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: rejectData})

	if fsm.Drafts["d1"].Status != evolution.SkillDraftRejected {
		t.Errorf("status = %s, want %s", fsm.Drafts["d1"].Status, evolution.SkillDraftRejected)
	}
	if fsm.Drafts["d1"].ReviewedAt == nil {
		t.Error("ReviewedAt should be set")
	}
}

// --- New Claim Handlers ---

func TestReefFSMApplyClaimPost(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: CmdClaimPost, Timestamp: 1000,
		Payload: json.RawMessage(`{"task_id":"t1","task_data":{"id":"t1","instruction":"claim task","status":"Created","priority":3}}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"] == nil || fsm.Tasks["t1"].Instruction != "claim task" {
		t.Error("claim post wrong")
	}
}

func TestReefFSMApplyClaimPostIdempotent(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// First post
	cmd := RaftCommand{Type: CmdClaimPost, Timestamp: 1000,
		Payload: json.RawMessage(`{"task_id":"t1","task_data":{"id":"t1","instruction":"first","status":"Created","priority":3}}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})

	// Second post with same task_id (idempotent — should not overwrite)
	cmd2 := RaftCommand{Type: CmdClaimPost, Timestamp: 2000,
		Payload: json.RawMessage(`{"task_id":"t1","task_data":{"id":"t1","instruction":"second","status":"Created","priority":5}}`)}
	data2, _ := json.Marshal(cmd2)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: data2})

	if fsm.Tasks["t1"].Instruction != "first" {
		t.Error("idempotent claim post should not overwrite existing task")
	}
}

func TestReefFSMApplyClaimAssign(t *testing.T) {
	fsm := newFSMWithTask(t, "t1")
	cmd := RaftCommand{Type: CmdClaimAssign, Timestamp: 2000,
		Payload: json.RawMessage(`{"task_id":"t1","client_id":"cA"}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"].AssignedClient != "cA" || fsm.Tasks["t1"].Status != reef.TaskRunning {
		t.Error("claim assign wrong")
	}
}

func TestReefFSMApplyClaimExpire(t *testing.T) {
	fsm := newFSMWithRunningTask(t, "t1", "cA")
	cmd := RaftCommand{Type: CmdClaimExpire, Timestamp: 3000,
		Payload: json.RawMessage(`{"task_id":"t1","retry_count":1}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 3, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Tasks["t1"].Status != reef.TaskQueued {
		t.Errorf("status = %s, want %s", fsm.Tasks["t1"].Status, reef.TaskQueued)
	}
	if fsm.Tasks["t1"].AssignedClient != "" {
		t.Error("AssignedClient should be cleared on expire")
	}
}

// --- DAG Handler ---

func TestReefFSMApplyDagUpdate(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: CmdDagUpdate, Timestamp: 1000,
		Payload: json.RawMessage(`{"node_id":"n1","status":"running","output":"{\"progress\":50}","updated_at":1000}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.DagNodes["n1"] == nil {
		t.Fatal("dag node not stored")
	}
	if fsm.DagNodes["n1"].Status != "running" {
		t.Errorf("status = %s, want running", fsm.DagNodes["n1"].Status)
	}
}

func TestReefFSMApplyDagUpdateExisting(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// First update
	cmd1 := RaftCommand{Type: CmdDagUpdate, Timestamp: 1000,
		Payload: json.RawMessage(`{"node_id":"n1","status":"running","output":"{}","updated_at":1000}`)}
	data1, _ := json.Marshal(cmd1)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data1})

	// Second update (same node)
	cmd2 := RaftCommand{Type: CmdDagUpdate, Timestamp: 2000,
		Payload: json.RawMessage(`{"node_id":"n1","status":"completed","output":"{\"result\":\"ok\"}","updated_at":2000}`)}
	data2, _ := json.Marshal(cmd2)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: data2})

	if fsm.DagNodes["n1"].Status != "completed" {
		t.Errorf("status = %s, want completed", fsm.DagNodes["n1"].Status)
	}
}

// --- Error handling and edge cases ---

func TestReefFSMUnknownCommandType(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: RaftCommandType(99), Timestamp: 1000,
		Payload: json.RawMessage(`{}`), Proposer: "n1"}
	data, _ := json.Marshal(cmd)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if err != nil {
		t.Errorf("unknown command type should not error, got: %v", err)
	}
}

func TestReefFSMMalformedPayload(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// Valid RaftCommand envelope but inner payload is garbage JSON
	cmd := RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000,
		Payload: json.RawMessage(`not-valid-json`), Proposer: "n1"}
	data, _ := json.Marshal(cmd)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if err == nil {
		t.Error("malformed payload should return error")
	}
}

func TestReefFSMIdempotentClientOps(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// Register same client twice
	regPayload := json.RawMessage(`{"client_id":"c1","role":"coder","state":"connected"}`)
	cmd1 := RaftCommand{Type: CmdClientRegister, Timestamp: 1000, Payload: regPayload}
	data1, _ := json.Marshal(cmd1)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data1})

	cmd2 := RaftCommand{Type: CmdClientRegister, Timestamp: 2000, Payload: regPayload}
	data2, _ := json.Marshal(cmd2)
	fsm.Apply(&raftpb.Entry{Index: 2, Term: 1, Type: raftpb.EntryNormal, Data: data2})

	if fsm.Clients["c1"] == nil {
		t.Fatal("client should still exist after second register")
	}

	// Unregister twice — second should be no-op (no error)
	unregPayload := json.RawMessage(`{"client_id":"c1"}`)
	cmd3 := RaftCommand{Type: CmdClientUnregister, Timestamp: 3000, Payload: unregPayload}
	data3, _ := json.Marshal(cmd3)
	fsm.Apply(&raftpb.Entry{Index: 3, Term: 1, Type: raftpb.EntryNormal, Data: data3})

	if _, exists := fsm.Clients["c1"]; exists {
		t.Error("client should be deleted")
	}

	// Second unregister (idempotent)
	cmd4 := RaftCommand{Type: CmdClientUnregister, Timestamp: 4000, Payload: unregPayload}
	data4, _ := json.Marshal(cmd4)
	err := fsm.Apply(&raftpb.Entry{Index: 4, Term: 1, Type: raftpb.EntryNormal, Data: data4})
	if err != nil {
		t.Errorf("second unregister should not error: %v", err)
	}
}

func TestReefFSMTaskNotFound(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// Assign a non-existent task
	cmd := RaftCommand{Type: CmdTaskAssign, Timestamp: 1000,
		Payload: json.RawMessage(`{"task_id":"nonexistent","client_id":"c1"}`)}
	data, _ := json.Marshal(cmd)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if err == nil || err.Error() != "task nonexistent not found" {
		t.Errorf("expected 'task nonexistent not found', got: %v", err)
	}
}

func TestReefFSMGeneNotFound(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// Approve a non-existent gene
	payload, _ := json.Marshal(map[string]string{"gene_id": "nonexistent", "approver_node": "n1"})
	cmd := RaftCommand{Type: CmdGeneApprove, Timestamp: 1000, Payload: payload}
	data, _ := json.Marshal(cmd)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if err == nil || err.Error() != "gene nonexistent not found" {
		t.Errorf("expected 'gene nonexistent not found', got: %v", err)
	}
}

// --- FSM Enhancement Tests ---

func TestReefFSMLastAppliedSnapshotIndex(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	if fsm.LastApplied() != 0 {
		t.Error("initial LastApplied should be 0")
	}
	if fsm.SnapshotIndex() != 0 {
		t.Error("initial SnapshotIndex should be 0")
	}

	cmd := RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000,
		Payload: json.RawMessage(`{"id":"t1","instruction":"test","status":"Created"}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 5, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.LastApplied() != 5 {
		t.Errorf("LastApplied = %d, want 5", fsm.LastApplied())
	}

	fsm.Snapshot()
	if fsm.SnapshotIndex() != 5 {
		t.Errorf("SnapshotIndex = %d, want 5 after snapshot", fsm.SnapshotIndex())
	}
}

func TestReefFSMShouldCompact(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	fsm.SetCompactThreshold(5)

	fsm.Snapshot() // snapshotIndex = 0

	if fsm.ShouldCompact() {
		t.Error("should not compact when appliedIndex <= threshold")
	}

	// Apply 6 entries
	for i := uint64(1); i <= 6; i++ {
		cmd := RaftCommand{Type: CmdTaskEnqueue, Timestamp: int64(i * 1000),
			Payload: json.RawMessage(`{"id":"t` + string(rune('0'+i)) + `","instruction":"test","status":"Created"}`)}
		data, _ := json.Marshal(cmd)
		fsm.Apply(&raftpb.Entry{Index: i, Term: 1, Type: raftpb.EntryNormal, Data: data})
	}

	if !fsm.ShouldCompact() {
		t.Error("should compact when appliedIndex - snapshotIndex > threshold")
	}

	// After snapshot, should not compact
	fsm.Snapshot()
	if fsm.ShouldCompact() {
		t.Error("should not compact after snapshot")
	}
}

func TestReefFSMSaveLoadState(t *testing.T) {
	db := newTestDB(t)
	store := NewBoltStore(db)
	store.InitBuckets()

	fsm := NewReefFSM(db, store)
	cmd := RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000,
		Payload: json.RawMessage(`{"id":"t1","instruction":"persist me","status":"Created"}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})

	if err := fsm.SaveState(); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Create a new FSM with same store and load
	fsm2 := NewReefFSM(db, store)
	if err := fsm2.LoadState(); err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if fsm2.Tasks["t1"] == nil || fsm2.Tasks["t1"].Instruction != "persist me" {
		t.Error("LoadState did not restore task")
	}
}

func TestReefFSMSaveLoadStateNilStore(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil) // nil store = in-memory only
	if err := fsm.SaveState(); err != nil {
		t.Errorf("SaveState with nil store should not error: %v", err)
	}
	if err := fsm.LoadState(); err != nil {
		t.Errorf("LoadState with nil store should not error: %v", err)
	}
}

func TestReefFSMSnapshotEmpty(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	snap, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap) == 0 {
		t.Error("empty snapshot should produce valid JSON")
	}

	// Restore empty snapshot into another FSM
	fsm2 := NewReefFSM(newTestDB(t), nil)
	if err := fsm2.Restore(snap); err != nil {
		t.Fatalf("Restore empty snapshot: %v", err)
	}
	if !fsm.Equal(fsm2) {
		t.Error("restore of empty snapshot should match")
	}
}

func TestReefFSMRestoreNilSnapshot(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	if err := fsm.Restore(nil); err != nil {
		t.Fatalf("Restore nil snapshot: %v", err)
	}
	if len(fsm.Tasks) != 0 || len(fsm.Clients) != 0 {
		t.Error("restore nil snapshot should result in empty state")
	}
}

func TestReefFSMRestoreCorruptedJSON(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	err := fsm.Restore([]byte(`not-json`))
	if err == nil {
		t.Error("restore corrupted JSON should return error")
	}
}

func TestReefFSMDeterminism100x(t *testing.T) {
	// Generate a fixed sequence covering all 21 command types
	inputs := []struct {
		ct  RaftCommandType
		pay string
	}{
		// Register clients
		{CmdClientRegister, `{"client_id":"c1","role":"coder","state":"connected"}`},
		{CmdClientRegister, `{"client_id":"c2","role":"reviewer","state":"connected"}`},
		// Enqueue tasks
		{CmdTaskEnqueue, `{"id":"t1","instruction":"task 1","status":"Created","priority":5}`},
		{CmdTaskEnqueue, `{"id":"t2","instruction":"task 2","status":"Created","priority":3}`},
		// Start task
		{CmdTaskStart, `{"task_id":"t1","client_id":"c1"}`},
		// Assign task
		{CmdTaskAssign, `{"task_id":"t2","client_id":"c1"}`},
		// Complete a task
		{CmdTaskComplete, `{"task_id":"t1","execution_time_ms":100,"result":{"text":"done"}}`},
		// Fail a task
		{CmdTaskFailed, `{"task_id":"t2","error":{"type":"E001","message":"oops"},"attempt_history":[]}`},
		// Escalate
		{CmdTaskEscalate, `{"task_id":"t2","escalation_level":1}`},
		// Timeout (enqueue new task first)
		{CmdTaskEnqueue, `{"id":"t3","instruction":"task 3","status":"Created","priority":8}`},
		{CmdTaskTimedOut, `{"task_id":"t3","timeout_at":5000}`},
		// Cancel (enqueue new task first)
		{CmdTaskEnqueue, `{"id":"t4","instruction":"task 4","status":"Created","priority":1}`},
		{CmdTaskCancel, `{"task_id":"t4"}`},
		// Client stale and unregister
		{CmdClientStale, `{"client_id":"c2"}`},
		{CmdClientUnregister, `{"client_id":"c2"}`},
		// Gene operations
		{CmdGeneSubmit, `{"id":"g1","strategy_name":"fb","role":"coder","control_signal":"echo hi","version":1,"status":"submitted"}`},
		{CmdGeneApprove, `{"gene_id":"g1","approver_node":"n1"}`},
		{CmdGeneSubmit, `{"id":"g2","strategy_name":"nn","role":"reviewer","control_signal":"echo bye","version":1,"status":"submitted"}`},
		{CmdGeneReject, `{"gene_id":"g2","reason":"bad","rejecter_node":"n1"}`},
		// Skill draft operations
		{CmdSkillDraft, `{"id":"d1","role":"coder","skill_name":"greet","content":"hello","source_gene_ids":["g1"],"status":"pending_review"}`},
		{CmdSkillApprove, `{"draft_id":"d1","approver":"admin"}`},
		{CmdSkillDraft, `{"id":"d2","role":"coder","skill_name":"farewell","content":"bye","source_gene_ids":["g2"],"status":"pending_review"}`},
		{CmdSkillReject, `{"draft_id":"d2","reason":"bad content","rejecter":"admin"}`},
		// Claim operations
		{CmdClaimPost, `{"task_id":"t5","task_data":{"id":"t5","instruction":"claim task","status":"Created","priority":3}}`},
		{CmdClaimAssign, `{"task_id":"t5","client_id":"c1"}`},
		{CmdClaimExpire, `{"task_id":"t5","retry_count":1}`},
		// DAG operations
		{CmdDagUpdate, `{"node_id":"n1","status":"running","output":"{}","updated_at":10000}`},
		{CmdDagUpdate, `{"node_id":"n1","status":"completed","output":"{\"ok\":true}","updated_at":20000}`},
	}

	// Run 100 iterations, compare all results
	var firstSnapshotJSON string
	for round := 0; round < 100; round++ {
		fsm := NewReefFSM(newTestDB(t), nil)
		for i, in := range inputs {
			c := RaftCommand{
				Type:      in.ct,
				Payload:   json.RawMessage(in.pay),
				Timestamp: 1000000 + int64(i*1000),
				Proposer:  "node-1",
			}
			d, err := json.Marshal(c)
			if err != nil {
				t.Fatalf("round %d step %d marshal: %v", round, i, err)
			}
			e := &raftpb.Entry{
				Index: uint64(i + 1),
				Term:  1,
				Type:  raftpb.EntryNormal,
				Data:  d,
			}
			if err := fsm.Apply(e); err != nil {
				// Some commands return "not found" errors for non-existent items
				// These are informational; the FSM should continue
			}
		}
		snap, err := fsm.Snapshot()
		if err != nil {
			t.Fatalf("round %d snapshot: %v", round, err)
		}
		if round == 0 {
			firstSnapshotJSON = string(snap)
		} else if string(snap) != firstSnapshotJSON {
			t.Fatalf("round %d diverged from round 0", round)
		}
	}
}

func TestReefFSMEmptySnapshotRestore(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	snap, _ := fsm.Snapshot()

	fsm2 := NewReefFSM(newTestDB(t), nil)
	if err := fsm2.Restore(snap); err != nil {
		t.Fatalf("restore empty snap: %v", err)
	}
	if !fsm.Equal(fsm2) {
		t.Error("empty snapshot round-trip mismatch")
	}
}

func TestReefFSMFullSnapshotRestore(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)

	// Populate FSM with data across all 5 maps
	cmds := []struct {
		ct  RaftCommandType
		pay string
	}{
		{CmdTaskEnqueue, `{"id":"t1","instruction":"a","status":"Created"}`},
		{CmdTaskEnqueue, `{"id":"t2","instruction":"b","status":"Created"}`},
		{CmdTaskEnqueue, `{"id":"t3","instruction":"c","status":"Created"}`},
		{CmdClientRegister, `{"client_id":"c1","role":"coder","state":"connected"}`},
		{CmdClientRegister, `{"client_id":"c2","role":"reviewer","state":"connected"}`},
		{CmdGeneSubmit, `{"id":"g1","strategy_name":"fb","role":"coder","control_signal":"echo","version":1,"status":"submitted"}`},
		{CmdGeneSubmit, `{"id":"g2","strategy_name":"nn","role":"reviewer","control_signal":"echo2","version":1,"status":"submitted"}`},
		{CmdSkillDraft, `{"id":"d1","role":"coder","skill_name":"s1","content":"c1","source_gene_ids":["g1"],"status":"pending_review"}`},
		{CmdDagUpdate, `{"node_id":"n1","status":"running","output":"{}","updated_at":1000}`},
	}

	for i, c := range cmds {
		cmd := RaftCommand{Type: c.ct, Payload: json.RawMessage(c.pay), Timestamp: 1000 + int64(i), Proposer: "n1"}
		data, _ := json.Marshal(cmd)
		fsm.Apply(&raftpb.Entry{Index: uint64(i + 1), Term: 1, Type: raftpb.EntryNormal, Data: data})
	}

	snap, err := fsm.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	restored := NewReefFSM(newTestDB(t), nil)
	if err := restored.Restore(snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if !fsm.Equal(restored) {
		t.Error("full snapshot round-trip mismatch")
	}

	// Verify specific counts
	if len(restored.Tasks) != 3 {
		t.Errorf("tasks = %d, want 3", len(restored.Tasks))
	}
	if len(restored.Clients) != 2 {
		t.Errorf("clients = %d, want 2", len(restored.Clients))
	}
	if len(restored.Genes) != 2 {
		t.Errorf("genes = %d, want 2", len(restored.Genes))
	}
	if len(restored.Drafts) != 1 {
		t.Errorf("drafts = %d, want 1", len(restored.Drafts))
	}
	if len(restored.DagNodes) != 1 {
		t.Errorf("dagNodes = %d, want 1", len(restored.DagNodes))
	}
}

// Benchmark for snapshot of 10K tasks
func BenchmarkFSMSnapshot(b *testing.B) {
	fsm := NewReefFSM(nil, nil) // in-memory only
	// Insert 10,000 tasks
	for i := 0; i < 10000; i++ {
		task := &reef.Task{
			ID:          fmt.Sprintf("task-%d", i),
			Instruction: fmt.Sprintf("instruction-%d", i),
			Status:      reef.TaskCreated,
			Priority:    i % 10,
		}
		fsm.Tasks[task.ID] = task
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fsm.Snapshot()
	}
}

func TestReefFSMClientStaleNoop(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	// Mark stale a non-existent client (should not error)
	cmd := RaftCommand{Type: CmdClientStale, Timestamp: 1000,
		Payload: json.RawMessage(`{"client_id":"nonexistent"}`)}
	data, _ := json.Marshal(cmd)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if err != nil {
		t.Errorf("stale on non-existent client should not error: %v", err)
	}
}

// =====================================================================
// Coverage gap tests
// =====================================================================

func TestReefFSMShouldCompactZeroThreshold(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	fsm.SetCompactThreshold(0) // disable compaction
	// Apply 100 entries
	for i := uint64(1); i <= 100; i++ {
		cmd := RaftCommand{Type: CmdTaskEnqueue, Timestamp: int64(i * 1000),
			Payload: json.RawMessage(`{"id":"t` + fmt.Sprintf("%d", i) + `","instruction":"test","status":"Created"}`)}
		data, _ := json.Marshal(cmd)
		fsm.Apply(&raftpb.Entry{Index: i, Term: 1, Type: raftpb.EntryNormal, Data: data})
	}
	if fsm.ShouldCompact() {
		t.Error("should not compact when threshold is 0")
	}
}

func TestReefFSMLoadStateWithNilMaps(t *testing.T) {
	db := newTestDB(t)
	store := NewBoltStore(db)
	store.InitBuckets()

	// Save a snapshot with some nil maps
	snap := Fsmsnapshot{
		Tasks:    nil,
		Clients:  nil,
		Genes:    map[string]*evolution.Gene{"g1": {ID: "g1", StrategyName: "fb"}},
		Drafts:   nil,
		DagNodes: nil,
	}
	store.SaveSnapshot(snap)

	fsm := NewReefFSM(db, store)
	if err := fsm.LoadState(); err != nil {
		t.Fatalf("LoadState with nil maps: %v", err)
	}
	if len(fsm.Tasks) != 0 || len(fsm.Clients) != 0 || len(fsm.Drafts) != 0 || len(fsm.DagNodes) != 0 {
		t.Error("nil maps should be initialized as empty")
	}
	if len(fsm.Genes) != 1 {
		t.Error("non-nil map should be preserved")
	}
}

func TestRaftCommandTypeStringDefault(t *testing.T) {
	// Test the default case in String()
	unknown := RaftCommandType(99)
	if unknown.String() != "RaftCommandType(99)" {
		t.Errorf("String() = %q, want %q", unknown.String(), "RaftCommandType(99)")
	}
	zero := RaftCommandType(0)
	if zero.String() != "RaftCommandType(0)" {
		t.Errorf("String() = %q", zero.String())
	}
}

func TestClientConnPoolSendToLeaderWithLeader(t *testing.T) {
	pool := NewClientConnPool(PoolConfig{ServerAddrs: []string{"ws://n1:8080"}})
	pool.OnLeaderChange("ws://n1:8080")
	msg, _ := reef.NewMessage(reef.MsgTaskCompleted, "", nil)
	// SendToLeader always returns nil currently (no actual transport)
	err := pool.SendToLeader(msg)
	if err != nil {
		t.Errorf("SendToLeader with leader: %v", err)
	}
}

func TestLeaderedServerProposeAsLeader(t *testing.T) {
	ls := &LeaderedServer{}
	ls.isLeader.Store(true) // become leader
	err := ls.Propose(RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000})
	if err != nil {
		t.Errorf("Propose as leader should not error: %v", err)
	}
}

func TestRaftCommandStringAllTypes(t *testing.T) {
	// Ensure all 21 command types have correct String() output
	tests := []struct {
		ct   RaftCommandType
		want string
	}{
		{CmdTaskEnqueue, "CmdTaskEnqueue"},
		{CmdTaskAssign, "CmdTaskAssign"},
		{CmdTaskStart, "CmdTaskStart"},
		{CmdTaskComplete, "CmdTaskComplete"},
		{CmdTaskFailed, "CmdTaskFailed"},
		{CmdTaskCancel, "CmdTaskCancel"},
		{CmdTaskEscalate, "CmdTaskEscalate"},
		{CmdTaskTimedOut, "CmdTaskTimedOut"},
		{CmdClientRegister, "CmdClientRegister"},
		{CmdClientUnregister, "CmdClientUnregister"},
		{CmdClientStale, "CmdClientStale"},
		{CmdGeneSubmit, "CmdGeneSubmit"},
		{CmdGeneApprove, "CmdGeneApprove"},
		{CmdGeneReject, "CmdGeneReject"},
		{CmdSkillDraft, "CmdSkillDraft"},
		{CmdSkillApprove, "CmdSkillApprove"},
		{CmdSkillReject, "CmdSkillReject"},
		{CmdClaimPost, "CmdClaimPost"},
		{CmdClaimAssign, "CmdClaimAssign"},
		{CmdClaimExpire, "CmdClaimExpire"},
		{CmdDagUpdate, "CmdDagUpdate"},
	}
	for _, tt := range tests {
		if got := tt.ct.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.ct, got, tt.want)
		}
	}
}

func TestReefFSMApplyTaskCompleteNotFound(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: CmdTaskComplete, Timestamp: 1000,
		Payload: json.RawMessage(`{"task_id":"nonexistent","execution_time_ms":100,"result":{"text":"done"}}`)}
	data, _ := json.Marshal(cmd)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if err == nil || err.Error() != "task nonexistent not found" {
		t.Errorf("expected 'task nonexistent not found', got: %v", err)
	}
}

func TestReefFSMApplyTaskFailedNotFound(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: CmdTaskFailed, Timestamp: 1000,
		Payload: json.RawMessage(`{"task_id":"nonexistent","error":{"type":"E001"},"attempt_history":[]}`)}
	data, _ := json.Marshal(cmd)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if err == nil || err.Error() != "task nonexistent not found" {
		t.Errorf("expected 'task nonexistent not found', got: %v", err)
	}
}

func TestReefFSMApplyTaskCancelNotFound(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	cmd := RaftCommand{Type: CmdTaskCancel, Timestamp: 1000,
		Payload: json.RawMessage(`{"task_id":"nonexistent"}`)}
	data, _ := json.Marshal(cmd)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if err == nil || err.Error() != "task nonexistent not found" {
		t.Errorf("expected 'task nonexistent not found', got: %v", err)
	}
}

func TestReefFSMNewWithStore(t *testing.T) {
	db := newTestDB(t)
	store := NewBoltStore(db)
	store.InitBuckets()
	fsm := NewReefFSM(db, store)
	if fsm.Tasks == nil || fsm.Clients == nil || fsm.Genes == nil || fsm.Drafts == nil || fsm.DagNodes == nil {
		t.Error("all maps should be initialized")
	}
	if fsm.LastApplied() != 0 || fsm.SnapshotIndex() != 0 {
		t.Error("initial indices should be 0")
	}
}

func TestReefFSMDeserializeError(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t), nil)
	err := fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: []byte(`not-json`)})
	if err == nil {
		t.Error("deserialize error should be returned")
	}
}

func TestNewRaftCommandMarshalError(t *testing.T) {
	// Try to marshal a channel which can't be JSON-marshaled
	_, err := NewRaftCommand(CmdTaskEnqueue, make(chan int), "n1")
	if err == nil {
		t.Error("expected marshal error for unmarshalable payload")
	}
}
