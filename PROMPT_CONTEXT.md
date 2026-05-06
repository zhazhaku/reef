# Reef Agent 提交上下文 (Prompt Context) 完整构成

> 本文档解释每次 LLM 调用时，发送给 AI 模型的完整上下文由哪几部分组成，每部分的来源、文件路径和作用。

---

## 总体架构

```
┌─────────────────────────────────────────────────────┐
│                  最终 API 调用                       │
│  messages = [system_prompt, ...history, user_msg]   │
└─────────────────────────────────────────────────────┘
                          ▲
                          │ BuildMessagesFromPrompt()
                          │
    ┌─────────────────────┼─────────────────────┐
    │                     │                     │
    ▼                     ▼                     ▼
┌────────┐          ┌──────────┐          ┌──────────┐
│ SYSTEM │          │ HISTORY  │          │  USER    │
│ PROMPT │          │ messages │          │ MESSAGE  │
│ (静态)  │          │ (摘要)    │          │ (当前)    │
└────────┘          └──────────┘          └──────────┘
    │                     │                     │
    │ BuildSystemPrompt   │ ContextManager      │ pipeline_setup.go
    │ WithCache()         │ .Assemble()         │
    │                     │                     │
    ▼                     ▼                     ▼
 context.go          context_cnp.go        turn_state.go
 (4 层 PromptPart)   (CNP 协议历史)         (用户输入)
```

---

## 第一部分：System Prompt（系统提示词）

**组装文件**: `picoclaw/pkg/agent/context.go`
**核心函数**: `BuildSystemPromptParts()` (L164) → `BuildSystemPromptWithCache()` (L263) → `BuildMessagesFromPrompt()` (L666)

系统提示词由 **4 个 PromptLayer** 组成，按优先级排序渲染，层间用 `\n\n---\n\n` 分隔：

### L0: KERNEL（内核层，优先级 100）

| 插槽 (Slot) | 来源 | 文件/路径 | 说明 |
|:---|------|------|------|
| `identity` | `getIdentity()` | `context.go:114` | Agent 身份、Workspace 路径、4 条 Hard Rules |
| `hierarchy` | Prompt Registry builtin | `prompt.go` → `builtinPromptSources()` | 提示词层级规则（未启用时为空） |
| `identity` (hermes) | Hermes role | `hermes_prompt.go` | 多 Agent 协调角色定义（可选） |

**identity 模板** (`context.go:114-145`):
```go
`# reef 🪸 (%s)

You are reef, a helpful AI assistant.

## Workspace
Your workspace is at: %s
- Memory: %s/memory/MEMORY.md
- Daily Notes: %s/memory/YYYYMM/YYYYMMDD.md
- Skills: %s/skills/{skill-name}/SKILL.md

## Important Rules
1. ALWAYS use tools...
2. Be helpful and accurate...
3. Memory...
4. Context summaries...`
```

- `%s` 参数：`config.FormatVersion()` → `dev` / `v1.0.0` 等
- `%s` 参数：`workspacePath` → `/root/reef_server/.reef/workspace`

### L1: INSTRUCTION（指令层，优先级 80）

| 插槽 (Slot) | 来源 | 文件/路径 | 说明 |
|:---|------|------|------|
| `workspace` | `LoadBootstrapFiles()` | `context.go:571` | 读取 workspace 下的引导文件 |

**文件读取顺序** (`LoadBootstrapFiles()`):

```
1. AGENT.md          → AgentDefinition.Agent.Body
2. SOUL.md           → AgentDefinition.Soul.Content
3. USER.md           → AgentDefinition.User.Content
4. IDENTITY.md       → 直接 os.ReadFile (仅当 Source != AgentDefinitionSourceAgent)
```

这些文件通过 `LoadAgentDefinition()` 从配置中加载，可以是 workspace 下的本地文件，也可以是远程 URL。

### L2: CAPABILITY（能力层，优先级 60）

| 插槽 (Slot) | 来源 | 文件/路径 | 说明 |
|:---|------|------|------|
| `skill_catalog` | `skillsLoader.BuildSkillsSummary()` | `pkg/skills/` | 已安装的 Skill 目录摘要 |
| `active_skill` | Active Skills for current turn | `pkg/skills/` 中的 SKILL.md | 当前激活的 Skill 完整内容 |
| `tooling` | Tool Registry | `pkg/tools/` → Native Provider | 工具定义（native + MCP） |
| `tooling` | `toolDiscoveryPromptContributor` | `prompt_contributors.go:12` | 工具发现规则（bm25/regex） |
| `mcp` | `mcpServerPromptContributor` | `prompt_contributors.go:43` | MCP 服务器能力描述 |

**Tool Discovery 规则模板** (`prompt_contributors.go:12-40`):
```
5. **Tool Discovery** - Your visible tools are limited to save memory, 
   but a vast hidden library exists. If you lack the right tool for a task, 
   BEFORE giving up, you MUST search using the tool_search_tool_bm25 tool.
```

### L3: CONTEXT（上下文层，优先级 40）

| 插槽 (Slot) | 来源 | 文件/路径 | 说明 |
|:---|------|------|------|
| `memory` | `memory.GetMemoryContext()` | `pkg/memory/` → `MEMORY.md` | Workspace 记忆文件内容 |
| `output` | Split-on-marker policy | `context.go:240` | 多消息输出策略（可选） |
| `runtime` | `buildDynamicContext()` | `context.go:628` | **每次请求动态生成** |
| `summary` | `req.Summary` | `ContextManager.Assemble()` | 对话摘要（约简压缩后的历史） |

**Runtime Context 动态内容** (`context.go:628-652`):
```go
## Current Time
2026-05-05 06:44 (Tuesday)        ← time.Now()

## Runtime
android arm64, Go go1.26.2        ← runtime.GOOS, GOARCH, runtime.Version()

## Current Session
Channel: feishu                    ← 当前频道
Chat ID: oc_53e971dc...           ← 当前会话ID

## Current Sender
Current sender: ou_8fba50c...     ← 当前发送者ID
```

---

## 第二部分：Conversation History（对话历史）

**来源**: `pkg/agent/pipeline_setup.go:28-37` → `ContextManager.Assemble()` → `buildMessagesFromLayers()`

历史消息由 **ContextManager**（CNP 协议实现）管理：

```
ContextManager.Assemble()
  ↓
buildMessagesFromLayers(layers *ContextLayers)
  ↓
L0 Immutable → system 消息 (静态提示词)
L1 Task      → system 消息 (任务指令)
L2 Working   → user/assistant 消息 (对话轮次)
```

**Working Rounds** 来自 `short_grep` + `short_expand` 的检索结果：
- `short_grep` 搜索对话历史和摘要
- `short_expand` 展开完整消息内容
- 每轮对话作为 `WorkingRound` 追加到 `ContextLayers.Working`

当超过 Token 预算时，`ContextWindow.Compact()` 会丢弃最早的轮次，只保留最近 5 轮。

---

## 第三部分：Current User Message（当前用户消息）

**来源**: `pkg/agent/pipeline_setup.go` → `ts.userMessage` + `ts.media`

```
messages = append(messages, providers.Message{
    Role:    "user",
    Content: currentMessage,
    Media:   mediaURIs,
})
```

- `ts.userMessage` — 来自 Channel Adapter 解析的用户消息文本
- `ts.media` — 多媒体附件 URI 列表（图片等）

---

## 最终组装流程图

```
picoclaw/pkg/agent/
│
├── context.go ─────────────────────────────────────
│   BuildSystemPromptParts()   ← 组装 L0~L3 所有 PromptPart
│   BuildSystemPrompt()        ← renderPromptPartsLegacy() 渲染为字符串
│   BuildSystemPromptWithCache() ← 带缓存的系统提示词
│   BuildMessagesFromPrompt()  ← 组合为最终 []providers.Message
│   getIdentity()              ← 身份模板 (hardcoded)
│   LoadBootstrapFiles()       ← AGENT.md/SOUL.md/USER.md/IDENTITY.md
│   buildDynamicContext()      ← 时间/OS/Channel/Sender (每次动态)
│
├── context_layers.go ─────────────────────────────
│   ContextLayers{Immutable, Task, Working, Memory} ← 四层结构
│   SetImmutable()             ← L0: 提示词+角色+技能+基因
│   SetTask()                  ← L1: 任务指令+元数据
│   AppendRound()              ← L2: 追加对话轮次
│   InjectMemory()             ← 注入记忆
│
├── context_window.go ─────────────────────────────
│   ContextWindow              ← Token 预算管理
│   Compact()                  ← 超过阈值时压缩历史
│
├── context_cnp.go ────────────────────────────────
│   CNPContextManager          ← CNP 协议实现的上下文管理器
│   Assemble()                 ← 从 layers 构建 messages
│   Compact()                  ← 压缩
│   Ingest()                   ← 记录新消息
│
├── prompt.go ─────────────────────────────────────
│   PromptLayer/PromptSlot     ← 层级/插槽枚举
│   PromptRegistry             ← 注册表：校验 + 收集 PromptPart
│   renderPromptPartsLegacy()  ← 渲染：按优先级排序，---分隔
│   builtinPromptSources()     ← 17 个内置 PromptSource 定义
│
├── prompt_contributors.go ────────────────────────
│   toolDiscoveryPromptContributor   ← 工具发现提示
│   mcpServerPromptContributor       ← MCP 服务器提示
│
├── pipeline_setup.go ─────────────────────────────
│   SetupTurn()                ← 组装完整消息数组
│   ContextManager.Assemble()  ← 获取历史+摘要
│   BuildMessagesFromPrompt()  ← 构建系统提示词+历史+用户消息
│
└── pipeline_llm.go ──────────────────────────────
    ExecuteLLMCall()           ← 实际发起 LLM API 调用
    messages[0] = system_prompt
    messages[1..n-1] = history
    messages[n] = user_message
```

---

## 完整 Prompt 结构示例

```
┌──────────────────────────────────────────────────┐
│ # reef 🪸 (dev)                                  │  ← L0: getIdentity()
│ You are reef, a helpful AI assistant.            │
│                                                  │
│ ## Workspace                                     │
│ Your workspace is at: /root/reef_server/...      │
│ ...                                              │
│ ## Important Rules                               │
│ 1. ALWAYS use tools...                           │
│ 2. Be helpful and accurate...                    │
│ 3. Memory...                                     │
│ 4. Context summaries...                          │
│                                                  │
│ ---                                              │  ← 分隔符
│                                                  │
│ ## AGENT.md                                      │  ← L1: LoadBootstrapFiles()
│ # Agent Developer Guide                          │
│ ...                                              │
│ ## USER.md                                       │
│ # User Guide                                     │
│ ...                                              │
│                                                  │
│ ---                                              │
│                                                  │
│ # Skills                                         │  ← L2: Skills + Tools
│ The following skills extend your capabilities... │
│ ...                                              │
│                                                  │
│ ---                                              │
│                                                  │
│ # Memory                                         │  ← L3: Memory
│ (workspace memory content)                       │
│                                                  │
│ ---                                              │
│                                                  │
│ ## Current Time                                  │  ← L3: Runtime (动态)
│ 2026-05-05 06:44 (Tuesday)                       │
│ ## Runtime                                       │
│ android arm64, Go go1.26.2                       │
│ ## Current Session                               │
│ Channel: feishu                                  │
│ ## Current Sender                                │
│ Current sender: ou_8fba50c...                    │
│                                                  │
│ ---                                              │
│                                                  │
│ CONTEXT_SUMMARY: The following is an...          │  ← L3: Summary
│ Conversation summaries provided as context...    │
│ [压缩后的历史摘要]                                 │
└──────────────────────────────────────────────────┘
                        +
┌──────────────────────────────────────────────────┐
│ [user] 之前的消息...                               │  ← History
│ [assistant] 之前的回复...                          │  (from ContextManager)
│ ...                                              │
└──────────────────────────────────────────────────┘
                        +
┌──────────────────────────────────────────────────┐
│ [user] 当前用户消息                                │  ← Current Message
└──────────────────────────────────────────────────┘
```

---

## 关键文件索引

| 文件 | 行数 | 职责 |
|------|:---:|------|
| `pkg/agent/context.go` | 1107 | 系统提示词构建、缓存、消息组装 |
| `pkg/agent/context_layers.go` | 244 | 四层上下文数据模型 |
| `pkg/agent/context_window.go` | 89 | Token 预算 + 压缩 |
| `pkg/agent/context_cnp.go` | 150+ | CNP 协议上下文管理器 |
| `pkg/agent/prompt.go` | 504 | PromptPart/PromptLayer/PromptSlot 定义、注册表 |
| `pkg/agent/prompt_contributors.go` | 139 | 工具发现 + MCP 贡献者 |
| `pkg/agent/prompt_turn.go` | 129 | 每轮 Prompt 构建 |
| `pkg/agent/pipeline_setup.go` | 100+ | 完整消息数组组装入口 |
| `pkg/agent/context_manager.go` | 80+ | ContextManager 接口定义 |
| `pkg/agent/hermes.go` | — | Hermes 多 Agent 角色提示词 |
| `pkg/memory/` | — | 记忆系统（MEMORY.md 读取） |
| `pkg/skills/` | — | 技能加载器（SKILL.md 读取） |
| `{workspace}/AGENT.md` | — | Agent 开发者指南 |
| `{workspace}/USER.md` | — | 用户指南 |
| `{workspace}/memory/MEMORY.md` | — | 记忆文件 |
