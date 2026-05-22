# 源代码详细分析报告: context-anti-corruption 未实施功能

> 日期: 2026-05-16
> 范围: `/root/reef_server/.reef/workspace/picoclaw/pkg/agent/`
> 方法: 逐文件逐函数对比 GSD plan 定义的完整功能

---

## 一、已完成 (L1, L2, L4) — 无需再动

### L1: Think-in-Code ✅

**代码位置**: `context.go:143-160`

```go
// Context Conservation 规则 a-e 全部在 system prompt 中
```

**验证**: 规则已进入生产 prompt，引导 LLM 用 exec+脚本替代 read_file 循环。

### L2: Checkpoint before Compaction ✅

**代码位置**: `context_seahorse.go:141-260`

- `createAndInjectCheckpoint()` — 扫描 assistant messages，提取 files/tasks/decisions
- `extractFilesFromContent()`, `extractTasksFromContent()`, `extractDecisionsFromContent()` — 三个正则提取器
- Compact 入口集成 (`context_seahorse.go:141`)
- 测试: `checkpoint_test.go` 5 个测试

### L4: Stable Prefix Partitioning ✅

**代码位置**: `context.go:740-810`

- `staticSystemPrompt` — 独立不变的 system message
- dynamic context (time/channel/sender/summary) → `<CONTEXT>` 标签注入 user msg 尾部
- `BuildMessagesFromPrompt()` 拆分完成
- 测试: 已更新

---

## 二、L3: Tool Output Sandbox — 详细缺口分析

### 当前实现 (简化版)

#### 2.1 `tool_truncate.go` (102 行) — 纯截断方案

| 组件 | 代码 | 行数 |
|------|------|------|
| `toolTruncateLimits` | 按工具名 → 字符上限 map | 14-18 |
| `fullPassTools` | 14 个工具白名单(不截断) | 23-36 |
| `truncateToolResult()` | 按 limit 截断 + 标记 | 52-74 |
| `truncateToolResultMessage()` | 对 providers.Message 应用截断 | 79-85 |
| `resolveToolName()` | tool_call_id → 工具名映射 | 89-100 |

**调用点**:
- `context.go:944` — `sanitizeHistoryForProvider()` 第二轮扫描时截断 tool result
- `pipeline_execute.go:220,606` — 工具执行后截断

**限制**:
- 截断是**破坏性**的：超出 limit 的内容直接被丢弃（在 API request 层面）
- 没有索引机制：LLM 无法搜索被截断的内容
- 恢复依赖 `short_expand` tool — 但 short_expand 只能恢复 seahorse 中的 messages，不能恢复被截断的 tool output

#### 2.2 `context_sandbox.go` (55 行) — 文件存储方案

| 组件 | 代码 | 行数 |
|------|------|------|
| `MaxToolResultChars` | 默认截断上限 4096 | 12 |
| `sandboxDir()` | 创建 `.sandbox/` 目录 | 15-20 |
| `TruncateToolResult()` | 截断 + SHA256 文件名存储完整输出 | 28-54 |

**限制**:
- `sandboxDir()` 使用相对路径 `./.sandbox`，取决于进程工作目录
- 存储后用 `read_file` 恢复，但**没有搜索索引** — LLM 必须知道文件名
- 文件名基于 content hash — 无法按语义检索

### GSD 完整版定义的缺口

#### ❌ L3.1: `reef_execute` tool 不存在

| GSD 要求 | 当前状态 |
|----------|---------|
| 注册到 `agent_init.go` | **不存在** — 无 `reef_execute` tool 注册 |
| 参数: `command`, `intent`, `timeout_ms`(30s), `keep_tail`(500) | **不存在** |
| 返回值含 summary + `retrieve_with: short_expand({message_id})` | **不存在** |

**代码搜索**: `grep -rn "reef_execute" pkg/` → 零匹配

#### ❌ L3.2: `ctx_executions` 表不存在

| GSD 要求 | 当前状态 |
|----------|---------|
| `ctx_executions` 表 (execution_id, session_key, command, full_output, summary, created_at) | **不存在** |

**代码搜索**: `grep -rn "ctx_executions\|execution_id\|executions" pkg/seahorse/schema.go` → 零匹配

**现有 seahorse 表**: conversations, messages, message_parts, summaries, summary_parents, summary_messages, context_items, task_episodes, conversation_mode, hermes_workflow_sessions — 共 10 张表，无 sandbox execution 表。

**可以复用的**: `messages` 表可存储完整 output 作为独立 message，`messages_fts` FTS5 虚拟表已存在可提供索引。但需要：
- 新增 message kind 标记（区分普通 message vs sandbox output）
- 或使用 `context_items` 表存储

#### ❌ L3.3: PreToolUse Hook 未实施

| GSD 要求 | 当前状态 |
|----------|---------|
| Hook 检测 `exec` tool 的 command 参数 | **不存在** |
| 长输出拦截 → 写 seahorse → 返回摘要 | **不存在** |

**现有基础设施**: `h hooks.go` 有 `BeforeTool` hook 接口，`pipeline_execute.go:93` 调用它。Hook 系统**已就绪**，但**没有注册**用于沙箱拦截的 hook。

`pipeline_execute.go:93`:
```go
toolReq, decision := al.hooks.BeforeTool(turnCtx, &ToolCallHookRequest{...})
```

Hook 可以返回 `HookActionRespond` 跳过实际执行并返回自定义结果 — 这正是 L3.3 需要的拦截能力。

#### ❌ L3.4: 验证缺失

- 无 `reef_execute` 单元测试
- 无 FTS5 搜索沙箱输出的集成测试
- 无多轮上下文不膨胀的 E2E 测试

### L3 总结

| 功能 | 代码位置 | 状态 |
|------|---------|:---:|
| 截断 + 文件存储 | `tool_truncate.go` + `context_sandbox.go` | ✅ 简化版 |
| `reef_execute` tool | 无 | ❌ |
| `ctx_executions` 表 | 无 | ❌ |
| PreToolUse hook 拦截 | hook 系统就绪，无注册 | ❌ |
| FTS5 沙箱输出索引 | 无 | ❌ |
| 测试 | 无 | ❌ |

**可复用基础设施**:
- `messages_fts` FTS5 虚拟表 → 可直接建索引
- `BeforeTool` hook → 已就绪，只差注册
- `seahorse.Engine` → 可写入完整输出作为独立 message

---

## 三、L5: Recursive Reflector — 详细缺口分析

### 当前实现 (简化版)

#### 3.1 `reflector.go` (319 行) — LLM 反射方案

| 组件 | 函数 | 行数 |
|------|------|------|
| 触发 | `shouldReflect()` — MinTurns≥3 or MinToolErrors≥1 | 130-146 |
| Prompt | `buildReflectionPrompt()` + `buildHistorySummary()` | 68-128 |
| 反射 | `runReflection()` — cheap model → JSON learnings | 156-189 |
| 写入 | `ApplyLearnings()` → `MEMORY.md` | 195-245 |
| 入口 | `ReflectOnTurn()` — goroutine 调用 | 260-320 |

**调用点**: `turn_coord.go:46`
```go
go ReflectOnTurn(al, provider, model, sessionKey, ...)
```

**工作流程**:
1. turn_end → 异步 goroutine
2. 判断 `shouldReflect()` — 错误/多轮/用户不满
3. cheap model (LightProvider) 分析对话
4. 提取 learnings (mistake/success/observation)
5. 写入 `MEMORY.md` 的 `## Recent Learnings` 段
6. 下次对话时 `PromptSourceMemory` 自动注入

### GSD 完整版定义的缺口

#### ❌ L5.4: 完整 Skillbook 不存在

| GSD 要求 | 当前状态 |
|----------|---------|
| Python 递归反射器 | **不存在** — 代码搜索 `find . -name "*reflector*.py" -o -name "*skillbook*.py"` → 零匹配 |
| Skillbook 持久化 | **不存在** — 无 `Skillbook` 类型，无 `SkillManager` 类型 |
| 跨会话策略注入 | **不存在** — 只有 MEMORY.md 追加 learnings，无结构化策略库 |
| Agent↔Reflector↔SkillManager 三角色 | **不存在** — 只有 Agent + LLM 反射 |

**LLM 反射 vs Python 递归反射的区别**:

| 维度 | LLM 反射 (简化版) | Python 递归反射 (完整版) |
|------|-------------------|------------------------|
| 执行环境 | LLM API call | 沙箱 Python 子进程 |
| 分析深度 | 语义级 (cheap model) | 程序级 (trace + pattern match) |
| 输出 | JSON learnings → MEMORY.md | 结构化 Skill 对象 → Skillbook |
| 跨会话 | learnings 自然语言注入 prompt | Skill 对象按需注入 |
| 学习 | 被动追加 | 主动更新/合并/淘汰 |
| 策略复用 | 无 (每次重新注入) | Skillbook 持久化，跨会话复用 |

### L5 总结

| 功能 | 代码位置 | 状态 |
|------|---------|:---:|
| 触发逻辑 | `reflector.go:shouldReflect()` | ✅ |
| LLM 反射 | `reflector.go:runReflection()` | ✅ |
| MEMORY.md 写入 | `reflector.go:ApplyLearnings()` | ✅ |
| Python 递归反射器 | 无 | ❌ |
| Skillbook 持久化 | 无 | ❌ |
| 跨会话策略注入 | 无 | ❌ |
| SkillManager | 无 | ❌ |

---

## 四、配套基础设施现状

### 4.1 有但未用于 L3/L5 的

| 基础设施 | 位置 | 可用于 |
|----------|------|--------|
| `BeforeTool` hook | `hooks.go:450` | L3.3 PreToolUse 拦截 |
| `messages_fts` FTS5 | `schema.go:17` | L3.2 沙箱输出索引 |
| `seahorse.Engine.Ingest()` | `short_engine.go` | L3.2 完整输出存储 |
| `context_items` 表 | `schema.go:97` | L3.2 替代 ctx_executions |
| `short_grep` tool | `tool_grep.go` | L3.4 搜索沙箱输出 |
| `exec` tool | `instance.go:105` | L3.1 reef_execute 基础 |

### 4.2 缺失的

| 基础设施 | GSD 要求 | 替代方案 |
|----------|---------|----------|
| `ctx_executions` 表 | L3.2 新表 | 复用 `messages` 或 `context_items` 表 |
| `reef_execute` tool 定义 | L3.1 新 tool | 复用现有 `exec` tool + hook 拦截 |
| Python skillbook | L5.4 Python 程序 | 可用 Go 实现替代（更简洁） |

---

## 五、实施建议

### 5.1 L3 完整版 (8h)

```
阶段 1 (3h): reef_execute tool + hook 集成
  - 注册 reef_execute tool (复用 exec 实现，加 summary 逻辑)
  - 注册 BeforeTool hook 拦截 exec 大输出

阶段 2 (3h): FTS5 索引
  - 建 ctx_executions 表 (或复用 messages + kind 标记)
  - 写入完整输出 → messages_fts 建索引
  - short_grep 可搜索

阶段 3 (2h): 测试 + E2E
  - 单元测试: 大输出摘要生成
  - 集成测试: FTS5 搜索
  - E2E: 多轮不膨胀
```

### 5.2 L5 完整版 (5.5h)

```
阶段 1 (3h): Python 递归反射器
  - 写 scripts/reflector.py
  - 子进程隔离执行
  - 程序级 trace 分析 → Skill 对象

阶段 2 (2.5h): Skillbook 持久化
  - Skillbook 类型: 结构化策略存储
  - SkillManager: 跨会话注入/合并/淘汰
  - 与现有 MEMORY.md learnings 共存
```

### 5.3 风险/注意事项

1. **L3 完整版是否必要**: 当前截断方案已覆盖 80% 收益。完整版的核心价值是"搜索被截断内容"和"摘要而非截断"，对重度用 exec 的 agent 更有价值。

2. **L5 完整版投入产出**: Python 递归反射器需要维护 Python 运行时，Go 原生实现可能更好。LLM 反射已覆盖基础学习，Skillbook 适合长期运行的 agent。

3. **context-anti-corruption 与 deepseek-token-optimization 的分离**: L3/L5 完整版是反腐化工作，不是 token 优化。如果 deepseek-token-optimization 已关闭，这些代码应作为独立工作跟踪。

---

*分析完成时间: 2026-05-16 07:45*
*覆盖: 14 个源文件，共 3513 行代码*
*GSD plan: 50 个 checkbox，29✅ + 21❌*
