# P7-05: RaftNode — Summary

## Status: COMPLETE (all tests pass, go vet clean)

## Files Modified

### pkg/reef/raft/node.go
- **RaftConfig**: Config struct with NodeID, Peers, ElectionTimeoutMs, HeartbeatIntervalMs, MaxSizePerMsg, MaxInflightMsgs, MaxUncommittedEntriesSize, CheckQuorum, PreVote
- **RaftNode struct**: Wraps raft.Node with lifecycle (Start/Stop), leadership tracking (IsLeader, GetLeaderID, OnLeaderStart, OnLeaderStop), proposal forwarding, member changes
- **NewRaftNode**: Creates fresh raft node via raft.StartNode. Empty peers auto-adds self for single-node bootstrap (raft/v3.6.0 requires non-empty peers)
- **NewRestartNode**: Restarts from BoltDB persisting state — replays committed entries to FSM, populates MemoryStorage with HardState/ConfState/Entries, then calls raft.RestartNode
- **Start/Stop**: Tick loop (HeartbeatIntervalMs/2), Ready loop, receive loop. Stop is idempotent (sync.Once)
- **Ready loop**: 5-stage processing: persist HardState, persist Entries, send Messages, apply CommittedEntries (normal/ConfChange/ConfChangeV2), handle Snapshot, track leadership, Advance
- **Propose/ProposeCmd/ProposeConfChange**: Leader-proposes directly, follower forwards via Transport with MsgProp message
- **AddNode/RemoveNode**: ConfChange proposals via raft.ProposeConfChange — only leader can call
- **raftLoggerAdapter**: Adapts slog.Logger to raft.Logger; Fatal/Panic logged at Error level
- Transport interface: Send, AddPeer, RemovePeer, Stop

### pkg/reef/raft/node_test.go
- **testTransport/testCluster**: In-memory channel-based transport and cluster test harness
- 30+ tests covering:
  - Construction + validation (nil store, nil fsm, nil transport, config edge cases)
  - Single-node: start/stop, propose, leadership callbacks
  - Multi-node: leader election, proposal forwarding (follower→leader), conf change (add node)
  - Restart: persist + replay from BoltDB
  - 3-node integration: 5 proposals, FSM consistency across all nodes
  - Member changes: AddNode/RemoveNode with ErrNotLeader guard
  - Edge cases: idempotent Stop, raftLoggerAdapter, ConfState persistence

### pkg/reef/raft/types.go
- Fixed CmdTaskEnqueue FSM handler to support both TaskEnqueuePayload format (`{"task_id":"...","task_data":{...}}`) and direct reef.Task format (`{"id":"...","instruction":"..."}`)

## Key Design Decisions
1. Empty peers auto-adds self — raft/v3.6.0 requires non-empty peers for StartNode
2. Follower Propose forwards via Transport (MsgProp message) rather than DisableProposalForwarding
3. NewRestartNode replays entries to FSM THEN populates MemoryStorage for correct appliedIndex
4. ConfState persisted to BoltDB and restored via snapshot in MemoryStorage on restart

## Test Results
```
ok  github.com/sipeed/reef/pkg/reef/raft  12.261s
All tests PASS. go vet: zero warnings.
```
