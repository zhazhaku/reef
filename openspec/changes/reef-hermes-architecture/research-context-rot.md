---
change: reef-hermes-architecture
artifact: research
phase: research
topic: context-rot
created: 2026-05-11
---

# Research: Context 腐化问题深度分析

## 1. 数据采集

从 `gateway.log` 中截取 turn-22, iteration 17 的 token breakdown：

```
total_tokens_est:       138,201
api_prompt_tokens:      148,600
history_count:              559
history_tokens:         132,616
reasoning_tokens:        57,195    (41% ⚠️)
reasoning_chars:        138,816
tool_result_chars:      144,501
system_total_tokens:        620
tool_defs_tokens:         4,965
dynamic_tokens:             102
summary_tokens:               0    (0% ❌)
summary_chars:                0
```

## 2. 腐化因子分析

### Factor A: reasoning_content 膨胀 (41%)

每轮 LLM 回复都携带 DeepSeek thinking 输出。非 tool-call 消息的 reasoning 无意义但依然会累积到 seahorse 存储中。

**已修复**: `providerToSeahorseMessage` + `seahorseToProviderMessages` 双保险剥离无 tool-call 消息的 reasoning。

**剩余问题**: 带有 tool_calls 的消息仍会累积 reasoning。长对话中 tool-call 消息可达数十条，每条 2-3k reasoning tokens。

### Factor B: tool_result 膨胀

Tool results（如 `exec` 输出、`read_file` 内容）累计 144k chars。这些是必要的上下文但未被压缩。

### Factor C: 压缩未触发 (0%)

```
summarize_message_threshold: 20
summarize_token_percent: 75
context_window: 262,144
```

虽然有 559 条消息和 148k prompt tokens，但 `summary_tokens: 0` 说明 **压缩从未触发**。

原因: `forceCompress` 仅在 `contextWindow` 被超出时才触发 (`pipeline_setup.go:59`)。当前 148k < 262k 窗口，系统判定不需要压缩。

**但有效上下文已经严重稀释**:
- 148k tokens 中真正有用的对话内容 ≈ 70k
- 57k reasoning + 大量 tool result 骨架使 LLM 注意力分散

## 3. 根因链

```
DeepSeek thinking_mode 全开
  → 每轮 assistant 回复带 2-3k reasoning tokens
    → 559 轮后累积 57k reasoning
      → prompt 膨胀至 148k
        → 但 < 262k 窗口，不触发压缩
          → LLM 在噪声中运转
            → 响应质量下降，只做 1 轮 tool call 就 stop
```

## 4. 建议方案

### 4.1 立即修复（已完成）
- [x] 无 tool-call 消息剥离 reasoning（双保险）

### 4.2 短期修复
- [ ] 降低 `forceCompress` 触发阈值: 当 `reasoning_tokens / total > 30%` 时强制压缩
- [ ] 对 tool_result 启用 `max_length` 截断（如 4096 chars/条）
- [ ] seahorse compact 时把 reasoning_tokens 计入 budget 判断

### 4.3 中期优化
- [ ] Tool result summary: 长 tool 结果由 LLM 压缩为摘要
- [ ] 基于 "有效 token 比例" 的动态压缩触发

### 4.4 长期架构
- [ ] Hermes 模式下 coordinator 不亲自调工具，context 永不膨胀（已有设计，待实现 client 委托）
