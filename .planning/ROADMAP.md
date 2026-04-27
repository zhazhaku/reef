# Reef Roadmap

## Overview

| # | Phase | Goal | Requirements | Success Criteria |
|---|-------|------|--------------|------------------|
| 1 | Swarm Protocol & Core Types | Define wire protocol, message types, task state machine, and shared structs | SWARM-01~04 | 4 |
| 2 | Reef Server | Build the hub: registry, scheduler, HTTP admin, and WebSocket acceptor | SWARM-01~03, SCHED-01~03, ADMIN-01~03 | 6 |
| 3 | Reef Client & SwarmChannel | Build the spoke: WebSocket client channel, task injection into AgentLoop, progress reporting, reconnection | TASK-01~04, CONN-01~03 | 6 |
| 4 | Task Lifecycle & Failure Handling | Add cancel/pause/resume, local retry, and Server-side escalation decisions | LIFE-01~04, RETRY-01~03 | 7 |
| 5 | Role-based Skills & E2E Integration | Wire role-to-skills mapping, system prompt override, and end-to-end integration tests | ROLE-01~03 | 3 |

---

## Phase 1: Swarm Protocol & Core Types

**Goal:** Establish the contract between Server and Client so both sides can be built in parallel.

**Requirements:** SWARM-01, SWARM-02, SWARM-03, SWARM-04

**Success Criteria:**
1. `pkg/reef/protocol.go` defines all message types with JSON serialization
2. `pkg/reef/task.go` defines Task state machine (Created → Assigned → Running → Completed/Failed/Paused)
3. `pkg/reef/client.go` defines Client capability struct (role, skills, capacity)
4. Protocol constants and enums are versioned (ReefProtocolV1)
5. Unit tests cover marshal/unmarshal round-trip for all message types

---

## Phase 2: Reef Server

**Goal:** Server can accept Client connections, maintain registry, schedule tasks, and expose admin status.

**Requirements:** SWARM-01, SWARM-02, SWARM-03, SCHED-01, SCHED-02, SCHED-03, ADMIN-01, ADMIN-02, ADMIN-03

**Success Criteria:**
1. `cmd/reef/` main entry supports `--mode=server` with config loading
2. `pkg/reef/server/` WebSocket acceptor handles register/heartbeat messages
3. `pkg/reef/server/registry.go` maintains thread-safe Client registry with heartbeat eviction
4. `pkg/reef/server/scheduler.go` matches tasks to Clients by role+skills and dispatches via WebSocket
5. `pkg/reef/server/queue.go` holds tasks when no Client is available
6. HTTP admin endpoints `/admin/status` and `/admin/tasks` return correct JSON
7. Structured logging emits connect, register, dispatch, disconnect events
8. Server integration test spins up, accepts a mock Client, and dispatches a task

---

## Phase 3: Reef Client & SwarmChannel

**Goal:** Client can connect to Server, receive tasks, execute them via PicoClaw AgentLoop, report progress, and survive reconnections.

**Requirements:** TASK-01, TASK-02, TASK-03, TASK-04, CONN-01, CONN-02, CONN-03

**Success Criteria:**
1. `pkg/channels/swarm/` implements PicoClaw `Channel` interface over WebSocket
2. Client sends register message on connect with role, skills, capacity
3. Client heartbeats at configurable interval
4. Received task is published to MessageBus as Inbound message and picked up by AgentLoop
5. AgentLoop hooks (`processOptions` extension) wrap execution with TaskContext (taskID, cancelFunc)
6. Progress/completion/failure messages sent back to Server during and after execution
7. Exponential backoff reconnection on disconnect; in-flight task pauses (not fails)
8. Client integration test: connect → receive task → mock execution → report completed

---

## Phase 4: Task Lifecycle & Failure Handling

**Goal:** Full control over running tasks: cancel, pause, resume, retry, and escalate.

**Requirements:** LIFE-01, LIFE-02, LIFE-03, LIFE-04, RETRY-01, RETRY-02, RETRY-03

**Success Criteria:**
1. Server can send `cancel` control message; Client aborts task via `context.Cancel`
2. Server can send `pause` control message; Client blocks AgentLoop continue until `resume`
3. Server can send `resume` control message; Client resumes blocked task
4. Client retries failed task up to `max_retries` from task config before reporting
5. Client `task_failed` message includes error, logs, and attempt history
6. Server escalation handler decides: reassign (to another Client), terminate, or escalate to admin
7. State machine transitions verified in unit tests for all paths

---

## Phase 5: Role-based Skills & E2E Integration

**Goal:** Clients boot with role-specific skills and system prompts; full system works end-to-end.

**Requirements:** ROLE-01, ROLE-02, ROLE-03

**Success Criteria:**
1. Config supports `reef.role` field (coder, analyst, tester, etc.)
2. Role maps to skill manifest in `skills/roles/<role>.yaml` listing which skills to load
3. Role maps to system prompt override for AgentLoop initialization
4. Client advertises only loaded skills during registration
5. E2E test: start Server → start two Clients with different roles → submit task requiring specific role → verify correct Client executes it → verify result returned
6. README documents how to add a new role and deploy a Client node

---

*Last updated: 2026-04-27*
