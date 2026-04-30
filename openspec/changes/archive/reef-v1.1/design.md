# Design: Reef v1.1

## 变更概览

```
修改文件:
  pkg/config/config_channel.go        — SwarmSettings 增加 Mode/WSAddr/AdminAddr/WebhookURLs
  pkg/reef/server/server.go           — Config 增加 WebhookURLs
  pkg/reef/server/admin.go            — AdminServer 增加 Token 认证中间件
  pkg/reef/server/scheduler.go        — escalate() 中实现 Webhook 推送
  pkg/reef/task.go                    — Task 增加 ModelHint 字段
  pkg/reef/protocol.go                — TaskDispatchPayload 增加 ModelHint
  pkg/reef/server/admin.go            — SubmitTaskRequest 增加 ModelHint
  pkg/channels/swarm/swarm.go         — TaskRunner 传递 ModelHint
  pkg/gateway/gateway.go              — 启动时检测 Mode，决定启动 Server 还是 Client
  cmd/picoclaw/internal/reef/command.go — 复用 Config 构建逻辑
  docs/reef/*.md                      — 更新文档示例
  test/e2e/reef_e2e_test.go           — 新增 v1.1 场景测试

新增文件:
  docker/docker-compose.reef.yml      — Reef 一键部署
  docker/reef-server-config.json      — Server 配置模板
  docker/reef-client-coder-config.json
  docker/reef-client-analyst-config.json
```

---

## 1. Mode 字段

### 数据结构变更

```go
// pkg/config/config_channel.go
type SwarmSettings struct {
    Enabled           bool     `json:"enabled"`
    Mode              string   `json:"mode,omitempty"`              // "server" | "client" (default)
    ServerURL         string   `json:"server_url,omitempty"`        // Client 模式: WebSocket 服务器地址
    Token             string   `json:"token,omitempty"`
    ClientID          string   `json:"client_id,omitempty"`
    Role              string   `json:"role,omitempty"`              // Client 模式
    Skills            []string `json:"skills,omitempty"`            // Client 模式
    Providers         []string `json:"providers,omitempty"`         // Client 模式
    Capacity          int      `json:"capacity,omitempty"`          // Client 模式
    HeartbeatInterval int      `json:"heartbeat_interval,omitempty"`// Client 模式
    WSAddr            string   `json:"ws_addr,omitempty"`           // Server 模式: WebSocket 监听地址
    AdminAddr         string   `json:"admin_addr,omitempty"`        // Server 模式: Admin 监听地址
    MaxQueue          int      `json:"max_queue,omitempty"`         // Server 模式
    MaxEscalations    int      `json:"max_escalations,omitempty"`   // Server 模式
    WebhookURLs       []string `json:"webhook_urls,omitempty"`      // Server 模式: 升级告警
}
```

### Gateway 启动逻辑

```go
// pkg/gateway/gateway.go — 伪代码
func startGateway(cfg *config.Config) {
    swarmCfg := cfg.Channels["swarm"]
    if swarmCfg != nil && swarmCfg.Enabled {
        settings := swarmCfg.Settings.(*config.SwarmSettings)
        switch settings.Mode {
        case "server":
            startReefServer(settings)
            return  // Server 模式不启动常规 Gateway
        default:
            // 走现有 SwarmChannel Client 逻辑
        }
    }
    // ... 正常 Gateway 启动
}
```

### 兼容性

- `mode` 为空或 `"client"` → 行为与 v1 完全一致
- `mode = "server"` → 新增行为，启动 Server
- CLI `picoclaw reef-server` → 始终启动 Server，忽略 config 中的 mode

---

## 2. Docker Compose

### 文件结构

```
docker/
├── docker-compose.reef.yml          # 新增
├── reef-server-config.json          # 新增
├── reef-client-coder-config.json    # 新增
└── reef-client-analyst-config.json  # 新增
```

### docker-compose.reef.yml

```yaml
version: "3.8"

services:
  reef-server:
    image: sipeed/picoclaw:latest
    command: ["--config", "/data/config.json"]
    volumes:
      - ./reef-server-config.json:/data/config.json:ro
    ports:
      - "8080:8080"
      - "8081:8081"
    networks:
      - reef
    restart: unless-stopped

  reef-client-coder:
    image: sipeed/picoclaw:latest
    command: ["--config", "/data/config.json"]
    volumes:
      - ./reef-client-coder-config.json:/data/config.json:ro
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}
    networks:
      - reef
    depends_on:
      - reef-server
    restart: unless-stopped

  reef-client-analyst:
    image: sipeed/picoclaw:latest
    command: ["--config", "/data/config.json"]
    volumes:
      - ./reef-client-analyst-config.json:/data/config.json:ro
    environment:
      - OPENAI_API_KEY=${OPENAI_API_KEY}
    networks:
      - reef
    depends_on:
      - reef-server
    restart: unless-stopped

networks:
  reef:
    driver: bridge
```

### reef-server-config.json

```json
{
  "channels": {
    "swarm": {
      "enabled": true,
      "mode": "server",
      "ws_addr": ":8080",
      "admin_addr": ":8081",
      "token": "${REEF_TOKEN}",
      "webhook_urls": []
    }
  }
}
```

---

## 3. Admin Webhook 告警

### Webhook Payload

```json
{
  "event": "task_escalated",
  "task_id": "task-1-abc123",
  "status": "Escalated",
  "instruction": "Write a unit test",
  "required_role": "coder",
  "error": {
    "type": "execution_error",
    "message": "Compilation failed"
  },
  "attempt_history": [
    {
      "attempt_number": 1,
      "client_id": "coder-a",
      "status": "failed",
      "error_message": "Compilation failed",
      "started_at": 1714195100000,
      "ended_at": 1714195150000
    }
  ],
  "escalation_count": 2,
  "max_escalations": 2,
  "timestamp": 1714195200000
}
```

### 实现方式

```go
// pkg/reef/server/webhook.go (新文件)
func sendWebhookAlert(logger *slog.Logger, urls []string, payload WebhookPayload) {
    body, _ := json.Marshal(payload)
    for _, url := range urls {
        go func(u string) {
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            req, _ := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(body))
            req.Header.Set("Content-Type", "application/json")
            resp, err := http.DefaultClient.Do(req)
            if err != nil {
                logger.Warn("webhook failed", slog.String("url", u), slog.Error(err))
                return
            }
            resp.Body.Close()
            if resp.StatusCode >= 400 {
                logger.Warn("webhook error", slog.String("url", u), slog.Int("status", resp.StatusCode))
            }
        }(url)
    }
}
```

### 调用点

```go
// pkg/reef/server/scheduler.go — escalate() 中
case EscalationToAdmin:
    _ = task.Transition(reef.TaskEscalated)
    go sendWebhookAlert(s.logger, s.webhookURLs, WebhookPayload{
        Event:           "task_escalated",
        TaskID:          task.ID,
        // ... 其他字段
    })
```

---

## 4. Admin API 认证

### 中间件模式

```go
// pkg/reef/server/admin.go
type AdminServer struct {
    registry  *Registry
    scheduler *Scheduler
    token     string          // 新增
    logger    *slog.Logger
}

func (a *AdminServer) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if a.token == "" {
            next(w, r)  // 未配置 token，跳过认证
            return
        }
        auth := r.Header.Get("Authorization")
        if auth != "Bearer "+a.token {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        next(w, r)
    }
}

func (a *AdminServer) RegisterRoutes(mux *http.ServeMux) {
    mux.HandleFunc("/admin/status", a.authMiddleware(a.handleStatus))
    mux.HandleFunc("/admin/tasks", a.authMiddleware(a.handleTasks))
    mux.HandleFunc("/tasks", a.authMiddleware(a.handleSubmitTask))
}
```

### Token 来源

- Server 模式通过 config: `swarm.token`
- CLI 命令: `--token`
- 两者复用同一个 token，WebSocket 和 Admin API 共享

---

## 5. 模型路由提示

### 数据结构变更

```go
// pkg/reef/task.go
type Task struct {
    // ... 现有字段
    ModelHint string `json:"model_hint,omitempty"` // 可选：指定执行模型
}

// pkg/reef/protocol.go
type TaskDispatchPayload struct {
    // ... 现有字段
    ModelHint string `json:"model_hint,omitempty"` // 新增
}

// pkg/reef/server/admin.go
type SubmitTaskRequest struct {
    // ... 现有字段
    ModelHint string `json:"model_hint,omitempty"` // 新增
}
```

### Client 端使用

```go
// pkg/reef/client/task_runner.go
func (tr *TaskRunner) executeTask(ctx context.Context, task *TaskDispatchPayload) {
    // 如果指定了 model_hint，临时覆盖 AgentLoop 的 model
    if task.ModelHint != "" {
        // 通过 AgentLoop 的 session allocation 传递 model override
        // 具体实现依赖 AgentLoop 的 model override 接口
    }
    // ... 正常执行
}
```

---

## 测试计划

### 新增 E2E 测试场景

| # | 场景 | 覆盖需求 |
|---|------|---------|
| 18 | Server 模式通过 config 启动 | Mode 字段 |
| 19 | Admin API 有效 Token 访问成功 | Admin 认证 |
| 20 | Admin API 无效 Token 返回 401 | Admin 认证 |
| 21 | Admin API 无 Token 返回 401 | Admin 认证 |
| 22 | 任务升级触发 Webhook（mock server） | Webhook 告警 |
| 23 | 任务携带 model_hint 调度 | 模型路由 |

### 单元测试

- `SwarmSettings` Mode 字段序列化/反序列化
- `AdminServer.authMiddleware` 正确/错误 token
- `sendWebhookAlert` HTTP 调用（mock）
- `Task.ModelHint` 字段传递
