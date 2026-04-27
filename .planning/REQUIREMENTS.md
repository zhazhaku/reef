# Reef v1 Requirements

## v1 Requirements

### Swarm Protocol
- [ ] **SWARM-01**: Server maintains a live registry of connected Clients, including role, skills list, capacity, and last heartbeat time
- [ ] **SWARM-02**: Client registers with Server on startup, advertising its role, skills, providers, and max concurrent capacity
- [ ] **SWARM-03**: Client sends periodic heartbeats to Server; Server marks Client offline after missed heartbeat threshold
- [ ] **SWARM-04**: WebSocket message protocol supports register, heartbeat, task, progress, cancel, pause, resume, failed, completed message types

### Task Scheduling
- [ ] **SCHED-01**: Server matches incoming tasks to best-fit Client based on required role and skill set
- [ ] **SCHED-02**: Server dispatches task to selected Client via WebSocket with task ID, instruction, context, and max retry count
- [ ] **SCHED-03**: Server queues tasks when no matching Client is available and dispatches when one becomes ready

### Task Execution
- [ ] **TASK-01**: Client receives dispatched task, injects it into PicoClaw AgentLoop as an inbound message, and executes it
- [ ] **TASK-02**: Client reports task progress to Server at configurable intervals (started, running %, completed)
- [ ] **TASK-03**: Client reports task completion with result payload to Server
- [ ] **TASK-04**: Client reports task failure with error details and attempt count to Server

### Task Lifecycle Control
- [ ] **LIFE-01**: Server can send cancellation signal to Client; Client aborts in-flight task via context cancellation
- [ ] **LIFE-02**: Server can send pause signal to Client; Client pauses task execution and reports paused status
- [ ] **LIFE-03**: Server can send resume signal to Client; Client resumes paused task execution
- [ ] **LIFE-04**: Client-side task context carries task ID and cancel function, hookable by AgentLoop processOptions

### Failure Handling
- [ ] **RETRY-01**: Client retries failed task execution locally up to configurable max attempts before reporting failure
- [ ] **RETRY-02**: After exhausting local retries, Client escalates to Server with full error logs and attempt history
- [ ] **RETRY-03**: Server decides escalation outcome: reassign to another Client, terminate task, or escalate to human/admin

### Connection Resilience
- [ ] **CONN-01**: Client reconnects to Server with exponential backoff after WebSocket disconnection
- [ ] **CONN-02**: Client pauses (does not fail) in-flight task during disconnection; resumes after reconnection
- [ ] **CONN-03**: Server handles Client reconnection gracefully, reusing existing registry entry if within timeout window

### Role-based Skills
- [ ] **ROLE-01**: Client loads a role-specific subset of skills from the built-in toolbox at startup
- [ ] **ROLE-02**: Role config maps to a skills manifest (list of skill names) and a system prompt override
- [ ] **ROLE-03**: Client advertises loaded skills during registration so Server can match accurately

### Admin & Observability
- [ ] **ADMIN-01**: Server exposes HTTP `/admin/status` endpoint returning JSON with all connected Clients and their states
- [ ] **ADMIN-02**: Server exposes HTTP `/admin/tasks` endpoint returning JSON with task queue and in-flight task statuses
- [ ] **ADMIN-03**: All Reef components log structured events (connect, register, task dispatch, failure, cancel) at info level

## v2 Requirements (Deferred)
- Persistent task queue with disk-backed recovery (SQLite/WAL)
- Web UI dashboard for visual task and Client management
- Multi-Server federation with gossip protocol
- Dynamic role expansion via skill hot-loading
- Client-to-Client direct channels for data streaming
- Authentication with per-Client API keys and JWT

## Out of Scope
- gRPC or HTTP polling transport — WebSocket only
- LLM model training or fine-tuning — Reef is an orchestration layer
- Cross-platform GUI client — CLI only for v1
- Billing or quota metering per Client

## Traceability

| Requirement | Phase |
|-------------|-------|
| SWARM-01 ~ 04 | Phase 1 |
| SCHED-01 ~ 03 | Phase 2 |
| TASK-01 ~ 04 | Phase 3 |
| LIFE-01 ~ 04 | Phase 4 |
| RETRY-01 ~ 03 | Phase 4 |
| CONN-01 ~ 03 | Phase 3 |
| ROLE-01 ~ 03 | Phase 5 |
| ADMIN-01 ~ 03 | Phase 2 |
