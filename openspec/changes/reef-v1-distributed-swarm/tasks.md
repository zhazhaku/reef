# Reef v1 实施任务清单

> 按 Phase 1-5 分组，每个任务包含：描述、复杂度(S/M/L)、依赖任务、验收标准。

---

## Phase 1: 协议与核心类型（Swarm Protocol & Core Types）

**目标**：建立 Server 与 Client 之间的通信契约，使双方可并行开发。

| ID | 任务描述 | 复杂度 | 依赖 | 验收标准 |
|----|---------|--------|------|----------|
| P1-1 | 创建 `pkg/reef/protocol.go`，定义所有 MessageType 枚举和 Message 顶层结构体 | S | 无 | 1. 包含全部 11 种消息类型常量定义；2. JSON marshal/unmarshal 单元测试通过；3. 未知 type 解析不 panic |
| P1-2 | 创建 `pkg/reef/protocol.go` 中所有 Payload 结构体（RegisterPayload、TaskDispatchPayload、TaskProgressPayload、TaskCompletedPayload、TaskFailedPayload 等） | M | P1-1 | 1. 所有 Payload 字段与 specs.md 一致；2. 每个 Payload 有 Validator 接口实现；3. 单元测试覆盖 round-trip marshal/unmarshal |
| P1-3 | 创建 `pkg/reef/task.go`，定义 TaskStatus 枚举、Task 结构体、状态转换规则 | M | 无 | 1. 9 种状态常量定义完整；2. `Task.CanTransition(to)` 方法实现状态矩阵；3. 无效状态转换返回错误；4. 单元测试覆盖所有有效和无效转换路径 |
| P1-4 | 创建 `pkg/reef/client.go`，定义 ClientCapability 和 ClientStatus 结构体 | S | 无 | 1. 包含 client_id、role、skills、capacity、status、last_heartbeat 字段；2. JSON 序列化单元测试通过 |
| P1-5 | 定义协议版本常量 `ReefProtocolV1` 和版本协商逻辑 | S | P1-1 | 1. `Message.Version` 固定为 `"reef-v1"`；2. 收到版本不匹配消息时返回 error；3. 单元测试验证版本校验逻辑 |
| P1-6 | 编写 `pkg/reef/` 包的整体单元测试，确保所有类型的序列化 round-trip 100% 通过 | M | P1-1~P1-5 | 1. 测试覆盖率 > 90%；2. CI 通过；3. 所有边界情况（空数组、nil、超长字符串）覆盖 |

**Phase 1 成功标准**：
- `pkg/reef/` 包编译通过，单元测试全部通过。
- 任意消息类型的 JSON 序列化/反序列化 round-trip 无损。
- 状态机转换规则经过单元测试验证。

---

## Phase 2: Server 实现（Reef Server）

**目标**：Server 可接受 Client 连接、维护注册表、调度任务、暴露 Admin 端点。

| ID | 任务描述 | 复杂度 | 依赖 | 验收标准 |
|----|---------|--------|------|----------|
| P2-1 | 创建 `cmd/reef/` 主入口，支持 `--mode=server` 参数解析和配置加载 | S | 无 | 1. `reef --mode=server` 启动不报错；2. `reef --mode=client` 启动不报错；3. 缺少 `--mode` 时默认 client 并打印警告 |
| P2-2 | 实现 `pkg/reef/server/server.go`：WebSocket acceptor、连接生命周期管理、Shutdown 优雅关闭 | M | P1-1, P1-4 | 1. 可接受 WebSocket 连接；2. 每个连接一个 goroutine；3. `Shutdown` 时关闭所有连接并等待 goroutine 退出；4. 单元测试模拟 10 个并发连接 |
| P2-3 | 实现 `pkg/reef/server/registry.go`：Client 注册表、心跳更新、超时剔除 | M | P2-2 | 1. `Register` 新 Client 成功；2. `Heartbeat` 更新时间戳；3. 后台 goroutine 每 10s 扫描并剔除超时 Client；4. `FindCandidates` 按角色和技能过滤；5. 并发安全测试通过 |
| P2-4 | 实现 `pkg/reef/server/scheduler.go`：任务调度器，支持角色+技能匹配和容量优先级 | M | P2-3, P1-3 | 1. `Schedule` 方法正确匹配候选 Client；2. 无候选时任务入队；3. Client 容量满时不被选中；4. 调度器单元测试覆盖各种边界（无候选、多候选、容量相同） |
| P2-5 | 实现 `pkg/reef/server/queue.go`：内存任务队列，支持 FIFO 和 FindMatch | S | P1-3 | 1. `Enqueue`/`Dequeue` O(1)；2. `FindMatch` 正确返回第一个匹配任务；3. 队列满时返回 `ErrQueueFull`；4. 并发安全测试通过 |
| P2-6 | 实现 `pkg/reef/server/admin.go`：HTTP `/admin/status` 和 `/admin/tasks` 端点 | M | P2-3, P2-5 | 1. `/admin/status` 返回正确 JSON（包含所有 Client 元数据）；2. `/admin/tasks` 返回正确 JSON（按状态分组）；3. 支持 `?client_id=` 过滤；4. 错误 token 返回 401 |
| P2-7 | 实现 `pkg/reef/server/escalation.go`：失败升级决策器 | M | P1-3, P2-4 | 1. `Reassign` 逻辑正确（排除已尝试 Client）；2. `Terminate` 逻辑正确；3. `EscalateToHuman` 逻辑正确；4. 决策结果写入 Task.EscalationDecision |
| P2-8 | 实现消息路由：Server 收到各类消息后正确分发到 registry/scheduler/task manager | M | P2-2~P2-7 | 1. register → registry；2. heartbeat → registry；3. task_progress → task manager；4. task_completed → scheduler（释放容量）+ task manager；5. task_failed → escalation handler |
| P2-9 | Server 集成测试：启动 Server → 模拟 Client 连接 → 注册 → 心跳 → dispatch 任务 → 完成 | L | P2-1~P2-8 | 1. 使用 mock WebSocket client；2. 全流程在 5 秒内完成；3. 断言每个阶段的 Server 状态正确；4. Admin 端点返回预期数据 |
| P2-10 | 结构化日志：Server 所有关键事件输出结构化 JSON 日志 | S | P2-1~P2-9 | 1. connect/register/dispatch/completed/failed/disconnect/heartbeat_timeout 事件均有 info 日志；2. 错误事件有 error 日志；3. 日志字段包含 event/timestamp/component |

**Phase 2 成功标准**：
- Server 可独立运行，接受 mock Client 连接并完成一次任务调度。
- Admin 端点返回正确的 JSON 数据。
- 集成测试通过。

---

## Phase 3: Client 与 SwarmChannel（Reef Client & SwarmChannel）

**目标**：Client 可连接 Server、接收任务、通过 AgentLoop 执行、报告进度、支持重连。

| ID | 任务描述 | 复杂度 | 依赖 | 验收标准 |
|----|---------|--------|------|----------|
| P3-1 | 实现 `pkg/reef/client/connector.go`：WebSocket 连接管理、指数退避重连、心跳发送 | M | P1-1, P1-4 | 1. 连接成功后发送 register；2. 收到 registered 后进入 ready；3. 断连后指数退避重连（初始 1s，最大 60s）；4. 每 30s 发送 heartbeat；5. 单元测试模拟断连和重连 |
| P3-2 | 实现 `pkg/reef/client/runner.go`：接收 task_dispatch、构造 InboundMessage、注入 AgentLoop | M | P3-1, P1-2 | 1. 收到 task_dispatch 后 PublishInbound；2. Metadata 包含 reef_task_id；3. AgentLoop 正确处理该消息；4. 单元测试验证 InboundMessage 构造正确 |
| P3-3 | 扩展 `pkg/agent/loop.go` 的 `processOptions`，新增 `TaskContext` 可选字段 | S | 无 | 1. `processOptions` 新增 `TaskContext *reef.TaskContext`（omitempty）；2. 现有代码无需修改即可编译；3. `runAgentLoop` 将 TaskContext 绑定到 turnState |
| P3-4 | 实现 ReefTaskHook：监听 EventKindTurnStart，将 TaskContext 注入到 turn 上下文 | M | P3-3 | 1. Hook 实现 `EventObserver` 接口；2. TurnStart 时将 task_id 和 cancelFunc 存入 turnState；3. Hook 注册到 HookManager；4. 单元测试验证注入成功 |
| P3-5 | 实现 `pkg/channels/swarm/swarm.go`：SwarmChannel 实现 Channel 接口 | M | P3-1, P3-2 | 1. `Name()` 返回 `"swarm"`；2. `Start` 启动 Connector；3. `Stop` 关闭 Connector；4. `Send` 拦截带有 reef_task_id 的 OutboundMessage 并上报；5. `IsAllowed` 始终返回 true |
| P3-6 | 实现 `pkg/reef/client/reporter.go`：进度/完成/失败报告 | S | P3-1, P1-2 | 1. `ReportProgress` 发送 task_progress；2. `ReportCompleted` 发送 task_completed；3. `ReportFailed` 发送 task_failed（含 attempt_history）；4. 单元测试验证消息格式正确 |
| P3-7 | 实现断连期间任务暂停逻辑：Connector 检测到断连时自动暂停在飞任务 | M | P3-1, P3-2 | 1. 断连时所有 Running 任务变为 Paused；2. 重连后等待 Server 发送 resume；3. 断连期间不发送失败报告；4. 单元测试模拟断连-重连-恢复流程 |
| P3-8 | Client 集成测试：启动 Client → 连接 mock Server → 接收任务 → 模拟 AgentLoop 执行 → 报告完成 | L | P3-1~P3-7 | 1. 使用 mock WebSocket server；2. 全流程在 5 秒内完成；3. 断言每个消息的类型和 payload 正确；4. 断言断连后任务状态为 Paused |
| P3-9 | 将 SwarmChannel 注册到 `channels.Manager`，支持与其他 Channel（如 Telegram）同时运行 | S | P3-5 | 1. Manager 初始化时识别 `channelsConfig.Swarm`；2. SwarmChannel 与其他 Channel 并行工作；3. 单元测试验证多 Channel 共存 |

**Phase 3 成功标准**：
- Client 可独立运行，连接 mock Server 并完成一次任务执行。
- 断连后任务状态为 Paused，重连后可恢复。
- SwarmChannel 符合 PicoClaw Channel 接口规范。

---

## Phase 4: 任务生命周期与失败处理（Task Lifecycle & Failure Handling）

**目标**：完整的 cancel/pause/resume/retry/escalate 功能。

| ID | 任务描述 | 复杂度 | 依赖 | 验收标准 |
|----|---------|--------|------|----------|
| P4-1 | 实现 LIFE-01：Server 发送 cancel，Client 调用 CancelFunc 中止任务 | M | P3-3, P3-4 | 1. Server `broadcast` 发送 task_cancel；2. Client Runner 查找 ActiveTask 并调用 CancelFunc；3. AgentLoop context 被取消，turn 中断；4. Client 返回 task_progress(cancelled)；5. 单元测试验证 cancel 信号在 1s 内生效 |
| P4-2 | 实现 LIFE-02/03：Server 发送 pause/resume，Client 阻塞/恢复 AgentLoop | M | P3-3, P3-4 | 1. pause 时阻塞 AgentLoop 下一步执行；2. resume 时解除阻塞；3. Client 返回对应 task_progress；4. 单元测试验证 pause 期间 AgentLoop 不推进 |
| P4-3 | 实现 RETRY-01：Client 本地重试机制，可恢复错误时指数退避重试 | M | P3-2, P3-6 | 1. 识别可恢复错误（LLM 5xx、工具超时）；2. attempt < max_retries 时退避重试（1s, 2s, 4s, 8s... 最大 30s）；3. 重试成功时正常报告 completed；4. 单元测试模拟 3 次重试后成功 |
| P4-4 | 实现 RETRY-02：Client 耗尽本地重试后构造 task_failed 消息（含 attempt_history） | M | P4-3, P1-2 | 1. task_failed 包含完整 attempt_history；2. 每条 attempt 记录包含 timestamp、error、duration_ms、client_id；3. logs 字段包含最近 N 条日志；4. 单元测试验证 payload 格式 |
| P4-5 | 实现 RETRY-03：Server Escalation Handler 的 Reassign 决策 | M | P2-7, P4-4 | 1. 收到 task_failed 后检查候选 Client；2. 存在未尝试候选时返回 Reassign；3. 任务状态回退到 Assigned 并重新 dispatch；4. previous_attempts 附加到新 task_dispatch |
| P4-6 | 实现 RETRY-03：Server Escalation Handler 的 Terminate 和 Human 决策 | M | P2-7, P4-4 | 1. 无候选或不可恢复错误时返回 Terminate；2. 配置 auto_escalate_to_human 时返回 Human；3. 任务状态正确迁移；4. Admin 端点显示 Escalated 任务 |
| P4-7 | 实现 CONN-03：Server 在心跳超时窗口内优雅处理 Client 重连 | M | P2-3, P3-1 | 1. 90s 内重连复用原注册表条目；2. 超过 90s 视为新 Client；3. 原有关联任务进入 Escalation；4. 单元测试模拟两种场景 |
| P4-8 | 任务状态机全路径单元测试：覆盖所有有效和无效状态转换 | M | P1-3, P4-1~P4-6 | 1. Created→Queued→Assigned→Running→Completed；2. Created→Queued→Assigned→Running→Paused→Running→Failed→Escalated→Reassign→Assigned→Running→Completed；3. 所有无效转换返回错误；4. 测试覆盖率 100% |
| P4-9 | E2E 生命周期测试：1 Server + 1 Client，测试 cancel/pause/resume/retry/escalate | L | P4-1~P4-8 | 1. 每个控制指令的端到端流程 < 5s；2. 断言每个阶段双方状态一致；3. 日志完整记录所有事件 |

**Phase 4 成功标准**：
- 所有任务生命周期控制指令（cancel/pause/resume）在 1s 内生效。
- 本地重试机制正确工作，耗尽后上报 Server。
- Escalation 决策覆盖 Reassign/Terminate/Human 三种路径。
- 断连重连逻辑经过边界条件测试。

---

## Phase 5: 角色技能与 E2E 集成（Role-based Skills & E2E Integration）

**目标**：Client 启动时加载角色特定技能，完整系统端到端验证。

| ID | 任务描述 | 复杂度 | 依赖 | 验收标准 |
|----|---------|--------|------|----------|
| P5-1 | 设计并实现角色技能 manifest 格式（`skills/roles/<role>.yaml`） | S | 无 | 1. YAML 包含 role、skills 数组、system_prompt_override；2. 提供 schema 验证；3. 提供默认模板（coder、analyst、tester） |
| P5-2 | 实现角色技能加载器：Client 启动时读取 manifest 并过滤 SkillsLoader | M | P5-1 | 1. 仅加载 manifest 中列出的技能；2. 缺失技能打印警告但不失败；3. `AgentInstance.SkillsFilter` 正确设置；4. 单元测试验证过滤逻辑 |
| P5-3 | 实现系统提示词覆盖：role 的 system_prompt_override 替换默认 system prompt | S | P5-2 | 1. ContextBuilder 使用覆盖值；2. AgentLoop 的 LLM 请求包含新 system prompt；3. 单元测试验证 prompt 内容 |
| P5-4 | Client 注册时仅通告已加载技能 | S | P5-2, P3-1 | 1. register.skills 仅包含实际加载的技能；2. 不包含未加载或不可用技能；3. 单元测试验证 |
| P5-5 | E2E 集成测试：1 Server + 2 不同角色 Client → 提交 2 个角色任务 → 验证正确执行 | L | P3-8, P4-9, P5-4 | 1. Server 启动；2. Client-A(coder) 和 Client-B(analyst) 启动并注册；3. 提交 coder 任务，验证 Client-A 执行；4. 提交 analyst 任务，验证 Client-B 执行；5. 验证结果正确返回；6. 全流程 < 30s |
| P5-6 | 编写 README 文档：部署指南、角色添加教程、配置参考 | M | P5-1~P5-5 | 1. 包含 Server 和 Client 的启动命令；2. 包含角色 manifest 编写示例；3. 包含常见问题排查；4. 包含架构 overview 图 |
| P5-7 | 性能基准测试：测量 Client 内存增量和 Server 调度延迟 | S | P5-5 | 1. Client 内存增量 < 5MB（相对于 standalone PicoClaw）；2. Server 调度延迟 P95 < 1s（100 任务、10 Client）；3. 报告写入 `docs/benchmark.md` |

**Phase 5 成功标准**：
- 两个不同角色的 Client 同时运行，各自加载不同技能。
- Server 正确将任务路由到匹配角色的 Client。
- E2E 测试全流程通过。
- README 文档完整，新用户可按文档部署。

---

## 跨 Phase 依赖图

```
P1-1 ──► P1-2 ──► P2-2 ──► P2-3 ──► P2-4 ──► P2-8 ──► P2-9
  │        │        │        │        │        │        │
  │        │        │        │        ▼        ▼        ▼
  │        │        │        │       P2-5    P2-7    P3-8
  │        │        │        │        │        │        │
  │        │        │        │        └────────┴────────┘
  │        │        │        │                 │
  │        │        │        ▼                 ▼
  │        │        │       P3-1 ──► P3-2 ──► P3-8
  │        │        │        │        │        │
  │        │        │        │        ▼        ▼
  │        │        │        │       P3-3 ──► P4-1
  │        │        │        │        │        │
  │        │        │        │        ▼        ▼
  │        │        │        │       P3-4 ──► P4-2
  │        │        │        │        │        │
  │        │        │        │        ▼        ▼
  │        │        │        │       P3-5 ──► P4-3 ──► P4-4 ──► P4-5 ──► P4-6
  │        │        │        │        │        │        │        │        │
  │        │        │        │        ▼        └────────┴────────┴────────┘
  │        │        │        │       P3-6                      │
  │        │        │        │        │                        ▼
  │        │        │        │        └─────────────────────► P4-9
  │        │        │        │                                 │
  │        │        │        │       P3-7 ───────────────────► P4-7
  │        │        │        │                                 │
  │        │        │        │                                P4-8
  │        │        │        │                                 │
  │        │        │        └────────────────────────────────► P5-5
  │        │        │                                          │
  │        │        └─────────────────────────────────────────► P5-4
  │        │                                                   │
  │        └─────────────────────────────────────────────────► P5-2
  │                                                            │
  └─────────────────────────────────────────────────────────► P5-1
                                                               │
                                                              P5-3
                                                               │
                                                              P5-6
                                                               │
                                                              P5-7
```

---

## 附录：复杂度定义

| 复杂度 | 估算工时 | 说明 |
|--------|----------|------|
| S (Small) | 2-4 小时 | 独立函数或简单结构体，单元测试覆盖即可 |
| M (Medium) | 1-2 天 | 涉及多个结构体协作，需要 mock 和集成测试 |
| L (Large) | 3-5 天 | 跨包协作，需要完整的 E2E 测试和调试 |
