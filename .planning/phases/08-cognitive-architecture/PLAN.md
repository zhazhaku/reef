# Phase 08: Agent 认知架构 — 执行计划

> 基于 CONTEXT.md 三大决策 + specs/specs-cognitive.md 27 条 COG 规格  
> 扩展已有 PicoClaw 包，不新建独立 `pkg/reef/cognition/`

---

## 总视

| Wave | 任务 | 目标 | 文件 | 工时 |
|------|------|------|------|------|
| **W1: 上下文** | P8-1~3 | ContextManager 四层 + Compact + 腐化检测 | ~8 文件 | 3d |
| **W2: 记忆** | P8-4 | 四层记忆生命周期 + seahorse schema | ~6 文件 | 3d |
| **W3: 沙箱+检查点** | P8-5~6 | TaskSandbox + CheckpointManager | ~6 文件 | 3d |
| **W4: CNP 协议** | P8-7~8 | 16 种 CNP 消息 + 路由 + 双通道 | ~8 文件 | 4d |
| **W5: 集成** | P8-9~11 | Sandbox↔TaskRunner, CNP↔Raft, Session 隔离 | ~6 文件 | 4d |
| **W6: E2E+Perf** | P8-12~19 | 端到端验证 + 性能基准 | ~8 文件 | 5d |

---

## Wave 1: 上下文管理（3 天）

### P8-1: ContextLayers 数据结构 — `picoclaw/pkg/agent/context_layers.go` (NEW)

**TDD:** `context_layers_test.go` 先写

| 测试 | 覆盖 |
|------|------|
| `TestContextLayers_New` | 四层初始化，L0-L3 结构完整 |
| `TestContextLayers_TokenEstimate` | 各层 token 估算函数 |
| `TestContextLayers_BuildPrompt` | 四层拼接为 LLM prompt |
| `TestContextLayers_WithCapacity` | 不同 capacity 下 L2 轮数上限 |

**实现：**

```go
type ContextLayers struct {
    Immutable   string        // L0: System Prompt + Role + Skills + Genes
    Task        string        // L1: Task instruction + metadata
    Working     []WorkingRound // L2: sliding window of tool calls + outputs
    Injections  []MemoryInjection // L3: matched Genes + Episodic memories
    config      ContextConfig
    mu          sync.RWMutex
}

type WorkingRound struct {
    Round  int
    Call   string // tool call
    Output string // tool result
    Thought string // LLM reasoning
}

type MemoryInjection struct {
    Source string // "gene" | "episodic"
    Content string
    Relevance float64
}
```

**规格覆盖:** COG-CTX-01, COG-CTX-03

---

### P8-2: ContextManager — `picoclaw/pkg/agent/context_manager.go` (NEW)

**TDD:** `context_manager_test.go` 先写

| 测试 | 覆盖 |
|------|------|
| `TestContextManager_Build` | 从 AgentInstance 构建四层上下文 |
| `TestContextManager_Compact_BelowThreshold` | 不超阈值时不压缩 |
| `TestContextManager_Compact_AboveThreshold` | 超 80% 后压缩 L2 |
| `TestContextManager_Compact_PreservesRecent` | 压缩后保留最近 5 轮完整内容 |
| `TestContextManager_Compact_OldRoundsSummarized` | 旧轮次变为摘要 |
| `TestContextManager_BudgetEnforce` | 强制度超过 MaxTokens 时截断 |
| `TestContextManager_Compact_TokenSaved` | 压缩后 token 节省 > 60% |
| `TestContextManager_InjectMemory` | 注入记忆到 L3 |

**实现：**

```go
type ContextManager struct {
    layers ContextLayers
    config ContextConfig
    mu     sync.RWMutex
}

func (cm *ContextManager) Build(agent *AgentInstance, task *reef.Task) ContextLayers
func (cm *ContextManager) Compact() error
func (cm *ContextManager) AppendRound(round WorkingRound)
func (cm *ContextManager) InjectMemory(injections []MemoryInjection)
func (cm *ContextManager) TokenCount() int
func (cm *ContextManager) IsOverBudget() bool
```

**规格覆盖:** COG-CTX-02, COG-CTX-05, COG-CTX-06, COG-CTX-07

---

### P8-3: CorruptionGuard — `picoclaw/pkg/agent/corruption_guard.go` (NEW)

**TDD:** `corruption_guard_test.go` 先写

| 测试 | 覆盖 |
|------|------|
| `TestCorruptionGuard_LoopDetected` | 同一工具调用 5 次 → 检测 |
| `TestCorruptionGuard_LoopNotDetected` | 不同工具 4 次 → 不检测 |
| `TestCorruptionGuard_DriftDetected` | 任务目标替换 → 检测 |
| `TestCorruptionGuard_BlankDetected` | 连续 3 轮空输出 → 检测 |
| `TestCorruptionGuard_Check_NoCorruption` | 正常上下文 → 通过 |
| `TestCorruptionGuard_Report_SentToServer` | 检测后生成 context_corruption 消息 |

**实现：**

```go
type CorruptionGuard struct {
    config    CorruptionConfig
    toolCount map[string]int
    blankRounds int
    lastGoal  string
}

func (cg *CorruptionGuard) Check(layers *ContextLayers) *CorruptionReport
func (cg *CorruptionGuard) Reset()
```

**规格覆盖:** COG-CTX-04, COG-CTX-08

---

## Wave 2: 记忆系统（3 天）

### P8-4: MemorySystem — 扩展 `picoclaw/pkg/memory/` + `picoclaw/pkg/seahorse/`

**修改文件：**

| 文件 | 操作 | 内容 |
|------|------|------|
| `picoclaw/pkg/seahorse/schema.go` | MODIFY | 新增 `task_episodes` 表 |
| `picoclaw/pkg/memory/episodic.go` | NEW | MemoryLifecycle: Extract, Prune, Retrieve |
| `picoclaw/pkg/memory/episodic_store.go` | NEW | EpisodicStore: SQLite CRUD (seahorse-backed) |
| `picoclaw/pkg/memory/semantic.go` | NEW | SemanticRetriever: 从 genes 表检索匹配 Gene |
| `picoclaw/pkg/memory/memory_system.go` | NEW | MemorySystem: 统一入口（Lifecycle + Retriever） |

**TDD:** 每个文件的 `_test.go` 先写

| 测试文件 | 关键用例 |
|---------|---------|
| `schema_test.go` (追加) | `task_episodes` 表存在 + 索引验证 |
| `episodic_test.go` | Extract: task 完成后 Working→Episodic, Prune: 30天过期清除, Retrieve: 按 task_id + tags 查询 |
| `episodic_store_test.go` | Save/Get/Delete/ListByTask 四操作 round-trip |
| `semantic_test.go` | 按 role+tags 检索匹配 Gene, 无匹配返回空, 相关性排序 |
| `memory_system_test.go` | MemoryLifecycle.Extract 写入 SQLite, MemoryRetriever 检索到已写入, 任务命名空间隔离 |

**实现关键结构：**

```go
// picoclaw/pkg/memory/episodic.go
type MemoryLifecycle struct {
    store EpisodicStore
}

func (ml *MemoryLifecycle) ExtractEpisodic(
    taskID string, 
    layers *ContextLayers, 
    eventType string, // "success" | "failure"
) (*EpisodicEntry, error)

func (ml *MemoryLifecycle) Prune(ctx context.Context) error
// 清除 30 天前 + 超过 1000 条的旧记录

// picoclaw/pkg/memory/episodic_store.go
type EpisodicStore interface {
    Save(entry EpisodicEntry) error
    GetByTask(taskID string) ([]EpisodicEntry, error)
    Search(query string, limit int) ([]EpisodicEntry, error)
    DeleteBefore(timestamp int64) error
    Count() (int, error)
}

// picoclaw/pkg/memory/semantic.go
type SemanticRetriever struct {
    store *seahorse.Store
}

func (sr *SemanticRetriever) Retrieve(
    role string, 
    taskTags []string, 
    limit int,
) ([]*Gene, error)
```

**规格覆盖:** COG-MEM-01 ~ COG-MEM-05

---

## Wave 3: 任务沙箱 + 检查点（3 天）

### P8-5: TaskSandbox — `picoclaw/pkg/agent/sandbox.go` (NEW)

**TDD:** `sandbox_test.go` 先写

| 测试 | 覆盖 |
|------|------|
| `TestTaskSandbox_New` | 创建 → 独立 workspace, session, layers |
| `TestTaskSandbox_ContextIsolation` | Task-A/B ContextLayers 完全独立 |
| `TestTaskSandbox_MemoryNamespace` | 记忆命名空间隔离（不同 task_id） |
| `TestTaskSandbox_WorkDir_Independent` | 各自独立工作目录 |
| `TestTaskSandbox_Genes_SharedReadOnly` | Genes 共享但只读 |
| `TestTaskSandbox_Destroy` | 销毁 → 提取记忆 + 清理目录 + 释放 |
| `TestTaskSandbox_TwoConcurrent` | 两 task 并行执行不互相影响 |

**实现：**

```go
type TaskSandbox struct {
    taskID    string
    agent     *AgentInstance
    ctx       *ContextManager
    mem       *MemorySystem
    workDir   string
    mu        sync.Mutex
}

func NewTaskSandbox(task *reef.Task, baseCfg *AgentConfig) (*TaskSandbox, error)
func (s *TaskSandbox) Execute(ctx context.Context, instruction string) (string, error)
func (s *TaskSandbox) Destroy() error
```

**规格覆盖:** COG-SBOX-01 ~ COG-SBOX-04

---

### P8-6: CheckpointManager — `picoclaw/pkg/agent/checkpoint.go` (NEW)

**TDD:** `checkpoint_test.go` 先写

| 测试 | 覆盖 |
|------|------|
| `TestCheckpoint_AutoSave_Interval` | 5 分钟自动保存 |
| `TestCheckpoint_AutoSave_Rounds` | 5 轮自动保存（先触发者） |
| `TestCheckpoint_SaveAndRestore` | 保存 → 恢复 → 注入恢复指令 |
| `TestCheckpoint_SummaryOnly` | 检查点内容为摘要（非全量上下文） |
| `TestCheckpoint_MaxRotation` | 最多 10 个检查点轮转 |
| `TestCheckpoint_Restore_ResumeInstruction` | 恢复时告诉 LLM "从第 N 步继续" |

**实现：**

```go
type CheckpointManager struct {
    taskID     string
    dir        string // tasks/{task_id}/checkpoints/
    interval   time.Duration // 5min
    maxRounds  int // 5
    maxCount   int // 10
    current    int
    mu         sync.Mutex
}

func NewCheckpointManager(taskID string) *CheckpointManager
func (cm *CheckpointManager) ShouldSave(roundNum int, lastSave time.Time) bool
func (cm *CheckpointManager) Save(layers *ContextLayers, roundNum int) error
func (cm *CheckpointManager) Restore() (*Checkpoint, error)
func (cm *CheckpointManager) BuildResumeInstruction(cp *Checkpoint) string
```

**规格覆盖:** COG-CPT-01 ~ COG-CPT-04

---

## Wave 4: CNP 认知网络协议（4 天）

### P8-7: CNP 消息类型 — `pkg/reef/cnp_messages.go` (NEW in reef)

**TDD:** `cnp_messages_test.go` 先写

| 测试 | 覆盖 |
|------|------|
| `TestCNPMessage_All16Types_Serialize` | 所有 16 种消息 round-trip |
| `TestCNPMessage_ContextCorruption` | context_corruption 带 details |
| `TestCNPMessage_MemoryUpdate` | memory_update 带 episodic entry |
| `TestCNPMessage_CheckpointSave` | checkpoint_save 带摘要 |
| `TestCNPMessage_InvalidType_Rejected` | 非法消息类型拒绝 |
| `TestCNPMessage_ConsensusRouting` | 5 种共识消息识别正确 |

**实现：**

```go
// pkg/reef/cnp_messages.go
type CNPMessageType string

const (
    MsgContextCorruption  CNPMessageType = "context_corruption"
    MsgContextCompactDone CNPMessageType = "context_compact_done"
    MsgContextInject      CNPMessageType = "context_inject"
    MsgContextRestore     CNPMessageType = "context_restore"
    MsgMemoryUpdate       CNPMessageType = "memory_update"
    MsgMemoryQuery        CNPMessageType = "memory_query"
    MsgMemoryInject       CNPMessageType = "memory_inject"
    MsgMemoryPrune        CNPMessageType = "memory_prune"
    MsgStrategySuggest    CNPMessageType = "strategy_suggest"
    MsgStrategyAck        CNPMessageType = "strategy_ack"
    MsgStrategyResult     CNPMessageType = "strategy_result"
    MsgCheckpointSave     CNPMessageType = "checkpoint_save"
    MsgCheckpointRestore  CNPMessageType = "checkpoint_restore"
    MsgLongTaskHeartbeat  CNPMessageType = "long_task_heartbeat"
    MsgLongTaskProgress   CNPMessageType = "long_task_progress"
    MsgLongTaskComplete   CNPMessageType = "long_task_complete"
)

type CNPMessage struct {
    Type      CNPMessageType `json:"type"`
    TaskID    string         `json:"task_id"`
    Timestamp int64          `json:"timestamp"`
    Payload   interface{}    `json:"payload"`
}

// 共识路由
var ConsensusTypes = map[CNPMessageType]bool{
    MsgMemoryUpdate:   true,
    MsgMemoryPrune:    true,
    MsgCheckpointSave: true,
    MsgContextInject:  true,
    MsgStrategyResult: true,
}

func IsConsensus(msgType CNPMessageType) bool {
    return ConsensusTypes[msgType]
}
```

**规格覆盖:** CNP 消息类型表（proposal-cognitive.md §6）

---

### P8-8: CNP Handler — `picoclaw/pkg/agent/cnp_handler.go` (NEW) + `pkg/reef/server/cnp_handler.go` (NEW)

**TDD:** `cnp_handler_test.go` 先写（两处）

| 测试 | 覆盖 |
|------|------|
| `TestCNPHandler_Client_ContextCorruption` | 腐化 → 生成 context_corruption → 发送 |
| `TestCNPHandler_Client_MemoryUpdate` | 任务完成 → 提取记忆 → memory_update → Raft |
| `TestCNPHandler_Client_CheckpointSave` | 自动保存 → checkpoint_save → Raft |
| `TestCNPHandler_Server_StrategySuggest` | 收到 corruption → 生成 strategy_suggest |
| `TestCNPHandler_Server_MemoryInject` | 收到 memory_query → 检索 → inject |
| `TestCNPHandler_Server_MemoryPrune` | 定时 prune → memory_prune → Raft |
| `TestCNPHandler_Consensus_Route` | 共识消息走 Propose，非共识消息直接处理 |
| `TestCNPHandler_Timeout` | Raft Propose 超时 → 重试 |

**实现：**

```go
// picoclaw/pkg/agent/cnp_handler.go (Client 端)
type ClientCNPHandler struct {
    connector *Connector
    sandbox   *TaskSandbox
    raft      *RaftNode
}

func (h *ClientCNPHandler) HandleCorruption(report *CorruptionReport) error
func (h *ClientCNPHandler) SendMemoryUpdate(entry *EpisodicEntry) error
func (h *ClientCNPHandler) SendCheckpoint(cp *Checkpoint) error
func (h *ClientCNPHandler) HandleServerMessage(msg CNPMessage) error

// pkg/reef/server/cnp_handler.go (Server 端)
type ServerCNPHandler struct {
    hub      *EvolutionHub
    seahorse *seahorse.Store
    raft     *RaftNode
}

func (h *ServerCNPHandler) HandleClientMessage(clientID string, msg CNPMessage) error
func (h *ServerCNPHandler) GenerateStrategy(corruption *CorruptionReport) (*StrategySuggestion, error)
func (h *ServerCNPHandler) HandleMemoryQuery(clientID string, query string) (*MemoryInjectPayload, error)
```

**规格覆盖:** CNP 协议全 16 种消息处理

---

## Wave 5: 集成（4 天）

### P8-9: Sandbox → TaskRunner 集成

**修改文件：**

| 文件 | 操作 | 内容 |
|------|------|------|
| `pkg/reef/client/task_runner.go` | MODIFY | `runOnce` 中替换裸 `r.exec()` 为 Sandbox.Execute() |
| `pkg/reef/client/task_runner.go` | MODIFY | `StartTask` 中创建 Sandbox |
| `pkg/reef/client/task_runner.go` | MODIFY | `reportCompleted/Failed` 中调用 Destroy() + MemoryLifecycle.Extract() |

**关键改动：**

```go
// Before (Phase 7)
func (r *TaskRunner) runOnce(rt *RunningTask) (string, error) {
    return r.exec(rt.TaskCtx.Context, rt.Instruction)
}

// After (Phase 8)
func (r *TaskRunner) runOnce(rt *RunningTask) (string, error) {
    // 每轮执行前：上下文紧凑 + 腐化检查
    if rt.Sandbox != nil {
        rt.Sandbox.Compact()
        if report := rt.Sandbox.CheckCorruption(); report != nil {
            rt.CNP.SendCorruption(report)
            return "", report.Error()
        }
        // 检查点自动保存
        if rt.Checkpoint.ShouldSave(rt.Round, rt.LastSave) {
            rt.Checkpoint.Save(rt.Sandbox.Layers(), rt.Round)
        }
    }
    return rt.Sandbox.Execute(rt.TaskCtx.Context, rt.Instruction)
}
```

**TDD:** 扩展现有 `task_runner_test.go`：Mock Sandbox, 验证 cycle 调用链

---

### P8-10: CNP → EvolutionHub 集成

**修改文件：**

| 文件 | 操作 | 内容 |
|------|------|------|
| `pkg/reef/evolution/server/hub.go` | MODIFY | 新增 `HandleCNP(msg)` 方法 |
| `pkg/reef/evolution/server/hub.go` | MODIFY | 收到 memory_update → 存储到 seahorse |
| `pkg/reef/evolution/server/hub.go` | MODIFY | 收到 context_corruption → 生成 strategy |
| `pkg/reef/evolution/server/broadcaster.go` | MODIFY | 收到 strategy_result → 影响 Gene 权重 |

---

### P8-11: Session 隔离适配

**修改文件：**

| 文件 | 操作 | 内容 |
|------|------|------|
| `picoclaw/pkg/session/store.go` | MODIFY | 支持 task_id 命名空间的 session 读写 |
| `picoclaw/pkg/agent/instance.go` | MODIFY | `NewAgentInstance` 接受 workspace=task/{id} |

**TDD:** `sandbox_session_test.go` — 两 task 同时创建 session，验证 key 不冲突

---

## Wave 6: E2E + 性能（5 天）

### P8-12~19: 端到端验证 + 性能基准

| ID | 测试 | 类型 | 工时 |
|----|------|------|------|
| P8-12 | 上下文压缩 E2E（50+ 轮 → 压缩 → 一致性验证） | Integration | 1d |
| P8-13 | 腐化检测 + 恢复（模拟循环模式） | Integration | 1d |
| P8-14 | 检查点恢复（kill → 重启 → 恢复） | Integration | 1d |
| P8-15 | 多任务隔离（3 task 并行） | Integration | 0.5d |
| P8-16 | 记忆生命周期（10 任务 → extract → retrieve → prune） | Integration | 1d |
| P8-17 | CNP 全协议（16 种消息 round-trip） | Integration | 0.5d |
| P8-18 | Compaction < 2s, CorruptionCheck < 500ms, MemoryIO < 100ms | Benchmark | 0.5d |
| P8-19 | Token 节省验证（50 轮对比）| Benchmark | 0.5d |

---

## 覆盖率门禁

| 包/文件 | 要求 | 类型 |
|---------|------|------|
| `picoclaw/pkg/agent/context_layers.go` | ≥90% | 新包 Plan 门禁 |
| `picoclaw/pkg/agent/context_manager.go` | ≥90% | 新包 Plan 门禁 |
| `picoclaw/pkg/agent/corruption_guard.go` | ≥90% | 新包 Plan 门禁 |
| `picoclaw/pkg/memory/episodic*.go` | ≥90% | 新包 Plan 门禁 |
| `picoclaw/pkg/agent/sandbox.go` | ≥90% | 新包 Plan 门禁 |
| `picoclaw/pkg/agent/checkpoint.go` | ≥90% | 新包 Plan 门禁 |
| `pkg/reef/cnp_messages.go` | ≥90% | 新包 Plan 门禁 |
| `picoclaw/pkg/agent/cnp_handler.go` | ≥85% | 新包 Plan 门禁 |
| `pkg/reef/server/cnp_handler.go` | ≥85% | 新包 Plan 门禁 |
| **Phase 08 总门禁** | **≥90%** | Phase Gate |

---

## 依赖图

```
W1 (上下文) ──────┐
                   ├──► W3 (沙箱+检查点) ──► W5 (集成) ──► W6 (E2E)
W2 (记忆) ────────┘         │
                             │
W4 (CNP 协议) ──────────────┘
```

- W1 和 W2 可并行（无相互依赖）
- W4 可并行（仅依赖 reef protocol 包，与 W1/W2/W3 无关）
- W3 依赖 W1（Sandbox 用 ContextManager）+ W2（Sandbox 用 MemorySystem）
- W5 依赖 W3+W4（集成需要 Sandbox + CNP Handler）
- W6 依赖全部

---

## 总工时

| Wave | 任务 | 工时 |
|------|------|------|
| W1 | P8-1,2,3 | 3d |
| W2 | P8-4 | 3d |
| W3 | P8-5,6 | 3d |
| W4 | P8-7,8 | 4d |
| W5 | P8-9,10,11 | 4d |
| W6 | P8-12~19 | 5d |
| **总计** | | **22 人天** |

---

*Plan generated 2026-05-02 | Ready for user review*
