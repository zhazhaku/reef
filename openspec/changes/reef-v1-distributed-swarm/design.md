# Reef v1 技术设计文档

## 1. Architecture Overview（架构总览）

### 1.1 三层架构

Reef 采用 **Server-Client-Protocol 三层架构**，基于 Hub-and-Spoke 拓扑：

```
┌─────────────────────────────────────────────────────────────┐
│                        Reef Server                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Registry  │  │  Scheduler  │  │   HTTP Admin API    │  │
│  │  (client表) │  │ (匹配+分发) │  │  /admin/status      │  │
│  └──────┬──────┘  └──────┬──────┘  │  /admin/tasks       │  │
│         │                │          └─────────────────────┘  │
│  ┌──────▼────────────────▼──────┐                            │
│  │        Task State Machine    │  ◄── 任务状态管理核心        │
│  │   (Created→Running→Done)     │                            │
│  └──────────────────────────────┘                            │
│  ┌──────────────────────────────┐                            │
│  │   WebSocket Acceptor         │  ◄── gorilla/websocket     │
│  │   (监听端口, 管理连接生命周期)  │                            │
│  └──────────────────────────────┘                            │
└────────────────────────┬────────────────────────────────────┘
                         │ WebSocket
        ┌────────────────┼────────────────┐
        │                │                │
        ▼                ▼                ▼
┌──────────────┐ ┌──────────────┐ ┌──────────────┐
│ Reef Client  │ │ Reef Client  │ │ Reef Client  │
│  (role=coder)│ │(role=analyst)│ │(role=tester) │
└──────┬───────┘ └──────┬───────┘ └──────┬───────┘
       │                │                │
       ▼                ▼                ▼
┌─────────────────────────────────────────────────────────┐
│              PicoClaw Core (复用组件)                     │
│  ┌─────────┐ ┌──────────┐ ┌──────────┐ ┌─────────────┐  │
│  │EventBus │ │AgentLoop │ │  Skills  │ │  Providers  │  │
│  │/Message │ │+TaskCtx  │ │ Registry │ │   Router    │  │
│  │  Bus    │ │  Hook    │ │          │ │             │  │
│  └─────────┘ └──────────┘ └──────────┘ └─────────────┘  │
│  ┌─────────────────────────────────────────────────────┐  │
│  │         SwarmChannel (pkg/channels/swarm/)          │  │
│  │    实现 Channel 接口，桥接 WebSocket 与 MessageBus    │  │
│  └─────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

### 1.2 部署视图

```
单二进制 (cmd/reef/main.go)
├── --mode=server
│   ├── 启动 WebSocket Acceptor (端口 8080)
│   ├── 启动 HTTP Admin (端口 8081)
│   ├── 启动心跳扫描 goroutine
│   └── 启动调度器 goroutine
│
└── --mode=client
    ├── 加载角色配置 (reef.role)
    ├── 加载技能子集 (skills/roles/<role>.yaml)
    ├── 初始化 SwarmChannel
    ├── 连接 Server WebSocket
    ├── 发送 register 消息
    └── 启动 AgentLoop (复用 PicoClaw)
```

---

## 2. Component Breakdown（组件分解）

### 2.1 pkg/reef/protocol.go — 消息协议定义

职责：定义 Server 与 Client 之间的所有消息类型、枚举常量和序列化逻辑。

```go
package reef

// 协议版本
const ProtocolVersion = "reef-v1"

// MessageType 枚举
type MessageType string
const (
    MsgRegister    MessageType = "register"
    MsgRegisterAck MessageType = "register_ack"
    MsgHeartbeat   MessageType = "heartbeat"
    MsgTaskDispatch MessageType = "task_dispatch"
    MsgTaskProgress MessageType = "task_progress"
    MsgTaskCompleted MessageType = "task_completed"
    MsgTaskFailed    MessageType = "task_failed"
    MsgCancel        MessageType = "cancel"
    MsgPause         MessageType = "pause"
    MsgResume        MessageType = "resume"
    MsgControlAck    MessageType = "control_ack"
)

// Message 通用消息包装器
type Message struct {
    MsgType   MessageType     `json:"msg_type"`
    TaskID    string          `json:"task_id,omitempty"`
    Timestamp int64           `json:"timestamp"`
    Payload   json.RawMessage `json:"payload"`
}

// RegisterPayload, TaskDispatchPayload, TaskProgressPayload ...
// 每个 Payload 为独立 struct，通过 json.RawMessage 延迟解析
```

**设计要点：**
- 使用 `json.RawMessage` 实现通用消息解析：先解出 `MsgType`，再二次解析 `Payload`
- 所有时间戳统一为 Unix 毫秒（int64），避免时区问题
- 协议版本在 `register` 消息中显式声明，Server 拒绝不兼容版本

---

### 2.2 pkg/reef/task.go — 任务状态机

职责：定义 Task 领域模型、状态枚举、状态转换规则。

```go
package reef

// TaskStatus 枚举
type TaskStatus string
const (
    TaskCreated    TaskStatus = "Created"
    TaskQueued     TaskStatus = "Queued"
    TaskAssigned   TaskStatus = "Assigned"
    TaskRunning    TaskStatus = "Running"
    TaskPaused     TaskStatus = "Paused"
    TaskCompleted  TaskStatus = "Completed"
    TaskFailed     TaskStatus = "Failed"
    TaskCancelled  TaskStatus = "Cancelled"  // Failed 的子状态
)

// Task 领域对象
type Task struct {
    ID              string
    Status          TaskStatus
    Instruction     string
    RequiredRole    string
    RequiredSkills  []string
    MaxRetries      int
    TimeoutMs       int64
    AssignedClient  string          // client_id
    Result          *TaskResult
    Error           *TaskError
    AttemptHistory  []AttemptRecord
    CreatedAt       time.Time
    AssignedAt      *time.Time
    StartedAt       *time.Time
    CompletedAt     *time.Time
    EscalationCount int             // 重分配次数
}

// TaskContext —— 注入 AgentLoop 的执行上下文
type TaskContext struct {
    TaskID      string
    CancelFunc  context.CancelFunc
    PauseCh     chan struct{}
    ResumeCh    chan struct{}
}
```

**状态转换规则（有效边）：**

```
Created ──► Queued ──► Assigned ──► Running ──► Completed
                              │         │
                              │         ├─► Failed (escalated)
                              │         │       │
                              │         │       ▼
                              │         │   Reassign ──► Assigned
                              │         │
                              │         ├─► Paused ──► Running
                              │         │      │
                              │         │      └─► Failed (超时/断线)
                              │         │
                              │         └─► Cancelled
                              │
                              └─► Failed (Client 注册后断开)
```

**状态转换守卫条件：**
- `Running → Paused`：仅允许收到 `pause` 控制消息
- `Paused → Running`：仅允许收到 `resume` 控制消息或断线重连
- `Running → Cancelled`：仅允许收到 `cancel` 控制消息
- `Running → Failed`：Client 上报 `task_failed` 或心跳超时
- `Failed → Assigned`：Escalation Handler 决策为 Reassign 且 `EscalationCount < max`

---

### 2.3 pkg/reef/server/ — Server 组件

#### 2.3.1 registry.go — Client 注册表

```go
type Registry struct {
    mu      sync.RWMutex
    clients map[string]*ClientInfo  // key: client_id
}

type ClientInfo struct {
    ID           string
    Role         string
    Skills       []string
    Providers    []string
    Capacity     int
    CurrentLoad  int
    LastHeartbeat time.Time
    Conn         *websocket.Conn
    State        ClientState
}
```

**线程安全策略：**
- 读操作：`RLock()` —— 调度器频繁读取
- 写操作：`Lock()` —— 注册、心跳更新、驱逐
- 心跳扫描独立 goroutine，每 5 秒遍历一次，对超时 Client 标记 stale

#### 2.3.2 scheduler.go — 任务调度器

```go
type Scheduler struct {
    registry *Registry
    queue    *TaskQueue
    tasks    map[string]*Task  // 全局任务索引
}

func (s *Scheduler) Schedule(task *Task) error
func (s *Scheduler) matchClient(task *Task) *ClientInfo
func (s *Scheduler) dispatch(task *Task, client *ClientInfo) error
func (s *Scheduler) onClientAvailable(clientID string)  // 触发队列重新调度
```

**调度算法（O(n)）：**
1. 遍历注册表中 `State == Connected` 的 Client
2. 过滤：`Role == RequiredRole` && `Skills` 覆盖 `RequiredSkills` && `CurrentLoad < Capacity`
3. 选择 `CurrentLoad` 最小者（负载均衡）
4. 若匹配失败，入队；否则立即 dispatch

#### 2.3.3 queue.go — 内存任务队列

```go
type TaskQueue struct {
    mu     sync.Mutex
    tasks  []*Task
    maxLen int  // 默认 1000
}
```

- FIFO，支持 `Enqueue`、`Dequeue`、`Peek`
- 达到 `maxLen` 时返回 `ErrQueueFull`
- 调度器通过 `onClientAvailable` 回调触发重新调度

#### 2.3.4 websocket.go — WebSocket Acceptor

```go
type Acceptor struct {
    upgrader websocket.Upgrader
    registry *Registry
    scheduler *Scheduler
}

func (a *Acceptor) ServeHTTP(w http.ResponseWriter, r *http.Request)
func (a *Acceptor) handleConn(conn *websocket.Conn)
```

- 使用 `gorilla/websocket` 的 `Upgrader`
- 升级前验证 `x-reef-token` Header
- 每个连接启动两个 goroutine：
  - **Reader**：循环 `ReadMessage()`，分发到消息处理器
  - **Writer**：通过 channel 接收待发送消息，保证写顺序

---

### 2.4 pkg/reef/client/ — Client 组件

#### 2.4.1 connector.go — WebSocket 连接器

```go
type Connector struct {
    serverURL   string
    token       string
    conn        *websocket.Conn
    msgCh       chan Message      // 待发送消息队列
    reconnectCh chan struct{}     // 重连触发信号
    backoff     *Backoff          // 指数退避状态
}

func (c *Connector) Connect() error
func (c *Connector) reconnectLoop()
func (c *Connector) Send(msg Message) error
```

**重连策略：**
- 断线检测：`ReadMessage()` 返回 error 或写入失败
- 指数退避：1s, 2s, 4s, ... 最大 60s
- 重连成功后发送 `register`，携带相同 `client_id`

#### 2.4.2 task_runner.go — 任务执行器

```go
type TaskRunner struct {
    connector   *Connector
    agentLoop   *agent.AgentLoop  // PicoClaw AgentLoop 实例
    currentTask *RunningTask
}

type RunningTask struct {
    TaskID     string
    Ctx        context.Context
    CancelFunc context.CancelFunc
    PauseCh    chan struct{}
    ResumeCh   chan struct{}
}

func (r *TaskRunner) OnTaskDispatch(task TaskDispatchPayload)
func (r *TaskRunner) executeWithRetry(task *Task) error
func (r *TaskRunner) reportProgress(status TaskStatus, percent int)
```

**AgentLoop 集成：**
- 构造 `bus.Message{Type: Inbound, Text: task.Instruction}`
- 通过 `processOptions` 注入 `TaskContext`：`opts.TaskContext = &reef.TaskContext{...}`
- AgentLoop 执行完成后回调 `OnTaskCompleted` 或 `OnTaskFailed`

---

### 2.5 pkg/channels/swarm/ — SwarmChannel 实现

职责：实现 PicoClaw `Channel` 接口，使 Reef Client 可通过标准 Channel 机制收发消息。

```go
package swarm

type SwarmChannel struct {
    connector *client.Connector
    inCh      chan bus.Message   // 接收 Server 下发任务 → AgentLoop
    outCh     chan bus.Message   // AgentLoop 输出 → Server
}

// 实现 PicoClaw Channel 接口
func (s *SwarmChannel) Start() error
func (s *SwarmChannel) Stop() error
func (s *SwarmChannel) Send(msg bus.Message) error   // 发送 outbound 消息
func (s *SwarmChannel) Receive() <-chan bus.Message  // 接收 inbound 消息
```

**桥接逻辑：**
- `task_dispatch` → 转换为 `bus.Message{Type: Inbound}` → `inCh`
- AgentLoop 的 `outCh` 输出 → 转换为 `task_progress` / `task_completed` → WebSocket
- SwarmChannel 屏蔽了底层 WebSocket 细节，使 AgentLoop 无感知

---

### 2.6 cmd/reef/ — 主入口双模式支持

```go
package main

func main() {
    mode := flag.String("mode", "client", "运行模式: server 或 client")
    configPath := flag.String("config", "config.json", "配置文件路径")
    flag.Parse()

    switch *mode {
    case "server":
        runServer(*configPath)
    case "client":
        runClient(*configPath)
    default:
        log.Fatalf("未知模式: %s", *mode)
    }
}

func runServer(configPath string) {
    // 1. 加载配置
    // 2. 初始化 Registry、Scheduler、Queue
    // 3. 启动 WebSocket Acceptor
    // 4. 启动 HTTP Admin Server
    // 5. 启动心跳扫描 goroutine
}

func runClient(configPath string) {
    // 1. 加载配置（含 reef.role）
    // 2. 加载角色技能清单
    // 3. 初始化 Skill Registry（过滤加载）
    // 4. 初始化 AgentLoop（含角色 system_prompt）
    // 5. 初始化 SwarmChannel 并连接 Server
    // 6. 启动 AgentLoop
}
```

---

## 3. Data Flow（数据流）

### 3.1 任务从提交到完成的完整数据流

```
[任务提交端]        [Reef Server]                    [Reef Client]
     │                    │                               │
     │  POST /tasks       │                               │
     │───────────────────►│                               │
     │                    │ 1. 创建 Task (Created)        │
     │                    │ 2. Schedule() → 匹配 Client   │
     │                    │ 3. 状态 → Assigned            │
     │                    │                               │
     │                    │  task_dispatch (WebSocket)    │
     │                    │──────────────────────────────►│
     │                    │                               │ 4. 接收任务
     │                    │                               │ 5. 构造 bus.Message
     │                    │                               │ 6. 注入 TaskContext
     │                    │                               │ 7. 发布到 MessageBus
     │                    │                               │ 8. AgentLoop 消费执行
     │                    │                               │
     │                    │  task_progress (started)      │
     │                    │◄──────────────────────────────│
     │                    │ 状态 → Running                │
     │                    │                               │
     │                    │  task_progress (running 50%)  │
     │                    │◄──────────────────────────────│
     │                    │                               │
     │                    │  task_completed               │
     │                    │◄──────────────────────────────│ 9. AgentLoop 回调
     │                    │ 状态 → Completed              │
     │                    │ 释放 Client 容量              │
     │  HTTP 200 + result │                               │
     │◄───────────────────│                               │
```

### 3.2 控制流（Cancel / Pause / Resume）

```
[Admin/API]         [Reef Server]                    [Reef Client]
     │                    │                               │
     │ POST /tasks/:id/cancel                             │
     │───────────────────►│                               │
     │                    │ 1. 查找任务所在 Client          │
     │                    │ 2. 发送 cancel 消息             │
     │                    │──────────────────────────────►│
     │                    │                               │ 3. 调用 CancelFunc()
     │                    │                               │ 4. context.Cancel()
     │                    │                               │ 5. AgentLoop 终止
     │                    │  control_ack                  │
     │                    │◄──────────────────────────────│
     │                    │ 状态 → Cancelled              │
     │  HTTP 200          │                               │
     │◄───────────────────│                               │
```

---

## 4. State Machines（状态机）

### 4.1 任务状态机（Server 侧）

```
                    ┌─────────────┐
         ┌─────────►│   Created   │◄──────── 新任务提交
         │          └──────┬──────┘
         │                 │
         │                 ▼
         │          ┌─────────────┐
         │          │   Queued    │────────── 无可用 Client
         │          └──────┬──────┘
         │                 │
         │                 ▼
         │          ┌─────────────┐
         │          │  Assigned   │◄──────── 匹配到 Client
         │          └──────┬──────┘
         │                 │ dispatch
         │                 ▼
         │          ┌─────────────┐
         │          │   Running   │◄──────── Client 报告 started
         │          └──────┬──────┘
         │         ┌───────┼───────┬────────┐
         │         │       │       │        │
         │         ▼       ▼       ▼        ▼
         │   ┌────────┐ ┌──────┐ ┌─────┐ ┌──────────┐
         └───┤Paused  │ │Failed│ │Completed│ │Cancelled │
             └───┬────┘ └──┬───┘ └─────┘ └──────────┘
                 │         │
                 │    (escalation)
                 │         │
                 │         ▼
                 │    ┌─────────┐
                 │    │Reassign │──────► Assigned (EscalationCount++)
                 │    └─────────┘
                 │
                 └──────────────────────► Running (resume)
```

**关键规则：**
- 每个状态转换 MUST 在独立 goroutine 中串行处理（每个 Task 一个 goroutine，通过 channel 驱动）
- `Paused` 可由 `pause` 消息或 `disconnect` 事件触发
- `Failed` 可由 `task_failed`、心跳超时或 `reconnect_window` 过期触发

### 4.2 连接状态机（Client 侧）

```
┌─────────┐    connect()    ┌─────────┐    register()     ┌──────────┐
│  Idle   │ ───────────────►│Connecting│ ────────────────►│Registered│
└─────────┘                 └─────────┘                  └────┬─────┘
     ▲                                                        │
     │              ┌─────────────────────────────────────────┘
     │              │ heartbeat / task activity
     │              ▼
     │         ┌──────────┐
     │         │ Active   │◄────────────────────────────────┐
     │         └────┬─────┘                                 │
     │              │ disconnect                             │
     │              ▼                                        │
     │         ┌──────────┐    reconnect() success          │
     └─────────│Disconnected│───────────────────────────────┘
               └──────────┘
                    │
                    │ reconnect() fail / max attempts
                    ▼
               ┌──────────┐
               │  Exited  │
               └──────────┘
```

**关键规则：**
- `Disconnected` 状态下，in-flight 任务继续执行但停止进度上报
- `reconnect_window`（60s）内恢复连接 → 回到 `Active`
- 超过 `reconnect_window` → Server 标记 stale，Client 下次连接视为新注册

---

## 5. Concurrency Model（并发模型）

### 5.1 Server 端 Goroutine 模型

```
main goroutine
    ├── WebSocket Acceptor (net/http server)
    │   └── per-connection goroutines:
    │       ├── reader goroutine (ReadMessage loop)
    │       └── writer goroutine (WriteMessage loop via channel)
    │
    ├── Heartbeat Scanner (每 5s tick)
    │   └── 遍历 registry，标记 stale
    │
    ├── Scheduler (事件驱动)
    │   └── 处理 task enqueue, client available 事件
    │
    ├── Per-Task Goroutine (任务生命周期管理)
    │   └── 串行处理状态转换、超时检测、escalation
    │
    └── HTTP Admin Server (net/http)
        └── per-request goroutine
```

**锁策略：**

| 资源 | 锁类型 | 说明 |
|------|--------|------|
| Registry | `sync.RWMutex` | 读多写少；调度器频繁读取 |
| TaskQueue | `sync.Mutex` | 纯写/读，无并发遍历需求 |
| Task (全局索引) | `sync.RWMutex` | 按 TaskID 索引；Per-Task goroutine 修改自身时仍需锁 |
| Per-Task State | channel-driven | 每个 Task 一个 goroutine + 状态事件 channel，避免显式锁 |

### 5.2 Client 端 Goroutine 模型

```
main goroutine
    ├── Connector
    │   ├── reader goroutine (ReadMessage loop)
    │   ├── writer goroutine (WriteMessage loop)
    │   └── reconnector goroutine (指数退避重试)
    │
    ├── AgentLoop (PicoClaw 原有)
    │   └── 内部 goroutines 处理消息消费和工具调用
    │
    └── TaskRunner
        └── 每个执行任务一个 goroutine（受 Capacity 限制并发数）
```

**Context 传播：**
- `TaskContext.CancelFunc` 通过 `context.WithCancel` 派生
- AgentLoop 的所有工具调用接收 `ctx`，定期检测 `ctx.Err()`
- `PauseCh` / `ResumeCh` 通过 goroutine 阻塞实现暂停语义

---

## 6. Error Handling（错误处理）

### 6.1 错误分层

| 层级 | 错误类型 | 处理方式 |
|------|---------|---------|
| 网络层 | WebSocket 断开、TLS 错误 | 指数退避重连（Client）；标记 stale（Server） |
| 协议层 | 消息解析失败、未知 msg_type | 记录 warn 日志，丢弃消息，保持连接 |
| 调度层 | 无匹配 Client、队列满 | 入队等待或返回 429；Admin 端点暴露 |
| 执行层 | LLM 超时、工具调用失败 | Client 本地重试（RETRY-01） |
| 生命周期 | cancel、pause 超时 | 强制 context 取消；记录 error |

### 6.2 本地重试流程（Client）

```
执行失败
    │
    ▼
判断错误类型 ──► cancelled / escalated ──► 不重试，直接上报
    │
    ▼ (execution_error / timeout)
attempt < max_retries?
    │
    ├─ Yes ──► 指数退避等待 ──► 重新执行
    │              ▲
    │              └── 再次失败 ──► attempt++
    │
    └─ No ──► 构造 attempt_history ──► 上报 Server (task_failed, escalated)
```

### 6.3 Server Escalation 决策流程

```
收到 task_failed (escalated)
    │
    ▼
检查 EscalationCount < MaxEscalation?
    │
    ├─ Yes ──► 检查是否有其他可用 Client?
    │              │
    │              ├─ Yes ──► 决策: Reassign
    │              │              │
    │              │              ▼
    │              │          任务入队 → 调度器匹配新 Client
    │              │          EscalationCount++
    │              │
    │              └─ No ──► 决策: Terminate
    │                          │
    │                          ▼
    │                      任务状态 → Failed
    │
    └─ No ──► 决策: Escalate to Admin
                  │
                  ▼
              记录告警日志
              任务状态 → Failed
```

---

## 7. Reuse Strategy（PicoClaw 复用策略）

### 7.1 直接复用组件（无需修改）

| PicoClaw 组件 | 复用方式 | 说明 |
|--------------|---------|------|
| `pkg/bus` (EventBus/MessageBus) | 直接 import | Client 侧任务通过 MessageBus 进入 AgentLoop |
| `pkg/skills` (Skill Registry) | 直接 import | Client 启动时加载角色过滤后的技能子集 |
| `pkg/providers` (LLM Router) | 直接 import | AgentLoop 内部调用，Reef 无感知 |
| `pkg/channels` (Channel 接口) | 实现新 Channel | 新增 `SwarmChannel` 实现 `Channel` 接口 |

### 7.2 扩展接口点

| 扩展点 | 位置 | 修改内容 | 侵入性 |
|--------|------|---------|--------|
| `processOptions` Hook | `pkg/agent` | 新增 `TaskContext` 字段 | 低（可选字段，向后兼容） |
| AgentLoop 初始化 | `pkg/agent` | 支持注入外部 `system_prompt` | 低 |
| Channel 注册 | `pkg/channels` | 新增 `swarm` 通道类型到工厂 | 低 |

### 7.3 不依赖 PicoClaw 源码的独立组件

| 组件 | 说明 |
|------|------|
| `pkg/reef/protocol.go` | 独立消息协议定义 |
| `pkg/reef/task.go` | 独立任务领域模型 |
| `pkg/reef/server/*` | Server 独有，不依赖 PicoClaw |
| `pkg/reef/client/*` | Client 独有，仅依赖 PicoClaw 公开接口 |
| `cmd/reef/main.go` | 新入口，条件编译或运行时模式切换 |

---

## 8. Key Decisions（关键技术决策）

### 8.1 决策矩阵

| # | 决策 | 选项 A（选中） | 选项 B | 选项 C | 选择理由 |
|---|------|--------------|--------|--------|---------|
| D1 | 架构路径 | Fork PicoClaw + 扩展 | 外部包装（独立进程） | 从零构建 | 最大化复用现有组件，保持架构一致性；扩展 `processOptions` 即可注入 TaskContext |
| D2 | 部署模式 | 单二进制，双模式 | 两个独立二进制 | 容器化微服务 | 简化部署和 CI/CD；边缘设备资源受限，单二进制更易管理 |
| D3 | 角色模型 | 静态角色（启动绑定） | 动态角色发现 | 无角色（全量加载） | 降低复杂度；v1 场景下静态角色足够覆盖；动态发现引入一致性问题 |
| D4 | 状态所有权 | Server 拥有任务状态机 | Client 拥有状态机 | 分布式共识 | 简化故障处理；Client 仅维护 in-flight 任务上下文；Server 集中决策重试/重分配 |
| D5 | 传输协议 | WebSocket (gorilla) | gRPC | HTTP 轮询 | PicoClaw 已有依赖；全双工低延迟；gRPC 增加二进制体积和复杂度 |
| D6 | 重连语义 | 任务暂停 + 续跑 | 任务失败 + 重分配 | 任务继续（忽略断线） | 边缘网络频繁抖动，暂停续跑避免无效重试；与 Server 状态机一致 |
| D7 | 队列持久化 | 内存队列（v1） | SQLite 持久化 | Redis 外部队列 | v1 范围控制；内存队列实现简单；v2 再引入磁盘持久化 |
| D8 | 认证方式 | 共享 Token (Header) | JWT per-Client | mTLS | v1 最小可行；共享 Token 足够保护内网场景；JWT/mTLS 增加运维负担 |
| D9 | 并发模型 | goroutine + mutex | Actor 模型 | 线程池 | Go 原生范式；与 PicoClaw 一致；Per-Task goroutine 模型清晰 |
| D10 | AgentLoop 集成 | `processOptions` Hook | 重写 AgentLoop | Sidecar 模式 | 最小侵入；保持 PicoClaw AgentLoop 不变；Hook 模式向后兼容 |

### 8.2 决策详细说明

#### D1: 为什么 Fork 而非外部包装？

- **外部包装**（独立进程通过 HTTP/IPC 与 PicoClaw 通信）需要维护两套生命周期，增加进程间通信开销，且无法复用 PicoClaw 的 MessageBus 和 AgentLoop 内部状态。
- **Fork + 扩展**允许直接复用 `pkg/bus`、`pkg/skills`、`pkg/agent`，只需新增 `pkg/channels/swarm` 和少量 Hook 扩展。
- 风险：需要同步 PicoClaw 上游更新。缓解：扩展点最小化，集中在 `processOptions` 和 `cmd/reef/` 入口。

#### D4: 为什么 Server 拥有任务状态机？

- **Client 状态机**方案下，Client 断线后 Server 无法准确判断任务状态，重分配逻辑分散到各节点。
- **Server 中心化**使重试、重分配、Escalation 决策集中在一个地方，便于观测和调试。
- Client 仅维护 `RunningTask` 的本地执行上下文（cancel、pause、resume），不维护持久状态。

#### D6: 为什么断线 = 暂停而非失败？

- 目标平台包括 Sipeed ARM64 边缘设备，运行在不稳定的 WiFi/4G 环境下。
- 若断线 = 失败，则每次网络抖动都会触发不必要的重试和重分配，浪费算力和 Token。
- 暂停语义允许 Client 在数秒~数分钟内恢复连接并继续执行，用户体验更优。
- 配合 `reconnect_window`（60s）防止无限期悬挂。

---

## 9. 接口契约摘要

### 9.1 Server 公开接口

| 接口 | 协议 | 路径/端口 | 说明 |
|------|------|----------|------|
| WebSocket | WS/WSS | `:8080/ws` | Client 长连接；验证 `x-reef-token` |
| Admin Status | HTTP GET | `:8081/admin/status` | JSON，Client 注册表 |
| Admin Tasks | HTTP GET | `:8081/admin/tasks` | JSON，任务队列和状态 |
| Task Submit | HTTP POST | `:8081/tasks` | 提交新任务（v1 可选，也可内部触发） |

### 9.2 Client 配置契约

```yaml
reef:
  mode: "client"              # 运行时也可通过 --mode 覆盖
  server_url: "ws://reef-server:8080/ws"
  token: "shared-secret-token"
  role: "coder"
  max_concurrent_tasks: 2
  heartbeat_interval_sec: 10
  reconnect:
    max_backoff_sec: 60
    max_attempts: 0           # 0 = 无限
  
  # 角色技能清单路径 (相对于可执行文件或绝对路径)
  role_manifest: "skills/roles/coder.yaml"
```

---

*设计版本：v1.0*  
*日期：2026-04-27*  
*作者：Reef Research & Design Agent*
