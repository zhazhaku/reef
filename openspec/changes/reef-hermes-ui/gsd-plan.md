# GSD Plan: Hermes UI — Production-Grade Multi-Agent Dashboard

> project: reef-hermes-ui
> methodology: GSD (Get Shit Done)
> created: 2026-05-06
> total tasks: 187
> estimated effort: 15 days

---

## Overview

```
Phase 0: Foundation (基础设施)         — 2 days — 24 tasks
Phase 1: Backend API (后端 API)        — 3 days — 47 tasks
Phase 2: Frontend Core (前端核心)      — 4 days — 52 tasks
Phase 3: Frontend Advanced (前端高级)  — 3 days — 42 tasks
Phase 4: Integration (集成测试)        — 2 days — 16 tasks
Phase 5: Polish (打磨优化)             — 1 day  — 6 tasks
```

---

## Phase 0: Foundation (基础设施)

> 目标：搭建前端 SPA 骨架 + 后端 API 扩展基础
> 预估：2 days | 24 tasks

### Wave 0.1: 前端目录结构 (0.5 day)

- [ ] **0.1.1** 创建 `ui/static/css/` 目录
- [ ] **0.1.2** 创建 `ui/static/js/` 目录
- [ ] **0.1.3** 创建 `ui/static/js/lib/` 目录 (用于 Chart.js)
- [ ] **0.1.4** 下载 Chart.js UMD 到 `ui/static/js/lib/chart.js`
- [ ] **0.1.5** 创建 `ui/static/css/theme.css` — CSS 变量骨架 (light/dark)
- [ ] **0.1.6** 创建 `ui/static/css/layout.css` — 侧边栏 + 顶部栏 + 内容区 grid
- [ ] **0.1.7** 创建 `ui/static/css/components.css` — 卡片/表格/按钮/表单/徽标/进度条基础样式

### Wave 0.2: SPA 路由 + 状态管理 (0.5 day)

- [ ] **0.2.1** 创建 `ui/static/js/utils.js` — `api(method, path, body)` fetch 封装
- [ ] **0.2.2** 创建 `ui/static/js/utils.js` — `formatTime(ts)` 时间格式化
- [ ] **0.2.3** 创建 `ui/static/js/utils.js` — `formatDuration(ms)` 时长格式化
- [ ] **0.2.4** 创建 `ui/static/js/utils.js` — `formatBytes(b)` 字节格式化
- [ ] **0.2.5** 创建 `ui/static/js/utils.js` — `createElement(tag, cls, text)` DOM 工具
- [ ] **0.2.6** 创建 `ui/static/js/app.js` — HashRouter (`#/path` → handler)
- [ ] **0.2.7** 创建 `ui/static/js/app.js` — 全局状态 store (`state.clients`, `state.tasks`, `state.theme`)
- [ ] **0.2.8** 创建 `ui/static/js/app.js` — SSE 连接管理 (`EventSource` + 自动重连 + 指数退避)
- [ ] **0.2.9** 创建 `ui/static/js/app.js` — 主题切换逻辑 (`localStorage` 读写 + CSS class 切换)

### Wave 0.3: 侧边栏 + 顶部栏 (0.5 day)

- [ ] **0.3.1** 创建 `ui/static/index.html` — 重写为 SPA shell (侧边栏 + 内容区 + 顶部栏)
- [ ] **0.3.2** 侧边栏导航项: Dashboard / Board / Chatroom / Clients / Tasks / Evolution / Hermes / Config / Monitoring / Activity
- [ ] **0.3.3** 顶部栏: Logo + Hermes 模式指示 + 主题切换按钮 + 刷新频率选择
- [ ] **0.3.4** 侧边栏折叠逻辑 (viewport < 768px → hamburger menu)
- [ ] **0.3.5** 路由切换时侧边栏高亮当前项

### Wave 0.4: 后端 Handler 扩展基础 (0.5 day)

- [ ] **0.4.1** 在 `ui.go` 中定义 `ClientRegistry.Get(id)` 方法接口
- [ ] **0.4.2** 在 `ui.go` 中定义 `TaskScheduler.GetTask(id)` 方法接口
- [ ] **0.4.3** 在 `ui.go` 中定义 `EvolutionHub` 接口 (Genes/Strategy/Capsules)
- [ ] **0.4.4** 在 `ui.go` 中定义 `ConfigStore` 接口 (Get/Put)
- [ ] **0.4.5** 在 `ui.go` 中定义 `ChatStore` 接口 (Messages/Send)
- [ ] **0.4.6** 在 `ui.go` 中定义 `ActivityStore` 接口 (List/Filter)
- [ ] **0.4.7** 扩展 `EventBus` 支持多频道 (task/client/evolution/activity/chatroom)

---

## Phase 1: Backend API (后端 API)

> 目标：实现全部 22 个新 API 端点
> 预估：3 days | 47 tasks

### Wave 1.1: Client API (0.5 day)

- [ ] **1.1.1** 定义 `ClientDetailResponse` 结构体 (ui.go)
- [ ] **1.1.2** 实现 `handleV2ClientDetail` — GET /api/v2/client/{id}
- [ ] **1.1.3** 实现 client 不存在时返回 404
- [ ] **1.1.4** 注册路由 `/api/v2/client/{id}`
- [ ] **1.1.5** 定义 `SessionEvent` 结构体 (type: reasoning/tool_call/read_file/exec/message)
- [ ] **1.1.6** 实现 `handleV2ClientSession` — SSE /api/v2/client/{id}/session
- [ ] **1.1.7** 实现 `EventBus.SubscribeClient(clientID)` — 按 client 过滤
- [ ] **1.1.8** 实现 `handleV2ClientPause` — POST /api/v2/client/{id}/pause
- [ ] **1.1.9** 实现 `handleV2ClientResume` — POST /api/v2/client/{id}/resume
- [ ] **1.1.10** 实现 `handleV2ClientRestart` — POST /api/v2/client/{id}/restart
- [ ] **1.1.11** 注册路由 pause/resume/restart
- [ ] **1.1.12** 单元测试: client detail (存在/不存在/JSON 格式)
- [ ] **1.1.13** 单元测试: client session SSE (连接/过滤/断开)
- [ ] **1.1.14** 单元测试: client pause/resume/restart

### Wave 1.2: Board API (0.5 day)

- [ ] **1.2.1** 定义 `BoardResponse` 结构体 (backlog/in_progress/review/done 四列)
- [ ] **1.2.2** 定义 `BoardCard` 结构体 (id/instruction/assignee/priority/status/created_at)
- [ ] **1.2.3** 实现 `handleV2Board` — GET /api/v2/board (按状态分组)
- [ ] **1.2.4** 实现 `handleV2BoardMove` — POST /api/v2/board/move (task_id + new_status)
- [ ] **1.2.5** BoardMove 时验证状态转换合法性
- [ ] **1.2.6** BoardMove 成功后 PublishEvent("board_update")
- [ ] **1.2.7** 注册路由
- [ ] **1.2.8** 单元测试: board 列表 (空/有数据/分组正确)
- [ ] **1.2.9** 单元测试: board move (合法/非法转换/不存在)

### Wave 1.3: Chatroom API (0.5 day)

- [ ] **1.3.1** 定义 `ChatMessage` 结构体 (id/task_id/sender/sender_type/content/content_type/timestamp)
- [ ] **1.3.2** 定义 `ChatroomResponse` 结构体 (messages/agents/task_tree)
- [ ] **1.3.3** 实现内存 ChatStore (map[task_id][]ChatMessage)
- [ ] **1.3.4** 实现 `handleV2ChatroomMessages` — GET /api/v2/chatroom/{task_id}
- [ ] **1.3.5** 实现 `handleV2ChatroomSend` — POST /api/v2/chatroom/{task_id}/send
- [ ] **1.3.6** Send 成功后 PublishEvent("chatroom_message")
- [ ] **1.3.7** 注册路由
- [ ] **1.3.8** 单元测试: chatroom messages (空/有消息/分页)
- [ ] **1.3.9** 单元测试: chatroom send (成功/空内容/不存在 task)

### Wave 1.4: Task Decomposition API (0.5 day)

- [ ] **1.4.1** 定义 `TaskNode` 结构体 (id/instruction/assignee/status/children/parent_id)
- [ ] **1.4.2** 在 Task 结构体中添加 `SubTasks []TaskNode` 字段
- [ ] **1.4.3** 实现 `handleV2TaskDecompose` — GET /api/v2/tasks/{id}/decompose
- [ ] **1.4.4** 实现 `handleV2TaskDecomposeCreate` — POST /api/v2/tasks/{id}/decompose
- [ ] **1.4.5** DecomposeCreate 验证 parent task 存在
- [ ] **1.4.6** 注册路由
- [ ] **1.4.7** 单元测试: decompose (无子任务/有子任务/树形结构)
- [ ] **1.4.8** 单元测试: create sub-task (成功/parent 不存在)

### Wave 1.5: Evolution API (0.5 day)

- [ ] **1.5.1** 定义 `GeneResponse` 结构体 (id/role/control_signal/status/created_at)
- [ ] **1.5.2** 定义 `EvolutionStrategyResponse` 结构体 (strategy/innovate/optimize/repair)
- [ ] **1.5.3** 定义 `CapsuleResponse` 结构体 (id/name/role/skill_count/rating)
- [ ] **1.5.4** 实现 `handleV2GenesList` — GET /api/v2/evolution/genes
- [ ] **1.5.5** 实现 `handleV2GeneApprove` — POST /api/v2/evolution/genes/{id}/approve
- [ ] **1.5.6** 实现 `handleV2GeneReject` — POST /api/v2/evolution/genes/{id}/reject
- [ ] **1.5.7** 实现 `handleV2EvolutionStrategy` — GET /api/v2/evolution/strategy
- [ ] **1.5.8** 实现 `handleV2EvolutionStrategy` — PUT /api/v2/evolution/strategy
- [ ] **1.5.9** 实现 `handleV2CapsulesList` — GET /api/v2/evolution/capsules
- [ ] **1.5.10** 注册路由
- [ ] **1.5.11** 单元测试: genes list/approve/reject
- [ ] **1.5.12** 单元测试: strategy get/put
- [ ] **1.5.13** 单元测试: capsules list

### Wave 1.6: Activity + Config + Hermes + Logs API (0.5 day)

- [ ] **1.6.1** 定义 `ActivityEvent` 结构体 (id/type/icon/actor/description/timestamp)
- [ ] **1.6.2** 实现内存 ActivityStore (ring buffer, 最近 1000 条)
- [ ] **1.6.3** 实现 `handleV2Activity` — GET /api/v2/activity (?type=agent/task/evolution/system&limit=50)
- [ ] **1.6.4** 实现 `handleV2ConfigGet` — GET /api/v2/config
- [ ] **1.6.5** 实现 `handleV2ConfigPut` — PUT /api/v2/config
- [ ] **1.6.6** 实现 `handleV2HermesGet` — GET /api/v2/hermes
- [ ] **1.6.7** 实现 `handleV2HermesPut` — PUT /api/v2/hermes
- [ ] **1.6.8** 实现 `handleV2Logs` — GET /api/v2/logs (SSE, ?level=INFO/WARN/ERROR)
- [ ] **1.6.9** 注册所有路由
- [ ] **1.6.10** 单元测试: activity (空/有过滤/分页)
- [ ] **1.6.11** 单元测试: config get/put
- [ ] **1.6.12** 单元测试: hermes get/put
- [ ] **1.6.13** 单元测试: logs SSE

---

## Phase 2: Frontend Core (前端核心)

> 目标：实现 Dashboard / Board / Chatroom / Clients 四个核心页面
> 预估：4 days | 52 tasks

### Wave 2.1: Dashboard 页面 (1 day)

- [ ] **2.1.1** 创建 `ui/static/js/dashboard.js` — `renderDashboard()` 函数
- [ ] **2.1.2** 统计卡片组件: Connected Clients (count + green dot)
- [ ] **2.1.3** 统计卡片组件: Completed Tasks (count + checkmark)
- [ ] **2.1.4** 统计卡片组件: Queue Depth (count)
- [ ] **2.1.5** 统计卡片组件: Uptime (formatted duration)
- [ ] **2.1.6** 统计卡片组件: Server Version
- [ ] **2.1.7** Chart.js 折线图: 任务吞吐 (tasks/min, 最近 60 分钟)
- [ ] **2.1.8** Chart.js 柱状图: Client 负载 (load vs capacity)
- [ ] **2.1.9** Chart.js 饼图: 任务状态分布 (Queued/Running/Completed/Failed/Escalated)
- [ ] **2.1.10** Kanban 预览卡片: 3 列 mini 版 (Backlog/Active/Done 计数)
- [ ] **2.1.11** 进化状态卡片: Strategy + Gene count + Capsule count
- [ ] **2.1.12** 最近任务列表: 最近 5 条 (ID/Status/Instruction/Time)
- [ ] **2.1.13** SSE `stats_update` 事件处理 → 更新所有统计卡片
- [ ] **2.1.14** Chart.js 数据更新逻辑 (每 5 秒追加新数据点)

### Wave 2.2: Board — Kanban 看板 (1 day)

- [ ] **2.2.1** 创建 `ui/static/js/board.js` — `renderBoard()` 函数
- [ ] **2.2.2** 四列 Kanban 布局: Backlog / In Progress / Review / Done
- [ ] **2.2.3** 任务卡片组件: 显示 ID + Instruction (truncated) + Priority badge
- [ ] **2.2.4** 任务卡片组件: Agent 头像/initials + 在线状态 dot
- [ ] **2.2.5** 任务卡片组件: 进度条 (如果 In Progress)
- [ ] **2.2.6** 拖拽实现: HTML5 Drag & Drop API
- [ ] **2.2.7** 拖拽: dragstart 事件 → 设置 drag data (task_id + old_status)
- [ ] **2.2.8** 拖拽: dragover 事件 → 高亮目标列
- [ ] **2.2.9** 拖拽: drop 事件 → 乐观更新 UI + 调用 POST /api/v2/board/move
- [ ] **2.2.10** 拖拽: API 失败时回滚卡片到原位置
- [ ] **2.2.11** 筛选器: Agent 下拉 (All + 各 Agent ID)
- [ ] **2.2.12** 筛选器: Role 下拉 (All + coder/tester/reviewer/analyst)
- [ ] **2.2.13** 筛选器: Priority 下拉 (All + P0/P1/P2)
- [ ] **2.2.14** 筛选器变更时重新渲染 Board
- [ ] **2.2.15** SSE `board_update` 事件处理 → 重新拉取并渲染

### Wave 2.3: Chatroom — 团队讨论 (1 day)

- [ ] **2.3.1** 创建 `ui/static/js/chatroom.js` — `renderChatroom(taskId)` 函数
- [ ] **2.3.2** 页面布局: 左侧消息流 (70%) + 右侧 Agent 面板 (30%)
- [ ] **2.3.3** 消息组件: User 消息 (右侧蓝色气泡)
- [ ] **2.3.4** 消息组件: Agent 消息 (左侧灰色气泡 + Agent 名称 + 头像)
- [ ] **2.3.5** 消息组件: reasoning_content 类型 → 💭 思考链样式 (斜体/折叠)
- [ ] **2.3.6** 消息组件: tool_call 类型 → 🔧 工具调用样式 (可展开 input/output)
- [ ] **2.3.7** 消息组件: read_file 类型 → 📖 文件读取样式 (文件名 + 行数)
- [ ] **2.3.8** 消息组件: exec 类型 → ⚡ 命令执行样式 (命令 + 输出)
- [ ] **2.3.9** 消息组件: message 类型 → 普通文本
- [ ] **2.3.10** Agent 状态面板: 每个 Agent 卡片 (头像/名称/状态/当前活动)
- [ ] **2.3.11** Agent 状态面板: Round count + Token usage
- [ ] **2.3.12** Agent 状态面板: [Pause] [Resume] 按钮
- [ ] **2.3.13** 任务树面板: 右下角显示当前任务的子任务树
- [ ] **2.3.14** 任务树面板: 每个节点显示状态 icon + assignee
- [ ] **2.3.15** 消息输入框: 底部固定 + [Send] 按钮
- [ ] **2.3.16** 消息发送: POST /api/v2/chatroom/{task_id}/send + 乐观更新
- [ ] **2.3.17** SSE `chatroom_message` 事件处理 → 追加消息到列表
- [ ] **2.3.18** 自动滚动到底部 (新消息到达时)

### Wave 2.4: Clients 页面 (1 day)

- [ ] **2.4.1** 创建 `ui/static/js/clients.js` — `renderClients()` 函数
- [ ] **2.4.2** 卡片视图: 网格布局 (responsive, 4列 → 2列 → 1列)
- [ ] **2.4.3** 卡片组件: Client ID + Role + Skills (badges) + State (colored dot)
- [ ] **2.4.4** 卡片组件: Load 进度条 (current/capacity)
- [ ] **2.4.5** 卡片组件: Tasks completed count
- [ ] **2.4.6** 卡片组件: [View] 按钮 → 跳转详情页
- [ ] **2.4.7** 表格视图: 切换按钮 (Card/Table toggle)
- [ ] **2.4.8** 表格视图: 可排序列 (ID/Role/State/Load)
- [ ] **2.4.9** 状态过滤器: All / Online / Offline / Stale
- [ ] **2.4.10** 空状态: "No clients connected" 提示
- [ ] **2.4.11** Client 详情页: `renderClientDetail(clientId)` 函数
- [ ] **2.4.12** Client 详情: 基本信息面板 (ID/Role/Skills/State/Load/Heartbeat)
- [ ] **2.4.13** Client 详情: 当前任务面板 (Task ID/Status/Rounds/Token)
- [ ] **2.4.14** Client 详情: 实时执行流 (SSE, reasoning/tool_call/read_file/exec)
- [ ] **2.4.15** Client 详情: 执行历史列表 (过去 rounds)
- [ ] **2.4.16** Client 详情: 操作按钮 (Pause/Resume/Restart + 确认对话框)
- [ ] **2.4.17** SSE `client_update` 事件处理 → 更新卡片状态
- [ ] **2.4.18** SSE `session_event` 事件处理 → 追加到执行流

---

## Phase 3: Frontend Advanced (前端高级)

> 目标：实现 Tasks / Decomposition / Evolution / Hermes / Config / Activity / Monitoring
> 预估：3 days | 42 tasks

### Wave 3.1: Tasks 页面 (0.5 day)

- [ ] **3.1.1** 创建 `ui/static/js/tasks.js` — `renderTasks()` 函数
- [ ] **3.1.2** 任务列表表格: ID / Status (badge) / Instruction (truncated 60) / Role / Client / Created
- [ ] **3.1.3** 筛选器: Status (Queued/Running/Completed/Failed/Cancelled/Escalated)
- [ ] **3.1.4** 筛选器: Role / Client
- [ ] **3.1.5** 分页: 上一页/下一页 + 页码 + 每页条数选择
- [ ] **3.1.6** 任务详情弹窗: 点击行弹出 modal
- [ ] **3.1.7** 详情弹窗: 完整 instruction + 时间线 (Created→Queued→Assigned→Running→Completed)
- [ ] **3.1.8** 详情弹窗: 尝试历史 (AttemptHistory) + 结果/错误
- [ ] **3.1.9** 详情弹窗: [Cancel] [Pause] [Resume] 操作按钮
- [ ] **3.1.10** 提交新任务页面: `renderTaskSubmit()` 函数
- [ ] **3.1.11** 提交表单: instruction (textarea) / required_role (dropdown) / skills (multi-select)
- [ ] **3.1.12** 提交表单: max_retries (number) / timeout (number)
- [ ] **3.1.13** 提交表单: [Submit] 按钮 → POST /api/v2/tasks + 跳转列表

### Wave 3.2: Task Decomposition 页面 (0.5 day)

- [ ] **3.2.1** 创建 `ui/static/js/tasks.js` — `renderDecompose(taskId)` 函数
- [ ] **3.2.2** 树形组件: 缩进层级 + 连接线
- [ ] **3.2.3** 树节点: Status icon (✅/🟡/⏳/🔴) + Instruction + Assignee badge
- [ ] **3.2.4** 树节点: 点击展开/折叠子节点
- [ ] **3.2.5** 进度条: 总体完成百分比
- [ ] **3.2.6** 甘特图: 水平时间线 (CSS 实现, 无需第三方库)
- [ ] **3.2.7** 甘特图: 每个子任务的起止时间条
- [ ] **3.2.8** [Add Sub-task] 按钮 → 弹出表单
- [ ] **3.2.9** 添加子任务表单: instruction + assignee (dropdown of online Agents)
- [ ] **3.2.10** 添加子任务: POST /api/v2/tasks/{id}/decompose + 刷新树
- [ ] **3.2.11** [Reassign] 按钮 → 选择新 Agent → 更新

### Wave 3.3: Evolution Dashboard (1 day)

- [ ] **3.3.1** 创建 `ui/static/js/evolution.js` — `renderEvolution()` 函数
- [ ] **3.3.2** 页面布局: 左侧 Gene 列表 + 右侧 Timeline/Audit
- [ ] **3.3.3** 进化策略选择器: dropdown (balanced/innovate/harden/repair-only)
- [ ] **3.3.4** 策略变更: PUT /api/v2/evolution/strategy + 确认对话框
- [ ] **3.3.5** Gene 列表表格: ID / Role / Control Signal (进度条) / Status (badge) / Actions
- [ ] **3.3.6** Gene 状态过滤: All / Draft / Submitted / Approved / Rejected
- [ ] **3.3.7** Gene 操作: [Approve] 按钮 → POST approve + 刷新
- [ ] **3.3.8** Gene 操作: [Reject] 按钮 → 弹出原因输入 → POST reject
- [ ] **3.3.9** Gene 操作: [View] 按钮 → 弹出详情 (完整 Gene 内容)
- [ ] **3.3.10** 进化时间线: 垂直时间线 (版本 → 事件 → 结果)
- [ ] **3.3.11** 时间线节点: Gene 激活 / Signal 变化 / Accuracy 提升 / Token 成本
- [ ] **3.3.12** 审计追踪面板: 点击时间线节点 → 显示详情
- [ ] **3.3.13** 审计详情: event type / gene / mutation / result / timestamp
- [ ] **3.3.14** 审计详情: [Rollback] 按钮 (仅对可回滚事件显示)
- [ ] **3.3.15** Capsule 商店: 卡片网格 (name/role/skill_count/rating)
- [ ] **3.3.16** Capsule 商店: [Install] [View] 按钮

### Wave 3.4: Hermes + Configuration (0.5 day)

- [ ] **3.4.1** 创建 `ui/static/js/hermes.js` — `renderHermes()` 函数
- [ ] **3.4.2** 模式切换: 三个 radio 按钮 (Full/Coordinator/Executor)
- [ ] **3.4.3** 当前模式高亮 + API 加载
- [ ] **3.4.4** 工具白名单: checkbox 列表 (所有可用工具)
- [ ] **3.4.5** Coordinator 模式下预选允许的工具
- [ ] **3.4.6** Fallback 配置: toggle + timeout number input
- [ ] **3.4.7** [Apply] 按钮 → PUT /api/v2/hermes + 成功提示
- [ ] **3.4.8** [Reset] 按钮 → 恢复到当前服务器配置
- [ ] **3.4.9** 创建 `ui/static/js/config.js` — `renderConfig()` 函数
- [ ] **3.4.10** Server 配置表单: Store Type / Store Path / WS Addr / Admin Addr
- [ ] **3.4.11** Client 配置表单: Role / Skills / Model / Temperature / Capacity
- [ ] **3.4.12** 通知配置: 多通道 (type dropdown + URL + token + [Add] [Remove])
- [ ] **3.4.13** TLS 配置: toggle + cert/key/ca file path
- [ ] **3.4.14** [Save] 按钮 → PUT /api/v2/config + 成功提示

### Wave 3.5: Activity + Monitoring (0.5 day)

- [ ] **3.5.1** 创建 `ui/static/js/activity.js` — `renderActivity()` 函数
- [ ] **3.5.2** 事件列表: 时间戳 + Icon (🟢/📋/🧬/⚡) + Actor + Description
- [ ] **3.5.3** 过滤器: All / Agent / Task / Evolution / System
- [ ] **3.5.4** 搜索框: keyword 过滤
- [ ] **3.5.5** [Export] 按钮 → 下载 JSON
- [ ] **3.5.6** [Load More] 按钮 → 分页加载
- [ ] **3.5.7** 创建 `ui/static/js/monitoring.js` — `renderMonitoring()` 函数
- [ ] **3.5.8** 实时日志流: SSE /api/v2/logs → 滚动列表
- [ ] **3.5.9** 日志级别过滤: INFO / WARN / ERROR toggle
- [ ] **3.5.10** 日志搜索: keyword 过滤
- [ ] **3.5.11** Chart.js 性能图表: task throughput / dispatch latency / queue depth

---

## Phase 4: Integration (集成测试)

> 目标：端到端测试 + 兼容性验证
> 预估：2 days | 16 tasks

### Wave 4.1: 后端集成测试 (1 day)

- [ ] **4.1.1** 测试: 启动 server → 访问 /ui → 返回 HTML
- [ ] **4.1.2** 测试: GET /api/v2/status → JSON 响应
- [ ] **4.1.3** 测试: GET /api/v2/board → 空板/有数据
- [ ] **4.1.4** 测试: POST /api/v2/board/move → 状态变更
- [ ] **4.1.5** 测试: GET/POST /api/v2/chatroom/{task_id} → 消息收发
- [ ] **4.1.6** 测试: GET /api/v2/tasks/{id}/decompose → 树形结构
- [ ] **4.1.7** 测试: GET/PUT /api/v2/evolution/strategy → 策略切换
- [ ] **4.1.8** 测试: GET /api/v2/activity → 事件列表

### Wave 4.2: 前端测试 (1 day)

- [ ] **4.2.1** 测试: Hash 路由切换 (/ → /board → /clients → ...)
- [ ] **4.2.2** 测试: SSE 连接建立 + 自动重连 (断开后指数退避)
- [ ] **4.2.3** 测试: 暗色/亮色主题切换 (localStorage 持久化)
- [ ] **4.2.4** 测试: Board 拖拽 (drag → drop → API call → 回滚)
- [ ] **4.2.5** 测试: Chatroom 消息发送 + SSE 实时接收
- [ ] **4.2.6** 测试: 响应式布局 (viewport < 768px 侧边栏折叠)
- [ ] **4.2.7** 测试: Chart.js 图表渲染 + 数据更新
- [ ] **4.2.8** 测试: Evolution 策略切换 + Gene 审批

---

## Phase 5: Polish (打磨优化)

> 目标：性能优化 + 文档更新
> 预估：1 day | 6 tasks

### Wave 5.1: 优化 (0.5 day)

- [ ] **5.1.1** Chart.js 懒加载 (仅 Dashboard 页面加载)
- [ ] **5.1.2** SSE 事件防抖 (stats_update 最多 2次/秒)
- [ ] **5.1.3** Board 拖拽性能优化 (requestAnimationFrame)
- [ ] **5.1.4** Chatroom 消息列表虚拟滚动 (超过 100 条时)

### Wave 5.2: 文档 (0.5 day)

- [ ] **5.2.1** 更新 README.md — UI 截图 + 功能说明 + 快速启动
- [ ] **5.2.2** 更新 CHANGELOG.md — v2.1 Hermes UI 新功能

---

## 任务统计

| Phase | Wave 数 | 任务数 | 预估 |
|:---:|:---:|:---:|:---:|
| 0: Foundation | 4 | 24 | 2 days |
| 1: Backend API | 6 | 47 | 3 days |
| 2: Frontend Core | 4 | 52 | 4 days |
| 3: Frontend Advanced | 5 | 42 | 3 days |
| 4: Integration | 2 | 16 | 2 days |
| 5: Polish | 2 | 6 | 1 day |
| **Total** | **23** | **187** | **15 days** |

---

## Dependency Graph

```
Phase 0 (Foundation)
  ├─→ Phase 1 (Backend API) ──→ Phase 4.1 (Backend Tests)
  │       │
  │       ↓
  ├─→ Phase 2 (Frontend Core) ──→ Phase 4.2 (Frontend Tests)
  │       │
  │       ↓
  └─→ Phase 3 (Frontend Advanced) ──→ Phase 5 (Polish)
```

### Critical Path

```
0.2 (SPA Router) → 1.1 (Client API) → 2.4 (Clients Page) → 4.2 (Frontend Tests) → 5.1 (Optimize)
```

### Parallelizable

```
Phase 1 Waves 1.1~1.6 可并行 (无依赖)
Phase 2 Waves 2.1~2.4 可并行 (依赖 Phase 0 + Phase 1 对应 API)
Phase 3 Waves 3.1~3.5 可并行 (依赖 Phase 0 + Phase 1 对应 API)
```

---

## GSD 执行模式 (3-Agent)

| Agent | 职责 | Phase 分配 |
|-------|------|-----------|
| **主 Agent** | 架构决策、代码审查、合并 | 全程 |
| **CodeAgent** | 编码实现 | Phase 0~3 全部 Wave |
| **TestAgent** | 测试验证 | Phase 4 全部 + Phase 1~3 单元测试 |

### spawn 示例

```
# Phase 0
spawn(agent_id="codeagent", label="GSD:phase0-wave0.1", task="创建前端目录结构 + CSS 变量骨架")
spawn(agent_id="codeagent", label="GSD:phase0-wave0.2", task="实现 SPA 路由 + 状态管理 + SSE 连接")

# Phase 1 (并行)
spawn(agent_id="codeagent", label="GSD:phase1-wave1.1", task="实现 Client API (detail/session/pause/resume/restart)")
spawn(agent_id="codeagent", label="GSD:phase1-wave1.2", task="实现 Board API (list/move)")
spawn(agent_id="codeagent", label="GSD:phase1-wave1.3", task="实现 Chatroom API (messages/send)")

# Phase 2 (并行)
spawn(agent_id="codeagent", label="GSD:phase2-wave2.1", task="实现 Dashboard 页面 (统计卡片 + Chart.js)")
spawn(agent_id="codeagent", label="GSD:phase2-wave2.2", task="实现 Board Kanban 看板 (拖拽 + 筛选)")
```

---

*GSD Plan v1.0 | 2026-05-06 | 187 tasks | 23 waves | 15 days*
