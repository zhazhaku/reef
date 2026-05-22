# Proposal: Token 削减 & DeepSeek 缓存命中率联合优化

## 问题

### 1. Token 消耗过高

| 瓶颈 | 现状 | 浪费量 |
|------|------|--------|
| **Tool call 结果膨胀** | `web_fetch` / `exec` / `read_file` 输出完整存入 API 消息 | 单次 2000-20000 chars |
| **System prompt 动态污染** | `Current Time` + `CONTEXT_SUMMARY` 混在 system msg 里 | ~1800 tokens/次 |
| **Reasoning_content 回传** | 每条 thinking 的 assistant msg 回传完整 reasoning | 500-5000 chars/条 |
| **Skill catalog 冗余** | 80+ 工具定义重复出现在每次请求 | ~1600 tokens/次 |

**典型对话中**，只有 ~45% 的 token 是「语义必需的」，其余是结构开销和冗余。

### 2. DeepSeek 缓存命中率极低

**DeepSeek 上下文缓存原理**：前缀匹配。从 messages[0] 开始逐 token 对比历史请求。匹配的前缀按 **¥0.02/1M** 计费（**98% off**），未匹配按 ¥1/1M。

**当前代码根因**：system prompt 是一个单体，其中：
- 静态部分（identity, rules, memory, skills）占 45%，**每轮完全不变**
- 动态部分（`Current Time`, `CONTEXT_SUMMARY`, sender info）混在**同一 system message 中**
- 时间每轮变化 → 整个 system msg hash 改变 → 前缀缓存 100% miss

**一句话：我们把不会变的东西和每轮都在变的东西塞进了同一个 system message，导致 DeepSeek 无法识别相同前缀。**

### 3. 承诺：不丢记忆

**两层存储保证**：

```
┌─────────────────────────────────────────────┐
│           seahorse SQLite (完整存储)           │
│  - 所有消息完整存储 (Content + Parts + Reasoning) │
│  - 摘要树深度保存 (leaf / condensed)           │
│  - short_expand / short_grep 可检索           │
├─────────────────────────────────────────────┤
│           JSONL 文件 (完整归档)                │
│  - sessions/{sessionKey}/messages.jsonl      │
│  - 完整的 message 序列                        │
├─────────────────────────────────────────────┤
│    ─── 优化截断层 (仅影响 API 请求) ───         │
│  - System prompt 分区（分离稳定/动态）          │
│  - Tool output 截断（保留前 N chars）          │
│  - Reasoning 截断（超过阈值时压缩）             │
│  - 这些优化只改 API payload，不改存储           │
└─────────────────────────────────────────────┘
```

**优化只触及 `BuildMessages` → `prepareMessagesForRequest` → `SerializeMessages` 这条链路，seahorse 和 JSONL 完全不受影响。**

## 目标

| 指标 | 当前（估计） | 目标 | 手段 |
|------|------------|------|------|
| 每次 API input tokens | 9000-15000 | **5000-8000** | 工具输出截断 + reasoning 压缩 |
| DeepSeek 缓存命中率 | ~0% | **50-70%** | 稳定前缀分区 |
| 每轮 API 输入成本 | ¥0.009-0.015 | **¥0.003-0.005** | 缓存命中 + token 削减叠加 |
| 对话记忆完整性 | 100% | **100%** | seahorse + JSONL 不变 |

## 优化方案矩阵

| # | 方向 | 收益 | 风险 | 实现位置 |
|---|------|------|------|----------|
| P1 | Stable Prefix Partitioning | 缓存命中 0→60%+ | 中（需验证多 system msg 行为） | `prompt.go`, `context_seahorse.go` |
| P2 | 动态内容后置 | 缓存命中 +20% | 低 | `prompt.go` |
| P3 | Tool Output Truncation | Token -15~25% | 中（截断过多影响决策） | `context_seahorse.go` 或新增 |
| P4 | Reasoning 截断 | Token -5~10% | 低 | `pipeline_llm.go` |
| P5 | Token 分布可观测 | 测量基线 | 无 | `context_usage.go` |

**P1+P2 是缓存命中率的核心，P3+P4 是 token 削减的核心，P5 是诊断基础。**

## 风险与缓解

| 风险 | 缓解 |
|------|------|
| 多 system message 不被 DeepSeek 接受 | 回退到「动态内容注入最后 user msg」方案 |
| 截断关键工具输出导致 LLM 误判 | 关键工具（read_file/exec）保守截断；`short_expand` 可恢复 |
| 分区后 prompt 结构变化降低回复质量 | A/B 人工评估 |
| seahorse 摘要质量下降 | 摘要策略不变，仅截断 API 消息 |

## 相关文件索引

见 `design.md` §5。
