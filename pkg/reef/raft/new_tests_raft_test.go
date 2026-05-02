package raft

import (
	"context"
	"strings"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	raftpb "go.etcd.io/raft/v3/raftpb"
)

func TestRaftNode_ProposeCmd_NoRaftNode(t *testing.T) {
	rn := &RaftNode{}
	cmd := &RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000}
	err := rn.ProposeCmd(context.Background(), cmd)
	if err == nil {
		t.Error("expected error when proposing to nil raft.Node")
	}
}

func TestRaftNode_RemoveNode_NoRaftNode(t *testing.T) {
	rn := &RaftNode{}
	err := rn.RemoveNode(context.Background(), 99)
	if err == nil {
		t.Error("expected error when removing from nil raft.Node")
	}
}

func TestNewRestartNode_NoStore(t *testing.T) {
	cfg := RaftConfig{NodeID: 1, Peers: []PeerInfo{{ID: 1, RaftAddr: "n1"}}}
	_, err := NewRestartNode(cfg, nil, nil, nil, nil)
	if err == nil {
		t.Error("expected error without store")
	}
}

func TestHTTPTransport_StartStop(t *testing.T) {
	tr := NewHTTPTransport(1, "127.0.0.1:0", nil, nil)
	err := tr.Start()
	if err != nil {
		t.Fatalf("failed to start: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	tr.Stop()
}

func TestHTTPTransport_StopBeforeStart(t *testing.T) {
	tr := NewHTTPTransport(2, "127.0.0.1:0", nil, nil)
	tr.Stop()
}

func TestHTTPTransport_Addr(t *testing.T) {
	tr := NewHTTPTransport(3, "127.0.0.1:9999", nil, nil)
	_ = tr.Addr()
}

func TestHTTPTransport_Incoming(t *testing.T) {
	tr := NewHTTPTransport(4, "127.0.0.1:0", nil, nil)
	ch := tr.Incoming()
	if ch == nil {
		t.Fatal("incoming channel nil")
	}
}

func TestHTTPTransport_AddPeer(t *testing.T) {
	tr := NewHTTPTransport(5, "127.0.0.1:0", nil, nil)
	tr.AddPeer(2, "http://n2:8080")
	tr.AddPeer(2, "http://n2:8080")
}

func TestHTTPTransport_RemovePeer(t *testing.T) {
	tr := NewHTTPTransport(6, "127.0.0.1:0", nil, nil)
	tr.AddPeer(3, "http://n3:8080")
	tr.RemovePeer(3)
	tr.RemovePeer(99)
}

func TestHTTPTransport_Send_NoPeers(t *testing.T) {
	tr := NewHTTPTransport(7, "127.0.0.1:0", nil, nil)
	msg := raftpb.Message{Type: raftpb.MsgHeartbeat, From: 7, To: 2}
	tr.Send([]raftpb.Message{msg})
}

func TestForwardToLeader_Success(t *testing.T) {
	gotRequest := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequest = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ls := &LeaderedServer{}
	cmd := &RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000}
	err := ls.ForwardToLeader(strings.TrimPrefix(srv.URL, "http://"), cmd)
	if err != nil {
		t.Fatalf("ForwardToLeader: %v", err)
	}
	if !gotRequest {
		t.Error("server did not receive request")
	}
}

func TestForwardToLeader_NetworkError(t *testing.T) {
	ls := &LeaderedServer{}
	cmd := &RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000}
	err := ls.ForwardToLeader("http://127.0.0.1:19999", cmd)
	if err == nil {
		t.Error("expected network error")
	}
}

func TestForwardToLeader_BadURL(t *testing.T) {
	ls := &LeaderedServer{}
	cmd := &RaftCommand{Type: CmdTaskEnqueue, Timestamp: 1000}
	err := ls.ForwardToLeader("://bad", cmd)
	if err == nil {
		t.Error("expected error for bad URL")
	}
}

func TestPoolConfig_Validate_EmptyAddrs(t *testing.T) {
	cfg := PoolConfig{}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty addr list")
	}
}

func TestPoolConfig_Validate_Valid(t *testing.T) {
	cfg := PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080"},
		ReconnectBackoff: 1 * time.Second,
		MaxReconnect:     30 * time.Second,
		PingInterval:     10 * time.Second,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPoolConfig_Validate_NegativeBackoff(t *testing.T) {
	cfg := PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080"},
		ReconnectBackoff: -1 * time.Second,
		PingInterval:     10 * time.Second,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for negative backoff")
	}
}

func TestPoolConfig_Validate_MaxReconnectLessThanBackoff(t *testing.T) {
	cfg := PoolConfig{
		ServerAddrs:      []string{"ws://n1:8080"},
		ReconnectBackoff: 10 * time.Second,
		MaxReconnect:     5 * time.Second,
		PingInterval:     10 * time.Second,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error when MaxReconnect < ReconnectBackoff")
	}
}

func TestDispatchTable_AllTypes(t *testing.T) {
	allTypes := []RaftCommandType{
		CmdTaskEnqueue, CmdTaskAssign, CmdTaskComplete, CmdTaskFailed,
		CmdTaskCancel, CmdTaskEscalate, CmdTaskTimedOut, CmdTaskStart,
		CmdClientRegister, CmdClientUnregister, CmdClientStale,
		CmdGeneSubmit, CmdGeneApprove, CmdGeneReject,
		CmdSkillDraft, CmdSkillApprove, CmdSkillReject,
		CmdClaimPost, CmdClaimAssign, CmdClaimExpire,
		CmdDagUpdate,
	}
	for _, ct := range allTypes {
		_ = GetDispatchTarget(ct)
		if !DispatchesToConsensus(ct) {
			t.Errorf("%s should be consensus", ct.String())
		}
	}
}

func openTestBolt(t *testing.T) *bolt.DB {
	t.Helper()
	db, err := bolt.Open(t.TempDir()+"/test.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}


func TestBoltStore_HardState_Roundtrip(t *testing.T) {
	db := openTestBolt(t)
	store := NewBoltStore(db)
	store.InitBuckets()
	err := store.SaveHardState(raftpb.HardState{Term: 5, Vote: 2, Commit: 10})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	hs, err := store.LoadHardState()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if hs.Term != 5 || hs.Vote != 2 || hs.Commit != 10 {
		t.Errorf("roundtrip: %+v", hs)
	}
}

func TestBoltStore_LoadHardState_Empty(t *testing.T) {
	db := openTestBolt(t)
	store := NewBoltStore(db)
	store.InitBuckets()
	_, err := store.LoadHardState()
	if err != nil {
		t.Fatal(err)
	}
}

func TestBoltStore_TruncateEntries(t *testing.T) {
	db := openTestBolt(t)
	store := NewBoltStore(db)
	store.InitBuckets()
	if err := store.TruncateEntries(5); err != nil {
		t.Fatal(err)
	}
}

func TestBoltStore_CompactLog(t *testing.T) {
	db := openTestBolt(t)
	store := NewBoltStore(db)
	store.InitBuckets()
	if err := store.CompactLog(5); err != nil {
		t.Fatal(err)
	}
}

func TestBoltStore_ClearSnapshot(t *testing.T) {
	db := openTestBolt(t)
	store := NewBoltStore(db)
	store.InitBuckets()
	if err := store.ClearSnapshot(); err != nil {
		t.Fatal(err)
	}
}

func TestBoltStore_CloseIdempotent(t *testing.T) {
	db := openTestBolt(t)
	store := NewBoltStore(db)
	store.Close()
	store.Close()
}

func TestBoltStore_LockStubs(t *testing.T) {
	db := openTestBolt(t)
	store := NewBoltStore(db)
	_, err := store.PessimisticLockAcquire("k1", 10*time.Second)
	if err == nil {
		t.Error("expected not-implemented")
	}
	err = store.PessimisticLockRelease("k1")
	if err == nil {
		t.Error("expected not-implemented")
	}
	err = store.PessimisticLeaseRenew("k1", 10*time.Second)
	if err == nil {
		t.Error("expected not-implemented")
	}
}

func TestClusterStatus_Fields(t *testing.T) {
	st := ClusterStatus{
		NodeID:      1,
		IsLeader:    true,
		LeaderID:    1,
		Term:        3,
		LastApplied: 42,
		Peers:       3,
	}
	if !st.IsLeader {
		t.Error("IsLeader")
	}
	if st.Term != 3 {
		t.Errorf("Term: %d", st.Term)
	}
}
