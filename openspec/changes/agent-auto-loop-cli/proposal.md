---
change: agent-auto-loop-cli
schema: spec-driven
status: research
created: 2026-07-20
updated: 2026-07-20
---

# Proposal: Agent Auto Loop Orchestrator — 服务级自动循环执行引擎

## 1. 核心问题

当前 reef/picoclaw 架构的核心缺陷：

| 问题 | 描述 | 严重度 |
|------|------|--------|
| **无自动循环** | Agent 不能自主循环执行任务序列 | P0 |
| **无客户端生命周期管理** | reef client 进程需要手动启停，无法按需弹性伸缩 | P0 |
| **无自愈能力** | task 失败或 client 断开后，没有自动恢复机制 | P0 |
| **长时任务无守护** | 长任务（>5min）执行中 client 崩溃则任务永久丢失 | P1 |
| **模式入口单一** | 只有 `/hermes` 和聊天模式，无法灵活切换 | P1 |
| **无状态可见性** | 用户无法在对话中查看当前任务队列/运行状态 | P2 |

## 2. 目标

### 核心目标
在现有 Hermes 架构 + Server Scheduler/Registry 体系之上，设计一个 **AutoLoop Orchestrator**——服务端级自动循环执行引擎，让用户通过 `/auto` CLI 触发自动工作流，系统自动管理客户端生命周期、任务调度、自愈恢复。

### 关键能力
- **[P0]** **服务端编排** — Orchestrator 运行在 Server 层，对接 Scheduler + Registry
- **[P0]** **客户端自动启停** — 根据任务需求动态 spawn/kill reef client 进程
- **[P0]** **自愈恢复** — 任务失败自动重试（排除失败 client），client 断开自动新建
- **[P0]** **自动/手动执行** — `/auto run` 自动循环、`/auto step` 手动单步
- **[P1]** **扩缩容** — 根据队列深度自动增加/减少空闲 client
- **[P1]** **长时任务守护** — 长任务专用 PersistentClient + 30s 心跳监控
- **[P1]** **CLI 统一入口** — `/auto` 命令家族控制一切
- **[P2]** **状态可见** — 实时查看任务队列、client 状态、历史记录

### 非目标
- 不改变现有的 Hermes 6 阶段工作流（作为模式之一共存）
- 不改变 AgentLoop.Run() 的事件循环结构；仅在 select 块中增加 Orchestrator Tick/Done channel 监听（向后兼容，不影响现有消息处理路径）
- 不涉及 WebSocket 协议变更
- 不涉及 LLM provider 变更

## 2.5 术语澄清：ConversationMode vs HermesMode

现有 picoclaw 有两套模式系统，必须严格区分：

| 系统 | 枚举类型 | 当前值 | 作用域 |
|------|---------|--------|--------|
| **ConversationMode** | `agent.ConversationMode` | `chat`, `hermes` | 单个 seahorse conversation 级别，运行时可通过命令切换 |
| **HermesMode** | `agent.HermesMode` | `full`, `coordinator`, `executor` | AgentLoop 进程级角色，启动时确定，运行时不可变 |

本变更新增的 `ModeManual` / `ModeAuto` **属于 ConversationMode**，扩展后完整枚举为：

| 模式 | 行为 | 适用场景 |
|------|------|----------|
| `ModeChat` | 标准 AI 对话，自由交互 | 通用聊天、问答 |
| `ModeHermes` | 6 阶段协作工作流（接收→头脑风暴→评审→调研→设计→报告） | 复杂任务分工 |
| `ModeManual` | **新增**：手动单步执行，用户逐条输入指令，执行完等待 | 调试、教学、精细控制 |
| `ModeAuto` | **新增**：自动循环执行，Orchestrator 从队列取出任务持续处理 | 批量执行、定时任务、自动化工作流 |

### 交互规则

当 ConversationMode 和 HermesMode 同时活跃时的优先级：

```
ConversationMode 控制会话级行为（自动/手动/聊天→决定消息如何入队和处理）
        ↓
HermesMode 控制该 AgentLoop 实例的能力集（全/协/执→决定可用工具和路由）
        ↓
两者正交：HermesMode=coordinator + ConversationMode=auto 意味着
"以 coordinator 能力集运行自动模式"
```

## 3. 方案概要                           │
│                     (服务端级，对接 Scheduler)                        │
│                                                                      │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌─────────┐ │
│  │ Task Planner  │  │ Client Pool  │  │   Healer     │  │ Scaler  │ │
│  │ 指令→子任务   │  │ 启停 client  │  │ 失败重试/    │  │ 扩缩容  │ │
│  │ DAG 依赖编排  │  │ 进程管理     │  │ client 恢复  │  │ 决策    │ │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └────┬────┘ │
│         │                 │                 │               │       │
│  ┌──────▼─────────────────▼─────────────────▼───────────────▼──────┐│
│  │                 Scheduler (现有)                                  ││
│  │  Submit → TryDispatch → matchClient → dispatch → wsServer.Send  ││
│  └──────────────────────────────────────────────────────────────────┘│
│                              │                                        │
│  ┌──────────────────────────▼──────────────────────────────────────┐ │
│  │                     Registry (现有)                               │ │
│  │  Register / Unregister / ScanStale / ListByRole / IsAvailable   │ │
│  └──────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
        reef client c1   reef client c2    reef client c3..N
        (executor)        (coder)           (按需启动/销毁)
```

## 4. 新增架构组件

| 组件 | 所在包 | 职责 |
|------|--------|------|
| `AutoLoopOrchestrator` | `pkg/agent/auto_orchestrator.go` | 全流程编排（plan→dispatch→monitor→heal→scale→report） |
| `ClientPool` | `pkg/agent/client_pool.go` | 客户端进程生命周期管理（spawn/kill/保活/状态） |
| `TaskPlanner` | `pkg/agent/auto_planner.go` | 用户任务拆分 + DAG 依赖管理 |
| `Healer` | `pkg/agent/auto_healer.go` | 自愈策略（retry/escalate/restart） |
| `AutoScaler` | `pkg/agent/auto_scaler.go` | 扩缩容决策（基于队列深度+空闲超时） |
| `AutoLoopStates` | `pkg/agent/auto_state.go` | 状态管理结构体，被 Orchestrator 引用（无独立业务逻辑） |

## 5. 工作流程

### 5.1 /auto run 完整流程
```
用户: /auto run "编译所有微服务并部署"

1. TaskPlanner 解析:
   → [0] 编译 user-svc      → role:coder, deps:[]
   → [1] 编译 order-svc     → role:coder, deps:[]
   → [2] 编译 payment-svc   → role:coder, deps:[]
   → [3] 部署 user-svc      → role:ops, deps:[0]
   → [4] 部署 order-svc     → role:ops, deps:[1]
   → [5] 部署 payment-svc   → role:ops, deps:[2]
   → [6] 健康检查           → role:executor, deps:[3,4,5]

2. ClientPool.EnsureClient("coder"):
   ⟶ Registry.ListByRole("coder") 为空
   ⟶ setsid /root/reef_server/reef client --role coder ...
   ⟶ 等待直到 Registry 可查到 coder client (30s timeout)
   ⟶ 返回 clientID

3. ClientPool.EnsureClient("ops"):
   ⟶ Registry 无 ops client
   ⟶ 启动 ops client...

4. Scheduler.Submit(task[0])  // 编译 user-svc
5. Scheduler.Submit(task[1])  // 编译 order-svc (并行, 无依赖冲突)
6. Scheduler.Submit(task[2])  // 编译 payment-svc

7. Healer 监控:
   ⟶ task[0] assigned to client coder-1
   ⟶ task[1] assigned to client coder-1 (load=2)
   ⟶ task[2] 排队等待 coder-1 空闲

8. 当 task[0] 完成 → task[3] 可调度
   当 task[1] 完成 → task[4] 可调度
   ...

9. 所有 7 个 Task 完成后:
   ⟶ Scaler 检查: coder-1 空闲超时 >5min → ScaleDown(kill)
   ⟶ ops-1 空闲超时 >5min → ScaleDown(kill)
   ⟶ 保留 1 个 executor client 作为默认值守

10. 返回聚合报告给用户
```

### 5.2 自愈流程
```
场景: task[0] 编译 user-svc 失败 (exit code 1)

Healer.onTaskFailed(task[0]):
  1. 判断: EscalationCount(0) < maxRetries(3)
  2. 决策: HealRetry{ExcludeClient: "coder-1"}
  3. ClientPool.EnsureClient("coder") → 启动新 coder-2
  4. Scheduler.Submit(task[0]) → 分配 coder-2
  5. 重试成功 → 继续正常流程
  6. 若 coder-2 也失败 → EscalationCount=1 → 再试
  7. 若 3 次全部失败 → HealEscalate{NotifyAdmin: true}
```

### 5.3 客户端断开自愈
```
场景: coder-1 进程崩溃，Registry.ScanStale 发现心跳超时

Healer.onClientStale("coder-1"):
  1. Registry 标记 ClientStale
  2. 查询 coder-1 正在运行的 task[s] → task[3] (部署 user-svc)
  3. Scheduler.HandleTaskPaused(task[3].ID, "client crashed")
  4. ClientPool.SpawnClient("coder") → coder-2
  5. Scheduler.HandleClientAvailable(coder-2.ID)
  6. 触发 TryDispatch → task[3] 重新分配给 coder-2
```

## 6. CLI 命令设计

| 命令 | 别名 | 功能 | 模式 |
|------|------|------|------|
| `/auto mode` | — | 切换 chat/manual/auto 模式 | 通用 |
| `/auto run <task>` | `/autotask` | 提交并自动执行一个任务（含 client 管理和自愈） | auto |
| `/auto step <task>` | `/autostep` | 手动单步执行 | manual |
| `/auto loop N <task>` | `/autoloop` | 自动循环 N 次 | auto |
| `/auto stop` | — | 停止当前执行 | 通用 |
| `/auto status` | — | 查看 orchestrator/client/queue 状态 | 通用 |
| `/auto queue` | — | 查看/管理任务队列 | 通用 |
| `/auto history [N]` | — | 查看最近 N 条执行历史 | 通用 |
| `/auto config` | — | 查看/修改 orchestrator 运行时配置 | 通用 |

## 7. 与现有架构的关系

### Hermes 模式共存
```go
const (
    ModeChat   ConversationMode = "chat"    // 已有: 自由对话
    ModeHermes ConversationMode = "hermes"  // 已有: 6 阶段协作
    ModeAuto   ConversationMode = "auto"    // 新增: Orchestrator 自动模式
    ModeManual ConversationMode = "manual"  // 新增: 手动单步模式
)
```

### AgentLoop.Run() 修改
```go
for {
    select {
    case msg := <-al.bus.InboundChan():
        // 现有消息处理...
        // 新增: ModeAuto/ModeManual 路由到 Orchestrator

    case <-al.orchestrator.Tick():  // 新增
        al.orchestrator.Poll(ctx)   // 监控队列 + client + 自愈

    case <-al.orchestrator.Done():
        al.orchestrator.OnComplete()
    }
}
```

## 8. 安全考量

### 8.1 ClientPool 子进程凭证隔离
- spawn 时不直接复制 `config.json`（含 model_list 中的 LLM API Key），改为通过环境变量 `REEF_API_KEY` 传递
- 子进程使用独立临时 `REEF_HOME`，不可访问父进程配置目录
- 临时目录使用 `defer os.RemoveAll` 确保退出时清理
- `.security.yml` 中的敏感信息（channel tokens、API keys）仅传递子进程需要的子集

### 8.2 子进程资源限制
- 每个 `ManagedClient` 进程的并发任务数固定为 Capacity（默认 3）
- `MaxClients` 上限防止进程爆炸（默认 5，可通过 /auto config 调整）
- `SpawnTimeout` (30s) 防止无限等待客户端注册

### 8.3 进程生命周期安全
- SIGTERM → 2s grace period → SIGKILL 确保子进程不会变成僵尸
- `ManagedState` 状态机防止重复 spawn/kill
- 子进程 stdout/stderr 重定向到独立日志，避免污染父进程日志
