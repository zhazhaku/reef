# Reef Protocol Specification

Reef uses a versioned JSON message protocol over WebSocket. This document defines all message types, payload schemas, and version compatibility rules.

## Version

Current protocol version: **`1.0.0`**

Version format follows [SemVer](https://semver.org/):
- Major: Breaking protocol changes
- Minor: Backward-compatible additions
- Patch: Clarifications, no behavioral changes

## Message Envelope

Every message uses this envelope:

```json
{
  "msg_type": "register",
  "task_id": "",
  "version": "1.0.0",
  "timestamp": 1714195200000,
  "payload": { }
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `msg_type` | string | Yes | Message type (see table below) |
| `task_id` | string | No | Associated task ID. Empty for system messages. |
| `version` | string | Yes | Protocol version |
| `timestamp` | int64 | Yes | Unix timestamp in milliseconds |
| `payload` | object | Yes | Type-specific payload |

## Message Types

### Client → Server

| Type | Direction | Description |
|------|-----------|-------------|
| `register` | C→S | Client registration and capability advertisement |
| `heartbeat` | C→S | Keep-alive ping |
| `task_progress` | C→S | Task execution progress update |
| `task_completed` | C→S | Task finished successfully |
| `task_failed` | C→S | Task failed with error details |
| `control_ack` | C→S | Control message acknowledgment |

### Server → Client

| Type | Direction | Description |
|------|-----------|-------------|
| `register_ack` | S→C | Registration accepted |
| `register_nack` | S→C | Registration rejected |
| `task_dispatch` | S→C | Assign task to client |
| `cancel` | S→C | Cancel running task |
| `pause` | S→C | Pause running task |
| `resume` | S→C | Resume paused task |

## Payload Schemas

### RegisterPayload (C→S)

```json
{
  "protocol_version": "1.0.0",
  "client_id": "coder-node-1",
  "role": "coder",
  "skills": ["github", "write_file"],
  "providers": ["openai"],
  "capacity": 3,
  "timestamp": 1714195200000
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `protocol_version` | string | Yes | Must match server's expected version |
| `client_id` | string | Yes | Unique client identifier |
| `role` | string | Yes | Agent role |
| `skills` | array | No | Supported skills |
| `providers` | array | No | Available LLM providers |
| `capacity` | int | Yes | Max concurrent tasks (≥1) |
| `timestamp` | int64 | Yes | Client timestamp |

---

### RegisterAckPayload (S→C)

```json
{
  "client_id": "coder-node-1",
  "server_time": 1714195200000
}
```

---

### RegisterNackPayload (S→C)

```json
{
  "reason": "protocol version mismatch: expected 1.0.0, got 0.9.0"
}
```

---

### HeartbeatPayload (C→S)

```json
{
  "timestamp": 1714195200000
}
```

---

### TaskDispatchPayload (S→C)

```json
{
  "task_id": "task-1-abc123",
  "instruction": "Write a unit test for the auth module",
  "required_role": "coder",
  "required_skills": ["github"],
  "context": { "repo": "myorg/backend" },
  "timestamp": 1714195200000
}
```

| Field | Type | Description |
|-------|------|-------------|
| `task_id` | string | Unique task identifier |
| `instruction` | string | Task description for the agent |
| `required_role` | string | Role that should execute |
| `required_skills` | array | Skills needed |
| `context` | object | Optional context data (key-value) |
| `timestamp` | int64 | Dispatch time |

---

### TaskProgressPayload (C→S)

```json
{
  "task_id": "task-1-abc123",
  "status": "running",
  "progress_percent": 50,
  "message": "Writing test cases for login flow",
  "timestamp": 1714195200000
}
```

| Field | Type | Description |
|-------|------|-------------|
| `task_id` | string | Task identifier |
| `status` | string | `started`, `running`, or `paused` |
| `progress_percent` | int | 0-100 completion estimate |
| `message` | string | Human-readable status message |

---

### TaskCompletedPayload (C→S)

```json
{
  "task_id": "task-1-abc123",
  "result": {
    "text": "Test file created at auth_test.go",
    "files_created": ["auth_test.go"]
  },
  "execution_time_ms": 15000,
  "timestamp": 1714195200000
}
```

| Field | Type | Description |
|-------|------|-------------|
| `task_id` | string | Task identifier |
| `result` | object | Result data (arbitrary JSON) |
| `execution_time_ms` | int | Time spent executing |

---

### TaskFailedPayload (C→S)

```json
{
  "task_id": "task-1-abc123",
  "error_type": "execution_error",
  "error_message": "Compilation failed: undefined variable 'auth'",
  "error_detail": "Full stack trace or context...",
  "attempt_history": [
    {
      "attempt_number": 1,
      "started_at": 1714195100000,
      "ended_at": 1714195150000,
      "status": "failed",
      "error_message": "Compilation failed",
      "client_id": "coder-node-1"
    }
  ],
  "timestamp": 1714195200000
}
```

| Field | Type | Description |
|-------|------|-------------|
| `task_id` | string | Task identifier |
| `error_type` | string | `execution_error`, `cancelled`, `timeout`, `escalated` |
| `error_message` | string | Human-readable error |
| `error_detail` | string | Detailed error context |
| `attempt_history` | array | Previous attempt records |

**AttemptRecord:**

| Field | Type | Description |
|-------|------|-------------|
| `attempt_number` | int | 1-based attempt number |
| `started_at` | int64 | Start time (Unix ms) |
| `ended_at` | int64 | End time (Unix ms) |
| `status` | string | `failed` or `completed` |
| `error_message` | string | Error message (if failed) |
| `client_id` | string | Client that attempted |

---

### ControlPayload (S→C)

Used for `cancel`, `pause`, and `resume` messages.

```json
{
  "control_type": "cancel",
  "task_id": "task-1-abc123"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `control_type` | string | `cancel`, `pause`, or `resume` |
| `task_id` | string | Target task |

---

### ControlAckPayload (C→S)

```json
{
  "control_type": "cancel",
  "task_id": "task-1-abc123",
  "timestamp": 1714195200000
}
```

## Connection Lifecycle

```
Client                          Server
  │                                │
  │── WebSocket Upgrade ──────────►│
  │   + x-reef-token header        │
  │                                │
  │── register ───────────────────►│
  │                                │
  │◄── register_ack / register_nack│
  │                                │
  │◄══════════════════════════════►│  (normal operation)
  │   heartbeat (every N seconds)  │
  │   task_dispatch                │
  │   task_progress / completed    │
  │                                │
  │── close / disconnect ─────────►│
  │                                │
```

**Handshake timeout**: 5 seconds to send `register` after WebSocket upgrade.

**Heartbeat**: Clients should send `heartbeat` every 10 seconds. Server marks clients stale after 30 seconds without heartbeat.

## Version Compatibility

### Server Rules

- Server accepts clients with matching major version and equal or lower minor version
- Example: Server `1.0.0` accepts clients `1.0.0`, `1.0.1`, but rejects `0.9.0` or `2.0.0`

### Client Rules

- Clients should report their protocol version in `register`
- On `register_nack` with version mismatch, clients may attempt to reconnect with a fallback version

## Control Message Buffering

When a client disconnects while it has pending control messages (`cancel`, `pause`, `resume`), the Server buffers these messages. When the client reconnects and re-registers with the same `client_id`, buffered controls are flushed before new messages.

This ensures that:
- A `cancel` sent just before disconnect is not lost
- A `pause` followed by reconnect is still applied

## Error Handling

### WebSocket Errors

| Scenario | Behavior |
|----------|----------|
| Invalid token | HTTP 401, connection rejected |
| Missing register | Connection closed after 5s timeout |
| Invalid message type | Logged and ignored |
| Malformed JSON | Logged and ignored |
| Client disconnect | Tasks paused, client marked disconnected |

### Task Errors

| Scenario | Behavior |
|----------|----------|
| Task dispatch fails | Task requeued, scheduler retries |
| Client reports failure | Retry with another client, or escalate |
| Max escalations reached | Task marked `Escalated`, admin notified |
| Task timeout | Client should abort; server marks failed |
