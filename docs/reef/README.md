# Reef — Distributed Multi-Agent Swarm

Reef is a distributed multi-agent orchestration system built into PicoClaw. It enables a fleet of PicoClaw nodes to collaborate as a swarm, with tasks automatically routed to the best-fit agent based on role, skills, and current load.

## Table of Contents

- [Overview](#overview)
- [Quick Start](#quick-start)
- [Concepts](#concepts)
- [Documentation](#documentation)
- [Testing](#testing)

## Overview

Reef uses a **hub-and-spoke** topology:

- **Reef Server** (hub): Accepts tasks via HTTP Admin API, maintains a registry of connected clients, and dispatches tasks over WebSocket.
- **Reef Client** (spoke): A PicoClaw node that connects to the Server, advertises its capabilities (role + skills), and executes dispatched tasks.

```
┌─────────────────┐     WebSocket      ┌─────────────────┐
│   Reef Server   │ ◄─────────────────► │  Reef Client A  │  role: coder
│   :8080 / :8081 │                     │   (PicoClaw)    │  skills: [github]
│                 │ ◄─────────────────► ├─────────────────┤
│  Admin API      │                     │  Reef Client B  │  role: analyst
│  /admin/status  │ ◄─────────────────► │   (PicoClaw)    │  skills: [web_fetch]
│  /admin/tasks   │                     └─────────────────┘
│  /tasks         │
└─────────────────┘
```

## Quick Start

### 1. Start a Reef Server

```bash
# Using the CLI command
picoclaw reef-server --ws-addr :8080 --admin-addr :8081 --token my-secret-token
```

Or configure via `config.json`:

```json
{
  "channels": {
    "swarm": {
      "enabled": true,
      "mode": "server",
      "server_url": "",
      "ws_addr": ":8080",
      "admin_addr": ":8081",
      "token": "my-secret-token"
    }
  }
}
```

### 2. Connect a Client Node

On another PicoClaw instance:

```json
{
  "channels": {
    "swarm": {
      "enabled": true,
      "mode": "client",
      "server_url": "ws://server-ip:8080",
      "role": "coder",
      "skills": ["github", "write_file"],
      "token": "my-secret-token"
    }
  }
}
```

### 3. Submit a Task

```bash
curl -X POST http://server-ip:8081/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "instruction": "Write a unit test for the auth module",
    "required_role": "coder",
    "required_skills": ["github"],
    "max_retries": 2
  }'
```

Response:

```json
{
  "task_id": "task-1-abc123",
  "status": "Running"
}
```

### 4. Check Status

```bash
curl http://server-ip:8081/admin/status | jq .
curl "http://server-ip:8081/admin/tasks?role=coder" | jq .
```

## Concepts

### Role

A role defines the type of agent. Built-in roles:

| Role | Description | Default Skills |
|------|-------------|----------------|
| `coder` | Software development | `github`, `write_file`, `exec` |
| `analyst` | Data analysis & research | `web_fetch`, `web_search` |
| `tester` | QA & testing | `exec`, `write_file` |

You can define custom roles in `skills/roles/<role>.yaml`.

### Task Lifecycle

```
Created → Queued → Assigned → Running → Completed
                      ↓           ↓
                   Paused     Failed → (Retry/Escalate)
                      ↓
                   Resumed → Running
```

### Client State

- **Connected**: Active and accepting tasks
- **Stale**: Missed heartbeats, not accepting new tasks
- **Disconnected**: WebSocket closed, in-flight tasks paused

## Documentation

- [Architecture](architecture.md) — System design, component interactions, message flow
- [Deployment](deployment.md) — Docker Compose, systemd, multi-node setup
- [API Reference](api.md) — Admin HTTP endpoints and WebSocket protocol
- [Roles & Skills](roles.md) — Built-in roles, custom role definition
- [Protocol](protocol.md) — Message types, payload schemas, version compatibility

## Testing

```bash
# Run all Reef tests (unit + e2e)
go test ./pkg/reef/... ./pkg/channels/swarm/... ./test/e2e/... -v

# Run only E2E tests
go test ./test/e2e/... -v

# Run with race detector
go test ./test/e2e/... -race
```

See `test/e2e/` for the full E2E test suite covering 17 scenarios.
