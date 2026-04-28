---
change: reef-hermes-architecture
artifact: specs
phase: research
---

# Specs: Hermes 能力架构研究

## R1. Hermes 能力模型定义

### Research Question: 三种角色的能力边界如何划分？

需要定义的能力模型：

| 维度 | Coordinator（协调者） | Executor（执行者） | Full（全能） |
|------|----------------------|-------------------|-------------|
| 触发条件 | `picoclaw --server` | Client 连接到 Server | `picoclaw`（默认） |
| SystemPrompt 身份 | "你是团队协调者" | "你是任务执行专家" | "你是 PicoClaw 助手" |
| 可用 Tool 集 | reef_submit, reef_query, reef_status, send_message | 全部工具集 | 全部工具集 |
| 禁用 Tool 集 | web_search, exec, read_file, write_file... | — | — |
| LLM 决策范围 | 意图识别 + 任务分解 + 结果聚合 | 任务执行 + 结果报告 | 全部 |
| 直接回复能力 | 仅简单问候/元问题 | 仅任务结果 | 全部 |
| 降级行为 | 无 Client → 临时升级为 Full | 无 Server → 退化为 Full | — |

### 需要研究的点
- Coordinator 的"最小 Tool 集"如何确定？
- 简单问候 vs 复杂任务的判定标准是什么？
- 降级触发条件和恢复条件如何定义？

---

## R2. SystemPrompt 分层注入

### Research Question: 如何在现有 PromptStack 中注入协调者身份？

现有 PromptStack 结构：
```
Layer: kernel      → Slot: identity     ← "You are PicoClaw..."
Layer: instruction → Slot: workspace    ← AGENT.md / SOUL.md
Layer: capability  → Slot: skill_catalog ← Skills list
Layer: capability  → Slot: active_skill  ← Active skill instructions
Layer: context     → Slot: memory        ← Memory
Layer: context     → Slot: runtime       ← Runtime context
Layer: turn        → Slot: message       ← User message
```

**方案 A：覆盖 PromptSlotIdentity**
- Coordinator 模式下，将 identity slot 的内容替换为协调者身份
- 优点：简单直接
- 缺点：丢失原始 PicoClaw 身份

**方案 B：新增 PromptSlotHermesRole**
- 在 identity 和 workspace 之间新增一个 slot
- 优点：不破坏现有结构
- 缺点：增加复杂度

**方案 C：新增 PromptLayerHermes**
- 在 kernel 和 instruction 之间新增一个 layer
- 优点：完整隔离
- 缺点：改动较大

### 需要研究的点
- 哪种方案对现有代码侵入最小？
- 协调者身份 Prompt 的具体内容如何设计？
- 如何让 LLM 理解"你只能协调，不能直接执行"？

---

## R3. Tool 注册策略

### Research Question: 如何按模式切换 Tool 集？

现有 Tool 注册在 `registerSharedTools()` 中，无条件注册所有工具。

**方案 A：条件注册（编译期）**
```go
if hermesMode == HermesCoordinator {
    // 只注册协调者工具
    agent.Tools.Register(reefSubmitTool)
    agent.Tools.Register(reefQueryTool)
} else {
    // 注册全部工具
    registerSharedTools(al, cfg, ...)
}
```

**方案 B：运行时过滤（运行期）**
```go
// 注册全部工具，但添加 Guard
agent.Tools.Register(webSearchTool.WithGuard(HermesGuard))
agent.Tools.Register(execTool.WithGuard(HermesGuard))
```

**方案 C：Prompt 级约束（软约束）**
```go
// 在 SystemPrompt 中明确告知 LLM 不要使用某些工具
// "你只能使用 reef_submit_task 和 reef_query_task"
```

**方案 D：双重保障（A + C）**
- 条件注册 + Prompt 约束
- 硬约束兜底，软约束引导

### 需要研究的点
- 方案 A 是否会导致降级困难？
- 方案 B 的 Guard 实现复杂度？
- 方案 C 的 LLM 遵从率？
- 方案 D 的维护成本？

---

## R4. 运行时 Guard 设计

### Research Question: 如何在运行时防止越界？

**Guard 接口设计：**
```go
type ToolGuard interface {
    Allow(ctx context.Context, toolName string, args map[string]any) bool
    DenyMessage(toolName string) string
}
```

**HermesCoordinatorGuard：**
- Allow: 只允许 reef_* 工具和 send_message
- DenyMessage: "作为协调者，你不能直接执行此操作，请使用 reef_submit_task 分发到团队成员"

**降级 Guard：**
- 当无 Client 在线时，Guard 动态放行
- 当 Client 上线后，Guard 恢复约束

### 需要研究的点
- Guard 检查的性能开销？
- 降级/恢复的触发时机？
- Guard 与 Hook 系统的关系？

---

## R5. 降级策略

### Research Question: 无 Client 在线时如何处理？

**策略 A：硬拒绝**
- 无 Client → 返回错误消息："当前没有可用的团队成员，请稍后再试"
- 优点：行为一致
- 缺点：用户体验差

**策略 B：自动降级**
- 无 Client → 临时升级为 Full 模式
- Client 上线 → 恢复 Coordinator 模式
- 优点：始终可用
- 缺点：行为不一致，可能混乱

**策略 C：队列等待**
- 无 Client → 任务入队等待
- 超时 → 降级为 Full 模式
- 优点：优先团队作战
- 缺点：延迟增加

**策略 D：混合策略**
- 简单问题 → Server 直接处理（无论是否有 Client）
- 复杂任务 → 有 Client 则分发，无 Client 则降级
- 超时 → 降级

### 需要研究的点
- 如何判断"简单问题" vs "复杂任务"？
- 降级后的行为如何通知用户？
- 降级恢复的时机如何确定？

---

## R6. 启动模式切换

### Research Question: CLI 参数如何映射到行为模式？

```
picoclaw                    → Full 模式（现有行为）
picoclaw --server           → Server 模式（Coordinator）
picoclaw --server --fallback → Server 模式（Coordinator + 降级为 Full）
```

**映射链：**
```
CLI 参数 → HermesMode → {
    SystemPrompt 身份,
    Tool 注册策略,
    Guard 配置,
    降级策略,
}
```

**配置持久化：**
```json
{
  "hermes": {
    "mode": "coordinator",
    "fallback": true,
    "fallback_timeout_ms": 30000
  }
}
```

### 需要研究的点
- 是否需要 config.json 中的 hermes 配置段？
- 模式是否可以运行时切换（热重载）？
- Server 模式下是否还需要支持 CLI 直接对话？

---

## R7. 防绕过机制

### Research Question: 如何防止 LLM 绕过约束？

**软约束（Prompt 级）：**
- SystemPrompt 明确角色定义
- Tool 描述中标注"仅协调者可用" / "仅执行者可用"
- LLM 遵从率约 90-95%

**硬约束（Tool 级）：**
- Tool 注册时过滤（不注册 = 不可调用）
- Tool Guard 拦截（注册但拦截 = 可调用但被拒绝）
- 遵从率 100%

**双重保障策略：**
```
Layer 1: SystemPrompt 角色定义 → 引导 LLM 主动选择正确工具
Layer 2: Tool 注册过滤 → 物理上移除不该有的工具
Layer 3: Tool Guard → 运行时拦截意外调用
```

### 需要研究的点
- 三层保障是否过度设计？
- Tool 注册过滤 vs Guard 的性能差异？
- LLM 看到被过滤的工具列表是否会产生困惑？

---

## R8. SubTurn 与 Hermes 的关系

### Research Question: 现有 SubTurn 机制如何与 Hermes 协调者模式共存？

**SubTurn（现有）：**
- AgentLoop 内部的子任务并发机制
- spawn 工具 → 创建 SubTurn → 内部 LLM 调用
- 适用于单 Client 内的子任务拆分

**Hermes（新增）：**
- AgentLoop 外部的任务分发机制
- reef_submit_task → Scheduler → Client 执行
- 适用于多 Client 间的任务分发

**关系：**
```
Coordinator AgentLoop:
  → reef_submit_task → Client-A (Executor)
  → reef_submit_task → Client-B (Executor)
  → 不使用 SubTurn（协调者不直接执行）

Executor AgentLoop (Client-A):
  → spawn → SubTurn-1 (内部子任务)
  → spawn → SubTurn-2 (内部子任务)
  → 不使用 reef_submit_task（执行者不外部分发）
```

### 需要研究的点
- Coordinator 模式下是否完全禁用 spawn 工具？
- Executor 模式下是否完全禁用 reef_submit_task？
- 两种并发机制如何避免混淆？
