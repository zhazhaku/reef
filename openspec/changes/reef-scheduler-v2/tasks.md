---
change: reef-scheduler-v2
artifact: tasks
created: 2026-04-29
status: in-progress
---

# Tasks: Reef Scheduler v2

> 每个 task 是一个可独立编译/测试的原子单元。
> Phase 0-1 已在 reef-v2.0 中完成，此处标记为 [x] 作为参考。

---

## Phase 0: 数据目录统一 ✅

> 已在 reef-v2.0 Phase 1.6 / reef-scheduler-v2 design §0 实施。

- [x] **0.1** 修改 `pkg/config/envkeys.go`：`GetHome()` 优先级 PICOCLAW_HOME > 可执行文件目录 > ~/.picoclaw
- [x] **0.2** 实现 `isWritableDir(dir string) bool` 辅助函数
- [x] **0.3** 修改 Reef Server DefaultConfig：StorePath 基于 GetHome()
- [x] **0.4** 编译 + 测试：`go build ./...`

---

## Phase 1: TaskStore 持久化 ✅

> 已在 reef-v2.0 Phase 1 全部完成。

- [x] **1.1** TaskStore 接口定义 (`store/store.go`)
- [x] **1.2** MemoryStore 实现 (`store/memory.go` + test)
- [x] **1.3** SQLiteStore 实现 (`store/sqlite.go` + test)
- [x] **1.4** PersistentQueue 集成 (`queue_persistent.go` + test)
- [x] **1.5** Server 集成 (Config.StoreType/StorePath)
- [x] **1.6** 配置集成 (SwarmSettings + CLI args)

---

## Phase 2: 非阻塞优先级调度器（P0）✅

> 替代当前 FIFO Queue，引入优先级、非阻塞扫描、可插拔策略、超时检测。

### 2.1 PriorityQueue 实现

- [x] **2.1.1** 创建 `pkg/reef/server/priority_queue.go`：定义 `PriorityQueue` 结构体
- [x] **2.1.2** 实现 `container/heap` 接口：`Len/Less/Swap/Push/Pop`
- [x] **2.1.3** 实现 `Enqueue(task) error` — 入队时使用 `-priority` 保证高优先级先出
- [x] **2.1.4** 实现 `Dequeue() *Task` — 从堆顶弹出
- [x] **2.1.5** 实现 `Peek() *Task` — 查看堆顶不移除
- [x] **2.1.6** 实现 `Len() int` — 队列长度
- [x] **2.1.7** 实现 `Snapshot() []*Task` — 返回当前队列副本（用于 admin）
- [x] **2.1.8** 实现 `Scan(matchFn func(*Task) bool) []*Task` — 非阻塞扫描匹配任务
- [x] **2.1.9** 实现 `Remove(taskID string) bool` — 按 ID 移除
- [x] **2.1.10** 实现 `BoostStarvation(threshold, boost, maxPriority)` — 饥饿动态提升
- [x] **2.1.11** 创建 `priority_queue_test.go`：覆盖入队/出队/优先级/饥饿/并发
- [x] **2.1.12** 编译 + 测试：`go test ./pkg/reef/server/... -run PriorityQueue -v`

### 2.2 MatchStrategy 可插拔策略

- [x] **2.2.1** 创建 `pkg/reef/server/strategy.go`：定义 `MatchStrategy` 接口
- [x] **2.2.2** 实现 `LeastLoadStrategy` — 选择 CurrentLoad 最小的 Client
- [x] **2.2.3** 实现 `RoundRobinStrategy` — 轮询分配
- [x] **2.2.4** 实现 `AffinityStrategy` — 选择历史成功率最高的 Client
- [x] **2.2.5** 创建 `strategy_test.go`：覆盖 3 种策略的选择逻辑
- [x] **2.2.6** 编译 + 测试：`go test ./pkg/reef/server/... -run Strategy -v`

### 2.3 TimeoutScanner 超时检测

- [x] **2.3.1** 创建 `pkg/reef/server/timeout_scanner.go`：定义 `TimeoutScanner` 结构体
- [x] **2.3.2** 实现 `NewTimeoutScanner(scheduler, store, interval)` — 构造函数
- [x] **2.3.3** 实现 `Run(ctx)` — 定时扫描 Running 任务，超时标记 Failed(timeout)
- [x] **2.3.4** 实现 `Stop()` — 优雅停止
- [x] **2.3.5** 创建 `timeout_scanner_test.go`：模拟超时场景
- [x] **2.3.6** 编译 + 测试

### 2.4 Scheduler 集成

- [x] **2.4.1** 修改 `scheduler.go`：`TaskQueue` 替换为 `PriorityQueue`
- [x] **2.4.2** 修改 `TryDispatch()`：使用非阻塞 Scan 匹配任务到 Client
- [x] **2.4.3** 修改 `scheduler.go`：集成 `MatchStrategy`，通过 `Config.Strategy` 选择
- [x] **2.4.4** 修改 `HandleTaskCompleted`：添加幂等检查（终态任务静默忽略重复提交）
- [x] **2.4.5** 修改 `HandleTaskFailed`：添加幂等检查
- [x] **2.4.6** 修改 `server.go`：`Start()` 中启动 TimeoutScanner goroutine
- [x] **2.4.7** 修改 `server.go`：`Stop()` 中停止 TimeoutScanner
- [x] **2.4.8** 运行全部 scheduler 测试：`go test ./pkg/reef/server/... -v`

### 2.5 配置集成

- [x] **2.5.1** 修改 `config_channel.go`：`SwarmSettings` 增加 `Strategy`, `DefaultTimeoutMs`, `TimeoutScanIntervalSec`, `StarvationThresholdMs`
- [x] **2.5.2** 修改 `gateway.go`：传递新配置到 Server Config
- [x] **2.5.3** 修改 `cmd/picoclaw/internal/reef/command.go`：CLI 增加 `--strategy`/`--task-timeout` 参数

---

## Phase 3: Gateway 集成 + 任务分发（P1）🟡

> 完善 GatewayBridge、ReplyTo 上下文追踪、结果回传。协调工具已部分完成。

### 3.1 GatewayBridge 完善

 - [x] **3.1.1** 修改 `pkg/reef/server/gateway.go`（不存在则创建）：定义 `GatewayBridge` 结构体
 - [x] **3.1.2** 实现 `NewGatewayBridge(cfg GatewayConfig, pCfg *config.Config, scheduler *Scheduler)` — 初始化 MessageBus + AgentLoop
 - [x] **3.1.3** 实现 `Start(ctx)` — 启动频道监听器（复用 picoclaw channel manager）
 - [x] **3.1.4** 实现 `Stop(ctx)` — 优雅关闭
 - [x] **3.1.5** 创建 `gateway_test.go`：测试 GatewayBridge 生命周期
 - [x] **3.1.6** 编译 + 测试

### 3.2 ReplyTo 上下文追踪

- [x] **3.2.1** 修改 `pkg/reef/task.go`：Task 结构体增加 `ReplyTo *ReplyToContext` 字段
- [x] **3.2.2** 修改 `pkg/reef/task.go`（或新建 `pkg/reef/reply_to.go`）：定义 `ReplyToContext` 结构体
- [x] **3.2.3** 修改 `pkg/reef/protocol.go`：`TaskDispatchPayload` 增加 `ReplyTo` 字段
- [ ] **3.2.4** 修改 `pkg/reef/server/store/store.go`：增加 `SaveReplyTo`/`GetReplyTo` 方法
- [ ] **3.2.5** 修改 `pkg/reef/server/store/sqlite.go`：实现新方法 + 创建 `task_reply_to` 表
- [ ] **3.2.6** 修改 `pkg/reef/server/store/memory.go`：实现新方法
 - [x] **3.2.7** 修改 `pkg/tools/reef_tools.go`：`reef_submit_task` 从 InboundMessage 提取来源并存入 ReplyTo
 - [x] **3.2.8** 修改 `pkg/channels/swarm/swarm.go`：`dispatchTask` 携带 reply_to
- [x] **3.2.9** 创建 `reply_to_test.go`：测试 ReplyTo 上下文持久化 + 透传

### 3.3 结果回传

- [x] **3.3.1** 修改 `pkg/reef/server/scheduler.go`：`HandleTaskCompleted` 时检查 ReplyTo，触发回传
- [x] **3.3.2** 实现 `buildResultMessage(task, result)` — 构造 InboundMessage 发给 GatewayBridge.AgentLoop
- [x] **3.3.3** 实现 AgentLoop 聚合回复 → MessageBus outbound → 频道回传
- [x] **3.3.4** 实现单任务完成直接回传
- [x] **3.3.5** 实现任务失败时发送失败通知
- [x] **3.3.6** 创建 `result_delivery_test.go`：E2E 验证结果回传链路

### 3.4 Task 模型扩展

- [x] **3.4.1** 修改 `pkg/reef/task.go`：增加 `Priority int`（1-10），默认 5
- [x] **3.4.2** 修改 `pkg/reef/task.go`：增加 `ParentTaskID string`
- [x] **3.4.3** 修改 `pkg/reef/task.go`：增加 `SubTaskIDs []string`
- [x] **3.4.4** 修改 `pkg/reef/task.go`：增加 `Dependencies []string`
- [x] **3.4.5** 修改 `pkg/reef/task.go`：增加新状态 `TaskBlocked`, `TaskRecovering`, `TaskAggregating`
- [x] **3.4.6** 修改 `pkg/reef/server/store/sqlite.go`：tasks 表增加新列、新索引
- [x] **3.4.7** 修改 `pkg/reef/server/admin.go`：`/tasks` 接口支持 priority 参数

---

## Phase 4: DAG Engine + 结果聚合（P1）✅

### 4.1 DAG Engine 核心

- [x] **4.1.1** 创建 `pkg/reef/server/dag_engine.go`：定义 `DAGEngine` 结构体
- [x] **4.1.2** 实现 `NewDAGEngine(store, scheduler, logger)` — 构造函数
- [x] **4.1.3** 实现 `CreateSubTasks(parentTask, plan)` — 创建子任务并记录父子关系
- [x] **4.1.4** 实现 `OnSubTaskCompleted(subTaskID)` — 检查依赖，解除阻塞
- [x] **4.1.5** 实现 `OnSubTaskFailed(subTaskID)` — 依赖任务标记 Failed(dependency_failed)
- [x] **4.1.6** 实现 `CheckUnblock(parentTaskID)` — 扫描 Blocked 子任务，依赖满足时变 Queued
- [x] **4.1.7** 实现 `OnAllSubTasksCompleted(parentTaskID)` — 触发结果聚合
- [x] **4.1.8** 实现 `BuildAggregationMessage(parentTaskID)` — 收集子任务结果构造 InboundMessage
- [x] **4.1.9** 创建 `dag_engine_test.go`：
  - 创建父子任务 → 验证关系
  - 子任务完成 → 验证依赖解除
  - 子任务失败 → 验证失败传播
  - 全部完成 → 验证聚合消息
- [x] **4.1.10** 编译 + 测试

### 4.2 Store 扩展（task_relations）

- [x] **4.2.1** 修改 `pkg/reef/server/store/store.go`：增加 `SaveRelation(parentID, childID, dependency)` / `GetSubTaskIDs(parentID)`
- [x] **4.2.2** 修改 `pkg/reef/server/store/sqlite.go`：创建 `task_relations` 表 + 实现方法
- [x] **4.2.3** 修改 `pkg/reef/server/store/memory.go`：实现方法
- [x] **4.2.4** 修改 `pkg/reef/server/store/sqlite.go`：新增 `UpdateTaskStatus(id, status, fields)` 支持部分更新
- [x] **4.2.5** 修改 `pkg/reef/server/store/sqlite.go`：新增 `ListActiveTasks()` 查询非终态任务

### 4.3 Server 集成

- [x] **4.3.1** 修改 `server.go`：`NewServer` 中初始化 DAGEngine
- [x] **4.3.2** 修改 `scheduler.go`：`HandleTaskCompleted` 调用 `dagEngine.OnSubTaskCompleted`
- [x] **4.3.3** 修改 `scheduler.go`：`HandleTaskFailed` 调用 `dagEngine.OnSubTaskFailed`
- [x] **4.3.4** 修改 `pkg/reef/server/recovery.go`（不存在则创建）：实现 `RecoveryManager`
  - 启动时从 Store 恢复非终态任务
  - Running 重置为 Queued 或 Recovering
  - Blocked 保持，等待依赖
  - Aggregating 检查子任务状态
- [x] **4.3.5** 修改 `server.go`：`Start()` 中调用 RecoveryManager.Recover

### 4.4 AgentLoop 聚合集成

- [x] **4.4.1** 修改 `pkg/reef/server/gateway.go`：接收聚合消息并路由到 AgentLoop
- [x] **4.4.2** 修改 `pkg/tools/reef_tools.go`：`reef_submit_task` 支持 `decompose` 参数触发 DAG
- [x] **4.4.3** 创建 `aggregation_test.go`：E2E 测试分解→执行→聚合→回传链路

---

## Phase 5: Web UI 统一（P2） ✅

### 5.1 Reef 前端页面（picoclaw Web 前端）

- [x] **5.1.1** 创建 `web/frontend/src/routes/reef.tsx`：Reef 布局路由（含 Tab 导航）
- [x] **5.1.2** 创建 `web/frontend/src/routes/reef/index.tsx`：Overview 页面
  - Server 信息卡片（版本、运行时间）
  - 实时统计卡片（queued/running/completed/failed）
  - 客户端状态摘要
  - 最近 10 条任务列表
- [x] **5.1.3** 创建 `web/frontend/src/routes/reef/tasks.tsx`：Tasks 页面
  - 表格展示（ID/Status/Role/CreatedAt/Client）
  - 状态/角色筛选下拉框
  - 分页控件
  - 任务详情弹窗
  - 任务提交表单
  - 取消按钮
- [x] **5.1.4** 创建 `web/frontend/src/routes/reef/clients.tsx`：Clients 页面
  - 表格展示（ID/Role/Skills/Status/Load/Heartbeat）
  - 状态徽标（online/stale/disconnected）
  - 负载进度条
- [x] **5.1.5** 创建 `web/frontend/src/components/reef/overview-cards.tsx`：统计卡片组件
- [x] **5.1.6** 创建 `web/frontend/src/components/reef/task-table.tsx`：任务表格组件
- [x] **5.1.7** 创建 `web/frontend/src/components/reef/task-detail-sheet.tsx`：任务详情抽屉
- [x] **5.1.8** 创建 `web/frontend/src/components/reef/client-table.tsx`：Client 表格组件
- [x] **5.1.9** 创建 `web/frontend/src/components/reef/task-submit-dialog.tsx`：提交任务对话框
- [x] **5.1.10** 创建 `web/frontend/src/components/reef/use-reef-events.ts`：SSE 事件 Hook
- [x] **5.1.11** 修改 `web/frontend/src/components/app-sidebar.tsx`：新增 Reef 导航组
- [x] **5.1.12** 创建 `web/frontend/src/api/reef.ts`：Reef API 客户端

### 5.2 Reef API 端点（picoclaw Web 后端）

- [x] **5.2.1** 修改 `web/backend/api/router.go`：注册 `registerReefRoutes`
- [x] **5.2.2** 实现 `GET /api/reef/status` — 返回服务器状态 JSON
- [x] **5.2.3** 实现 `GET /api/reef/tasks` — 任务列表（分页、筛选）
- [x] **5.2.4** 实现 `GET /api/reef/tasks/:id` — 任务详情
- [x] **5.2.5** 实现 `GET /api/reef/tasks/:id/subtasks` — 子任务列表
- [x] **5.2.6** 实现 `GET /api/reef/clients` — Client 列表
- [x] **5.2.7** 实现 `GET /api/reef/events` — SSE 事件流
- [x] **5.2.8** 创建 `web/backend/api/reef_test.go`：测试所有 Reef API 端点

### 5.3 降级保留

- [x] **5.3.1** 保留 `pkg/reef/server/ui/` 独立 UI 代码不变
- [ ] **5.3.2** 验证独立 `/ui` 入口功能正常
- [ ] **5.3.3** 验证 SSE 事件流包含 stats_update / task_created / task_completed / task_failed / client_connected / client_disconnected

---

## Phase 6: 集成与文档（P2）✅

### 6.1 E2E 回归测试

- [x] **6.1.1** 运行全部 reef E2E 测试：`go test ./test/e2e/... -v`
- [x] **6.1.2** 运行性能基准：`go test ./test/perf/... -v`
- [x] **6.1.3** 运行代码检查：`go vet ./pkg/reef/...`
- [x] **6.1.4** 验证无 flake（3 次运行）

### 6.2 文档

- [x] **6.2.1** 更新 `docs/reef/README.md`：v2 特性概览
- [ ] **6.2.2** 更新 `docs/reef/deployment.md`：数据目录、优先级、策略配置
- [ ] **6.2.3** 更新 `docs/reef/api.md`：新增 API 端点文档
- [ ] **6.2.4** 更新 `CHANGELOG.md`：v2.0 scheduler 升级条目
- [ ] **6.2.5** Git commit：按 Phase 分别提交（2/3/4/5/6）

---

## 任务统计

| Phase | 任务数 | 预估工时 | 状态 |
|-------|--------|----------|------|
| 0: 数据目录统一 | 4 | 0.5d | ✅ |
| 1: TaskStore 持久化 | 6 | 3-4d | ✅ |
| 2: 优先级调度器 | 27 | 2-3d | ✅ |
| 3: Gateway + ReplyTo | 15 | 1-2d | 🟡 |
| 4: DAG Engine | 20 | 2-3d | ✅ |
| 5: Web UI 统一 | 20 | 2-3d | ✅ |
| 6: 集成发布 | 9 | 1d | ✅ |
| **Phase 1-6 合计** | **97** | **11-15d** | — |
| **总计（含0）** | **101** | **11.5-15.5d** | — |

## 建议执行顺序

```
Phase 2 (优先级调度器) ──► Phase 3 (Gateway 集成) ──► Phase 4 (DAG Engine)
                                                        │
                                                        ▼
                                              Phase 5 (Web UI 统一) ──► Phase 6 (集成发布)
```

**理由：**
- Phase 2 是基础设施升级，Phase 3/4 依赖它
- Phase 4 依赖 Phase 3 的 ReplyTo 上下文
- Phase 5 依赖 Phase 3/4 的 API（status/tasks/clients）
- Phase 6 是最终验证和发布
