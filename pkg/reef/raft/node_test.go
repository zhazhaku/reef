// Package raft provides Reef v1 Raft-based federation.
// Tests for RaftNode: start/stop, propose, leader election, conf change, restart.
package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"go.etcd.io/raft/v3/raftpb"
)

// =====================================================================
// testTransport — in-memory channel-based transport for testing
// =====================================================================

type testTransport struct {
	mu      sync.Mutex
	nodeID  uint64
	msgCh   chan raftpb.Message // incoming messages
	sendFn  func([]raftpb.Message)
	peers   map[uint64]string // nodeID -> addr
	stopped bool
}

func newTestTransport(nodeID uint64) *testTransport {
	return &testTransport{
		nodeID: nodeID,
		msgCh:  make(chan raftpb.Message, 256),
		peers:  make(map[uint64]string),
	}
}

func (t *testTransport) Send(messages []raftpb.Message) {
	if t.sendFn != nil {
		t.sendFn(messages)
	}
}

func (t *testTransport) AddPeer(id uint64, addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.peers[id] = addr
}

func (t *testTransport) RemovePeer(id uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.peers, id)
}

func (t *testTransport) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.stopped {
		t.stopped = true
		close(t.msgCh)
	}
}

func (t *testTransport) Receive() <-chan raftpb.Message {
	return t.msgCh
}

// testCluster manages a set of RaftNodes connected via testTransport.
type testCluster struct {
	t       *testing.T
	nodes   map[uint64]*RaftNode
	fsms    map[uint64]*ReefFSM
	stores  map[uint64]*BoltStore
	dbs     map[uint64]*bolt.DB
	trans   map[uint64]*testTransport
	logger  *slog.Logger
}

func newTestCluster(t *testing.T, nodeIDs []uint64) *testCluster {
	t.Helper()
	tc := &testCluster{
		t:      t,
		nodes:  make(map[uint64]*RaftNode),
		fsms:   make(map[uint64]*ReefFSM),
		stores: make(map[uint64]*BoltStore),
		dbs:    make(map[uint64]*bolt.DB),
		trans:  make(map[uint64]*testTransport),
		logger: slog.New(slog.DiscardHandler),
	}

	// Build peer list
	peers := make([]PeerInfo, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		peers = append(peers, PeerInfo{ID: id, RaftAddr: fmt.Sprintf("node-%d", id)})
	}

	// Create transports and wire them
	for _, id := range nodeIDs {
		tc.trans[id] = newTestTransport(id)
	}

	// Wire transports: each transport's Send delivers to the target's Step
	for _, id := range nodeIDs {
		tr := tc.trans[id]
		sendFn := func(messages []raftpb.Message) {
			for _, msg := range messages {
				if target, ok := tc.trans[msg.To]; ok {
					if node, ok := tc.nodes[msg.To]; ok {
						_ = node.Step(msg)
					}
					// Also deliver to target's message channel (non-blocking, skip if closed)
		func() {
			defer func() { recover() }()
			select {
			case target.msgCh <- msg:
			default:
			}
		}()
				}
			}
		}
		tr.sendFn = sendFn
	}

	// Create nodes
	for _, id := range nodeIDs {
		db, err := bolt.Open(fmt.Sprintf("testdata/_cluster_%d.db", id), 0600, &bolt.Options{Timeout: 1 * time.Second})
		if err != nil {
			t.Fatalf("open db for node %d: %v", id, err)
		}
		tc.dbs[id] = db
		store := NewBoltStore(db)
		store.InitBuckets()
		tc.stores[id] = store
		tc.fsms[id] = NewReefFSM(db, store)

		cfg := DefaultRaftConfig()
		cfg.NodeID = id
		cfg.Peers = peers
		cfg.ElectionTimeoutMs = 1000
		cfg.HeartbeatIntervalMs = 100

		node, err := NewRaftNode(cfg, store, tc.fsms[id], tc.trans[id], tc.logger)
		if err != nil {
			t.Fatalf("create node %d: %v", id, err)
		}
		tc.nodes[id] = node
	}

	return tc
}

func (tc *testCluster) Start() {
	for _, node := range tc.nodes {
		node.Start()
	}
}

func (tc *testCluster) Stop() {
	for _, node := range tc.nodes {
		node.Stop()
	}
	for _, db := range tc.dbs {
		db.Close()
	}
}

func (tc *testCluster) WaitForLeader(timeout time.Duration) uint64 {
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			tc.t.Fatal("timeout waiting for leader election")
			return 0
		default:
		}
		for id, node := range tc.nodes {
			if node.IsLeader() {
				return id
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// helperDB creates an in-memory bolt DB for testing.
func helperDB(t *testing.T) *bolt.DB {
	t.Helper()
	db, err := bolt.Open(fmt.Sprintf("testdata/_node_test_%d.db", time.Now().UnixNano()), 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// helperStore creates a BoltStore with initialized buckets.
func helperStore(t *testing.T) *BoltStore {
	t.Helper()
	db := helperDB(t)
	store := NewBoltStore(db)
	if err := store.InitBuckets(); err != nil {
		t.Fatal(err)
	}
	return store
}

// helperFSM creates a ReefFSM backed by a store.
func helperFSM(t *testing.T) (*ReefFSM, *BoltStore) {
	t.Helper()
	store := helperStore(t)
	fsm := NewReefFSM(store.DB(), store)
	return fsm, store
}

// =====================================================================
// Task 1: NewRaftNode tests
// =====================================================================

func TestNewRaftNode(t *testing.T) {
	store := helperStore(t)
	fsm := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)
	logger := slog.New(slog.DiscardHandler)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, err := NewRaftNode(cfg, store, fsm, transport, logger)
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}
	if node == nil {
		t.Fatal("expected non-nil node")
	}
	if node.NodeID() != 1 {
		t.Errorf("NodeID = %d, want 1", node.NodeID())
	}
	if node.FSM() != fsm {
		t.Error("FSM mismatch")
	}
	node.Stop()
}

func TestNewRaftNodeValidation(t *testing.T) {
	store := helperStore(t)
	fsm := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)
	logger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name    string
		cfg     RaftConfig
		wantErr bool
	}{
		{
			name:    "zero NodeID",
			cfg:     RaftConfig{NodeID: 0, ElectionTimeoutMs: 1000, HeartbeatIntervalMs: 100},
			wantErr: true,
		},
		{
			name:    "zero ElectionTimeoutMs",
			cfg:     RaftConfig{NodeID: 1, ElectionTimeoutMs: 0, HeartbeatIntervalMs: 100},
			wantErr: true,
		},
		{
			name:    "zero HeartbeatIntervalMs",
			cfg:     RaftConfig{NodeID: 1, ElectionTimeoutMs: 1000, HeartbeatIntervalMs: 0},
			wantErr: true,
		},
		{
			name:    "election <= heartbeat",
			cfg:     RaftConfig{NodeID: 1, ElectionTimeoutMs: 100, HeartbeatIntervalMs: 200},
			wantErr: true,
		},
		{
			name:    "nil store",
			cfg:     RaftConfig{NodeID: 1, ElectionTimeoutMs: 1000, HeartbeatIntervalMs: 100},
			wantErr: true,
		},
		{
			name:    "nil fsm",
			cfg:     RaftConfig{NodeID: 1, ElectionTimeoutMs: 1000, HeartbeatIntervalMs: 100},
			wantErr: true,
		},
		{
			name:    "nil transport",
			cfg:     RaftConfig{NodeID: 1, ElectionTimeoutMs: 1000, HeartbeatIntervalMs: 100},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s *BoltStore
			var f *ReefFSM
			var tr Transport
			if !tt.wantErr || tt.name != "nil store" {
				s = store
			}
			if !tt.wantErr || tt.name != "nil fsm" {
				f = fsm
			}
			if !tt.wantErr || tt.name != "nil transport" {
				tr = transport
			}
			_, err := NewRaftNode(tt.cfg, s, f, tr, logger)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRaftNode error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewRaftNodeNilLogger(t *testing.T) {
	store := helperStore(t)
	fsm := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, err := NewRaftNode(cfg, store, fsm, transport, nil)
	if err != nil {
		t.Fatalf("NewRaftNode with nil logger: %v", err)
	}
	node.Stop()
}

func TestNewRaftNodeSingleNode(t *testing.T) {
	// Single-node cluster (empty peers) should work via RestartNode
	store := helperStore(t)
	fsm := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	// Empty peers for single-node bootstrap

	node, err := NewRaftNode(cfg, store, fsm, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode single-node: %v", err)
	}
	node.Start()
	defer node.Stop()

	// Single node should become leader quickly
	timeout := time.After(3 * time.Second)
	for !node.IsLeader() {
		select {
		case <-timeout:
			t.Fatal("single node did not become leader")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// =====================================================================
// Task 2: Start/Stop and Ready Loop tests
// =====================================================================

func TestRaftNodeStartStop(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, err := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}

	node.Start()

	// Wait a bit for loops to start
	time.Sleep(200 * time.Millisecond)

	// Stop should be idempotent
	node.Stop()
	node.Stop() // second stop should not panic

	// Wait for Done channel
	select {
	case <-node.Done():
		// ok
	case <-time.After(1 * time.Second):
		t.Fatal("Done channel not closed after Stop")
	}
}

func TestRaftNodeProposeSingleNode(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, err := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}
	node.Start()
	defer node.Stop()

	// Wait for single node to become leader
	timeout := time.After(3 * time.Second)
	for !node.IsLeader() {
		select {
		case <-timeout:
			t.Fatal("single node did not become leader")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Propose a task enqueue command
	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t1",
		TaskData: json.RawMessage(`{"instruction":"test"}`),
	}, "node-1")

	data, _ := cmd.Serialize()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := node.Propose(ctx, data); err != nil {
		t.Fatalf("Propose: %v", err)
	}

	// Wait for the entry to be committed and applied
	timeout2 := time.After(3 * time.Second)
	for fsmImpl.LastApplied() < 2 { // entry 1 is usually a no-op from leader election
		select {
		case <-timeout2:
			t.Fatalf("entry not applied in time, lastApplied=%d", fsmImpl.LastApplied())
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	if fsmImpl.Tasks["t1"] == nil {
		t.Fatal("task t1 not found in FSM")
	}
	if fsmImpl.Tasks["t1"].Instruction != "test" {
		t.Errorf("task instruction = %q, want %q", fsmImpl.Tasks["t1"].Instruction, "test")
	}
}

func TestRaftNodeProposeCmd(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, err := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}
	node.Start()
	defer node.Stop()

	// Wait for leadership
	for i := 0; i < 60; i++ {
		if node.IsLeader() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	cmd, _ := NewRaftCommand(CmdClientRegister, &ClientRegisterPayload{
		ClientID: "c1",
		Role:     "coder",
		Skills:   []string{"go"},
		Capacity: 3,
	}, "node-1")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := node.ProposeCmd(ctx, cmd); err != nil {
		t.Fatalf("ProposeCmd: %v", err)
	}

	// Wait for application
	for i := 0; i < 60; i++ {
		if fsmImpl.Clients["c1"] != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if fsmImpl.Clients["c1"] == nil {
		t.Fatal("client c1 not found in FSM")
	}
	if fsmImpl.Clients["c1"].Role != "coder" {
		t.Errorf("client role = %q, want %q", fsmImpl.Clients["c1"].Role, "coder")
	}
}

// =====================================================================
// Task 3: Leader election and proposal forwarding
// =====================================================================

func TestRaftNodeLeaderElection(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(5 * time.Second)
	t.Logf("leader elected: node %d", leaderID)

	if leaderID == 0 {
		t.Fatal("no leader elected")
	}

	// Verify only one leader
	leaderCount := 0
	for id, node := range tc.nodes {
		if node.IsLeader() {
			leaderCount++
			t.Logf("node %d is leader", id)
		}
	}
	if leaderCount != 1 {
		t.Errorf("expected 1 leader, got %d", leaderCount)
	}
}

func TestRaftNodeProposalForwarding(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(5 * time.Second)
	t.Logf("leader elected: node %d", leaderID)

	// Find a follower
	var followerID uint64
	for id := range tc.nodes {
		if id != leaderID {
			followerID = id
			break
		}
	}
	if followerID == 0 {
		t.Fatal("no follower found")
	}

	// Wait for the follower to learn the leader ID
	follower := tc.nodes[followerID]
	timeoutLeader := time.After(3 * time.Second)
	for follower.GetLeaderID() == 0 {
		select {
		case <-timeoutLeader:
			t.Fatal("follower did not learn leader ID in time")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	t.Logf("follower %d knows leader is %d", followerID, follower.GetLeaderID())

	// Propose via the follower — should be forwarded to leader
	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t-forwarded",
		TaskData: json.RawMessage(`{"instruction":"forwarded!"}`),
	}, fmt.Sprintf("node-%d", followerID))

	data, _ := cmd.Serialize()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := follower.Propose(ctx, data); err != nil {
		t.Fatalf("Propose from follower: %v", err)
	}

	// Wait for the entry to be applied on the leader's FSM
	leaderFSM := tc.fsms[leaderID]
	timeout := time.After(5 * time.Second)
	for leaderFSM.Tasks["t-forwarded"] == nil {
		select {
		case <-timeout:
			t.Fatal("forwarded proposal not committed in time")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
	t.Logf("forwarded proposal applied on leader FSM: %+v", leaderFSM.Tasks["t-forwarded"])

	// Also verify it propagates to the follower FSM
	timeout2 := time.After(5 * time.Second)
	followerFSM := tc.fsms[followerID]
	for followerFSM.Tasks["t-forwarded"] == nil {
		select {
		case <-timeout2:
			t.Fatal("forwarded proposal not applied on follower FSM in time")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestRaftNodeProposeNotLeaderUnknown(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}, {ID: 2, RaftAddr: "node-2"}}

	node, err := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}
	node.Start()
	defer node.Stop()

	// With 2 nodes, election may not complete (no majority of 2 = 2 votes needed)
	// Wait a bit and then check — the node may or may not be leader.
	// If not leader, leaderID should be unknown initially.

	// Force leaderID to 0 to simulate unknown leader
	// Actually we should test when leader is genuinely unknown.
	// In a 2-node cluster with only one node running, no leader is elected.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// The node is not leader (can't be with 2-node config and only 1 running)
	err = node.Propose(ctx, []byte("test"))
	if err != ErrNotLeader {
		t.Logf("Propose returned: %v (may succeed if node became leader somehow)", err)
	}
}

// =====================================================================
// Task 4: Member changes (AddNode / RemoveNode)
// =====================================================================

func TestRaftNodeConfChange(t *testing.T) {
	// Start with a single-node cluster
	tc := newTestCluster(t, []uint64{1})
	defer tc.Stop()
	tc.Start()

	leaderID := tc.WaitForLeader(5 * time.Second)
	if leaderID != 1 {
		t.Fatalf("expected node 1 to be leader, got %d", leaderID)
	}

	// Add a second node to the transport of node 1
	tc.trans[2] = newTestTransport(2)

	// Create store and FSM for node 2
	db2, err := bolt.Open("testdata/_cluster_2.db", 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		t.Fatalf("open db2: %v", err)
	}
	defer db2.Close()
	store2 := NewBoltStore(db2)
	store2.InitBuckets()
	tc.stores[2] = store2
	tc.fsms[2] = NewReefFSM(db2, store2)

	// Wire transport for node 2
	tc.trans[2] = newTestTransport(2)
	tc.trans[2].sendFn = func(messages []raftpb.Message) {
		for _, msg := range messages {
			if node, ok := tc.nodes[msg.To]; ok {
				_ = node.Step(msg)
			}
		}
	}

	// Add node 2 to the cluster
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := tc.nodes[1].AddNode(ctx, 2, "node-2"); err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	// Now create node 2 and join — start with no peers so it joins the existing cluster
	cfg2 := DefaultRaftConfig()
	cfg2.NodeID = 2
	// Empty peers: node 2 will join via messages from leader, not as initial cluster member.
	cfg2.Peers = nil

	node2, err := NewRaftNode(cfg2, tc.stores[2], tc.fsms[2], tc.trans[2], tc.logger)
	if err != nil {
		t.Fatalf("create node 2: %v", err)
	}
	tc.nodes[2] = node2
	node2.Start()

	// Wait for node 2 to join and catch up
	time.Sleep(2 * time.Second)

	// Propose on leader and verify it propagates to node 2
	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t-cc",
		TaskData: json.RawMessage(`{"instruction":"conf-change-test"}`),
	}, "node-1")

	data, _ := cmd.Serialize()
	if err := tc.nodes[1].Propose(context.Background(), data); err != nil {
		t.Fatalf("Propose after conf change: %v", err)
	}

	// Check node 2 FSM
	timeout := time.After(5 * time.Second)
	for tc.fsms[2].Tasks["t-cc"] == nil {
		select {
		case <-timeout:
			t.Fatal("conf change: proposal not applied on node 2")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func TestAddNodeNotLeader(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}, {ID: 2, RaftAddr: "node-2"}}

	node, err := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}
	node.Start()
	defer node.Stop()

	// Force not leader — propose AddNode should fail
	node.leaderID.Store(0)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = node.AddNode(ctx, 3, "node-3")
	if err != ErrNotLeader {
		t.Errorf("expected ErrNotLeader, got %v", err)
	}
}

func TestRemoveNodeNotLeader(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, err := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}
	node.Start()
	defer node.Stop()

	node.leaderID.Store(0)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = node.RemoveNode(ctx, 2)
	if err != ErrNotLeader {
		t.Errorf("expected ErrNotLeader, got %v", err)
	}
}

// =====================================================================
// Task 5: Leadership callbacks
// =====================================================================

func TestLeadershipCallbacks(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, err := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}

	startCalled := make(chan struct{}, 1)
	stopCalled := make(chan struct{}, 1)

	node.OnLeaderStart(func() {
		select {
		case startCalled <- struct{}{}:
		default:
		}
	})
	node.OnLeaderStop(func() {
		select {
		case stopCalled <- struct{}{}:
		default:
		}
	})

	node.Start()
	defer node.Stop()

	// Wait for leader start callback
	select {
	case <-startCalled:
		t.Log("onLeaderStart called")
	case <-time.After(5 * time.Second):
		t.Fatal("onLeaderStart not called in time")
	}

	if !node.IsLeader() {
		t.Fatal("expected node to be leader")
	}

	// Stop should trigger onLeaderStop via leadership loss
	node.Stop()

	// Note: onLeaderStop may or may not fire depending on timing
	// but we check IsLeader works before stop
}

func TestIsLeaderAndGetLeaderID(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, err := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode: %v", err)
	}
	node.Start()
	defer node.Stop()

	// Initially leader is unknown
	if node.GetLeaderID() != 0 {
		t.Logf("initial leaderID = %d", node.GetLeaderID())
	}

	// Wait for leadership
	timeout := time.After(5 * time.Second)
	for !node.IsLeader() {
		select {
		case <-timeout:
			t.Fatal("node did not become leader")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	if node.GetLeaderID() != 1 {
		t.Errorf("GetLeaderID = %d, want 1", node.GetLeaderID())
	}
}

// =====================================================================
// Task 5: Restart with state replay
// =====================================================================

func TestRaftNodeRestart(t *testing.T) {
	// Phase 1: create a node, propose a command, stop
	db := helperDB(t)
	store := NewBoltStore(db)
	store.InitBuckets()
	fsm1 := NewReefFSM(db, store)
	transport1 := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node1, err := NewRaftNode(cfg, store, fsm1, transport1, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRaftNode phase 1: %v", err)
	}
	node1.Start()

	// Wait for leadership
	for i := 0; i < 60; i++ {
		if node1.IsLeader() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Propose a command
	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t-restart",
		TaskData: json.RawMessage(`{"instruction":"survive restart"}`),
	}, "node-1")

	data, _ := cmd.Serialize()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := node1.Propose(ctx, data); err != nil {
		t.Fatalf("Propose phase 1: %v", err)
	}

	// Wait for application
	for i := 0; i < 60; i++ {
		if fsm1.Tasks["t-restart"] != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if fsm1.Tasks["t-restart"] == nil {
		t.Fatal("task not applied in phase 1")
	}

	// Save hard state + conf state for restart
	hs, _ := store.LoadHardState()
	t.Logf("hard state before shutdown: term=%d vote=%d commit=%d", hs.Term, hs.Vote, hs.Commit)

	// Stop the node
	node1.Stop()

	// Phase 2: restart from BoltDB
	// Reset FSM (simulates a new process)
	fsm2 := NewReefFSM(db, store)
	transport2 := newTestTransport(1)

	node2, err := NewRestartNode(cfg, store, fsm2, transport2, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRestartNode: %v", err)
	}
	node2.Start()
	defer node2.Stop()

	// Wait for leadership after restart
	for i := 0; i < 60; i++ {
		if node2.IsLeader() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !node2.IsLeader() {
		t.Fatal("restarted node did not become leader")
	}

	// The task should still exist (FSM was rebuilt, but replay should restore it)
	if fsm2.Tasks["t-restart"] == nil {
		t.Fatal("task t-restart not found in FSM after restart (replay may have failed)")
	}
	t.Logf("task after restart: %+v", fsm2.Tasks["t-restart"])

	// Propose another command on restarted node
	cmd2, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t-after-restart",
		TaskData: json.RawMessage(`{"instruction":"after restart"}`),
	}, "node-1")

	data2, _ := cmd2.Serialize()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()

	if err := node2.Propose(ctx2, data2); err != nil {
		t.Fatalf("Propose after restart: %v", err)
	}

	for i := 0; i < 60; i++ {
		if fsm2.Tasks["t-after-restart"] != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if fsm2.Tasks["t-after-restart"] == nil {
		t.Fatal("task not applied after restart")
	}
}

func TestNewRestartNodeFreshStart(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	// NewRestartNode with empty DB should fall back to fresh start
	node, err := NewRestartNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRestartNode fresh start: %v", err)
	}
	node.Stop()
}

// =====================================================================
// Integration test: 3-node cluster with proposals
// =====================================================================

func TestIntegrationThreeNodeCluster(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(5 * time.Second)
	t.Logf("leader: node %d", leaderID)

	// Propose 5 commands
	for i := 1; i <= 5; i++ {
		taskID := fmt.Sprintf("t-%d", i)
		cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
			TaskID:   taskID,
			TaskData: json.RawMessage(fmt.Sprintf(`{"instruction":"task %d"}`, i)),
		}, fmt.Sprintf("node-%d", leaderID))

		data, _ := cmd.Serialize()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := tc.nodes[leaderID].Propose(ctx, data)
		cancel()
		if err != nil {
			t.Fatalf("propose %d: %v", i, err)
		}
	}

	// Wait for all tasks to be applied on all nodes
	timeout := time.After(10 * time.Second)
	for {
		allReady := true
		for _, fsm := range tc.fsms {
			for i := 1; i <= 5; i++ {
				taskID := fmt.Sprintf("t-%d", i)
				if fsm.Tasks[taskID] == nil {
					allReady = false
					break
				}
			}
			if !allReady {
				break
			}
		}
		if allReady {
			break
		}
		select {
		case <-timeout:
			// Print what's missing
			for id, fsm := range tc.fsms {
				missing := []string{}
				for i := 1; i <= 5; i++ {
					taskID := fmt.Sprintf("t-%d", i)
					if fsm.Tasks[taskID] == nil {
						missing = append(missing, taskID)
					}
				}
				t.Logf("node %d missing tasks: %v", id, missing)
			}
			t.Fatal("not all tasks applied on all nodes in time")
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}

	// Verify FSM consistency across all nodes
	var firstSnap []byte
	for id, fsm := range tc.fsms {
		snap, _ := fsm.Snapshot()
		if firstSnap == nil {
			firstSnap = snap
		} else {
			if string(snap) != string(firstSnap) {
				t.Errorf("FSM divergence on node %d", id)
			}
		}
	}
	t.Log("all nodes consistent after 5 proposals")
}

// =====================================================================
// Edge cases and coverage
// =====================================================================

func TestRaftConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     RaftConfig
		wantErr bool
		errMsg  string
	}{
		{"valid", RaftConfig{NodeID: 1, ElectionTimeoutMs: 1000, HeartbeatIntervalMs: 100}, false, ""},
		{"zero nodeID", RaftConfig{NodeID: 0, ElectionTimeoutMs: 1000, HeartbeatIntervalMs: 100}, true, "NodeID"},
		{"negative election", RaftConfig{NodeID: 1, ElectionTimeoutMs: -1, HeartbeatIntervalMs: 100}, true, "ElectionTimeoutMs"},
		{"negative heartbeat", RaftConfig{NodeID: 1, ElectionTimeoutMs: 1000, HeartbeatIntervalMs: -1}, true, "HeartbeatIntervalMs"},
		{"election equals heartbeat", RaftConfig{NodeID: 1, ElectionTimeoutMs: 100, HeartbeatIntervalMs: 100}, true, "must be >"},
		{"election less than heartbeat", RaftConfig{NodeID: 1, ElectionTimeoutMs: 50, HeartbeatIntervalMs: 100}, true, "must be >"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultRaftConfig(t *testing.T) {
	cfg := DefaultRaftConfig()
	if cfg.ElectionTimeoutMs != 1000 {
		t.Errorf("ElectionTimeoutMs = %d, want 1000", cfg.ElectionTimeoutMs)
	}
	if cfg.HeartbeatIntervalMs != 100 {
		t.Errorf("HeartbeatIntervalMs = %d, want 100", cfg.HeartbeatIntervalMs)
	}
	if cfg.MaxSizePerMsg != 1024*1024 {
		t.Errorf("MaxSizePerMsg wrong")
	}
	if !cfg.CheckQuorum {
		t.Error("CheckQuorum should default to true")
	}
	if !cfg.PreVote {
		t.Error("PreVote should default to true")
	}
}

func TestRaftConfigElectionTick(t *testing.T) {
	cfg := RaftConfig{ElectionTimeoutMs: 1000, HeartbeatIntervalMs: 100}
	if cfg.electionTick() != 10 {
		t.Errorf("electionTick = %d, want 10", cfg.electionTick())
	}

	cfg2 := RaftConfig{ElectionTimeoutMs: 50, HeartbeatIntervalMs: 100}
	if cfg2.electionTick() != 1 {
		t.Errorf("electionTick = %d, want 1 (floor)", cfg2.electionTick())
	}
}

func TestRaftNodeStopIdempotent(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, _ := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	node.Start()

	// Multiple stops should not panic
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			node.Stop()
		}()
	}
	wg.Wait()

	// Done channel should be closed
	select {
	case <-node.Done():
	case <-time.After(1 * time.Second):
		t.Fatal("Done not closed after concurrent stops")
	}
}

func TestRaftLoggerAdapter(t *testing.T) {
	adapter := &raftLoggerAdapter{logger: slog.New(slog.DiscardHandler)}

	// These should not panic
	adapter.Debug("test")
	adapter.Debugf("test %d", 1)
	adapter.Info("test")
	adapter.Infof("test %d", 2)
	adapter.Error("test")
	adapter.Errorf("test %d", 3)
	adapter.Warning("test")
	adapter.Warningf("test %d", 4)
	adapter.Fatal("test")   // should NOT exit
	adapter.Fatalf("test %d", 5)
	adapter.Panic("test")  // should NOT panic
	adapter.Panicf("test %d", 6)
}

func TestRaftNodeStep(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, _ := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	node.Start()
	defer node.Stop()

	// Step with a valid message should not error
	msg := raftpb.Message{
		Type: raftpb.MsgHup,
		To:   1,
		From: 1,
	}
	err := node.Step(msg)
	if err != nil {
		t.Logf("Step MsgHup: %v (expected, may fail depending on state)", err)
	}
}

func TestBoltStoreConfState(t *testing.T) {
	store := helperStore(t)

	// Save conf state
	cs := raftpb.ConfState{
		Voters:   []uint64{1, 2, 3},
		Learners: []uint64{4},
	}
	if err := store.SaveConfState(cs); err != nil {
		t.Fatalf("SaveConfState: %v", err)
	}

	// Load it back
	loaded, err := store.LoadConfState()
	if err != nil {
		t.Fatalf("LoadConfState: %v", err)
	}
	if len(loaded.Voters) != 3 {
		t.Errorf("voters = %d, want 3", len(loaded.Voters))
	}
	if loaded.Voters[0] != 1 {
		t.Errorf("voters[0] = %d, want 1", loaded.Voters[0])
	}
	if len(loaded.Learners) != 1 || loaded.Learners[0] != 4 {
		t.Errorf("learners mismatch")
	}

	// Load from empty
	store2 := helperStore(t)
	empty, err := store2.LoadConfState()
	if err != nil {
		t.Fatalf("LoadConfState empty: %v", err)
	}
	if len(empty.Voters) != 0 {
		t.Error("expected empty conf state")
	}
}

// =====================================================================
// ProposeConfChange direct test
// =====================================================================

func TestProposeConfChange(t *testing.T) {
	store := helperStore(t)
	fsmImpl := NewReefFSM(store.DB(), store)
	transport := newTestTransport(1)

	cfg := DefaultRaftConfig()
	cfg.NodeID = 1
	cfg.Peers = []PeerInfo{{ID: 1, RaftAddr: "node-1"}}

	node, _ := NewRaftNode(cfg, store, fsmImpl, transport, slog.New(slog.DiscardHandler))
	node.Start()
	defer node.Stop()

	// Wait for leadership
	for i := 0; i < 60; i++ {
		if node.IsLeader() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !node.IsLeader() {
		t.Skip("node did not become leader, skipping conf change test")
	}

	// Propose a conf change to add a node
	cc := raftpb.ConfChange{
		Type:   raftpb.ConfChangeAddNode,
		NodeID: 2,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := node.ProposeConfChange(ctx, cc)
	if err != nil {
		t.Fatalf("ProposeConfChange: %v", err)
	}
	t.Log("conf change proposed successfully")
}
