# Tasks: Reef v2.0

> 任务分解粒度：每个 task 是一个可独立编译/测试的原子单元。
> 预估工时基于单人开发。

---

## Phase 1: 持久化任务队列（P0）

### 1.1 TaskStore 接口定义

- [x] **1.1.1** 创建 `pkg/reef/server/store/` 目录
- [x] **1.1.2** 创建 `store.go`：定义 `TaskStore` 接口（8 个方法）
- [x] **1.1.3** 创建 `filter.go`：定义 `TaskFilter` 结构体（Statuses、Roles、Limit、Offset）
- [x] **1.1.4** 编译验证：`go build ./pkg/reef/server/store/...`

### 1.2 MemoryStore 实现

- [x] **1.2.1** 创建 `memory.go`：实现 `MemoryStore`（基于 `sync.RWMutex` + `map[string]*Task`）
- [x] **1.2.2** 实现 `SaveTask` — 写入内存 map，ID 冲突返回错误
- [x] **1.2.3** 实现 `GetTask` — 从 map 读取，不存在返回 nil
- [x] **1.2.4** 实现 `UpdateTask` — 更新已有任务，不存在返回错误
- [x] **1.2.5** 实现 `DeleteTask` — 从 map 删除
- [x] **1.2.6** 实现 `ListTasks` — 按 TaskFilter 过滤、分页
- [x] **1.2.7** 实现 `SaveAttempt` — 追加到内存切片
- [x] **1.2.8** 实现 `GetAttempts` — 返回任务的所有尝试记录
- [x] **1.2.9** 实现 `Close` — 清空内存
- [x] **1.2.10** 创建 `memory_test.go`：覆盖所有 8 个方法的单元测试
- [x] **1.2.11** 编译 + 测试：`go test ./pkg/reef/server/store/...`

### 1.3 SQLiteStore 实现

- [x] **1.3.1** 添加依赖：`go get modernc.org/sqlite`（纯 Go SQLite，无 CGO）
- [x] **1.3.2** 创建 `sqlite.go`：定义 `SQLiteStore` 结构体（db、path、mu）
- [x] **1.3.3** 实现 `NewSQLiteStore(path string)` — 打开数据库，启用 WAL 模式
- [x] **1.3.4** 实现 `migrate()` — 创建 `tasks` 和 `task_attempts` 表 + 索引
- [x] **1.3.5** 实现 `SaveTask` — INSERT INTO tasks
- [x] **1.3.6** 实现 `GetTask` — SELECT FROM tasks WHERE id = ?
- [x] **1.3.7** 实现 `UpdateTask` — UPDATE tasks SET ... WHERE id = ?
- [x] **1.3.8** 实现 `DeleteTask` — DELETE FROM tasks WHERE id = ?
- [x] **1.3.9** 实现 `ListTasks` — SELECT + WHERE + LIMIT + OFFSET
- [x] **1.3.10** 实现 `SaveAttempt` — INSERT INTO task_attempts
- [x] **1.3.11** 实现 `GetAttempts` — SELECT FROM task_attempts WHERE task_id = ?
- [x] **1.3.12** 实现 `Close` — 关闭数据库连接
- [x] **1.3.13** 实现辅助函数：`taskToRow`/`rowToTask`（Task ↔ SQL 行映射）
- [x] **1.3.14** 实现辅助函数：`marshalSkills`/`unmarshalSkills`（[]string ↔ JSON）
- [x] **1.3.15** 实现辅助函数：`marshalResult`/`marshalError`（结构体 ↔ JSON）
- [x] **1.3.16** 创建 `sqlite_test.go`：覆盖所有 CRUD 方法
- [x] **1.3.17** 测试并发安全性：多个 goroutine 同时读写
- [x] **1.3.18** 测试 WAL 模式：验证读不阻塞写
- [x] **1.3.19** 测试数据库文件自动创建：路径不存在时自动创建目录
- [x] **1.3.20** 编译 + 测试：`go test ./pkg/reef/server/store/... -v`

### 1.4 PersistentQueue 集成

- [x] **1.4.1** 修改 `queue.go`：添加 `PersistentQueue` 结构体（包装 TaskStore + 内存缓存）
- [x] **1.4.2** 实现 `NewPersistentQueue(store, maxLen, maxAge)` — 创建并调用 restore
- [x] **1.4.3** 实现 `restore()` — 从 Store 加载非终态任务，Running 重置为 Queued
- [x] **1.4.4** 实现 `Enqueue` — 写入 Store + 内存缓存
- [x] **1.4.5** 实现 `Dequeue` — 从缓存取 + 更新 Store 状态
- [x] **1.4.6** 实现 `Peek` — 从缓存读取头部
- [x] **1.4.7** 实现 `Len` — 返回缓存长度
- [x] **1.4.8** 实现 `Snapshot` — 返回缓存副本
- [x] **1.4.9** 实现 `Expire` — 过期任务从缓存移除 + 更新 Store
- [x] **1.4.10** 创建 `queue_persistent_test.go`：测试持久化队列的入队/出队/恢复
- [x] **1.4.11** 测试恢复场景：模拟 Store 中有 Queued + Running 任务，验证恢复逻辑

### 1.5 Server 集成

- [x] **1.5.1** 修改 `server.go` `Config`：增加 `StoreType string`、`StorePath string`
- [x] **1.5.2** 修改 `NewServer`：根据 `StoreType` 创建 MemoryStore 或 SQLiteStore
- [x] **1.5.3** 修改 `NewServer`：用 PersistentQueue 替换原有 TaskQueue
- [x] **1.5.4** 修改 `Stop`：关闭 TaskStore 连接
- [x] **1.5.5** 修改 `heartbeatScanner`：任务状态变更时同步到 Store
- [x] **1.5.6** 修改 `scheduler.go` `HandleTaskCompleted`/`HandleTaskFailed`：调用 `store.UpdateTask`
- [x] **1.5.7** 修改 `admin.go` `handleSubmitTask`：任务创建时调用 `store.SaveTask`

### 1.6 配置集成

- [x] **1.6.1** 修改 `config_channel.go` `SwarmSettings`：增加 `StoreType`、`StorePath` 字段
- [x] **1.6.2** 修改 `gateway.go` `runReefServerMode`：传递 StoreType/StorePath 到 Config
- [x] **1.6.3** 修改 `cmd/picoclaw/internal/reef/command.go`：CLI 增加 `--store-type`/`--store-path` 参数

### 1.7 E2E 测试

- [x] **1.7.1** 创建 `test/e2e/persistence_test.go`
- [x] **1.7.2** 测试：提交任务 → 重启 Server → 验证任务恢复
- [x] **1.7.3** 测试：SQLite 模式下任务完整生命周期
- [x] **1.7.4** 测试：内存模式下行为与 v1.1 一致
- [x] **1.7.5** 运行全部测试：`go test ./pkg/reef/... ./test/e2e/... -v`

### 1.8 文档

- [x] **1.8.1** 更新 `docs/reef/deployment.md`：添加 SQLite 配置说明
- [x] **1.8.2** 更新 `docs/reef/api.md`：无变更
- [x] **1.8.3** 更新 `CHANGELOG.md`：添加 v2.0 持久化队列条目

---

## Phase 2: TLS 原生支持（P1）

### 2.1 TLS 配置

- [x] **2.1.1** 创建 `pkg/reef/server/tls.go`：定义 `TLSConfig` 结构体
- [x] **2.1.2** 修改 `server.go` `Config`：增加 `TLS *TLSConfig` 字段
- [x] **2.1.3** 修改 `config_channel.go` `SwarmSettings`：增加 `TLS` 子结构

### 2.2 Server TLS

- [x] **2.2.1** 修改 `server.go` `Start()`：检测 TLS 配置，创建 `tls.Config`
- [x] **2.2.2** 实现 `loadTLSCert(certFile, keyFile string) (*tls.Certificate, error)`
- [x] **2.2.3** 修改 WebSocket listener：TLS 启用时使用 `tls.NewListener`
- [x] **2.2.4** 修改 Admin listener：TLS 启用时使用 `tls.NewListener`
- [x] **2.2.5** 日志输出：TLS 启用时打印 `wss://` 和 `https://` 地址

### 2.3 Client TLS

- [x] **2.3.1** 修改 `connector.go`：检测 `wss://` scheme
- [x] **2.3.2** 实现 `dialWithTLS`：使用 `tls.Config` 建立 WebSocket 连接
- [x] **2.3.3** 实现自定义 CA 支持：加载 `ca_file` 到 `RootCAs` 池
- [x] **2.3.4** 实现 `verify` 选项：`false` 时跳过证书验证（开发环境）

### 2.4 配置集成

- [x] **2.4.1** 修改 `gateway.go`：传递 TLS 配置到 Server Config
- [x] **2.4.2** 修改 Docker Compose：添加 TLS 配置示例（注释掉）

### 2.5 测试

- [x] **2.5.1** 生成测试证书：`openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 365 -nodes`
- [x] **2.5.2** 创建 `test/e2e/tls_test.go`
- [x] **2.5.3** 测试：TLS Server + TLS Client 连接成功
- [x] **2.5.4** 测试：自定义 CA 证书验证
- [x] **2.5.5** 测试：未配置 TLS 时行为不变
- [x] **2.5.6** 运行全部测试

### 2.6 文档

- [x] **2.6.1** 更新 `docs/reef/deployment.md`：添加 TLS 配置指南
- [x] **2.6.2** 更新 `CHANGELOG.md`

---

## Phase 3: 多通道告警通知（P2）

### 3.1 Notifier 接口

- [x] **3.1.1** 创建 `pkg/reef/server/notify/` 目录
- [x] **3.1.2** 创建 `notifier.go`：定义 `Alert` 结构体和 `Notifier` 接口
- [x] **3.1.3** 创建 `manager.go`：实现 `NotificationManager`（扇出发送）

### 3.2 WebhookNotifier

- [x] **3.2.1** 从 `webhook.go` 迁移到 `notify/webhook.go`
- [x] **3.2.2** 适配 `Notifier` 接口
- [x] **3.2.3** 更新 `scheduler.go`：使用 `NotificationManager` 替换直接调用

### 3.3 SlackNotifier

- [x] **3.3.1** 创建 `notify/slack.go`
- [x] **3.3.2** 实现 `SlackNotifier.Notify`：POST 到 Slack Incoming Webhook
- [x] **3.3.3** 格式化 Slack Block Kit 消息（标题、字段、颜色）
- [x] **3.3.4** 创建 `slack_test.go`：httptest mock 验证请求格式

### 3.4 SMTPNotifier

- [x] **3.4.1** 创建 `notify/smtp.go`
- [x] **3.4.2** 实现 `SMTPNotifier.Notify`：通过 `net/smtp` 发送邮件
- [x] **3.4.3** 支持 PLAIN/LOGIN 认证
- [x] **3.4.4** 邮件模板：HTML 格式，包含任务详情表格
- [x] **3.4.5** 创建 `smtp_test.go`：mock SMTP server 验证

### 3.5 FeishuNotifier

- [x] **3.5.1** 创建 `notify/feishu.go`
- [x] **3.5.2** 实现 `FeishuNotifier.Notify`：POST 飞书 Webhook，富文本卡片
- [x] **3.5.3** 创建 `feishu_test.go`

### 3.6 WeComNotifier

- [x] **3.6.1** 创建 `notify/wecom.go`
- [x] **3.6.2** 实现 `WeComNotifier.Notify`：POST 企业微信 Webhook，Markdown 消息
- [x] **3.6.3** 创建 `wecom_test.go`

### 3.7 配置集成

- [x] **3.7.1** 修改 `config_channel.go`：`SwarmSettings` 增加 `Notifications []NotificationConfig`
- [x] **3.7.2** 修改 `server.go` `NewServer`：根据配置创建 NotificationManager
- [x] **3.7.3** 修改 `scheduler.go`：escalate 时调用 `notifyManager.NotifyAll`
- [x] **3.7.4** 修改 `gateway.go`：传递通知配置

### 3.8 测试

- [x] **3.8.1** 创建 `test/e2e/notification_test.go`
- [x] **3.8.2** 测试：Webhook 通知（已有，验证兼容性）
- [x] **3.8.3** 测试：多渠道同时通知
- [x] **3.8.4** 测试：单渠道失败不影响其他渠道
- [x] **3.8.5** 运行全部测试

### 3.9 文档

- [x] **3.9.1** 创建 `docs/reef/notifications.md`：各渠道配置说明
- [x] **3.9.2** 更新 `CHANGELOG.md`

---

## Phase 4: Web UI 仪表盘（P1）

### 4.1 后端 API

- [x] **4.1.1** 创建 `pkg/reef/server/ui/` 目录
- [x] **4.1.2** 创建 `ui.go`：定义 UI HTTP handler + go:embed
- [x] **4.1.3** 实现 `GET /ui` — 重定向到 `/ui/`
- [x] **4.1.4** 实现 `GET /ui/` — 返回 `index.html`
- [x] **4.1.5** 实现静态文件服务：`/ui/static/*` 映射到嵌入文件
- [x] **4.1.6** 实现 `GET /api/v2/status` — 增强状态 JSON（版本、运行时间、队列深度、任务统计）
- [x] **4.1.7** 实现 `GET /api/v2/tasks` — 分页任务列表 JSON（支持 status/role 筛选）
- [x] **4.1.8** 实现 `GET /api/v2/clients` — 客户端列表 JSON（含负载、心跳）
- [x] **4.1.9** 实现 `GET /api/v2/events` — SSE 实时事件流
- [x] **4.1.10** 实现 SSE 事件推送：task_update、client_update、stats_update
- [x] **4.1.11** 修改 `admin.go` `RegisterRoutes`：注册 UI 路由

### 4.2 前端 — 概览页面

- [x] **4.2.1** 创建 `ui/static/index.html`：SPA 骨架 + 导航栏
- [x] **4.2.2** 创建 `ui/static/style.css`：响应式布局、暗色主题
- [x] **4.2.3** 创建 `ui/static/app.js`：路由管理 + API 调用 + SSE 连接
- [x] **4.2.4** 实现概览页面：Server 信息卡片（版本、运行时间）
- [x] **4.2.5** 实现概览页面：实时统计卡片（queued/running/completed/failed）
- [x] **4.2.6** 实现概览页面：客户端状态摘要（在线/离线/过期数）
- [x] **4.2.7** 实现概览页面：最近 10 条任务快速列表

### 4.3 前端 — 任务列表

- [x] **4.3.1** 实现任务列表页面：表格展示（ID、状态、角色、创建时间、客户端）
- [x] **4.3.2** 实现状态筛选下拉框（All/Queued/Running/Completed/Failed/Escalated）
- [x] **4.3.3** 实现角色筛选下拉框
- [x] **4.3.4** 实现分页控件（上一页/下一页，每页 50 条）
- [x] **4.3.5** 实现任务详情弹窗（点击任务 ID 展开）
- [x] **4.3.6** 实现任务提交表单（instruction、role、skills、model_hint）
- [x] **4.3.7** 实现任务取消按钮

### 4.4 前端 — 客户端拓扑

- [x] **4.4.1** 实现客户端列表页面：表格展示（ID、角色、技能、状态、负载、心跳）
- [x] **4.4.2** 实现客户端状态徽标（绿色=在线、黄色=过期、红色=断开）
- [x] **4.4.3** 实现负载进度条

### 4.5 前端 — 指标图表

- [x] **4.5.1** 内嵌 Chart.js（~60KB gzip）
- [x] **4.5.2** 实现任务状态分布饼图
- [x] **4.5.3** 实现任务吞吐折线图（最近 1 小时，按分钟聚合）
- [x] **4.5.4** 实现客户端负载柱状图

### 4.6 实时更新

- [x] **4.6.1** 实现 SSE 连接管理（EventSource on 前端）
- [x] **4.6.2** 实现断线自动重连
- [x] **4.6.3** 实现任务状态变更时自动更新表格行
- [x] **4.6.4** 实现统计数字动画更新

### 4.7 测试

- [x] **4.7.1** 创建 `pkg/reef/server/ui/ui_test.go`
- [x] **4.7.2** 测试：`/ui/` 返回 HTML
- [x] **4.7.3** 测试：`/api/v2/status` 返回正确 JSON
- [x] **4.7.4** 测试：`/api/v2/tasks` 分页和筛选
- [x] **4.7.5** 测试：SSE 连接建立和事件推送
- [x] **4.7.6** 创建 `test/e2e/ui_test.go`：端到端 UI API 测试

### 4.8 文档

- [x] **4.8.1** 创建 `docs/reef/web-ui.md`：使用指南 + 截图说明
- [x] **4.8.2** 更新 `CHANGELOG.md`

---

## Phase 5: 性能基线测试（P0）

### 5.1 测试框架

- [x] **5.1.1** 创建 `test/perf/` 目录
- [x] **5.1.2** 创建 `report.go`：定义 `Report` 结构体 + JSON 序列化
- [x] **5.1.3** 创建 `report.go`：实现 `SaveReport(path string, r Report) error`
- [x] **5.1.4** 创建 `report.go`：实现 `LoadReport(path string) (*Report, error)`
- [x] **5.1.5** 创建 `report.go`：实现 `Compare(baseline, current Report) RegressionReport`
- [x] **5.1.6** 创建 `perf_test.go`：定义 `PerfServer` 测试辅助（复用 E2EServer）
- [x] **5.1.7** 创建 `perf_test.go`：定义 `PerfClient` 批量连接辅助

### 5.2 场景：任务提交吞吐

- [x] **5.2.1** 创建 `scenarios_test.go` `TestPerf_TaskSubmitThroughput_1`
- [x] **5.2.2** 创建 `scenarios_test.go` `TestPerf_TaskSubmitThroughput_10`
- [x] **5.2.3** 创建 `scenarios_test.go` `TestPerf_TaskSubmitThroughput_50`
- [x] **5.2.4** 创建 `scenarios_test.go` `TestPerf_TaskSubmitThroughput_100`
- [x] **5.2.5** 每个场景：并发提交 N 个任务，测量延迟分布

### 5.3 场景：调度延迟

- [x] **5.3.1** 创建 `TestPerf_DispatchLatency`：提交任务 → 测量 Client 收到 dispatch 的时间
- [x] **5.3.2** 运行 100 次取 p50/p95/p99

### 5.4 场景：WebSocket 心跳吞吐

- [x] **5.4.1** 创建 `TestPerf_HeartbeatThroughput_10`
- [x] **5.4.2** 创建 `TestPerf_HeartbeatThroughput_50`
- [x] **5.4.3** 创建 `TestPerf_HeartbeatThroughput_100`
- [x] **5.4.4** 每个场景：所有 Client 同时发心跳，测量 Server 处理吞吐

### 5.5 场景：并发连接建立

- [x] **5.5.1** 创建 `TestPerf_ConcurrentConnect_10`
- [x] **5.5.2** 创建 `TestPerf_ConcurrentConnect_50`
- [x] **5.5.3** 创建 `TestPerf_ConcurrentConnect_100`
- [x] **5.5.4** 每个场景：同时建立 N 个 WebSocket 连接，测量建立延迟

### 5.6 场景：端到端任务完成

- [x] **5.6.1** 创建 `TestPerf_E2ETaskCompletion`：10 Client + 100 任务
- [x] **5.6.2** 测量：提交 → dispatch → 完成的全链路延迟

### 5.7 CI 集成

- [x] **5.7.1** 创建 `test/perf/Makefile`：`make perf` 运行所有基准测试
- [x] **5.7.2** 创建 `test/perf/README.md`：使用说明
- [x] **5.7.3** 创建基线报告：首次运行作为 baseline.json
- [x] **5.7.4** 实现回归检测脚本：对比 baseline vs current

### 5.8 文档

- [x] **5.8.1** 创建 `docs/reef/performance.md`：性能基线报告 + 调优建议
- [x] **5.8.2** 更新 `CHANGELOG.md`

---

## Phase 6: 多 Server 联邦（P3，探索性）

> ⚠️ 此 Phase 复杂度极高，建议作为独立里程碑 v2.1。
> 以下任务为初步分解，实际执行时可能需要进一步细化。

### 6.1 Raft 基础

- [ ] **6.1.1** 添加依赖：`go get github.com/hashicorp/raft github.com/hashicorp/raft-boltdb`
- [ ] **6.1.2** 创建 `pkg/reef/server/federation/` 目录
- [ ] **6.1.3** 创建 `raft.go`：定义 `RaftNode` 结构体
- [ ] **6.1.4** 实现 `NewRaftNode(nodeID, bindAddr, dataDir string)` — 初始化 Raft 节点
- [ ] **6.1.5** 创建 `fsm.go`：实现 `ReefFSM`（Raft 状态机）
- [ ] **6.1.6** 实现 FSM.Apply — 应用日志到状态机
- [ ] **6.1.7** 实现 FSM.Snapshot/Restore — 快照和恢复

### 6.2 跨 Server 通信

- [ ] **6.2.1** 创建 `transport.go`：定义跨 Server RPC 协议
- [ ] **6.2.2** 实现 Leader 代理：非 Leader 节点转发任务请求到 Leader
- [ ] **6.2.3** 实现任务结果回传

### 6.3 任务路由

- [ ] **6.3.1** 创建 `router.go`：定义联邦任务路由器
- [ ] **6.3.2** 实现 Leader 检测：判断当前节点是否为 Leader
- [ ] **6.3.3** 实现请求代理：Follower → Leader 转发

### 6.4 配置集成

- [ ] **6.4.1** 修改 `config_channel.go`：增加 `Federation` 配置块
- [ ] **6.4.2** 修改 `server.go`：可选初始化 Raft 节点
- [ ] **6.4.3** 修改 `gateway.go`：传递联邦配置

### 6.5 测试

- [ ] **6.5.1** 创建 `test/e2e/federation_test.go`
- [ ] **6.5.2** 测试：3 节点集群 Leader 选举
- [ ] **6.5.3** 测试：Leader 故障后新 Leader 选举
- [ ] **6.5.4** 测试：Client 连接到 Follower，任务路由到 Leader
- [ ] **6.5.5** 运行全部测试

### 6.6 文档

- [ ] **6.6.1** 创建 `docs/reef/federation.md`：架构说明 + 部署指南
- [ ] **6.6.2** 更新 `CHANGELOG.md`

---

## Phase 7: 集成与发布

### 7.1 全量测试

 - [x] **7.1.1** 运行所有单元测试：`go test ./pkg/reef/... -v`
 - [x] **7.1.2** 运行所有 E2E 测试：`go test ./test/e2e/... -v`
- [ ] **7.1.3** 运行性能基准：`go test ./test/perf/... -v`
 - [x] **7.1.4** 验证无 flake（3 次运行）

### 7.2 文档整合

 - [x] **7.2.1** 更新 `docs/reef/README.md`：v2.0 特性概览
 - [x] **7.2.2** 更新 `docs/reef/deployment.md`：完整部署指南
 - [x] **7.2.3** 更新 `docs/reef/api.md`：v2 API 变更
 - [x] **7.2.4** 更新 `CHANGELOG.md`：v2.0 完整变更记录
- [ ] **7.2.5** `make lint-docs` 验证

### 7.3 Docker 更新

- [ ] **7.3.1** 更新 `docker/docker-compose.reef.yml`：添加 SQLite volume、TLS 证书 volume
- [ ] **7.3.2** 更新配置模板 JSON 文件
- [ ] **7.3.3** 验证：`docker compose -f docker/docker-compose.reef.yml config`

### 7.4 提交

 - [x] **7.4.1** Git commit：按 Phase 分别提交
- [ ] **7.4.2** Git push
 - [x] **7.4.3** 更新 `.planning/STATE.md`

---

## 任务统计

| Phase | 任务数 | 预估工时 |
|-------|--------|----------|
| 1: 持久化队列 | 42 | 3-4 天 |
| 2: TLS 支持 | 16 | 1-2 天 |
| 3: 多通道通知 | 22 | 2-3 天 |
| 4: Web UI | 32 | 4-5 天 |
| 5: 性能测试 | 22 | 2-3 天 |
| 6: 联邦 | 16 | 5-7 天 |
| 7: 集成发布 | 11 | 1 天 |
| **总计** | **161** | **18-25 天** |

## 建议执行顺序

```
Phase 1 (持久化) ──► Phase 5 (性能测试) ──► Phase 2 (TLS) ──► Phase 3 (通知)
                                                           ──► Phase 4 (UI)
                                                           ──► Phase 7 (集成)
Phase 6 (联邦) 作为独立里程碑 v2.1，不阻塞 v2.0 发布
```

**理由：**
- Phase 1 是基础设施，其他 Phase 依赖它
- Phase 5 紧接 Phase 1，验证持久化后的性能
- Phase 2/3/4 可以并行开发
- Phase 6 复杂度高，建议独立里程碑
