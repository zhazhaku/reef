---
change: reef-hermes-architecture
artifact: design
phase: research
---

# Design: Hermes 能力架构 — 研究设计

## 1. 概念模型：Hermes 三角色能力架构

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Hermes 能力架构                                  │
│                                                                     │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐    │
│  │  Coordinator     │  │  Executor        │  │  Full            │    │
│  │  (协调者)        │  │  (执行者)        │  │  (全能)          │    │
│  │                 │  │                 │  │                 │    │
│  │  触发: --server  │  │  触发: Client连接│  │  触发: 默认启动  │    │
│  │                 │  │                 │  │                 │    │
│  │  身份: 团队协调者│  │  身份: 任务专家  │  │  身份: PicoClaw  │    │
│  │                 │  │                 │  │                 │    │
│  │  Tool: 仅协调   │  │  Tool: 全部     │  │  Tool: 全部      │    │
│  │  - reef_submit  │  │  - web_search   │  │  - 全部工具      │    │
│  │  - reef_query   │  │  - exec         │  │                 │    │
│  │  - reef_status  │  │  - read_file    │  │  SubTurn: ✅    │    │
│  │  - send_message │  │  - write_file   │  │  ReefSubmit: ❌ │    │
│  │                 │  │  - ...全部      │  │                 │    │
│  │  禁止: 直接执行 │  │  禁止: 外部分发 │  │  无约束          │    │
│  │  SubTurn: ❌    │  │  SubTurn: ✅    │  │                 │    │
│  │  ReefSubmit: ✅ │  │  ReefSubmit: ❌ │  │                 │    │
│  └─────────────────┘  └─────────────────┘  └─────────────────┘    │
│                                                                     │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │  降级策略                                                    │   │
│  │  Coordinator + 无Client → 临时 Full（超时后降级）            │   │
│  │  Executor + 无Server → 临时 Full（独立工作）                │   │
│  └─────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

## 2. 约束链：三层防绕过

```
用户消息 → AgentLoop
             │
             ▼
     ┌───────────────┐
     │ Layer 1:      │  SystemPrompt 角色定义
     │ Prompt 约束    │  "你是团队协调者，只能使用 reef_* 工具"
     │ (软约束, ~95%) │  → LLM 主动选择正确工具
     └───────┬───────┘
             │
             ▼
     ┌───────────────┐
     │ Layer 2:      │  Tool 注册过滤
     │ 注册 约束      │  Coordinator 只注册 reef_* + send_message
     │ (硬约束, 100%) │  → 物理上移除不该有的工具
     └───────┬───────┘
             │
             ▼
     ┌───────────────┐
     │ Layer 3:      │  Tool Guard
     │ 运行时 Guard   │  降级时动态放行/恢复
     │ (动态约束)     │  → 运行时拦截意外调用
     └───────┬───────┘
             │
             ▼
     Tool 执行
```

## 3. SystemPrompt 注入设计

### 3.1 Coordinator 身份 Prompt

```markdown
# Identity

You are a **Team Coordinator** in a multi-agent system. Your role is to:

1. **Understand** the user's request
2. **Decide** whether to handle it directly (simple greeting/meta) or delegate (complex task)
3. **Delegate** complex tasks to specialized team members using `reef_submit_task`
4. **Aggregate** results from team members and present to the user

## Hard Rules

- You MUST NOT directly execute tasks that involve web search, code execution, file operations, or any specialized capability
- You MUST use `reef_submit_task` to delegate complex tasks to team members
- You MAY directly respond to simple greetings, meta-questions about the team, or status queries
- When all team members complete their tasks, you MUST aggregate results into a coherent response

## Available Team Members

{动态注入：当前在线 Client 的 role + skills 列表}

## Decision Framework

For each user message, ask yourself:
1. Is this a simple greeting or meta-question? → Respond directly
2. Does this require specialized capabilities (search, code, analysis)? → Delegate via reef_submit_task
3. Is this a multi-step task? → Break down and delegate multiple sub-tasks
```

### 3.2 Executor 身份 Prompt

```markdown
# Identity

You are a **Task Executor** in a multi-agent system. Your role is to:

1. **Receive** tasks delegated by the coordinator
2. **Execute** tasks using your specialized capabilities
3. **Report** results back clearly and concisely

## Available Capabilities

{动态注入：当前 Client 的 role + skills}
```

### 3.3 PromptStack 集成方案

**推荐方案 B：新增 PromptSlotHermesRole**

```go
// 在 PromptSlotIdentity 之后新增
PromptSlotHermesRole PromptSlot = "hermes_role"

// 新增 PromptSource
PromptSourceHermesRole PromptSourceID = "hermes:role"

// 在 BuildSystemPromptParts 中注入
if hermesMode != HermesFull {
    add(PromptPart{
        ID:      "kernel.hermes_role",
        Layer:   PromptLayerKernel,       // kernel 层，最高优先级
        Slot:    PromptSlotHermesRole,    // 新 slot
        Source:  PromptSource{ID: PromptSourceHermesRole, Name: "hermes:coordinator"},
        Title:   "Hermes role definition",
        Content: hermesRolePrompt,
        Stable:  true,
        Cache:   PromptCacheEphemeral,
    })
}
```

**为什么选 kernel 层？**
- kernel 层是最高优先级，LLM 最重视
- 角色定义是核心身份，应与 identity 同级
- 不覆盖 identity（保留 PicoClaw 基础身份），在其后追加角色约束

## 4. Tool 注册策略

### 4.1 HermesToolPolicy

```go
type HermesToolPolicy int

const (
    HermesPolicyFull         HermesToolPolicy = iota // 注册全部工具
    HermesPolicyCoordinator                          // 仅注册协调者工具
    HermesPolicyExecutor                             // 注册全部工具 + 禁用 reef_submit
)

type HermesMode struct {
    Policy      HermesToolPolicy
    Fallback    bool          // 是否允许降级为 Full
    FallbackTimeout time.Duration // 降级超时
}
```

### 4.2 条件注册（推荐方案 D：双重保障）

```go
func registerSharedTools(al *AgentLoop, cfg *config.Config, ...) {
    hermesMode := al.hermesMode // 从 AgentLoop 获取当前模式

    for _, agentID := range registry.ListAgentIDs() {
        agent, _ := registry.GetAgent(agentID)

        switch hermesMode.Policy {
        case HermesPolicyCoordinator:
            // === 协调者工具集 ===
            agent.Tools.Register(reefSubmitTool)
            agent.Tools.Register(reefQueryTool)
            agent.Tools.Register(reefStatusTool)
            // send_message 始终可用
            agent.Tools.Register(sendMessageTool)
            // 不注册: web_search, exec, read_file, write_file, ...

        case HermesPolicyExecutor:
            // === 执行者工具集 ===
            registerAllTools(agent, cfg, ...)  // 全部工具
            // 但不注册 reef_submit_task（执行者不外部分发）

        default: // HermesPolicyFull
            // === 全能工具集 ===
            registerAllTools(agent, cfg, ...)
        }
    }
}
```

### 4.3 降级时的动态工具注册

```go
// 当降级发生时，动态注册缺失的工具
func (al *AgentLoop) EnableFallbackTools() {
    agent := al.GetRegistry().GetDefaultAgent()
    // 注册 web_search, exec, read_file 等缺失工具
    registerExecutionTools(agent, al.cfg, ...)
}

// 当 Client 上线恢复时，移除降级工具
func (al *AgentLoop) DisableFallbackTools() {
    agent := al.GetRegistry().GetDefaultAgent()
    agent.Tools.Remove("web_search")
    agent.Tools.Remove("exec")
    agent.Tools.Remove("read_file")
    // ...
}
```

## 5. 运行时 Guard 设计

```go
// pkg/agent/hermes_guard.go

type HermesGuard struct {
    mode       HermesToolPolicy
    allowed    map[string]struct{}  // 允许的工具名
    fallback   atomic.Bool         // 是否处于降级状态
}

func NewHermesGuard(mode HermesToolPolicy) *HermesGuard {
    g := &HermesGuard{mode: mode}
    switch mode {
    case HermesPolicyCoordinator:
        g.allowed = map[string]struct{}{
            "reef_submit_task": {},
            "reef_query_task":  {},
            "reef_status":      {},
            "send_message":     {},
        }
    default:
        g.allowed = nil // nil = 全部允许
    }
    return g
}

func (g *HermesGuard) Allow(toolName string) bool {
    if g.allowed == nil || g.fallback.Load() {
        return true // Full 模式或降级状态，全部放行
    }
    _, ok := g.allowed[toolName]
    return ok
}

func (g *HermesGuard) SetFallback(enabled bool) {
    g.fallback.Store(enabled)
}
```

## 6. 降级策略设计（推荐方案 D：混合策略）

```
用户消息到达 Coordinator
       │
       ▼
  意图识别（LLM 判断）
       │
       ├── 简单问候/元问题 → 直接回复 ✅
       │
       └── 复杂任务 → 检查在线 Client
              │
              ├── 有 Client → reef_submit_task ✅
              │
              └── 无 Client → 检查 fallback 配置
                     │
                     ├── fallback=true → 降级为 Full 执行
                     │   ├── 注册缺失工具
                     │   ├── 更新 SystemPrompt
                     │   ├── 执行任务
                     │   ├── 恢复约束
                     │   └── 回复用户（附带降级提示）
                     │
                     └── fallback=false → 返回等待提示
                         └── "当前没有可用的团队成员，任务已入队等待"
```

## 7. 启动模式切换完整链路

```
CLI: picoclaw --server
       │
       ▼
  cmd/picoclaw/internal/gateway/command.go
       │  解析 --server 标志
       │  设置 HermesMode = Coordinator
       ▼
  pkg/gateway/gateway.go → Run()
       │
       ▼
  pkg/agent/agent_init.go → NewAgentLoop()
       │  根据 HermesMode:
       │  1. 设置 SystemPrompt 中的 Hermes 角色
       │  2. 条件注册 Tool 集
       │  3. 初始化 HermesGuard
       ▼
  pkg/agent/agent.go → ProcessMessage()
       │  每次处理消息时:
       │  1. SystemPrompt 已含协调者身份
       │  2. Tool 集已被过滤
       │  3. Guard 已就位
       ▼
  LLM 决策 → 只能用 reef_* 工具 → 分发到 Client
```

## 8. 与 reef-scheduler-v2 的融合点

| 融合点 | 说明 |
|--------|------|
| **GatewayBridge** | 启动时传入 HermesMode，影响 AgentLoop 初始化 |
| **ReefSwarmTool** | 作为 Coordinator 的核心工具，Tool 描述中标注"协调者专用" |
| **ReefQueryTool** | Coordinator 查询任务状态 |
| **SystemPrompt** | PromptStack 新增 HermesRole slot |
| **Tool 注册** | registerSharedTools 根据 HermesMode 条件注册 |
| **降级策略** | 与 Scheduler 的 Client 在线状态联动 |
| **Web UI** | Reef Overview 页面显示当前 Hermes 模式 |

## 9. 研究产出物

| # | 产出物 | 说明 |
|---|--------|------|
| 1 | Hermes 能力模型规格 | 三种角色的完整定义 |
| 2 | PromptStack 扩展设计 | 新增 Slot/Source 的代码变更点 |
| 3 | Tool 策略代码变更点 | registerSharedTools 修改方案 |
| 4 | HermesGuard 实现 | 运行时约束 + 降级切换 |
| 5 | 降级策略决策树 | 完整的降级/恢复流程 |
| 6 | CLI 参数映射 | --server → 行为模式的完整链路 |
| 7 | reef-scheduler-v2 设计更新 | 融合 Hermes 后的 design.md 修订 |
