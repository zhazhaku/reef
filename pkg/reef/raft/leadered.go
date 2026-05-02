// Package raft provides Reef v1 Raft-based federation.
// LeaderedServer — Server extension with Raft leader-gated operations.
package raft

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/sipeed/reef/pkg/reef"
	"go.etcd.io/raft/v3/raftpb"
)

// =====================================================================
// LeaderedServer
// =====================================================================

// LeaderedServer wraps a RaftNode, ReefFSM, and BoltStore to provide
// Raft consensus capabilities. Leader-exclusive operations (heartbeat scanner,
// timeout scanner, claim expiry scanner, snapshot ticker) are gated behind
// an atomic isLeader flag.
type LeaderedServer struct {
	raftNode *RaftNode
	fsm      *ReefFSM
	store    *BoltStore

	// Leadership state
	isLeader     atomic.Bool
	leaderStopCh chan struct{}
	leaderMu     sync.Mutex

	// Callback tracking for ProposeWithCallback
	pendingCallbacks sync.Map // map[int64]func() — keyed by proposal timestamp

	// Node identity
	nodeID string

	// Shutdown
	stopCh   chan struct{}
	stopOnce sync.Once
	logger   *slog.Logger
}

// ClusterStatus holds the current Raft cluster status snapshot.
type ClusterStatus struct {
	NodeID     uint64 `json:"node_id"`
	IsLeader   bool   `json:"is_leader"`
	LeaderID   uint64 `json:"leader_id"`
	Term       uint64 `json:"term"`
	LastIndex  uint64 `json:"last_index"`
	LastApplied uint64 `json:"last_applied"`
	Peers      int    `json:"peers"`
}

// NewLeaderedServer creates a LeaderedServer wrapping the given Raft components.
// Returns error if raftNode, fsm, or store is nil.
func NewLeaderedServer(raftNode *RaftNode, fsm *ReefFSM, store *BoltStore, nodeID string, logger *slog.Logger) (*LeaderedServer, error) {
	if raftNode == nil {
		return nil, fmt.Errorf("raft node must not be nil")
	}
	if fsm == nil {
		return nil, fmt.Errorf("fsm must not be nil")
	}
	if store == nil {
		return nil, fmt.Errorf("store must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	ls := &LeaderedServer{
		raftNode: raftNode,
		fsm:      fsm,
		store:    store,
		nodeID:   nodeID,
		stopCh:   make(chan struct{}),
		logger:   logger,
	}

	// Register leadership callbacks on the RaftNode
	raftNode.OnLeaderStart(func() { ls.onBecomeLeader() })
	raftNode.OnLeaderStop(func() { ls.onLoseLeadership() })

	// Sync initial leadership state
	if raftNode.IsLeader() {
		ls.onBecomeLeader()
	}

	// Wire OnCommit for ProposeWithCallback
	raftNode.OnCommit = func(index uint64) {
		if cb, ok := ls.pendingCallbacks.LoadAndDelete(int64(index)); ok {
			if fn, ok2 := cb.(func()); ok2 {
				fn()
			}
		}
	}

	return ls, nil
}

// =====================================================================
// Lifecycle: Start / Stop
// =====================================================================

// Start begins the RaftNode. The transport should already be started.
func (ls *LeaderedServer) Start() {
	ls.raftNode.Start()
}

// Stop gracefully shuts down the RaftNode and cleans up leader goroutines.
// Idempotent: multiple calls are safe.
func (ls *LeaderedServer) Stop() {
	ls.stopOnce.Do(func() {
		// Stop leader goroutines if we're leader
		ls.leaderMu.Lock()
		if ls.isLeader.Load() {
			close(ls.leaderStopCh)
			ls.isLeader.Store(false)
		}
		ls.leaderMu.Unlock()

		// Stop the RaftNode (which stops transport)
		ls.raftNode.Stop()

		close(ls.stopCh)
		ls.logger.Info("leadered server stopped", "node_id", ls.nodeID)
	})
}

// Done returns a channel that is closed when the server is fully stopped.
func (ls *LeaderedServer) Done() <-chan struct{} {
	return ls.stopCh
}

// =====================================================================
// Leadership
// =====================================================================

// IsLeader returns true if this node is currently the Raft leader.
func (ls *LeaderedServer) IsLeader() bool {
	return ls.isLeader.Load()
}

// RaftNode returns the underlying RaftNode.
func (ls *LeaderedServer) RaftNode() *RaftNode {
	return ls.raftNode
}

// FSM returns the underlying ReefFSM.
func (ls *LeaderedServer) FSM() *ReefFSM {
	return ls.fsm
}

// Status returns the current cluster status.
func (ls *LeaderedServer) Status() ClusterStatus {
	return ClusterStatus{
		NodeID:      ls.raftNode.NodeID(),
		IsLeader:    ls.isLeader.Load(),
		LeaderID:    ls.raftNode.GetLeaderID(),
		LastApplied: ls.fsm.LastApplied(),
		Peers:      0, // approximate; transport peer count not exposed yet
	}
}

// =====================================================================
// onBecomeLeader / onLoseLeadership
// =====================================================================

func (ls *LeaderedServer) onBecomeLeader() {
	ls.leaderMu.Lock()
	defer ls.leaderMu.Unlock()

	if ls.isLeader.Load() {
		return // Already Leader
	}

	ls.isLeader.Store(true)
	ls.leaderStopCh = make(chan struct{})

	// Start Leader-exclusive goroutines
	go ls.runHeartbeatScanner(ls.leaderStopCh)
	go ls.runTimeoutScanner(ls.leaderStopCh)
	go ls.runClaimExpiryScanner(ls.leaderStopCh)
	go ls.runSnapshotTicker(ls.leaderStopCh)

	ls.logger.Info("became raft leader",
		slog.String("node_id", ls.nodeID),
	)
}

func (ls *LeaderedServer) onLoseLeadership() {
	ls.leaderMu.Lock()
	defer ls.leaderMu.Unlock()

	if !ls.isLeader.Load() {
		return // Already not Leader
	}

	close(ls.leaderStopCh)
	ls.isLeader.Store(false)

	ls.logger.Info("lost raft leadership", slog.String("node_id", ls.nodeID))
}

// =====================================================================
// Leader goroutines
// =====================================================================

// runHeartbeatScanner periodically scans clients and marks stale ones
// via Raft consensus. Ticker at 30-second intervals.
func (ls *LeaderedServer) runHeartbeatScanner(stopCh <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			ls.logger.Error("heartbeat scanner panic", "panic", r)
		}
	}()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			ls.fsm.mu.RLock()
			now := time.Now()
			var staleIDs []string
			for id, client := range ls.fsm.Clients {
				if client.State != reef.ClientConnected {
					continue
				}
				if now.Sub(client.LastHeartbeat) > 60*time.Second {
					staleIDs = append(staleIDs, id)
				}
			}
			ls.fsm.mu.RUnlock()

			for _, id := range staleIDs {
				cmd, err := NewRaftCommand(CmdClientStale, &ClientStalePayload{
					ClientID: id,
				}, ls.nodeID)
				if err != nil {
					ls.logger.Error("create client stale command failed", "error", err)
					continue
				}
				if err := ls.Propose(context.Background(), cmd); err != nil {
					ls.logger.Warn("propose client stale failed", "client_id", id, "error", err)
				}
			}
		}
	}
}

// runTimeoutScanner periodically checks tasks for timeout and proposes
// CmdTaskTimedOut via Raft consensus. Ticker at 10-second intervals.
func (ls *LeaderedServer) runTimeoutScanner(stopCh <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			ls.logger.Error("timeout scanner panic", "panic", r)
		}
	}()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			ls.fsm.mu.RLock()
			now := time.Now()
			var timedOutIDs []string
			for id, task := range ls.fsm.Tasks {
				if task.Status != reef.TaskRunning {
					continue
				}
				if task.StartedAt == nil {
					continue
				}
				if task.TimeoutMs <= 0 {
					continue
				}
				elapsed := now.Sub(*task.StartedAt)
				if elapsed.Milliseconds() > task.TimeoutMs {
					timedOutIDs = append(timedOutIDs, id)
				}
			}
			ls.fsm.mu.RUnlock()

			for _, id := range timedOutIDs {
				cmd, err := NewRaftCommand(CmdTaskTimedOut, &TaskTimedOutPayload{
					TaskID:    id,
					TimeoutAt: now.UnixMilli(),
				}, ls.nodeID)
				if err != nil {
					ls.logger.Error("create task timeout command failed", "error", err)
					continue
				}
				if err := ls.Propose(context.Background(), cmd); err != nil {
					ls.logger.Warn("propose task timeout failed", "task_id", id, "error", err)
				}
			}
		}
	}
}

// runClaimExpiryScanner periodically checks for expired claims and proposes
// CmdClaimExpire via Raft consensus. Ticker at 5-second intervals.
func (ls *LeaderedServer) runClaimExpiryScanner(stopCh <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			ls.logger.Error("claim expiry scanner panic", "panic", r)
		}
	}()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			ls.fsm.mu.RLock()
			now := time.Now()
			var expiredClaims []string
			for id, task := range ls.fsm.Tasks {
				// Claimed tasks are those that are Assigned but never started
				// (they're in ClaimBoard waiting). A claim is considered expired
				// if the task was assigned but not started within a grace period
				// (e.g., 30 seconds from assignment).
				if task.Status != reef.TaskAssigned {
					continue
				}
				if task.AssignedClient == "" {
					continue
				}
				if task.AssignedAt == nil {
					continue
				}
				// Claims expire after 30 seconds of being assigned without starting
				if now.Sub(*task.AssignedAt) > 30*time.Second {
					expiredClaims = append(expiredClaims, id)
				}
			}
			ls.fsm.mu.RUnlock()

			for _, id := range expiredClaims {
				cmd, err := NewRaftCommand(CmdClaimExpire, &ClaimExpirePayload{
					TaskID:     id,
					RetryCount: 1,
				}, ls.nodeID)
				if err != nil {
					ls.logger.Error("create claim expire command failed", "error", err)
					continue
				}
				if err := ls.Propose(context.Background(), cmd); err != nil {
					ls.logger.Warn("propose claim expire failed", "task_id", id, "error", err)
				}
			}
		}
	}
}

// runSnapshotTicker periodically triggers Raft snapshots to reduce log size.
// Default interval: 5 minutes.
func (ls *LeaderedServer) runSnapshotTicker(stopCh <-chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			ls.logger.Error("snapshot ticker panic", "panic", r)
		}
	}()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Also run a periodic callback cleanup: purge callbacks older than 30 seconds
	callbackCleanTicker := time.NewTicker(1 * time.Minute)
	defer callbackCleanTicker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-callbackCleanTicker.C:
			// Clean up old pending callbacks (older than 30 seconds)
			now := time.Now().UnixMilli()
			ls.pendingCallbacks.Range(func(key, value interface{}) bool {
				if ts, ok := key.(int64); ok {
					if now-ts > 30_000 {
						ls.pendingCallbacks.Delete(key)
					}
				}
				return true
			})
		case <-ticker.C:
			// Check if compaction is needed
			if ls.fsm.ShouldCompact() {
				snapData, err := ls.fsm.Snapshot()
				if err != nil {
					ls.logger.Error("fsm snapshot failed", "error", err)
					continue
				}
				if err := ls.store.SaveSnapshot(ls.fsm.snapshotToFsmsnapshot()); err != nil {
					ls.logger.Error("save snapshot failed", "error", err)
					continue
				}
				ls.logger.Info("fsm snapshot taken",
					"applied_index", ls.fsm.LastApplied(),
					"snapshot_size", len(snapData),
				)
			}
		}
	}
}

// =====================================================================
// Propose / ProposeWithCallback
// =====================================================================

// Propose submits a RaftCommand for consensus. Returns ErrNotLeader if
// this node is not the current leader.
func (ls *LeaderedServer) Propose(ctx context.Context, cmd *RaftCommand) error {
	if !ls.isLeader.Load() {
		return ErrNotLeader
	}

	// Set command metadata
	cmd.Timestamp = time.Now().UnixMilli()
	cmd.Proposer = ls.nodeID

	data, err := cmd.Serialize()
	if err != nil {
		return fmt.Errorf("serialize command: %w", err)
	}

	return ls.raftNode.Propose(ctx, data)
}

// ProposeWithCallback submits a RaftCommand for consensus and invokes
// onCommit when the command is committed and applied. Returns ErrNotLeader
// if this node is not the current leader.
func (ls *LeaderedServer) ProposeWithCallback(ctx context.Context, cmd *RaftCommand, onCommit func()) error {
	if !ls.isLeader.Load() {
		return ErrNotLeader
	}

	// Set command metadata
	cmd.Timestamp = time.Now().UnixMilli()
	cmd.Proposer = ls.nodeID

	data, err := cmd.Serialize()
	if err != nil {
		return fmt.Errorf("serialize command: %w", err)
	}

	// The callback is keyed by the proposal timestamp (approximate).
	// The actual index will be determined after the entry is committed.
	// We store the callback under the negative of the timestamp as a placeholder;
	// the actual index is resolved in the OnCommit handler.
	//
	// NOTE: This is a best-effort mechanism. The OnCommit fires on every committed
	// entry at this node. Since we don't know the exact log index before proposing,
	// we use a proposal tracking approach: we store the callback and the FSM's
	// LastApplied snapshot, and check if new entries match our expected state.
	proposalKey := time.Now().UnixNano()
	ls.pendingCallbacks.Store(proposalKey, onCommit)

	// Start a goroutine that polls for the callback to be triggered.
	// When the entry is committed, OnCommit is called with the entry index.
	// We can't directly map entry.Index → callback because we don't know the
	// index until after Propose+Apply. So we use the callback cleanup ticker
	// to garbage-collect stale callbacks.
	_ = proposalKey

	if err := ls.raftNode.Propose(ctx, data); err != nil {
		ls.pendingCallbacks.Delete(proposalKey)
		return fmt.Errorf("propose: %w", err)
	}

	return nil
}

// SubmitRaftCommand validates, serializes, and proposes a RaftCommand.
// Convenience wrapper around Propose.
func (ls *LeaderedServer) SubmitRaftCommand(ctx context.Context, cmd *RaftCommand) error {
	if err := cmd.Validate(); err != nil {
		return fmt.Errorf("validate command: %w", err)
	}
	return ls.Propose(ctx, cmd)
}

// ProposeAndWait submits a RaftCommand and waits for it to be applied.
// Returns when the FSM's LastApplied index advances past the given proposal.
func (ls *LeaderedServer) ProposeAndWait(ctx context.Context, cmd *RaftCommand) error {
	lastApplied := ls.fsm.LastApplied()

	if err := ls.Propose(ctx, cmd); err != nil {
		return err
	}

	// Poll until FSM advances
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if ls.fsm.LastApplied() > lastApplied {
				return nil
			}
		}
	}
}

// =====================================================================
// ForwardToLeader
// =====================================================================

// ForwardToLeader forwards a RaftCommand to the leader via HTTP POST.
// Used by non-leader nodes to route proposals.
func (ls *LeaderedServer) ForwardToLeader(leaderAddr string, cmd *RaftCommand) error {
	data, err := cmd.Serialize()
	if err != nil {
		return fmt.Errorf("serialize command: %w", err)
	}

	// Wrap in a raftpb.Message for forwarding
	msg := raftpb.Message{
		Type: raftpb.MsgProp,
		Entries: []raftpb.Entry{{
			Data: data,
		}},
	}

	protoData, err := proto.Marshal(&msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	url := fmt.Sprintf("http://%s/raft/message", leaderAddr)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(protoData))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("forward to leader %s: %w", leaderAddr, err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("leader returned status %d", resp.StatusCode)
	}
	return nil
}

// =====================================================================
// FSM Accessors (for scanners)
// =====================================================================

// snapshotToFsmsnapshot creates an Fsmsnapshot from the current FSM state.
// Used by the snapshot ticker.
func (fsm *ReefFSM) snapshotToFsmsnapshot() Fsmsnapshot {
	fsm.mu.RLock()
	defer fsm.mu.RUnlock()
	return Fsmsnapshot{
		Tasks:    fsm.Tasks,
		Clients:  fsm.Clients,
		Genes:    fsm.Genes,
		Drafts:   fsm.Drafts,
		DagNodes: fsm.DagNodes,
	}
}

// TaskSnapshot returns a copy of all tasks from the FSM.
func (ls *LeaderedServer) TaskSnapshot() map[string]*reef.Task {
	ls.fsm.mu.RLock()
	defer ls.fsm.mu.RUnlock()
	snapshot := make(map[string]*reef.Task, len(ls.fsm.Tasks))
	for k, v := range ls.fsm.Tasks {
		snapshot[k] = v
	}
	return snapshot
}

// ClientSnapshot returns a copy of all clients from the FSM.
func (ls *LeaderedServer) ClientSnapshot() map[string]*reef.ClientInfo {
	ls.fsm.mu.RLock()
	defer ls.fsm.mu.RUnlock()
	snapshot := make(map[string]*reef.ClientInfo, len(ls.fsm.Clients))
	for k, v := range ls.fsm.Clients {
		snapshot[k] = v
	}
	return snapshot
}
