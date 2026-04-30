# Reef Admin API Reference

The Reef Server exposes HTTP endpoints for task submission, status monitoring, and task inspection. All endpoints are served on the Admin port (default `:8081`).

## Base URL

```
http://<server>:8081
```

## Authentication

WebSocket connections require the `x-reef-token` header.

Admin API endpoints (`/admin/*` and `/tasks`) require a Bearer token when the server is configured with a `token`. Include it in the `Authorization` header:

```
Authorization: Bearer <token>
```

When no token is configured on the server, authentication is skipped (development mode).

**Example:**

```bash
curl -H "Authorization: Bearer my-secret-token" http://localhost:8081/admin/status
```

## Endpoints

### GET /admin/status

Returns the current system status including connected clients and uptime.

**Response:**

```json
{
  "server_version": "1.0.0",
  "start_time": 1714195200000,
  "uptime_ms": 3600000,
  "connected_clients": [
    {
      "client_id": "coder-node-1",
      "role": "coder",
      "skills": ["github", "write_file"],
      "providers": ["openai"],
      "capacity": 3,
      "current_load": 1,
      "last_heartbeat": "2026-04-27T12:00:00Z",
      "state": "connected"
    }
  ],
  "disconnected_count": 0,
  "stale_count": 0
}
```

| Field | Type | Description |
|-------|------|-------------|
| `server_version` | string | Reef protocol version |
| `start_time` | int64 | Server start time (Unix ms) |
| `uptime_ms` | int64 | Server uptime in milliseconds |
| `connected_clients` | array | List of currently connected clients |
| `disconnected_count` | int | Number of disconnected clients |
| `stale_count` | int | Number of stale clients |

**ClientInfo fields:**

| Field | Type | Description |
|-------|------|-------------|
| `client_id` | string | Unique client identifier |
| `role` | string | Agent role (e.g. `coder`) |
| `skills` | array | Supported skills |
| `providers` | array | Available LLM providers |
| `capacity` | int | Max concurrent tasks |
| `current_load` | int | Currently assigned tasks |
| `last_heartbeat` | string | ISO 8601 timestamp |
| `state` | string | `connected`, `disconnected`, or `stale` |

---

### GET /admin/tasks

Returns task summaries grouped by status.

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `role` | string | Filter by required role |
| `status` | string | Filter by task status |

**Response:**

```json
{
  "queued_tasks": [
    {
      "task_id": "task-1-abc123",
      "status": "Queued",
      "required_role": "coder",
      "required_skills": ["github"],
      "assigned_client_id": "",
      "created_at": 1714195200000
    }
  ],
  "inflight_tasks": [
    {
      "task_id": "task-2-def456",
      "status": "Running",
      "required_role": "analyst",
      "required_skills": ["web_fetch"],
      "assigned_client_id": "analyst-node-1",
      "created_at": 1714195200000,
      "started_at": 1714195260000
    }
  ],
  "completed_tasks": [
    {
      "task_id": "task-0-old789",
      "status": "Completed",
      "required_role": "coder",
      "required_skills": ["github"],
      "assigned_client_id": "coder-node-1",
      "created_at": 1714195100000,
      "started_at": 1714195150000,
      "completed_at": 1714195200000
    }
  ],
  "stats": {
    "total": 3,
    "success": 1,
    "failed": 0,
    "cancelled": 0,
    "queued": 1,
    "running": 1
  }
}
```

**TaskSummary fields:**

| Field | Type | Description |
|-------|------|-------------|
| `task_id` | string | Unique task identifier |
| `status` | string | `Queued`, `Assigned`, `Running`, `Paused`, `Completed`, `Failed`, `Cancelled`, or `Escalated` |
| `required_role` | string | Role required to execute |
| `required_skills` | array | Skills required |
| `assigned_client_id` | string | Client currently executing (if any) |
| `created_at` | int64 | Creation time (Unix ms) |
| `started_at` | int64 | Start time (Unix ms) |
| `completed_at` | int64 | Completion time (Unix ms) |

**Stats fields:**

| Field | Type | Description |
|-------|------|-------------|
| `total` | int | Total tasks known to the server |
| `success` | int | Completed successfully |
| `failed` | int | Failed (not escalated) |
| `cancelled` | int | Cancelled |
| `queued` | int | Waiting for a matching client |
| `running` | int | Currently executing |

---

### POST /tasks

Submit a new task to the Reef Server.

**Request Body:**

```json
{
  "instruction": "Write a unit test for the auth module",
  "required_role": "coder",
  "required_skills": ["github", "write_file"],
  "max_retries": 2,
  "timeout_ms": 300000,
  "model_hint": "gpt-4o"
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `instruction` | string | Yes | Task description sent to the agent |
| `required_role` | string | Yes | Role required to execute |
| `required_skills` | array | No | Skills the agent must have |
| `max_retries` | int | No | Max retry attempts (default: 2) |
| `timeout_ms` | int64 | No | Task timeout in milliseconds |
| `model_hint` | string | No | Preferred model for execution (overrides smart routing) |

**Response (202 Accepted):**

```json
{
  "task_id": "task-3-ghi789",
  "status": "Queued"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `task_id` | string | Unique identifier for the task |
| `status` | string | Initial status (`Queued` or `Running`) |

**Error Responses:**

| Status | Cause |
|--------|-------|
| 400 | Missing `instruction` or `required_role`, or invalid JSON |
| 405 | Method not allowed (only POST) |
| 500 | Queue full or internal error |

---

## WebSocket Protocol

Clients connect to the WebSocket endpoint and exchange messages using the Reef protocol.

### Connection

```
GET ws://<server>:8080/ws
Headers:
  x-reef-token: <token>   (if server is configured with token)
```

The first message sent by the client **must** be `register`. The server responds with `register_ack` or `register_nack`.

### Client → Server Messages

| Message Type | Payload | Description |
|--------------|---------|-------------|
| `register` | `RegisterPayload` | Client capabilities and identification |
| `heartbeat` | `HeartbeatPayload` | Keep-alive ping |
| `task_progress` | `TaskProgressPayload` | Report task execution progress |
| `task_completed` | `TaskCompletedPayload` | Report successful completion |
| `task_failed` | `TaskFailedPayload` | Report failure with error details |
| `control_ack` | `ControlAckPayload` | Acknowledge a control message |

### Server → Client Messages

| Message Type | Payload | Description |
|--------------|---------|-------------|
| `register_ack` | `RegisterAckPayload` | Registration accepted |
| `register_nack` | `RegisterNackPayload` | Registration rejected |
| `task_dispatch` | `TaskDispatchPayload` | Assign a task to this client |
| `cancel` | `ControlPayload` | Request task cancellation |
| `pause` | `ControlPayload` | Request task pause |
| `resume` | `ControlPayload` | Request task resume |

### Message Format

All messages are JSON with this envelope:

```json
{
  "msg_type": "task_dispatch",
  "task_id": "task-1-abc123",
  "version": "1.0.0",
  "timestamp": 1714195200000,
  "payload": { ... }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `msg_type` | string | Message type identifier |
| `task_id` | string | Associated task ID (empty for system messages) |
| `version` | string | Protocol version |
| `timestamp` | int64 | Unix timestamp (milliseconds) |
| `payload` | object | Type-specific payload |

See [Protocol](protocol.md) for complete payload schemas.

---

## Example: Complete Task Submission Flow

```bash
# 1. Check server status
curl http://localhost:8081/admin/status | jq .

# 2. Submit a task
TASK=$(curl -s -X POST http://localhost:8081/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "instruction": "Write a Go function that reverses a string",
    "required_role": "coder",
    "required_skills": ["write_file"]
  }' | jq -r '.task_id')

echo "Task ID: $TASK"

# 3. Poll until completion
while true; do
  STATUS=$(curl -s "http://localhost:8081/admin/tasks?status=Completed" | \
    jq -r --arg id "$TASK" '.completed_tasks[] | select(.task_id == $id) | .status')
  if [ "$STATUS" = "Completed" ]; then
    echo "Task completed!"
    break
  fi
  echo "Waiting..."
  sleep 1
done
```

### GET /admin/tasks?priority=N

Filter tasks by priority level (1-10). Can be combined with `?status=` and `?role=`.

**Response:**
```json
{
  "tasks": [
    {
      "id": "task-001",
      "instruction": "Search for latest AI news",
      "status": "completed",
      "priority": 8,
      "role": "executor",
      "client": "client-001",
      "created": 1714435200,
      "completed": 1714435230
    }
  ]
}
```

### Web UI Routes

Reef v2.0 includes a unified Web UI accessible at `http://<admin-addr>/reef`:
- `/reef/overview` — System status cards, connected clients, task statistics
- `/reef/tasks` — Searchable task list with status/priority filtering
- `/reef/clients` — Connected client list with role/skills/capacity
- SSE event stream at `/api/reef/events` — Real-time updates for task lifecycle

## Web UI API (v2.0)

The Web UI exposes REST APIs at the Admin port. These are used by the embedded dashboard but are also available for programmatic access.

### GET /api/v2/status

Returns server status with task statistics.

**Response:**

```json
{
  "server_version": "1.0.0",
  "start_time": 1714195200000,
  "uptime_ms": 3600000,
  "connected_clients": 3,
  "queue_depth": 5,
  "task_stats": {
    "queued": 5,
    "running": 2,
    "completed": 10,
    "failed": 1,
    "cancelled": 0,
    "escalated": 0
  }
}
```

### GET /api/v2/tasks

Returns paginated task list with optional filters.

**Query Parameters:**

| Param | Type | Default | Description |
|-------|------|---------|-------------|
| `status` | string | — | Filter by status (Queued, Running, Completed, Failed, etc.) |
| `role` | string | — | Filter by required role |
| `limit` | int | 50 | Maximum results per page |
| `offset` | int | 0 | Pagination offset |

**Response:**

```json
{
  "tasks": [
    {
      "task_id": "task-1-abc",
      "status": "Running",
      "instruction": "Write a function",
      "required_role": "coder",
      "assigned_client_id": "client-1",
      "created_at": 1714195200000,
      "started_at": 1714195201000
    }
  ],
  "total": 42,
  "limit": 50,
  "offset": 0
}
```

### GET /api/v2/clients

Returns connected clients with capabilities and load.

**Response:**

```json
{
  "clients": [
    {
      "client_id": "client-1",
      "role": "coder",
      "skills": ["github", "write_file"],
      "state": "Connected",
      "current_load": 1,
      "last_heartbeat": 1714195200000
    }
  ]
}
```

### GET /api/v2/events

Server-Sent Events (SSE) endpoint for real-time updates.

**Event Types:**

| Event | Description |
|-------|-------------|
| `task_update` | Task status changed |
| `client_update` | Client connected/disconnected |
| `stats_update` | Periodic stats refresh (every 5s) |

**Example (JavaScript):**

```javascript
const events = new EventSource('/api/v2/events');
events.addEventListener('task_update', (e) => {
  const task = JSON.parse(e.data);
  console.log(`Task ${task.task_id}: ${task.status}`);
});
events.addEventListener('stats_update', (e) => {
  const stats = JSON.parse(e.data);
  console.log(`Queue depth: ${stats.queue_depth}`);
});
```


---

## v2.0 New Endpoints

### GET /admin/tasks?priority=N

Filter tasks by priority level (1-10). Can be combined with `?status=` and `?role=`.

**Response:**
```json
{
  "tasks": [
    {
      "id": "task-001",
      "instruction": "Search for latest AI news",
      "status": "completed",
      "priority": 8,
      "role": "executor",
      "client": "client-001",
      "created": 1714435200,
      "completed": 1714435230
    }
  ]
}
```

### Web UI Routes

Reef v2.0 includes a unified Web UI accessible at `http://<admin-addr>/reef`:
- `/reef/overview` — System status cards, connected clients, task statistics
- `/reef/tasks` — Searchable task list with status/priority filtering
- `/reef/clients` — Connected client list with role/skills/capacity
- SSE event stream at `/api/reef/events` — Real-time updates for task lifecycle
