// Package raft provides Reef v1 Raft-based federation.
// RaftNode wraps go.etcd.io/raft/v3 with Ready processing, proposal forwarding,
// member changes, and restart support.
package raft

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"go.etcd.io/raft/v3"
	"go.etcd.io/raft/v3/raftpb"
)

// =====================================================================
// Transport interface — stub for testing; real impl in P7-06.
// =====================================================================

// Transport abstracts message delivery between Raft peers.
type Transport interface {
	// Send delivers a batch of raft messages to their destinations.
	Send(messages []raftpb.Message)
	// AddPeer registers a new peer for message delivery.
	AddPeer(id uint64, addr string)
	// RemovePeer unregisters a peer.
	RemovePeer(id uint64)
	// Stop shuts down the transport.
	Stop()
}

// =====================================================================
// RaftConfig
// =====================================================================

// PeerInfo describes a Raft peer.
type PeerInfo struct {
	ID       uint64
	RaftAddr string
}

// RaftConfig holds all parameters needed to create a RaftNode.
type RaftConfig struct {
	// NodeID is this node's Raft ID (1-based, non-zero).
	NodeID uint64
	// Peers is the initial cluster membership (including self).
	Peers []PeerInfo
	// ElectionTimeoutMs is the election timeout in milliseconds.
	ElectionTimeoutMs int
	// HeartbeatIntervalMs is the heartbeat interval in milliseconds.
	HeartbeatIntervalMs int
	// MaxSizePerMsg limits the max byte size of each append message (0 = default 1MB).
	MaxSizePerMsg uint64
	// MaxInflightMsgs limits the max number of in-flight append messages (0 = default 256).
	MaxInflightMsgs int
	// MaxUncommittedEntriesSize limits the aggregate byte size of uncommitted entries.
	MaxUncommittedEntriesSize uint64
	// CheckQuorum enables leader quorum checking.
	CheckQuorum bool
	// PreVote enables the Pre-Vote algorithm.
	PreVote bool
}

// DefaultRaftConfig returns a RaftConfig with sensible defaults.
func DefaultRaftConfig() RaftConfig {
	return RaftConfig{
		ElectionTimeoutMs:         1000,
		HeartbeatIntervalMs:       100,
		MaxSizePerMsg:             1024 * 1024, // 1MB
		MaxInflightMsgs:           256,
		MaxUncommittedEntriesSize: 1 << 30, // 1GB
		CheckQuorum:               true,
		PreVote:                   true,
	}
}

// Validate checks the config for common errors.
func (c *RaftConfig) Validate() error {
	if c.NodeID == 0 {
		return fmt.Errorf("RaftConfig.NodeID must be non-zero")
	}
	if c.ElectionTimeoutMs <= 0 {
		return fmt.Errorf("RaftConfig.ElectionTimeoutMs must be positive, got %d", c.ElectionTimeoutMs)
	}
	if c.HeartbeatIntervalMs <= 0 {
		return fmt.Errorf("RaftConfig.HeartbeatIntervalMs must be positive, got %d", c.HeartbeatIntervalMs)
	}
	if c.ElectionTimeoutMs <= c.HeartbeatIntervalMs {
		return fmt.Errorf("ElectionTimeoutMs (%d) must be > HeartbeatIntervalMs (%d)",
			c.ElectionTimeoutMs, c.HeartbeatIntervalMs)
	}
	return nil
}

// raftPeers builds the []raft.Peer slice from config.
func (c *RaftConfig) raftPeers() []raft.Peer {
	peers := make([]raft.Peer, 0, len(c.Peers))
	for _, p := range c.Peers {
		peers = append(peers, raft.Peer{
			ID:      p.ID,
			Context: []byte(p.RaftAddr),
		})
	}
	return peers
}

// electionTick computes the election tick count from timeouts.
func (c *RaftConfig) electionTick() int {
	tick := c.ElectionTimeoutMs / c.HeartbeatIntervalMs
	if tick < 1 {
		tick = 1
	}
	return tick
}

// =====================================================================
// raftLoggerAdapter — adapts slog.Logger to raft.Logger
// =====================================================================

type raftLoggerAdapter struct {
	logger *slog.Logger
}

func (a *raftLoggerAdapter) Debug(v ...interface{}) {
	a.logger.Debug(fmt.Sprint(v...))
}

func (a *raftLoggerAdapter) Debugf(format string, v ...interface{}) {
	a.logger.Debug(fmt.Sprintf(format, v...))
}

func (a *raftLoggerAdapter) Info(v ...interface{}) {
	a.logger.Info(fmt.Sprint(v...))
}

func (a *raftLoggerAdapter) Infof(format string, v ...interface{}) {
	a.logger.Info(fmt.Sprintf(format, v...))
}

func (a *raftLoggerAdapter) Error(v ...interface{}) {
	a.logger.Error(fmt.Sprint(v...))
}

func (a *raftLoggerAdapter) Errorf(format string, v ...interface{}) {
	a.logger.Error(fmt.Sprintf(format, v...))
}

func (a *raftLoggerAdapter) Warning(v ...interface{}) {
	a.logger.Warn(fmt.Sprint(v...))
}

func (a *raftLoggerAdapter) Warningf(format string, v ...interface{}) {
	a.logger.Warn(fmt.Sprintf(format, v...))
}

// Fatal and Panic are logged at Error level to avoid crashing the process.
func (a *raftLoggerAdapter) Fatal(v ...interface{}) {
	a.logger.Error("RAFT FATAL: " + fmt.Sprint(v...))
}

func (a *raftLoggerAdapter) Fatalf(format string, v ...interface{}) {
	a.logger.Error("RAFT FATAL: " + fmt.Sprintf(format, v...))
}

func (a *raftLoggerAdapter) Panic(v ...interface{}) {
	a.logger.Error("RAFT PANIC: " + fmt.Sprint(v...))
}

func (a *raftLoggerAdapter) Panicf(format string, v ...interface{}) {
	a.logger.Error("RAFT PANIC: " + fmt.Sprintf(format, v...))
}

// =====================================================================
// RaftNode
// =====================================================================

// RaftNode wraps a raft.Node with lifecycle management, Ready processing,
// proposal forwarding, member changes, and leadership tracking.
type RaftNode struct {
	nodeID    uint64
	node      raft.Node
	fsm       *ReefFSM
	store     *BoltStore
	transport Transport
	config    RaftConfig
	logger    *slog.Logger

	// memStorage is the raft-internal MemoryStorage used for log lookups.
	// We keep a reference to update it during Ready processing.
	memStorage *raft.MemoryStorage

	// Lifecycle
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	doneCh  chan struct{} // closed when fully shut down

	// Leadership tracking
	leaderID      atomic.Uint64
	onLeaderStart func() // callback when this node becomes Leader
	onLeaderStop  func() // callback when this node loses leadership

	// Stop
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewRaftNode creates a RaftNode with a fresh raft.Node via raft.StartNode.
// The Ready loop is not started until Start() is called.
func NewRaftNode(config RaftConfig, store *BoltStore, fsm *ReefFSM, transport Transport, logger *slog.Logger) (*RaftNode, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	if store == nil {
		return nil, fmt.Errorf("store must not be nil")
	}
	if fsm == nil {
		return nil, fmt.Errorf("fsm must not be nil")
	}
	if transport == nil {
		return nil, fmt.Errorf("transport must not be nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	memStorage := raft.NewMemoryStorage()

	raftCfg := &raft.Config{
		ID:                        config.NodeID,
		ElectionTick:              config.electionTick(),
		HeartbeatTick:            1,
		Storage:                   memStorage,
		MaxSizePerMsg:             config.MaxSizePerMsg,
		MaxInflightMsgs:           config.MaxInflightMsgs,
		MaxUncommittedEntriesSize: config.MaxUncommittedEntriesSize,
		Logger:                    &raftLoggerAdapter{logger: logger.With("component", "raft")},
		CheckQuorum:               config.CheckQuorum,
		PreVote:                   config.PreVote,
	}

	rn := &RaftNode{
		nodeID:     config.NodeID,
		fsm:        fsm,
		store:      store,
		transport:  transport,
		config:     config,
		logger:     logger,
		memStorage: memStorage,
		doneCh:     make(chan struct{}),
		stopCh:     make(chan struct{}),
	}

	// Start the raft node.
	// If peers is nil/empty (single-node bootstrap), add self as the sole peer.
	peers := config.raftPeers()
	if len(peers) == 0 {
		peers = []raft.Peer{{ID: config.NodeID, Context: []byte("")}}
	}
	rn.node = raft.StartNode(raftCfg, peers)
	rn.logger.Info("raft node started", "peers", len(peers))

	return rn, nil
}

// NewRestartNode creates a RaftNode from previously persisted state in BoltDB.
// Loads HardState, ConfState, and applied index, then replays entries through the FSM.
func NewRestartNode(config RaftConfig, store *BoltStore, fsm *ReefFSM, transport Transport, logger *slog.Logger) (*RaftNode, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	hs, err := store.LoadHardState()
	if err != nil {
		return nil, fmt.Errorf("load hard state: %w", err)
	}

	cs, err := store.LoadConfState()
	if err != nil {
		return nil, fmt.Errorf("load conf state: %w", err)
	}

	// If no previous state, this is a fresh start — delegate to NewRaftNode.
	if raft.IsEmptyHardState(hs) {
		logger.Info("no previous hard state, doing fresh start")
		return NewRaftNode(config, store, fsm, transport, logger)
	}

	_ = cs // conf state loaded; raft will manage membership on restart

	// Replay committed entries into FSM first — this updates fsm.LastApplied().
	if err := replayEntries(store, fsm, 0); err != nil {
		return nil, fmt.Errorf("replay entries: %w", err)
	}

	appliedIndex := fsm.LastApplied()

	memStorage := raft.NewMemoryStorage()

	// Populate MemoryStorage with persisted state for proper restart.
	if err := memStorage.SetHardState(hs); err != nil {
		return nil, fmt.Errorf("set hard state: %w", err)
	}

	// Build a snapshot at the applied index to capture ConfState.
	if appliedIndex > 0 {
		snap := raftpb.Snapshot{
			Metadata: raftpb.SnapshotMetadata{
				Index:     appliedIndex,
				Term:      hs.Term,
				ConfState: cs,
			},
		}
		if err := memStorage.ApplySnapshot(snap); err != nil {
			return nil, fmt.Errorf("apply snapshot: %w", err)
		}
	}

	// Load unapplied entries from BoltDB and append to MemoryStorage.
	remaining, err := store.LoadEntries(appliedIndex+1, ^uint64(0))
	if err != nil {
		return nil, fmt.Errorf("load remaining entries: %w", err)
	}
	if len(remaining) > 0 {
		if err := memStorage.Append(remaining); err != nil {
			return nil, fmt.Errorf("append entries: %w", err)
		}
	}

	raftCfg := &raft.Config{
		ID:                        config.NodeID,
		ElectionTick:              config.electionTick(),
		HeartbeatTick:            1,
		Storage:                   memStorage,
		Applied:                   appliedIndex,
		MaxSizePerMsg:             config.MaxSizePerMsg,
		MaxInflightMsgs:           config.MaxInflightMsgs,
		MaxUncommittedEntriesSize: config.MaxUncommittedEntriesSize,
		Logger:                    &raftLoggerAdapter{logger: logger.With("component", "raft")},
		CheckQuorum:               config.CheckQuorum,
		PreVote:                   config.PreVote,
	}

	rn := &RaftNode{
		nodeID:     config.NodeID,
		fsm:        fsm,
		store:      store,
		transport:  transport,
		config:     config,
		logger:     logger,
		memStorage: memStorage,
		doneCh:     make(chan struct{}),
		stopCh:     make(chan struct{}),
	}

	rn.node = raft.RestartNode(raftCfg)
	rn.logger.Info("raft node restarted from persisted state",
		"term", hs.Term,
		"commit", hs.Commit,
		"applied", appliedIndex,
	)

	return rn, nil
}

// replayEntries replays committed entries from BoltDB that are after
// the last applied index through the FSM.
func replayEntries(store *BoltStore, fsm *ReefFSM, appliedIndex uint64) error {
	// Load entries from appliedIndex+1 onwards.
	// We use a large hi bound to load all remaining entries.
	entries, err := store.LoadEntries(appliedIndex+1, ^uint64(0))
	if err != nil {
		return err
	}
	for i := range entries {
		if err := fsm.Apply(&entries[i]); err != nil {
			// Non-fatal: log and continue (FSM errors are not fatal)
			continue
		}
	}
	return nil
}

// =====================================================================
// Lifecycle: Start / Stop
// =====================================================================

// Start begins the tick, Ready, and receive loops. Call after construction.
func (rn *RaftNode) Start() {
	rn.ctx, rn.cancel = context.WithCancel(context.Background())

	// Tick loop: fires raft node ticks at heartbeat interval / 2.
	tickInterval := time.Duration(rn.config.HeartbeatIntervalMs) * time.Millisecond / 2
	if tickInterval < time.Millisecond {
		tickInterval = time.Millisecond
	}
	rn.wg.Add(1)
	go rn.tickLoop(tickInterval)

	// Ready loop: processes raft.Ready() channel.
	rn.wg.Add(1)
	go rn.readyLoop()

	// Receive loop: reads incoming messages from transport and feeds to raft.
	rn.wg.Add(1)
	go rn.receiveLoop()

	rn.logger.Info("raft node started",
		"id", rn.nodeID,
		"tick_interval", tickInterval,
	)
}

// Stop gracefully shuts down the RaftNode. Idempotent.
func (rn *RaftNode) Stop() {
	rn.stopOnce.Do(func() {
		rn.logger.Info("raft node stopping", "id", rn.nodeID)
		close(rn.stopCh)
		if rn.cancel != nil {
			rn.cancel()
		}
		rn.node.Stop()
		rn.wg.Wait()
		rn.transport.Stop()
		close(rn.doneCh)
		rn.logger.Info("raft node stopped", "id", rn.nodeID)
	})
}

// Done returns a channel that is closed when the node is fully stopped.
func (rn *RaftNode) Done() <-chan struct{} {
	return rn.doneCh
}

// =====================================================================
// Core loops
// =====================================================================

func (rn *RaftNode) tickLoop(interval time.Duration) {
	defer rn.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-rn.stopCh:
			return
		case <-ticker.C:
			rn.node.Tick()
		}
	}
}

func (rn *RaftNode) readyLoop() {
	defer rn.wg.Done()
	for {
		select {
		case <-rn.stopCh:
			return
		case rd, ok := <-rn.node.Ready():
			if !ok {
				rn.logger.Info("raft ready channel closed")
				return
			}

			// 1. Persist HardState to MemoryStorage and BoltDB
			if !raft.IsEmptyHardState(rd.HardState) {
				rn.memStorage.SetHardState(rd.HardState)
				if err := rn.store.SaveHardState(rd.HardState); err != nil {
					rn.logger.Error("save hard state failed", "error", err)
				}
			}

			// 2. Persist Entries to MemoryStorage and BoltDB
			if len(rd.Entries) > 0 {
				rn.memStorage.Append(rd.Entries)
				if err := rn.store.SaveEntries(rd.Entries); err != nil {
					rn.logger.Error("save entries failed", "error", err)
				}
			}

			// 3. Send Messages to peers via Transport
			if len(rd.Messages) > 0 {
				rn.transport.Send(rd.Messages)
			}

			// 4. Apply CommittedEntries to FSM
			for i := range rd.CommittedEntries {
				entry := &rd.CommittedEntries[i]
				switch entry.Type {
				case raftpb.EntryConfChange:
					rn.applyConfChange(*entry)
				case raftpb.EntryConfChangeV2:
					rn.applyConfChangeV2(*entry)
				case raftpb.EntryNormal:
					if len(entry.Data) > 0 {
						if err := rn.fsm.Apply(entry); err != nil {
							rn.logger.Error("fsm apply failed",
								"error", err,
								"index", entry.Index,
							)
							// Non-fatal: continue processing
						}
					}
					// Empty entries: commit marker; still counted in applied index
				}
			}

			// 5. Handle Snapshot
			if !raft.IsEmptySnap(rd.Snapshot) {
				rn.handleSnapshot(rd.Snapshot)
			}

			// 6. Leadership tracking
			rn.trackLeadership(rd.SoftState)

			// 7. Acknowledge processing
			rn.node.Advance()
		}
	}
}

func (rn *RaftNode) receiveLoop() {
	defer rn.wg.Done()
	// The receiveLoop waits for incoming messages from the transport.
	// In the current channel-based transport stub, the transport calls
	// rn.Step() directly. This loop is a placeholder until P7-06
	// implements a real transport with a Receive() channel.
	<-rn.stopCh
}

// =====================================================================
// Proposal
// =====================================================================

// Propose submits data for consensus. If this node is not the leader,
// the proposal is forwarded to the leader via the transport.
func (rn *RaftNode) Propose(ctx context.Context, data []byte) error {
	if rn.IsLeader() {
		return rn.node.Propose(ctx, data)
	}

	// Not leader — forward to leader
	leaderID := rn.leaderID.Load()
	if leaderID == 0 || leaderID == raft.None {
		return ErrNotLeader
	}
	// Forward the proposal to the leader via the transport.
	// Construct a MsgProp message addressed to the leader.
	msg := raftpb.Message{
		Type:    raftpb.MsgProp,
		To:      leaderID,
		From:    rn.nodeID,
		Entries: []raftpb.Entry{{Data: data}},
	}
	rn.transport.Send([]raftpb.Message{msg})
	return nil
}

// ProposeCmd serializes a RaftCommand and submits it for consensus.
func (rn *RaftNode) ProposeCmd(ctx context.Context, cmd *RaftCommand) error {
	data, err := cmd.Serialize()
	if err != nil {
		return fmt.Errorf("serialize command: %w", err)
	}
	return rn.Propose(ctx, data)
}

// ProposeConfChange submits a configuration change through Raft consensus.
func (rn *RaftNode) ProposeConfChange(ctx context.Context, cc raftpb.ConfChangeI) error {
	return rn.node.ProposeConfChange(ctx, cc)
}

// Step feeds an incoming raft message to the node. Called by the transport
// layer when a message arrives from a peer.
func (rn *RaftNode) Step(msg raftpb.Message) error {
	return rn.node.Step(rn.ctx, msg)
}

// =====================================================================
// Member changes
// =====================================================================

// AddNode proposes adding a node to the cluster. Only the leader can call this.
func (rn *RaftNode) AddNode(ctx context.Context, nodeID uint64, raftAddr string) error {
	if !rn.IsLeader() {
		return ErrNotLeader
	}
	cc := raftpb.ConfChange{
		Type:    raftpb.ConfChangeAddNode,
		NodeID:  nodeID,
		Context: []byte(raftAddr),
	}
	return rn.ProposeConfChange(ctx, cc)
}

// RemoveNode proposes removing a node from the cluster. Only the leader can call this.
func (rn *RaftNode) RemoveNode(ctx context.Context, nodeID uint64) error {
	if !rn.IsLeader() {
		return ErrNotLeader
	}
	cc := raftpb.ConfChange{
		Type:   raftpb.ConfChangeRemoveNode,
		NodeID: nodeID,
	}
	return rn.ProposeConfChange(ctx, cc)
}

// applyConfChange applies a committed ConfChange entry.
func (rn *RaftNode) applyConfChange(entry raftpb.Entry) {
	var cc raftpb.ConfChange
	if err := cc.Unmarshal(entry.Data); err != nil {
		rn.logger.Error("unmarshal conf change failed", "error", err)
		return
	}
	cs := rn.node.ApplyConfChange(cc)

	// Update transport
	switch cc.Type {
	case raftpb.ConfChangeAddNode:
		rn.transport.AddPeer(cc.NodeID, string(cc.Context))
		rn.logger.Info("node added to cluster", "nodeID", cc.NodeID)
	case raftpb.ConfChangeRemoveNode:
		rn.transport.RemovePeer(cc.NodeID)
		rn.logger.Info("node removed from cluster", "nodeID", cc.NodeID)
	}

	// Save updated ConfState
	if err := rn.store.SaveConfState(*cs); err != nil {
		rn.logger.Error("save conf state failed", "error", err)
	}
}

// applyConfChangeV2 applies a committed ConfChangeV2 entry.
func (rn *RaftNode) applyConfChangeV2(entry raftpb.Entry) {
	var cc raftpb.ConfChangeV2
	if err := cc.Unmarshal(entry.Data); err != nil {
		rn.logger.Error("unmarshal conf change v2 failed", "error", err)
		return
	}
	cs := rn.node.ApplyConfChange(cc)

	for _, ch := range cc.Changes {
		switch ch.Type {
		case raftpb.ConfChangeAddNode:
			rn.transport.AddPeer(ch.NodeID, string(cc.Context))
		case raftpb.ConfChangeRemoveNode:
			rn.transport.RemovePeer(ch.NodeID)
		}
	}

	if err := rn.store.SaveConfState(*cs); err != nil {
		rn.logger.Error("save conf state failed", "error", err)
	}
}

// =====================================================================
// Snapshot handling
// =====================================================================

func (rn *RaftNode) handleSnapshot(snap raftpb.Snapshot) {
	rn.logger.Info("applying snapshot",
		"index", snap.Metadata.Index,
		"term", snap.Metadata.Term,
	)
	// Restore FSM from snapshot
	if err := rn.fsm.Restore(snap.Data); err != nil {
		rn.logger.Error("fsm restore from snapshot failed", "error", err)
		return
	}
	// Compact MemoryStorage
	if err := rn.memStorage.ApplySnapshot(snap); err != nil {
		rn.logger.Error("memStorage apply snapshot failed", "error", err)
	}
	// Compact BoltDB log: delete entries up to snapshot index
	if err := rn.store.CompactLog(snap.Metadata.Index); err != nil {
		rn.logger.Error("compact log failed", "error", err)
	}
}

// =====================================================================
// Leadership tracking
// =====================================================================

func (rn *RaftNode) trackLeadership(ss *raft.SoftState) {
	if ss == nil {
		return
	}
	oldLeader := rn.leaderID.Load()
	newLeader := ss.Lead

	if oldLeader != newLeader {
		rn.leaderID.Store(newLeader)
		rn.logger.Info("leadership changed",
			"old_leader", oldLeader,
			"new_leader", newLeader,
		)

		// Became leader
		if oldLeader != rn.nodeID && newLeader == rn.nodeID {
			rn.logger.Info("this node became leader", "id", rn.nodeID)
			if rn.onLeaderStart != nil {
				rn.onLeaderStart()
			}
		}

		// Lost leadership
		if oldLeader == rn.nodeID && newLeader != rn.nodeID {
			rn.logger.Info("this node lost leadership", "id", rn.nodeID)
			if rn.onLeaderStop != nil {
				rn.onLeaderStop()
			}
		}
	}
}

// IsLeader returns true if this node is currently the Raft leader.
func (rn *RaftNode) IsLeader() bool {
	return rn.leaderID.Load() == rn.nodeID
}

// GetLeaderID returns the current leader's node ID, or 0 if unknown.
func (rn *RaftNode) GetLeaderID() uint64 {
	return rn.leaderID.Load()
}

// OnLeaderStart sets a callback invoked when this node becomes the leader.
func (rn *RaftNode) OnLeaderStart(fn func()) {
	rn.onLeaderStart = fn
}

// OnLeaderStop sets a callback invoked when this node loses leadership.
func (rn *RaftNode) OnLeaderStop(fn func()) {
	rn.onLeaderStop = fn
}

// =====================================================================
// Accessors
// =====================================================================

// NodeID returns this node's Raft ID.
func (rn *RaftNode) NodeID() uint64 {
	return rn.nodeID
}

// FSM returns the underlying ReefFSM.
func (rn *RaftNode) FSM() *ReefFSM {
	return rn.fsm
}
