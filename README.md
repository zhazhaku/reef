# 🪸 PicoClaw — Reef Agent Runtime

> **The cognitive engine for Reef Swarm.**
>
> Distributed multi-agent orchestration starts here.

PicoClaw is the **Agent Runtime** of the Reef distributed swarm. It provides the cognitive architecture — structured memory, corruption detection, task isolation, and episodic learning — that powers every node in a Reef cluster.

---

## 🧩 What is PicoClaw?

PicoClaw is an **ultra-lightweight personal AI agent** by [Sipeed](https://sipeed.com). In the Reef architecture, it serves as the **Client runtime** — executing tasks inside isolated sandboxes while maintaining long-term cognitive memory.

```
┌──────────────────────────────────────────┐
│              Reef Server                  │
│         (Scheduler + Raft)               │
└──────────────┬───────────────────────────┘
               │ CNP Protocol
        ┌──────┴──────┐
        ▼             ▼
┌──────────────┐  ┌──────────────┐
│   PicoClaw   │  │   PicoClaw   │
│  (Agent Node)│  │  (Agent Node)│
├──────────────┤  ├──────────────┤
│ Sandbox      │  │ Sandbox      │
│ MemorySystem │  │ MemorySystem │
│ AgentLoop    │  │ AgentLoop    │
└──────────────┘  └──────────────┘
```

---

## 🧠 Cognitive Architecture (P8)

PicoClaw implements an **8-phase cognitive architecture** with four layers of structured context:

### Four-Layer Context Model

```
┌─────────────────────────────────────────────┐
│ L0: Immutable Layer                         │
│   System prompt, role config, skills, genes │
│   NEVER compacted                           │
├─────────────────────────────────────────────┤
│ L1: Task Layer                              │
│   Instruction, metadata, tool descriptions  │
│   Compacted only on task switch             │
├─────────────────────────────────────────────┤
│ L2: Working Rounds Layer                    │
│   Round 1: [user] → [tool:exec] → [output]  │
│   Round 2: [tool:read] → [output]           │
│   ...                                       │
│   Compact: old rounds → seahorse summary    │
├─────────────────────────────────────────────┤
│ L3: Memory Injections                       │
│   [gene: "use proper error handling"]       │
│   [episode: "last time: db timeout fix"]    │
│   Evict: LRU                                │
└─────────────────────────────────────────────┘
```

### Key Components

| Component | File | Purpose |
|-----------|------|---------|
| **ContextLayers** | `pkg/agent/context_layers.go` | Four-layer structured context |
| **ContextWindow** | `pkg/agent/context_window.go` | Token budget + auto-compact |
| **CorruptionGuard** | `pkg/agent/corruption_guard.go` | Loop/Blank/Drift detection |
| **TaskSandbox** | `pkg/agent/sandbox.go` | Isolated per-task workspace |
| **CheckpointManager** | `pkg/agent/checkpoint.go` | Time + round-based snapshots |
| **MemorySystem** | `pkg/memory/` | Episodic + semantic memory |
| **AgentLoop** | `pkg/agent/agent.go` | Main execution pipeline |

---

## 🔗 Reef Integration

PicoClaw connects to Reef Server via **CNP (Cognitive Network Protocol)** over WebSocket.

### Bridge Interfaces

```
reef/client.Sandbox          ←── ReefSandboxFactory ──→ agent.TaskSandbox
reef/client.MemoryRecorder   ←── ReefMemoryRecorder ──→ memory.EpisodicStore
reef/client.ContextManager   ←── CNPContextManager ───→ agent.ContextLayers
```

### Files

| Bridge | File |
|--------|------|
| Sandbox Bridge | `pkg/agent/reef_sandbox.go` |
| Memory Bridge | `pkg/agent/reef_memory_recorder.go` |
| Context Bridge | `pkg/agent/context_cnp.go` |

---

## 🚀 Quick Start

```bash
# Build
go build -o bin/picoclaw .

# Run as Reef Client
picoclaw agent --server ws://reef-server:8765 --role coder --skills "go,bash"
```

---

## 📊 Stats

| Metric | Count |
|--------|-------|
| Go source files | 71 |
| Test files | 50+ |
| Total tests | ~260 |
| P8 cognitive tests | 94 (all pass) |
| Coverage | 88–100% |

---

## 📄 Documentation

- [Architecture](../REEF_SYSTEM.md) — Full Reef technical reference
- [ROADMAP](./ROADMAP.md) — Development roadmap
- [CHANGELOG](./CHANGELOG.md) — Release history
- [CONTRIBUTING](./CONTRIBUTING.md) — Contribution guide

---

## 🏷️ License

MIT — Part of the Reef project. Original PicoClaw license retained.
