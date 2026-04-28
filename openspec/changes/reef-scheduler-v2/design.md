---
change: reef-scheduler-v2
artifact: design
---

# Design: Reef Scheduler v2 — 统一设计方案

## 0. 数据目录统一

### 0.1 当前问题

```
~/.picoclaw/                    ← 数据目录（在用户主目录下，与程序分离）
├── config.json
├── .security.yml
├── .picoclaw.pid
├── auth.json
├── logs/
├── workspace/
└── skills/

/usr/local/bin/picoclaw         ← 可执行文件（在 PATH 中）
```

问题：
- 数据分散在用户主目录，不方便迁移和部署
- 多实例部署时冲突（共享 ~/.picoclaw）
- 不符合嵌入式设备（如 LicheeRV Nano）的单目录部署习惯
- Reef Server 的 SQLite 数据库路径需要单独指定

### 0.2 新方案：数据目录与程序目录一致

```
/path/to/picoclaw/              ← 程序目录 = 数据目录
├── picoclaw                    ← 可执行文件
├── config.json                 ← 配置文件
├── .security.yml               ← 安全配置
├── .picoclaw.pid               ← PID 文件
├── auth.json                   ← 认证存储
├── logs/                       ← 日志
├── workspace/                  ← 工作空间
├── skills/                     ← 技能
├── reef.db                     ← Reef SQLite 数据库（新增）
├── channels/                   ← 频道缓存
└── ...
```

### 0.3 目录解析优先级

```
1. $PICOCLAW_HOME 环境变量（显式覆盖，最高优先级）
2. 可执行文件所在目录（os.Executable() 的 Dir，需可写）
3. ~/.picoclaw（降级回退，保持兼容）
```

### 0.4 代码变更

```go
// pkg/config/envkeys.go — GetHome() 修改

func GetHome() string {
    // 1. 显式环境变量覆盖
    if picoclawHome := os.Getenv(EnvHome); picoclawHome != "" {
        return picoclawHome
    }

    // 2. 可执行文件所在目录（新默认值）
    if exe, err := os.Executable(); err == nil {
        exeDir := filepath.Dir(exe)
        // 验证目录可写（避免 /usr/bin 等系统目录）
        if isWritableDir(exeDir) {
            return exeDir
        }
    }

    // 3. 降级回退到用户主目录（保持兼容）
    homePath, _ := os.UserHomeDir()
    if homePath != "" {
        return filepath.Join(homePath, pkg.DefaultPicoClawHome)
    }

    return "."
}

// isWritableDir 检查目录是否可写
func isWritableDir(dir string) bool {
    testFile := filepath.Join(dir, ".picoclaw.write-test")
    if err := os.WriteFile(testFile, []byte{}, 0600); err != nil {
        return false
    }
    os.Remove(testFile)
    return true
}
```

### 0.5 受影响的路径解析

| 文件 | 旧路径 | 新路径（基于 GetHome） |
|------|--------|----------------------|
| config.json | ~/.picoclaw/config.json | {GetHome()}/config.json |
| .security.yml | ~/.picoclaw/.security.yml | {GetHome()}/.security.yml |
| .picoclaw.pid | ~/.picoclaw/.picoclaw.pid | {GetHome()}/.picoclaw.pid |
| auth.json | ~/.picoclaw/auth.json | {GetHome()}/auth.json |
| workspace/ | ~/.picoclaw/workspace/ | {GetHome()}/workspace/ |
| skills/ | ~/.picoclaw/skills/ | {GetHome()}/skills/ |
| logs/ | ~/.picoclaw/logs/ | {GetHome()}/logs/ |
| reef.db | — | {GetHome()}/reef.db |
| wecom/reqid-store.json | ~/.picoclaw/wecom/ | {GetHome()}/wecom/ |

### 0.6 Reef Server 数据目录

```go
// pkg/reef/server/server.go

func DefaultConfig() Config {
    homeDir := config.GetHome()  // 复用 picoclaw 的目录解析逻辑
    return Config{
        // ...
        StorePath: filepath.Join(homeDir, "reef.db"),  // 默认 SQLite 路径
        // ...
    }
}
```

### 0.7 迁移兼容

- **PICOCLAW_HOME 环境变量**：显式设置时优先级最高，行为完全不变
- **~/.picoclaw 降级**：可执行文件目录不可写时（如 /usr/bin），自动回退到 ~/.picoclaw
- **首次使用提示**：如果检测到 ~/.picoclaw 存在但程序目录下没有 config.json，日志提示可选迁移
- **不破坏现有部署**：设置 `PICOCLAW_HOME=~/.picoclaw` 即可保持旧行为

---

## 0.8 Hermes 能力架构（重要新增）

### 0.8.1 核心问题

Server 端 AgentLoop 没有能力边界约束，LLM 倾向于直接使用 web_search/exec/read_file 等工具处理任务，即使有合适的 Client 在线也不会分发 → 团队作战名存实亡。

### 0.8.2 三角色能力模型

| 维度 | Coordinator（协调者） | Executor（执行者） | Full（全能） |
|------|----------------------|-------------------|-------------|
| 触发 | `picoclaw server` | Client 连接到 Server | `picoclaw`（默认） |
| Tool 集 | reef_submit/query/status + message/reaction/cron | 全部工具（禁用 reef_submit） | 全部工具 |
| SubTurn | ❌ | ✅ | ✅ |
| ReefSubmit | ✅ | ❌ | ❌ |

### 0.8.3 三层防绕过约束链

```
Layer 1: PromptContributor → 注入协调者身份 (软约束, ~95%)
Layer 2: 条件注册 → 只注册允许的工具 (硬约束, 100%)
Layer 3: HermesGuard → 降级时动态放行 (动态约束)
```

### 0.8.4 降级策略

- Coordinator + 有 Client → 正常协调
- Coordinator + 无 Client + fallback=true → 降级为 Full
- Coordinator + 无 Client + fallback=false → 等待提示

详细设计见 `openspec/changes/reef-hermes-architecture/RESEARCH_REPORT.md`

---

## 1. 架构总览

```
┌──────────────────────────────────────────────────────────────────────────┐
│                        Reef Server (picoclaw)                            │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │               picoclaw Gateway（复用，可选启用）                     │  │
│  │                                                                    │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐            │  │
│  │  │ FeishuChannel│  │  WeixinCh.   │  │ TelegramCh.  │  ...       │  │
│  │  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘            │  │
│  │         │                  │                  │                    │  │
│  │  ┌──────▼──────────────────▼──────────────────▼────────────────┐  │  │
│  │  │                    MessageBus                                │  │  │
│  │  └──────────────────────┬──────────────────────────────────────┘  │  │
│  │                         │                                          │  │
│  │  ┌──────────────────────▼──────────────────────────────────────┐  │  │
│  │  │                    AgentLoop                                 │  │  │
│  │  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐ │  │  │
│  │  │  │   LLM       │  │ Tools/Skills│  │ ReefSwarmTool       │ │  │  │
│  │  │  │  (Provider)  │  │ (现有工具集) │  │ (提交/查询任务)      │ │  │  │
│  │  │  └─────────────┘  └─────────────┘  └──────────┬──────────┘ │  │  │
│  │  └───────────────────────────────────────────────│─────────────┘  │  │
│  │                                                   │                │  │
│  │  ┌────────────────────────────────────────────────▼─────────────┐ │  │
│  │  │        ChannelManager (outbound) → 回传结果到飞书/微信/...    │ │  │
│  │  └──────────────────────────────────────────────────────────────┘ │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │              Reef 调度层（保留+增强）                                │  │
│  │                                                                    │  │
│  │  ┌──────────────┐  ┌──────────────┐  ┌────────────────────────┐  │  │
│  │  │  Scheduler   │  │ PriorityQueue│  │   MatchStrategy        │  │  │
│  │  │  (增强保留)   │  │  (升级替代)   │  │   (新增可插拔)         │  │  │
│  │  └──────┬───────┘  └──────────────┘  └────────────────────────┘  │  │
│  │         │                                                          │  │
│  │  ┌──────▼───────┐  ┌──────────────┐  ┌────────────────────────┐  │  │
│  │  │ DAG Engine   │  │ Timeout      │  │ RecoveryManager        │  │  │
│  │  │ (新增)       │  │ Scanner(新增) │  │ (新增)                 │  │  │
│  │  └──────────────┘  └──────────────┘  └────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │              TaskStore (SQLite)（扩展保留）                         │  │
│  │  tasks | task_results | task_errors | task_attempts                │  │
│  │  task_relations (新增) | task_reply_to (新增)                      │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│                                                                          │
│  ┌──────────┐  ┌──────────────┐  ┌─────────────────────────────────┐  │
│  │ Registry  │  │  WebSocket   │  │  Admin API (保留) + Web UI      │  │
│  │ (保留)    │  │  (保留)      │  │  (保留独立 /ui + 融入 picoclaw) │  │
│  └──────────┘  └──────────────┘  └─────────────────────────────────┘  │
│                                                                          │
│  ┌────────────────────────────────────────────────────────────────────┐  │
│  │              NotifyManager (保留)                                   │  │
│  │  Webhook | Slack | SMTP | Feishu | WeCom                           │  │
│  └────────────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────────────┘
```

## 2. 完整数据流

### 2.1 用户 → Server → Client → Server → 用户

```
1. 用户在飞书发消息："帮我分析这个项目的代码质量"
2. Server 的 FeishuChannel 收到 → MessageBus → AgentLoop
3. AgentLoop + LLM："这是复杂任务，需要分解"
4. AgentLoop 调用 ReefSwarmTool.submit_task() × N
   → Scheduler.Submit(parentTask + 子任务)
   → DAG Engine 创建子任务 + 依赖
5. Scheduler 分发到 Client：
   Client-A ← SubTask-1 (analyzer, 无依赖)
   Client-B ← SubTask-2 (reviewer, 依赖 SubTask-1)
   Client-C ← SubTask-3 (writer, 依赖 SubTask-2)
6. Client-A 完成 → DAGEngine.OnSubTaskCompleted()
   → SubTask-2 解除阻塞 → 分发到 Client-B
7. Client-B 完成 → SubTask-3 解除阻塞 → 分发到 Client-C
8. Client-C 完成 → 所有子任务完成
9. DAG Engine 构造聚合 InboundMessage → AgentLoop + LLM 聚合
10. AgentLoop 生成最终回复 → MessageBus outbound → FeishuChannel → 飞书
11. 用户看到完整分析报告 ✅
```

### 2.2 简单问题 Server 直接回复

```
1. 用户发 "你好" → FeishuChannel → MessageBus → AgentLoop
2. AgentLoop + LLM："简单问候，直接回复"
3. AgentLoop 生成回复 → MessageBus outbound → FeishuChannel → 飞书
4. 不经过 Client，不创建任务 ✅
```

### 2.3 API 直接提交任务（无 Gateway）

```
1. POST /tasks {instruction, required_role, ...}
2. Scheduler.Submit(task) → PriorityQueue → TryDispatch()
3. Client 执行 → task_completed → Scheduler.HandleTaskCompleted()
4. 结果通过 API 查询 / Admin API 返回
5. 如果配置了 NotifyManager → 发送告警通知
```

## 3. 现有组件保留与变更

### 3.1 Scheduler（增强保留）

```go
// 保留的公开 API（不变）：
func (s *Scheduler) Submit(task *reef.Task) error
func (s *Scheduler) TryDispatch()
func (s *Scheduler) HandleTaskCompleted(taskID string, result *reef.TaskResult) error
func (s *Scheduler) HandleTaskFailed(taskID string, taskErr *reef.TaskError, attemptHistory []reef.AttemptRecord) error
func (s *Scheduler) HandleClientAvailable(clientID string)
func (s *Scheduler) GetTask(taskID string) *reef.Task
func (s *Scheduler) TasksSnapshot() []*reef.Task

// 新增字段：
type Scheduler struct {
    registry  *Registry          // 保留
    queue     *PriorityQueue     // 变更：FIFO → PriorityQueue
    store     store.TaskStore    // 新增：持久化存储
    strategy  MatchStrategy      // 新增：可插拔策略
    dagEngine *DAGEngine         // 新增：DAG 编排
    channelMgr *ChannelManager   // 新增：频道管理（可选，Gateway 启用时）
    // ... 保留原有字段
}

// 新增方法：
func (s *Scheduler) TasksByStatus(status reef.TaskStatus) []*reef.Task
func (s *Scheduler) Requeue(task *reef.Task)
func (s *Scheduler) TrackRecovering(task *reef.Task)
```

### 3.2 Queue → PriorityQueue（升级替代）

```go
// 保留 Queue 接口（queue_iface.go），PriorityQueue 实现该接口
type Queue interface {
    Enqueue(task *reef.Task) error
    Dequeue() *reef.Task
    Peek() *reef.Task
    Len() int
    Snapshot() []*reef.Task
}

// PriorityQueue 新增方法：
func (pq *PriorityQueue) Scan(matchFn func(*reef.Task) bool) []*reef.Task
func (pq *PriorityQueue) Remove(taskID string) bool
func (pq *PriorityQueue) BoostStarvation(threshold time.Duration, boost, maxPriority int)
```

### 3.3 TaskStore（扩展保留）

```go
// 保留原有方法（签名不变）：
SaveTask(task *reef.Task) error
GetTask(id string) (*reef.Task, error)
UpdateTask(task *reef.Task) error
DeleteTask(id string) error
ListTasks(filter TaskFilter) ([]*reef.Task, error)
SaveAttempt(taskID string, attempt reef.AttemptRecord) error
GetAttempts(taskID string) ([]reef.AttemptRecord, error)
Close() error

// 新增方法：
UpdateTaskStatus(id string, status reef.TaskStatus, fields map[string]any) error
ListActiveTasks() ([]*reef.Task, error)
SaveResult(taskID string, result *reef.TaskResult) error
SaveReplyTo(taskID string, replyTo *reef.ReplyToContext) error
GetReplyTo(taskID string) (*reef.ReplyToContext, error)
SaveRelation(parentID, childID, dependency string) error
GetSubTaskIDs(parentID string) ([]string, error)
DeleteOldTasks(before time.Time) error
```

### 3.4 Admin API（扩展保留）

```go
// 保留现有端点：
/admin/status    → GET  服务器状态
/admin/tasks     → GET  任务列表
/tasks           → POST 提交任务

// 新增端点：
/tasks/{id}/subtasks → GET  子任务列表
/tasks/{id}/cancel   → POST 取消任务
/tasks/{id}/retry    → POST 重试任务
```

### 3.5 NotifyManager（完整保留）

```go
// 保留所有现有 Notifier：
WebhookNotifier    → 保留
SlackNotifier      → 保留
SMTPNotifier       → 保留
FeishuNotifier     → 保留（告警通知）
WeComNotifier      → 保留

// 新增事件类型（扩展 Alert）：
Alert.Event = "task_completed"  // 任务完成通知
Alert.Event = "task_failed"    // 任务失败通知
// 原有 "task_escalated" 保留
```

### 3.6 WebSocket Server（扩展保留）

```go
// 保留所有现有消息类型和处理逻辑
// 新增：
// 1. 幂等检查：HandleTaskCompleted/Failed 先检查终态
// 2. ReplyTo 透传：task_dispatch 携带 reply_to
// 3. 恢复确认：MsgTaskStatusQuery/MsgTaskStatusResponse
```

### 3.7 Protocol（扩展保留）

```go
// 保留所有现有 MessageType（MsgRegister ~ MsgControlAck）
// TaskDispatchPayload 新增字段：
type TaskDispatchPayload struct {
    // ... 原有字段保留 ...
    ReplyTo *reef.ReplyToContext `json:"reply_to,omitempty"` // 新增
}

// 新增消息类型：
MsgTaskStatusQuery    MessageType = "task_status_query"
MsgTaskStatusResponse MessageType = "task_status_response"
```

## 4. 新增组件设计

### 4.1 GatewayBridge

```go
// pkg/reef/server/gateway.go
type GatewayBridge struct {
    msgBus    *bus.MessageBus
    agentLoop *agent.AgentLoop
    chanMgr   *channels.Manager
    provider  providers.LLMProvider
    scheduler *Scheduler
    logger    *slog.Logger
}

type GatewayConfig struct {
    Enabled   bool                `json:"enabled"`
    ModelName string              `json:"model_name"`
    Channels  *config.ChannelsConfig `json:"channels,omitempty"`
}

func NewGatewayBridge(cfg GatewayConfig, pCfg *config.Config, scheduler *Scheduler, logger *slog.Logger) (*GatewayBridge, error)
func (gb *GatewayBridge) Start(ctx context.Context) error
func (gb *GatewayBridge) Stop(ctx context.Context) error
```

### 4.2 ReefSwarmTool + ReefQueryTool

```go
// pkg/reef/server/reef_tool.go

// ReefSwarmTool — AgentLoop 提交任务到调度器
type ReefSwarmTool struct {
    scheduler *Scheduler
    msgBus    *bus.MessageBus  // 用于获取 InboundMessage 中的来源上下文
}

// Tool 定义：
// Name: "reef_submit_task"
// Parameters: instruction, required_role, required_skills, priority, timeout_ms

// ReefQueryTool — AgentLoop 查询任务状态
type ReefQueryTool struct {
    scheduler *Scheduler
}

// Tool 定义：
// Name: "reef_query_task"
// Parameters: task_id
```

### 4.3 DAG Engine

```go
// pkg/reef/server/dag_engine.go
type DAGEngine struct {
    store     store.TaskStore
    scheduler *Scheduler
    msgBus    *bus.MessageBus  // 聚合时构造 InboundMessage
    logger    *slog.Logger
}

func (e *DAGEngine) CreateSubTasks(parentTask *reef.Task, plan *Plan) error
func (e *DAGEngine) OnSubTaskCompleted(subTaskID string) error
func (e *DAGEngine) OnSubTaskFailed(subTaskID string) error
func (e *DAGEngine) CheckUnblock(parentTaskID string)
func (e *DAGEngine) OnAllSubTasksCompleted(parentTaskID string)
```

### 4.4 PriorityQueue

```go
// pkg/reef/server/priority_queue.go
// 实现 Queue 接口 + container/heap
type PriorityQueue struct {
    mu      sync.Mutex
    items   []*queueItem
    counter int64  // 入队序号
}

type queueItem struct {
    task  *reef.Task
    order int64
}
```

### 4.5 MatchStrategy

```go
// pkg/reef/server/strategy.go
type MatchStrategy interface {
    Name() string
    Match(task *reef.Task, candidates []*reef.ClientInfo, excludeID string) *reef.ClientInfo
}

type LeastLoadStrategy struct{}     // 默认，保持当前行为
type RoundRobinStrategy struct{}    // 轮询
type AffinityStrategy struct{}      // 亲和性
```

### 4.6 TimeoutScanner

```go
// pkg/reef/server/timeout_scanner.go
type TimeoutScanner struct {
    scheduler *Scheduler
    store     store.TaskStore
    interval  time.Duration
    logger    *slog.Logger
}

func (ts *TimeoutScanner) Run(ctx context.Context)
```

### 4.7 RecoveryManager

```go
// pkg/reef/server/recovery.go
type RecoveryManager struct {
    scheduler *Scheduler
    dagEngine *DAGEngine
    registry  *Registry
    store     store.TaskStore
    logger    *slog.Logger
}

func (rm *RecoveryManager) Recover(ctx context.Context) error
```

## 5. 数据模型

### 5.1 SQLite Schema（扩展保留现有表）

```sql
-- tasks 表：保留现有字段，新增字段
CREATE TABLE IF NOT EXISTS tasks (
    id            TEXT PRIMARY KEY,
    status        TEXT NOT NULL DEFAULT 'queued',
    priority      INTEGER NOT NULL DEFAULT 5,       -- 新增
    instruction   TEXT NOT NULL,
    required_role TEXT NOT NULL DEFAULT '',
    required_skills TEXT NOT NULL DEFAULT '[]',
    max_retries   INTEGER NOT NULL DEFAULT 3,
    timeout_ms    INTEGER NOT NULL DEFAULT 300000,
    model_hint    TEXT NOT NULL DEFAULT '',
    assigned_client TEXT NOT NULL DEFAULT '',
    escalation_count INTEGER NOT NULL DEFAULT 0,
    parent_task_id TEXT NOT NULL DEFAULT '',         -- 新增
    aggregation_hint TEXT NOT NULL DEFAULT '',       -- 新增
    decompose     INTEGER NOT NULL DEFAULT 1,        -- 新增
    created_at    INTEGER NOT NULL,
    assigned_at   INTEGER,
    started_at    INTEGER,
    completed_at  INTEGER,
    pause_reason  TEXT NOT NULL DEFAULT ''
);

-- 保留现有表
CREATE TABLE IF NOT EXISTS task_results (...);
CREATE TABLE IF NOT EXISTS task_errors (...);
CREATE TABLE IF NOT EXISTS task_attempts (...);

-- 新增表
CREATE TABLE IF NOT EXISTS task_relations (
    parent_id    TEXT NOT NULL REFERENCES tasks(id),
    child_id     TEXT NOT NULL REFERENCES tasks(id),
    dependency   TEXT NOT NULL DEFAULT '',
    order_index  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (parent_id, child_id)
);

CREATE TABLE IF NOT EXISTS task_reply_to (
    task_id     TEXT PRIMARY KEY REFERENCES tasks(id),
    channel     TEXT NOT NULL,
    chat_id     TEXT NOT NULL,
    message_id  TEXT NOT NULL DEFAULT '',
    sender_id   TEXT NOT NULL DEFAULT '',
    extra       TEXT NOT NULL DEFAULT '{}'
);

-- 新增索引
CREATE INDEX IF NOT EXISTS idx_tasks_priority ON tasks(priority DESC, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_tasks_parent ON tasks(parent_task_id);
CREATE INDEX IF NOT EXISTS idx_reply_to_channel ON task_reply_to(channel, chat_id);
```

### 5.2 Task 模型扩展

```go
type Task struct {
    // ... 原有字段全部保留 ...
    Priority        int              // 新增：1-10, default 5
    ParentTaskID    string           // 新增
    SubTaskIDs      []string         // 新增
    Dependencies    []string         // 新增
    AggregationHint string           // 新增
    Decompose       bool             // 新增
    EffectivePriority int            // 新增：含饥饿提升
    ReplyTo         *ReplyToContext   // 新增
}

type ReplyToContext struct {
    Channel   string `json:"channel"`
    ChatID    string `json:"chat_id"`
    MessageID string `json:"message_id"`
    SenderID  string `json:"sender_id"`
    Extra     map[string]string `json:"extra,omitempty"`
}

// 新增状态
const TaskBlocked     TaskStatus = "Blocked"
const TaskRecovering  TaskStatus = "Recovering"
const TaskAggregating TaskStatus = "Aggregating"
```

## 6. Web UI 统一设计

### 6.1 现状

| 组件 | 位置 | 技术栈 |
|------|------|--------|
| picoclaw Web UI | `web/frontend/` | React + TanStack Router + shadcn/ui + Vite |
| Reef 仪表盘 | `pkg/reef/server/ui/static/` | 独立 HTML/CSS/JS SPA |

### 6.2 融合方案

**在 picoclaw Web 前端新增 Reef 页面：**

```
web/frontend/src/
├── routes/
│   ├── reef.tsx                    ← 新增：Reef 布局路由
│   ├── reef/
│   │   ├── index.tsx               ← 新增：Overview 页面
│   │   ├── tasks.tsx               ← 新增：Tasks 页面
│   │   └── clients.tsx             ← 新增：Clients 页面
├── components/
│   ├── reef/                       ← 新增：Reef 组件
│   │   ├── overview-cards.tsx      ← 状态卡片
│   │   ├── task-table.tsx          ← 任务表格
│   │   ├── task-detail-sheet.tsx   ← 任务详情抽屉
│   │   ├── client-table.tsx        ← Client 表格
│   │   ├── task-submit-dialog.tsx  ← 提交任务对话框
│   │   └── use-reef-events.ts      ← SSE 事件 Hook
├── api/
│   └── reef.ts                     ← 新增：Reef API 客户端
```

**侧边栏新增导航组：**

```tsx
// app-sidebar.tsx 新增
{
  label: "navigation.reef_group",
  defaultOpen: true,
  items: [
    { title: "navigation.reef_overview", url: "/reef", icon: IconServer },
    { title: "navigation.reef_tasks", url: "/reef/tasks", icon: IconListCheck },
    { title: "navigation.reef_clients", url: "/reef/clients", icon: IconDevices },
  ],
}
```

**Reef API 注册到 picoclaw Web 后端：**

```go
// web/backend/api/router.go 新增
func (h *Handler) registerReefRoutes(mux *http.ServeMux) {
    // 代理到 Reef Server 的 Admin API
    // 或者直接调用 Scheduler/Registry（如果 Reef Server 嵌入同一进程）
}
```

### 6.3 降级方案

- 独立 `/ui` 路径保留，不依赖 picoclaw Web 前端
- `pkg/reef/server/ui/` 代码和静态文件保留
- 未构建 picoclaw 前端时，Reef 仪表盘仍可通过 `/ui` 访问

### 6.4 SSE 事件流

picoclaw Web 前端通过 SSE 获取实时更新：

```
/api/reef/events → SSE
  event: stats_update    → 状态统计刷新
  event: task_created    → 新任务创建
  event: task_completed  → 任务完成
  event: task_failed     → 任务失败
  event: client_connected    → Client 上线
  event: client_disconnected → Client 下线
```

## 7. Server 启动流程

```go
func NewServer(cfg Config, logger *slog.Logger) *Server {
    s := &Server{config: cfg, logger: logger}

    // === 保留原有初始化 ===
    s.registry = NewRegistry(...)
    
    // Task queue — 升级为 PriorityQueue
    s.queue = NewPriorityQueue(cfg.QueueMaxLen)
    
    // TaskStore — 扩展
    var taskStore store.TaskStore
    if cfg.StoreType == "sqlite" {
        taskStore, _ = store.NewSQLiteStore(cfg.StorePath)
    } else {
        taskStore = store.NewMemoryStore()
    }
    s.store = taskStore

    // NotifyManager — 保留
    notifyMgr := notify.NewManager(logger)
    // ... 注册 Notifier（保留原有逻辑）

    // Scheduler — 增强
    s.scheduler = NewScheduler(s.registry, s.queue, SchedulerOptions{
        Store:             taskStore,        // 新增
        Strategy:          strategy,         // 新增
        MaxEscalations:    cfg.MaxEscalations,
        NotifyManager:     notifyMgr,
        OnDispatch:        ...,              // 保留
        OnRequeue:         ...,              // 保留
    })

    // DAG Engine — 新增
    s.dagEngine = NewDAGEngine(taskStore, s.scheduler, nil, logger)

    // TimeoutScanner — 新增
    s.timeoutScanner = NewTimeoutScanner(s.scheduler, taskStore, cfg.SchedulerConfig.TimeoutScanInterval, logger)

    // RecoveryManager — 新增
    s.recoveryManager = NewRecoveryManager(s.scheduler, s.dagEngine, s.registry, taskStore, logger)

    // WebSocket Server — 保留
    s.wsServer = NewWebSocketServer(s.registry, s.scheduler, cfg.Token, logger)

    // Admin Server — 保留
    s.admin = NewAdminServer(s.registry, s.scheduler, cfg.Token, logger)

    // Web UI — 保留
    s.ui = ui.NewHandler(s.registry, s.scheduler, time.Now(), logger)

    // === 新增：Gateway 桥接（可选） ===
    if cfg.Gateway.Enabled {
        s.gateway, _ = NewGatewayBridge(cfg.Gateway, cfg.PicoclawConfig, s.scheduler, logger)
    }

    return s
}

func (s *Server) Start() error {
    // ... 保留原有启动逻辑 ...

    // 新增：RecoveryManager 恢复
    if err := s.recoveryManager.Recover(ctx); err != nil {
        s.logger.Warn("recovery failed", ...)
    }

    // 新增：TimeoutScanner 启动
    go s.timeoutScanner.Run(ctx)

    // 新增：Gateway 启动（如果启用）
    if s.gateway != nil {
        if err := s.gateway.Start(ctx); err != nil {
            return err
        }
    }

    return nil
}
```

## 8. 文件变更清单

| 文件 | 操作 | 说明 |
|------|------|------|
| `pkg/config/envkeys.go` | 修改 | GetHome() 优先级：PICOCLAW_HOME > 可执行文件目录 > ~/.picoclaw |
| `pkg/config/defaults.go` | 修改 | DefaultConfig 使用 GetHome() 解析路径 |
| `pkg/agent/hermes.go` | **新增** | HermesMode 定义 + HermesGuard |
| `pkg/agent/hermes_prompt.go` | **新增** | hermesRoleContributor (PromptContributor) |
| `pkg/agent/agent.go` | 修改 | 新增 hermesMode 字段 |
| `pkg/agent/agent_init.go` | 修改 | registerSharedTools 按 HermesMode 条件注册 |
| `pkg/tools/registry.go` | 修改 | 新增 Remove(name string) 方法 |
| `cmd/picoclaw/internal/server/command.go` | **新增** | `picoclaw server` 命令 |
| `pkg/reef/task.go` | 修改 | 新增 Priority/ParentTaskID/SubTaskIDs/Dependencies/ReplyTo/新状态 |
| `pkg/reef/protocol.go` | 修改 | TaskDispatchPayload 新增 ReplyTo，新增 MsgTaskStatusQuery/Response |
| `pkg/reef/server/server.go` | 重构 | 集成 GatewayBridge/DAGEngine/TimeoutScanner/RecoveryManager/TaskStore |
| `pkg/reef/server/scheduler.go` | 增强 | 集成 PriorityQueue/Strategy/TaskStore/DAGEngine/幂等检查 |
| `pkg/reef/server/store/store.go` | 修改 | 扩展 TaskStore 接口（保留旧方法） |
| `pkg/reef/server/store/sqlite.go` | 重构 | 新增表/索引/方法，保留现有实现 |
| `pkg/reef/server/admin.go` | 修改 | 新增端点，保留现有端点 |
| `pkg/reef/server/websocket.go` | 修改 | 幂等检查 + ReplyTo 透传 + 恢复确认 |
| `pkg/reef/server/queue.go` | 保留 | 保留作为 PriorityQueue 的降级 |
| `pkg/reef/server/queue_iface.go` | 保留 | Queue 接口不变 |
| `pkg/reef/server/registry.go` | 保留 | 无变更 |
| `pkg/reef/server/notify/*` | 保留 | 无变更 |
| `pkg/reef/server/ui/*` | 保留 | 独立 UI 保留作为降级 |
| `pkg/channels/swarm/swarm.go` | 修改 | dispatchTask 携带 reply_to |
| **`pkg/reef/server/gateway.go`** | **新增** | GatewayBridge |
| **`pkg/reef/server/reef_tool.go`** | **新增** | ReefSwarmTool/ReefQueryTool |
| **`pkg/reef/server/dag_engine.go`** | **新增** | DAG 编排引擎 |
| **`pkg/reef/server/priority_queue.go`** | **新增** | 优先级队列 |
| **`pkg/reef/server/strategy.go`** | **新增** | MatchStrategy + 3 种实现 |
| **`pkg/reef/server/timeout_scanner.go`** | **新增** | 超时扫描器 |
| **`pkg/reef/server/recovery.go`** | **新增** | 恢复管理器 |
| **`web/frontend/src/routes/reef.tsx`** | **新增** | Reef 布局路由 |
| **`web/frontend/src/routes/reef/index.tsx`** | **新增** | Overview 页面 |
| **`web/frontend/src/routes/reef/tasks.tsx`** | **新增** | Tasks 页面 |
| **`web/frontend/src/routes/reef/clients.tsx`** | **新增** | Clients 页面 |
| **`web/frontend/src/components/reef/*`** | **新增** | Reef UI 组件 |
| **`web/frontend/src/api/reef.ts`** | **新增** | Reef API 客户端 |
| `web/frontend/src/components/app-sidebar.tsx` | 修改 | 新增 Reef 导航组 |
| `web/backend/api/router.go` | 修改 | 新增 registerReefRoutes |
| `cmd/picoclaw/internal/reef/command.go` | 修改 | 新增 Gateway/Scheduler 命令行参数 |

## 9. 关键设计决策

| 决策 | 选项 | 选择 | 理由 |
|------|------|------|------|
| 数据目录 | ~/.picoclaw / 程序目录 | **程序目录优先** | 便于部署，PICOCLAW_HOME 仍可覆盖 |
| Server 行为约束 | 无约束 / Hermes 三角色 | **Hermes 三角色** | 防止 Server 端意图发散 |
| Server 频道能力 | 新建 / 复用 Gateway | **复用 Gateway** | 已有完整实现，无需重复 |
| 结果回传 | 新建 Adapter / 复用 MessageBus | **复用 MessageBus** | AgentLoop 回复自动分发，零额外代码 |
| AgentLoop 调用调度器 | 直接调用 / 通过 Tool | **通过 Tool** | LLM 自主决策是否分发 |
| 任务分解 | 代码逻辑 / LLM 自主 | **LLM 自主** | AgentLoop 多次调用 Tool 即为分解 |
| 结果聚合 | 代码逻辑 / LLM 自主 | **LLM 自主** | InboundMessage 发给 AgentLoop 自动聚合 |
| Gateway 启动 | 始终 / 可选 | **可选** | 不配置时保持轻量 |
| Web UI | 替换 / 融入 | **融入** | 复用 picoclaw 前端组件库 |
| 独立 UI | 删除 / 保留 | **保留降级** | 不依赖前端构建时仍可用 |
| Queue 接口 | 替换 / 保留 | **保留** | PriorityQueue 实现相同接口 |
| TaskStore 接口 | 替换 / 扩展 | **扩展** | 旧方法签名不变，新增方法 |
| NotifyManager | 替换 / 保留 | **保留** | 告警通知仍走原有路径 |

## 10. 实施顺序

```
Phase 0: 数据目录统一
  ├── 修改 GetHome() 优先级
  ├── 添加 isWritableDir() 可写性检查
  ├── 修改 Reef Server 默认 StorePath
  └── 迁移提示（日志）

Phase 1: TaskStore 持久化 + 恢复
  ├── 扩展 TaskStore 接口和 SQLite
  ├── 新增 task_reply_to / task_relations 表
  ├── RecoveryManager
  └── Server 启动恢复

Phase 2: 优先级调度器
  ├── PriorityQueue（实现 Queue 接口）
  ├── 非阻塞 Scan
  ├── MatchStrategy（3 种实现）
  ├── TimeoutScanner
  └── 幂等检查

Phase 3: Gateway 集成 + 任务分发
  ├── GatewayBridge
  ├── ReefSwarmTool / ReefQueryTool
  ├── ReplyTo 上下文追踪
  ├── 结果回传（MessageBus outbound）
  └── Task 模型扩展

Phase 4: DAG Engine + 聚合
  ├── DAG Engine（创建/依赖/失败传播）
  ├── 子任务结果收集
  └── AgentLoop 聚合（InboundMessage）

Phase 5: Web UI 统一
  ├── Reef 前端页面（Overview/Tasks/Clients）
  ├── Reef API 端点（picoclaw Web 后端）
  ├── SSE 事件流
  └── 侧边栏导航
```
