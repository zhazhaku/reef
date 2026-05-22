# GSD Plan: Hermes 智能协作工作流

> project: reef-hermes-architecture
> methodology: GSD (Get Stuff Done)
> created: 2026-05-09
> phase: implemented (Wave 0 + Wave 5, 31/132 tasks)
> openspec: openspec/changes/reef-hermes-architecture/specs/workflow.md

---

## Overview

**Implementation Status (2026-05-16)**:
- ✅ Wave 0 (模式系统) — fully implemented in `conversation_mode.go` + `mode_store.go`
- ❌ Waves 1-4 (工作流) — code scaffolding exists (`hermes_workflow.go`, `hermes_orchestrator.go`, `hermes_prompt.go`) but state machine/phase logic unimplemented
- ✅ Wave 5 (HermesGuard) — implemented in `pipeline_execute.go` with ModeChat bypass
- ❌ Wave 6 (E2E) — not started

**31/132 tasks checked off. Remaining: 101 tasks (~7 days).**

```
Wave 0: 模式系统 + 上下文隔离          — ✅ done  —  7 units
Wave 1: 工作流状态机 + 任务分类         — 🟡 scaffold —  6 units
Wave 2: 头脑风暴                        — ❌ not started — 10 units
Wave 3: 风暴评审                        — ❌ not started —  5 units
Wave 4: 调研 + 设计 + 报告              — ❌ not started —  6 units
Wave 5: HermesGuard + Prompt 注入       — ✅ done  —  4 units
Wave 6: 集成测试 + E2E                  — ❌ not started —  5 units
─────────────────────────────────────────────────────────
Total:                                   8 days    43 units
```

每个 Wave 可独立编译、测试、验收。

---

## 依赖关系

```
Wave 0 ──→ Wave 1 ──→ Wave 2 ──→ Wave 3 ──→ Wave 4 ──→ Wave 6
                │                                         │
                └──→ Wave 5 ─────────────────────────────┘
```

Wave 5 可与 Wave 2-4 并行，因为 HermesGuard + Prompt 是独立模块。

---

## 前置决策 (已确认)

| # | 决策 | 结论 |
|---|------|------|
| D1 | Client 角色注册 | 复用现有 `ClientInfo.Role/Skills` + `Registry.ListByRole()` |
| D2 | Tool 策略 | 放弃 Layer 2 条件注册，统一 Tool 全集 + HermesGuard |
| D3 | OpenSpec/GSD 产出 | SystemPrompt 注入方法论，产出存入 seahorse，按需导出文件 |

---

---

# Wave 0: 模式系统 + 上下文隔离

> 目标: 飞书频道内输入 "切换聊天/切换Hermes" 可在两种模式间切换，上下文完全隔离，DeepSeek 缓存命中 90%+
> 产出: ModeChat 与 ModeHermes 可自由切换，互不污染
> 依赖: 无
> 验收: 发送 "切换Hermes模式" → 进入 Hermes，发送 "切换聊天模式" → 回到 Chat，中间 Chat 对话在 Hermes 下不可见

## U0.1: ConversationMode 类型定义

**文件**: `pkg/agent/conversation_mode.go` (新增)

```
type ConversationMode string
const (
    ModeChat   ConversationMode = "chat"
    ModeHermes ConversationMode = "hermes"
)
```

- [x] 类型定义 + String() 方法
- [x] 编译通过

## U0.2: ModeStore — SQLite 持久化

**文件**: `pkg/agent/mode_store.go` (新增)

在 seahorse SQLite 新增表 `conversation_mode`:
```sql
CREATE TABLE IF NOT EXISTS conversation_mode (
    conversation_id INTEGER PRIMARY KEY,
    mode            TEXT NOT NULL DEFAULT 'chat',
    updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
);
```

- [x] `NewModeStore(db *sql.DB) *ModeStore`
- [x] `GetMode(ctx, convID) (ConversationMode, error)` — 默认 chat
- [x] `SetMode(ctx, convID, mode)` — upsert
- [x] seahorse Store 初始化时建表

## U0.3: Session Key 隔离

**文件**: `pkg/agent/conversation_mode.go`

```go
func SessionKey(convID string, mode ConversationMode) string {
    switch mode {
    case ModeChat:
        return fmt.Sprintf("conv:%d:chat", convID)
    case ModeHermes:
        return fmt.Sprintf("conv:%d:hermes", convID)
    default:
        return fmt.Sprintf("conv:%d:chat", convID)
    }
}
```

- [x] SessionKey 函数
- [x] 所有 seahorse 调用点改用 SessionKey(convID, mode) 而非 raw convID
- [x] 确保 AgentLoop/context_loader 使用正确的 session key

## U0.4: Pipeline 模式切换指令拦截

**文件**: `pkg/agent/pipeline_setup.go` (修改)

```go
func (p *Pipeline) preProcessModeSwitch(msg *Message) (handled bool, response string) {
    cmd := parseModeCommand(msg.Content)
    if cmd == nil { return false, "" }
    
    switch cmd {
    case "switch_to_chat":
        p.modeStore.SetMode(msg.ConversationID, ModeChat)
        return true, "已切换到聊天模式"
        
    case "switch_to_hermes":
        p.modeStore.SetMode(msg.ConversationID, ModeHermes)
        return true, "已切换到 Hermes 模式。"
        
    case "show_mode":
        mode := p.modeStore.GetMode(msg.ConversationID)
        return true, fmt.Sprintf("当前模式: %s", mode)
    }
}
```

- [x] `parseModeCommand(text string) string` — 支持 "切换聊天模式" / "chat mode" / "切换Hermes模式" / "hermes mode" / "查看模式" / "status"
- [x] 切换指令 NOT 写入 seahorse conversation (不污染上下文)
- [x] 编译器注入到 pipeline_setup.go 的消息处理链

## U0.5: AgentLoop 模式分支

**文件**: `pkg/agent/agent.go` (修改 `processMessage`)

```go
func (al *AgentLoop) processMessage(msg Message) {
    mode := al.modeStore.GetMode(msg.ConversationID)
    
    switch mode {
    case ModeChat:
        al.processChatMode(msg)          // 现有行为
    case ModeHermes:
        al.processHermesMode(msg)        // 新: → Phase 1 intake 或 resume
    }
}
```

- [x] `processChatMode` — 提取现有 AgentLoop.processMessage 内容
- [x] `processHermesMode` — 空壳，Wave 1 填充
- [x] AgentLoop 初始化时注入 ModeStore

## U0.6: 前缀稳定性 — SystemPrompt 只改 ModeTag

**文件**: `pkg/agent/hermes_prompt.go` (修改)

```
Stable Prefix (两种模式一致):
  kernel.identity     "You are Reef..."            ← 不变
  tool.definitions    全部 Tool                    ← 统一 (D2)
  capability.skills   Skill catalog                ← 不变

Tunable Block (仅此不同):
  kernel.mode_tag:
    Chat   = "You are Reef, a helpful AI assistant."         (~100 tokens)
    Hermes = "You are Hermes, a workflow coordinator..."     (~200 tokens)
```

- [x] ModeTag PromptPart —— 固定 Slot = PromptSlotIdentity, Source = "system:mode"
- [x] `BuildSystemPromptParts` 根据 mode 选择 ModeTag
- [x] 验证: 两种模式下 Stable Prefix 的 sha256 一致 (Tool 列表顺序一致)

## U0.7: 编译验证

- [x] `go build ./cmd/reef/` 通过
- [x] `reef server` 启动不报错
- [x] 发送 "切换聊天模式" — 返回 "已切换到聊天模式"
- [x] 发送 "切换Hermes模式" — 返回 "已切换到 Hermes 模式。"
- [x] 发送 "查看模式" — 返回当前模式

---

# Wave 1: 工作流状态机 + 任务分类

> 目标: Phase 1 (Intake) 完成 — 用户输入需求 → Server 解析 TaskProfile → 自动决定进入 Chat 还是 Hermes 工作流
> 产出: TaskProfile + WorkflowSession 结构，Intake Phase 完整
> 依赖: Wave 0
> 验收: 发送复杂需求 → Server 自动进入 Hermes 模式，显示 TaskProfile

## U1.1: WorkflowPhase 状态机

**文件**: `pkg/agent/hermes_workflow.go` (新增)

```go
type WorkflowPhase string
const (
    PhaseIdle      WorkflowPhase = "idle"
    PhaseIntake    WorkflowPhase = "intake"
    PhaseBrainstorm WorkflowPhase = "brainstorm"
    PhaseReview    WorkflowPhase = "review"
    PhaseResearch  WorkflowPhase = "research"
    PhaseDesign    WorkflowPhase = "design"
    PhaseReport    WorkflowPhase = "report"
    PhaseComplete  WorkflowPhase = "complete"
    PhaseAborted   WorkflowPhase = "aborted"
)

type TaskProfile struct {
    TaskID           string   `json:"task_id"`
    Domain           string   `json:"domain"`
    Type             string   `json:"type"`         // system-design | market-research | ...
    Complexity       int      `json:"complexity"`   // 1-5
    Keywords         []string `json:"keywords"`
    EstimatedClients int      `json:"estimated_clients"`
}
```

- [ ] Phase 枚举 + 状态转换表
- [ ] `validTransitions` map
- [ ] `CanTransition(from, to WorkflowPhase) bool`
- [ ] TaskProfile 结构体

## U1.2: WorkflowSession 持久化

**文件**: `pkg/agent/hermes_workflow_session.go` (新增)

在 seahorse SQLite 新增表:
```sql
CREATE TABLE IF NOT EXISTS hermes_workflow_sessions (
    conversation_id  INTEGER PRIMARY KEY,
    task_id          TEXT NOT NULL,
    phase            TEXT NOT NULL DEFAULT 'idle',
    task_profile     TEXT,              -- JSON
    current_round    INTEGER DEFAULT 0,
    max_rounds       INTEGER DEFAULT 5,
    directions       TEXT,              -- JSON array
    converge_streak  INTEGER DEFAULT 0,
    selected_clients TEXT,              -- JSON array
    task_boards      TEXT,              -- JSON: {phase: conversation_key}
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);
```

- [ ] `SaveWorkflowSession(ctx, session)` — upsert
- [ ] `GetWorkflowSession(ctx, convID) (*WorkflowSession, error)`
- [ ] `DeleteWorkflowSession(ctx, convID)` — 完成后清理
- [ ] seahorse 初始化建表

## U1.3: TaskProfile 分类器

**文件**: `pkg/agent/hermes_orchestrator.go` (新增)

Server LLM 轻量调用 (~500 tokens) 分类用户需求:

```
分类 Prompt (内嵌，不走 AgentLoop):
"分析以下用户需求，返回 JSON:
 {domain, type, complexity(1-5), estimated_clients, keywords}

type 枚举: system-design|market-research|code-review|tech-selection|debugging|simple-qa|unknown
complexity: 1=简单直接回, 2=单Client, 3=2-3Client, 4=4+Client多轮, 5=完整工作流"
```

- [ ] `ClassifyTask(ctx, userMessage, provider) (*TaskProfile, error)`
- [ ] 直接调用 LLM API (非 AgentLoop)，快速返回
- [ ] 超时 5s，失败时默认 complexity=1 (安全回退到 Chat)

## U1.4: Phase 1 Intake 实现

**文件**: `pkg/agent/hermes_orchestrator.go`

```
Intake 流程:
1. ClassifyTask(userMessage) → TaskProfile
2. if complexity <= 2: reply directly (chat)
3. if complexity >= 3:
   - Create WorkflowSession (taskID = uuid, phase = intake)
   - Set conversation mode = ModeHermes
   - Save WorkflowSession
   - Transition to PhaseBrainstorm (or wait for human: "继续")
```

- [ ] `RunIntake(ctx, msg) → (phase, response, error)`
- [ ] complexity >= 3 自动设置 ModeHermes
- [ ] 回复含 TaskProfile 摘要 + "是否进入头脑风暴？"
- [ ] 人类"继续" → 进入 Phase 2

## U1.5: AgentLoop.ProcessHermesMessage 骨架

**文件**: `pkg/agent/hermes_workflow.go`

```go
func (al *AgentLoop) processHermesMessage(msg Message) {
    session := al.workflowStore.GetSession(msg.ConversationID)
    
    if session == nil {
        // 首次进入 Hermes
        phase, reply := RunIntake(msg)
        replyToUser(reply)
        return
    }
    
    switch session.Phase {
    case PhaseIntake:
        // 等待人类确认 → 进入 brainstorm
    case PhaseBrainstorm:
        al.processBrainstorm(msg, session)
    case PhaseReview:
        al.processReview(msg, session)
    // ...
    }
}
```

- [ ] switch 框架
- [ ] 每个 case 空壳 (Wave 2-4 填充)
- [ ] 人类命令检测 ( "评审"/"跳过"/"停止" 等) 统一入口

## U1.6: 编译验证

- [ ] `go build` 通过
- [ ] 简单消息 "你好" → complexity=1 → Chat 模式直接回复
- [ ] 复杂消息 "设计一个分布式调度系统" → TaskProfile 输出 + "是否进入头脑风暴？"

---

# Wave 2: 头脑风暴

> 目标: Phase 2 完整 — 多 Client 多轮交叉讨论，共享任务板，收敛检测，产出 2-5 个方案方向
> 产出: BrainstormSession 完整实现
> 依赖: Wave 1
> 验收: 3 个不同角色的 Client 在任务板上进行 3 轮讨论，自动收敛为 3 个方向

## U2.1: BrainstormSession 结构

**文件**: `pkg/agent/hermes_brainstorm.go` (新增)

```go
type BrainstormSession struct {
    TaskBoard       string           // seahorse conversation key
    Rounds          int
    MaxRounds       int              // default 5
    Directions      []Direction      // 当前方案方向
    PrevClusterCount int
    ConvergeStreak  int
    SelectedClients []ClientSelection
    HumanInput      []Message        // 人类插入评论
}

type Direction struct {
    ID          string   `json:"id"`
    Label       string   `json:"label"`
    Description string   `json:"description"`
    Proposer    string   `json:"proposer"`     // client_id
    Round       int      `json:"round"`
    OpennspecCategory string `json:"openspec_category,omitempty"`
}

type ClientSelection struct {
    ClientID string `json:"client_id"`
    Role     string `json:"role"`
    Fallback bool   `json:"fallback,omitempty"`
}
```

- [ ] 结构体定义
- [ ] JSON 序列化/反序列化 (用于存入 WorkflowSession.directions)

## U2.2: 动态 Client 选择

**文件**: `pkg/agent/hermes_orchestrator.go`

```go
// 任务类型 → 所需角色映射
var roleMapping = map[string][]string{
    "system-design":    {"requirement-analyst", "architect", "researcher"},
    "market-research":  {"researcher", "data-analyst"},
    "code-review":      {"reviewer", "coder"},
    "tech-selection":   {"researcher", "architect"},
    "debugging":        {"debugger", "coder"},
    "simple-qa":        {"researcher"},
}
```

- [ ] `SelectClients(profile TaskProfile, phase WorkflowPhase) []ClientSelection`
- [ ] 调用 `Registry.ListByRole(role)` 精确匹配
- [ ] 无匹配时: `List()` 全部 + `skillMatchScore` fallback
- [ ] `skillMatchScore(client, targetRole)` — 简单关键词匹配即可 (v1)

## U2.3: 任务板创建与写入

**文件**: `pkg/agent/hermes_taskboard.go` (新增)

```
任务板 = 专用 seahorse conversation: "task-board:{taskID}:brainstorm"

每条 Client 发言 → 写入任务板:
  {
    role: "assistant",
    content: "...",
    metadata: {
      client_id: "client-A",
      client_role: "architect",
      round: 2,
      type: "proposal",
      openspec_category: "RESEARCH"
    }
  }
```

- [ ] `CreateTaskBoard(ctx, taskID, phase) (string, error)` — 返回 seahorse key
- [ ] `AppendToTaskBoard(ctx, boardKey, message, metadata)`
- [ ] `GetTaskBoardHistory(ctx, boardKey) ([]Message, error)` — 全部消息
- [ ] `GetTaskBoardRoundHistory(ctx, boardKey, round) ([]Message, error)` — 按轮次过滤

## U2.4: reef_submit_task 扩展 (role 参数)

**文件**: `pkg/tools/reef_tools.go` (修改)

现有 `reef_submit_task` 发送给任意 Client。需要增加可选 `client_role` 参数:

```go
type ReefSubmitArgs struct {
    Instruction string   `json:"instruction"`
    ClientRole  string   `json:"client_role,omitempty"` // 新增
    ClientID    string   `json:"client_id,omitempty"`
    Timeout     int      `json:"timeout,omitempty"`
}
```

- [ ] `client_role` 参数 → Server 调用 `Registry.ListByRole(role)` 选择 Client
- [ ] 兼容: 不传 role 时行为不变 (任意可用 Client)
- [ ] Client 返回结果写入任务板而非默认 conversation

## U2.5: Round 控制引擎

**文件**: `pkg/agent/hermes_brainstorm.go`

```
Round 1 "发散":
  prompt = "你是 {role}。针对 {task}，从你的角色角度提出 2-3 个方案方向。
           每个方向 200 字以内。"

Round 2-N "交叉":
  prompt = "阅读以下讨论记录: {taskBoardRounds}。
           在已有方向基础上提出修正、补充或新方向。"
```

- [ ] `StartRound(session, round) ([]SubmitResult, error)`
- [ ] 并行 reef_submit_task 到所有 selectedClients
- [ ] 收集所有 Client 结果 → 写入任务板
- [ ] `BuildRoundPrompt(session, round) string` — 生成 Prompt (含方法论注入)

## U2.6: 方法论文本注入

**文件**: `pkg/agent/hermes_prompt.go` (修改)

每个 Client 的 Prompt 自动包含 OpenSpec 标签要求:

```
## 输出格式
请在你的回复末尾使用以下标签分类你的贡献:
<!-- OPENDSPEC_CATEGORY: RESEARCH|DESIGN|CONSTRAINT|QUESTION -->
<!-- OPENDSPEC_REFERENCES: 方向A, 方向B -->
<!-- OPENDSPEC_SHOULD: 你的建议 (RFC 2119) -->
```

- [ ] `BuildMethodologyInjection() string` — OpenSpec 输出标签
- [ ] `BuildRoundSummary(boardMessages) string` — 任务板历史摘要

## U2.7: 收敛检测

**文件**: `pkg/agent/hermes_brainstorm.go`

```go
func (b *BrainstormSession) CheckConvergence() bool {
    // 从最近 2 轮提取所有方向
    directions := b.extractDirections(last2Rounds)
    
    // 简单聚类: 按关键词 Jaccard 相似度 ≥ 0.5 合并
    clusters := simpleCluster(directions)
    
    if len(clusters) <= 5 && len(clusters) == b.PrevClusterCount {
        b.ConvergeStreak++
    } else {
        b.ConvergeStreak = 0
    }
    b.PrevClusterCount = len(clusters)
    
    return b.ConvergeStreak >= 2 || b.Rounds >= b.MaxRounds
}
```

- [ ] `extractDirections(messages) []string` — LLM 提取方向摘要 (轻量调用)
- [ ] `simpleCluster(directions) [][]string` — 关键词 Jaccard 聚类
- [ ] 收敛后自动保存 directions 到 WorkflowSession

## U2.8: 人类介入处理

- [ ] "继续" / "next" → 推进到下一轮
- [ ] "评审吧" / "review" → 进入 Phase 3
- [ ] "方向 X 优先" → 注入为下一轮 Prompt 权重
- [ ] "跳过评审" → 直接进入 Phase 4
- [ ] "停" / "stop" → PhaseAborted
- [ ] 具体评论 → 追加到任务板，注入到下一轮每个 Client 的 Prompt

## U2.9: 编译验证

- [ ] `go build` 通过
- [ ] 手动测试: 2+ Client 在线 + 发送 "设计一个 XX 系统" → 任务板创建 → Round 1 分发 → 收集结果 → Round 2...

## U2.10: 自收敛测试 (2 Client 模拟)

- [ ] 2 个不同 role 的 Client 在线
- [ ] 发送 "帮我选一个消息队列方案"
- [ ] 观察: 3 轮后自动收敛，产出 2-3 个方向
- [ ] 验证: directions 已保存到 WorkflowSession

---

# Wave 3: 风暴评审

> 目标: Phase 3 完整 — 多 reviewer 评分 Brainstorm 产出的方向，排序推荐
> 产出: ReviewSession 实现，加权评分
> 依赖: Wave 2
> 验收: 方向 A/B/C → 2 个 reviewer 评分 → 排序 → Top-N 推荐

## U3.1: ReviewSession 结构

**文件**: `pkg/agent/hermes_review.go` (新增)

```go
type ReviewSession struct {
    TaskBoard   string           // "task-board:{taskID}:review"
    Directions  []Direction
    Scores      []DirectionScores
    FinalRank   []RankedDirection
}

type DirectionScores struct {
    DirectionID string         `json:"direction_id"`
    ReviewerID  string         `json:"reviewer_id"`
    Expertise   string         `json:"expertise"`
    Feasibility int            `json:"feasibility"`
    Innovation  int            `json:"innovation"`
    Cost        int            `json:"cost"`
    Risk        int            `json:"risk"`
    Scalability int            `json:"scalability"`
    Comment     string         `json:"comment"`
}
```

- [ ] 结构体定义
- [ ] JSON 序列化

## U3.2: 评分维度配置

**文件**: `pkg/config/config.go` (添加)

```json
"hermes": {
    "review": {
        "dimensions": ["feasibility", "innovation", "cost", "risk", "scalability"],
        "weights": {
            "feasibility": 0.30,
            "innovation": 0.25,
            "cost": 0.20,
            "risk": 0.15,
            "scalability": 0.10
        }
    }
}
```

- [ ] `HermesReviewConfig` 结构体
- [ ] config.json 加载

## U3.3: 评审分发

- [ ] `StartReview(session)` — 创建 review 任务板
- [ ] 选择 reviewer Client (role=reviewer, 1-3 个)
- [ ] 每个 reviewer 收到: 所有方向描述 + 5 维评分表
- [ ] reef_submit_task 并行发送

## U3.4: 加权聚合

```go
func AggregateScores(scores []DirectionScores, weights map[string]float64) []RankedDirection {
    // 按 directionID 分组
    // 加权总分 = Σ(score × weight)
    // 排序
}
```

- [ ] `AggregateScores` 实现
- [ ] 输出 JSON → 存入 review 任务板

## U3.5: 编译验证

- [ ] `go build` 通过
- [ ] 手动测试: 给定 3 个方向 + 2 个 reviewer → 评分 → 排序输出

---

# Wave 4: 调研 + 设计 + 报告

> 目标: Phase 4-6 完整 — 深度调研 Top-N 方向 → PRD/技术设计 → 最终报告
> 产出: 完整 6 阶段流水线可运行
> 依赖: Wave 3
> 验收: 完整工作流从 Intake 到 Report

## U4.1: ResearchSession (Phase 4)

**文件**: `pkg/agent/hermes_research.go` (新增)

- [ ] 每方向 1 个 researcher Client
- [ ] reef_submit_task 并行
- [ ] 调研报告格式标准化 (Markdown)
- [ ] 结果存入 seahorse conversation "task-board:{taskID}:research:{directionID}"

## U4.2: DesignSession (Phase 5)

**文件**: `pkg/agent/hermes_design.go` (新增)

- [ ] 人类确认方向后 → analyst + architect Client
- [ ] PRD 模板 + 技术设计模板
- [ ] 结果存入 seahorse

## U4.3: Report Generation (Phase 6)

**文件**: `pkg/agent/hermes_report.go` (新增)

- [ ] 聚合所有阶段 seahorse 内容
- [ ] 生成 Markdown 报告
- [ ] 通过 message tool 发送 (摘要 + 完整报告文件)
- [ ] WorkflowSession → PhaseComplete

## U4.4: OpenSpec 导出指令

**文件**: `pkg/agent/hermes_orchestrator.go`

"导出为 OpenSpec" → 遍历所有 seahorse task board conversations → 生成 OpenSpec 文件:
```
openspec/changes/{taskID}/
  ├── .openspec.yaml
  ├── proposal.md
  ├── design.md
  ├── specs/research-questions.md
  ├── specs/{direction}/requirements.md
  ├── tasks.md
  └── REPORT.md
```

- [ ] `ExportToOpenSpec(session)` — 通过 write_file tool 生成
- [ ] 仅在人类触发时调用

## U4.5: 完整流程串联

**文件**: `pkg/agent/hermes_workflow.go`

- [ ] `processHermesMessage` 完善: PhaseIntake → PhaseBrainstorm → PhaseReview → PhaseResearch → PhaseDesign → PhaseReport
- [ ] 每阶段切换自动更新 WorkflowSession.phase
- [ ] 人类中止 → PhaseAborted

## U4.6: 编译验证

- [ ] `go build` 通过
- [ ] 手动 E2E 测试 (2 Client)

---

# Wave 5: HermesGuard + Prompt 注入

> 目标: Hermes 模式下 LLM 尝试直接调用 web_search/exec 被 Guard 拦截并引导用 reef_submit_task
> 产出: HermesGuard 生效，遵从率 ≥ 90%
> 依赖: Wave 1 (可与 Wave 2-4 并行)
> 验收: Hermes 模式 → LLM 调用 web_search → Guard 拦截返回引导消息

## U5.1: HermesGuard 实现

**文件**: `pkg/agent/hermes_guard.go` (新增)

```go
type HermesGuard struct{}

func (g *HermesGuard) Allow(ctx context.Context, toolName string, mode ConversationMode) bool {
    if mode != ModeHermes {
        return true  // Chat 模式全放行
    }
    // Hermes 模式仅放行协调工具
    switch toolName {
    case "reef_submit_task", "reef_query_task", "reef_status",
         "message", "reaction", "cron",
         "read_file", "write_file":  // read/write 允许(查看任务板)
        return true
    default:
        return false
    }
}

func (g *HermesGuard) DenyMessage(toolName string) string {
    return fmt.Sprintf(
        "作为 Hermes 协调者，你不能直接使用 %s。请使用 reef_submit_task 分发给 Client 执行。", 
        toolName)
}
```

- [x] `Allow(toolName, mode) bool`
- [x] `DenyMessage(toolName) string`
- [x] 集成到 Pipeline 的 tool execution 路径

## U5.2: Guard 集成点

**文件**: `pkg/agent/pipeline_tools.go` (修改)

在 Tool execution 前检查:
```go
if mode == ModeHermes && !guard.Allow(toolName) {
    return ToolResult{Error: guard.DenyMessage(toolName)}
}
```

- [x] 确定 Tool execution 入口点
- [x] 注入 HermesGuard 检查
- [x] LLM 收到拒绝消息后应重选工具

## U5.3: Prompt 方法论注入完善

**文件**: `pkg/agent/hermes_prompt.go` (修改)

- `BuildHermesModeTag()` — "你是 Hermes 协调者..."
- `BuildMethodologyInjection(complexity)` — OpenSpec 标签要求 (complexity < 3 跳过)
- 每次 system prompt 生成时自动注入

## U5.4: 编译验证

- [x] `go build` 通过
- [x] 手动测试: Hermes 模式 → "帮我搜索 XXX" → Guard 拦截 → 引导 reef_submit_task

---

# Wave 6: 集成测试 + E2E

> 目标: 全流程 E2E 验证，边界情况测试
> 产出: 测试报告
> 依赖: Wave 0-5 全部
> 验收: 3 E2E 场景全通过

## U6.1: E2E 场景 1 — 系统设计全流程

```
用户: "帮我设计一个分布式任务调度系统"
→ Phase 1: complexity=5, Type=system-design
→ Phase 2: 3 Client (analyst+architect+researcher) × 3 轮 → 3 个方向
→ Phase 3: 2 reviewer 评分 → 方向B 最高
→ Phase 4: 方向B 深度调研 → 调研报告
→ Phase 5: analyst PRD + architect 技术设计
→ Phase 6: 完整报告
```

- [ ] 全流程无卡死
- [ ] seahorse 各阶段 conversation 独立隔离
- [ ] WorkflowSession 状态正确跟踪

## U6.2: E2E 场景 2 — 模式切换 + 断点恢复

```
用户: "设计 XX 系统"
→ Phase 2 Round 1 完成
用户: "切换聊天模式"
→ Chat 模式，"帮我查天气" → 正常回复
用户: "切换Hermes模式"
→ "当前阶段: brainstorm (第 1 轮)"
→ 继续 Round 2
```

- [ ] 切换后上下文隔离
- [ ] WorkflowSession 恢复正确
- [ ] 任务板内容完整

## U6.3: E2E 场景 3 — 人类介入 + 降级

```
用户: "帮我审查这段代码"
→ Phase 2 Round 1
用户: "评审吧"  (跳过中间轮次)
→ Phase 3 评审
用户: "停"
→ PhaseAborted
→ 再发新的需求
→ PhaseIntake 重新开始
```

- [ ] 人类指令正确路由
- [ ] 中止后重新开始不残留状态

## U6.4: 边界测试

- [ ] 0 Client 在线 → Hermes 降级提示 "无可用 Client"
- [ ] 1 Client 在线 → 仅用该 Client (role 匹配 or fallback)
- [ ] 角色不匹配 → fallback 到最接近的 Client
- [ ] 超长讨论 MaxRounds 到达 → 强制收敛
- [ ] 并发两个 conversation 各自独立工作流

## U6.5: 性能验证

- [ ] Phase 2 3 Client × 3 轮 token ≤ 70k
- [ ] 模式切换操作 < 100ms
- [ ] Store 操作无 SQL 错误

---

## 风险 & 缓解

| 风险 | 缓解 |
|------|------|
| reef_submit_task role 参数改动破坏现有 Client | 可选参数，无 role 时行为不变 |
| 收敛检测不准确导致过早/过晚收敛 | MaxRounds 硬限制兜底 + 人类可手动 "评审吧" |
| HermesGuard 拦截后 LLM 重试同样工具 (loop) | 最多拒绝 3 次，第 4 次提示 "请切换聊天模式使用此工具" |
| 多 Client 并行写入 seahorse 冲突 | Server 串行化 reef_submit_task 分发 (非并发写入) |
| DeepSeek 缓存命中率不达预期 | 对比 ModeTag on/off 的 api_prompt_tokens_cache_hit 数值 |

---

## 依赖关系总结

```
                Wave 0 (模式系统)
                    │
            ┌───────┴───────┐
            ▼               ▼
        Wave 1 (状态机)   Wave 5 (Guard+Prompt)
            │               │
            ▼               │
        Wave 2 (头脑风暴)    │
            │               │
            ▼               │
        Wave 3 (评审)       │
            │               │
            ▼               │
        Wave 4 (调研+设计)   │
            │               │
            └───────┬───────┘
                    ▼
                Wave 6 (集成测试)
```
