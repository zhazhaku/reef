# Tasks: Hermes UI — Production-Grade Multi-Agent Dashboard

> change: reef-hermes-ui
> artifact: tasks
> phase: planning
> created: 2026-05-05

---

## Phase 1: 后端 API 扩展 (P0, 2 days)

### 1.1 Client 详情 API

- [ ] **1.1.1** 定义 `ClientDetailResponse` 结构体（ui.go）
- [ ] **1.1.2** 实现 `handleV2ClientDetail` — GET /api/v2/client/{id}
- [ ] **1.1.3** 注册路由 `/api/v2/client/{id}`
- [ ] **1.1.4** 单元测试: Client 存在/不存在/JSON 格式

### 1.2 Client Session SSE API

- [ ] **1.2.1** 定义 `SessionEvent` 结构体
- [ ] **1.2.2** 实现 `handleV2ClientSession` — SSE 流
- [ ] **1.2.3** 注册路由 `/api/v2/client/{id}/session`
- [ ] **1.2.4** 实现 `EventBus.SubscribeClient(clientID)` — 按 client 过滤的事件订阅
- [ ] **1.2.5** 单元测试: SSE 连接/客户端过滤/断开

### 1.3 Client 操作 API

- [ ] **1.3.1** `handleV2ClientPause` — POST /api/v2/client/{id}/pause
- [ ] **1.3.2** `handleV2ClientResume` — POST /api/v2/client/{id}/resume
- [ ] **1.3.3** `handleV2ClientRestart` — POST /api/v2/client/{id}/restart
- [ ] **1.3.4** 注册路由
- [ ] **1.3.5** 单元测试

### 1.4 Configuration CRUD API

- [ ] **1.4.1** 实现 `handleV2ConfigGet` — GET /api/v2/config
- [ ] **1.4.2** 实现 `handleV2ConfigPut` — PUT /api/v2/config
- [ ] **1.4.3** 注册路由
- [ ] **1.4.4** 单元测试

### 1.5 Hermes Configuration API

- [ ] **1.5.1** 实现 `handleV2HermesGet` — GET /api/v2/hermes
- [ ] **1.5.2** 实现 `handleV2HermesPut` — PUT /api/v2/hermes
- [ ] **1.5.3** 注册路由
- [ ] **1.5.4** 单元测试

### 1.6 Evolution API

- [ ] **1.6.1** 实现 `handleV2GenesList` — GET /api/v2/evolution/genes
- [ ] **1.6.2** 实现 `handleV2GeneApprove` — POST /api/v2/evolution/genes/{id}/approve
- [ ] **1.6.3** 实现 `handleV2GeneReject` — POST /api/v2/evolution/genes/{id}/reject
- [ ] **1.6.4** 注册路由
- [ ] **1.6.5** 单元测试

### 1.7 Logs SSE API

- [ ] **1.7.1** 实现 `handleV2Logs` — GET /api/v2/logs (?level=INFO/WARN/ERROR)
- [ ] **1.7.2** 注册路由
- [ ] **1.7.3** 单元测试

---

## Phase 2: 前端重构 (P1, 3 days)

### 2.1 SPA 架构搭建

- [ ] **2.1.1** 创建 `ui/static/js/app.js` — HashRouter + 全局状态管理
- [ ] **2.1.2** 创建 `ui/static/js/utils.js` — API client, formatTime, formatBytes
- [ ] **2.1.3** 创建 `ui/static/css/theme.css` — CSS 变量 (light/dark)
- [ ] **2.1.4** 创建 `ui/static/css/layout.css` — 侧边栏 + 顶部栏 + 内容区
- [ ] **2.1.5** 创建 `ui/static/css/components.css` — 卡片/表格/按钮/表单/状态徽标

### 2.2 Dashboard 页面

- [ ] **2.2.1** 实现统计卡片组件 (clients/tasks/queue/uptime)
- [ ] **2.2.2** 实现 Chart.js 折线图 (任务吞吐)
- [ ] **2.2.3** 实现 Chart.js 柱状图 (Client 负载)
- [ ] **2.2.4** 实现 Chart.js 饼图 (任务状态分布)
- [ ] **2.2.5** 实现最近任务列表
- [ ] **2.2.6** 实现 SSE 实时统计更新

### 2.3 Clients 页面

- [ ] **2.3.1** 实现 Client 列表表格 (ID/Role/Skills/State/Load/Actions)
- [ ] **2.3.2** 实现 Client 状态过滤器 (Online/Busy/Offline)
- [ ] **2.3.3** 实现 Client 详情页 — 基本信息面板
- [ ] **2.3.4** 实现 Client 详情页 — 当前任务面板
- [ ] **2.3.5** 实现 Client 详情页 — 实时执行流 (SSE)
- [ ] **2.3.6** 实现 Client 详情页 — 执行历史列表
- [ ] **2.3.7** 实现 Client 操作按钮 (Pause/Resume/Restart)

### 2.4 Tasks 页面

- [ ] **2.4.1** 实现任务列表表格 (ID/Status/Role/Client/Created)
- [ ] **2.4.2** 实现任务筛选器 (Status/Role/Priority/Client)
- [ ] **2.4.3** 实现任务分页
- [ ] **2.4.4** 实现任务详情弹窗 (时间线/尝试历史/结果)
- [ ] **2.4.5** 实现提交新任务表单
- [ ] **2.4.6** 实现任务操作 (Cancel/Pause/Resume)

---

## Phase 3: 高级功能 (P2, 2 days)

### 3.1 Hermes 配置页

- [ ] **3.1.1** 实现模式切换器 (Full/Coordinator/Executor radio)
- [ ] **3.1.2** 实现 Fallback 配置 (enable/timeout)
- [ ] **3.1.3** 实现工具白名单编辑器 (checkbox list)
- [ ] **3.1.4** 实现团队成员列表 (在线状态/负载)
- [ ] **3.1.5** 实现 Apply/Reset 按钮 + API 调用

### 3.2 Configuration 页

- [ ] **3.2.1** 实现 Server 配置表单
- [ ] **3.2.2** 实现 Client 配置表单
- [ ] **3.2.3** 实现通知配置表单 (多通道)
- [ ] **3.2.4** 实现 TLS 配置表单
- [ ] **3.2.5** 实现 Save/Load 配置 API 调用

### 3.3 Evolution 页

- [ ] **3.3.1** 实现 Gene 列表 (Status filter)
- [ ] **3.3.2** 实现 Gene 详情弹窗
- [ ] **3.3.3** 实现审批面板 (Approve/Reject)
- [ ] **3.3.4** 实现 Skill Draft 列表

### 3.4 Monitoring 页

- [ ] **3.4.1** 实现实时日志流 (filterable by level)
- [ ] **3.4.2** 实现 SSE 事件历史 (最近 100 条)
- [ ] **3.4.3** 实现性能图表 (throughput/latency/queue depth)

---

## Phase 4: 测试与优化 (P3, 1 day)

### 4.1 测试

- [ ] **4.1.1** 后端 API 集成测试
- [ ] **4.1.2** 前端路由测试
- [ ] **4.1.3** SSE 连接恢复测试
- [ ] **4.1.4** 暗色/亮色主题切换测试

### 4.2 优化

- [ ] **4.2.1** Chart.js 懒加载
- [ ] **4.2.2** SSE 事件防抖 (500ms throttle)
- [ ] **4.2.3** 表格虚拟滚动 (大量任务时)
- [ ] **4.2.4** CSS/JS minification (build step)

### 4.3 文档

- [ ] **4.3.1** 更新 README.md — UI 截图 + 功能说明
- [ ] **4.3.2** 更新 CHANGELOG.md

---

## 任务统计

| Phase | 任务数 | 预估 |
|:---:|:---:|:---:|
| 1: 后端 API | 24 | 2 days |
| 2: 前端重构 | 23 | 3 days |
| 3: 高级功能 | 14 | 2 days |
| 4: 测试优化 | 11 | 1 day |
| **Total** | **72** | **8 days** |
