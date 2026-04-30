# Reef v1.1 — 详细规格说明

## 1. 配置驱动的 Server/Client 模式

### Requirement: SwarmSettings.Mode
系统 SHALL 支持通过 `config.json` 的 `channels.swarm.mode` 字段选择 Server 或 Client 模式。

#### Scenario: 通过 config 启动 Server 模式
- GIVEN `config.json` 中 `channels.swarm.mode = "server"`，`ws_addr = ":9080"`，`admin_addr = ":9081"`
- WHEN Gateway 启动并加载配置
- THEN 系统启动 Reef Server 监听 `:9080`（WebSocket）和 `:9081`（Admin）
- AND 不启动 SwarmChannel Client 连接逻辑

#### Scenario: 通过 config 启动 Client 模式（默认）
- GIVEN `config.json` 中 `channels.swarm.mode = "client"`（或省略 mode）
- WHEN Gateway 启动并加载配置
- THEN 系统启动 SwarmChannel Client 连接到 `server_url`
- AND 不启动 Reef Server

#### Scenario: Server 模式缺少 ws_addr 时报错
- GIVEN `config.json` 中 `channels.swarm.mode = "server"` 但 `ws_addr` 为空
- WHEN Gateway 启动
- THEN 返回明确的错误信息："swarm mode 'server' requires ws_addr"

#### Scenario: CLI 命令优先于 config
- GIVEN `config.json` 中 `mode = "client"`
- WHEN 用户执行 `picoclaw reef-server --ws-addr :8080`
- THEN CLI 命令启动 Server 模式，忽略 config 中的 mode 设置

---

## 2. Docker Compose 部署

### Requirement: 一键部署 Reef 集群
系统 SHALL 提供 `docker-compose.reef.yml` 实现 Server + 多 Client 一键启动。

#### Scenario: docker compose 启动成功
- GIVEN 用户已构建或拉取 PicoClaw 镜像
- WHEN 执行 `docker compose -f docker/docker-compose.reef.yml up -d`
- THEN 启动 1 个 Server 容器和 2 个 Client 容器（coder + analyst）
- AND Server 容器暴露 8080（WebSocket）和 8081（Admin）端口
- AND Client 容器通过 Docker 内部网络连接到 Server

#### Scenario: Client 自动重连
- GIVEN Reef 集群已通过 Docker Compose 启动
- WHEN Server 容器重启
- THEN Client 容器自动重连（依赖 Connector 的 exponential backoff）

#### Scenario: docker compose config 验证通过
- GIVEN `docker/docker-compose.reef.yml` 已创建
- WHEN 执行 `docker compose -f docker/docker-compose.reef.yml config`
- THEN 输出有效的 YAML 配置，无错误

---

## 3. Admin 升级告警 Webhook

### Requirement: 任务升级时推送 Webhook
当任务达到最大重试次数并升级到 Admin 时，系统 SHALL 通过 Webhook 推送告警。

#### Scenario: 任务升级触发 Webhook
- GIVEN Server 配置了 `webhook_urls = ["http://alert-service:9090/hook"]`
- AND 一个任务的 `escalation_count` 达到 `max_escalations`
- WHEN Scheduler 将任务状态转为 `Escalated`
- THEN Server 向 `http://alert-service:9090/hook` 发送 POST 请求
- AND 请求体包含 `task_id`、`status`、`error`、`attempt_history`、`timestamp`

#### Scenario: Webhook 推送失败不影响任务
- GIVEN Server 配置了 `webhook_urls`
- AND Webhook 端点不可达（返回错误或超时）
- WHEN 任务升级触发 Webhook
- THEN 任务仍然正常转为 `Escalated` 状态
- AND 错误仅记录到日志，不阻塞调度器

#### Scenario: 未配置 Webhook 时静默
- GIVEN Server 未配置 `webhook_urls`（空数组或 nil）
- WHEN 任务升级
- THEN 不发送任何 Webhook 请求
- AND 任务正常转为 `Escalated` 状态

#### Scenario: 多个 Webhook 端点
- GIVEN Server 配置了 `webhook_urls = ["http://a:9090/hook", "http://b:9090/hook"]`
- WHEN 任务升级
- THEN 向两个端点都发送 POST 请求（并发）

---

## 4. Admin API 认证

### Requirement: Token 保护 Admin 端点
Admin API SHALL 支持可选的 Bearer Token 认证。

#### Scenario: 有效 Token 访问成功
- GIVEN Server 配置了 `token = "secret123"`
- WHEN 客户端发送 `GET /admin/status` 并携带 `Authorization: Bearer secret123`
- THEN 返回 200 和正常的 status JSON

#### Scenario: 无效 Token 被拒绝
- GIVEN Server 配置了 `token = "secret123"`
- WHEN 客户端发送 `GET /admin/status` 并携带 `Authorization: Bearer wrong`
- THEN 返回 401 Unauthorized

#### Scenario: 缺少 Token 被拒绝
- GIVEN Server 配置了 `token = "secret123"`
- WHEN 客户端发送 `GET /admin/status` 不携带 Authorization 头
- THEN 返回 401 Unauthorized

#### Scenario: 未配置 Token 时跳过认证（向后兼容）
- GIVEN Server 未配置 `token`（空字符串）
- WHEN 客户端发送 `GET /admin/status` 不携带 Authorization 头
- THEN 返回 200，正常响应

#### Scenario: 所有 Admin 端点都受保护
- GIVEN Server 配置了 `token`
- WHEN 客户端不携带 Token 访问 `/admin/status`、`/admin/tasks`、`/tasks`
- THEN 所有端点都返回 401

---

## 5. 任务级模型路由提示

### Requirement: 任务可携带 model_hint
任务提交时 SHALL 支持可选的 `model_hint` 字段，Client 端据此选择执行模型。

#### Scenario: 提交任务时指定 model_hint
- GIVEN 用户提交任务 `{"instruction": "...", "required_role": "coder", "model_hint": "gpt-4o"}`
- WHEN Server 接收并调度任务
- THEN `TaskDispatchPayload` 中包含 `model_hint: "gpt-4o"`
- AND Client 端 AgentLoop 使用 `gpt-4o` 作为执行模型

#### Scenario: 未指定 model_hint 时使用默认路由
- GIVEN 用户提交任务不包含 `model_hint`
- WHEN Client 端执行任务
- THEN AgentLoop 使用 `pkg/routing/` 的智能路由选择模型
- AND 行为与 v1 完全一致

#### Scenario: model_hint 传递到 TaskDispatchPayload
- GIVEN 任务的 `model_hint = "claude-sonnet-4-20250514"`
- WHEN Server 向 Client 发送 `task_dispatch`
- THEN payload 中 `model_hint` 字段值为 `"claude-sonnet-4-20250514"`

#### Scenario: model_hint 为空时不覆盖路由
- GIVEN `model_hint = ""`（空字符串）
- WHEN Client 端执行任务
- THEN AgentLoop 的模型选择逻辑不受影响
