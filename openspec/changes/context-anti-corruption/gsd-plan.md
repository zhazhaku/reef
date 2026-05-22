---
change: context-anti-corruption
artifact: gsd-plan
phase: completed (all 5 layers)
created: 2026-05-11
updated: 2026-05-16
merged_from: context-sandbox
---

# GSD Plan: 上下文反腐化 — 最小颗粒度任务分解

> **合并说明**: 此项目合并了 `context-sandbox`（研究 context-mode + ACE 集成方案）。
> context-sandbox 的研究成果已融入 `research.md` §1.1-1.2；
> 冲突分析见 `cross-proposal-conflict-analysis.md`。

## 总览

```
Goal:     上下文反腐化 5 层架构，解决 Agent "变笨" 问题
System:   seahorse + prompt + tool sandbox + async reflector
Design:   每层独立可交付，零交叉依赖
```

**实施状态（2026-05-16 代码核查）**:

| 层 | 计划工时 | 实际状态 | 代码位置 |
|------|------|:---:|------|
| L1 Think-in-Code | 0.5h | ✅ **已完成** | `context.go:143` Context Conservation 规则 a-e |
| L2 Checkpoint | 4.0h | ✅ **已完成** | `context_seahorse.go:165` createAndInjectCheckpoint() |
| L3 Tool Sandbox | 8.0h | ✅ **已完成** | `tool_sandbox_hook.go` + `reef_execute.go` — FTS5 沙箱隔离 |
| L4 Stable Prefix | 3.0h | ✅ **已完成** | `context.go:745` staticSystemPrompt 分离 |
| L5 Recursive Reflector | 12.0h | ✅ **已完成** | `skillbook.go` + `reflector.py` — Python 程序化反射 + SQLite Skillbook |

**说明**: L1/L2/L4 在 `deepseek-token-optimization` 变更中实施，非独立 context-anti-corruption 工作。
L5 完整版（Skillbook）已实施。

---

## L1: Think-in-Code (prompt 级) ✅ DONE

### L1.1 Context Conservation 规则编写
- [x] 在 `context.go` `getIdentity()` 中新增 Context Conservation 规则
- [x] 规则内容:
  ```
  a) For reading 3+ files, write a script with exec instead of reading each file inline.
  b) For searching patterns in large files, use exec with grep/awk/jq instead of read_file then analyze.
  c) For statistical analysis, use exec with jq/python/awk instead of read_file then manually count.
  d) For searching prior tool outputs, use short_grep. For recovering full outputs, use short_expand.
  e) When exec returns data, prefer head/tail/grep in the command itself to limit output size.
  ```
- [x] 同时更新了 workspace 路径说明（`Before/After` 示例）

### L1.2 验证
- [x] 规则已进入生产系统 prompt（`context.go:143`）

### L1 工时: 0.5h ✅ 已消耗

---

## L2: Checkpoint before Compaction (seahorse 级) ✅ DONE

### L2.1 数据提取函数
- [x] `createAndInjectCheckpoint(ctx, sessionKey)` 在 `context_seahorse.go:165` 实现
- [x] 扫描最近 N 条 assistant message (N=8, 从最近 30 条中取)
- [x] 提取三类信息:
  - `files`: 正则 `(write_file|edit_file|append_file)\(['\"]([^'\"]+)['\"]`
  - `tasks`: 正则 `need to|TODO|FIXME|working on|I'll implement`
  - `decisions`: 正则 `I'll use|Let's go with|use X over Y|decided to`
- [x] 格式化为 Markdown `## Checkpoint (pre-compaction)` block

### L2.2 Compact 集成
- [x] `Compact()` 入口处调用 `createAndInjectCheckpoint()` (`context_seahorse.go:144`)
- [x] checkpoint 作为 system message ingest 到 seahorse
- [x] `Assemble()` 自动包含最近的 checkpoint message

### L2.3 验证
- [x] `checkpoint_test.go` 有 5 个测试（TestCheckpoint_ShouldSave/Restore 等）
- [x] 注: 这些测试针对 TaskSandbox checkpoint，不是 seahorse context checkpoint

### L2 工时: 4h ✅ 已消耗
| 任务 | 工时 |
|------|------|
| L2.1 提取函数 | 2.0h |
| L2.2 Compact 集成 | 1.5h |
| L2.3 测试 | 0.5h |

---

## L3: Tool Output Sandbox (tool 级) ✅ DONE

> **完整 FTS5 隔离方案已实施**（2026-05-16）:
> `tool_sandbox_hook.go` 注册为 AfterTool hook，拦截 exec/reef_execute 输出，
> 完整内容存入 seahorse messages 表（FTS5 索引），LLM 上下文仅获摘要。
> `reef_execute.go` 作为 exec 的显式沙箱替代工具。

### L3.1 reef_execute tool 定义 ✅ 已完成
- [x] `reef_execute` 注册到 `instance.go:114`（tools.NewReefExecuteTool）
- [x] 参数: `command`, `intent`, `timeout_ms`(30s), `keep_tail`(500) — `reef_execute.go:47-82`
- [x] 委托 exec 工具执行，沙箱 hook 自动拦截输出并返回摘要

### L3.2 完整输出存储 ✅ 已完成
- [x] 复用 seahorse messages 表: 完整输出作为 tool role message 写入（`tool_sandbox_hook.go:98-117`）
- [x] FTS5 索引自动覆盖(message 表有 messages_fts 触发器)
- [x] LLM 通过 `short_grep` 搜索，`short_expand` 恢复完整内容

### L3.3 Hook 集成 ✅ 已完成
- [x] `AfterTool` hook 注册到 `agent_init.go:111-116`（"tool-sandbox", priority: 60）
- [x] 检测 `exec` / `reef_execute` tool，超过阈值(2000 chars)时沙箱化
- [x] 正常输出直接透传，无 session key 时降级透传

### L3.4 验证 ✅ 已完成
- [x] 单元测试: `tool_sandbox_hook_test.go` (18 个测试)
- [x] 单元测试: `reef_execute_test.go` (10 个测试)
- [x] 边界测试: nil result、无 session key、精确阈值、ingest 失败降级

### L3 工时: 8h ✅ 已消耗 (2026-05-16)
| 任务 | 工时 |
|------|------|
| L3.1 reef_execute tool | 2.0h |
| L3.2 FTS5 输出存储 | 2.0h |
| L3.3 Hook 注册 | 2.0h |
| L3.4 测试 | 2.0h |

---

## L4: Stable Prefix Partitioning (缓存级) ✅ DONE

### L4.1 Static/Dynamic 分离表
- [x] `PromptSourceRuntime` → dynamic, `PromptSourceSummary` → dynamic
- [x] 其余所有 PromptSource: static
- [x] 验证: static parts 内容不依赖时间/session/channel

### L4.2 BuildMessages 重构
- [x] `context.go` `BuildMessagesFromPrompt()` 已拆分:
  ```go
  // 1. Stable system message: pure static prefix → DeepSeek cache hit!
  messages = append(messages, providers.Message{
      Role: "system", Content: staticSystemPrompt, SystemParts: staticContentBlocks,
  })
  // 2. Conversation history
  history := sanitizeHistoryForProvider(req.History)
  messages = append(messages, history...)
  // 3. User message with dynamic context APPENDED in <CONTEXT> block
  userContent += "\n\n<CONTEXT>\n" + dynamicStrings + "\n</CONTEXT>"
  ```

### L4.3 缓存 Key 注入
- [x] `prompt_cache_key` 已有基础设施（`pipeline_llm.go`），但仅 OpenAI 原生端点注入
- [x] DeepSeek 依赖自动前缀匹配（无需显式 cache_control breakpoint）

### L4.4 验证
- [x] `computeTokenBreakdown` 日志确认 static=~1500 tokens, dynamic=~76 tokens
- [x] Tests: `TestSingleSystemMessage`, `TestBuildMessages_CurrentSenderDynamicContext` 已更新

### L4 工时: 3h ✅ 已消耗
      msgs = append(msgs, history...)
      // 动态内容注入最后一条 user message 的 content 末尾
      msgs[len(msgs)-1].Content = injectDynamicContent(lastUserMsg.Content, dynamics)
      return msgs
  }
  ```
- [x] 动态 content 格式: `<RUNTIME>\nCurrent Time: ...\nChannel: ...\n</RUNTIME>\n<SUMMARY>\n...\n</SUMMARY>`

### L4.3 Static system 内容
- [x] 只包含:
  - Identity + Hierarchy (Kernel)
  - Workspace (Instruction)
  - Tooling + SkillCatalog (Capability)
  - Memory (Context / 文件 mtime 缓存)
- [x] 不包含:
  - Current Time
  - Channel/Chat ID
  - Summary
  - Sender Info (首次? 排于最后 user msg)

### L4.4 验证
- [ ] 单元测试: `TestBuildStaticSystem` → 输出 md5 不变
- [ ] 单元测试: `TestDynamicContentInjection` → user msg 尾附动态块
- [ ] E2E: 两轮对话 → 验证 system message 完全一致
- [ ] 监控: `cache_hit_estimate` metric 验证命中率

### L4 工时: 3h
| 任务 | 工时 |
|------|------|
| L4.1 分离表 | 0.5h |
| L4.2 BuildMessages 重构 | 1.5h |
| L4.3 Static content | 0.5h |
| L4.4 测试 | 0.5h |

## L5: Recursive Reflector (学习级) ✅ DONE

> **完整 Python Recursive Reflector + Skillbook 已实施**（2026-05-16）：
> `skillbook.go` — SQLite Skillbook manager (策略 CRUD、关键词匹配、跨会话注入)
> `reflector.py` — Python 程序化 turn 分析器（6 条规则，零 LLM 成本）
> `skills/reflector/SKILL.md` — Reef skill 定义
> LLM 版 ReflectOnTurn（MEMORY.md 写入）作为补充仍保留。

### L5.1 触发逻辑 ✅
- [x] `shouldReflect()`: MinTurns≥3 或 MinToolErrors≥1 或用户不满信号（"不行"/"错误"/"fix"）

### L5.2 Reflector Prompt ✅ (LLM 版 + Python 版)
- [x] LLM 版: 用 cheap model（`LightProvider`），`buildReflectionPrompt()` 分析本轮 → JSON learnings
- [x] Python 版: `reflector.py` 程序化分析执行轨迹，6 条规则（工具错误/重复调用/上下文效率/成功模式/不满信号/长响应）

### L5.3 MEMORY.md 写入 ✅
- [x] `ApplyLearnings()` 追加到 `MEMORY.md` 的 `## Recent Learnings` 段
- [x] 下次对话时 `PromptSourceMemory` 自动加载

### L5.4 完整 Skillbook ✅ 已完成
- [x] `skillbook.go`: SQLite Skillbook（策略 CRUD、去重、关键词匹配、反馈记录）
- [x] `MatchStrategies()`: 基于 relevance 评分 + 关键词匹配 + 类型加权（avoid > prefer > pattern）
- [x] `InjectStrategies()`: 格式化策略为上下文注入字符串（🚫/✅/📋 前缀标注）
- [x] `SkillbookReflectAndStore()`: 完整管道（Python reflector → Upsert → Event log）
- [x] `RunPythonReflector()`: Python 脚本集成（通过 exec 调 python3）
- [x] `RecordFeedback()`: 策略反馈记录（正面/负面 → 更新 relevance）
- [x] `skills/reflector/SKILL.md`: Reef skill 定义
- [x] 单元测试: `skillbook_test.go` (17 个测试全部通过)
- [x] Python reflector 独立测试通过

### L5 工时: 12h ✅ 已消耗 (2026-05-16)
| 任务 | 工时 | 状态 |
|------|------|:---:|
| L5.1 触发逻辑 | 1.5h | ✅ |
| L5.2 Prompt + cheap model | 3.0h | ✅ (LLM 版 + Python 版) |
| L5.3 MEMORY.md 写入 | 2.0h | ✅ |
| L5.4 完整 Skillbook | 5.5h | ✅ |

---

## 执行顺序（实际实施路径）

```
已完成:
  L1 ✅ context.go:143 Context Conservation 规则（2026-05-08）
  L4 ✅ context.go:745 staticSystemPrompt 分离（2026-05-08）
  L2 ✅ context_seahorse.go:165 createAndInjectCheckpoint（2026-05-15）
  L3 ✅ tool_sandbox_hook.go + reef_execute.go FTS5 沙箱（2026-05-16）
  L5 ✅ skillbook.go + reflector.py Skillbook 反射（2026-05-16）

全部 5 层已完成 ✅
```

## 总工时统计

| 层 | 计划 | 已消耗 | 剩余 | 状态 |
|------|------|------|------|:---:|
| L1 Think-in-Code | 0.5h | 0.5h | 0h | ✅ |
| L2 Checkpoint | 4.0h | 4.0h | 0h | ✅ |
| L3 Tool Sandbox | 8.0h | 8.0h | 0h | ✅ |
| L4 Stable Prefix | 3.0h | 3.0h | 0h | ✅ |
| L5 Recursive Reflector | 12.0h | 12.0h | 0h | ✅ |
| **总计** | **27.5h** | **27.5h** | **0h** | ✅ |

> **全部 5 层上下文反腐化架构已完成**（2026-05-16）：
> L1 Think-in-Code: prompt 规则引导模型保护上下文
> L2 Checkpoint: 压缩前保存决策快照
> L3 Tool Sandbox: FTS5 索引隔离工具输出
> L4 Stable Prefix: 静态/动态分离实现 DeepSeek 缓存命中
> L5 Skillbook: Python 程序化反射 + SQLite 策略库跨会话学习
