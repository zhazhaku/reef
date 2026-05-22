# Tasks: Token 削减 & DeepSeek 缓存命中率联合优化

> 核心约束：**不丢记忆** — seahorse SQLite + JSONL 不截断，仅优化 API 请求 payload。

---

## Phase 1: 测量基线 (1 day)

### T1.1 — Token Probe 探针 [SMALL] ⊕
**文件**: `pkg/agent/pipeline_llm.go`, `pkg/agent/context_usage.go`

在 `CallLLM` 中增加 token 分类日志：
- 计算并打印 `staticSystemTokens`（提取 BuildSystemPromptWithCache 的 token 数）
- 计算并打印 `dynamicTokens`（summary + time + channel 等）
- 计算并打印 `historyTokens`, `toolDefTokens`, `reasoningTokens`
- 记录 DeepSeek API 返回的 `prompt_tokens` / `completion_tokens`
- 在 `computeContextUsage` 中暴露分类数据到 bus event

**产出**: 每次 LLM 调用的 token 分布日志

### T1.2 — System Prompt 成分分析 [MEDIUM] ⊕
**文件**: 新 `test/` 或内嵌分析代码

- 导出一次完整 `BuildMessages` 输出的 system prompt 文本
- 逐段分析每个 PromptPart 的实际字符数/token 数
- 标记每个 part 的稳定/动态属性
- 产出 system prompt 剖析报告

**产出**: System prompt 成分占比数据

### T1.3 — 缓存命中率间接测量 [SMALL] ⊕
**文件**: 测试脚本或内嵌

- 连续发送 2 次完全相同的请求（相同 system + history + user msg）
- 对比两次的 `prompt_tokens` 计费是否有折扣
- 验证 DeepSeek 前缀缓存的行为特征
- 确定 `cached_tokens` 是否在 usage 中返回

**产出**: 缓存命中率估算基线

---

## Phase 2: 方案实现 (3-4 days)

### T2.1 — Stable Prefix Partitioning [LARGE]
**依赖**: T1.2
**文件**: `pkg/agent/prompt.go`

实现 `buildStaticSystemPrefix()`：
1. 从 `PromptRegistry.Collect()` 中分离出 Stable=true 的 PromptPart
2. 将静态 parts 渲染为独立 system message 内容
3. 修改 `BuildMessages()` / `BuildMessagesFromPrompt()`: 
   - 第一 system msg = 仅静态 parts
   - 最后 user msg = 原来 user msg + `<CONTEXT>`(dynamic parts) 注入
4. 保留 prompt contributors 的注册和收集机制不变
5. 确保 `sanitizeHistoryForProvider` 仍正确处理 reasoning_content

**验证点**:
- `TestSingleSystemMessage` 需更新（允许双 system msg 结构）
- `TestMtimeAutoInvalidation` 通过
- `TestCacheStability` 通过

### T2.2 — Dynamic Context Injection [MEDIUM]
**依赖**: T2.1
**文件**: `pkg/agent/prompt.go`, `pkg/agent/context_seahorse.go`

1. 将 `PromptSourceRuntime`（Current Time, Channel, Sender）从 system msg 移除
2. 将这些内容格式化后注入到最后一条 user message 的 content 尾部
3. 将 `PromptSourceSummary`（CONTEXT_SUMMARY）同样注入 user msg 尾部
4. 格式：`\n\n<CONTEXT>\n## Current Time\n...\n## CONTEXT_SUMMARY\n...\n</CONTEXT>`
5. 备选：验证多 system msg 方案（如果模型接受，将 summary 独立为最后 system msg）

### T2.3 — Tool Output Truncation [MEDIUM]
**依赖**: T1.1
**文件**: 新 `pkg/agent/tool_truncate.go`, `pkg/agent/context_seahorse.go`

1. 创建 `tool_truncate.go`：实现 `truncateToolResult(toolName, content) string`
2. 截断策略按 §P3 定义
3. 在 `seahorseToProviderMessages()` 中，对 `part.Type == "tool_result"` 的消息调用截断
4. 截断标记包含截断信息（原长→新长，恢复方式）
5. 验证 seahorse 存储不受影响

### T2.4 — Reasoning Content 截断 [SMALL]
**依赖**: 无
**文件**: `pkg/providers/openai_compat/provider.go`

1. 在 `filterDeepSeekReasoningTurn` 中新增 reasoning 截断逻辑
2. 阈值：4096 chars
3. 确保 `ReasoningContentPresent` 保持为 true

### T2.5 — Token 统计增强 [SMALL]
**依赖**: T2.1, T2.3
**文件**: `pkg/agent/context_usage.go`

1. `computeContextUsage` 区分 static/dynamic/history/truncated 分类计数
2. 计算预期缓存命中 token 数和比率
3. 暴露到 bus 事件（`ContextUsage`）

---

## Phase 3: 测试与验证 (2 days)

### T3.1 — Token 节省 A/B 测试 [MEDIUM]
**依赖**: T2.1-T2.5

选择 3-5 个典型场景对比优化前后：

| 场景 | 描述 | 测量指标 |
|------|------|----------|
| 短对话 | 3 轮简单问答 | input tokens, cost |
| 长对话 | 10 轮多工具调用 | input tokens, cost, cache hit rate |
| 密集工具 | 单轮 5+ tool calls | tool truncation effect |
| 跨天对话 | 跨越 24h 的对话 | time change impact |
| Memory test | 验证 short_expand 可恢复截断内容 | 100% recovery |

### T3.2 — 功能回归测试 [MEDIUM]
**依赖**: T2.1-T2.5

确保不破坏现有功能：
- `context_cache_test.go` 全部通过（需要适配新结构）
- `context_seahorse_test.go` 通过
- 多轮对话记忆不丢失（seahorse 穿梭验证）
- reasoning_content 正确回传（DeepSeek 400 不再出现）
- 不同 provider（Anthropic/OpenAI/DeepSeek/其他兼容）行为正常
- JSONL 持久化/恢复正常

### T3.3 — Memo Test [SMALL]
**依赖**: T3.2

专门验证「不丢记忆」：
1. 进行一次包含大文本 web_fetch 的对话
2. 后续轮次中通过 `short_expand` 恢复被截断的内容
3. 验证 seahorse 中内容完整
4. 验证 JSONL 中内容完整

### T3.4 — 文档更新 [SMALL]
**依赖**: T3.1

- 记录优化方案、实现细节、效果数据
- 更新架构图

---

## 依赖图

```
Phase 1:                    Phase 2:                    Phase 3:
T1.1 ──────────┐           ┌── T2.1 ── T2.2 ──┐
               ├─ T1.3     ├── T2.3 ────────────┼── T3.1 ── T3.4
T1.2 ──────────┘           │                    │   T3.2
                           ├── T2.4 ────────────┤   T3.3
                           └── T2.5 ────────────┘
```

⊕ = 可并行

## 工时估算

| 任务 | 复杂度 | 预估 |
|------|--------|------|
| T1.1 | S | 2h |
| T1.2 | M | 3h |
| T1.3 | S | 2h |
| T2.1 | L | 8h |
| T2.2 | M | 4h |
| T2.3 | M | 5h |
| T2.4 | S | 1h |
| T2.5 | S | 2h |
| T3.1 | M | 4h |
| T3.2 | M | 5h |
| T3.3 | S | 2h |
| T3.4 | S | 1h |
| **总计** | | **39h (~5 工作日)** |
