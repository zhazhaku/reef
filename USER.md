# User Guide

## For End Users

Reef is a distributed AI agent swarm. You interact with it through your familiar channels (Telegram, Feishu, etc.), and Reef routes your tasks to the most capable agent in the swarm.

### Basic Usage

1. **Send a task** — Message your agent as usual
2. **Reef schedules it** — The server finds the best agent by role and skills
3. **Agent executes** — In an isolated sandbox with cognitive memory
4. **Result returns** — Back to your original channel

### Task Types

| Type | Example | Role |
|------|---------|------|
| Code | "Write a Go HTTP handler" | `coder` |
| Analysis | "Summarize this CSV" | `analyst` |
| System | "Restart the database" | `ops` |

## For Operators

### Monitoring

```bash
# Swarm status
curl http://localhost:8080/admin/status

# Active tasks
curl http://localhost:8080/admin/tasks?status=running

# Stale clients
curl http://localhost:8080/admin/tasks?status=escalated
```

### Configuration

See `REEF_SYSTEM.md` §8 for the full configuration schema.
