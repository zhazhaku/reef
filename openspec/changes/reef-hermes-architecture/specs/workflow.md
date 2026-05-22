---
change: reef-hermes-architecture
artifact: specs
section: workflow
created: 2026-05-09
updated: 2026-05-09
---

# Spec: Hermes 智能协作工作流

## W1. 工作流状态机

### W1.1 状态定义

```go
type WorkflowPhase string

const (
    PhaseIdle         WorkflowPhase = "idle"          // 无活跃工作流
    PhaseIntake       WorkflowPhase = "intake"        // 需求接收：Server 解析任务
    PhaseBrainstorm   WorkflowPhase = "brainstorm"    // 头脑风暴：多 client 多轮讨论
    PhaseReview       WorkflowPhase = "review"        // 风暴评审：专家评分
    PhaseResearch     WorkflowPhase = "research"      // 需求调研：深度分析
    PhaseDesign       WorkflowPhase = "design"        // 需求设计：产出 PRD
    PhaseReport       WorkflowPhase = "report"        // 设计报告：最终产出
    PhaseComplete     WorkflowPhase = "complete"      // 完成
    PhaseAborted      WorkflowPhase = "aborted"       // 人类中止
)
```

### W1.2 状态转换

```
idle
  │
  ├── 简单消息 → idle (直接回复)
  └── 复杂任务 → intake
                   │
                   ▼
                 brainstorm ──→ human_abort → aborted
                   │
                   ▼
                 review ──→ human_abort → aborted
                   │
                   ▼
                 research ──→ human_abort → aborted
                   │
                   ▼
                 design ──→ human_abort → aborted
                   │
                   ▼
                 report → complete → idle
```

### W1.3 转换条件

| 转换 | 触发条件 |
|------|----------|
| idle → intake | 任务复杂度 ≥ 3 且需要多角色协作 |
| intake → brainstorm | 自动（解析完成后立即进入） |
| brainstorm → review | 收敛（方向 ≤ 5 且无新方向）或 MaxRounds 或人类命令 |
| brainstorm/review → idle | 人类决定跳过后续阶段 |
| review → research | 人类确认 Top-N 方向 |
| research → design | 人类确认调研结果 |
| design → report | 设计产出完成 |

---

## W2. 模式系统

### W2.1 模式定义

```go
type ConversationMode string

const (
    ModeChat   ConversationMode = "chat"    // 普通 agent Chat，无 Hermes
    ModeHermes ConversationMode = "hermes"  // Hermes 6 阶段工作流
)
```

### W2.2 切换指令

| 指令 | 效果 |
|------|------|
| `切换聊天模式` / `chat mode` | 保存 Hermes 状态 → 进入 ModeChat |
| `切换Hermes模式` / `hermes mode` | 恢复 Hermes 状态 → 进入 ModeHermes |
| `查看模式` / `status` | 显示当前模式 + 工作流进度 |

### W2.3 指令处理位置

消息进入 AgentLoop 前在 `pipeline_setup.go` 拦截：

```go
func (p *Pipeline) preProcessModeSwitch(msg *Message) (bool, *ModeSwitchResult) {
    cmd := detectModeCommand(msg.Content)
    if cmd == nil {
        return false, nil
    }
    
    switch cmd.Action {
    case "switch_to_chat":
        p.modeStore.SaveSession(msg.ConversationID, p.activeSession)
        p.modeStore.SetMode(msg.ConversationID, ModeChat)
        return true, &ModeSwitchResult{Message: "已切换到聊天模式"}
        
    case "switch_to_hermes":
        session := p.modeStore.RestoreSession(msg.ConversationID)
        p.modeStore.SetMode(msg.ConversationID, ModeHermes)
        if session != nil {
            return true, &ModeSwitchResult{
                Message: fmt.Sprintf("已切换到 Hermes 模式。当前阶段: %s (第 %d 轮)",
                    session.Phase, session.CurrentRound),
            }
        }
        return true, &ModeSwitchResult{Message: "已切换到 Hermes 模式。无进行中的工作流。"}
    }
}
```

### W2.4 AgentLoop 模式分支

```go
func (al *AgentLoop) processMessage(ctx context.Context, msg Message) {
    mode := al.getConversationMode(msg.ConversationID)

    switch mode {
    case ModeChat:
        al.processChatMode(ctx, msg)

    case ModeHermes:
        session := al.getWorkflowSession(msg.ConversationID)
        if session == nil {
            al.processIntake(ctx, msg)   // 首次 → Phase 1
        } else {
            al.resumeWorkflow(ctx, session, msg)  // 恢复到保存的阶段
        }
    }
}
```

### W2.5 状态持久化

```
seahorse SQLite 新增表:

conversation_mode:
  conversation_id  INTEGER PRIMARY KEY,
  mode             TEXT NOT NULL DEFAULT 'chat',
  updated_at       DATETIME

hermes_workflow_sessions:
  conversation_id  INTEGER PRIMARY KEY,
  task_id          TEXT,
  phase            TEXT,
  task_profile     BLOB,          -- JSON
  current_round    INTEGER DEFAULT 1,
  max_rounds       INTEGER DEFAULT 5,
  directions       BLOB,          -- JSON
  converge_streak  INTEGER DEFAULT 0,
  selected_clients BLOB,          -- JSON
  task_boards      BLOB,          -- JSON: phase→conversation mapping
  openspec_path    TEXT,           -- 关联的 openspec 路径
  gsd_track_id     TEXT,           -- 关联的 GSD track
  next_human_gate  TEXT,
  updated_at       DATETIME
```

---

## W3. 上下文隔离

### W3.1 设计原则

```
CONSTRAINT: 两种模式的 seahorse conversation MUST 完全隔离
            → 一个模式的对话历史不可见另一个模式
            → 切换模式后 LLM 看不到切换前另一模式的上下文
```

### W3.2 Session Key 隔离

```go
const (
    SessionKeyChatSuffix   = "chat"
    SessionKeyHermesSuffix = "hermes"
)

func sessionKey(convID string, mode ConversationMode) string {
    switch mode {
    case ModeChat:
        return fmt.Sprintf("conv:%s:%s", convID, SessionKeyChatSuffix)
    case ModeHermes:
        return fmt.Sprintf("conv:%s:%s", convID, SessionKeyHermesSuffix)
    }
}
```

### W3.3 切换流程示意

```
用户: "切换聊天模式"
  ├── 1. 序列化 Hermes session → hermes_workflow_sessions 表
  ├── 2. 切换指令本身 NOT 写入任何 conversation
  │     → 不污染 Chat 上下文，不污染 Hermes 上下文
  ├── 3. AgentLoop 绑定到 conv "{id}:chat"
  └── 后续消息写入 conv "{id}:chat"

用户: "切换Hermes模式"
  ├── 1. Chat 上下文已在 conv "{id}:chat"
  ├── 2. 切换指令 NOT 写入
  ├── 3. 反序列化 session → 恢复 phase/round/directions
  ├── 4. AgentLoop 绑定到 conv "{id}:hermes"
  └── LLM 看到的上下文: 只有 Hermes 相关历史
     → 完全不知道中间发生过"查天气"等 Chat 对话
```

---

## W4. 前缀稳定性

### W4.1 设计原则

```
CONSTRAINT: SystemPrompt 前缀两种模式 MUST 一致
            → 仅 ModeTag 块 (≤200 tokens) 可不同
            → DeepSeek cursor cache 命中率 90%+
```

### W4.2 SystemPrompt 结构

```
┌────────────────────────────────────────────┐
│  Stable Prefix (DeepSeek Cacheable)        │
│  ┌──────────────────────────────────────┐  │
│  │ kernel.identity  "You are Reef..."   │  │
│  │ tool.definitions  (所有 Tool，统一)  │  │
│  │ capability.skills (Skill catalog)    │  │
│  └──────────────────────────────────────┘  │
│  ~10,000 tokens                             │
├────────────────────────────────────────────┤
│  Tunable Block (仅此处模式间不同)          │
│  ┌──────────────────────────────────────┐  │
│  │ kernel.mode                           │  │
│  │   Chat:   "You are Reef..."  (~100t) │  │
│  │   Hermes: "You are Hermes..."(~200t) │  │
│  └──────────────────────────────────────┘  │
└────────────────────────────────────────────┘
```

### W4.3 Tool 统一注册

```
两种模式 MUST 注册相同的 Tool 全集:
  → SystemPrompt 前缀完全一致
  → DeepSeek 可复用 cursor cache

Hermes 模式下 LLM 仍可尝试直接调用 web_search/exec:
  → HermesGuard 运行时拦截
  → 返回: "作为 Hermes 协调者，你不能直接执行此操作。
           请使用 reef_submit_task 分发给 Client。"
  → LLM 学习后遵从率 ~95%
```

### W4.4 缓存命中率

| 场景 | 缓存命中 |
|------|:---:|
| Chat→Chat | 90%+ |
| Hermes→Hermes | 90%+ |
| Chat→Hermes | 90%+ (前缀共享) |
| Hermes→Chat | 90%+ (前缀共享) |

---

## W5. OpenSpec + GSD 自动融入

### W5.1 设计原则

```
CONSTRAINT: OpenSpec 和 GSD 方法论 MUST 自动嵌入
            → 用户无需显式输入 "用 OpenSpec"
            → 无需手动创建 proposal/design/specs/tasks 文件
            → 无需手动管理 GSD milestones
```

### W5.2 各阶段自动产出

| 阶段 | Server 自动操作 | OpenSpec 产出 | GSD 行为 |
|------|----------------|-------------|----------|
| Intake | TaskProfile 分析 | `proposal.md` 骨架 | 创建 GSD track + 5 milestones |
| Brainstorm | 每轮后提取 | `specs/research-questions.md` | — |
| Review | 评分 JSON 转换 | `design.md` review section | milestone: review ✓ |
| Research | 调研报告转换 | `specs/{direction}/requirements.md` | milestone: research ✓ |
| Design | PRD→proposal update + design.md | `design.md` + `tasks.md` | milestone: design ✓ |
| Report | 聚合所有产物 | `REPORT.md` + 完整 bundle | GSD track → completed |

### W5.3 OpenSpec 产出结构

```
openspec/changes/{task-id}/
  ├── .openspec.yaml
  ├── proposal.md                     (Phase 1→5 持续更新)
  ├── design.md                       (Phase 3 review + Phase 5 完整)
  ├── specs/
  │   ├── research-questions.md       (Phase 2)
  │   ├── direction-A/requirements.md (Phase 4, RFC 2119)
  │   └── direction-B/requirements.md (Phase 4)
  ├── tasks.md                        (Phase 5, 含 phase/deps/complexity)
  └── REPORT.md                       (Phase 6)
```

### W5.4 Prompt 自动注入

```go
func buildHermesMethodologyInjection(profile TaskProfile) string {
    if profile.Complexity < 3 {
        return ""  // 简单任务无需全量方法论
    }
    return `
## 方法论: OpenSpec + GSD (自动生效)

你正在以 Hermes 工作流协调者身份运行。以下方法论自动应用于所有阶段产出:

### OpenSpec (spec-driven)
- SHALL: 所有产出使用 proposal → design → specs → tasks 结构
- SHALL: Requirements 使用 RFC 2119 关键词 (SHALL/MUST/SHOULD)
- SHALL: 每个 requirement 标注 category (SWARM/SCHED/TASK/LIFE/RETRY/CONN/ROLE/ADMIN)
- Brainstorm 产出 → research-questions (specs/)
- Review 产出 → design review annotations (design.md)
- Research 产出 → specs/{direction}/requirements.md
- Design 产出 → design.md + tasks.md

### GSD (Getting Stuff Done)
- SHALL: 每个阶段 = 一个 milestone
- SHALL: 每个方案方向 = 一个 task item
- SHALL: 阶段切换时检查 milestone 是否完成
`
}
```

### W5.5 人类无需操作

| 不操作 | 说明 |
|--------|------|
| ❌ 说"用 OpenSpec" | OpenSpec 自动启用 (complexity≥3) |
| ❌ 说"创建 GSD track" | Intake 完成时自动创建 |
| ❌ 创建 proposal/design/specs 文件 | Server 自动写入 |
| ❌ 管理 milestones | 阶段切换时自动 check |
| ✅ 确认关键决策 | 仅需: 选方向？进入调研？确认设计？ |

---

## W6. 任务类型识别

### W6.1 识别规则

```
输入: 用户消息 + 历史上下文

Server LLM 分类调用（轻量，~500 tokens）:
  Prompt: "分析以下用户需求，输出 JSON:
    {domain, type, complexity(1-5), estimated_clients, keywords}"

  type 枚举: system-design | market-research | code-review |
             tech-selection | debugging | simple-qa | unknown

  complexity:
    1 = 单步简单任务（直接回复）
    2 = 单 Client 可完成
    3 = 需 2-3 Client 协作
    4 = 需 4+ Client 多轮协作
    5 = 需完整工作流
```

### W6.2 各阶段 Client 数量

| Phase | 最少 Client | 最多 Client | 说明 |
|-------|:----------:|:----------:|------|
| brainstorm | 2 | 5 | 2 个不同角色即可形成讨论 |
| review | 1 | 3 | 至少 1 个评审专家，最多 3 个不同维度 |
| research | 1 | N | 每个方向 1 个 researcher |
| design | 1 | 2 | analyst + architect |

### W6.3 动态 Client 选择算法

```go
func selectClients(profile TaskProfile, phase WorkflowPhase, online []ClientInfo) []ClientSelection {
    required := getRoleRequirements(profile.Type, phase)
    
    var selected []ClientSelection
    for _, role := range required {
        if c := findByExactRole(online, role); c != nil {
            selected = append(selected, ClientSelection{Client: c, Role: role})
        } else {
            // Fallback: skills 匹配度
            best := findBySkillMatch(online, role, profile)
            selected = append(selected, ClientSelection{Client: best, Role: role, Fallback: true})
        }
    }
    return selected
}
```

---

## W7. 头脑风暴详细设计

### W7.1 任务板

```
任务板 ID: "task-board:{taskID}:brainstorm"
  = seahorse conversation，存储所有参与者的发言

每条发言 metadata:
  {
    client_id: "client-xxx",
    client_role: "requirement-analyst",
    round: 2,
    type: "proposal" | "comment" | "question",
    openspec: {
      category: "RESEARCH",
      references: ["方向A", "约束X"]
    }
  }
```

### W7.2 轮次控制

```
第 1 轮: 发散
  Server → 各 Client: "从你的角色角度提出 2-3 个方向"

第 2 轮: 交叉
  Server 收集第 1 轮 → 注入 Prompt:
  "阅读以下讨论: {汇总}。提出你的新见解或修正"

第 3 轮: 深化
  Server 识别分歧点 → 点名特定 Client 深入:
  "architect，{analyst} 提出约束 X，如何影响你的方案？"

收敛判断:
  if 方向数 <= 5 AND 连续 2 轮无新方向:
    进入评审阶段
```

### W7.3 收敛检测

```go
func (b *BrainstormSession) checkConvergence() bool {
    recent := b.extractDirections(last2Rounds)
    clusters := clusterBySemanticSimilarity(recent, threshold=0.8)

    if len(clusters) <= 5 && len(clusters) == b.prevClusterCount {
        b.convergeStreak++
    } else {
        b.convergeStreak = 0
    }
    b.prevClusterCount = len(clusters)

    return b.convergeStreak >= 2 || b.Rounds >= b.MaxRounds
}
```

### W7.4 人类参与

| 人类指令 | 效果 |
|----------|------|
| "继续" / "下一轮" | 推进到下一轮 |
| "方向 X 优先" | Server 调整权重 |
| "评审吧" | 手动进入 Phase 3 |
| "跳过评审" | 进入 Phase 4 |
| 具体评论 | 注入到所有 Client 的下一轮 Prompt |

---

## W8. 评审阶段

### W8.1 评分维度 & 权重

| 维度 | 权重 | 评审者专长 |
|------|:---:|------|
| 技术可行性 | 0.30 | reviewer-1 |
| 创新性 | 0.25 | reviewer-2 |
| 实施成本 | 0.20 | reviewer-3 |
| 风险 | 0.15 | reviewer-1 (兼) |
| 扩展性 | 0.10 | reviewer-2 (兼) |

### W8.2 分数聚合

```
加权总分 = Σ(score × weight)

排序 → Top-N 推荐 → 呈现人类确认
```

### W8.3 交叉校验（可选）

```
评审完成后各 reviewer 可互相查看评分:
  "方向 A 可行性 8 分偏高，我发现 X 问题，建议 6 分"
  → 对方确认 → 最终分数取修正后平均
```

---

## W9. 调研 & 设计阶段

### W9.1 调研 Prompt 模板

```
深度调研 {方向描述}。
搜索最新论文、开源项目、工业实践。
输出: 调研报告（5000 字以上，含引用）
```

### W9.2 调研报告格式

```markdown
# 调研报告: {方向名称}
## 1. 概述
## 2. 现有方案分析
## 3. 技术可行性
## 4. 关键挑战
## 5. 推荐实施路径
## 6. 引用
```

### W9.3 设计产出

| 角色 | 产出 | 格式 |
|------|------|------|
| analyst | PRD | 背景/用户故事/功能需求/非功能需求/验收标准 |
| architect | 技术设计 | 架构图/组件设计/数据模型/API/部署方案 |

---

## W10. 配置扩展

```json
{
  "hermes": {
    "mode": "coordinator",
    "fallback": true,
    "workflow": {
      "enabled": true,
      "brainstorm": {
        "max_rounds": 5,
        "max_directions": 5,
        "converge_rounds": 2
      },
      "review": {
        "dimensions": ["feasibility", "innovation", "cost", "risk", "scalability"],
        "weights": {
          "feasibility": 0.30,
          "innovation": 0.25,
          "cost": 0.20,
          "risk": 0.15,
          "scalability": 0.10
        },
        "cross_validation": true
      },
      "research": {
        "max_parallel": 3,
        "min_report_length": 5000
      }
    },
    "openspec": {
      "auto_enable_complexity": 3,
      "output_dir": "openspec/changes"
    },
    "gsd": {
      "auto_create_track": true,
      "milestones": ["brainstorm", "review", "research", "design", "report"]
    }
  }
}
```

---

## W11. Token 消耗估算

| Phase | 调用 | ~Tokens |
|-------|:---:|:---:|
| 1 Intake | 1 (轻量分类) | 0.5k |
| 2 Brainstorm | 3 Client × 3 轮 | 63k |
| 3 Review | 2 reviewers | 22k |
| 4 Research | 2 directions | 22k |
| 5 Design | 2 clients | 20k |
| **合计** | | **~127k** |

DeepSeek V4 Pro: ¥0.44/完整工作流。

---

## W12. 完整流程示例

```
飞书频道:

用户: "帮我设计一个分布式任务调度系统"
  → TaskProfile: complexity=5 → 自动进入 Hermes 模式
  → Phase 1 Intake → proposal.md + GSD track
  → Phase 2 Brainstorm 第 1 轮

用户: "切换聊天模式"
  → "已切换到聊天模式"
  → Session 已保存 (brainstorm round=1)

用户: "帮我查天气"
  → Chat 模式 → web_search → 回复

用户: "切换Hermes模式"
  → "已切换到 Hermes 模式。当前阶段: brainstorm (第 1 轮)"
  → LLM 看到: 仅 Hermes 上下文（不知道查过天气）

用户: "继续"
  → Phase 2 Brainstorm 第 2 轮

用户: "评审吧"
  → Phase 3 Review → 评分排序 → Top-N

用户: "方向 A + B 都深入研究"
  → Phase 4 Research

用户: "确认方向 B"
  → Phase 5 Design → PRD + 技术设计

→ Phase 6 Report → 完整 OpenSpec bundle + GSD track completed
```
