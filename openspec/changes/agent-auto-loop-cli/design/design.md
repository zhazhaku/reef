# Agent Auto Loop Orchestrator — 技术设计

## 1. 架构总览

```
┌────────────────────────────────────────────────────────────────────────────┐
│                         AutoLoop Orchestrator                               │
│                       (pkg/agent/auto_orchestrator.go)                      │
│                                                                             │
│  ┌─────────────────────┐  ┌─────────────────────┐                          │
│  │    EventBus 订阅      │  │    Ticker 驱动       │                          │
│  │  - TaskCompleted     │  │  every 1s 轮询       │                          │
│  │  - TaskFailed        │  │  - 队列状态检查       │                          │
│  │  - ClientDisconnected│  │  - DAG 依赖解锁       │                          │
│  │  - ClientStale       │  │  - 自愈状态检查       │                          │
│  └──────────┬──────────┘  └──────────┬──────────┘                          │
│             │                        │                                      │
│  ┌──────────▼────────────────────────▼──────────────────────────────────┐  │
│  │                         Core Loop                                     │  │
│  │  ┌────────────────┐  ┌────────────────┐  ┌────────────────────────┐  │  │
│  │  │ PollQueue()     │  │ PollHealing()  │  │ PollScale()            │  │  │
│  │  │ → 取 ready task │  │ → 检查 pending │  │ → 检查队列深度          │  │  │
│  │  │ → EnsureClient  │  │    heal 操作   │  │ → 检查 idle client      │  │  │
│  │  │ → Submit(plan)  │  │ → 执行恢复     │  │ → ScaleUp/Down 决策     │  │  │
│  │  └────────────────┘  └────────────────┘  └────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│                             │                                              │
│  ┌──────────────────────────▼──────────────────────────────────────────┐  │
│  │  依赖组件                                                           │  │
│  │                                                                     │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌─────────┐│  │
│  │  │ TaskPlanner   │  │ ClientPool   │  │    Healer    │  │ Scaler  ││  │
│  │  │ 解析指令      │  │ spawn/kill   │  │ retry/       │  │ 扩容/   ││  │
│  │  │ 拆分子任务    │  │ 保活监控     │  │ escalate     │  │ 缩容    ││  │
│  │  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └────┬────┘│  │
│  │         │                 │                 │               │       │  │
│  └─────────┼─────────────────┼─────────────────┼───────────────┼───────┘  │
│            │                 │                 │               │          │
│  ┌─────────▼─────────────────▼─────────────────▼───────────────▼───────┐  │
│  │                    Server 基础设施                                     │  │
│  │  ┌────────────────┐  ┌────────────────┐                             │  │
│  │  │   Scheduler     │  │   Registry     │                             │  │
│  │  │  Submit/        │  │  List/Register │                             │  │
│  │  │  TryDispatch/   │  │  ScanStale/    │                             │  │
│  │  │  HandleTask*    │  │  ListByRole    │                             │  │
│  │  └────────────────┘  └────────────────┘                             │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────────────────────────────┘
```

## 2. 文件变更清单

### 新增文件 (6 个)

| 文件 | 职责 |
|------|------|
| `pkg/agent/auto_orchestrator.go` | Orchestrator 核心编排器 |
| `pkg/agent/client_pool.go` | 客户端进程生命周期管理 |
| `pkg/agent/auto_planner.go` | 任务解析 + DAG 编排 |
| `pkg/agent/auto_healer.go` | 自愈策略实现 |
| `pkg/agent/auto_scaler.go` | 扩缩容决策引擎 |
| `pkg/commands/cmd_auto.go` | `/auto` CLI 命令 |

### 修改文件 (4 个)

| 文件 | 修改内容 |
|------|----------|
| `pkg/agent/conversation_mode.go` | 新增 ModeAuto/ModeManual 常量 |
| `pkg/agent/agent.go` | Run() 中注入 orchestrator ticker |
| `pkg/agent/agent_init.go` | NewAgentLoop 初始化 Orchestrator |
| `pkg/commands/builtin.go` | 注册 autoCommand() |

### 删除文件
- 原 `pkg/agent/auto_loop.go` 概念 → 精简重构为 `auto_orchestrator.go`（原 AutoLoopController 大部分业务逻辑移至 Orchestrator，仅保留状态/CLI 路由）

## 3. 核心数据结构

### 3.1 AutoLoopOrchestrator — `pkg/agent/auto_orchestrator.go`

```go
package agent

import (
    "context"
    "fmt"
    "sync"
    "time"

    "github.com/zhazhaku/reef/pkg/bus"
    reefServer "github.com/zhazhaku/reef/pkg/reef/server"
)

// ==================== 状态定义 ====================

// OrchestratorState 定义 Orchestrator 的运行状态
type OrchestratorState int

const (
    OrchStateIdle       OrchestratorState = iota // 空闲
    OrchStatePlanning                             // 正在解析任务
    OrchStateDispatching                          // 正在派发任务
    OrchStateMonitoring                           // 监控执行中
    OrchStateHealing                              // 自愈恢复中
    OrchStateScaling                              // 扩缩容中
)

// AutoMode 定义自动循环模式
type AutoMode string

const (
    AutoOnce      AutoMode = "once"       // 单次执行
    AutoLoopN     AutoMode = "loop_n"     // 循环 N 次
    AutoInfinite  AutoMode = "infinite"   // 无限循环
    AutoUntilCond AutoMode = "until_cond" // 直到条件满足
)

// LoopConfig 定义循环配置
type LoopConfig struct {
    Mode        AutoMode      `json:"mode"`
    LoopCount   int           `json:"loop_count,omitempty"`    // LoopN 时的次数
    Parallelism int           `json:"parallelism"`             // 最大并行任务数
    AutoClient  bool          `json:"auto_client"`             // 是否自动启停 client
    MinClients  int           `json:"min_clients"`             // 最少保留 client
    MaxClients  int           `json:"max_clients"`             // 最多 client
    IdleTimeout time.Duration `json:"idle_timeout"`            // idle 回收超时
    RetryPolicy RetryPolicy   `json:"retry_policy"`            // 重试策略
}

// RetryPolicy 定义重试策略
type RetryPolicy struct {
    MaxRetries      int           `json:"max_retries"`       // 默认 3
    RetryDelay      time.Duration `json:"retry_delay"`       // 默认 5s
    BackoffFactor   float64       `json:"backoff_factor"`    // 默认 2.0
    ExcludeFailed   bool          `json:"exclude_failed"`    // 排除失败 client
    AutoSpawnOnRetry bool         `json:"auto_spawn_retry"`  // 重试时自动 spawn
}

// DefaultLoopConfig 默认配置
var DefaultLoopConfig = LoopConfig{
    Mode:        AutoOnce,
    Parallelism: 3,
    AutoClient:  true,
    MinClients:  1,
    MaxClients:  5,
    IdleTimeout: 5 * time.Minute,
    RetryPolicy: RetryPolicy{
        MaxRetries:      3,
        RetryDelay:      5 * time.Second,
        BackoffFactor:   2.0,
        ExcludeFailed:   true,
        AutoSpawnOnRetry: true,
    },
}

// ==================== Orchestrator ====================

// AutoLoopOrchestrator 是自动循环执行引擎的核心编排器
type AutoLoopOrchestrator struct {
    mu            sync.Mutex
    mode          ConversationMode    // 当前对话模式 (auto/manual/chat)
    state         OrchestratorState   // 运行状态
    loopConfig    LoopConfig          // 循环配置
    plan          *ExecutionPlan      // 当前执行计划 (DAG)
    planSeq       int                 // 执行计划序号

    // 依赖组件
    planner     *TaskPlanner
    clientPool  *ClientPool
    healer      *Healer
    scaler      *AutoScaler
    scheduler   *reefServer.Scheduler
    registry    *reefServer.Registry

    // 循环控制
    ticker      *time.Ticker
    tickerCh    chan time.Time
    doneCh      chan struct{}
    stopCh      chan struct{}

    // 事件总线
    eventBus    bus.Bus

    // 统计
    completedCount int
    failedCount    int
}

// ExecutionPlan 表示一次 /auto run 的执行计划
type ExecutionPlan struct {
    ID         string         `json:"id"`
    Instruction string        `json:"instruction"`
    Tasks      []*PlannedTask `json:"tasks"`
    CreatedAt  time.Time      `json:"created_at"`
    Status     PlanStatus     `json:"status"` // active/completed/failed/cancelled
}

type PlanStatus string
const (
    PlanActive    PlanStatus = "active"
    PlanCompleted PlanStatus = "completed"
    PlanFailed    PlanStatus = "failed"
    PlanCancelled PlanStatus = "cancelled"
)

// PlannedTask 表示执行计划中的一个任务节点
type PlannedTask struct {
    ID              string         `json:"id"`
    Instruction     string         `json:"instruction"`
    RequiredRole    string         `json:"required_role"`
    RequiredSkills  []string       `json:"required_skills,omitempty"`
    Dependencies    []string       `json:"dependencies,omitempty"`     // PlannedTask.ID
    Status          TaskNodeStatus `json:"status"`
    AssignedTaskID  string         `json:"assigned_task_id,omitempty"` // reef.Task.ID
    Result          string         `json:"result,omitempty"`
    Error           string         `json:"error,omitempty"`
    Priority        int            `json:"priority"`                  // 0=normal, 1=high
    CreatedAt       time.Time      `json:"created_at"`
}

type TaskNodeStatus string
const (
    NodePending TaskNodeStatus = "pending"    // 依赖未就绪
    NodeReady   TaskNodeStatus = "ready"      // 依赖就绪，可调度
    NodeRunning TaskNodeStatus = "running"    // 已提交给 Scheduler
    NodeDone    TaskNodeStatus = "done"       // 成功
    NodeFailed  TaskNodeStatus = "failed"     // 失败（不可恢复）
    NodeBlocked TaskNodeStatus = "blocked"    // 因依赖失败被阻塞
)

// ==================== 核心方法 ====================

// NewAutoLoopOrchestrator 创建编排器
func NewAutoLoopOrchestrator(
    scheduler *reefServer.Scheduler,
    registry *reefServer.Registry,
    eventBus bus.Bus,
) *AutoLoopOrchestrator {
    return &AutoLoopOrchestrator{
        mode:       ModeChat,
        state:      OrchStateIdle,
        loopConfig: DefaultLoopConfig,
        planner:    NewTaskPlanner(),
        clientPool: NewClientPool(ClientPoolOptions{
            Registry:   registry,
            ServerURL:  "ws://127.0.0.1:9999/ws",
            Token:      getToken(),     // 从 .reef.pid 读取
            ReefBin:    "/root/reef_server/reef",
            IdleTimeout: 5 * time.Minute,
            MaxClients:  5,
        }),
        healer:    NewHealer(DefaultRetryPolicy),
        scaler:    NewAutoScaler(),
        scheduler: scheduler,
        registry:  registry,
        tickerCh:  make(chan time.Time, 10),
        doneCh:    make(chan struct{}),
        stopCh:    make(chan struct{}),
        eventBus:  eventBus,
    }
}

// SubmitTask 提交一个独立任务（/auto run 入口）
func (o *AutoLoopOrchestrator) SubmitTask(ctx context.Context, instruction string, mode AutoMode, loopCount int) (*ExecutionPlan, error) {
    // 1. 解析指令 → 子任务列表
    // 2. 创建 ExecutionPlan
    // 3. 标记 ready 的子任务
    // 4. 触发 Tick() 使之开始调度
}

// SubmitRawTask 直接提交一条 reef.Task（/auto step 入口）
func (o *AutoLoopOrchestrator) SubmitRawTask(ctx context.Context, instruction string, role string, skills []string) error {
    // 1. 确保有可用 client
    // 2. Scheduler.Submit(task)
    // 3. 等待完成（同步）
}

// Stop 停止当前执行
func (o *AutoLoopOrchestrator) Stop() {
    close(o.stopCh)
    o.cleanup()
}

// Tick 返回 ticker channel（供 AgentLoop.Run 的 select 使用）
func (o *AutoLoopOrchestrator) Tick() <-chan time.Time {
    return o.tickerCh
}

// Done 返回 done channel
func (o *AutoLoopOrchestrator) Done() <-chan struct{} {
    return o.doneCh
}

// Trigger 触发一次轮询
func (o *AutoLoopOrchestrator) Trigger() {
    select {
    case o.tickerCh <- time.Now():
    default:
    }
}

// Poll 执行一次完整的轮询（AgentLoop.Run 中 ticker case 调用）
func (o *AutoLoopOrchestrator) Poll(ctx context.Context) {
    if o.mode != ModeAuto && o.mode != ModeManual {
        return
    }
    o.PollQueue(ctx)
    o.PollHealing(ctx)
    o.PollScale(ctx)
}

// PollQueue 检查队列状态，提交 ready 任务
func (o *AutoLoopOrchestrator) PollQueue(ctx context.Context) {
    o.mu.Lock()
    if o.plan == nil || o.plan.Status != PlanActive {
        o.mu.Unlock()
        return
    }

    // 找出所有 ready 状态的任务
    var readyTasks []*PlannedTask
    for _, task := range o.plan.Tasks {
        if task.Status != NodeReady {
            continue
        }
        readyTasks = append(readyTasks, task)
    }
    o.mu.Unlock()

    // 限制并行度
    maxBatch := o.loopConfig.Parallelism
    if len(readyTasks) > maxBatch {
        readyTasks = readyTasks[:maxBatch]
    }

    for _, pt := range readyTasks {
        // 确保有可用 client
        clientID, err := o.clientPool.EnsureClient(ctx, pt.RequiredRole, pt.RequiredSkills)
        if err != nil {
            continue // 等待下一次轮询
        }

        // 构建 reef.Task
        task := reef.NewTask(
            fmt.Sprintf("auto-%s-%s", o.plan.ID, pt.ID),
            pt.Instruction,
            pt.RequiredRole,
            pt.RequiredSkills,
        )
        task.ReplyTo = &reef.ReplyTo{} // 设置回执

        // 提交给 Scheduler
        if err := o.scheduler.Submit(task); err != nil {
            continue
        }

        // 更新状态
        o.mu.Lock()
        pt.Status = NodeRunning
        pt.AssignedTaskID = task.ID
        o.mu.Unlock()

        // 注册事件监听
        o.watchTask(task.ID, pt.ID)
    }
}

// PollHealing 检查恢复队列
func (o *AutoLoopOrchestrator) PollHealing(ctx context.Context) {
    o.healer.ProcessPending(ctx)
}

// PollScale 检查扩缩容条件
func (o *AutoLoopOrchestrator) PollScale(ctx context.Context) {
    // 不自动缩容 manual 模式
    if o.mode == ModeManual {
        return
    }
    action := o.scaler.Evaluate(o, o.loopConfig, o.registry, o.clientPool)
    o.scaler.Execute(action)
}

// watchTask 监听单个 task 完成/失败事件
func (o *AutoLoopOrchestrator) watchTask(taskID string, planTaskID string) {
    go func() {
        // 通过 EventBus 监听 TaskCompleted/TaskFailed
        // 更新 planTask 状态
        // 检查 DAG 依赖解锁
        // 如果全部完成 → 触发完成事件
    }()
}

// checkDAG 检查 DAG 依赖，解锁 ready 任务
func (o *AutoLoopOrchestrator) checkDAG() {
    o.mu.Lock()
    defer o.mu.Unlock()

    if o.plan == nil {
        return
    }

    for _, task := range o.plan.Tasks {
        if task.Status != NodePending {
            continue
        }

        // 检查所有依赖是否 done
        allDone := true
        for _, depID := range task.Dependencies {
            dep := o.findPlanTask(depID)
            if dep == nil || dep.Status != NodeDone {
                allDone = false
                break
            }
        }

        if allDone {
            task.Status = NodeReady
        }
    }
}
```

### 3.2 ClientPool — `pkg/agent/client_pool.go`

```go
// ClientPool 管理 reef client 进程生命周期
type ClientPool struct {
    mu        sync.Mutex
    clients   map[string]*ManagedClient  // clientID → ManagedClient
    registry  *reefServer.Registry
    options   ClientPoolOptions
}

type ClientPoolOptions struct {
    ServerURL        string        // ws://host:port/ws
    Token            string        // 认证 token
    ReefBin          string        // reef 二进制路径
    BaseDir          string        // 临时 REEF_HOME 基础路径
    IdleTimeout      time.Duration // 空闲回收超时
    MaxClients       int           // 最大 client 数
    SpawnTimeout     time.Duration // spawn 等待超时
}

type ManagedClient struct {
    ClientID   string
    PID        int
    Role       string
    Skills     []string
    State      ManagedState      // starting/running/draining/stopped
    StartedAt  time.Time
    LastActive time.Time          // 最近一次任务完成时间
    HomeDir    string             // 临时 REEF_HOME 路径
    StopCh     chan struct{}
}

type ManagedState string
const (
    ManagedStarting ManagedState = "starting"
    ManagedRunning  ManagedState = "running"
    ManagedDraining ManagedState = "draining"
    ManagedStopped  ManagedState = "stopped"
)

// EnsureClient 确保存在可用的 role+skills client，不存在则启动
func (p *ClientPool) EnsureClient(ctx context.Context, role string, skills []string) (string, error) {
    // 1. 尝试从 Registry 获取可用 client (IsAvailable + Matches)
    // 2. 如果有 → 直接返回 clientID
    // 3. 如果无 → 检查是否达到 max_clients
    // 4. 未达到 → SpawnClient(role, skills)
    // 5. 等待注册完成 → 返回 clientID
}

// SpawnClient 启动一个新的 reef client 进程
func (p *ClientPool) SpawnClient(role string, skills []string) (*ManagedClient, error) {
    // 1. 创建临时 HOME: /tmp/reef-client-{ts}-{rand}/
    // 2. 创建 .reef/ 子目录
    // 3. 复制 config.json (模板)
    // 4. 复制 .security.yml (模板)
    // 5. 构建命令:
    //    setsid /path/reef client \
    //      --server {ServerURL} \
    //      --token {Token} \
    //      --role {role} \
    //      --skills {skills} \
    //      --capacity 2
    // 6. exec.Command + SysProcAttr.Setsid
    // 7. 轮询 Registry.Get(clientID) → ClientConnected (30s 超时)
    // 8. 返回 ManagedClient
}

// KillClient 终止并清理 client
func (p *ClientPool) KillClient(clientID string) error {
    // 1. 标记 Draining
    // 2. 等待 load=0 (30s 超时)
    // 3. syscall.SIGTERM → 等待 2s → syscall.SIGKILL
    // 4. Registry.Unregister(clientID)
    // 5. 清理临时 HOME 目录
}

// ListIdle 返回空闲超时的 client 列表
func (p *ClientPool) ListIdle(timeout time.Duration) []*ManagedClient {
}

// GetActiveCount 返回 running/draining 状态的 client 数
func (p *ClientPool) GetActiveCount() int {}
```

### 3.3 TaskPlanner — `pkg/agent/auto_planner.go`

```go
// TaskPlanner 解析用户指令并拆分为 DAG 任务
type TaskPlanner struct {
    llmProvider providers.Provider // 可选：用 LLM 智能拆分
}

// Parse 解析指令
func (tp *TaskPlanner) Parse(ctx context.Context, instruction string) (*ExecutionPlan, error) {
    // 策略:
    // 1. 简单指令 → 单任务
    // 2. 含"并"或"和"的指令 → 并行子任务
    // 3. 含"先...再..."或"然后" → 串行子任务 (DAG)
    // 4. 复杂指令 → 调用 LLM 解析
    // 5. 用户可通过 /auto run "..." 显式指定
}
```

### DAG 管理算法

**DetectCycles (Kahn 算法)**:
```
1. 计算每个节点的入度（Dependencies 列表长度）
2. 所有入度为 0 的节点入队列
3. 循环弹出队列节点 → 减少其下游节点入度 → 下游入度变为 0 时入队列
4. 处理完节点数 == 总节点数 → 无环
   否则 → 返回剩余节点（构成环的节点集）
```

**TopologicalSort**: 基于 Kahn 算法的 BFS 实现，按入度 0 → 入队顺序输出执行序列。

**checkDAG 依赖解锁（Orchestrator.PollQueue 调用）**: 每次 Poll 遍历所有 Pending 状态的 PlanTask，检查其 Dependencies。所有依赖完成 → Ready；有依赖失败 → Blocked。Ready 任务受 Parallelism 限制。

```

### 3.4 Healer — `pkg/agent/auto_healer.go`

```go
// Healer 监控任务和客户端状态，执行自愈操作
type Healer struct {
    mu          sync.Mutex
    pending     []HealOperation           // 等待执行的恢复操作
    policy      RetryPolicy
}

type HealOperation struct {
    TaskID      string
    PlanTaskID  string
    Action      HealAction               // retry/escalate/restart/abort
    Attempts    int
    LastAttempt time.Time
    Reason      string
}

type HealAction int
const (
    HealRetry    HealAction = iota  // 重试
    HealEscalate                   // 升级给管理员
    HealRestartClient              // 重启 client
    HealAbort                      // 放弃
    HealNone                       // 无需处理
)

// OnTaskFailed 处理任务失败
func (h *Healer) OnTaskFailed(task *reef.Task, planTask *PlannedTask) HealAction {
    // 1. attempts < maxRetries → HealRetry{ExcludeFailedClient}
    // 2. attempts >= maxRetries → HealEscalate 或 HealAbort
}

// OnClientStale 处理客户端心跳超时
func (h *Healer) OnClientStale(clientID string) HealAction {
    // 1. 获取 client 运行中的 tasks
    // 2. 调用 Scheduler.HandleTaskPaused
    // 3. 返回 HealRestartClient
}

// ProcessPending 处理 pending 队列
func (h *Healer) ProcessPending(ctx context.Context) {
    // 遍历 pending, 检查是否需要重试
    // 已到达重试时间的 → 执行恢复
}

// HealRetry 执行重试
func (h *Healer) healRetry(ctx context.Context, op HealOperation) error {
    // 1. 获取原 task 信息
    // 2. 创建新 Task （排除失败 client）
    // 3. 如果需要，调用 ClientPool 启动新 client
    // 4. Scheduler.Submit(newTask)
}
```

### 3.5 PersistentClient — 长时任务守护

```go
// PersistentClient 是对 ManagedClient 的扩展，用于长时间运行的任务（>5min）
// 与普通 ManagedClient 的区别：
//   - 心跳超时更长（5min vs 30s）
//   - 有独立的 Monitor goroutine 持续守护
//   - 断线后自动重建（检测 TaskPaused → 重新调度）
type PersistentClient struct {
    ManagedClient                      // 嵌入基础客户端
    HeartbeatTimeout time.Duration     // 默认 300s（5min）
    MonitorInterval  time.Duration     // 默认 60s 心跳检查间隔
    TaskID           string            // 守护的 reef.Task ID
    PlanTaskID       string            // 对应的 PlannedTask ID
    onDisconnect     func(clientID string, planTaskID string)
}

// NewPersistentClient 创建守护客户端
func NewPersistentClient(base *ManagedClient, taskID string, planTaskID string) *PersistentClient {
    return &PersistentClient{
        ManagedClient:    *base,
        HeartbeatTimeout: 5 * time.Minute,
        MonitorInterval:  60 * time.Second,
        TaskID:           taskID,
        PlanTaskID:       planTaskID,
    }
}

// Monitor 启动守护 goroutine，每 60s 检查心跳
// 如果心跳超时 5min → 调用 scheduler.HandleTaskPaused
// → 新建 PersistentClient → 重新调度任务
func (pc *PersistentClient) Monitor(ctx context.Context, scheduler *reefServer.Scheduler) {
    go func() {
        ticker := time.NewTicker(pc.MonitorInterval)
        defer ticker.Stop()
        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                // 检查客户端状态
                if time.Since(pc.LastActive) > pc.HeartbeatTimeout {
                    scheduler.HandleTaskPaused(pc.TaskID, "client heartbeat timeout")
                    // 触发重新调度
                    if pc.onDisconnect != nil {
                        pc.onDisconnect(pc.ClientID, pc.PlanTaskID)
                    }
                    return
                }
            }
        }
    }()
}
```

### 3.6 AutoScaler — `pkg/agent/auto_scaler.go`

```go
// AutoScaler 根据负载自动扩缩容
type AutoScaler struct {
    lastScaleUp   time.Time
    lastScaleDown time.Time
    cooldown      time.Duration  // 默认 30s 冷却
}

type ScaleAction struct {
    Type    ScaleType
    Count   int
    Role    string
    Skills  []string
    ClientID string
}

type ScaleType int
const (
    ScaleNone   ScaleType = iota
    ScaleUp
    ScaleDown
)

// Evaluate 评估是否需要扩缩容
func (as *AutoScaler) Evaluate(
    orch *AutoLoopOrchestrator,
    config LoopConfig,
    registry *reefServer.Registry,
    pool *ClientPool,
) ScaleAction {
    // 获取队列深度
    queueDepth := orch.QueueDepth()

    // 获取 active clients
    clients := pool.GetActiveClients()

    // 扩容条件:
    if queueDepth > config.Parallelism*2 && len(clients) < config.MaxClients {
        if time.Since(as.lastScaleUp) > as.cooldown {
            return ScaleAction{Type: ScaleUp, Count: 1, Role: "executor"}
        }
    }

    // 缩容条件:
    idleClients := pool.ListIdle(config.IdleTimeout)
    for _, c := range idleClients {
        if len(clients)-1 >= config.MinClients {
            return ScaleAction{Type: ScaleDown, ClientID: c.ClientID}
        }
    }

    return ScaleAction{Type: ScaleNone}
}
```

## 4. AgentLoop.Run() 修改 — `pkg/agent/agent.go`

```go
type AgentLoop struct {
    // ... 现有字段 ...
    orchestrator *AutoLoopOrchestrator  // 新增
}

func (al *AgentLoop) Run(ctx context.Context) error {
    // ... 现有初始化逻辑 ...

    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case msg := <-al.bus.InboundChan():
            // 现有消息处理逻辑...

            // 新增: Auto/Manual 模式路由
            if al.orchestrator != nil {
                mode := al.orchestrator.GetMode()
                if mode == ModeAuto || mode == ModeManual {
                    if !commands.IsCommand(msg.Content) {
                        // 非命令消息 → 入队
                        al.orchestrator.EnqueueMessage(msg.Content)
                        continue
                    }
                }
            }

        case <-ticker.C:
            // 现有心跳处理...

        case <-al.orchestrator.Tick():   // 新增: Orchestrator 轮询
            al.orchestrator.Poll(ctx)

        case <-al.orchestrator.Done():   // 新增: Orchestrator 完成
            al.orchestrator.OnComplete()
        }
    }
}
```

## 5. CLI 命令实现 — `pkg/commands/cmd_auto.go`

```go
package commands

import (
    "context"
    "fmt"
    "strconv"
    "strings"
)

func autoCommand() Definition {
    return Definition{
        Name:        "auto",
        Description: "Agent 自动循环执行控制",
        Usage:       "/auto <subcommand> [args]",
        SubCommands: []SubCommand{
            {Name: "mode",    Description: "切换模式 (chat/manual/auto)", Handler: handleAutoMode},
            {Name: "run",     Description: "提交并自动执行任务",          Handler: handleAutoRun},
            {Name: "step",    Description: "手动执行一步",                Handler: handleAutoStep},
            {Name: "loop",    Description: "自动循环 N 次",               Handler: handleAutoLoop},
            {Name: "stop",    Description: "停止执行",                   Handler: handleAutoStop},
            {Name: "status",  Description: "查看运行状态",                Handler: handleAutoStatus},
            {Name: "queue",   Description: "管理任务队列",                Handler: handleAutoQueue},
            {Name: "history", Description: "查看执行历史",                Handler: handleAutoHistory},
            {Name: "config",  Description: "查看/修改配置",               Handler: handleAutoConfig},
        },
    }
}

func handleAutoRun(ctx context.Context, req Request, rt *Runtime) error {
    if rt == nil || rt.Orchestrator == nil {
        return req.Reply("Orchestrator not available")
    }
    instruction := strings.TrimSpace(strings.TrimPrefix(req.Text, "/auto run"))
    if instruction == "" {
        return req.Reply("用法: /auto run <指令>")
    }

    plan, err := rt.Orchestrator.SubmitTask(ctx, instruction, AutoOnce, 0)
    if err != nil {
        return req.Reply("提交失败: " + err.Error())
    }

    return req.Reply(fmt.Sprintf(
        "✅ 任务已提交\n📋 计划 ID: %s\n🔢 子任务数: %d\n👤 自动管理客户端: 是\n🔧 自愈: 是",
        plan.ID, len(plan.Tasks)))
}

// ... 其他 handler 类似 ...
```

## 6. Runtime 扩展

```go
// pkg/commands/commands.go
type Runtime struct {
    // ... 已有字段 ...
    Orchestrator *AgentLoopOrchestrator  // 新增
}
```

## 7. 初始化流程 — `pkg/agent/agent_init.go`

```go
func NewAgentLoop(...) *AgentLoop {
    // ... 现有初始化 ...

    // 创建 AutoLoopOrchestrator
    orchestrator := NewAutoLoopOrchestrator(scheduler, registry, eventBus)
    al.orchestrator = orchestrator

    return al
}
```

## 8. 状态查看示例

```
用户: /auto status

Agent: 📊 AutoLoop Orchestrator 状态
┌──────────────────────┬─────────────────────────────────┐
│ 当前模式              │ auto                            │
│ Orchestrator 状态     │ dispatching                     │
│ 执行计划 ID           │ plan-3                          │
│ 子任务总数/已完成     │ 7 / 3                           │
│ 正在运行              │ 编译 order-svc (plan-3-2)       │
│                       │ 部署 user-svc  (plan-3-4)       │
│ 队列深度              │ 2                               │
├──────────────────────┼─────────────────────────────────┤
│ 活动客户端            │ 2                               │
│  ─ coder-1 (coder)   │ load: 2/2, uptime: 12m          │
│  ─ ops-1 (ops)       │ load: 1/2, uptime: 5m           │
│ 空闲客户端            │ 1 (executor-1, idle 3m)         │
├──────────────────────┼─────────────────────────────────┤
│ 已完成任务            │ 42                              │
│ 失败任务              │ 3 (自愈成功 3/3)                │
│ 自愈次数              │ 3                               │
│ 扩缩容次数            │ 2↗ 1↘                           │
└──────────────────────┴─────────────────────────────────┘
```

## 9. 配置查看示例

```
用户: /auto config

Agent: ⚙️ 当前运行配置
┌──────────────────┬──────────┬──────────┐
│ 参数              │ 当前值    │ 默认值    │
├──────────────────┼──────────┼──────────┤
│ parallelism      │ 3        │ 3        │
│ auto_client      │ true     │ true     │
│ min_clients      │ 1        │ 1        │
│ max_clients      │ 5        │ 5        │
│ idle_timeout     │ 5m0s     │ 5m0s     │
│ retry_max        │ 3        │ 3        │
│ retry_delay      │ 5s       │ 5s       │
│ retry_backoff    │ 2.0      │ 2.0      │
└──────────────────┴──────────┴──────────┘

用户: /auto config max_clients=8
Agent: ✓ max_clients: 5 → 8
```

## 10. 实现路线图

| 阶段 | 内容 | 文件 | 依赖 |
|------|------|------|------|
| Phase 1 | 核心数据类型 + ConversationMode 扩展 | auto_orchestrator.go, conversation_mode.go | 无 |
| Phase 2 | ClientPool (spawn/kill/monitor) | client_pool.go | Phase 1 |
| Phase 3 | TaskPlanner (指令解析 + DAG) | auto_planner.go | Phase 1 |
| Phase 4 | Orchestrator Poll 循环 (PollQueue/PollScale/PollHealing) | auto_orchestrator.go | Phase 1-3 |
| Phase 5 | Healer (task retry/client restart) | auto_healer.go | Phase 1-2 |
| Phase 5.5 | PersistentClient（长时任务守护） | auto_orchestrator.go | Phase 5 |
| Phase 6 | AutoScaler (扩缩容决策) | auto_scaler.go | Phase 1-2 |
| Phase 7 | CLI 命令 (/auto 家族) | cmd_auto.go | Phase 1-6 |
| Phase 8 | AgentLoop 集成 (Run/Ticker/路由) | agent.go, agent_init.go | Phase 1-7 |
| Phase 9 | 测试 | auto_*_test.go, cmd_auto_test.go | Phase 1-8 |
| Phase 10 | 边界处理 + 文档 | — | Phase 1-9 |

## 11. 架构决策记录 (ADR)

### ADR-1: 重试层次协调

**背景**: 现有架构中存在三层重试机制：客户端 TaskRunner (maxRetries=3)、Scheduler escalation (maxEscalations=2)、新增加的 Healer 服务端重试。如果不协调，同一失败任务可能被尝试 N×(M+K) 次。

**决策**:
1. 当 Orchestrator 管理模式激活时，Scheduler 的 `maxEscalations` 设为 0（禁用 Scheduler 级别的重新分配）
2. 客户端 TaskRunner 的 `maxRetries` 设为 1（仅重试一次瞬态错误）
3. Healer 作为**唯一的服务端重试入口**，`maxRetries` 设为 3（含指数退避）
4. Healer 每次重试前通过 `ClientPool.EnsureClient(excludeClientID)` 确保分配到不同的客户端

**重试决策树**:
```
TaskFailed
  → TaskRunner 已重试 1 次（客户端侧）
  → Scheduler 未重新分配（maxEscalations=0）
  → Healer 收到 EventBus TaskFailed
    ├─ attempts < 3 → HealRetry(ExcludeFailedClient)
    │   → ClientPool.EnsureClient（排除失败 client）
    │   → Scheduler.Submit(newTask)
    │
    ├─ attempts >= 3 && < 5 → HealEscalate
    │   → EventBus 通知管理员
    │   → 计划标记为 PlanFailed
    │
    └─ attempts >= 5 → HealAbort
        → 计划标记为 TerminalFailed
        → 所有下游依赖标记为 Blocked
```

**替代方案**: 不协调，各自独立重试 → 最多 3×3×3=27 次尝试（拒绝）
**后果**: 减少无效重试次数 67%，单任务最多尝试 4 次（1 次 TaskRunner + 3 次 Healer）

### ADR-2: EventBus（进程级）vs HookManager（turn 级）的分工

**背景**: design 中 Orchestrator 通过 EventBus 订阅 `TaskCompleted`/`TaskFailed`/`ClientDisconnected` 事件。现有 `HookManager`（PreTurn/PostTurn/PreTool/PostTool）是 AgentLoop 内部的 turn 级钩子。

**决策**: 两者是不同层面的抽象，在设计中共存：

| | EventBus | HookManager |
|---|---------|------------|
| 作用域 | 进程级全局事件 | AgentLoop 单个 turn 生命周期 |
| 事件类型 | Task 完成/失败、Client 连接/断开 | Turn 开始/结束、Tool 调用前/后 |
| 消费者 | Orchestrator、Healer、Logger | 反射器、性能监控、安全审计 |
| 数据量 | 低频（每秒 0-10 次） | 高频（每秒 10-100 次） |
| 是否持久化 | 否（内存中 fire-and-forget） | 否 |

Orchestrator **只订阅 EventBus**，不依赖 HookManager，保持职责清晰。

### ADR-3: TaskPlanner LLM 依赖注入

**背景**: TaskPlanner 需要解析复杂指令（含 DAG 依赖关系），需要 LLM 辅助。但 TaskPlanner 在 Orchestrator 初始化时创建，此时 AgentLoop 的 providerFactory 尚未可用。

**决策**: TaskPlanner 不依赖 AgentLoop。LLM 访问通过独立注入：

```go
type TaskPlanner struct {
    llmProvider providers.Provider // 可选，为空时使用规则解析降级
}

func NewTaskPlanner(modelCfg *config.ModelConfig) *TaskPlanner {
    // 独立创建 Provider，不依赖 AgentLoop
    provider, err := providers.CreateProviderFromConfig(modelCfg)
    if err != nil {
        logger.Warn("TaskPlanner LLM unavailable, using rule-based fallback")
        return &TaskPlanner{llmProvider: nil}
    }
    return &TaskPlanner{llmProvider: provider}
}
```

初始化顺序：`config.LoadModelConfig` → 读取 model_list → 创建 TaskPlanner（含独立 Provider）→ 创建 ClientPool → 创建 Healer/Scaler → 创建 Orchestrator → 注入 AgentLoop
