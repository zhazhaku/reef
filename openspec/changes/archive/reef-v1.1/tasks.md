# Tasks: Reef v1.1

## Phase 1: 配置与数据结构（基础层）

- [x] `SwarmSettings` 增加 `Mode`、`WSAddr`、`AdminAddr`、`MaxQueue`、`MaxEscalations`、`WebhookURLs` 字段
- [x] `Task` 增加 `ModelHint string` 字段
- [x] `TaskDispatchPayload` 增加 `ModelHint string` 字段
- [x] `SubmitTaskRequest` 增加 `ModelHint string` 字段
- [x] `server.Config` 增加 `WebhookURLs []string` 字段
- [x] `NewServer()` 从 Config 传递 WebhookURLs 到 Scheduler
- [x] 编译验证：`go build ./...`

## Phase 2: Admin API 认证

- [x] `AdminServer` 增加 `token` 字段
- [x] 实现 `authMiddleware` 方法
- [x] `RegisterRoutes` 中所有端点包裹 `authMiddleware`
- [x] `NewAdminServer` 接收 token 参数
- [x] 单元测试：有效 Token → 200
- [x] 单元测试：无效 Token → 401
- [x] 单元测试：无 Token → 401
- [x] 单元测试：Token 为空时跳过认证

## Phase 3: Admin Webhook 告警

- [x] 创建 `pkg/reef/server/webhook.go`
- [x] 定义 `WebhookPayload` 结构体
- [x] 实现 `sendWebhookAlert()` 函数（并发、超时、错误处理）
- [x] `Scheduler` 增加 `webhookURLs` 字段
- [x] `escalate()` 的 `EscalationToAdmin` 分支调用 `sendWebhookAlert`
- [x] 单元测试：Webhook 被调用（httptest mock server）
- [x] 单元测试：Webhook 失败不影响任务状态
- [x] 单元测试：未配置 Webhook 时不发送

## Phase 4: 模型路由提示

- [x] `NewTask()` 接受 `modelHint` 参数（或 setter）
- [x] `admin.handleSubmitTask` 从请求体读取 `model_hint` 并设置到 Task
- [x] `scheduler.dispatch` 将 `ModelHint` 传递到 `TaskDispatchPayload`
- [x] `SwarmChannel` 接收 `task_dispatch` 时提取 `ModelHint`
- [x] `TaskRunner` 将 `ModelHint` 传入 AgentLoop session
- [x] 单元测试：ModelHint 从 Task → Dispatch → Client 端传递

## Phase 5: Mode 字段与 Gateway 集成

- [x] `pkg/gateway/gateway.go` 启动时检测 `swarm.mode`
- [x] `mode = "server"` 时：构建 `server.Config` 并启动 Server
- [x] `mode = "server"` 时：阻塞等待信号（SIGTERM/SIGINT）
- [x] `mode = "server"` 且 `ws_addr` 为空时：返回明确错误
- [x] `mode = "client"` 或空时：行为不变
- [x] CLI `picoclaw reef-server` 保持不变（独立于 config mode）

## Phase 6: Docker Compose

- [x] 创建 `docker/docker-compose.reef.yml`
- [x] 创建 `docker/reef-server-config.json`（mode=server）
- [x] 创建 `docker/reef-client-coder-config.json`（mode=client, role=coder）
- [x] 创建 `docker/reef-client-analyst-config.json`（mode=client, role=analyst）
- [x] 验证：`docker compose -f docker/docker-compose.reef.yml config` 通过

## Phase 7: 文档更新

- [x] `docs/reef/README.md` — 更新配置示例，移除不存在的字段
- [x] `docs/reef/deployment.md` — 更新 Docker Compose 示例，指向实际文件
- [x] `docs/reef/api.md` — 添加认证说明、model_hint 参数
- [x] `docs/reef/protocol.md` — 添加 model_hint payload 字段
- [x] `docs/reef/roles.md` — 无变更
- [x] `README.md` — 无变更（v1.1 不改根 README）
- [x] `CHANGELOG.md` — 添加 v1.1 变更记录
- [x] 验证：`make lint-docs` 通过

## Phase 8: E2E 测试

- [x] 测试：Admin API Token 认证（有效/无效/缺失/空）
- [x] 测试：任务升级触发 Webhook（httptest mock）
- [x] 测试：任务携带 model_hint 调度
- [x] 测试：Server 模式通过 config 启动（需要 Gateway 集成测试或单独测试）
- [x] 运行全部测试：`go test ./pkg/reef/... ./pkg/channels/swarm/... ./test/e2e/... -v`
- [x] 验证无 flake（3 次运行）

## Phase 9: 提交与验证

- [x] Git commit：`feat(reef): v1.1 — mode config, docker compose, webhook, auth, model hint`
- [x] Git push
- [x] 更新 `.planning/STATE.md`
