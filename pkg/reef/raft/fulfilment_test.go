// Package raft provides Reef v1 Raft-based federation.
// TDD Tests: BoltStore, RaftCommand, ReefFSM, ClientConnPool, LeaderedServer
package raft

import (
	"encoding/json"
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

func TestAllRaftCommandTypes(t *testing.T) {
	for _, ct := range []RaftCommandType{
		CmdTaskEnqueue, CmdTaskAssign, CmdTaskStart, CmdTaskComplete,
		CmdTaskFailed, CmdTaskCancel, CmdTaskEscalate, CmdTaskTimedOut,
		CmdClientRegister, CmdClientUnregister, CmdClientStale,
		CmdGeneApprove, CmdGeneReject, CmdSkillDraft, CmdSkillApprove, CmdSkillReject,
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

func TestReefFSMApplyTaskEnqueue(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t))
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
	fsm := NewReefFSM(newTestDB(t))
	cmd := RaftCommand{Type: CmdClientRegister, Timestamp: 1000, Payload: json.RawMessage(`{"client_id":"cA","role":"coder","state":"connected"}`)}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
	if fsm.Clients["cA"] == nil || fsm.Clients["cA"].Role != "coder" {
		t.Error("register wrong")
	}
}

func TestReefFSMApplyGeneApprove(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t))
	g, _ := json.Marshal(&evolution.Gene{ID: "g1", StrategyName: "fb", Role: "coder", ControlSignal: "go build", Version: 1})
	cmd := RaftCommand{Type: CmdGeneApprove, Timestamp: 1000, Payload: g}
	data, _ := json.Marshal(cmd)
	fsm.Apply(&raftpb.Entry{Index: 1, Term: 1, Type: raftpb.EntryNormal, Data: data})
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
		a := NewReefFSM(newTestDB(t))
		b := NewReefFSM(newTestDB(t))
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
	r := NewReefFSM(newTestDB(t))
	r.Restore(snap)
	if !fsm.Equal(r) {
		t.Error("restore mismatch")
	}
}

func TestReefFSMConfChangeNoop(t *testing.T) {
	fsm := NewReefFSM(newTestDB(t))
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

func newFSMWithTask(t *testing.T, id string) *ReefFSM {
	fsm := NewReefFSM(newTestDB(t))
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
