# E2E Integration Test Specifications

## Requirement: Server Startup and Client Registration
The system SHALL accept WebSocket connections and register clients with correct capabilities.

### Scenario: Single client registers successfully
- GIVEN a Reef Server is listening on an ephemeral port
- WHEN a Client opens a WebSocket connection and sends `register` with role="coder", skills=["github"], capacity=2
- THEN the Server responds with `register_ack`
- AND `/admin/status` returns 1 connected client with matching role and skills

### Scenario: Multiple clients with different roles register
- GIVEN a Reef Server is running
- WHEN a "coder" client and an "analyst" client both register
- THEN `/admin/status` returns 2 connected clients with correct distinct roles

### Scenario: Client heartbeat keeps connection alive
- GIVEN a registered client with heartbeat interval of 1 second
- WHEN the client sends heartbeats every second for 5 seconds
- THEN the client remains in "connected" state on `/admin/status`
- AND the Server's registry shows updated last_heartbeat timestamps

### Scenario: Unregistered client is rejected without valid token
- GIVEN a Server configured with token="secret123"
- WHEN a Client connects without the `x-reef-token` header
- THEN the WebSocket handshake is rejected (HTTP 401)

---

## Requirement: Task Dispatch and Execution
The system SHALL match tasks to clients by role and skills, dispatch via WebSocket, and track execution.

### Scenario: Task dispatched to matching role client
- GIVEN a Server with a registered "coder" client (capacity=2, skills=["github"])
- WHEN a task is submitted via POST /tasks with required_role="coder", instruction="write a test"
- THEN the client receives `task_dispatch` within 2 seconds
- AND the task's task_id matches the submitted task

### Scenario: Task queued when no matching client available
- GIVEN a Server with a registered "analyst" client
- WHEN a task is submitted with required_role="coder"
- THEN the task appears in `/admin/tasks` queued_tasks with status="Queued"
- AND no `task_dispatch` is sent to the analyst client

### Scenario: Task dispatched when matching client later connects
- GIVEN a Server with a queued task requiring role="coder"
- WHEN a "coder" client registers
- THEN the queued task is dispatched to the new client within 2 seconds

### Scenario: Task routed to lowest-load client
- GIVEN a Server with two "coder" clients (capacity=3 each), one with load=1 and one with load=0
- WHEN a task is submitted requiring role="coder"
- THEN the task is dispatched to the client with load=0

---

## Requirement: Task Completion and Result Reporting
The system SHALL receive task results from clients and update task state accordingly.

### Scenario: Client reports task completion
- GIVEN a dispatched task on a connected client
- WHEN the client sends `task_completed` with result text="done"
- THEN `/admin/tasks` shows the task with status="Completed"
- AND the task result contains text="done"

### Scenario: Client reports task progress
- GIVEN a running task on a connected client
- WHEN the client sends `task_progress` with status="running", progress_percent=50
- THEN `/admin/tasks` shows the task with status="Running"

---

## Requirement: Task Failure and Escalation
The system SHALL handle task failures with retry, reassignment, and escalation.

### Scenario: Failed task is reassigned to another client
- GIVEN a Server with two "coder" clients (A and B)
- AND a task dispatched to client A
- WHEN client A sends `task_failed` with error_type="execution_error"
- THEN the task is reassigned to client B (not A)
- AND client B receives `task_dispatch`

### Scenario: Task escalated after max retries exhausted
- GIVEN a Server with max_escalations=1 and one "coder" client
- AND a task that fails once
- WHEN the task fails again after reassignment (or retry)
- THEN the task transitions to status="Escalated"
- AND `/admin/tasks` shows the task as "Escalated"

### Scenario: Client local retry before reporting failure
- GIVEN a Reef Client with max_retries=2
- AND an exec function that fails twice then succeeds
- WHEN a task is started
- THEN the client retries up to 2 times locally
- AND only reports `task_completed` after eventual success

---

## Requirement: Task Lifecycle Control
The system SHALL support cancel, pause, and resume operations.

### Scenario: Server cancels a running task
- GIVEN a running task on a connected client
- WHEN the Server sends `cancel` control message
- THEN the client aborts the task
- AND sends `task_failed` with error_type="cancelled"

### Scenario: Server pauses and resumes a task
- GIVEN a running task on a connected client
- WHEN the Server sends `pause` control message
- THEN the client pauses execution and reports status="paused"
- WHEN the Server sends `resume` control message
- THEN the client resumes execution and reports status="running"

---

## Requirement: Connection Resilience
The system SHALL handle WebSocket disconnections gracefully.

### Scenario: Client reconnects after disconnection
- GIVEN a connected client with a running task
- WHEN the client disconnects (closes WebSocket)
- THEN the Server marks the client as "disconnected" or "stale"
- AND in-flight tasks are paused
- WHEN the client reconnects and re-registers
- THEN the client receives pending control messages (if any)

### Scenario: Client exponential backoff reconnection
- GIVEN a Client configured with server URL
- WHEN the Server is temporarily unavailable
- THEN the Client attempts reconnect with exponential backoff
- AND successfully reconnects when the Server comes back

---

## Requirement: Admin API Observability
The system SHALL expose HTTP endpoints for monitoring.

### Scenario: Admin status reflects accurate system state
- GIVEN a Server with 2 connected clients and 1 queued task
- WHEN GET /admin/status is called
- THEN response contains 2 connected clients, correct server_version
- AND uptime_ms is greater than 0

### Scenario: Admin tasks filtered by role
- GIVEN a Server with tasks for "coder" and "analyst" roles
- WHEN GET /admin/tasks?role=coder is called
- THEN only coder tasks are returned

### Scenario: Task submission via Admin API
- GIVEN a running Server
- WHEN POST /tasks is called with instruction and required_role
- THEN response returns 202 Accepted with task_id
- AND the task appears in `/admin/tasks`
