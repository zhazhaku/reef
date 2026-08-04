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

## 🦞 LHT 长时自主任务引擎

> **Long-Horizon Task Engine** — 天/周级目标驱动自主执行，三权分立，人机协同。

LHT Engine 是 Reef 的**长时自主任务子系统**，使用户提交目标后引擎自主完成「分析 → 拆解 → 规划 → 执行 → 评估 → 纠错 → 再执行」闭环，同时保证**可介入、可打断、可恢复、预算可控、绝不无限死循环**。

### 适用场景
- 完整项目搭建（如"搭建电商平台"，自动走 gsd 流程）
- 代码功能开发（如"实现用户登录"，自动走 openspec 流程）
- 研究分析报告（自动调研 → 分析 → 报告）
- 通用多阶段自主任务

### 状态机

```
                     ┌──────────┐
         ┌──────────→│ PAUSED   │←───────────┐
         │           └────┬─────┘            │
         │  pause/       │ resume           │  pause/
         │  (任意态)     │                  │  (任意态)
         │                ↓                  │
  GROUNDING → PLANNING → WAIT_APPROVAL → EXECUTING → EVALUATING
                  ↑            │                         ↓
                  │   reject   │   approve         PASS  │  FAIL(修复)
                  └────────────┘                         │
                                                        ↓
                              ┌─────────┐         REVIEWING
                              │ ESCALATED│              │
                              └────┬─────┘    PASS      │ 打回
                                   ↑                    ↓
                           升级求助          FINAL_APPROVAL ──→ COMPLETED
                                                                   ↑
  ABORTED(state terminal)                                              │
                                                                       │
  (任意态 ──stop──→ ABORTED)                                          │
```

### 核心能力

| 能力 | 说明 |
|------|------|
| **Checkpoint 持久化** | `workspace/longhorizon/{goal_id}/` 原子写 goal/plan/state/budget.json，崩溃可恢复，cron 跨天/周唤醒 |
| **预算硬上限** | 时间预算 / token 预算 / 最大迭代数 / 修复轮次 / 策略切换次数，超限暂停并请求追加 |
| **防死循环** | N=3 修复限次、M=2 策略切换、K=3 停滞检测；超限自动升级求助（ESCALATED） |
| **三方独立评审** | lht-gen 生成 / lht-eval 评估 / lht-rev 评审，三角色独立 client 实例，三权分立 |
| **能力审批门** | L1 白名单（14 技能 + 3 role client）直接可用；L2 新技能/插件/非 LHT client 需计划确认时一并批准 |
| **UI 长程面板** | SSE 实时流 + REST 控制；任务列表卡片 / 详情三栏布局（步骤+执行流+评审）/ 暂停/恢复/终止/插入要求/回答引擎提问 |
| **gsd/openspec 路由** | 目标类型自动判类 → gsd（完整项目）或 openspec（代码变更）或 research（研究）或 plan-and-execute（通用） |

### REST API 摘要

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/v2/lht/list` | GET | 任务列表 |
| `/api/v2/lht/{id}` | GET | 任务详情（含步骤、评审、工件） |
| `/api/v2/lht/{id}/logs` | GET | 分页执行日志 |
| `/api/v2/lht/{id}/pause` | POST | 暂停 |
| `/api/v2/lht/{id}/resume` | POST | 恢复 |
| `/api/v2/lht/{id}/stop` | POST | 终止 |
| `/api/v2/lht/{id}/insert` | POST | 插入新要求 |
| `/api/v2/lht/{id}/approve` | POST | 批准计划/终审 |
| `/api/v2/lht/{id}/reject` | POST | 打回计划/终审 |
| `/api/v2/lht/{id}/reply` | POST | 回答引擎提问 |
| `/api/v2/events` | SSE | `lht_state` / `lht_log` / `lht_milestone` 实时事件 |

### CLI 命令族

**消息通道** `/lht` 族（17 子命令，全含短别名）：
`new`(`n`) | `list`(`ls`) | `status`(`st`) | `plan`(`p`) | `approve`(`ok`) | `reject`(`no`) | `pause`(`pz`) | `resume`(`go`) | `stop`(`x`) | `insert`(`add`) | `budget`(`b`) | `escalate`(`esc`) | `logs`(`log`) | `artifacts`(`art`) | `review`(`rev`) | `history`(`h`) | `help`(`?`)

**服务端** `reef lht` 族：`list` | `status` | `resume` | `wake` | `gc`

> 完整命令使用指南：见 [`docs/lht-commands.md`](./docs/lht-commands.md)

### 代码位置

```
pkg/reef/lht/
├── engine.go         # Engine 主类型：任务注册、状态机循环、并发调度 (824 行)
├── state.go          # 11 状态 + 转换表 + CanTransition (87 行)
├── model.go          # Goal / Plan / Budget / TaskNode / ReviewRecord (133 行)
├── store.go          # workspace/longhorizon/ 持久化，原子写 (508 行)
├── budget.go         # 预算估算模板 + 硬上限 (238 行)
├── grounding.go      # GROUNDING 对齐问答 (247 行)
├── planning.go       # BuildPlan / DAG / 目标分类 / 流程路由 (327 行)
├── cmd_lht.go        # /lht 命令族 + reef lht CLI (342 行)
├── roles.go          # 三角色 RoleManager + YAML 加载 (196 行)
├── executor.go       # 子任务执行，调用 reef_submit_task (305 行)
├── anti_stall.go     # 修复限次 / 策略切换 / 停滞检测 (348 行)
├── notifier.go       # 关键节点通知 (200 行)
├── api.go            # /api/v2/lht/* REST handler + SSE (558 行)
├── capability.go     # L1/L2 能力审批门 (176 行)
└── *_test.go         # 15 测试文件，~287 测试函数，全部通过
```

---

## 📊 Stats

| Metric | Count |
|--------|-------|
| Go source files | 71 + LHT 14 = 85 |
| Test files | 50+ + LHT 15 = 65+ |
| Total tests | ~550 |
| LHT tests | ~287 (全部通过) |
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
