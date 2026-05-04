# P6-10 GeneBroadcaster — Implementation Summary

## Phase & Plan
- **Phase**: 06 — Evolution Engine
- **Plan**: P6-10-gene-broadcaster-PLAN.md
- **Wave**: 3
- **Depends on**: P6-08, P6-02

## Files Created/Modified

| File | Action | Lines |
|------|--------|-------|
| `pkg/reef/evolution/server/broadcaster.go` | Created | ~280 |
| `pkg/reef/evolution/server/broadcaster_test.go` | Created | ~510 |
| `pkg/reef/client/connector.go` | Modified (LIGHT) | +25 |
| `pkg/reef/client/connector_test.go` | Modified | +130 |

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  EvolutionHub                        │
│  HandleGeneSubmission(...)                           │
│    └─ b.broadcaster.Broadcast(ctx, gene, src)       │
└──────────────────────┬──────────────────────────────┘
                       │
          ┌────────────▼────────────┐
          │      Broadcaster        │
          │  implements             │
          │  GeneBroadcaster        │
          ├─────────────────────────┤
          │ • Broadcast()           │
          │ • sendToClient()        │
          │ • OnClientReconnect()   │
          │ • RecordOffline()       │
          ├─────────────────────────┤
          │  RoleFinder  FindByRole │──► registry (decoupled)
          │  ConnManager.SendToClient│──► WebSocketServer
          │  GeneStore   GetGene    │──► SQLite/BoltDB
          │  pendingSync map        │─── offline resync queue
          └─────────────────────────┘
                       │
          ┌────────────▼────────────┐
          │   Connector (client)    │
          │  readLoop               │
          │    └─ handleGeneBroadcast│
          │       └─ onGeneBroadcast │──► user callback
          └─────────────────────────┘
```

## Key Design Decisions

1. **Interface naming**: Struct is `Broadcaster`, implementing `GeneBroadcaster` interface (to avoid interface/struct name collision in same package).

2. **RoleFinder abstraction**: Uses a `RoleFinder` interface with `FindByRole()` and `FindBySkills()`. This avoids import cycles with `pkg/reef/server`. Server code wraps `Registry.ListByRole()` into a `RoleFinder` adapter.

3. **Best-effort delivery**: `Broadcast()` returns nil even when individual client sends fail. Failures are logged at WARN level and tracked in `pendingSync`.

4. **Concurrency control**: Semaphore channel (`MaxConcurrentSends` default 20) limits concurrent goroutines. Context cancellation stops launching new goroutines but already-launched ones complete.

5. **pendingSync cap**: Max 50 genes per offline client. Oldest entries dropped with WARN log. Duplicate gene IDs are detected and skipped.

6. **Client-side handler**: `handleGeneBroadcast()` intercepts gene_broadcast messages in the readLoop before they reach `msgInCh`. The callback is invoked in a goroutine to avoid blocking the read loop.

## Test Results

```
Broadcaster tests:       18/18 PASS
Connector gene_broadcast: 5/5 PASS
Full pkg/reef suite:      ALL PASS
Race detector:           NOT SUPPORTED (android/arm64)
```

## Must-Haves Checklist

- [x] Broadcast sends gene_broadcast to all online clients with matching role (excluding source)
- [x] Concurrent goroutine-per-target sends with configurable semaphore limit
- [x] Offline clients tracked in pendingSync map, resynced on reconnect
- [x] pendingSync capped at 50 entries per client (memory safety)
- [x] Client Connector handles gene_broadcast messages with callback
- [x] Best-effort delivery: individual failures do not fail overall broadcast
- [x] Zero import cycles between evolution/server and server packages

## Commits

```
85a9fad feat(06-10): implement Broadcaster with concurrent send and pendingSync
58c967f feat(06-10): add client-side gene_broadcast handler
```
