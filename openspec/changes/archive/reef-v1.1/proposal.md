# Proposal: Reef v1.1 — 补全缺口

## 背景

Reef v1 已完成核心功能（协议、Server、Client、生命周期、角色、E2E 测试、文档）。
审计发现以下 5 项缺口需要在 v1.1 中补齐：

| # | 缺口 | 严重程度 | 说明 |
|---|------|---------|------|
| 1 | `SwarmSettings` 缺少 `Mode` 字段 | 🔴 高 | 文档写了 `"mode": "server"` 但代码不支持，用户按文档操作会失败 |
| 2 | Docker Compose 文件缺失 | 🔴 高 | 文档承诺了但没有实际文件 |
| 3 | Admin 升级告警未实现 | 🟡 中 | `scheduler.go:226` 有 TODO，任务升级到 Admin 时无人知晓 |
| 4 | Admin API 无认证 | 🟡 中 | 生产环境安全隐患，任何人都能提交/查询任务 |
| 5 | Reef 任务缺少模型路由提示 | 🟡 中 | `pkg/routing/` 已实现智能路由，但 Reef 任务未集成 |

**不在本次范围**：性能基线测试（推迟到 v2）、持久化队列（v2）、Web UI（v2）、多 Server 联邦（v2）。

---

## 目标

1. 用户可以通过 `config.json` 的 `"mode": "server"` 启动 Reef Server，无需记忆 CLI 参数
2. `docker compose -f docker/docker-compose.reef.yml up` 一键启动 Server + 2 个 Client
3. 任务升级到 Admin 时，Server 通过 Webhook 推送告警
4. Admin API 要求 `Authorization: Bearer <token>` 认证
5. 任务提交时可指定 `model_hint`，Client 端 AgentLoop 据此选择模型

---

## Approach

### 1. Mode 字段

- `SwarmSettings` 增加 `Mode string`（`"server"` | `"client"` | 空 = client）
- `pkg/gateway/gateway.go` 启动时检测 `swarm` channel 的 `Mode`
  - `"server"` → 启动 `server.Server`（复用 `reef-server` CLI 的逻辑）
  - `"client"`（默认）→ 走现有 SwarmChannel 连接逻辑
- `SwarmSettings` 增加 `WSAddr` / `AdminAddr` 字段（Server 模式专用）
- 更新文档示例，确保与代码一致

### 2. Docker Compose

- 创建 `docker/docker-compose.reef.yml`
- 3 个 service：`reef-server`、`reef-client-coder`、`reef-client-analyst`
- 使用 `config.json` volume mount
- 复用现有 `docker/Dockerfile`

### 3. Admin Webhook 告警

- `server.Config` 增加 `WebhookURLs []string`
- `Scheduler.escalate()` 中 `EscalationToAdmin` 分支调用 webhook
- Webhook payload：JSON POST，包含 `task_id`、`status`、`error`、`attempt_history`
- 异步发送，不阻塞调度器
- 失败仅记日志，不影响任务状态

### 4. Admin API 认证

- `AdminServer` 接收 `token` 参数
- 所有 `/admin/*` 和 `/tasks` 端点检查 `Authorization: Bearer <token>`
- Token 为空时跳过认证（向后兼容开发环境）
- 返回 `401 Unauthorized` 当 token 不匹配

### 5. 模型路由提示

- `Task` 增加 `ModelHint string` 字段（可选）
- `TaskDispatchPayload` 增加 `model_hint` 字段
- `POST /tasks` 请求体增加 `model_hint` 可选参数
- Client 端 `TaskRunner` 将 `model_hint` 传入 `AgentLoop` 的 model override
- 不替换 `pkg/routing/` 的智能路由，而是作为"用户显式指定"的优先级覆盖

---

## 成功标准

- `go test ./pkg/reef/... ./pkg/channels/swarm/... ./test/e2e/... -v` 全部通过
- `docker compose -f docker/docker-compose.reef.yml config` 语法正确
- `make lint-docs` 通过
- 文档中的配置示例与实际代码 100% 一致
