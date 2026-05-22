---
change: context-anti-corruption
artifact: research
phase: implemented (partial)
created: 2026-05-11
updated: 2026-05-16
merged_from: context-sandbox
---

# Research: 上下文反腐化综合方案

> **合并说明**: 此文档合并了 `context-sandbox` 项目的研究成果（context-mode + ACE 分析）。
> 实施状态见 `gsd-plan.md`。冲突分析见 `cross-proposal-conflict-analysis.md`。
> 
> **代码核查 (2026-05-16)**: L1/L2/L4 已完整实施（在 deepseek-token-optimization 变更中），
> L3 有简化版（工具输出截断），L5 有简化版（LLM 反射 → MEMORY.md）。详见下文 §3。

## 0. 问题定义

```
对话轮次 ↑ →
  模型忘记早期重要信息      (Lost in the Middle)
  工具输出填满窗口          (Tool Output Pollution)
  推理 token 膨胀 40%+      (Reasoning Bloat)
  structured data 污染自然语言 (Serialization Noise)
  注意力衰减 → Agent“变笨”  (Cognitive Decay)
  最终偏离目标甚至完全失效  (Drift & Collapse)
```

## 1. 外部方案研究

### 1.1 context-mode (mksglu)

| 维度 | 设计 | 与我们的适配 |
|------|------|-------------|
| **Tool sandbox** | PreToolUse hook 拦截工具输出，97.4% 不入上下文 | ✅ 已有 seahorse SQLite，天然可用 |
| **FTS5/BM25** | 沙箱输出索引到 FTS5，按需检索 | ✅ 已有 `short_grep` + trigram 分词 |
| **Think-in-Code** | LLM 写脚本替代读文件，1 script = 10 tool calls | 🔶 已有 prompt 设计，需强化 |
| **Session continuity** | SQLite 追踪文件编辑/任务/错误，压缩后 BM25 检索 | 🔶 已有 L2 checkpoint 设计 |
| **15 平台支持** | 跨 IDE (Claude Code, Codex, Cursor, Copilot, etc.) | ❌ 不需要（单飞书通道） |

**可借鉴核心思路**：Think-in-Code 是范式级改变——不是优化工具输出，而是**不产生工具输出**。一个 `ctx_execute` 脚本替代 47 次 `Read()` → 700KB → 3.6KB。

### 1.2 ACE (kayba-ai)

| 维度 | 设计 | 与我们的适配 |
|------|------|-------------|
| **三角色** | Agent / Reflector / SkillManager | 🔶 L4 Reflector 设计对应用同模式 |
| **Skillbook** | 持久化学习策略，每次更新更聪明 | ❌ 不适合聊天 Agent（但可参考"错误不再犯"逻辑） |
| **Learning Loop** | 反馈→反思→技能更新→上下文注入 | 🔶 检查点可作为简化版 |
| **Pipeline Engine** | 可组合步骤：分支、并行、错误处理 | ❌ 过于重量级 |
| **Claude Code 集成** | Runner for Claude Code CLI | ❌ 我们的 Hermes 已有 client 分派 |

**可借鉴核心思路**：Reflector 异步反思 + 持久化学习。不是每次对话都从头来，而是**积累经验**。

## 2. 与现有设计的整合

### 2.1 我们的已有资产

| 资产 | 成熟度 | 作用 | 代码位置 |
|------|:---:|------|------|
| seahorse SQLite + FTS5 | ✅ 生产 | 知识库基础设施 | `pkg/seahorse/` |
| short_grep / short_expand | ✅ 生产 | 按需检索 | `pkg/seahorse/` |
| Context Conservation 规则 | ✅ 已部署 | L1: Think-in-Code prompt 引导 | `context.go:143` |
| createAndInjectCheckpoint | ✅ 已部署 | L2: 压缩前状态保存 | `context_seahorse.go:165` |
| staticSystemPrompt 分离 | ✅ 已部署 | L4: 缓存前缀分区 | `context.go:745` |
| reasoning_content 剥离 | ✅ 已部署 | 减少 41% 噪声 | `pipeline_llm.go` |
| compaction engine | ✅ 生产 | 窗口溢出时压缩 | `pkg/seahorse/` |
| tool result truncation (4096) | ✅ 已部署 | 单条工具输出上限 | `tool_truncate.go` + `context_sandbox.go` |
| ReflectOnTurn (LLM 反射) | ✅ 已部署 | L5 简化版: 异步反思 → MEMORY.md | `reflector.go:260` |
| reasoning-based compact trigger (30%) | ✅ 已部署 | 推理膨胀时压缩 | `short_compaction.go` |

### 2.2 待实现 (完整版)

| 资产 | 当前状态 | 完整版目标 | 工时 |
|------|:---:|------|------|
| reef_execute tool | ❌ 无 | L3: FTS5 索引 + PreToolUse hook 隔离 | 8h |
| Skillbook | ❌ 无 | L5: Python 递归反射 + 跨会话策略注入 | 5.5h |

### 2.2 待实现 + 外部思路注入

```
┌──────────────────────────────────────────────────────────────────────┐
│                     上下文反腐化 5 层架构                              │
│                                                                      │
│ L1: Think-in-Code (prompt 级)                                        │
│   context-mode 思路 → "不要读数据，写脚本处理数据"                      │
│   实现：system prompt 规则 + exec tool 引导                            │
│   工时：0.5h                                                          │
├──────────────────────────────────────────────────────────────────────┤
│ L2: Checkpoint before Compaction (seahorse 级)                       │
│   ACE 思路 → 压缩前持久化关键状态                                      │
│   实现：扫描 assistant msg → 提取 files/tasks/decisions → Summary      │
│   工时：4h                                                            │
├──────────────────────────────────────────────────────────────────────┤
│ L3: Tool Output Sandbox (tool 级)                                    │
│   context-mode 思路 → 工具输出不入窗口，FTS5 按需检索                   │
│   实现：新 reef_execute tool → 完整输出 → seahorse → 摘要回 LLM        │
│   工时：8h                                                            │
├──────────────────────────────────────────────────────────────────────┤
│ L4: Stable Prefix Partitioning (缓存级)                               │
│   自有设计 → 静态 system prefix + 动态 content 后置                    │
│   实现：BuildMessages 重构 → 60%+ 前缀缓存命中                         │
│   工时：3h                                                            │
├──────────────────────────────────────────────────────────────────────┤
│ L5: Async Reflector (会话级)                                         │
│   ACE 思路 → 对话结束后异步反思，积累 LEARNINGS                        │
│   实现：turn_end → cheap model 反思 → 写入 MEMORY.md                   │
│   工时：12h                                                           │
└──────────────────────────────────────────────────────────────────────┘
```

## 3. context-mode 深度适配

### 3.1 Think-in-Code 范式

context-mode 最激进的设计：**一个 exec 脚本替代 N 次工具调用**。

```
Before:
  read_file(a.go) → 5KB
  read_file(b.go) → 8KB
  read_file(c.go) → 6KB
  ...
  47 × Read = 700KB in context

After:
  ctx_execute("javascript", `
    const files = ['a.go','b.go','c.go',...];
    files.forEach(f => console.log(f + ': ' + 
      fs.readFileSync(f,'utf8').split('function').length + ' functions'));
  `) → 3.6KB summary
```

**适配方案**：在 system prompt 中强化 Context Conservation 规则，引导模型在以下情况用 `exec` + 脚本替代 `read_file` 循环：
- 需要读 3+ 个文件
- 需要在大文件中搜索
- 需要统计分析

### 3.2 Sandbox 对比

| | context-mode | 我们的设计 | 差异 |
|------|-------------|-----------|------|
| 截获点 | PreToolUse hook (MCP) | `seahorseToProviderMessages` 前 | context-mode 更早 |
| 索引 | FTS5 + BM25 | FTS5 + trigram | 同架构 |
| 检索 | `ctx_search()` | `short_grep` | 功能对等 |
| 恢复 | `ctx_expand()` | `short_expand` | 功能对等 |
| 会话连续 | SQLite tracking + BM25 | L2 checkpoint | context-mode 更细粒度 |

### 3.3 可以不做的事

- context-mode 的 15 平台支持 → 我们只需飞书/MCP
- context-mode 的 PreToolUse hook（MCP 协议）→ 我们的 hook 机制已足够

## 4. ACE 深度适配

### 4.1 Three Roles 映射

| ACE 角色 | 我们的映射 | 状态 |
|------|-----------|:---:|
| Agent | coordinator (Hermes) / normal agent | ✅ 已有 |
| Reflector | L5 async reflector | 🔶 待实现 |
| SkillManager | — | ❌ 不适合 chat agent |

SkillManager 在 ACE 中用于更新"问题→策略"技能库，但我们不是 task-based agent（每次对话不同），不适合。Reflector 更有价值——对话后反思"哪些工具调用失败了、哪些模式有效"。

### 4.2 Learning Loop 简化版

```
ACE 完整版:
  Sample → Agent → Environment → Reflector → SkillManager → Skillbook → Agent

我们的简化版:
  对话结束(turn_end) → Reflector(cheap model) → 反思 → MEMORY.md (Recent Learnings)
```

简化原因：Skillbook 对于多轮、多 session 的聊天 Agent 不适用（每次对话不同）。但反射式记忆可以避免"重复犯错"。

## 5. 实施优先级

| 层 | 源自 | 工时 | 收益 | 依赖 |
|------|------|------|------|------|
| **L1** Think-in-Code | context-mode | 0.5h | 中 | 无 |
| **L2** Checkpoint | ACE + 自有 | 4h | 高 | 无 |
| **L4** Stable Prefix | 自有 | 3h | 高 (cost ↓40%) | 无 |
| **L3** Tool Sandbox | context-mode | 8h | 很高 (98%) | L2 |
| **L5** Async Reflector | ACE | 12h | 远期 | 其他层稳定 |

## 6. 决策建议

**本周执行 L1 + L4**：
- L1：system prompt 新增 Context Conservation 规则（0.5h）
- L4：stable prefix partitioning（3h，缓存命中率 0%→60%+）
- 两者零依赖，立即可部署

**下周执行 L2**：
- checkpoint before compaction（4h）

**L3 视 L1+L4 效果决定**：
- 如果 L1 (Think-in-Code) 已经引导模型自我约束工具输出，L3 (reef_execute) 可能不那么紧迫

**L5 远期**：
- ACE Reflector 最有趣的思路，但需要积累足够对话数据才有效
