# Reef v1 详细规格（Specifications）

> 本文档使用 [RFC 2119](https://datatracker.ietf.org/doc/html/rfc2119) 关键词：MUST / MUST NOT / SHALL / SHALL NOT / SHOULD / SHOULD NOT / MAY。
> 每个需求采用 Given/When/Then 场景化描述，并标注对应 ROADMAP Phase。

---

## 1. Swarm Protocol（SWARM）— Phase 1

### SWARM-01：Server 维护实时 Client 注册表

**Given** Server 已启动并监听 WebSocket 端口，
**When** 一个 Reef Client 通过 WebSocket 建立连接并发送 `register` 消息，
**Then** Server MUST 将该 Client 的元数据（clientID、role、skills、capacity、lastHeartbeat）存入注册表，并返回 `registered` 确认消息。

**Given** 注册表中已存在 Client A，
**When** Client A 的心跳超时（连续 3 次未收到 heartbeat），
**Then** Server MUST 将 Client A 标记为 offline，并将其正在执行的任务状态置为 `Paused`（若存在）。

**Given** Server 同时接受多个 Client 连接，
**When** 任意 Client 发送 heartbeat，
**Then** Server MUST 原子更新该 Client 的 `lastHeartbeat` 时间戳，且 MUST NOT 影响其他 Client 的注册表条目。

### SWARM-02：Client 启动时向 Server 注册

**Given** Client 已配置 role、skills manifest 和 Server 地址，
**When** Client 启动并建立 WebSocket 连接，
**Then** Client MUST 在连接成功后 5 秒内发送 `register` 消息，消息体 MUST 包含：`client_id`（UUIDv4）、`role`（字符串）、`skills`（字符串数组）、`providers`（字符串数组）、`max_concurrent`（整数，≥1）。

**Given** Client 已发送 `register` 消息，
**When** Server 返回 `registered` 确认，
**Then** Client MUST 进入 `ready` 状态，开始按配置周期发送 heartbeat。

**Given** Client 发送 `register` 后 10 秒内未收到 `registered` 确认，
**When** 超时触发，
**Then** Client MUST 关闭当前连接，并使用指数退避策略重新连接。

### SWARM-03：Client 周期性心跳与 Server 超时剔除

**Given** Client 处于 `ready` 状态，
**When** 心跳间隔（默认 30 秒）到达，
**Then** Client MUST 发送 `heartbeat` 消息，消息体包含当前负载（`current_tasks` 整数）。

**Given** Server 注册表中存在 Client B，其 `lastHeartbeat` 为 T0，
**When** 当前时间超过 T0 + `heartbeat_timeout`（默认 3 个心跳间隔 = 90 秒），
**Then** Server MUST 将 Client B 标记为 offline，并触发该 Client 所有 `Assigned` / `Running` 任务的 `pause` 状态迁移。

**Given** Client 心跳消息到达 Server，
**When** Server 解析心跳负载发现 `current_tasks` 变化，
**Then** Server SHOULD 更新注册表中该 Client 的可用容量，供调度器决策使用。

### SWARM-04：WebSocket 消息协议类型定义

**Given** Reef Protocol v1 通信，
**When** 任意一方发送消息，
**Then** 消息 MUST 为 JSON 格式，且 MUST 包含以下顶层字段：`type`（枚举字符串）、`version`（固定字符串 `"reef-v1"`）、`payload`（对象）、`timestamp`（ISO8601 字符串）。

**Given** 协议消息类型枚举，
**Then** Server 和 Client MUST 支持以下消息类型：
- `register` — Client → Server，注册自身能力
- `registered` — Server → Client，注册确认
- `heartbeat` — Client → Server，周期性心跳
- `task_dispatch` — Server → Client，派发任务
- `task_progress` — Client → Server，任务进度报告
- `task_completed` — Client → Server，任务完成报告
- `task_failed` — Client → Server，任务失败报告（含 attempt history）
- `task_cancel` — Server → Client，取消任务指令
- `task_pause` — Server → Client，暂停任务指令
- `task_resume` — Server → Client，恢复任务指令
- `error` — 双向，通用错误通知

**Given** 收到未知 `type` 的消息，
**When** 解析器无法识别消息类型，
**Then** 接收方 MUST 返回 `error` 消息并 MUST NOT 崩溃或进入未定义状态。

---

## 2. Task Scheduling（SCHED）— Phase 2

### SCHED-01：基于角色和技能的任务匹配

**Given** Server 注册表中有 Client C（role=`coder`，skills=`["github","write_file"]`）和 Client D（role=`analyst`，skills=`["web_fetch","summarize"]`），
**When** 提交一个任务，其 `required_role`=`coder` 且 `required_skills`=`["github"]`，
**Then** Server MUST 将 Client C 列为候选，MUST NOT 将 Client D 列为候选。

**Given** 提交任务时未指定 `required_role`，仅指定 `required_skills`=`["summarize"]`，
**When** 调度器执行匹配，
**Then** Server MUST 在所有具备 `summarize` 技能的 Client 中选择当前负载最低（`current_tasks` 最小）的候选。

**Given** 多个 Client 均满足角色和技能要求，
**When** 调度器执行选择，
**Then** Server MUST 采用以下优先级策略：
1. 可用容量最大（`max_concurrent - current_tasks`）优先；
2. 若容量相同，选择最近心跳时间最近的 Client；
3. 若仍相同，随机选择。

### SCHED-02：Server 向 Client 派发任务

**Given** 调度器已选出目标 Client E，
**When** Server 发送 `task_dispatch` 消息，
**Then** 消息 payload MUST 包含：`task_id`（UUIDv4）、`instruction`（字符串，任务指令）、`context`（对象，可选上下文）、`max_retries`（整数，默认 3）、`timeout_seconds`（整数，默认 300）、`required_role`（字符串）、`required_skills`（字符串数组）。

**Given** Client E 已收到 `task_dispatch` 消息，
**When** Client 将任务注入本地 AgentLoop，
**Then** Client MUST 向 Server 返回 `task_progress` 消息，状态为 `started`。

**Given** `task_dispatch` 消息发送后 30 秒内 Client 未返回 `task_progress(started)`，
**When** 超时触发，
**Then** Server MUST 将该任务重新放入队列，并降低该 Client 的调度优先级。

### SCHED-03：无匹配 Client 时的任务排队

**Given** Server 注册表中无满足任务要求的 Client，
**When** 新任务提交到 Server，
**Then** Server MUST 将该任务放入内存任务队列，状态为 `Queued`，并 MUST NOT 丢弃任务。

**Given** 内存任务队列中存在等待任务，
**When** 一个满足要求的 Client 注册或心跳更新为可用状态，
**Then** Server SHOULD 立即尝试将该任务从队列中取出并 dispatch。

**Given** 内存任务队列已满（配置 `max_queue_size`，默认 1000），
**When** 新任务提交，
**Then** Server MUST 拒绝该任务并返回 `error` 消息，原因码为 `queue_full`。

---

## 3. Task Execution（TASK）— Phase 3

### TASK-01：Client 接收任务并注入 AgentLoop

**Given** Client 收到 `task_dispatch` 消息，
**When** Client 解析任务并准备执行，
**Then** Client MUST 构造一个 `bus.InboundMessage`，其中 `Content` 为 `instruction`，`Metadata` 中注入 `reef_task_id` 和 `reef_max_retries`，并通过 `MessageBus.PublishInbound` 发布。

**Given** AgentLoop 处理该 InboundMessage，
**When** `processOptions` 被构造时，
**Then** Client 的 SwarmChannel MUST 在 `processOptions.Metadata` 中携带 `task_id`，且 AgentLoop MUST 通过 Hook 将 `task_id` 和 `cancelFunc` 绑定到 TurnState。

**Given** 任务正在 AgentLoop 中执行，
**When** AgentLoop 产生 `OutboundMessage`，
**Then** SwarmChannel MUST 拦截该 outbound 消息（若 Channel 为 `"swarm"`），将其作为 `task_progress` 或 `task_completed` 的一部分回传给 Server，MUST NOT 向其他 Channel 发送。

### TASK-02：Client 向 Server 报告任务进度

**Given** 任务已进入 `Running` 状态，
**When** AgentLoop 每完成一次 LLM 调用或工具执行，
**Then** Client SHOULD 发送 `task_progress` 消息，payload 包含：`task_id`、`status`（`running`）、`progress_percent`（0-100 整数，可选）、`current_step`（字符串，可选）。

**Given** 任务执行耗时较长（> 60 秒），
**When** 每 30 秒间隔到达，
**Then** Client MUST 发送至少一次 `task_progress` 心跳，即使 `progress_percent` 未变化，以防止 Server 认为任务僵死。

### TASK-03：Client 报告任务完成

**Given** AgentLoop 成功完成一次 turn 并生成最终响应，
**When** 任务执行结束，
**Then** Client MUST 发送 `task_completed` 消息，payload 包含：`task_id`、`result`（字符串，最终输出）、`duration_ms`（整数）、`iterations`（整数，AgentLoop 迭代次数）。

**Given** `task_completed` 消息已发送，
**When** Server 收到并确认，
**Then** Server MUST 将任务状态迁移为 `Completed`，并释放该 Client 的容量占用。

### TASK-04：Client 报告任务失败

**Given** AgentLoop 执行 turn 时遇到不可恢复错误（如工具执行 panic、LLM 全部 fallback 失败），
**When** 本地重试次数已耗尽，
**Then** Client MUST 发送 `task_failed` 消息，payload 包含：`task_id`、`error`（字符串）、`error_type`（枚举：`execution_error`、`llm_error`、`timeout`、`cancelled`）、`attempts`（整数，已尝试次数）、`logs`（字符串数组，最近 N 条日志）。

**Given** `task_failed` 消息已发送，
**When** Server 收到该消息，
**Then** Server MUST 将任务状态迁移为 `Failed`，并触发 Escalation 决策流程（见 RETRY-03）。

---

## 4. Task Lifecycle Control（LIFE）— Phase 4

### LIFE-01：Server 取消任务

**Given** 任务处于 `Running` 状态，
**When** Server 发送 `task_cancel` 消息到目标 Client，
**Then** Client MUST 调用与该 `task_id` 绑定的 `cancelFunc`，导致 AgentLoop 的 `context` 被取消，当前 turn 中断。

**Given** Client 收到 `task_cancel`，
**When** `cancelFunc` 已调用，
**Then** Client MUST 向 Server 返回 `task_progress` 消息，状态为 `cancelled`，并 MUST NOT 继续执行该任务。

### LIFE-02：Server 暂停任务

**Given** 任务处于 `Running` 状态，
**When** Server 发送 `task_pause` 消息，
**Then** Client MUST 阻塞 AgentLoop 的下一步继续执行（不调用 `cancelFunc`，仅暂停），并向 Server 返回 `task_progress` 消息，状态为 `paused`。

**Given** 任务已暂停，
**When** Client 的 WebSocket 连接断开，
**Then** Client MUST 保持暂停状态，且在内存中保留任务的完整上下文（包括已生成的 LLM 响应和工具结果），MUST NOT 丢弃。

### LIFE-03：Server 恢复任务

**Given** 任务处于 `Paused` 状态，
**When** Server 发送 `task_resume` 消息，
**Then** Client MUST 解除阻塞，允许 AgentLoop 从暂停点继续执行，并向 Server 返回 `task_progress` 消息，状态为 `running`。

**Given** 任务因 Client 断连而暂停，
**When** Client 重连成功，
**Then** Server SHOULD 自动发送 `task_resume`（若策略配置为 auto-resume），或等待管理员手动触发 resume。

### LIFE-04：TaskContext 注入机制

**Given** AgentLoop 处理由 SwarmChannel 注入的任务消息，
**When** `processOptions` 被构造，
**Then** `processOptions` MUST 包含一个可选项 `TaskContext`，其结构为：
```go
type TaskContext struct {
    TaskID     string
    CancelFunc context.CancelFunc
    MaxRetries int
    Attempt    int
}
```

**Given** `TaskContext` 已绑定到 TurnState，
**When** Hook 拦截 `TurnStart` 事件，
**Then** Hook MAY 将 `TaskID` 注入到 LLM 请求的 system prompt 中，以便 Agent 知晓其正在执行分布式任务。

---

## 5. Failure Handling（RETRY）— Phase 4

### RETRY-01：Client 本地重试

**Given** Client 执行任务时遇到可恢复错误（如 LLM 临时 5xx、工具执行超时），
**When** 错误发生时，
**Then** Client MUST 检查 `TaskContext.Attempt < TaskContext.MaxRetries`，若成立，则等待指数退避（1s, 2s, 4s, 8s... 最大 30s）后重试同一任务，MUST NOT 立即上报 Server。

**Given** 本地重试正在进行，
**When** 某次重试成功，
**Then** Client MUST 正常发送 `task_completed`，且 `attempts` 字段 MUST 为实际尝试次数（包含失败的）。

### RETRY-02：Client 耗尽本地重试后上报 Server

**Given** Client 已连续失败 `MaxRetries` 次，
**When** 最后一次重试失败，
**Then** Client MUST 构造 `task_failed` 消息，其中 `logs` MUST 包含每次尝试的错误摘要和 `attempt_history`（JSON 数组，记录每次 attempt 的 timestamp、error、duration_ms）。

**Given** `task_failed` 消息已发送至 Server，
**When** Server 收到消息，
**Then** Server MUST 记录完整错误日志，并 MUST NOT 再次将同一任务自动 dispatch 到同一 Client（至少在当前 session 内）。

### RETRY-03：Server Escalation 决策

**Given** Server 收到 `task_failed` 消息，
**When** Escalation Handler 被触发，
**Then** Server MUST 根据以下策略之一做出决策：
1. **Reassign**：将任务 dispatch 到另一个满足条件的 Client（若存在），任务状态回退到 `Assigned`；
2. **Terminate**：任务状态迁移为 `Failed`，向调用方（或 Admin）返回失败结果；
3. **Escalate to Human**：任务状态迁移为 `Escalated`，Admin 端点 `/admin/tasks` 中标记该任务需要人工干预。

**Given** Escalation 决策为 `Reassign`，
**When** 新 Client 被选中，
**Then** Server MUST 在 `task_dispatch` 的 `context` 中附加 `previous_attempts`（来自原始 `task_failed` 的 `attempt_history`），以便新 Client 了解历史。

**Given** 任务已被 Escalate to Human，
**When** Admin 通过 HTTP API 发送 `resume` 或 `cancel` 指令，
**Then** Server MUST 按对应 LIFE 规则处理，并将任务状态更新为 `Running` 或 `Cancelled`。

---

## 6. Connection Resilience（CONN）— Phase 3

### CONN-01：Client 指数退避重连

**Given** Client 与 Server 的 WebSocket 连接断开（任何原因：网络故障、Server 重启、心跳超时），
**When** 断开被检测到，
**Then** Client MUST 进入 `reconnecting` 状态，并使用指数退避策略重新连接：初始间隔 1s，最大间隔 60s，乘数 2，附加 ±20% 随机抖动。

**Given** Client 正在指数退避重连，
**When** 达到最大重连次数（配置 `max_reconnect_attempts`，默认 0 表示无限），
**Then** Client MAY 进入 `failed` 状态并退出进程，或保持无限重连（取决于配置）。

### CONN-02：断连期间任务暂停而非失败

**Given** Client 正在执行任务 T，
**When** WebSocket 连接断开，
**Then** Client MUST 自动暂停任务 T（等效于收到 `task_pause`），MUST NOT 将任务标记为 `Failed` 或 `Cancelled`。

**Given** Client 已断连且任务 T 处于 `Paused` 状态，
**When** Client 成功重连，
**Then** Client MUST 重新发送 `register` 消息（携带相同 `client_id`），并在收到 `registered` 后等待 Server 发送 `task_resume`。

### CONN-03：Server 优雅处理 Client 重连

**Given** Client F 断连前已在注册表中，
**When** Client F 在心跳超时窗口内（默认 90s）重新连接并发送 `register`，
**Then** Server MUST 复用原有注册表条目（保留 `role`、`skills`、`task` 关联），MUST NOT 创建新条目。

**Given** Client F 断连时间超过心跳超时窗口，
**When** Client F 重新连接，
**Then** Server MUST 将其视为全新 Client，创建新注册表条目；原有关联任务 MUST 进入 Escalation 流程（RETRY-03）。

---

## 7. Role-based Skills（ROLE）— Phase 5

### ROLE-01：Client 启动时加载角色特定技能子集

**Given** Client 配置文件指定 `reef.role = "coder"`，
**When** Client 启动，
**Then** Client MUST 读取 `skills/roles/coder.yaml`，并仅加载该 manifest 中列出的技能到 SkillsLoader，MUST NOT 加载其他未列出的技能。

**Given** 角色技能 manifest 中列出 `github` 和 `write_file`，
**When** SkillsLoader 初始化，
**Then** `AgentInstance.SkillsFilter` MUST 设置为 `["github", "write_file"]`，AgentLoop 构建 context 时 MUST 仅包含这些技能的 SKILL.md 内容。

### ROLE-02：角色映射到技能清单和系统提示词覆盖

**Given** `skills/roles/<role>.yaml` 存在，
**Then** 该文件 MUST 包含以下字段：
```yaml
role: string
skills:
  - string  # skill name
system_prompt_override: string  # 可选，覆盖默认 system prompt
```

**Given** `system_prompt_override` 非空，
**When** AgentInstance 初始化，
**Then** `ContextBuilder` MUST 使用该覆盖值作为 system prompt，替代 config 中默认的 system prompt。

### ROLE-03：Client 注册时通告已加载技能

**Given** Client 已根据角色加载技能子集，
**When** Client 发送 `register` 消息，
**Then** `register.skills` MUST 仅包含实际已加载的技能名称数组，MUST NOT 包含未加载或不可用的技能。

**Given** Server 收到 Client 注册消息，
**When** 更新注册表，
**Then** Server MUST 将 `skills` 作为调度匹配的关键字段之一（见 SCHED-01）。

---

## 8. Admin & Observability（ADMIN）— Phase 2

### ADMIN-01：HTTP `/admin/status` 端点

**Given** Server 已启动，
**When** HTTP GET 请求到达 `/admin/status`，
**Then** Server MUST 返回 JSON 响应，包含：
```json
{
  "server_time": "ISO8601",
  "connected_clients": [
    {
      "client_id": "string",
      "role": "string",
      "skills": ["string"],
      "capacity": { "max": 3, "current": 1 },
      "status": "online|offline",
      "last_heartbeat": "ISO8601"
    }
  ],
  "total_clients": 5,
  "online_clients": 4
}
```

**Given** 请求未携带 `x-reef-token` 或 token 不正确，
**When** 请求到达，
**Then** Server MUST 返回 HTTP 401 Unauthorized。

### ADMIN-02：HTTP `/admin/tasks` 端点

**Given** Server 已启动，
**When** HTTP GET 请求到达 `/admin/tasks`，
**Then** Server MUST 返回 JSON 响应，包含所有任务的状态：
```json
{
  "queued": [ { "task_id": "...", "required_role": "...", "queued_at": "..." } ],
  "in_flight": [ { "task_id": "...", "client_id": "...", "status": "...", "started_at": "..." } ],
  "completed": [ ... ],
  "failed": [ ... ],
  "escalated": [ ... ]
}
```

**Given** 请求携带 `?client_id=xxx` 查询参数，
**When** 请求到达，
**Then** Server SHOULD 仅返回该 Client 关联的任务。

### ADMIN-03：结构化日志

**Given** 任意 Reef 组件运行中，
**When** 发生以下事件之一：connect、register、task_dispatch、task_completed、task_failed、client_disconnect、heartbeat_timeout、escalation_decision，
**Then** 组件 MUST 使用 `pkg/logger` 输出结构化 JSON 日志，级别为 `info`，字段必须包含：`event`、`timestamp`、`component`。

**Given** 发生错误事件（如 WebSocket 读写错误、调度异常），
**When** 错误发生，
**Then** 组件 MUST 输出 `error` 级别日志，字段必须包含：`error`、`stack_trace`（若可用）、`context`（相关 ID）。
