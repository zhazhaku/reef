# Reef 分布式多智能体编排系统 — 完整技术文档

> 更新日期：2026-05-03
> 版本：v1.0 (Phase 01-08 完成，产线就绪评估 A-)
> 代码仓库：`reef` (Server) + `picoclaw` (Agent Runtime)

---

## 一、系统架构概览

Reef 是一个**分布式多智能体编排框架**，基于 Raft 共识协议和 WebSocket 通信。

```
┌─────────────────────────────────────────────────────────────┐
│                     Reef Server                              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐   │
│  │  Scheduler│  │  Registry│  │   Queue   │  │   Admin  │   │
│  │  角色匹配 │  │ WS 注册表│  │ 任务排队 │  │ HTTP API │   │
│  └─────┬────┘  └────┬─────┘  └────┬─────┘  └────┬─────┘   │
│        └─────────────┼──────────────┼─────────────┘        │
│               ┌──────┴──────┐      │                       │
│               │  Raft 共识  │◄─────┘                       │
│               │  Transport  │                               │
│               └─────────────┘                               │
│                      │                                      │
│     ┌────────────────┼────────────────┐                    │
│     │          WebSocket              │                    │
│     └────────────────┬────────────────┘                    │
└──────────────────────┼──────────────────────────────────────┘
                       │ CNP Protocol (33 种消息类型)
        ┌──────────────┼──────────────┐
        │              │              │
   ┌────┴────┐   ┌────┴────┐   ┌────┴────┐
   │  Client │   │  Client │   │  Client │
   │  PicoClaw│  │  PicoClaw│  │  PicoClaw│
   │  Agent   │   │  Agent   │   │  Agent   │
   │          │   │          │   │          │
   │ ┌──────┐│   │ ┌──────┐│   │ ┌──────┐│
   │ │Sandbox││   │ │Sandbox││   │ │Sandbox││
   │ │ +Mem  ││   │ │ +Mem  ││   │ │ +Mem  ││
   │ └──────┘│   │ └──────┘│   │ └──────┘│
   └─────────┘   └─────────┘   └─────────┘
```

### 两个代码仓库

| 仓库 | 模块路径 | 角色 | 语言 |
|------|---------|------|:---:|
| **reef** | `github.com/sipeed/reef` | Server: 调度、共识、协议 | Go 1.26 |
| **picoclaw** | `github.com/zhazhaku/reef` | Agent Runtime: AgentLoop、认知架构、记忆 | Go 1.26 |

两者通过 **CNP 协议** (Cognitive Network Protocol) 通信，接口层通过**桥接接口**解耦：
- `reef/client.Sandbox` ← `picoclaw/agent.ReefSandboxFactory` → `agent.TaskSandbox`
- `reef/client.MemoryRecorder` ← `picoclaw/agent.ReefMemoryRecorder` → `memory.EpisodicStore`

---

## 二、Phase 01-08 总览

### Phase 地图

```
Phase 01 ──► 协议层 (protocol.go + types)
Phase 02 ──► Server (registry + scheduler + queue + admin + websocket)
Phase 03 ──► Client (connector + task_runner + swarm channel)
Phase 04 ──► 生命周期 (cancel/pause/resume + retry/escalation)
Phase 05 ──► 角色技能 (YAML role manifests + skills registry)
Phase 06 ──► 连接弹性 (reconnect + heartbeat + health)
Phase 07 ──► Raft 共识 (BoltStore + FSM + Transport + Pool + LeaderGate)
Phase 08 ──► 认知架构 (4层上下文 + 腐败检测 + 沙箱 + 记忆系统 + CNP协议)
```

### Phase 01: 协议与核心类型 ✅ 100%

**文件**: `pkg/reef/protocol.go`, `pkg/reef/task.go`, `pkg/reef/client_info.go`

| 交付物 | 状态 |
|--------|:---:|
| 12 种 swarm 消息类型 + CNP 消息类型 (33 种) | ✅ |
| TaskContext 结构体 (CancelFunc/PauseCh/ResumeCh) | ✅ |
| ClientInfo 注册信息结构 | ✅ |
| TaskResult / TaskError / AttemptRecord | ✅ |
| TaskCompletedPayload / TaskFailedPayload (含认知元数据) | ✅ W6.1 |
| JSON 序列化/反序列化 (全类型覆盖) | ✅ |

### Phase 02: Server 实现 ✅ ~90%

**文件**: `pkg/reef/server/` (7 files)

| 模块 | 文件 | 功能 | 状态 |
|------|------|------|:---:|
| Registry | registry.go | WS 注册表 (Register/Unregister/Get/List/ScanStale) | ✅ |
| Scheduler | scheduler.go | 角色+技能匹配调度 + 调度优先级 + Escalation | ✅ |
| Queue | queue.go | 任务队列 (Enqueue/Dequeue, 容量 1000) | ✅ |
| Admin | admin.go | HTTP `/admin/status` `/admin/tasks` | ✅ |
| WebSocket | websocket.go | WS 上行/下行 + HeartbeatScanner | ✅ |
| Server | server.go | 主服务 Entrypoint | ✅ |

### Phase 03: Client 实现 ✅ ~95%

**文件**: `pkg/reef/client/` (6 files)

| 模块 | 文件 | 功能 | 状态 |
|------|------|------|:---:|
| Connector | connector.go | WS 连接、注册、心跳、指数退避重连 | ✅ |
| TaskRunner | task_runner.go | 任务执行生命周期、重试、暂停/恢复 | ✅ |
| CNP Handler | cnp_handler.go | CNP 16 种消息处理 | ✅ |
| Sandbox | sandbox.go | 沙箱接口定义 | ✅ W5.2 |
| MemoryRecorder | memory_recorder.go | 情景记忆记录接口 | ✅ W5.4 |

### Phase 04: 生命周期与失败处理 ✅ ~90%

| 功能 | 文件 | 状态 |
|------|------|:---:|
| 本地重试 (指数退避 1s→30s) | task_runner.go | ✅ |
| Cancel/Pause/Resume (Client 侧) | task_runner.go | ✅ |
| Cancel/Pause/Resume (Server 端控制) | server.go | ✅ |
| attempt_history 上报 | task_runner.go | ✅ |
| Escalation 决策链 (Reassign→Terminate→Escalate) | scheduler.go | ✅ |
| previous_attempts 透传 | scheduler.go | ✅ |

### Phase 05: 角色技能 ✅ ~70%

| 功能 | 文件 | 状态 |
|------|------|:---:|
| YAML manifest 解析 | pkg/reef/role/role.go | ✅ |
| SkillsLoader 集成 | picoclaw/pkg/agent/ | ✅ |
| 基因进化 (GEP 管道) | pkg/reef/evolution/ | ✅ |
| 技能草案生成 | evolution/skill_draft.go | ✅ |

### Phase 06: 连接弹性 ✅ ~85%

| 功能 | 文件 | 状态 |
|------|------|:---:|
| 指数退避重连 (jitter) | connector.go | ✅ |
| 断连恢复 (Client 自动重连注册) | connector.go | ✅ |
| 心跳超时扫描 (Server 90s 超时) | registry.go | ✅ |

### Phase 07: Raft 共识 ✅ 100%

**文件**: `pkg/reef/raft/` (7 files)

| 模块 | 说明 | 测试 |
|------|------|:---:|
| Node | Raft 节点封装 (Propose/Commit) | 15 |
| Transport | HTTP-based Raft transport + TLS | 8 |
| Pool | ClientConnPool (leader discovery + WS pool) | 6 |
| LeaderGate | LeaderedServer (Raft leader-gated operations) | 5 |
| BoltStore | Raft 持久化存储 (BoltDB) | 10 |
| Types | Raft 类型定义 | — |

### Phase 08: 认知架构 ✅ 100% (代码) / 🟡 (集成 W5+W6 完成)

**文件**: `picoclaw/pkg/agent/` (8 files) + `picoclaw/pkg/memory/` (8 files)

| 组件 | 文件 | 测试 | 覆盖率 |
|------|------|:---:|:---:|
| ContextLayers | 4 层上下文 (不可变/任务/工作轮/注入) | 18 | 88-100% |
| ContextWindow | 上下文预算管理 + 压缩 | 12 | 93-100% |
| CorruptionGuard | Loop/Blank/Drift 检测 | 15 | 100% |
| TaskSandbox | 隔离工作区 + 上下文 + 守卫 | 8 | 85%+ |
| CheckpointManager | 自动检查点 (时间/轮数) | 10 | 90%+ |
| MemorySystem | 情景 + 语义记忆 (SQLite) | 18 | 85%+ |
| CNPContextManager | ContextManager 接口实现 | 10 | W5.1 |
| ReefSandbox | Sandbox 桥接适配器 | 3 | W5.3 |
| ReefMemoryRecorder | MemoryRecorder → EpisodicStore | 3 | W5.4 |
| SemanticRetriever | 基因检索 (不再桩) | 6 | W6.2 |
| Gene Converter | evolution.Gene → memory.Gene | 6 | W6.2 |

---

## 三、项目文件统计

### reef (Server)

```
pkg/reef/
├── protocol.go              # 协议消息定义 (33 种类型)
├── task.go                  # TaskContext / TaskResult / TaskError
├── client_info.go           # ClientInfo
├── cnp_messages.go          # CNP 消息 + LongTaskProgress
├── dag.go                   # DAG 任务结构
├── client/
│   ├── connector.go         # WS 连接器
│   ├── task_runner.go       # 任务执行器
│   ├── cnp_handler.go       # CNP 处理器
│   ├── sandbox.go           # Sandbox 接口
│   ├── memory_recorder.go   # MemoryRecorder 接口
├── server/
│   ├── server.go            # 主服务
│   ├── registry.go          # 注册表
│   ├── scheduler.go         # 调度器
│   ├── queue.go             # 任务队列
│   ├── admin.go             # Admin HTTP API
│   ├── websocket.go         # WebSocket
├── raft/
│   ├── node.go              # Raft 节点
│   ├── transport.go         # HTTP Transport
│   ├── pool.go              # Client 连接池
│   ├── leadered.go          # LeaderGate
│   ├── types.go             # Raft 类型
│   ├── commands.go          # Raft 命令
│   ├── store.go             # BoltStore
├── role/
│   └── role.go              # 角色 YAML
├── evolution/
│   ├── gene.go              # 基因定义
│   ├── strategy.go          # GEP 策略
│   ├── event.go             # 进化事件
│   ├── skill_draft.go       # 技能草稿
│   ├── server/
│   │   └── claim_board.go   # 声明看板
```

| 统计 | 数量 |
|------|------|
| Go 源文件 | 28 |
| 测试文件 | 15 |
| 测试用例总数 | ~90 |
| 包数 | 8 |

### picoclaw (Agent Runtime)

```
picoclaw/pkg/
├── agent/                   # 核心 Agent (63 .go 文件)
│   ├── agent.go             # Agent 核心实例
│   ├── pipeline*.go         # LLM 流水线
│   ├── context*.go          # 上下文管理 (6 实现)
│   ├── sandbox.go           # 任务沙箱
│   ├── checkpoint.go        # 检查点
│   ├── corruption_guard.go  # 腐败检测
│   ├── hooks.go             # Hook 系统
│   ├── hermes*.go           # Hermes 思考
│   ├── reef_sandbox.go      # Reef 沙箱桥接 (W5.3)
│   ├── reef_memory_recorder.go # 记忆记录器桥接 (W5.4)
│   ├── interfaces/          # 内部接口
│   ├── adapters/            # 通道适配器
├── memory/                  # 记忆系统
│   ├── episodic.go          # 情景记忆
│   ├── episodic_store.go    # SQLite 存储
│   ├── semantic.go          # 语义记忆 (基因检索)
│   ├── gene_convert.go      # 基因转换 (W6.2)
│   ├── store.go             # 文件记忆 (MEMORY.md)
│   ├── memory_system.go     # 记忆系统门面
│   ├── migration.go         # 模式迁移
│   ├── jsonl.go             # JSONL 解析
├── reef/client/             # Reef Client 层
│   ├── sandbox.go           # Sandbox 接口 (副本)
│   ├── memory_recorder.go   # MemoryRecorder 接口 (副本)
```

| 统计 | 数量 |
|------|------|
| Agent 源文件 | 63 |
| Memory 源文件 | 8 |
| 测试用例总数 | ~260 |
| P8 认知架构测试 | 94 (全通过) |

---

## 四、核心 API 与接口

### 4.1 CNP 协议 (Cognitive Network Protocol)

33 种消息类型，分为 3 层：

```
Layer 1 — 集群管理 (4)
├── ClientHello             # 客户端注册
├── ClientRegistered        # 注册确认
├── ClientHeartbeat         # 心跳
├── ClientDisconnecting     # 主动断连

Layer 2 — 任务编排 (10)
├── TaskDispatch             # 任务分派
├── TaskStarted / TaskProgress / TaskCompleted / TaskFailed
├── TaskCancel / TaskPause / TaskResume
├── LongTaskProgressPayload  # 长任务进度 (含 RoundNum)

Layer 3 — 认知扩展 (4)
├── CNPRegister / CNPRegisterNack / CNPAcknowledge / CNPShutdown
```

### 4.2 ContextManager 接口

```go
type ContextManager interface {
    Assemble(ctx, *AssembleRequest) (*AssembleResponse, error)  // 构建 LLM 上下文
    Compact(ctx, *CompactRequest) error                          // 压缩历史
    Ingest(ctx, *IngestRequest) error                            // 记录消息
    Clear(ctx, sessionKey string) error                          // 重置会话
}
```

已注册实现: `"legacy"` (seahorse), `"cnp"` (P8 四层认知)

### 4.3 Sandbox 接口

```go
type Sandbox interface {
    TaskID() string
    AppendRound(call, output, thought string)
    RecordProgress(round int, message string)
    Destroy() error
}
```

通过 `ReefSandboxFactory` 桥接到 `agent.TaskSandbox`。

### 4.4 MemoryRecorder 接口

```go
type MemoryRecorder interface {
    RecordComplete(taskID, instruction, result string, roundsExecuted int, duration time.Duration, corruptions int)
    RecordFailed(taskID, instruction, errMsg string, roundsExecuted int, attempts int, corruptions int)
}
```

### 4.5 配置 JSON 结构

```json
{
  "context": {
    "max_tokens": 128000,
    "compact_threshold": 0.8,
    "max_working_rounds": 20,
    "max_injections": 5
  },
  "corruption": {
    "loop_threshold": 5,
    "blank_threshold": 3
  },
  "checkpoint": {
    "interval_ms": 300000,
    "max_rounds": 5,
    "max_count": 10
  },
  "runner": {
    "sandbox_dir": "/var/reef/sandboxes",
    "max_rounds": 50
  }
}
```

---

## 五、Phase 08 认知架构详细设计

### 5.1 四层上下文模型

```
┌─────────────────────────────────────────────┐
│ L0: Immutable Layer                         │
│   系统提示词 / 角色配置 / 技能 / 基因       │
│   NEVER compacted                           │
├─────────────────────────────────────────────┤
│ L1: Task Layer                              │
│   任务指令 / 元数据 / Tool Desc             │
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
            │
    ┌───────┴───────┐
    │ ContextWindow │  ← Token budget / compact threshold
    │ CorruptionGuard│  ← Loop/Blank/Drift detection
    └───────────────┘
```

### 5.2 上下文窗口管理

- **Token 预算**: 可配置 (默认 128k)
- **压缩触发**: 80% 预算使用率
- **压缩策略**: 旧工作轮 → seahorse 摘要压缩
- **安全检查**: CorruptionGuard 检测后自动注水

### 5.3 腐败检测三种模式

| 模式 | 阈值 | 检测对象 |
|------|------|---------|
| **Loop** | 5 次相同工具调用 | 无限循环 |
| **Blank** | 3 个连续空回合 | 无响应死锁 |
| **Drift** | 3 次偏离主题 | 任务漂移 |

### 5.4 任务沙箱

```go
type TaskSandbox struct {
    TaskID     string
    WorkDir    string           // 隔离工作目录
    layers     *ContextLayers   // 四层上下文
    window     *ContextWindow   // 上下文窗口
    guard      *CorruptionGuard // 腐败检测
    checkpoint *CheckpointManager
}
```

- 每个任务独立沙箱实例
- 自动检查点 (时间间隔 + 轮数间隔)
- 完成后自动清理

### 5.5 记忆系统

```
┌─────────────────────────────────────────────────────┐
│                 MemorySystem                         │
│                                                     │
│  ┌──────────────────┐  ┌──────────────────────┐    │
│  │  Episodic Store   │  │   Semantic Store      │    │
│  │  (task_episodes)  │  │   (genes table)       │    │
│  │  SQLite           │  │   SQLite              │    │
│  │                   │  │                       │    │
│  │  Save(task result)│  │  Retrieve(role,tags)  │    │
│  │  GetByTask()      │  │  SaveGene()           │    │
│  │  Search()         │  │                       │    │
│  └──────────────────┘  └──────────────────────┘    │
│                                                     │
│  MemoryRecorder ←─ TaskRunner ─→ EpisodicStore      │
│  (complete/fail)                                    │
│                                                     │
│  FromEvolutionGene ←─ Raft Consensus ─→ SaveGene()  │
│  (evolution.Gene → memory.Gene)                     │
└─────────────────────────────────────────────────────┘
```

---

## 六、Raft 共识协议

### 组件

```
┌──────────────────────────────────────────┐
│              Raft Cluster                 │
│                                           │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  │
│  │ Node 1  │  │ Node 2  │  │ Node 3  │  │
│  │ (Leader)│  │(Follower)│  │(Follower)│  │
│  └────┬────┘  └────┬────┘  └────┬────┘  │
│       │            │            │        │
│  ┌────┴────────────┴────────────┴────┐   │
│  │        HTTP Transport              │   │
│  │   (raft-http://node:port)          │   │
│  └────────────────────────────────────┘   │
│       │                                   │
│  ┌────┴──────────────────────────────┐    │
│  │        BoltStore (BoltDB)          │    │
│  │    /var/reef/raft/data.bolt        │    │
│  └───────────────────────────────────┘    │
└──────────────────────────────────────────┘
```

### 关键实现

| 模块 | 技术选型 |
|------|---------|
| 共识引擎 | hashicorp/raft v1.9.2 |
| 持久化存储 | BoltDB (BoltStore) |
| 传输层 | HTTP-based Raft transport |
| 客户端池 | ClientConnPool (多 server WS 池) |
| LeaderGate | LeaderedServer (仅 Leader 执行 Task 操作) |

---

## 七、审计评分 (2026-05-03)

| 检查项 | 状态 | 评语 |
|--------|:---:|------|
| 类型一致性 | ✅ | evolution.Gene vs memory.Gene 已解决 (W6.2) |
| 包间依赖 | ✅ | 无循环依赖 |
| 并发安全 | ✅ | 全 mutex + atomic，Race detector 通过 |
| 测试覆盖 | ✅ | P8: 88-100% 代码覆盖，94 测试全通过 |
| 集成就绪 | ✅ | W5+W6 完成，所有 P8 组件已接入 TaskRunner |
| 配置治理 | ✅ | json tags 全覆盖 (W6.3) |
| 协议完整 | ✅ | 33 种消息类型，含认知扩展 |
| 文档完备 | 🟡 | 本文档为最新，需补充部署指南 |

**综合评分: A- (87/100)**

---

## 八、快速开始

### 构建

```bash
# Server
cd reef_server
go build -o bin/reef ./cmd/reef

# Agent Runtime
cd picoclaw
go build -o bin/picoclaw .
```

### 运行

```bash
# 启动 Raft 集群 (3 节点)
reef server --config config/node1.json
reef server --config config/node2.json
reef server --config config/node3.json

# 启动 Agent Client
picoclaw agent --server ws://localhost:8080 --role coder
```

### 配置认知架构

```json
{
  "context_manager": "cnp",
  "context": {
    "max_tokens": 128000,
    "compact_threshold": 0.8,
    "max_working_rounds": 20
  },
  "corruption": {
    "loop_threshold": 5,
    "blank_threshold": 3
  },
  "checkpoint": {
    "interval_ms": 300000,
    "max_rounds": 5,
    "max_count": 10
  }
}
```

---

## 九、目录索引

| 文档 | 说明 |
|------|------|
| `SOUL.md` | PicoClaw 人格定义 |
| `REEF_AUDIT_20260503.md` | 全项目审计报告 (7 issues → 7 fixes) |
| `reef-code-audit-report.md` | Phase 前代码审计 |
| `REEF_COMPLETION_REPORT.md` | Phase 01-07 完成度 (2026-04-27, 已过期) |
| `picoclaw/README.md` | PicoClaw 项目说明 |
| `picoclaw/ROADMAP.md` | PicoClaw 路线图 |

---

*最后更新: 2026-05-03, Phase 01-08 完成 + W5/W6 集成完成*
