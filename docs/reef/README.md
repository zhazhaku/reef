# Reef — Distributed Multi-Agent Swarm

Reef is a distributed multi-agent orchestration system built into PicoClaw. It enables a fleet of PicoClaw nodes to collaborate as a swarm, with tasks automatically routed to the best-fit agent based on role, skills, and current load.

## 🚀 v2.0 Features

- **Priority Scheduling**: Task prioritization (1-10) with configurable match strategies
- **DAG Workflow Engine**: Parent/subtask orchestration with dependency management
- **Persistent State**: SQLite-backed task queue with automatic recovery on restart
- **Result Routing**: Bidirectional result delivery via ReplyTo context
- **Web Dashboard**: Real-time swarm monitoring with SSE event stream
- **Hermes Coordinator**: AI-powered task delegation to connected agents
- **Multi-Strategy**: LeastLoad / RoundRobin / Affinity client selection

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

# With SQLite persistence and TLS
picoclaw reef-server \
  --ws-addr :8443 --admin-addr :8444 \
  --token my-secret-token \
  --store-type sqlite --store-path /var/lib/reef/reef.db
```

Or configure via `config.json`:

```json
{
  "channels": {
    "swarm": {
      "enabled": true,
      "mode": "server",
      "ws_addr": ":8080",
      "admin_addr": ":8081",
      "token": "my-secret-token",
      "store_type": "sqlite",
      "store_path": "/var/lib/reef/reef.db",
      "notifications": [
        { "type": "slack", "webhook_url": "https://hooks.slack.com/services/..." },
        { "type": "feishu", "hook_url": "https://open.feishu.cn/open-apis/bot/v2/hook/..." }
      ]
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

### 5. Web UI Dashboard

Open `http://server-ip:8081/ui/` in your browser for a real-time dashboard with:

- **Overview** — Server status, connected clients, task statistics
- **Tasks** — Task list with filtering, submission form, cancellation
- **Clients** — Connected client list with role, skills, load

The UI uses Server-Sent Events (SSE) for real-time updates.

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
                      ↓                      ↓
                   Resumed → Running    Escalated (admin alert)
```

### Client State

- **Connected**: Active and accepting tasks
- **Stale**: Missed heartbeats, not accepting new tasks
- **Disconnected**: WebSocket closed, in-flight tasks paused

### Persistent Storage (v2.0)

Reef supports SQLite-backed task persistence. When enabled, non-terminal tasks survive server restarts:

- **Queued tasks** are restored and re-dispatched
- **Running tasks** are reset to Queued and re-dispatched
- **WAL mode** ensures concurrent read/write without blocking

See [Deployment — Persistent Storage](deployment.md#persistent-storage-v20) for configuration.

### TLS (v2.0)

Native TLS support for both WebSocket and Admin API:

- Server: `tls_enabled`, `tls_cert_file`, `tls_key_file`
- Client: `wss://` URLs with optional custom CA and mutual TLS

See [Deployment — TLS Configuration](deployment.md#tls-configuration-v20) for setup.

### Notifications (v2.0)

Multi-channel notification system for task escalation alerts:

| Channel | Description |
|---------|-------------|
| Webhook | Generic HTTP POST |
| Slack | Incoming Webhook with Block Kit |
| Feishu (飞书) | Interactive card messages |
| WeCom (企业微信) | Markdown messages |
| SMTP | HTML email alerts |

All channels support concurrent fan-out with fault isolation. See [Notifications](notifications.md) for configuration.

## Documentation

- [Architecture](architecture.md) — System design, component interactions, message flow
- [Deployment](deployment.md) — Docker Compose, systemd, multi-node setup, TLS, persistent storage
- [API Reference](api.md) — Admin HTTP endpoints, WebSocket protocol, Web UI API
- [Roles & Skills](roles.md) — Built-in roles, custom role definition
- [Protocol](protocol.md) — Message types, payload schemas, version compatibility
- [Notifications](notifications.md) — Multi-channel notification configuration (Slack, Feishu, WeCom, SMTP, Webhook)

## Testing

```bash
# Run all Reef tests (unit + e2e + perf)
go test ./pkg/reef/... ./test/e2e/... ./test/perf/... -v

# Run only E2E tests
go test ./test/e2e/... -v

# Run only performance baselines
go test ./test/perf/... -v -timeout 120s

# Run with race detector
go test ./test/e2e/... -race
```

See `test/e2e/` for the full E2E test suite covering 17 scenarios.
See `test/perf/` for performance baseline tests with regression detection.
