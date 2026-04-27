# Reef Architecture

This document describes the design and internal structure of the Reef distributed multi-agent swarm system.

## Design Goals

1. **Decentralized execution**: Any PicoClaw node can act as a client; tasks execute where the best-fit agent lives.
2. **Resilient routing**: Task failures trigger automatic retry, reassignment, and escalation.
3. **Minimal footprint**: Server and client share the same binary; no separate services needed.
4. **Observable**: HTTP Admin API exposes full system state for monitoring and debugging.

## Component Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        Reef Server                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  WebSocket  │  │  Scheduler  │  │     Admin Server    │  │
│  │   Server    │  │             │  │                     │  │
│  │   :8080     │  │  - Match    │  │  - /admin/status    │  │
│  │             │  │  - Dispatch │  │  - /admin/tasks     │  │
│  │  Accepts    │  │  - Retry    │  │  - /tasks           │  │
│  │  Clients    │  │  - Escalate │  │                     │  │
│  └──────┬──────┘  └──────┬──────┘  └─────────────────────┘  │
│         │                │                                   │
│  ┌──────┴────────────────┴──────┐                           │
│  │          Registry            │                           │
│  │  - Client capabilities       │                           │
│  │  - Heartbeat tracking        │                           │
│  │  - Load balancing state      │                           │
│  └──────────────────────────────┘                           │
│         ▲                                                   │
│         │ WebSocket                                          │
│         │                                                    │
│  ┌──────┴──────┐     ┌─────────────────┐                   │
│  │  Task Queue │     │ Heartbeat Scanner│                  │
│  │  (in-mem)   │     │  (stale detection)│                 │
│  └─────────────┘     └─────────────────┘                   │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ WebSocket (real messages)
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
        ┌──────────┐   ┌──────────┐   ┌──────────┐
        │ Client A │   │ Client B │   │ Client C │
        │  coder   │   │ analyst  │   │  tester  │
        └──────────┘   └──────────┘   └──────────┘
```

## Component Details

### WebSocket Server (`pkg/reef/server/websocket.go`)

- Accepts WebSocket upgrades on `/ws`
- Validates `x-reef-token` header if token is configured
- Handshake requires the first message to be `register`
- Routes incoming messages to the scheduler/registry
- Buffers control messages (`cancel`, `pause`, `resume`) for disconnected clients

### Registry (`pkg/reef/server/registry.go`)

Thread-safe map of `ClientInfo`:

```go
type ClientInfo struct {
    ID            string      // unique client identifier
    Role          string      // e.g. "coder"
    Skills        []string    // e.g. ["github", "write_file"]
    Capacity      int         // max concurrent tasks
    CurrentLoad   int         // tasks currently assigned
    LastHeartbeat time.Time
    State         ClientState // connected | disconnected | stale
}
```

### Task Queue (`pkg/reef/server/queue.go`)

In-memory FIFO queue with:
- Max length (default 1000)
- Max age (default 10 minutes) — tasks older than this are expired and marked failed

### Scheduler (`pkg/reef/server/scheduler.go`)

The core dispatch engine:

1. **Task Submission**: Creates task, transitions to `Queued`, enqueues
2. **TryDispatch**: Dequeues tasks, matches to best-fit client
3. **Match Algorithm**: `role match → skill coverage → capacity → lowest current load`
4. **Failure Handling**: Records attempt history, decides `reassign | terminate | escalate`
5. **Escalation Policy**: Configurable `max_escalations` (default 2). After exhaustion, task moves to `Escalated`

### SwarmChannel (`pkg/channels/swarm/`)

Implements PicoClaw's `Channel` interface:

- When enabled in client mode, connects to Reef Server via WebSocket
- Registers with role + skills + capacity
- Receives `task_dispatch` messages and routes them into PicoClaw's `AgentLoop`
- Reports progress, completion, or failure back to the Server
- Responds to `cancel`, `pause`, `resume` control messages

### Task Runner (`pkg/reef/client/task_runner.go`)

Client-side execution wrapper:

- Wraps PicoClaw's `AgentLoop` execution
- Local retry with exponential backoff
- Respects `TaskContext` for cancellation and pause/resume
- Reports final result or failure to Server

## Message Flow

### Happy Path: Task Dispatch → Execution → Completion

```
User → POST /tasks ──► Server ──► Scheduler.Submit()
                                      │
                                      ▼
                              Scheduler.TryDispatch()
                                      │
                                      ▼
                              WebSocketServer.SendMessage()
                                      │
                                      ▼ (WebSocket)
                              SwarmChannel.receive()
                                      │
                                      ▼
                              AgentLoop.RunTurn()
                                      │
                                      ▼
                              task_completed message
                                      │
                                      ▼ (WebSocket)
                              Server.HandleTaskCompleted()
                                      │
                                      ▼
                              Task.Status = Completed
```

### Failure Path: Retry and Reassignment

```
AgentLoop.RunTurn() fails
        │
        ▼
task_failed message
        │
        ▼ (WebSocket)
Server.HandleTaskFailed()
        │
        ├── Record failed attempt
        ├── Decrement client load
        ├── Transition to Failed
        │
        ▼
Scheduler.escalate()
        │
        ├── EscalationReassign → requeue, TryDispatch() to another client
        ├── EscalationTerminate → stay Failed
        └── EscalationToAdmin → transition to Escalated
```

### Connection Resilience

```
Client disconnects
        │
        ▼
Server marks client "disconnected"
        │
        ▼
In-flight tasks → TaskPaused (PauseReason: "disconnect")
        │
        ▼
Client reconnects + re-registers
        │
        ▼
Server flushes buffered control messages
        │
        ▼
Scheduler.HandleClientAvailable() → TryDispatch() queued tasks
```

## Threading Model

| Component | Goroutines | Notes |
|-----------|-----------|-------|
| WebSocket Server | 2 per client (read + write) | One read loop, one write loop per connection |
| Heartbeat Scanner | 1 | Ticker-driven stale detection |
| Scheduler dispatch | 1 per TryDispatch | Short-lived goroutine for async dispatch |
| Client connector | 1 + retry goroutine | Main loop + exponential backoff reconnect |
| Client heartbeat | 1 | Ticker-driven heartbeat sender |

## Data Flow Diagram

```
┌──────────┐     HTTP      ┌────────────┐
│  Admin   │ ────────────► │  Scheduler │
│   API    │               │   Submit   │
└──────────┘               └─────┬──────┘
                                 │
                                 ▼
                          ┌──────────────┐
                          │  Task Queue  │
                          │   (FIFO)     │
                          └──────┬───────┘
                                 │ Dequeue
                                 ▼
                          ┌──────────────┐
                          │   Registry   │
                          │  matchClient │
                          └──────┬───────┘
                                 │
                                 ▼
                          ┌──────────────┐
                          │ WebSocket    │
                          │ SendMessage  │
                          └──────┬───────┘
                                 │ task_dispatch
                                 ▼
                          ┌──────────────┐
                          │   Client     │
                          │ AgentLoop    │
                          └──────┬───────┘
                                 │ task_completed
                                 ▼
                          ┌──────────────┐
                          │   Server     │
                          │ HandleTask   │
                          │   Completed  │
                          └──────────────┘
```

## Scaling Considerations (v1)

- **Server**: Single process, in-memory state. Handles 100s of clients comfortably.
- **Queue**: In-memory only. Tasks are lost on server restart.
- **Registry**: In-memory only. Clients must re-register after server restart.

For production workloads, consider:
- Persistent queue backend (Redis, NATS, or SQLite WAL)
- Server clustering behind a load balancer with shared state
- Client-side caching of pending results during reconnection

See [Deployment Guide](deployment.md) for Docker Compose and systemd setups.
