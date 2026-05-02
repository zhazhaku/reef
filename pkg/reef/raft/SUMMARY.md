# P7-07: LeaderedServer — Summary

## Changes Made

### 1. `pkg/reef/raft/leadered.go` (new file)
- **LeaderedServer** struct wrapping RaftNode, ReefFSM, and BoltStore
- **NewLeaderedServer(cfg)** — creates LeaderedServer, wires leadership callbacks, syncs initial state
- **Start()/Stop()** — lifecycle management with idempotent Stop
- **Propose(ctx, cmd)** — validates leadership, serializes and proposes via RaftNode
- **ProposeWithCallback(ctx, cmd, fn)** — proposes + registers callback
- **ProposeAndWait(ctx, cmd)** — proposes and waits for FSM to advance
- **SubmitRaftCommand(ctx, cmd)** — validates, serializes, proposes
- **ForwardToLeader(addr, cmd)** — HTTP POST forwarded proposal to leader
- **Status()** — returns ClusterStatus (leader, term, index, peers)
- **IsLeader()** — atomic bool check
- **4 Leader-exclusive goroutines**: runHeartbeatScanner, runTimeoutScanner, runClaimExpiryScanner, runSnapshotTicker
- **onBecomeLeader/onLoseLeadership** — goroutine lifecycle management via leaderStopCh
- **snapshotToFsmsnapshot** — FSM accessor for snapshot ticker
- **TaskSnapshot/ClientSnapshot** — thread-safe FSM state accessors

### 2. `pkg/reef/raft/node.go` (modified)
- Added `OnCommit func(index uint64)` callback field to RaftNode
- Fires after each committed entry is applied (for ProposeWithCallback)

### 3. `pkg/reef/raft/types.go` (modified)
- Removed stub `LeaderedServer` struct and methods
- Removed unused imports (`sync/atomic`, `go.etcd.io/raft/v3`)

### 4. `pkg/reef/server/server.go` (modified)
- Added `Mode` field to `Config` (defaults to "standalone")
- Added `Raft *RaftConfig` field to `Config`
- Added `RaftConfig` struct with validation (avoids import cycle with raft package)
- Added `Validate()` methods for both Config and RaftConfig

### 5. `pkg/reef/raft/leadered_test.go` (new file ~1425 lines)
- 23 test functions covering:
  - Construction and validation
  - Leadership callbacks (become/lose leader, idempotency)
  - Initial leadership sync
  - Propose, ProposeNotLeader, ProposeAndWait, ProposeWithCallback
  - SubmitRaftCommand (valid + invalid)
  - Status, ForwardToLeader
  - Task/Client snapshots
  - Done channel
  - Integration: full task lifecycle (register client → enqueue → assign → complete)
  - Multi-domain proposals (task, client, claim, dag)
  - Snapshot ticker triggers compaction
  - Scanner goroutine lifecycle (start/stop)
  - Heartbeat scanner: stale client detection
  - Timeout scanner: timed-out task detection
  - Claim expiry scanner: expired claim detection
  - Empty FSM safety
  - Stop idempotency

### 6. `pkg/reef/raft/fulfilment_test.go` (modified)
- Updated old stub tests to use new LeaderedServer API
- Added `context`, `log/slog` imports

## Test Results
```
go test -count=1 -short ./pkg/reef/raft/...  → PASS (24.430s)
go test -count=1 -short ./pkg/reef/server/... → PASS (2.753s)
go vet ./pkg/reef/...                         → clean
```

## Design Decisions
1. **No import cycle**: `server.Config.Raft` uses a local `RaftConfig` struct (not `raft.RaftConfig`) to avoid `server → raft` import. Higher-level factory code handles conversion.
2. **Separate isLeader flag**: LeaderedServer maintains its own atomic bool (not relying solely on RaftNode's leaderID) for faster checks without function calls.
3. **Callback cleanup**: Pending callbacks older than 30 seconds are garbage-collected by the snapshot ticker's cleanup goroutine.
4. **Scanner resilience**: All 4 scanner goroutines have panic recovery to prevent server crashes.
