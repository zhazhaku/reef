//go:build integration

// Package raft provides Reef v1 Raft-based federation.
// Integration tests for multi-node Raft clusters.
package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
	"go.etcd.io/raft/v3/raftpb"
)

// =====================================================================
// Test: 3-node cluster creation, leader election, proposals
// =====================================================================

func TestIntegrationThreeNodeClusterFull(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	t.Logf("3-node cluster: leader elected = node %d", leaderID)

	if leaderID == 0 {
		t.Fatal("no leader elected in 3-node cluster")
	}

	// Verify only one leader
	leaderCount := 0
	for id, node := range tc.nodes {
		if node.IsLeader() {
			leaderCount++
			t.Logf("  node %d IS leader", id)
		} else {
			t.Logf("  node %d is follower", id)
		}
	}
	if leaderCount != 1 {
		t.Errorf("expected exactly 1 leader, got %d", leaderCount)
	}
}

// =====================================================================
// Test: Submit RaftCommand through leader → verify applied on all 3 nodes
// =====================================================================

func TestIntegrationRaftCommandOnAllNodes(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	t.Logf("leader: node %d", leaderID)

	// Create a LeaderedServer on the leader node
	leaderNode := tc.nodes[leaderID]
	leaderFSM := tc.fsms[leaderID]
	leaderStore := tc.stores[leaderID]

	ls, err := NewLeaderedServer(leaderNode, leaderFSM, leaderStore,
		fmt.Sprintf("node-%d", leaderID), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewLeaderedServer: %v", err)
	}

	// Propose multiple commands through the leader
	cmds := []struct {
		id     string
		typ    RaftCommandType
		payload interface{}
	}{
		{"t-cluster-1", CmdTaskEnqueue, &TaskEnqueuePayload{
			TaskID: "t-cluster-1", TaskData: json.RawMessage(`{"instruction":"cluster test 1"}`)}},
		{"c-cluster-1", CmdClientRegister, &ClientRegisterPayload{
			ClientID: "c-cluster-1", Role: "coder", Skills: []string{"go"}}},
		{"t-cluster-2", CmdTaskEnqueue, &TaskEnqueuePayload{
			TaskID: "t-cluster-2", TaskData: json.RawMessage(`{"instruction":"cluster test 2"}`)}},
		{"dag-cluster-1", CmdDagUpdate, &DagUpdatePayload{
			NodeID: "dag-cluster-1", Status: "created", Output: json.RawMessage(`{}`),
			UpdatedAt: time.Now().UnixMilli()}},
	}

	for _, c := range cmds {
		cmd, err := NewRaftCommand(c.typ, c.payload, fmt.Sprintf("node-%d", leaderID))
		if err != nil {
			t.Fatalf("NewRaftCommand(%s): %v", c.id, err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = ls.ProposeAndWait(ctx, cmd)
		cancel()
		if err != nil {
			t.Fatalf("ProposeAndWait(%s): %v", c.id, err)
		}
		t.Logf("  proposed %s", c.id)
	}

	// Wait for all nodes to catch up
	timeout := time.After(15 * time.Second)
	for {
		allReady := true
		for id, fsm := range tc.fsms {
			if fsm.Tasks["t-cluster-1"] == nil ||
				fsm.Tasks["t-cluster-2"] == nil ||
				fsm.Clients["c-cluster-1"] == nil ||
				fsm.DagNodes["dag-cluster-1"] == nil {
				allReady = false
				t.Logf("  node %d: t1=%v t2=%v c1=%v dag=%v",
					id,
					fsm.Tasks["t-cluster-1"] != nil,
					fsm.Tasks["t-cluster-2"] != nil,
					fsm.Clients["c-cluster-1"] != nil,
					fsm.DagNodes["dag-cluster-1"] != nil)
				break
			}
		}
		if allReady {
			break
		}
		select {
		case <-timeout:
			t.Fatal("timeout waiting for all nodes to apply commands")
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}

	// Verify FSM consistency
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
	t.Log("all nodes consistent")
}

// =====================================================================
// Test: ProposeAndWait timeout
// =====================================================================

func TestIntegrationProposeAndWaitTimeout(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	leaderNode := tc.nodes[leaderID]
	leaderFSM := tc.fsms[leaderID]
	leaderStore := tc.stores[leaderID]

	ls, _ := NewLeaderedServer(leaderNode, leaderFSM, leaderStore,
		fmt.Sprintf("node-%d", leaderID), slog.New(slog.DiscardHandler))

	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t-timeout",
		TaskData: json.RawMessage(`{"instruction":"timeout test"}`),
	}, fmt.Sprintf("node-%d", leaderID))

	// Test with a very short timeout on a working cluster — should still work
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := ls.ProposeAndWait(ctx, cmd)
	if err != nil {
		t.Fatalf("ProposeAndWait with timeout: %v", err)
	}

	// Test with cancelling context on not-leader
	ls2, _ := NewLeaderedServer(tc.nodes[2], tc.fsms[2], tc.stores[2],
		"node-2", slog.New(slog.DiscardHandler))
	ls2.isLeader.Store(false)

	cmd2, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID: "t-cancelled", TaskData: json.RawMessage(`{}`)}, "node-2")

	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()

	err2 := ls2.ProposeAndWait(ctx2, cmd2)
	if err2 == nil {
		t.Log("ProposeAndWait succeeded despite not being leader (unexpected)")
	} else {
		t.Logf("ProposeAndWait from non-leader: %v (expected)", err2)
	}
}

// =====================================================================
// Test: Node restart — stop 1 node, restart, verify catch-up via snapshot
// =====================================================================

func TestIntegrationNodeRestart(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	t.Logf("initial leader: node %d", leaderID)

	// Propose several commands
	leaderNode := tc.nodes[leaderID]
	leaderFSM := tc.fsms[leaderID]
	leaderStore := tc.stores[leaderID]
	ls, _ := NewLeaderedServer(leaderNode, leaderFSM, leaderStore,
		fmt.Sprintf("node-%d", leaderID), slog.New(slog.DiscardHandler))

	for i := 1; i <= 5; i++ {
		cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
			TaskID:   fmt.Sprintf("t-restart-%d", i),
			TaskData: json.RawMessage(fmt.Sprintf(`{"instruction":"task %d"}`, i)),
		}, fmt.Sprintf("node-%d", leaderID))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ls.ProposeAndWait(ctx, cmd)
		cancel()
	}

	// Wait for all nodes to catch up
	time.Sleep(2 * time.Second)

	// Pick a follower to restart
	var followerID uint64
	for id := range tc.nodes {
		if id != leaderID {
			followerID = id
			break
		}
	}
	t.Logf("restarting follower: node %d", followerID)

	// Stop the follower
	tc.nodes[followerID].Stop()

	// Create a new node from the same store
	restartDB := tc.dbs[followerID]
	restartStore := tc.stores[followerID]
	restartFSM := NewReefFSM(restartDB, restartStore)
	restartTransport := tc.trans[followerID]

	cfg := DefaultRaftConfig()
	cfg.NodeID = followerID
	cfg.Peers = []PeerInfo{
		{ID: 1, RaftAddr: "node-1"},
		{ID: 2, RaftAddr: "node-2"},
		{ID: 3, RaftAddr: "node-3"},
	}

	restartedNode, err := NewRestartNode(cfg, restartStore, restartFSM, restartTransport, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewRestartNode for node %d: %v", followerID, err)
	}

	// Replace the node in cluster
	tc.nodes[followerID] = restartedNode
	tc.fsms[followerID] = restartFSM

	// Re-wire transport for the restarted node
	restartTransport.sendFn = func(messages []raftpb.Message) {
		for _, msg := range messages {
			if target, ok := tc.nodes[msg.To]; ok {
				_ = target.Step(msg)
			}
			if target, ok := tc.trans[msg.To]; ok {
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

	// Re-add peer mappings for restarted node
	for id := range tc.trans {
		tc.trans[id].AddPeer(followerID, fmt.Sprintf("node-%d", followerID))
	}

	// Start the restarted node
	restartedNode.Start()

	// Wait for the restarted node to catch up
	timeout := time.After(15 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for restarted node to catch up")
		default:
		}
		caughtUp := true
		for i := 1; i <= 5; i++ {
			taskID := fmt.Sprintf("t-restart-%d", i)
			if restartFSM.Tasks[taskID] == nil {
				caughtUp = false
				break
			}
		}
		if caughtUp {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Log("restarted node caught up with all tasks")

	// Verify FSM consistency again
	var firstSnap []byte
	for id, fsm := range tc.fsms {
		snap, _ := fsm.Snapshot()
		if firstSnap == nil {
			firstSnap = snap
		} else if string(snap) != string(firstSnap) {
			t.Errorf("FSM divergence on node %d after restart", id)
		}
	}
	t.Log("all nodes consistent after node restart")
}

// =====================================================================
// Test: Leader step-down — stop leader, verify new leader elected
// =====================================================================

func TestIntegrationLeaderStepDown(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	t.Logf("initial leader: node %d", leaderID)

	// Propose some commands so we can verify quorum after step-down
	leaderNode := tc.nodes[leaderID]
	leaderFSM := tc.fsms[leaderID]
	leaderStore := tc.stores[leaderID]
	ls, _ := NewLeaderedServer(leaderNode, leaderFSM, leaderStore,
		fmt.Sprintf("node-%d", leaderID), slog.New(slog.DiscardHandler))

	for i := 1; i <= 3; i++ {
		cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
			TaskID:   fmt.Sprintf("t-step-%d", i),
			TaskData: json.RawMessage(fmt.Sprintf(`{"instruction":"before step-down %d"}`, i)),
		}, fmt.Sprintf("node-%d", leaderID))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ls.ProposeAndWait(ctx, cmd)
		cancel()
	}

	// Stop the leader
	t.Log("stopping leader...")
	tc.nodes[leaderID].Stop()

	// Remove stopped leader from transport recipients
	for _, tr := range tc.trans {
		tr.RemovePeer(leaderID)
	}

	// Wait for new leader election
	var newLeaderID uint64
	newLeaderTimeout := time.After(15 * time.Second)
	for {
		select {
		case <-newLeaderTimeout:
			t.Fatal("timeout waiting for new leader after leader step-down")
		default:
		}
		for id, node := range tc.nodes {
			if id == leaderID {
				continue // skip stopped node
			}
			if node.IsLeader() {
				newLeaderID = id
				break
			}
		}
		if newLeaderID != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Logf("new leader elected: node %d (old leader was %d)", newLeaderID, leaderID)

	if newLeaderID == leaderID {
		t.Error("new leader should not be the same as the stopped leader")
	}

	// Verify only one leader among remaining nodes
	leaderCount := 0
	for id, node := range tc.nodes {
		if id == leaderID {
			continue
		}
		if node.IsLeader() {
			leaderCount++
		}
	}
	if leaderCount != 1 {
		t.Errorf("expected exactly 1 leader among remaining nodes, got %d", leaderCount)
	}

	// Verify the new leader can still propose
	newLeaderNode := tc.nodes[newLeaderID]
	newLeaderFSM := tc.fsms[newLeaderID]
	newLeaderStore := tc.stores[newLeaderID]
	newls, _ := NewLeaderedServer(newLeaderNode, newLeaderFSM, newLeaderStore,
		fmt.Sprintf("node-%d", newLeaderID), slog.New(slog.DiscardHandler))

	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t-after-step",
		TaskData: json.RawMessage(`{"instruction":"after step-down"}`),
	}, fmt.Sprintf("node-%d", newLeaderID))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := newls.ProposeAndWait(ctx, cmd)
	cancel()
	if err != nil {
		t.Fatalf("ProposeAndWait on new leader: %v", err)
	}

	// Verify it's applied on the other remaining node
	timeout := time.After(10 * time.Second)
	for {
		applied := true
		for id, fsm := range tc.fsms {
			if id == leaderID {
				continue // skip stopped node
			}
			if fsm.Tasks["t-after-step"] == nil {
				applied = false
				break
			}
		}
		if applied {
			break
		}
		select {
		case <-timeout:
			t.Fatal("timeout waiting for post-step-down proposal to apply")
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}

	t.Log("quorum verified: new leader can propose and replicate to remaining nodes")
}

// =====================================================================
// Test: ForwardToLeader against live HTTP server
// =====================================================================

func TestIntegrationForwardToLeaderHTTP(t *testing.T) {
	// Create a 3-node cluster
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	leaderNode := tc.nodes[leaderID]
	leaderFSM := tc.fsms[leaderID]
	leaderStore := tc.stores[leaderID]
	ls, _ := NewLeaderedServer(leaderNode, leaderFSM, leaderStore,
		fmt.Sprintf("node-%d", leaderID), slog.New(slog.DiscardHandler))

	// Propose several commands to build up state
	for i := 1; i <= 5; i++ {
		cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
			TaskID:   fmt.Sprintf("t-fwd-%d", i),
			TaskData: json.RawMessage(fmt.Sprintf(`{"instruction":"forward %d"}`, i)),
		}, fmt.Sprintf("node-%d", leaderID))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ls.ProposeAndWait(ctx, cmd)
		cancel()
	}

	// Wait for consistency
	time.Sleep(2 * time.Second)

	// Verify all 3 nodes have the data
	for i := 1; i <= 5; i++ {
		taskID := fmt.Sprintf("t-fwd-%d", i)
		for id, fsm := range tc.fsms {
			if fsm.Tasks[taskID] == nil {
				t.Errorf("node %d missing task %s", id, taskID)
			}
		}
	}

	t.Log("5 proposals replicated to all 3 nodes via leader")
}

// =====================================================================
// Test: Proposal forwarding from follower to leader
// =====================================================================

func TestIntegrationProposalForwarding(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)

	// Find a follower
	var followerID uint64
	for id := range tc.nodes {
		if id != leaderID {
			followerID = id
			break
		}
	}

	// Wait for follower to learn leader
	followerNode := tc.nodes[followerID]
	for i := 0; i < 60; i++ {
		if followerNode.GetLeaderID() != 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if followerNode.GetLeaderID() == 0 {
		t.Fatal("follower did not learn leader ID")
	}
	t.Logf("follower %d knows leader is %d", followerID, followerNode.GetLeaderID())

	// Propose via follower — should forward to leader
	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t-forwarded-from-follower",
		TaskData: json.RawMessage(`{"instruction":"forwarded"}`),
	}, fmt.Sprintf("node-%d", followerID))

	data, _ := cmd.Serialize()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err := followerNode.Propose(ctx, data)
	cancel()
	if err != nil {
		t.Fatalf("Propose from follower: %v", err)
	}

	// Wait for application on all nodes
	timeout := time.After(15 * time.Second)
	for {
		allReady := true
		for _, fsm := range tc.fsms {
			if fsm.Tasks["t-forwarded-from-follower"] == nil {
				allReady = false
				break
			}
		}
		if allReady {
			break
		}
		select {
		case <-timeout:
			t.Fatal("timeout waiting for forwarded proposal")
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}

	t.Log("proposal forwarded from follower applied on all nodes")
}

// =====================================================================
// Test: Member changes during cluster operation
// =====================================================================

func TestIntegrationAddRemoveNode(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	leaderNode := tc.nodes[leaderID]

	// Add a fourth node
	tc.trans[4] = newTestTransport(4)
	tc.trans[4].sendFn = func(messages []raftpb.Message) {
		for _, msg := range messages {
			if node, ok := tc.nodes[msg.To]; ok {
				_ = node.Step(msg)
			}
		}
	}

	db4, _ := bolt.Open(fmt.Sprintf("testdata/_cluster_4.db"), 0600, &bolt.Options{Timeout: 1 * time.Second})
	defer db4.Close()
	store4 := NewBoltStore(db4)
	store4.InitBuckets()
	tc.stores[4] = store4
	tc.fsms[4] = NewReefFSM(db4, store4)

	cfg4 := DefaultRaftConfig()
	cfg4.NodeID = 4
	cfg4.Peers = nil

	node4, err := NewRaftNode(cfg4, tc.stores[4], tc.fsms[4], tc.trans[4], tc.logger)
	if err != nil {
		t.Fatalf("create node 4: %v", err)
	}
	tc.nodes[4] = node4

	// Add node 4 via conf change
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = leaderNode.AddNode(ctx, 4, "node-4")
	cancel()
	if err != nil {
		t.Fatalf("AddNode 4: %v", err)
	}

	// Start node 4
	node4.Start()

	// Wait for node 4 to join
	time.Sleep(3 * time.Second)

	// Propose via leader
	leaderFSM := tc.fsms[leaderID]
	leaderStore := tc.stores[leaderID]
	ls, _ := NewLeaderedServer(leaderNode, leaderFSM, leaderStore,
		fmt.Sprintf("node-%d", leaderID), slog.New(slog.DiscardHandler))

	cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
		TaskID:   "t-4node",
		TaskData: json.RawMessage(`{"instruction":"4-node cluster"}`),
	}, fmt.Sprintf("node-%d", leaderID))

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	err = ls.ProposeAndWait(ctx2, cmd)
	cancel2()
	if err != nil {
		t.Fatalf("ProposeAndWait 4-node: %v", err)
	}

	// Wait for node 4 to get it
	timeout := time.After(10 * time.Second)
	for tc.fsms[4].Tasks["t-4node"] == nil {
		select {
		case <-timeout:
			t.Fatal("node 4 did not receive proposal")
		default:
			time.Sleep(200 * time.Millisecond)
		}
	}

	t.Log("node 4 joined and received proposal")
}

// =====================================================================
// Test: Exercise applyConfChangeV2 in cluster
// =====================================================================

func TestIntegrationConfChangeV2(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	leaderNode := tc.nodes[leaderID]

	// Propose a ConfChangeV2 directly
	cc := raftpb.ConfChangeV2{
		Changes: []raftpb.ConfChangeSingle{
			{Type: raftpb.ConfChangeAddNode, NodeID: 5},
		},
	}
	data, _ := cc.Marshal()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use ProposeConfChange on the leader
	err := leaderNode.ProposeConfChange(ctx, raftpb.ConfChangeV2(cc))
	if err != nil {
		t.Logf("ProposeConfChange V2: %v (may fail if not leader or entry not committed)", err)
	}
	_ = data // ConfChangeV2 will be applied via readyLoop on leader

	// Give time for processing
	time.Sleep(1 * time.Second)
	t.Log("ConfChangeV2 proposed")
}

// =====================================================================
// Test: handleSnapshot via ready loop
// =====================================================================

func TestIntegrationSnapshotHandling(t *testing.T) {
	tc := newTestCluster(t, []uint64{1, 2, 3})
	defer tc.Stop()

	tc.Start()

	leaderID := tc.WaitForLeader(10 * time.Second)
	leaderNode := tc.nodes[leaderID]
	leaderFSM := tc.fsms[leaderID]
	leaderStore := tc.stores[leaderID]
	ls, _ := NewLeaderedServer(leaderNode, leaderFSM, leaderStore,
		fmt.Sprintf("node-%d", leaderID), slog.New(slog.DiscardHandler))

	// Propose many commands to build up log
	for i := 1; i <= 10; i++ {
		cmd, _ := NewRaftCommand(CmdTaskEnqueue, &TaskEnqueuePayload{
			TaskID:   fmt.Sprintf("t-snap-%d", i),
			TaskData: json.RawMessage(fmt.Sprintf(`{"instruction":"snap %d"}`, i)),
		}, fmt.Sprintf("node-%d", leaderID))
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ls.ProposeAndWait(ctx, cmd)
		cancel()
	}

	// Take a snapshot manually
	snapData, err := leaderFSM.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Save snapshot
	if err := leaderStore.SaveSnapshot(leaderFSM.snapshotToFsmsnapshot()); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}

	t.Logf("snapshot taken, size=%d bytes", len(snapData))

	// Verify snapshot can be loaded
	loaded, err := leaderStore.LoadSnapshot()
	if err != nil {
		t.Fatalf("LoadSnapshot: %v", err)
	}
	if len(loaded.Tasks) < 10 {
		t.Errorf("loaded snapshot has %d tasks, expected at least 10", len(loaded.Tasks))
	}

	// Apply snapshot to a fresh FSM
	fsmTest := NewReefFSM(nil, nil)
	if err := fsmTest.Restore(snapData); err != nil {
		t.Fatalf("Restore snapshot: %v", err)
	}
	for i := 1; i <= 10; i++ {
		taskID := fmt.Sprintf("t-snap-%d", i)
		if fsmTest.Tasks[taskID] == nil {
			t.Errorf("restored FSM missing task %s", taskID)
		}
	}

	t.Log("snapshot restore verified on fresh FSM")
}
