# Tasks: Hermes UI — Production-Grade Multi-Agent Dashboard

> change: reef-hermes-ui
> artifact: tasks
> phase: planning
> created: 2026-05-05
> updated: 2026-05-05 (v2.0 — added Board/Chatroom/Decompose/Evolution/Activity)

---

## Phase 1: 后端 API 扩展 (P0, 3 days)

### 1.1 Client API

- [ ] **1.1.1** `handleV2ClientDetail` — GET /api/v2/client/{id}
- [ ] **1.1.2** `handleV2ClientSession` — SSE /api/v2/client/{id}/session
- [ ] **1.1.3** `handleV2ClientPause/Resume/Restart` — POST
- [ ] **1.1.4** 单元测试

### 1.2 Board API [Multica]

- [ ] **1.2.1** `handleV2Board` — GET /api/v2/board (按状态分组)
- [ ] **1.2.2** `handleV2BoardMove` — POST /api/v2/board/move (拖拽)
- [ ] **1.2.3** 单元测试

### 1.3 Chatroom API [Ruflo + Multica]

- [ ] **1.3.1** `handleV2ChatroomMessages` — GET /api/v2/chatroom/{task_id}
- [ ] **1.3.2** `handleV2ChatroomSend` — POST /api/v2/chatroom/{task_id}/send
- [ ] **1.3.3** 单元测试

### 1.4 Task Decomposition API [MetaGPT + CrewAI]

- [ ] **1.4.1** `handleV2TaskDecompose` — GET /api/v2/tasks/{id}/decompose
- [ ] **1.4.2** `handleV2TaskDecomposeCreate` — POST /api/v2/tasks/{id}/decompose
- [ ] **1.4.3** 单元测试

### 1.5 Evolution API [Evolver]

- [ ] **1.5.1** `handleV2GenesList` — GET /api/v2/evolution/genes
- [ ] **1.5.2** `handleV2GeneApprove/Reject` — POST
- [ ] **1.5.3** `handleV2EvolutionStrategy` — GET/PUT /api/v2/evolution/strategy
- [ ] **1.5.4** `handleV2CapsulesList` — GET /api/v2/evolution/capsules
- [ ] **1.5.5** 单元测试

### 1.6 Activity Timeline API [Multica + Evolver]

- [ ] **1.6.1** `handleV2Activity` — GET /api/v2/activity
- [ ] **1.6.2** 单元测试

### 1.7 Config/Hermes/Logs API

- [ ] **1.7.1** `handleV2ConfigGet/Put` — GET/PUT /api/v2/config
- [ ] **1.7.2** `handleV2HermesGet/Put` — GET/PUT /api/v2/hermes
- [ ] **1.7.3** `handleV2Logs` — GET /api/v2/logs (SSE)
- [ ] **1.7.4** 单元测试

---

## Phase 2: 前端核心 (P1, 4 days)

### 2.1 SPA 架构

- [ ] **2.1.1** `app.js` — HashRouter + 全局状态
- [ ] **2.1.2** `utils.js` — API client
- [ ] **2.1.3** `theme.css` — dark/light CSS 变量
- [ ] **2.1.4** `layout.css` — 侧边栏 + 顶部栏
- [ ] **2.1.5** `components.css` — 卡片/表格/按钮/表单/Kanban

### 2.2 Dashboard

- [ ] **2.2.1** 统计卡片
- [ ] **2.2.2** Chart.js 折线图/柱状图/饼图
- [ ] **2.2.3** Kanban 预览 + 进化状态卡片
- [ ] **2.2.4** SSE 实时更新

### 2.3 Board — Kanban 看板 [Multica]

- [ ] **2.3.1** 四列 Kanban (Backlog/InProgress/Review/Done)
- [ ] **2.3.2** 任务卡片 (Agent 头像/状态/优先级)
- [ ] **2.3.3** 拖拽移动 (drag & drop → POST /api/v2/board/move)
- [ ] **2.3.4** 筛选器 (Agent/Role/Priority)

### 2.4 Chatroom — 团队讨论 [Ruflo + Multica]

- [ ] **2.4.1** 消息流 (统一 chatlog, Agent 分轨 bubbles)
- [ ] **2.4.2** Agent 状态面板 (右侧, 实时思考链)
- [ ] **2.4.3** 任务树面板 (右下, 子任务状态)
- [ ] **2.4.4** 消息输入 + 发送
- [ ] **2.4.5** SSE 实时消息推送

### 2.5 Clients

- [ ] **2.5.1** 卡片视图 [Multica] + 表格视图切换
- [ ] **2.5.2** Client 详情页 (基本信息/当前任务/实时执行流)
- [ ] **2.5.3** 操作按钮 (Pause/Resume/Restart)

---

## Phase 3: 前端高级 (P2, 3 days)

### 3.1 Task Decomposition [MetaGPT + CrewAI]

- [ ] **3.1.1** 树形图层层展开
- [ ] **3.1.2** 子任务 Assignee 分配
- [ ] **3.1.3** 状态标识 (Todo/InProgress/Done/Blocked)
- [ ] **3.1.4** 甘特图时间线
- [ ] **3.1.5** 添加子任务表单

### 3.2 Evolution Dashboard [Evolver]

- [ ] **3.2.1** Gene 列表 (ID/Role/Signal/Status)
- [ ] **3.2.2** 进化策略选择器
- [ ] **3.2.3** 进化时间线
- [ ] **3.2.4** 审计追踪 (可回滚)
- [ ] **3.2.5** Capsule 商店

### 3.3 Hermes 配置

- [ ] **3.3.1** 模式切换 (Full/Coordinator/Executor)
- [ ] **3.3.2** 工具白名单编辑器
- [ ] **3.3.3** Fallback 配置

### 3.4 Configuration

- [ ] **3.4.1** Server/Client/Notify/TLS 表单

### 3.5 Activity Timeline [Multica + Evolver]

- [ ] **3.5.1** 全局事件流
- [ ] **3.5.2** 按类型/Agent 过滤
- [ ] **3.5.3** 可搜索/可导出

### 3.6 Monitoring

- [ ] **3.6.1** 实时日志流 (SSE)
- [ ] **3.6.2** 性能图表

---

## Phase 4: 测试与优化 (P3, 1 day)

- [ ] **4.1** 后端 API 集成测试
- [ ] **4.2** 前端路由测试
- [ ] **4.3** SSE 重连测试
- [ ] **4.4** 暗色/亮色主题测试
- [ ] **4.5** 响应式布局测试
- [ ] **4.6** Chart.js 懒加载
- [ ] **4.7** SSE 事件防抖
- [ ] **4.8** 更新 README.md

---

## 任务统计

| Phase | 任务数 | 预估 |
|:---:|:---:|:---:|
| 1: 后端 API | 22 | 3 days |
| 2: 前端核心 | 18 | 4 days |
| 3: 前端高级 | 16 | 3 days |
| 4: 测试优化 | 8 | 1 day |
| **Total** | **64** | **11 days** |
