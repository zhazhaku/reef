---
change: reef-hermes-architecture
schema: spec-driven
status: research
created: 2026-04-28
updated: 2026-04-28
---

# Proposal: Hermes 能力架构 — Server/Client 行为约束与模式切换

## 1. 核心问题

### 1.1 意图发散问题

当前设计中，Server 端 AgentLoop 收到用户消息后，LLM 自主决策是否调用 ReefSwarmTool 分发到 Client。但 LLM 没有约束机制，会倾向于：

```
用户: "帮我分析代码质量"
Server AgentLoop + LLM:
  → 直接调用 web_search + read_file + LLM 分析 → 直接回复用户
  → 完全跳过 ReefSwarmTool，即使有 analyzer 角色的 Client 在线
```

**结果**：Server 端"自作主张"处理了本该分发的任务，Client 闲置，团队作战名存实亡。

### 1.2 模式切换问题

- `picoclaw` 启动 → 单 Client 模式（现有行为，不变）
- `picoclaw --server` 启动 → Server 模式（虚拟团队作战）

但当前架构中，Server 模式下 AgentLoop 的行为与单 Client 模式完全相同，没有能力边界约束。

### 1.3 根本原因

AgentLoop 的 SystemPrompt 是通用的，没有"你是一个团队协调者"的角色定义。
AgentLoop 的 Tool 集合包含所有工具（web_search、read_file、exec...），LLM 倾向于直接使用而非分发。

## 2. 研究目标

设计一套 **Hermes 能力架构**，实现：

1. **Server 模式 = 协调者角色**：AgentLoop 只做意图识别 + 任务分发 + 结果聚合，不直接执行
2. **Client 模式 = 执行者角色**：AgentLoop 拥有完整工具集，直接执行任务
3. **能力边界约束**：通过 SystemPrompt + Tool 白名单 + 运行时策略三重约束，防止 Server 端发散
4. **优雅降级**：无 Client 在线时，Server 可降级为自行处理
5. **启动参数切换**：`picoclaw` vs `picoclaw --server` 决定行为模式

## 3. 研究范围

### In Scope
- Hermes 能力模型定义（协调者 vs 执行者 vs 全能）
- SystemPrompt 分层架构（基于现有 PromptStack）
- Tool 注册策略（白名单/黑名单/条件注册）
- 运行时能力约束（Router + Guard）
- 启动模式切换（CLI 参数 → 行为模式）
- 降级策略（无 Client 时的行为）
- 与现有 PromptLayer/PromptSlot/PromptSource 的融合

### Out of Scope
- 具体实现代码
- 前端 UI 变更
- 协议变更

## 4. 研究问题

| # | 问题 | 关键点 |
|---|------|--------|
| Q1 | 如何定义 Hermes 能力模型？ | 协调者/执行者/全能三种角色的能力边界 |
| Q2 | 如何约束 Server 端 AgentLoop 不直接执行？ | SystemPrompt + Tool 白名单 + 运行时 Guard |
| Q3 | 如何在现有 PromptStack 中注入协调者角色？ | 新增 PromptSlot？还是覆盖 PromptSlotIdentity？ |
| Q4 | Tool 注册策略如何按模式切换？ | 条件注册 vs 运行时过滤 vs LLM 级约束 |
| Q5 | 无 Client 在线时如何降级？ | 自动降级 vs 提示用户 vs 队列等待 |
| Q6 | 启动参数如何映射到行为模式？ | --server → HermesMode → 能力约束链 |
| Q7 | 如何防止 LLM 绕过约束？ | Tool 级硬约束 vs Prompt 级软约束 vs 双重保障 |
| Q8 | 现有 SubTurn 机制如何与 Hermes 协调者模式共存？ | SubTurn 是内部并发，Hermes 是外部分发 |

## 5. 预期产出

1. **Hermes 能力模型规格**：三种角色的能力定义和约束规则
2. **PromptStack 扩展设计**：新增 Slot/Source 注入协调者身份
3. **Tool 策略设计**：按模式注册/过滤/约束工具集
4. **运行时 Guard 设计**：Router + Guard 防止越界
5. **降级策略设计**：无 Client 时的行为决策
6. **模式切换设计**：CLI 参数 → 行为模式的完整链路
7. **与 reef-scheduler-v2 设计方案的融合点**

## 6. 研究方法

1. 代码走读：AgentLoop / PromptStack / Tool 注册 / Steering / SubTurn
2. 对比分析：OpenAI Swarm / AutoGen / CrewAI 的角色约束机制
3. 原型验证：最小 POC 验证约束有效性
4. 设计评审：与 reef-scheduler-v2 设计方案的融合评审

## 7. 风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| LLM 总是能绕过 Prompt 约束 | Server 端仍可能直接执行 | Tool 级硬约束兜底 |
| 过度约束导致无法降级 | 无 Client 时无法处理任何任务 | 降级策略动态放宽 |
| 与现有 SubTurn 冲突 | 两种并发机制混淆 | 明确分工：SubTurn=内部，Hermes=外部 |
