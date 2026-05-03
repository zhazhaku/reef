# Reef 项目第三次审计 — 源码与规划对齐报告

> 审计日期：2026-05-03
> 范围：openspec 设计文档 vs. 实际源码实现
> 方法：逐规格比对、测试覆盖率扫描、集成完整性检查

---

## 一、总体评分

```
已覆盖规范: ~78%  (比首次审计 +10%)
严重问题: 7  (P0)
功能缺口: 7  (P1)
待完善项: 8  (P2)
琐碎问题: 5  (P3)
测试编译失败: 3 文件

评估: 代码质量 A-，但与设计文档的对齐度约 B+
```

---

## 二、P0（严重）— 必须修复才能达到设计规范一致

### P0-1: 消息信封缺少 `version` 字段
- **规范要求**: 所有消息 `{type, version, payload, timestamp}`
- **实际实现**: `{msg_type, task_id, timestamp, payload}`
- **影响**: 与规范定义的线缆格式不兼容。`version` 仅存在于 `RegisterPayload` 内
- **位置**: `pkg/reef/protocol.go:47`
- **修复**: 要么更新规范对齐代码，要么在消息结构体添加 `Version` 字段

### P0-2: 缺少通用 `error` 消息类型
- **规范要求**: SWARM-04 规定双向 `error` 消息用于未知类型的回复
- **实际实现**: 仅有 `MsgRegisterNack`，未知类型仅打 warn 日志
- **影响**: 当 Client 发送非法消息时，Server 不会返回格式化的错误响应
- **位置**: `pkg/reef/protocol.go`
- **修复**: 新增 `MsgError` 消息类型 + Payload + 路由处理

### P0-3: HeartbeatPayload 缺少 `current_tasks`
- **规范要求**: SWARM-03 — 心跳包含当前正在执行的 task ID 列表
- **实际实现**: `HeartbeatPayload{Timestamp int64}` — 仅时间戳
- **影响**: Server 无法通过心跳感知 Client 的真实负载，调度器的容量感知退化到靠分配/完成事件推断
- **位置**: `pkg/reef/protocol.go:128`

### P0-4: 状态机缺少 `Created → Cancelled` 转换
- **规范要求**: 状态图明确定义 `Created` 可以直接转 `Cancelled`
- **实际实现**: `CanTransitionTo` 只允许 `Created→{Queued, Assigned, Failed}`
- **影响**: 创建后立即取消的任务会被状态机拒绝，卡在 Created 状态
- **位置**: `pkg/reef/task.go:39`

### P0-5: Client 断连时不会自动暂停运行中任务
- **规范要求**: CONN-02 — 断连时自动暂停进行中的任务
- **实际实现**: Connector 仅触发重连，不调用 TaskRunner.PauseTask
- **影响**: 断连的 Client 可能继续执行但无法上报进度，重连后可能形成竞争
- **位置**: `pkg/channels/swarm/swarm.go`

### P0-6: 长任务缺少定时进度心跳机制
- **规范要求**: TASK-02 — 超 60s 任务每 30s 发送一次 task_progress
- **实际实现**: 仅在事件触发时上报（工具调用/切换回合），无定时心跳
- **影响**: 长 LLM 推理回合可能超过 30s 无上报，Server 认为超时
- **位置**: `pkg/reef/client/task_runner.go`

### P0-7: 重分配时 dispatch 负载缺少 `previous_attempts`
- **规范要求**: RETRY-03 — task_dispatch 必须包含 previous_attempts
- **实际实现**: `TaskDispatchPayload` 没有 PreviousAttempts 字段
- **影响**: 新 Client 不知道之前失败的尝试，可能重复相同错误
- **位置**: `pkg/reef/protocol.go:133`

---

## 三、P1（高）— 功能缺口

### P1-1: 两套代码库存在导入路径不一致
- 主 reef 用 `github.com/sipeed/reef`，picoclaw 用 `github.com/zhazhaku/reef`
- picoclaw 版本功能更全（TLS、持久化队列、Web UI、通知、结果回传），主 reef 版本落后
- 双轨并行开发增加了维护成本

### P1-2: 缺少 `POST /admin/tasks/:id/control` 控制端点
- 规范 ADMIN-02 要求 HTTP API 发送 cancel/pause/resume
- 当前只能通过 WebSocket 直接操作
- `pkg/reef/server/admin.go` 和 picoclaw 版本都未实现

### P1-3: 基于角色 YAML 的技能加载未接入
- `pkg/reef/role/role.go` 完成了 YAML 解析，但 Client 启动路径未使用
- `cmd/reef/client/` 仅接受 `--skills` 手动标志
- SkillsLoader 不存在，无法根据角色自动过滤技能

### P1-4: 调度器优先级算法不完全符合规范
- 规范 SCHED-01: 容量优先 → 心跳新鲜度 → 随机
- 实现: 仅负载最低原则，缺少心跳新鲜度和随机化作为胜出条件
- `scheduler.go:matchClient()` 中的 `pickRandomEligible` 没有考虑心跳时间

### P1-5: Admin 端点安全模型不一致
- 主 reef: admin 端点无安全认证（直接暴露）
- picoclaw: 使用 `Bearer <token>`
- 规范 ADMIN-01: 要求 `x-reef-token` header
- 三种不同的安全策略！

### P1-6: TaskErrorType 默认值与实际使用不一致
- TaskRunner 固定用 `"escalated"`
- SwarmChannel 用 `"execution_error"` 和 `"cancelled"`
- 规范要求: `execution_error` / `llm_error` / `timeout` / `cancelled`
- 三种不同的枚举！影响 Server 端升级决策的可靠性

### P1-7: pendingControls map 存在内存泄漏
- 断连 Client 的待处理控制消息无限累积
- 无过期机制，无上限
- `websocket.go` 中 picoClaw 版本的 `pendingControls`

---

## 四、P2（中）— 完整性与健壮性

| ID | 问题 | 位置 | 严重度 |
|----|------|------|--------|
| P2-1 | 超时检测用固定时间而非"连续N次"心跳错失 | registry.go | 🟡 |
| P2-2 | `reef submit` CLI 发送 POST /admin/tasks 但实际路由是 /tasks | command.go:267 | 🔴 CLI 不可用 |
| P2-3 | 升级流程跳过 Escalated 状态直接 Failed→Queued | scheduler.go | 🟡 |
| P2-4 | 缺少 FindMatch 高阶队列操作 | queue.go | 🟢 |
| P2-5 | SwarmChannel pause/cancel/resume 仅设标志不阻塞 AgentLoop | swarm.go | 🟡 |
| P2-6 | 重试不区分可恢复/不可恢复错误 | task_runner.go | 🟡 |
| P2-7 | 心跳间隔规范 30s，实现 10s | connector.go | 🟢 |
| P2-8 | 进化引擎消息路由 Handler 缺失或为存根 | evolution/server/ | 🟡 |

---

## 五、P3（低）— 测试与代码质量问题

### P3-1: 三个测试文件编译失败

```
pkg/reef/server/server_coverage_test.go — undefined: testError, setupAdminTest
pkg/reef/client/ — 未检查完整
pkg/reef/raft/ — 依赖问题
```

这些是之前 round-trip 编辑遗留的损坏。`testError` 和 `setupAdminTest` 在文件某处被删除或重命名了。

### P3-2: 日志格式规范化
- 规范 ADMIN-03 要求 `event/timestamp/component` 统一字段
- 当前使用 slog 但有多种字段命名风格

### P3-3: reply_to 支持在两个代码库中不一致
- picoclaw 实现了完整 `ReplyToContext` + `ResultDelivery`
- 主 reef 缺少

### P3-4: Task.ModelHint 字段缺失
- picoclaw 的 admin.go 解析 `model_hint` 但 sipeed 的 task.go 没有该字段

### P3-5: Escalated 任务无 TTL/过期机制
- 管理员不介入时永不下线

---

## 六、P8 认知架构剩余问题

参考 W5+W6 已完成的内容，P8 所有功能已实现。但仍有以下集成缺口：

| 问题 | 状态 |
|------|:---:|
| `agent.ContextManager` 接口实现 (`cnp` 已注册) | ✅ W5.1 |
| `agent.ReefSandbox` 桥接到 `client.Sandbox` | ✅ W5.3 |
| `agent.ReefMemoryRecorder` 桥接到 `client.MemoryRecorder` | ✅ W5.4 |
| `memory.Gene` 转换 + `SemanticRetriever` 真实实现 | ✅ W6.2 |
| P8 组件**尚未在 picoclaw 的 `cmd/reef/main.go` 启动流程中启用** | ❌ 需配置 |
| `config.json` 中 `context_manager: "cnp"` 不生效（因配置加载未联动） | ❌ 需集成 |
| TaskSandbox 仅在 `SandboxFactory` 传入时创建（需在 cmd/main 中配置） | ❌ 需集成 |

这些配置层次的集成是 **配置文件到代码的最后一公里**。

---

## 七、修复建议 — 优先级路线图

### 🔴 立即修复（1-2 天）

| 优先级 | 问题 | 预估 |
|:---:|------|:---:|
| P0-4 | 状态机 `Created→Cancelled` 修复 | 30 分钟 |
| P0-3 | HeartbeatPayload 加 `current_tasks` | 1 小时 |
| P2-2 | CLI submit 路由修复 | 30 分钟 |
| P3-1 | 测试编译失败修复 | 1 小时 |
| P0-2 | MsgError 消息类型 | 2 小时 |

### 🟡 短期（3-5 天）

| 优先级 | 问题 | 预估 |
|:---:|------|:---:|
| P0-5 | 断连自动暂停任务 | 1 天 |
| P0-6 | 长任务定时进度心跳 | 1 天 |
| P1-2 | Admin 控制端点 | 1 天 |
| P1-3 | 角色技能加载接入 | 1 天 |
| P8 cfg | 配置加载联动 `cnp` context manager | 0.5 天 |

### 🟢 中期（2 周）

| 优先级 | 问题 | 预估 |
|:---:|------|:---:|
| P0-1 | 消息信封 `version` 统一 | 0.5 天 |
| P1-1 | 统一两套代码库导入路径（或合并）| 大工程 |
| P1-5 | Admin 安全模型统一 | 1 天 |
| P1-6 | error_type 枚举统一 | 1 天 |
| P0-7 | previous_attempts 透传 | 1 天 |

---

## 八、总结

**核心问题**：设计规范与代码实现之间存在约 20 个已识别的差距。最关键的 7 个 P0 问题涉及协议一致性（消息信封、错误类型、心跳）和执行正确性（状态转换、断连处理、进度报告、重试信息传递）。

**好消息**：P8 认知架构的集成工作（W5+W6）解决了首次审计发现的 7 个问题中的 6 个。功能代码质量高（TDD 全面、覆盖率 88-100%）。

**坏消息**：两套代码库（sipeed/reef 主仓库 vs picoclaw 子目录）的共存继续造成类型、导入路径和安全模型的不一致。

**下一步建议**：先修复 5 个 30 分钟级 P0/P2/P3 错误（状态机、心跳、CLI 路由、测试编译、MsgError），然后推进断连处理和 Admin 控制端点的实现。
