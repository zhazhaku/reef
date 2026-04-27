# Reef v1 实施任务清单

> 按 Phase 1-5 分组，每个任务包含描述、复杂度(S/M/L)、依赖任务、验收标准。
> 复杂度定义：S=≤4h, M=≤2天, L=≤1周（单人估算）。

---

## Phase 1: Swarm Protocol & Core Types（协议与核心类型）

**目标：** 建立 Server/Client 通信契约，产出可直接编译的 Go 类型定义。

| # | 任务 | 描述 | 复杂度 | 依赖 | 验收标准 |
|---|------|------|--------|------|---------|
| 1.1 | 定义消息协议常量 | 在 `pkg/reef/protocol.go` 中定义 `MessageType` 枚举、`ProtocolVersion`、通用 `Message` 结构体（含 `json.RawMessage` payload） | S | 无 | 所有消息类型可 JSON 序列化/反序列化，单元测试覆盖 round-trip |
| 1.2 | 定义消息 Payload 结构体 | 为每个消息类型定义独立 Payload：`RegisterPayload`、`RegisterAckPayload`、`TaskDispatchPayload`、`TaskProgressPayload`、`TaskCompletedPayload`、`TaskFailedPayload`、`ControlPayload` | S | 1.1 | 每个 Payload 结构体包含完整字段，marshal/unmarshal 测试通过 |
| 1.3 | 定义任务状态机 | 在 `pkg/reef/task.go` 中定义 `TaskStatus` 枚举、`Task` 领域对象、`TaskResult`、`TaskError`、`AttemptRecord` | S | 无 | 状态转换守卫条件通过单元测试验证 |
| 1.4 | 定义 TaskContext | 在 `pkg/reef/task.go` 中定义 `TaskContext` 结构体，包含 `TaskID`、`CancelFunc`、`PauseCh`、`ResumeCh` | S | 1.3 | `TaskContext` 可通过 `context.WithValue` 嵌入 context |
| 1.5 | 定义 Client 能力模型 | 在 `pkg/reef/client_info.go` 中定义 `ClientInfo`、`ClientState` 枚举 | S | 无 | 结构体字段覆盖注册表所需全部信息 |
| 1.6 | 协议版本校验 | 实现 `register` 消息中的 `protocol_version` 校验逻辑，不兼容版本返回 `register_nack` | S | 1.1, 1.2 | 单元测试：兼容版本通过，不兼容版本拒绝 |

**Phase 1 出口标准：** `pkg/reef/` 包可编译，所有类型通过单元测试，消息序列化 round-trip 100% 覆盖。

---

## Phase 2: Reef Server（Server 实现）

**目标：** Server 可接受 Client 连接、维护注册表、调度任务、暴露 Admin 端点。

| # | 任务 | 描述 | 复杂度 | 依赖 | 验收标准 |
|---|------|------|--------|------|---------|
| 2.1 | Client 注册表实现 | 在 `pkg/reef/server/registry.go` 中实现线程安全的 `Registry`，支持 Register、Unregister、UpdateHeartbeat、GetByID、ListByRole、MarkStale | M | 1.5 | 并发读写测试通过（`go test -race`），O(1) ID 查询 |
| 2.2 | 心跳扫描器 | 实现独立 goroutine，每 5 秒扫描注册表，将超时 Client 标记为 stale，暂停其 in-flight 任务 | M | 2.1 | 模拟心跳超时场景，验证 stale 标记和任务暂停 |
| 2.3 | 任务调度器 | 在 `pkg/reef/server/scheduler.go` 中实现 `Scheduler`，包含 `Schedule()` 匹配算法、`dispatch()` 下发、`onClientAvailable()` 触发重调度 | M | 2.1 | 单元测试：4 个 Client（2 coder, 2 analyst），任务按角色+技能+负载匹配到正确 Client |
| 2.4 | 内存任务队列 | 在 `pkg/reef/server/queue.go` 中实现 FIFO `TaskQueue`，支持最大长度限制、超时过期检测 | S | 1.3 | 队列满时返回错误，FIFO 顺序验证，超时任务触发 Escalation |
| 2.5 | WebSocket Acceptor | 在 `pkg/reef/server/websocket.go` 中实现 `Acceptor`，处理连接升级、token 校验、per-connection reader/writer goroutine | M | 2.1, 2.3 | 集成测试：mock Client 连接 → register → heartbeat → 保持连接 |
| 2.6 | HTTP Admin 端点 | 实现 `/admin/status` 和 `/admin/tasks` HTTP handler，返回 JSON | M | 2.1, 2.3, 2.4 | `curl` 测试返回正确结构和数据，响应时间 < 100ms |
| 2.7 | Server 主入口 | 在 `cmd/reef/main.go` 中实现 `runServer()`，加载配置、初始化所有组件、启动 HTTP 和 WebSocket 服务 | M | 2.1~2.6 | `go run cmd/reef/main.go --mode=server` 可启动，无 panic |
| 2.8 | Server 集成测试 | 编写 `pkg/reef/server/server_test.go`，启动完整 Server，mock Client 完成注册→心跳→任务下发→完成全链路 | L | 2.1~2.7 | 测试通过，覆盖注册、调度、心跳超时、任务完成 |

**Phase 2 出口标准：** Server 可独立运行，接受 mock Client 连接并完成至少一个任务的调度-执行-完成闭环。

---

## Phase 3: Reef Client & SwarmChannel（Client 与通道实现）

**目标：** Client 可连接 Server、接收任务、注入 AgentLoop、报告进度、断线重连。

| # | 任务 | 描述 | 复杂度 | 依赖 | 验收标准 |
|---|------|------|--------|------|---------|
| 3.1 | WebSocket 连接器 | 在 `pkg/reef/client/connector.go` 中实现 `Connector`，支持 Connect、Send、自动重连（指数退避）、reader/writer goroutine | M | 1.1, 1.2 | 模拟断线 3 次，验证退避间隔 1s→2s→4s，最终恢复 |
| 3.2 | 注册与心跳 | Client 连接成功后自动发送 `register`，成功后启动心跳 goroutine | S | 3.1 | 抓包/日志验证 register + 周期性 heartbeat 消息 |
| 3.3 | SwarmChannel 实现 | 在 `pkg/channels/swarm/` 中实现 `SwarmChannel`，满足 PicoClaw `Channel` 接口（Start/Stop/Send/Receive） | M | 3.1 | SwarmChannel 可被 PicoClaw AgentLoop 正常初始化使用 |
| 3.4 | 任务接收与注入 | `task_dispatch` 消息 → 构造 `bus.Message{Type: Inbound}` → 发布到 MessageBus | M | 3.3 | AgentLoop 消费到正确的 inbound 消息，消息内容完整 |
| 3.5 | TaskContext 注入 Hook | 扩展 `pkg/agent` 的 `processOptions`，支持注入 `TaskContext`；Client 在任务执行前设置 cancel/pause/resume 通道 | M | 1.4, 3.4 | AgentLoop 执行时可访问 TaskContext，cancel 可终止执行 |
| 3.6 | 进度报告 | AgentLoop 执行过程中，Client 定期发送 `task_progress`；完成后发送 `task_completed`，失败后发送 `task_failed` | M | 3.4 | Server 收到正确的 progress/complete/failed 消息，状态更新正确 |
| 3.7 | 断线任务暂停 | Client 检测到断线时，将 in-flight 任务置为本地 Paused，停止进度报告；重连后恢复 | M | 3.1, 3.6 | 模拟断线 10 秒，验证任务未失败，重连后恢复报告 |
| 3.8 | Client 主入口 | 在 `cmd/reef/main.go` 中实现 `runClient()`，加载配置、初始化 AgentLoop、SwarmChannel、启动连接 | M | 3.1~3.7 | `go run cmd/reef/main.go --mode=client` 可启动，成功注册到 Server |
| 3.9 | Client 集成测试 | 编写 `pkg/reef/client/client_test.go`，mock Server，验证连接→注册→接收任务→报告完成全链路 | L | 3.1~3.8 | 测试通过，覆盖正常流程和断线重连场景 |

**Phase 3 出口标准：** Client 可独立运行，成功注册到 mock Server，接收并"执行"任务（模拟执行，无真实 LLM 调用），报告完成。

---

## Phase 4: Task Lifecycle & Failure Handling（生命周期与失败处理）

**目标：** 完整的任务控制（取消/暂停/恢复）和失败处理（重试、上报、Escalation）。

| # | 任务 | 描述 | 复杂度 | 依赖 | 验收标准 |
|---|------|------|--------|------|---------|
| 4.1 | Cancel 实现 | Server 发送 `cancel` → Client 调用 `CancelFunc()` → AgentLoop 响应 context 取消 → 发送 `control_ack` | M | 3.5 | AgentLoop 在 5 秒内终止，Server 状态更新为 Cancelled |
| 4.2 | Pause 实现 | Server 发送 `pause` → Client 通过 `PauseCh` 阻塞 AgentLoop → 发送 `task_progress(paused)` | M | 3.5 | AgentLoop 在可中断点进入阻塞，进度报告状态为 paused |
| 4.3 | Resume 实现 | Server 发送 `resume` → Client 通过 `ResumeCh` 解除阻塞 → AgentLoop 继续 → 发送 `task_progress(running)` | M | 4.2 | Resume 幂等验证，重复发送不导致异常 |
| 4.4 | 本地重试机制 | Client 执行失败时（非 cancel/escalated），自动重试最多 `max_retries` 次，指数退避 | M | 3.6 | 模拟 LLM 超时失败，验证重试 3 次后上报 escalated |
| 4.5 | Escalation 上报 | 重试耗尽后，Client 发送 `task_failed(error_type=escalated)`，附带完整 `attempt_history` | S | 4.4 | Server 收到包含 4 次尝试记录的完整 history |
| 4.6 | Server Escalation 决策 | 实现 `EscalationHandler`，决策逻辑：Reassign / Terminate / Escalate to Admin | M | 4.5 | 单元测试覆盖三种决策路径，验证状态正确转换 |
| 4.7 | 断线期间控制消息缓冲 | Server 在 Client 断线期间缓存 pending 的 cancel/pause/resume，重连后重发 | M | 2.5, 3.7 | 模拟断线期间发送 cancel，验证重连后 Client 收到并执行 |
| 4.8 | 生命周期集成测试 | 编写完整测试：任务运行中 → pause → resume → cancel，以及 失败→重试→escalation→reassign | L | 4.1~4.7 | 所有状态转换路径测试通过 |

**Phase 4 出口标准：** Server 和 Client 可完成 cancel/pause/resume 控制流，失败任务经本地重试后正确触发 Server Escalation 决策。

---

## Phase 5: Role-based Skills & E2E Integration（角色技能与端到端集成）

**目标：** Client 按角色加载技能，完整系统端到端验证。

| # | 任务 | 描述 | 复杂度 | 依赖 | 验收标准 |
|---|------|------|--------|------|---------|
| 5.1 | 角色技能清单格式 | 定义 `skills/roles/<role>.yaml` 格式（skills 数组 + system_prompt 可选） | S | 无 | YAML 解析正确，缺失文件时 Client 报错退出 |
| 5.2 | 角色技能加载器 | Client 启动时根据 `reef.role` 读取清单，仅加载指定技能到 Skill Registry | M | 5.1 | 加载后内存中仅包含清单技能，其他技能未加载 |
| 5.3 | 系统提示词覆盖 | 角色配置中的 `system_prompt` 覆盖 AgentLoop 默认系统提示词 | S | 5.2 | 验证 AgentLoop 初始化时使用了角色提示词 |
| 5.4 | 注册时技能广告 | Client `register` 消息的 `skills` 字段仅包含成功加载的技能 | S | 5.2 | Server 注册表中技能列表与清单一致 |
| 5.5 | 端到端集成测试 | 启动 Server + 2 个 Client（coder / analyst），提交需要 coder 的任务，验证调度到正确 Client 并返回结果 | L | 2.8, 3.9, 5.4 | 测试自动化运行，输出通过/失败报告 |
| 5.6 | 角色添加文档 | 在 README 中撰写"如何添加新角色"指南，包含清单文件格式、配置方法、验证步骤 | S | 5.1 | 文档清晰，新人可按步骤 10 分钟内添加新角色 |
| 5.7 | 部署指南 | 在 README 中撰写 Server 和 Client 的部署指南（编译、配置、启动、验证） | S | 2.7, 3.8 | 包含配置示例和故障排查 checklist |

**Phase 5 出口标准：** E2E 测试通过，README 包含完整部署和角色扩展指南。

---

## 汇总统计

| Phase | 任务数 | 预估总工时 | 关键路径 |
|-------|--------|-----------|---------|
| 1 | 6 | 1 天 | 1.1 → 1.2 → 1.3 |
| 2 | 8 | 5 天 | 2.1 → 2.3 → 2.5 → 2.7 → 2.8 |
| 3 | 9 | 6 天 | 3.1 → 3.3 → 3.4 → 3.5 → 3.8 → 3.9 |
| 4 | 8 | 5 天 | 3.5 → 4.1/4.2 → 4.4 → 4.6 → 4.8 |
| 5 | 7 | 3 天 | 5.2 → 5.4 → 5.5 |
| **合计** | **38** | **~20 天** | — |

> 注：Phase 2 和 Phase 3 可并行开发（协议定义完成后，两边独立实现）。
> 关键路径约为 **14 天**（Phase 1 → Phase 2 核心 → Phase 3 核心 → Phase 4 依赖 Phase 3 → Phase 5 E2E）。

---

## 优先级建议（若需裁剪）

**MVP（最小可行产品）：** 1.1~1.3, 2.1, 2.3, 2.5, 2.7, 3.1, 3.3, 3.4, 3.6, 3.8, 5.5
- 可工作的 Server-Client 通信
- 任务调度+执行+完成
- 基础 E2E 验证

**v1 完整：** 全部 38 项

---

*任务清单版本：v1.0*  
*日期：2026-04-27*  
*作者：Reef Research & Design Agent*
