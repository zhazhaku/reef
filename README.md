# 🪸 Reef

**Distributed Multi-Agent Swarm Orchestration System**

> Built on [PicoClaw](https://github.com/sipeed/picoclaw) — the ultra-lightweight personal AI agent by [Sipeed](https://sipeed.com).
> Reef extends PicoClaw into a distributed swarm architecture with multi-agent coordination,
> priority scheduling, DAG workflow execution, and cross-channel task routing.

[中文文档](./README_zh.md)

---

## 💡 What is Reef?

Reef is a **distributed multi-agent swarm orchestration system** written in Go. It transforms PicoClaw's single-agent architecture into a **coordinated swarm** where:

- A **Reef Server** (Hermes Coordinator) receives tasks and delegates them to available worker nodes
- **Reef Clients** register their capabilities (role, skills, capacity) and execute tasks
- **Priority scheduling** with pluggable strategies (least-load, skill-match, round-robin)
- **DAG Engine** for complex multi-step workflows with dependency tracking
- **Cross-channel task routing** — tasks submitted via Telegram/Feishu/etc. get results back to the same channel
- **Web UI** for real-time swarm monitoring

## ✨ Features

- 🌊 **Swarm Orchestration** — Server + Client nodes with WebSocket protocol
- 🧠 **Hermes Role Delegation** — Coordinator / Executor / Full capability models
- 📊 **Priority-Based Scheduling** — Pluggable MatchStrategy with least-load default
- 🔗 **DAG Workflow Engine** — Sub-tasks, dependencies, aggregation
- 🔄 **Cross-Channel ReplyTo** — Tasks route results back to source channels
- 🖥️ **Web Dashboard** — Real-time stats, task table, client monitoring, SSE events
- ⚡ **Ultra-Lightweight** — Inherits PicoClaw's <10MB footprint
- 🔌 **Persistent Storage** — SQLite-backed task store with in-memory fallback

## 🏗️ Architecture

```
┌─────────────────────────────────────────────┐
│                 Reef Server                   │
│  ┌─────────┐  ┌──────────┐  ┌────────────┐  │
│  │Priority │→│Scheduler  │→│  Registry   │  │
│  │ Queue   │  │           │  │  (Clients)  │  │
│  └─────────┘  └─────┬─────┘  └──────┬─────┘  │
│                     │               │        │
│              ┌──────▼──────┐        │        │
│              │  DAG Engine │        │        │
│              └──────┬──────┘        │        │
│                     │               │        │
│              ┌──────▼───────────────▼───┐    │
│              │     WebSocket Protocol    │    │
│              └──────┬───────────────────┘    │
└─────────────────────┼────────────────────────┘
                      │
        ┌─────────────┼─────────────┐
        │             │             │
   ┌────▼────┐   ┌────▼────┐   ┌────▼────┐
   │ Client  │   │ Client  │   │ Client  │
   │executor │   │analyst  │   │  full   │
   │ python  │   │  sql    │   │   all   │
   └─────────┘   └─────────┘   └─────────┘
```

## 📦 Quick Start

### Install

```bash
go install github.com/zhazhaku/reef/cmd/reef@latest
```

### Start Server

```bash
reef server
```

### Start Client

```bash
reef client --server ws://localhost:8765 --role executor --skills python,bash
```

### Web Dashboard

Open `http://localhost:8080/reef/overview` in your browser.

## 📊 Reef Scheduler v2

The scheduler has been significantly upgraded:

| Capability | Description |
|------------|-------------|
| Priority Queue | Tasks prioritized by urgency (1-10) |
| MatchStrategy | Pluggable client selection (least-load, skill-match) |
| DAG Engine | Sub-task creation, dependency tracking, auto-unblock |
| ReplyTo | Cross-channel result routing |
| Persistence | SQLite-backed task store |
| SSE Events | Real-time Web UI updates |

See [reef-scheduler-v2 design docs](./openspec/changes/reef-scheduler-v2/) for details.

## 📁 Project Structure

```
reef/
├── cmd/reef/          # CLI binary
├── pkg/
│   ├── reef/          # Swarm core (Task, Protocol, Bridge)
│   │   ├── server/    # Scheduler, DAG, Queue, Registry
│   │   └── client/    # Client connector & task runner
│   ├── agent/         # Hermes agent (Coordinator/Executor)
│   ├── gateway/       # Channel gateway integration
│   └── ...
├── openspec/          # Design docs & specifications
├── web/
│   ├── backend/       # Go API server
│   └── frontend/      # React + TypeScript UI
└── docker/            # Docker & Compose configs
```

## 👥 Contributing

Contributions are welcome! Check the [OpenSpec proposals](./openspec/) for current design decisions.

## 📄 License

MIT — Based on PicoClaw. Original PicoClaw license retained.

---

## 🙏 Credits

**Reef** is built upon **[PicoClaw](https://github.com/sipeed/picoclaw)**, the ultra-lightweight personal AI agent by **[Sipeed](https://sipeed.com)**.

PicoClaw provided the foundation: Go-native agent architecture, multi-channel messaging, and tool system. Reef extends this foundation into a distributed swarm orchestration platform.

We are deeply grateful to the PicoClaw community and maintainers for their excellent work.
