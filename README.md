# 🪸 Reef

**Think together. Execute everywhere.**

> A distributed multi-agent swarm with cognitive memory — built on Raft consensus and real-time WebSocket orchestration.

[English](./README.md) · [中文](./README_zh.md) · [Architecture](./REEF_SYSTEM.md)

---

## 🧠 One-liner

Reef transforms a single AI agent into a **fault-tolerant swarm** — where multiple agents share cognitive memory, elect a leader via Raft, and execute tasks in isolated cognitive sandboxes.

---

## 🏛️ Architecture at a Glance

```
┌──────────────────────────────────────────────────────────────────┐
│                        REEF SERVER                                │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌───────────────┐   │
│  │ Scheduler│  │ Registry │  │  Queue   │  │   Admin API   │   │
│  │ + DAG    │  │ (WS map) │  │ (1000 cap)│  │  /admin/tasks │   │
│  └─────┬────┘  └────┬─────┘  └────┬─────┘  └───────┬───────┘   │
│        └────────────┼─────────────┼────────────────┘           │
│              ┌──────┴─────────────┴──────┐                     │
│              │      Raft Consensus       │                     │
│              │   (hashicorp/raft + Bolt) │                     │
│              └──────────────┬─────────────┘                     │
│                             │                                   │
│  ┌──────────────────────────┴───────────────────┐              │
│  │          CNP / WebSocket (33 msg types)      │              │
│  └──────────────────────────┬───────────────────┘              │
└─────────────────────────────┼──────────────────────────────────┘
                              │
          ┌───────────────────┼───────────────────┐
          │                   │                   │
    ┌─────▼─────┐       ┌─────▼─────┐       ┌────▼────┐
    │  Client   │       │  Client   │       │ Client  │
    │ PicoClaw  │       │ PicoClaw  │       │PicoClaw │
    │  (Agent)  │       │  (Agent)  │       │ (Agent) │
    ├───────────┤       ├───────────┤       ├─────────┤
    │ Sandbox   │       │ Sandbox   │       │ Sandbox │
    │ 4-Layer   │       │ 4-Layer   │       │ 4-Layer │
    │ Context   │       │ Context   │       │ Context │
    ├───────────┤       ├───────────┤       ├─────────┤
    │ Corrupt.  │       │ Corrupt.  │       │Corrupt. │
    │ Guard     │       │ Guard     │       │ Guard   │
    ├───────────┤       ├───────────┤       ├─────────┤
    │ Episodic  │       │ Episodic  │       │Episode. │
    │ Memory    │       │ Memory    │       │ Memory  │
    └───────────┘       └───────────┘       └─────────┘
```

---

## ⚡ Why Reef?

| Capability | Traditional Agent | Reef Swarm |
|:---|:---|:---|
| **Scale** | Single process | Multi-node with Raft consensus |
| **Fault Tolerance** | Restart loses everything | Checkpoint + auto-failover |
| **Cognitive Context** | Flat chat history | 4-layer structured memory |
| **Task Isolation** | Shared state per agent | Per-task sandbox |
| **Evolution** | Static skills | GEP gene evolution across swarm |

---

## 🧩 Core Components

### 🔷 Phase 01 — Protocol Layer
- 33 CNP message types (Swarm + Cognitive)
- JSON-serialized with type safety (`protocol.go`)

### 🔷 Phase 02 — Server
| Module | File | Lines | Test Coverage |
|---|---|---|---:|
| Registry | `pkg/reef/server/registry.go` | ~350 | 95% |
| Scheduler | `pkg/reef/server/scheduler.go` | ~400 | 90% |
| Queue | `pkg/reef/server/queue.go` | ~150 | 100% |
| Admin | `pkg/reef/server/admin.go` | ~200 | 80% |

### 🔷 Phase 03 — Client
| Module | File | Key Feature |
|---|---|---|
| Connector | `client/connector.go` | Auto-reconnect with exponential backoff + jitter |
| TaskRunner | `client/task_runner.go` | Retry, pause/resume, sandbox, memory hook |
| CNP Handler | `client/cnp_handler.go` | 16 cognitive message types |

### 🔷 Phase 07 — Raft Consensus
- **BoltStore**: BoltDB-backed log persistence
- **Transport**: HTTP-based Raft transport (TLS optional)
- **ClientConnPool**: Multi-server WebSocket pool with leader discovery
- **LeaderGate**: Leader-only task operations
- **87.6% test coverage**, 60+ tests, 100× determinism replay

### 🔷 Phase 08 — Cognitive Architecture *(PicoClaw)*

```
┌─────────────────────────────────────────┐
│        P8 Cognitive Sandbox             │
├─────────────────────────────────────────┤
│ L0 Immutable  │ System Prompt, Role,    │
│               │ Skills, Genes           │
├───────────────┼─────────────────────────┤
│ L1 Task       │ Instruction, Metadata   │
├───────────────┼─────────────────────────┤
│ L2 Working    │ Round 1 [tool] → output │
│               │ Round 2 [tool] → output │
│               │ ...                     │
├───────────────┼─────────────────────────┤
│ L3 Injections │ Genes, Episodes         │
└───────────────┴─────────────────────────┘
        │
┌───────┴───────┐
│ CorruptionGuard│  Loop / Blank / Drift detection
│ ContextWindow  │  Token budget, auto-compact
│ CheckpointMgr  │  Time + round-based snapshots
└───────────────┘
```

---

## 🚀 Quick Start

### Build

```bash
# Server
go build -o bin/reef ./cmd/reef

# Agent Runtime (picoclaw)
cd picoclaw && go build -o bin/picoclaw .
```

### Run a 3-node cluster

```bash
# Node 1 (becomes leader)
reef server --id node-1 --raft-addr :12000 --ws-addr :8765 --data-dir ./data/1

# Node 2
reef server --id node-2 --raft-addr :12001 --ws-addr :8766 --data-dir ./data/2 \
            --join ws://localhost:8765

# Node 3
reef server --id node-3 --raft-addr :12002 --ws-addr :8767 --data-dir ./data/3 \
            --join ws://localhost:8765
```

### Connect an agent

```bash
picoclaw agent --server ws://localhost:8765 --role coder --skills "go,bash"
```

### Admin

```bash
curl http://localhost:8080/admin/status
curl http://localhost:8080/admin/tasks?status=running
```

---

## 📊 Project Stats

| Metric | reef (Server) | picoclaw (Agent) |
|---|---|---|
| Go files | 28 | 71 |
| Test files | 15 | 50+ |
| Tests passed | ~90 | ~260 |
| P8 Coverage | — | 88–100% |
| Raft Coverage | 87.6% | — |
| Lines of code | ~6,500 | ~18,000 |

---

## 📁 Documentation Map

| Doc | What it covers |
|---|---|
| `REEF_SYSTEM.md` | Full technical reference (Phase 01–08) |
| `SUMMARY.md` | Per-phase execution summaries |
| `SOUL.md` | PicoClaw personality |
| `reef-code-audit-report.md` | Pre-phase audit |
| `picoclaw/ROADMAP.md` | Agent Runtime roadmap |

---

## 🏷️ License

MIT — Built on [PicoClaw](https://github.com/sipeed/picoclaw) by [Sipeed](https://sipeed.com).
