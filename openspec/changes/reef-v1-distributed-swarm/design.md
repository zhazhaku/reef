# Reef v1 技术设计文档

## 1. Architecture Overview（架构总览）

Reef 采用三层架构：Protocol 层、Server 层、Client 层。Protocol 层为共享代码，Server 和 Client 分别依赖它。

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              Reef 系统架构                                │
├─────────────────────────────────────────────────────────────────────────┤
│  Layer 3: Application                                                   │
│  ┌─────────────┐              ┌─────────────────────────────────────┐   │
│  │  cmd/reef   │              │  cmd/reef --mode=client             │   │
│  │  --mode=server              │  (PicoClaw Agent + SwarmChannel)   │   │
│  └──────┬──────┘              └─────────────────────────────────────┘   │
├─────────┼───────────────────────────────────────────────────────────────┤
│  Layer 2: Reef Core                                                     │
│  ┌──────┴────────────────────────────────────────────────────┐         │
│  │  pkg/reef/                                                  │         │
│  │  ├── protocol.go   (消息协议、枚举、常量)                      │         │
│  │  ├── task.go       (任务状态机、Task 结构体)                  │         │
│  │  ├── client.go     (ClientCapability 注册信息)               │         │
│  │  ├── server/       (Server 组件包)                           │         │
│  │  │   ├── server.go     (WebSocket acceptor、生命周期)        │         │
│  │  │   ├── registry.go   (Client 注册表)                       │         │
│  │  │   ├── scheduler.go  (任务调度器)                          │         │
│  │  │   ├── queue.go      (内存任务队列)                        │         │
│  │  │   ├── admin.go      (HTTP admin 端点)                    │         │
│  │  │   └── escalation.go (失败升级决策器)                      │         │
│  │  └── client/       (Client 组件包)                           │         │
│  │      ├── connector.go  (WebSocket 连接管理、重连)             │         │
│  │      ├── runner.go     (任务接收、注入 AgentLoop)             │         │
│  │      └── reporter.go   (进度/完成/失败报告)                   │         │
│  └────────────────────────────────────────────────────────────┘         │
├─────────────────────────────────────────────────────────────────────────┤
│  Layer 1: PicoClaw Foundation (复用层)                                   │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │
│  │  pkg/bus    │  │ pkg/channels│  │ pkg/agent   │  │ pkg/skills  │    │
│  │ MessageBus  │  │ Channel     │  │ AgentLoop   │  │ SkillsLoader│    │
│  │ In/Outbound │  │ BaseChannel │  │ EventBus    │  │ Registry    │    │
│  │             │  │ Manager     │  │ HookManager │  │             │    │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                      │
│  │pkg/providers│  │ pkg/config  │  │ pkg/logger  │                      │
│  │ LLMProvider │  │ Config      │  │ Structured  │                      │
│  │ FallbackChain│  │ Load/Save   │  │ JSON logs   │                      │
│  └─────────────┘  └─────────────┘  └─────────────┘                      │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Component Breakdown（组件拆解）

### 2.1 pkg/reef/protocol.go — 消息协议定义

**职责**：定义 Server 与 Client 之间的所有 Wire Protocol 消息类型、枚举常量、JSON 序列化逻辑。

**核心类型**：
```go
package reef

const ProtocolVersion = "reef-v1"

type MessageType string

const (
    MsgRegister      MessageType = "register"
    MsgRegistered    MessageType = "registered"
    MsgHeartbeat     MessageType = "heartbeat"
    MsgTaskDispatch  MessageType = "task_dispatch"
    MsgTaskProgress  MessageType = "task_progress"
    MsgTaskCompleted MessageType = "task_completed"
    MsgTaskFailed    MessageType = "task_failed"
    MsgTaskCancel    MessageType = "task_cancel"
    MsgTaskPause     MessageType = "task_pause"
    MsgTaskResume    MessageType = "task_resume"
    MsgError         MessageType = "error"
)

type Message struct {
    Type      MessageType     `json:"type"`
    Version   string          `json:"version"`
    Payload   json.RawMessage `json:"payload"`
    Timestamp time.Time       `json:"timestamp"`
}

// 具体 Payload 结构体（示例）
type RegisterPayload struct {
    ClientID      string   `json:"client_id"`
    Role          string   `json:"role"`
    Skills        []string `json:"skills"`
    Providers     []string `json:"providers"`
    MaxConcurrent int      `json:"max_concurrent"`
}

type TaskDispatchPayload struct {
    TaskID          string            `json:"task_id"`
    Instruction     string            `json:"instruction"`
    Context         map[string]any    `json:"context,omitempty"`
    MaxRetries      int               `json:"max_retries"`
    TimeoutSeconds  int               `json:"timeout_seconds"`
    RequiredRole    string            `json:"required_role"`
    RequiredSkills  []string          `json:"required_skills"`
    PreviousAttempts []AttemptLog    `json:"previous_attempts,omitempty"`
}

type TaskFailedPayload struct {
    TaskID        string       `json:"task_id"`
    Error         string       `json:"error"`
    ErrorType     ErrorType    `json:"error_type"`
    Attempts      int          `json:"attempts"`
    Logs          []string     `json:"logs"`
    AttemptHistory []AttemptLog `json:"attempt_history"`
}
```

**设计决策**：
- 使用 `json.RawMessage` 作为 `Payload` 类型，实现延迟解析，减少不必要的反序列化开销。
- 所有 Payload 结构体必须实现 `Validator` 接口，在 decode 后进行字段校验。
- `Version` 字段为未来协议升级预留，当前固定为 `"reef-v1"`。

### 2.2 pkg/reef/task.go — 任务状态机

**职责**：定义 Task 实体、状态机枚举、状态转换规则、状态变更事件。

```go
package reef

type TaskStatus string

const (
    TaskCreated    TaskStatus = "created"
    TaskQueued     TaskStatus = "queued"
    TaskAssigned   TaskStatus = "assigned"
    TaskRunning    TaskStatus = "running"
    TaskPaused     TaskStatus = "paused"
    TaskCompleted  TaskStatus = "completed"
    TaskFailed     TaskStatus = "failed"
    TaskCancelled  TaskStatus = "cancelled"
    TaskEscalated  TaskStatus = "escalated"
)

type Task struct {
    ID             string
    Status         TaskStatus
    Instruction    string
    Context        map[string]any
    RequiredRole   string
    RequiredSkills []string
    MaxRetries     int
    TimeoutSeconds int

    // 运行时状态（Server 维护）
    AssignedClientID string
    QueueTime        time.Time
    AssignTime       *time.Time
    StartTime        *time.Time
    EndTime          *time.Time
    Result           *TaskResult

    // 失败处理
    AttemptHistory   []AttemptLog
    EscalationDecision *EscalationDecision
}

type AttemptLog struct {
    Timestamp  time.Time `json:"timestamp"`
    Error      string    `json:"error"`
    DurationMs int       `json:"duration_ms"`
    ClientID   string    `json:"client_id"`
}
```

**状态转换规则**（代码中以 `Task.CanTransition(to TaskStatus) bool` 方法实现）：

```
Created ──► Queued ──► Assigned ──► Running ──► Completed
                              │          │
                              │          ├──► Failed ──► Escalated ──► Reassigned ──► Assigned
                              │          │                  │
                              │          ├──► Paused ◄─────┘         └──► Terminated
                              │          │      │
                              │          │      └──► Running (resume)
                              │          │
                              └──► Cancelled
```

**有效转换矩阵**：

| From \ To | Queued | Assigned | Running | Paused | Completed | Failed | Cancelled | Escalated |
|-----------|--------|----------|---------|--------|-----------|--------|-----------|-----------|
| Created   | ✓      |          |         |        |           |        | ✓         |           |
| Queued    |        | ✓        |         |        |           |        | ✓         |           |
| Assigned  | ✓      |          | ✓       |        |           |        | ✓         |           |
| Running   |        |          |         | ✓      | ✓         | ✓      | ✓         |           |
| Paused    |        |          | ✓       |        |           |        | ✓         |           |
| Failed    |        |          |         |        |           |        |           | ✓         |
| Escalated | ✓      |          |         |        |           | ✓      | ✓         |           |

### 2.3 pkg/reef/server/ — Server 组件

#### server.go — WebSocket Acceptor & 生命周期管理

```go
type Server struct {
    config      *ServerConfig
    registry    *Registry
    scheduler   *Scheduler
    queue       *TaskQueue
    escalator   *EscalationHandler
    admin       *AdminServer
    upgrader    websocket.Upgrader
    
    mu          sync.RWMutex
    clients     map[string]*clientConn // clientID -> conn
    tasks       map[string]*Task       // taskID -> task
    
    ctx         context.Context
    cancel      context.CancelFunc
    wg          sync.WaitGroup
}
```

**核心职责**：
- `ListenAndServe`：启动 HTTP 服务，升级 WebSocket 连接，启动 admin HTTP 服务。
- `handleWS`：每个连接一个 goroutine，消息路由到 `registry`、`scheduler` 或 `task manager`。
- `broadcast`：向指定 Client 发送控制消息（cancel/pause/resume）。
- `Shutdown`：优雅关闭，等待所有在飞任务完成或超时。

#### registry.go — Client 注册表

```go
type Registry struct {
    mu      sync.RWMutex
    clients map[string]*ClientInfo // clientID -> info
}

type ClientInfo struct {
    ClientID      string
    Role          string
    Skills        []string
    Providers     []string
    MaxConcurrent int
    CurrentTasks  int
    Status        ClientStatus // online / offline
    LastHeartbeat time.Time
    Conn          *clientConn  // nil when offline
}
```

**核心职责**：
- `Register`：新 Client 注册，或旧 Client 重连复用。
- `Heartbeat`：更新 `LastHeartbeat` 和 `CurrentTasks`。
- `Evict`：后台 goroutine 每 10s 扫描一次，剔除超时 Client。
- `FindCandidates`：按角色和技能过滤，返回候选 Client 列表。

**线程安全**：所有方法通过 `sync.RWMutex` 保护。`FindCandidates` 持有读锁，返回 ClientInfo 的副本切片，避免调用方持有锁。

#### scheduler.go — 任务调度器

```go
type Scheduler struct {
    registry *Registry
    queue    *TaskQueue
    server   *Server // 用于发送 dispatch 消息
}
```

**调度算法**（`Schedule(task *Task) error`）：
1. 调用 `registry.FindCandidates(task.RequiredRole, task.RequiredSkills)` 获取候选列表。
2. 过滤掉 `Status != online` 或 `CurrentTasks >= MaxConcurrent` 的候选。
3. 按可用容量降序排序，容量相同按心跳 freshness 排序。
4. 选择最优候选，调用 `server.dispatchTask(task, clientID)`。
5. 若无候选，调用 `queue.Enqueue(task)`。

**调度触发时机**：
- 新任务提交时（HTTP API 或内部调用）。
- Client 注册/心跳更新后，检查队列中是否有匹配任务。
- 任务完成/失败后，释放 Client 容量，触发队列检查。

#### queue.go — 内存任务队列

```go
type TaskQueue struct {
    mu     sync.Mutex
    tasks  []*Task
    maxSize int
}
```

**核心职责**：
- `Enqueue`：FIFO 入队，满时返回 `ErrQueueFull`。
- `Dequeue`：出队。
- `Peek`：查看队首但不移除。
- `FindMatch`：遍历队列，返回第一个匹配给定角色和技能要求的任务。

**复杂度**：
- `Enqueue` / `Dequeue`：O(1)
- `FindMatch`：O(n)，n 为队列长度。v1 中 n 最大 1000，可接受。

#### admin.go — HTTP Admin 端点

```go
type AdminServer struct {
    server *Server
    token  string
    addr   string
}
```

**端点**：
- `GET /admin/status` — 返回所有 Client 状态 JSON。
- `GET /admin/tasks` — 返回所有任务状态 JSON，支持 `?client_id=` 过滤。
- `POST /admin/tasks/:task_id/control` — Body `{ "action": "cancel|pause|resume" }`，向目标 Client 发送控制消息。

**认证中间件**：检查 Header `x-reef-token` 与配置中的共享 token 是否一致。

#### escalation.go — 失败升级决策器

```go
type EscalationHandler struct {
    server *Server
}

type EscalationDecisionType string

const (
    EscalateReassign   EscalationDecisionType = "reassign"
    EscalateTerminate  EscalationDecisionType = "terminate"
    EscalateToHuman    EscalationDecisionType = "human"
)
```

**决策逻辑**：
1. 若存在其他未尝试过该任务的候选 Client → `Reassign`。
2. 若所有候选均已尝试，或任务明确不可恢复（如 `error_type = cancelled`） → `Terminate`。
3. 若配置 `auto_escalate_to_human = true`，或重试次数超过阈值 → `EscalateToHuman`。

### 2.4 pkg/reef/client/ — Client 组件

#### connector.go — WebSocket 连接管理

```go
type Connector struct {
    config      *ClientConfig
    conn        *websocket.Conn
    mu          sync.Mutex
    status      ClientStatus // idle / ready / reconnecting / failed
    
    // 重连策略
    backoff     backoff.BackOff
    maxAttempts int
    
    // 消息泵
    sendCh      chan *Message
    recvCh      chan *Message
    
    ctx         context.Context
    cancel      context.CancelFunc
}
```

**核心职责**：
- `Connect`：建立 WebSocket 连接，发送 `register`。
- `reconnectLoop`：断连后指数退避重连。
- `Send` / `Receive`：异步消息读写，各自一个 goroutine。
- `Close`：优雅关闭连接。

**重连策略参数**：
- 初始间隔：1s
- 乘数：2
- 最大间隔：60s
- 随机抖动：±20%
- 最大尝试次数：0（无限）

#### runner.go — 任务接收与 AgentLoop 注入

```go
type TaskRunner struct {
    connector   *Connector
    agentLoop   *agent.AgentLoop
    bus         *bus.MessageBus
    
    // 在飞任务映射
    mu          sync.RWMutex
    activeTasks map[string]*ActiveTask
}

type ActiveTask struct {
    TaskID     string
    CancelFunc context.CancelFunc
    Status     reef.TaskStatus
    PauseCh    chan struct{} // 用于 pause/resume 信号
    ResumeCh   chan struct{}
}
```

**核心职责**：
- `OnTaskDispatch`：收到 `task_dispatch` 消息后，构造 `bus.InboundMessage`，设置 `Metadata["reef_task_id"]`，发布到 MessageBus。
- `wrapWithTaskContext`：在 AgentLoop 处理前，通过 Hook 拦截 `EventKindTurnStart`，将 `TaskContext` 注入到 `processOptions` 中。
- `OnTaskCancel`：查找 ActiveTask，调用 `CancelFunc()`。
- `OnTaskPause`：关闭 `PauseCh`，AgentLoop 中的 Hook 检查到暂停信号后阻塞在 `ResumeCh`。
- `OnTaskResume`：向 `ResumeCh` 发送信号，解除阻塞。

**AgentLoop 集成细节**：
Client 不修改 `pkg/agent/loop.go` 的核心逻辑，而是通过以下扩展点集成：
1. **Hook 机制**：注册一个 `EventObserver` Hook，监听 `EventKindTurnStart`。当事件触发时，检查 `EventMeta` 中是否包含 `reef_task_id`。若有，将 `task_id` 和 `cancelFunc` 存入 `turnState` 的扩展字段（通过 `sync.Map` 或 context value）。
2. **processOptions 扩展**：在 `SwarmChannel.HandleMessage` 中构造 `bus.InboundMessage` 时，`Metadata` 携带 `reef_task_id`。AgentLoop 的 `processMessage` 将 `Metadata` 透传到 `processOptions`。
3. **Outbound 拦截**：`SwarmChannel.Send` 方法实现 `channels.Channel` 接口。当收到 `bus.OutboundMessage` 时，检查 `Metadata["reef_task_id"]`。若存在，不发送到外部平台，而是转换为 `task_progress` 或 `task_completed` 消息发送到 Server。

#### reporter.go — 进度/完成/失败报告

```go
type TaskReporter struct {
    connector *Connector
}
```

**核心职责**：
- `ReportProgress`：定时或事件驱动发送 `task_progress`。
- `ReportCompleted`：AgentLoop turn 结束后发送 `task_completed`。
- `ReportFailed`：本地重试耗尽后发送 `task_failed`。

**进度报告触发时机**：
- `started`：任务注入 AgentLoop 后立即发送。
- `running`：每 30 秒或每完成一次 LLM 调用/工具执行时发送。
- `completed` / `failed` / `cancelled` / `paused`：状态变更时立即发送。

### 2.5 pkg/channels/swarm/ — SwarmChannel 实现

```go
package swarm

type SwarmChannel struct {
    *channels.BaseChannel
    runner    *client.TaskRunner
    reporter  *client.TaskReporter
}
```

**Channel 接口实现**：
- `Name()` → 返回 `"swarm"`。
- `Start(ctx)` → 启动 Connector，连接 Server。
- `Stop(ctx)` → 关闭 Connector。
- `Send(ctx, msg)` → 检查 `msg.Metadata["reef_task_id"]`。若存在，将 `msg.Content` 作为任务结果/进度通过 Reporter 上报 Server；否则忽略（或记录警告）。
- `IsAllowed(senderID)` → 始终返回 `true`（Server 发送的任务无需 allowlist 检查）。

**SwarmChannel 作为 PicoClaw 的 Channel**：
SwarmChannel 符合 PicoClaw 的 `channels.Channel` 接口，可被 `channels.Manager` 统一管理。这意味着 PicoClaw 的多 Channel 架构（如同时连接 Telegram 和 Reef Server）天然支持。

### 2.6 cmd/reef/ — 主入口双模式支持

```go
package main

func main() {
    var mode string
    flag.StringVar(&mode, "mode", "client", "Run mode: server or client")
    flag.Parse()

    switch mode {
    case "server":
        runServer()
    case "client":
        runClient()
    default:
        log.Fatal("unknown mode:", mode)
    }
}

func runServer() {
    cfg := loadServerConfig()
    srv := reefserver.NewServer(cfg)
    if err := srv.ListenAndServe(); err != nil {
        log.Fatal(err)
    }
}

func runClient() {
    cfg := loadClientConfig()
    
    // 初始化 PicoClaw 核心
    msgBus := bus.NewMessageBus()
    agentLoop := agent.NewAgentLoop(cfg.PicoClaw, msgBus, provider)
    
    // 初始化 SwarmChannel
    swarmCh := swarm.NewSwarmChannel(cfg.Swarm, msgBus)
    
    // 启动 AgentLoop 和 Channel
    go agentLoop.Run(context.Background())
    go swarmCh.Start(context.Background())
    
    // 阻塞
    select {}
}
```

---

## 3. Data Flow（数据流）

### 3.1 任务从提交到完成的完整数据流

```
阶段 0: 任务提交
─────────────────────────────────────────────────────────────
[外部系统 / Admin API]
        │
        ▼ POST /admin/tasks (或内部 API)
┌───────────────┐
│  Reef Server  │  task 进入 TaskQueue，状态 = Created → Queued
└───────┬───────┘
        │

阶段 1: 调度与派发
─────────────────────────────────────────────────────────────
        │  Scheduler.FindMatch()
        ▼
┌───────────────┐
│  Registry     │  查询候选 Client (role + skill 匹配)
└───────┬───────┘
        │ 选中 Client-X
        ▼ ws.Send(task_dispatch)
┌───────────────┐
│  Client-X     │  收到 task_dispatch
│  SwarmChannel │  构造 bus.InboundMessage
│  MessageBus   │  PublishInbound(msg)
└───────┬───────┘
        │

阶段 2: 本地执行
─────────────────────────────────────────────────────────────
        │
        ▼
┌───────────────┐
│  AgentLoop    │  从 InboundChan 消费消息
│  processMessage│  构造 processOptions (携带 task_id)
│  runAgentLoop │  执行 LLM 调用、工具执行
│  Hook         │  TurnStart 事件注入 TaskContext
└───────┬───────┘
        │
        │ 每 30s / 每步骤完成
        ▼ ws.Send(task_progress)
┌───────────────┐
│  Reef Server  │  更新 task 状态 = Running
└───────┬───────┘
        │

阶段 3: 完成或失败
─────────────────────────────────────────────────────────────
        │ 执行完成
        ▼
┌───────────────┐     ┌──────────────────┐
│  AgentLoop    │────►│ OutboundMessage  │
│  SendResponse │     │ Metadata[reef_task_id]
└───────┬───────┘     └──────────────────┘
        │
        ▼ SwarmChannel.Send() 拦截
┌───────────────┐
│  TaskReporter │  发送 task_completed
│  ws.Send()    │
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  Reef Server  │  状态 = Completed，释放 Client 容量
│  Scheduler    │  检查队列是否有匹配任务
└───────────────┘

阶段 4: 失败处理（分支）
─────────────────────────────────────────────────────────────
        │ 本地执行失败
        ▼
┌───────────────┐
│  TaskRunner   │  attempt < max_retries ?
└───────┬───────┘
   Yes /      \ No
      ▼        ▼
  指数退避   ws.Send(task_failed)
  重试       ▼
        ┌───────────────┐
        │  Escalation   │  Reassign / Terminate / Human
        │  Handler      │
        └───────────────┘
```

---

## 4. State Machines（状态机）

### 4.1 任务状态机（Task State Machine）

```
                    ┌─────────────┐
         ┌─────────│   Created   │◄──────────────────┐
         │         └──────┬──────┘                   │
         │                │ Enqueue                   │
         │                ▼                           │
         │         ┌─────────────┐    Dequeue         │
         │         │    Queued   │◄───────────────────┤
         │         └──────┬──────┘    (reassign)     │
         │                │ Dispatch                  │
         │                ▼                           │
         │         ┌─────────────┐                    │
         │    ┌───│   Assigned  │                    │
         │    │    └──────┬──────┘                    │
         │    │           │ Client reports started    │
         │    │           ▼                           │
         │    │    ┌─────────────┐                    │
 Cancel  │    └──►│   Running   │────────────────────┤
 (Server)│         └──────┬──────┘  Client completes   │
         │                │                          │
         │    ┌───────────┼───────────┐              │
         │    │           │           │              │
         │    ▼           ▼           ▼              │
         │ ┌──────┐  ┌────────┐  ┌────────┐         │
         └►│Paused│  │Failed  │  │Completed│         │
           └──┬───┘  └───┬────┘  └────────┘         │
              │          │                          │
         Resume│          │ Escalate                 │
              ▼          ▼                          │
           ┌────────┐  ┌──────────┐                 │
           │Running │  │Escalated │─────────────────┘
           └────────┘  └────┬─────┘   (reassign or terminate)
                            │
                            ▼
                      ┌──────────┐
                      │Terminated│
                      └──────────┘
```

### 4.2 连接状态机（Connection State Machine）

```
                    ┌─────────────┐
         ┌─────────│ disconnected│
         │         └──────┬──────┘
         │                │ Connect()
         │                ▼
         │         ┌─────────────┐
         │         │  connecting │
         │         └──────┬──────┘
         │                │ Success
         │                ▼
         │         ┌─────────────┐
         │    ┌───│  connected  │
         │    │    └──────┬──────┘
         │    │           │ Send register
         │    │           ▼
         │    │    ┌─────────────┐
         │    └──►│  registered │◄─────────┐
         │         └──────┬──────┘          │
         │                │                 │ Reconnect
         │         ┌──────┘                 │ (same client_id)
         │         ▼                        │
         │    ┌─────────────┐               │
         └───►│ reconnecting│───────────────┘
              └─────────────┘  (backoff)
```

---

## 5. Concurrency Model（并发模型）

### 5.1 Goroutine 模型

**Server 端 Goroutine 分布**：
```
main goroutine
├── HTTP Listener (net/http) — 1个
│   ├── WebSocket upgrader — 每 Client 1个 read goroutine
│   │   └── message router (同步处理，快速返回)
│   └── Admin HTTP handler — 每请求 1个（net/http 默认）
├── Registry Eviction Loop — 1个（每 10s 扫描）
├── Scheduler Trigger Loop — 1个（监听 registry 变更 channel）
└── Task Queue Monitor — 1个（每 5s 检查过期任务）
```

**Client 端 Goroutine 分布**：
```
main goroutine
├── AgentLoop.Run — 1个主循环 goroutine
│   └── 每 turn 1个 goroutine（processMessage 已内建）
├── SwarmChannel
│   ├── WebSocket read loop — 1个
│   ├── WebSocket write loop — 1个
│   └── heartbeat ticker — 1个
├── Connector reconnect loop — 仅在断连时启动
└── TaskReporter progress ticker — 每在飞任务 1个（或全局 1个轮询）
```

### 5.2 锁策略

**Server 锁层次**：
1. `Registry.mu`（`sync.RWMutex`）：保护 `clients` map。读操作（`FindCandidates`、`GetClient`）使用 `RLock`，写操作（`Register`、`Heartbeat`、`Evict`）使用 `Lock`。
2. `TaskQueue.mu`（`sync.Mutex`）：保护 `tasks` 切片。`Enqueue`/`Dequeue` 持锁时间极短（O(1)）。
3. `Server.mu`（`sync.RWMutex`）：保护 `clients`（conn map）和 `tasks`（task map）。主要用于连接管理和任务查找。

**锁顺序规则（避免死锁）**：
```
Server.mu → Registry.mu → TaskQueue.mu
```
任何代码路径必须按此顺序获取锁。反向顺序绝对禁止。

**Client 锁层次**：
1. `Connector.mu`（`sync.Mutex`）：保护 `conn` 和 `status`。
2. `TaskRunner.mu`（`sync.RWMutex`）：保护 `activeTasks` map。

### 5.3 Context 传播

- **Server 层**：顶层 `context.Background()` 派生 `Server.ctx`，`Shutdown` 时调用 `cancel()`，级联取消所有子 goroutine。
- **WebSocket Handler**：每个连接使用 `http.Request.Context()`，客户端断连时自动取消。
- **Task 执行**：`task_dispatch` 处理时创建 `taskCtx, taskCancel := context.WithTimeout(Server.ctx, task.Timeout)`。`taskCancel` 存入 `ActiveTask.CancelFunc`，支持 LIFE-01 的 cancel 操作。
- **Client AgentLoop**：`processMessage` 的 `ctx` 来自 `taskCtx`，LLM 调用和工具执行均接收此 context，确保 cancel 信号可穿透到最底层。

---

## 6. Error Handling（错误处理）

### 6.1 本地重试流程

```
[AgentLoop 执行失败]
        │
        ▼
┌───────────────┐
│  ErrorClassifier │  判断错误类型
└───────┬───────┘
        │
   可恢复 / 不可恢复
      ▼         ▼
  重试计数+1   立即上报
      │
      ▼
  attempt < max ?
   Yes /   \ No
    ▼       ▼
 退避等待  上报 Server
 后重试   (task_failed)
```

**可恢复错误类型**：
- LLM 临时 5xx / 429
- 工具执行超时（context deadline exceeded）
- WebSocket 瞬时写失败（重连前最后一次尝试）

**不可恢复错误类型**：
- LLM 全部 fallback 耗尽
- 工具执行 panic（已被 recovery）
- 任务被 Server 明确 cancel
- Client 收到 `task_pause` 后断连超过超时窗口

### 6.2 上报与 Server 决策流程

```
Client 发送 task_failed
        │
        ▼
┌───────────────┐
│  Server       │  解析 attempt_history
│  Task Manager │  更新任务状态 = Failed
└───────┬───────┘
        │
        ▼
┌───────────────┐
│  Escalation   │
│  Handler      │
└───────┬───────┘
        │
   ┌────┼────┐
   ▼    ▼    ▼
Reassign Terminate Human
   │      │      │
   ▼      ▼      ▼
回到    状态=   状态=
调度器   Failed  Escalated
```

---

## 7. Reuse Strategy（复用策略）

### 7.1 直接复用的 PicoClaw 组件

| 组件 | 复用方式 | 说明 |
|------|----------|------|
| `pkg/bus.MessageBus` | 直接实例化 | Client 复用为 AgentLoop 和 SwarmChannel 的通信总线 |
| `pkg/bus.InboundMessage` / `OutboundMessage` | 直接复用 | SwarmChannel 构造 InboundMessage 注入任务 |
| `pkg/agent.AgentLoop` | 直接实例化 | Client 核心执行引擎，通过 `processOptions` 扩展注入 TaskContext |
| `pkg/agent.EventBus` | 直接实例化 | 用于 Hook 机制监听 turn 事件 |
| `pkg/agent.HookManager` | 直接实例化 | 注册 ReefTaskHook 拦截 TurnStart |
| `pkg/channels.Channel` | 实现接口 | SwarmChannel 实现此接口 |
| `pkg/channels.BaseChannel` | 嵌入组合 | SwarmChannel 嵌入 BaseChannel 复用通用逻辑 |
| `pkg/skills.SkillsLoader` | 直接实例化 | 加载角色特定技能子集 |
| `pkg/skills.SkillInfo` | 直接复用 | 注册时通告技能列表 |
| `pkg/config.Config` | 扩展字段 | 在 Config 中新增 `ReefConfig` 字段 |
| `pkg/providers.LLMProvider` | 直接复用 | Client 的 LLM 调用能力 |
| `pkg/logger` | 直接调用 | 所有组件统一使用结构化日志 |

### 7.2 需要扩展的接口点

| 扩展点 | 位置 | 扩展方式 |
|--------|------|----------|
| `processOptions` 注入 TaskContext | `pkg/agent/loop.go` | 在 `processOptions` 结构体中新增 `TaskContext *reef.TaskContext` 字段（omitempty），不破坏现有逻辑 |
| `turnState` 扩展字段 | `pkg/agent/turn.go` | 新增 `taskContext atomic.Value` 字段，用于 Hook 和 cancel 访问 |
| `Config` 新增 Reef 配置 | `pkg/config/config.go` | 在 `Config` 结构体中新增 `Reef ReefConfig` 字段 |
| `Channel` 注册 | `pkg/channels/manager.go` | Manager 初始化时识别 `channelsConfig.Swarm` 配置，实例化 SwarmChannel |

---

## 8. Key Decisions（关键技术决策）

### 8.1 为什么 Server 拥有任务状态机？

**权衡**：
- **Client 拥有状态机**：减少 Server 状态，Client 自主决策。但导致状态分散，Server 无法做全局调度决策，且 Client 断连后状态丢失。
- **Server 拥有状态机**（选定）：Server 是唯一的 canonical 状态来源，Client 仅作为执行器。断连后 Server 知道任务应暂停，重连后可恢复。所有 retry/escalation 逻辑中心化，易于维护和调试。

**代价**：Server 需要更多内存（每个任务 ~1KB 元数据，1000 任务 ~1MB，可忽略）。

### 8.2 为什么采用单二进制双模式？

**权衡**：
- **分离二进制**：代码隔离更彻底，Server 不依赖 AgentLoop。但需要维护两个构建流程，部署复杂。
- **单二进制双模式**（选定）：简化部署（边缘设备只需一个二进制），共享 `pkg/reef` 协议代码避免重复。`--mode=server` 时不初始化 AgentLoop，`--mode=client` 时不初始化 HTTP admin，启动时开销无差异。

**代价**：二进制体积略增（包含 Server 和 Client 的代码），但 Go 的 dead code elimination 确保未使用代码不进入最终二进制。

### 8.3 为什么使用 Hook 机制注入 TaskContext 而非修改 AgentLoop？

**权衡**：
- **直接修改 AgentLoop**：在 `processMessage` 和 `runAgentLoop` 中硬编码 TaskContext 逻辑，代码路径更短。但侵入 PicoClaw 核心，增加维护负担，且与 PicoClaw  upstream 同步困难。
- **Hook 机制注入**（选定）：完全利用 PicoClaw 已有的 Hook 系统（`EventObserver`、`LLMInterceptor`），零侵入核心循环。`processOptions` 中新增可选字段即可，向后兼容。

**代价**：Hook 回调有微小延迟（~1ms），对任务执行无实质影响。

### 8.4 为什么 WebSocket 断连 = Pause 而非 Fail？

**权衡**：
- **断连 = Fail**：实现简单，Server 收到 heartbeat 超时后直接标记任务失败。但边缘网络不稳定时导致大量误失败。
- **断连 = Pause**（选定）：给予 Client 重连恢复的机会。任务上下文保留在 Client 内存中，重连后从暂停点继续。只有断连超过超时窗口（90s）才触发 Escalation。

**代价**：Client 需要在断连期间保留任务上下文（内存占用增加，但通常一个任务上下文 < 1MB）。

### 8.5 为什么调度器使用简单 O(n) 匹配而非更复杂算法？

**权衡**：
- **复杂算法**（如一致性哈希、负载均衡加权轮询）：v1 规模下（<100 Client）过度设计，增加代码复杂度。
- **O(n) 简单匹配**（选定）：遍历候选列表，按容量和心跳 freshness 排序。代码简单、可预测、易于测试。v1 的 n 最大为在线 Client 数，通常 < 50，性能完全可接受。

**未来演进**：若规模扩大到 1000+ Client，可引入角色索引（`map[role][]clientID`）将匹配优化到 O(k)，k 为同角色 Client 数。
