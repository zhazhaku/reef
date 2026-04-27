# Reef

A distributed multi-agent orchestration system extending PicoClaw into a hub-and-spoke topology over WebSocket.

## What This Is

Reef turns PicoClaw from a single-node AI agent into a **swarm of role-based agents** coordinated by a central Server. Each Client node runs a PicoClaw instance with a static role (e.g., coder, analyst, tester), loads a tailored skill subset, connects to the Server over WebSocket, and executes tasks dispatched by the Server's scheduler.

The Server maintains a live capability registry of all connected Clients, matches incoming tasks to the best-fit agent based on role and skill availability, and manages the full task lifecycle: dispatch, progress tracking, mid-task cancellation/pause, failure retry, and escalation.

## Why Build This

PicoClaw is an ultra-lightweight Go agent framework (~10MB RAM) with a powerful internal architecture: EventBus, skill registry, multi-provider LLM routing, and channel abstraction. Reef extends these primitives into a distributed layer **without replacing them** — the same AgentLoop, MessageBus, and Skills system now operates across multiple nodes.

Use cases:
- **Code review pipeline**: A `coder` agent writes code, a `reviewer` agent reviews it, a `tester` agent runs tests — all coordinated by Reef Server.
- **Multi-domain analysis**: An `analyst` agent processes data while a `visualizer` agent generates charts, parallelized across nodes.
- **CI/CD integration**: Reef Clients deployed as GitHub Actions runners or local build agents, scheduled by a central Server.

## Constraints

- Must stay within PicoClaw's existing architecture: reuse `pkg/bus`, `pkg/skills`, `pkg/channels`, `pkg/agent`, `pkg/providers`.
- WebSocket transport only (no gRPC, no HTTP polling).
- Go 1.25+, ARM64 compatible (targets Sipeed boards and edge devices).
- Single binary with `--mode=server` or `--mode=client` runtime switch.
- Client nodes must work offline after initial task receipt (WebSocket drop = pause, not fail).

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Fork PicoClaw (Path B) | Need to extend AgentLoop with TaskContext, add SwarmChannel, and modify main entry for `--mode` | — In Progress |
| Single binary, dual mode | Simplifies deployment: same artifact runs as Server or Client | — Pending |
| Static roles, not dynamic | Reduces complexity; role maps to skill subset + system prompt at startup | — Pending |
| Server owns task state machine | Client is stateless except for in-flight task; all retry/escalation logic centralized | — Pending |
| gorilla/websocket for transport | Already a PicoClaw dependency; no new external deps | — Pending |

## Requirements

### Validated
(None yet — ship to validate)

### Active
- [ ] Reef Server maintains a live registry of connected Clients with role, skills, capacity
- [ ] Reef Client registers on startup and sends periodic heartbeats
- [ ] Server schedules tasks to Clients based on role + skill match
- [ ] Client receives task, executes via PicoClaw AgentLoop, reports progress
- [ ] Server supports mid-task cancellation and pause/resume
- [ ] Client retries locally on execution errors (configurable attempts)
- [ ] After exhausting local retries, Client escalates to Server for reassign/terminate/intervene
- [ ] Client loads role-specific skill subset from built-in toolbox
- [ ] WebSocket reconnection with exponential backoff on connection drop
- [ ] Server exposes basic HTTP admin endpoint for Client status and task queue overview

### Out of Scope
- Dynamic role discovery / auto-skill-learning — roles are static config
- Client-to-Client direct communication — all routing through Server hub
- Multi-Server federation / sharding — single Server instance only
- Persistent task queue with disk-backed recovery — in-memory for v1
- Web UI dashboard — admin endpoint returns JSON only
- Authentication beyond shared token — simple `x-reef-token` header for v1

---
*Last updated: 2026-04-27 after initialization*
