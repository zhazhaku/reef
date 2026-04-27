# Reef v1 详细规格说明书

> 本文档使用 [RFC 2119](https://tools.ietf.org/html/rfc2119) 关键词：MUST（必须）、SHALL（应当）、SHOULD（建议）、MAY（可以）。  
> 每个需求以 **Given/When/Then** 场景化描述，并标注对应的需求标签和 Roadmap Phase。

---

## 目录

1. [Swarm Protocol（集群协议）](#1-swarm-protocol集群协议) — SWARM-01 ~ SWARM-04
2. [Task Scheduling（任务调度）](#2-task-scheduling任务调度) — SCHED-01 ~ SCHED-03
3. [Task Execution（任务执行）](#3-task-execution任务执行) — TASK-01 ~ TASK-04
4. [Task Lifecycle Control（生命周期控制）](#4-task-lifecycle-control生命周期控制) — LIFE-01 ~ LIFE-04
5. [Failure Handling（失败处理）](#5-failure-handling失败处理) — RETRY-01 ~ RETRY-03
6. [Connection Resilience（连接韧性）](#6-connection-resilience连接韧性) — CONN-01 ~ CONN-03
7. [Role-based Skills（角色化技能）](#7-role-based-skills角色化技能) — ROLE-01 ~ ROLE-03
8. [Admin & Observability（管理与可观测）](#8-admin--observability管理与可观测) — ADMIN-01 ~ ADMIN-03

---

## 1. Swarm Protocol（集群协议）

**对应 Phase：** Phase 1（协议与核心类型）

### SWARM-01 Server 维护 Client 实时注册表

> **Given** Reef Server 已启动并监听 WebSocket 端口  
> **When** 一个或多个 Reef Client 通过 WebSocket 连接到 Server 并发送 `register` 消息  
> **Then** Server MUST 维护一个线程安全的注册表，记录每个 Client 的以下信息：
> - `client_id`（唯一标识，UUID 或连接级标识）
> - `role`（角色名称，如 "coder"）
> - `skills`（已加载的技能名称列表）
> - `providers`（可用 LLM Provider 列表）
> - `capacity`（最大并发任务数，≥1）
> - `current_load`（当前执行任务数）
> - `last_heartbeat`（上次心跳时间戳，Unix 毫秒）
> - `connection_state`（connected / disconnected / stale）

**验收标准：**
- 注册表操作 MUST 是线程安全的（支持并发读写）。
- Server MUST 在收到 `register` 消息后 100ms 内完成注册表更新。
- 注册表信息 MUST 可通过 HTTP Admin `/admin/status` 端点查询。

---

### SWARM-02 Client 启动时向 Server 注册

> **Given** Reef Client 已启动，配置文件中指定了 `server_url`、`role`、`reef_token`  
> **When** Client 成功建立 WebSocket 连接后  
> **Then** Client MUST 在 1 秒内发送 `register` 消息，消息体 SHALL 包含：
> - `msg_type: "register"`
> - `protocol_version: "reef-v1"`
> - `role`（来自配置文件）
> - `skills`（实际加载的技能名称数组）
> - `providers`（配置文件中的 provider 列表）
> - `capacity`（配置项 `max_concurrent_tasks`，默认 1）
> - `timestamp`（发送时的 Unix 毫秒时间戳）

**验收标准：**
- 若 Server 在 5 秒内未响应 `register_ack`，Client SHOULD 关闭连接并重连。
- 若注册被 Server 拒绝（如 token 无效），Server MUST 发送 `register_nack` 并关闭连接。
- Client MUST 在注册成功后才开始发送心跳。

---

### SWARM-03 Client 周期性心跳与 Server 超时驱逐

> **Given** Client 已完成注册并与 Server 保持 WebSocket 连接  
> **When** 每经过 `heartbeat_interval`（默认 10 秒，可配置）  
> **Then** Client MUST 发送 `heartbeat` 消息，Server MUST 更新该 Client 的 `last_heartbeat`。

> **Given** 某个 Client 的 `last_heartbeat` 距离当前时间超过 `heartbeat_timeout`（默认 30 秒）  
> **When** Server 的心跳检查 goroutine 执行周期性扫描  
> **Then** Server MUST 将该 Client 标记为 `stale`，并执行以下操作：
> - 若该 Client 有 in-flight 任务，SHALL 将这些任务状态置为 `Paused`
> - 将该 Client 从可调度池中移除
> - 记录结构化日志：`client_marked_stale`

**验收标准：**
- 心跳消息 MUST 仅包含 `msg_type: "heartbeat"` 和 `timestamp`。
- Server 的心跳扫描间隔 SHOULD 为 5 秒。
- 被标记为 stale 的 Client 若在 `reconnect_window`（默认 60 秒）内重连，Server MUST 恢复其注册表条目并恢复 Paused 任务。
- 超过 `reconnect_window` 未重连，Server MAY 将任务标记为 Failed 并触发 Escalation。

---

### SWARM-04 WebSocket 消息协议支持完整消息类型

> **Given** Server 与 Client 已建立 WebSocket 连接  
> **When** 任意一方需要发送信息  
> **Then** 所有消息 MUST 采用统一的 JSON 格式，包含以下字段：
> - `msg_type`（字符串，枚举值见下）
> - `payload`（对象，根据 msg_type 变化）
> - `task_id`（字符串，可选，关联到特定任务的消息必须携带）
> - `timestamp`（Unix 毫秒）

**v1 消息类型枚举：**

| msg_type | 发送方 | 说明 |
|---------|--------|------|
| `register` | Client | 初始注册 |
| `register_ack` | Server | 注册确认 |
| `register_nack` | Server | 注册拒绝（含 reason） |
| `heartbeat` | Client | 周期性心跳 |
| `task_dispatch` | Server | 任务下发 |
| `task_progress` | Client | 进度报告 |
| `task_completed` | Client | 任务完成 |
| `task_failed` | Client | 任务失败 |
| `cancel` | Server | 取消任务 |
| `pause` | Server | 暂停任务 |
| `resume` | Server | 恢复任务 |
| `control_ack` | Client | 控制指令确认 |

**验收标准：**
- 未知 `msg_type` MUST 被接收方忽略并记录警告日志。
- 所有消息 MUST 支持 JSON 序列化/反序列化的双向 round-trip。
- 协议版本 `reef-v1` MUST 在 `register` 消息中声明，Server MUST 拒绝不兼容版本。

---

## 2. Task Scheduling（任务调度）

**对应 Phase：** Phase 2（Reef Server）

### SCHED-01 任务匹配最佳 Client

> **Given** Server 收到一个新任务提交（通过 API 或内部触发），任务包含 `required_role` 和 `required_skills`  
> **When** Server 的调度器执行匹配  
> **Then** Server MUST 按以下优先级选择 Client：
> 1. **角色匹配**：Client 的 `role` 与 `required_role` 完全一致（大小写敏感）
> 2. **技能覆盖**：Client 的 `skills` 必须包含所有 `required_skills`
> 3. **容量可用**：Client 的 `current_load < capacity`
> 4. **负载均衡**：在满足以上条件的 Client 中，选择 `current_load` 最小者

> **Given** 没有任何 Client 满足匹配条件  
> **When** 调度器执行匹配  
> **Then** 任务 MUST 进入等待队列（Queue），并在有 Client 注册或负载释放时重新触发调度。

**验收标准：**
- 匹配算法 MUST 在 O(n) 时间内完成（n = 注册 Client 数）。
- 调度决策 MUST 记录结构化日志，包含 `task_id`、`selected_client_id`、`match_score`。
- 若 `required_skills` 为空数组，SHOULD 跳过技能覆盖检查（仅按角色匹配）。

---

### SCHED-02 Server 向 Client 下发任务

> **Given** 调度器已选定目标 Client  
> **When** Server 发送任务  
> **Then** Server MUST 通过 WebSocket 发送 `task_dispatch` 消息，payload MUST 包含：
> - `task_id`（UUID，全局唯一）
> - `instruction`（任务指令文本）
> - `context`（上下文对象，可选，包含历史消息、文件引用等）
> - `required_role`（要求角色）
> - `required_skills`（要求技能列表）
> - `max_retries`（Client 本地最大重试次数，默认 3）
> - `timeout_ms`（任务执行超时，默认 300000 = 5 分钟）
> - `created_at`（任务创建时间戳）

> **Given** Client 成功收到 `task_dispatch` 消息  
> **When** Client 解析消息并将任务注入 AgentLoop  
> **Then** Client MUST 向 Server 回复 `task_progress`（`status: "started"`），Server MUST 将任务状态更新为 `Running`。

**验收标准：**
- 若发送 `task_dispatch` 时 WebSocket 连接已断开，Server MUST 将该任务回退到队列头部，等待该 Client 重连或其他 Client 可用。
- 任务下发后 10 秒内未收到 Client 的 `started` 进度，Server SHOULD 标记该 Client 为疑似不可用，并尝试重分配。

---

### SCHED-03 无可用 Client 时的任务队列

> **Given** 新任务提交但无匹配的可用 Client  
> **When** 调度器执行匹配  
> **Then** 任务 MUST 进入内存队列，队列实现 MUST 支持 FIFO 优先级。

> **Given** 队列中有等待任务  
> **When** 新 Client 注册，或现有 Client 完成任务释放容量  
> **Then** Server MUST 自动触发重新调度，按 FIFO 顺序尝试为队首任务匹配 Client。

**验收标准：**
- 队列 MUST 支持最大长度限制（默认 1000），超限后新任务 MUST 被拒绝并返回错误。
- 队列中的任务 MUST 在 Admin `/admin/tasks` 端点可见，状态为 `Queued`。
- 任务在队列中的等待时间超过 `queue_timeout`（默认 10 分钟）SHOULD 被标记为 `Expired` 并触发 Escalation。

---

## 3. Task Execution（任务执行）

**对应 Phase：** Phase 3（Reef Client & SwarmChannel）

### TASK-01 Client 将任务注入 PicoClaw AgentLoop

> **Given** Client 收到 `task_dispatch` 消息  
> **When** Client 准备执行任务  
> **Then** Client MUST 构造一个 `Message` 对象（复用 PicoClaw `pkg/bus` 类型），将其发布到 MessageBus 作为 **Inbound** 消息，并由 AgentLoop 消费。

> **Given** AgentLoop 正在处理该任务  
> **When** AgentLoop 的 `processOptions` 执行  
> **Then** Client MUST 通过扩展的 hook 将 `TaskContext` 注入执行上下文，`TaskContext` MUST 包含：
> - `task_id`
> - `cancel_func`（context.CancelFunc）
> - `pause_ch`（chan struct{}，暂停信号通道）
> - `resume_ch`（chan struct{}，恢复信号通道）

**验收标准：**
- AgentLoop 的修改 MUST 是最小侵入性的：通过 `processOptions` 的扩展字段注入，不修改核心循环逻辑。
- `TaskContext` MUST 在整个 AgentLoop 执行链路中可访问（通过 context.WithValue）。
- 任务执行完成后，AgentLoop MUST 调用回调通知 Reef Client 层。

---

### TASK-02 Client 向 Server 报告进度

> **Given** AgentLoop 正在执行任务  
> **When** 任务状态发生变化或达到进度报告间隔  
> **Then** Client MUST 发送 `task_progress` 消息，payload MUST 包含：
> - `task_id`
> - `status`（枚举：`started`、`running`、`paused`）
> - `progress_percent`（0-100，可选，仅在 `running` 时提供）
> - `message`（状态描述文本，可选）
> - `timestamp`

**验收标准：**
- 进度报告间隔 MUST 可配置（默认 5 秒），最小间隔为 1 秒。
- 仅在 `status` 发生变化或 `progress_percent` 变化 ≥10% 时 SHOULD 发送，减少网络开销。
- Server 收到 `task_progress` 后 MUST 更新任务状态，并记录到 Admin 可见状态。

---

### TASK-03 Client 报告任务完成

> **Given** AgentLoop 成功完成任务执行  
> **When** 任务结果可用  
> **Then** Client MUST 发送 `task_completed` 消息，payload MUST 包含：
> - `task_id`
> - `result`（结果对象，包含 text、files、metadata 等）
> - `execution_time_ms`（实际执行耗时）
> - `timestamp`

> **Given** Server 收到 `task_completed` 消息  
> **When** 验证消息完整性  
> **Then** Server MUST 将任务状态更新为 `Completed`，释放 Client 容量，并触发下游回调（如有）。

**验收标准：**
- 结果对象大小超过 64KB 时，SHOULD 采用分片或引用外部存储（v1 暂不实现，直接发送）。
- 任务完成后，相关日志和临时文件 SHOULD 在 Client 本地保留 24 小时后清理。

---

### TASK-04 Client 报告任务失败

> **Given** AgentLoop 执行任务时遇到不可恢复错误  
> **When** 本地重试次数已耗尽  
> **Then** Client MUST 发送 `task_failed` 消息，payload MUST 包含：
> - `task_id`
> - `error_type`（枚举：`execution_error`、`timeout`、`cancelled`、`escalated`）
> - `error_message`（人类可读的错误描述）
> - `error_detail`（详细错误信息，如堆栈、日志片段）
> - `attempt_history`（每次重试的时间戳和结果数组）
> - `timestamp`

> **Given** Server 收到 `task_failed` 消息  
> **When** 处理失败上报  
> **Then** Server MUST 调用 Escalation Handler 决策下一步动作。

**验收标准：**
- 错误信息 MUST 经过脱敏处理，不得包含 API Key、Token 等敏感字段。
- `attempt_history` MUST 包含所有尝试的时间戳、成功/失败状态和简要结果。

---

## 4. Task Lifecycle Control（生命周期控制）

**对应 Phase：** Phase 4（任务生命周期与失败处理）

### LIFE-01 Server 发送取消信号

> **Given** 一个任务处于 `Running` 状态  
> **When** Admin 或上层系统请求取消该任务  
> **Then** Server MUST 向执行该任务的 Client 发送 `cancel` 控制消息（携带 `task_id`）。

> **Given** Client 收到 `cancel` 消息  
> **When** 任务正在 AgentLoop 中执行  
> **Then** Client MUST 调用 `TaskContext.cancel_func()`，触发 `context.Cancel`，AgentLoop 收到取消信号后 MUST 在 5 秒内终止执行。

> **Given** Client 成功取消任务  
> **When** 取消操作完成  
> **Then** Client MUST 发送 `control_ack` 消息（`control_type: "cancel"`, `task_id`），Server MUST 将任务状态更新为 `Failed`（子状态 `cancelled`），释放 Client 容量。

**验收标准：**
- 取消信号 MUST 在 100ms 内从 Server 到达 Client（局域网假设）。
- AgentLoop 对 context 取消 MUST 是响应式的：每个工具调用前检查 `ctx.Err()`。
- 若 Client 已断开连接，Server MUST 在 Client 重连时重新发送 pending 的 `cancel` 消息。

---

### LIFE-02 Server 发送暂停信号

> **Given** 一个任务处于 `Running` 状态  
> **When** Admin 或上层系统请求暂停该任务  
> **Then** Server MUST 向执行 Client 发送 `pause` 控制消息。

> **Given** Client 收到 `pause` 消息  
> **When** 任务正在执行  
> **Then** Client MUST 通过 `TaskContext.pause_ch` 向 AgentLoop 发送暂停信号，AgentLoop MUST 在下一个可中断点阻塞等待恢复。

> **Given** AgentLoop 已进入暂停状态  
> **When** 暂停生效  
> **Then** Client MUST 发送 `task_progress`（`status: "paused"`），Server MUST 将任务状态更新为 `Paused`。

**验收标准：**
- 暂停操作 SHOULD 在 1 秒内生效。
- 暂停期间，Client 的 `current_load` 仍计为该任务（占用 capacity）。
- 暂停超 30 分钟 SHOULD 触发 Server 侧的 Escalation（人工介入提示）。

---

### LIFE-03 Server 发送恢复信号

> **Given** 一个任务处于 `Paused` 状态  
> **When** Admin 或上层系统请求恢复该任务  
> **Then** Server MUST 向执行 Client 发送 `resume` 控制消息。

> **Given** Client 收到 `resume` 消息  
> **When** 任务处于暂停阻塞状态  
> **Then** Client MUST 通过 `TaskContext.resume_ch` 发送恢复信号，AgentLoop MUST 解除阻塞继续执行。

> **Given** AgentLoop 已恢复执行  
> **When** 恢复生效  
> **Then** Client MUST 发送 `task_progress`（`status: "running"`），Server MUST 将任务状态更新为 `Running`。

**验收标准：**
- 恢复操作 MUST 幂等：多次发送 `resume` 不导致异常。
- 若 Client 在暂停期间断开并重连，Server MUST 在确认连接恢复后自动重新发送 `resume`。

---

### LIFE-04 TaskContext 集成

> **Given** AgentLoop 初始化任务执行  
> **When** `processOptions` 被调用  
> **Then** 扩展后的 `processOptions` MUST 接受 `TaskContext` 参数，并将其嵌入 `context.Context`（通过 `context.WithValue`）。

> **Given** AgentLoop 内部执行工具调用或 LLM 调用  
> **When** 需要检查任务是否被取消或暂停  
> **Then** AgentLoop 及所有工具 MUST 能够通过 `ctx.Value(TaskContextKey)` 获取 `TaskContext`，并检查 `ctx.Err()` 和暂停通道状态。

**验收标准：**
- `TaskContext` 的定义 MUST 位于 `pkg/reef/task.go`。
- 不使用全局变量传递 TaskContext；所有状态 MUST 通过 context 传播。
- 对现有 AgentLoop 的修改 MUST 保持向后兼容：当非 Reef 模式运行时（无 TaskContext），AgentLoop 正常执行。

---

## 5. Failure Handling（失败处理）

**对应 Phase：** Phase 4（任务生命周期与失败处理）

### RETRY-01 Client 本地重试

> **Given** AgentLoop 执行任务时遇到可恢复错误（如 LLM 超时、网络抖动）  
> **When** 错误首次发生  
> **Then** Client MUST 自动重试执行，重试次数上限为 `task.max_retries`（来自 `task_dispatch`）。

> **Given** 重试正在进行  
> **When** 每次重试前  
> **Then** Client MUST 使用指数退避延迟：`delay = base_delay * 2^attempt`，其中 `base_delay` 默认 1 秒，最大上限 30 秒。

**验收标准：**
- 重试 MUST 仅针对 `execution_error` 和 `timeout` 类型错误；`cancelled` 和 `escalated` 错误 MUST NOT 重试。
- 每次重试 MUST 记录结构化日志，包含 `attempt_number`、`delay_ms`、`error`。
- 重试期间，任务状态在 Server 侧保持 `Running`；Client 的进度报告 SHOULD 包含 `retrying` 子状态。

---

### RETRY-02 耗尽本地重试后上报 Server

> **Given** Client 已连续失败 `max_retries + 1` 次（初始尝试 + max_retries 次重试）  
> **When** 最后一次重试失败  
> **Then** Client MUST 发送 `task_failed` 消息（TASK-04），其中 `error_type` 为 `escalated`，并附带完整的 `attempt_history`。

**验收标准：**
- `attempt_history` MUST 按时间顺序包含所有尝试的：
>   - `attempt_number`
>   - `started_at`
>   - `ended_at`
>   - `status`（success / failed）
>   - `error_message`（失败时）
- 上报后，Client MUST 立即释放该任务的本地资源，不再尝试执行。

---

### RETRY-03 Server 决策 Escalation

> **Given** Server 收到 `task_failed` 消息且 `error_type == "escalated"`  
> **When** Escalation Handler 被触发  
> **Then** Server MUST 根据以下策略决策：
> 1. **Reassign（重分配）**：若存在其他可用 Client 满足角色+技能匹配，SHOULD 将任务重新入队并调度到新 Client。
> 2. **Terminate（终止）**：若错误为永久性错误（如指令语法错误、权限不足），或重分配次数已达上限（默认 2 次），SHOULD 将任务标记为 `Failed`。
> 3. **Escalate to Admin（人工介入）**：若任务标记为 `critical`，或系统无法决策，MUST 通过日志/告警通道通知管理员。

**验收标准：**
- Escalation 决策 MUST 在收到 `task_failed` 后 1 秒内完成。
- 重分配次数 MUST 可配置（默认 2 次），防止无限循环。
- 所有 Escalation 决策 MUST 记录结构化日志：`escalation_decision`、`task_id`、`decision`、`reason`。
- Admin `/admin/tasks` 端点 MUST 可查询任务的 Escalation 历史和当前决策。

---

## 6. Connection Resilience（连接韧性）

**对应 Phase：** Phase 3（Reef Client & SwarmChannel）

### CONN-01 指数退避重连

> **Given** Client 与 Server 的 WebSocket 连接意外断开  
> **When** 断开事件被检测到  
> **Then** Client MUST 启动重连循环，采用指数退避策略：
> - 第 1 次重试：等待 1 秒
> - 第 2 次重试：等待 2 秒
> - 第 3 次重试：等待 4 秒
> - ...
> - 最大间隔：60 秒
> - 达到最大间隔后保持固定间隔重试，直到成功

**验收标准：**
- 重连 MUST 是自动的，无需人工干预。
- 每次重连尝试 MUST 记录日志，包含 `attempt`、`backoff_ms`。
- 若配置文件设置了 `max_reconnect_attempts`（默认 0 = 无限），达到上限后 Client SHOULD 退出进程并返回非零状态码。

---

### CONN-02 断线期间任务暂停

> **Given** Client 正在执行任务时 WebSocket 连接断开  
> **When** 断开事件被检测到  
> **Then** Client MUST 将 in-flight 任务状态置为本地 `Paused`，暂停进度报告，但 MUST NOT 终止任务执行。

> **Given** Client 成功重连到 Server  
> **When** 重连完成后  
> **Then** Client MUST 重新发送 `register` 消息（携带相同 `client_id`），Server 识别为同一 Client 后，MUST 恢复注册表状态。Client SHOULD 自动恢复 `Paused` 任务的进度报告。

**验收标准：**
- 断线期间，AgentLoop 的执行 SHOULD 继续运行（若不需要与 Server 交互）；若 AgentLoop 需要上报进度而断线，进度数据 MUST 在本地缓冲，恢复连接后批量发送。
- 断线超过 `reconnect_window`（60 秒）未恢复，Server MUST 将任务标记为 `Failed`。

---

### CONN-03 Server 优雅处理 Client 重连

> **Given** 一个 Client 因网络抖动断开连接  
> **When** 该 Client 在 `reconnect_window`（默认 60 秒）内重新连接  
> **Then** Server MUST 复用原有注册表条目（保留 `role`、`skills`、`current_load` 等），MUST NOT 要求重新全量注册。

> **Given** 一个 Client 断开超过 `reconnect_window`  
> **When** 该 Client 重新连接  
> **Then** Server MUST 将其视为新 Client，分配新的 `client_id`，原有 in-flight 任务 MUST 已触发 Escalation。

**验收标准：**
- Server MUST 通过 `register` 消息中的 `client_id`（或 fallback 到连接指纹如 IP+role）识别重连 Client。
- 重连后，Server 的 pending 控制消息（cancel、pause、resume）MUST 重新发送给 Client。

---

## 7. Role-based Skills（角色化技能）

**对应 Phase：** Phase 5（角色技能与 E2E 集成）

### ROLE-01 Client 加载角色相关 Skill 子集

> **Given** Client 启动时配置文件中指定了 `reef.role = "coder"`  
> **When** Client 初始化 Skill Registry  
> **Then** Client MUST 从 `skills/roles/<role>.yaml` 读取角色技能清单，并仅加载清单中列出的技能。

> **Given** 角色配置文件不存在或格式错误  
> **When** Client 尝试加载  
> **Then** Client MUST 记录错误日志并退出（返回非零状态码），避免以不完整能力注册到 Server。

**验收标准：**
- 技能清单文件 MUST 采用 YAML 格式，包含 `skills` 数组（技能名称列表）。
- 清单中的技能名称 MUST 与内置 toolbox 中的技能名称大小写敏感匹配。
- 未在清单中的技能 MUST NOT 被加载到内存中，减少 Client 资源占用。

---

### ROLE-02 角色映射到系统提示词覆盖

> **Given** 角色配置文件包含 `system_prompt` 字段  
> **When** Client 初始化 AgentLoop  
> **Then** Client MUST 使用角色指定的 `system_prompt` 覆盖默认的 PicoClaw 系统提示词。

**验收标准：**
- `system_prompt` 字段可选；若缺失，SHOULD 使用默认提示词。
- 系统提示词覆盖 MUST 在 AgentLoop 初始化时生效，影响该 Client 处理的所有任务。
- 不同角色的系统提示词 MUST 完全隔离，不得相互影响。

---

### ROLE-03 Client 在注册时广告已加载技能

> **Given** Client 已完成角色化技能加载  
> **When** Client 向 Server 发送 `register` 消息  
> **Then** `register` 消息中的 `skills` 字段 MUST 仅包含实际成功加载的技能名称。

**验收标准：**
- 若某个技能加载失败（如依赖缺失），MUST 从 `skills` 列表中排除，并记录警告日志。
- Server MUST 使用 Client 广告的技能列表进行调度匹配（SCHED-01）。
- 技能列表在运行时不可动态变更（v1 限制），Client 重启后重新加载并重新注册。

---

## 8. Admin & Observability（管理与可观测）

**对应 Phase：** Phase 2（Reef Server）

### ADMIN-01 `/admin/status` 端点

> **Given** Server 正在运行  
> **When** HTTP GET 请求到达 `/admin/status`  
> **Then** Server MUST 返回 JSON 响应，包含：
> - `server_version`
> - `start_time`
> - `connected_clients`（数组，每个元素包含 `client_id`、`role`、`skills`、`capacity`、`current_load`、`last_heartbeat`、`connection_state`）
> - `uptime_ms`

**验收标准：**
- 端点 MUST 绑定在独立的管理端口（默认 `:8081`，与 WebSocket 端口分离，可配置）。
- 响应格式 MUST 为 `application/json`，HTTP 200。
- 响应 MUST 在 100ms 内返回（注册表读操作为 O(1)）。

---

### ADMIN-02 `/admin/tasks` 端点

> **Given** Server 正在运行  
> **When** HTTP GET 请求到达 `/admin/tasks`  
> **Then** Server MUST 返回 JSON 响应，包含：
> - `queued_tasks`（数组，`task_id`、`required_role`、`required_skills`、`queued_at`）
> - `inflight_tasks`（数组，`task_id`、`assigned_client_id`、`status`、`started_at`）
> - `completed_tasks`（最近 100 条，`task_id`、`status`、`completed_at`）
> - `stats`（任务总数、成功数、失败数、平均执行时间）

**验收标准：**
- `completed_tasks` 为环形缓冲区，保留最近 100 条，防止内存无限增长。
- 支持可选查询参数：`?status=running&role=coder` 进行过滤。
- 响应 MUST 在 200ms 内返回。

---

### ADMIN-03 结构化日志

> **Given** Reef Server 或 Client 运行时  
> **When** 发生关键事件  
> **Then** 组件 MUST 输出结构化日志，包含以下字段：
> - `timestamp`（ISO8601）
> - `level`（info / warn / error）
> - `component`（server / client / scheduler / registry / connector / agentloop）
> - `event`（见下枚举）
> - `task_id`（可选）
> - `client_id`（可选）
> - `message`（人类可读描述）
> - `details`（结构化上下文对象）

**关键事件枚举：**

| event | 说明 | 级别 |
|-------|------|------|
| `connect` | WebSocket 连接建立 | info |
| `register` | Client 注册成功 | info |
| `register_nack` | Client 注册被拒绝 | warn |
| `heartbeat_timeout` | Client 心跳超时 | warn |
| `task_dispatch` | 任务下发 | info |
| `task_progress` | 任务进度更新 | info |
| `task_completed` | 任务完成 | info |
| `task_failed` | 任务失败 | error |
| `task_cancelled` | 任务被取消 | info |
| `task_paused` | 任务暂停 | info |
| `task_resumed` | 任务恢复 | info |
| `escalation_decision` | Escalation 决策 | warn |
| `reconnect` | Client 重连成功 | info |
| `disconnect` | Client 断开 | warn |

**验收标准：**
- 日志输出 MUST 支持文本和 JSON 两种格式，通过配置文件切换。
- 默认日志级别为 `info`，可通过 `--log-level` flag 或环境变量覆盖。
- 所有 error 级别日志 MUST 包含足够上下文（task_id、client_id、error detail）以便故障排查。

---

*规格版本：v1.0*  
*日期：2026-04-27*  
*作者：Reef Research & Design Agent*
