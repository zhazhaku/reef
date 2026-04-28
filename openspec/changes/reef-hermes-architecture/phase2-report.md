# Phase 2 Research Report: 对比分析与 Hermes 能力模型定义

## 2.1 OpenAI Swarm / Agents SDK 角色约束机制

### Swarm 核心机制

```
Agent = { name, instructions, functions, tool_choice }
Handoff = 函数返回另一个 Agent 对象 → 自动切换
```

**约束方式：**
- **每个 Agent 独立定义 functions 列表** → 物理隔离，Agent 只能用自己注册的函数
- **Handoff 是显式的** → Agent 必须返回另一个 Agent 对象才能切换，不会隐式越界
- **没有全局工具池** → 每个 Agent 的工具集完全独立

**关键设计：**
- Agent 之间通过 `transfer_to_agent_x()` 函数实现 handoff
- handoff 函数是 Agent 的 `functions` 之一，LLM 选择调用即触发切换
- **约束是结构性的**：Agent A 没有 Agent B 的工具，自然不会越界

### Agents SDK Guardrails

```
input_guardrails  → 输入拦截（Agent 执行前）
output_guardrails → 输出拦截（Agent 执行后）
```

- Guardrail 可以返回 `tripwire_triggered=True` 阻止执行
- 但 Guardrails 是**输入/输出级**的，不是**工具级**的

## 2.2 CrewAI 角色约束机制

### 核心机制

```
Agent = { role, goal, backstory, tools, allow_delegation }
```

**约束方式：**
- **每个 Agent 独立定义 tools 列表** → 物理隔离
- **allow_delegation 参数** → 控制是否可以向其他 Agent 委托任务
  - `allow_delegation=False` → 只能自己执行，不能委托
  - `allow_delegation=True` → 可以委托给其他 Agent
- **Process 模式** → sequential / hierarchical / parallel
  - hierarchical 模式下，manager Agent 负责分配任务

**关键设计：**
- `allow_delegation` 是**声明式约束**，在 Agent 定义时确定
- Manager Agent（hierarchical 模式）= 协调者，只有分配权
- Worker Agent = 执行者，只有执行权
- **工具隔离是结构性的**：每个 Agent 只能用自己注册的工具

## 2.3 多框架对比

| 维度 | OpenAI Swarm/Agents | CrewAI | PicoClaw (当前) | PicoClaw + Hermes (目标) |
|------|-------------------|--------|-----------------|-------------------------|
| Agent 工具隔离 | ✅ 每个 Agent 独立 tools | ✅ 每个 Agent 独立 tools | ❌ 全局共享 | ✅ 按 HermesMode 隔离 |
| 协调者/执行者区分 | Handoff 切换 Agent | allow_delegation + hierarchical | 无区分 | HermesMode 三角色 |
| 工具级约束 | 结构性（不注册=不可用） | 结构性 | 无 | 结构性 + Prompt + Guard |
| 运行时 Guard | input/output guardrails | 无 | Hook 系统 | HermesGuard |
| 降级策略 | 无（必须 handoff） | 无（必须 delegation） | 无 | 混合策略 |
| 启动模式切换 | 无 | 无 | 无 | --server 参数 |

## 2.4 可借鉴的设计模式

### 模式 1: 结构性工具隔离（Swarm/CrewAI 共有）

**核心思想**：不注册 = 不可用，LLM 看不到不该有的工具

**适用于 PicoClaw**：Coordinator 模式下只注册 reef_* + send_message

### 模式 2: 声明式角色约束（CrewAI allow_delegation）

**核心思想**：在 Agent 定义时声明是否允许委托

**适用于 PicoClaw**：HermesMode 作为 AgentLoop 的配置，启动时确定

### 模式 3: Guardrails 拦截（Agents SDK）

**核心思想**：输入/输出级拦截，运行时检查

**适用于 PicoClaw**：BeforeTool Hook 实现 HermesGuard，降级时动态放行

### 模式 4: Handoff 显式切换（Swarm）

**核心思想**：Agent 之间通过显式函数切换

**适用于 PicoClaw**：reef_submit_task 就是 Coordinator 的 "handoff" 函数

---

## 2.5 Hermes 能力模型规格（最终定义）

### 三种角色

```
┌──────────────────────────────────────────────────────────────────────────┐
│ HermesMode = "full" | "coordinator" | "executor"                        │
│                                                                          │
│ ┌──────────────────────────────────────────────────────────────────────┐ │
│ │ full (全能模式)                                                      │ │
│ │ 触发: picoclaw / picoclaw gateway (默认)                             │ │
│ │ SystemPrompt: "You are PicoClaw, a helpful AI assistant"             │ │
│ │ Tools: 全部工具 (web_search, exec, read_file, spawn, ...)            │ │
│ │ SubTurn: ✅ 可用                                                     │ │
│ │ ReefSubmit: ❌ 不需要                                                │ │
│ │ 降级: 不适用                                                         │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│                                                                          │
│ ┌──────────────────────────────────────────────────────────────────────┐ │
│ │ coordinator (协调者模式)                                              │ │
│ │ 触发: picoclaw server                                                │ │
│ │ SystemPrompt: "You are a Team Coordinator..."                        │ │
│ │ Tools: reef_submit_task, reef_query_task, reef_status,               │ │
│ │        message, reaction, cron                                       │ │
│ │ SubTurn: ❌ 禁用                                                     │ │
│ │ ReefSubmit: ✅ 核心工具                                              │ │
│ │ 降级: 无 Client 时 → 临时 full (fallback=true)                       │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
│                                                                          │
│ ┌──────────────────────────────────────────────────────────────────────┐ │
│ │ executor (执行者模式)                                                 │ │
│ │ 触发: Client 连接到 Server (SwarmChannel)                            │ │
│ │ SystemPrompt: "You are a Task Executor..."                           │ │
│ │ Tools: 全部工具 (web_search, exec, read_file, spawn, ...)            │ │
│ │ SubTurn: ✅ 可用                                                     │ │
│ │ ReefSubmit: ❌ 禁用 (执行者不外部分发)                                │ │
│ │ 降级: 无 Server 时 → 临时 full                                       │ │
│ └──────────────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────────────┘
```

### Coordinator 详细 Tool 集

| Tool | 允许 | 理由 |
|------|------|------|
| reef_submit_task | ✅ | 核心工具：分发任务到 Client |
| reef_query_task | ✅ | 查询任务状态 |
| reef_status | ✅ | 查询 Server/Client 状态 |
| message | ✅ | 向用户发送消息 |
| reaction | ✅ | 消息反应（轻量交互） |
| cron | ✅ | 定时任务管理（协调者需要） |
| web_search | ❌ | 应分发到 Client 执行 |
| web_fetch | ❌ | 应分发到 Client 执行 |
| exec | ❌ | 应分发到 Client 执行 |
| read_file | ❌ | 应分发到 Client 执行 |
| write_file | ❌ | 应分发到 Client 执行 |
| append_file | ❌ | 应分发到 Client 执行 |
| send_file | ❌ | 应分发到 Client 执行 |
| send_tts | ❌ | 应分发到 Client 执行 |
| load_image | ❌ | 应分发到 Client 执行 |
| find_skills | ❌ | 协调者不需要搜索技能 |
| install_skill | ❌ | 协调者不需要安装技能 |
| spawn/subagent | ❌ | 协调者使用 reef_submit 替代 |
| spawn_status | ❌ | 协调者使用 reef_query 替代 |

### 约束规则

1. **结构性约束（硬约束）**：Coordinator 只注册允许的工具 → LLM 看不到禁止的工具
2. **Prompt 约束（软约束）**：SystemPrompt 明确角色定义 → 引导 LLM 正确决策
3. **Guard 约束（动态约束）**：降级时动态放行 → 恢复时重新约束

### 降级规则

```
coordinator + 有 Client 在线 → 正常协调模式
coordinator + 无 Client + fallback=true → 降级为 full
coordinator + 无 Client + fallback=false → 返回等待提示
coordinator + Client 上线 → 恢复协调模式

executor + 有 Server 连接 → 正常执行模式
executor + 无 Server 连接 → 降级为 full（独立工作）
```
