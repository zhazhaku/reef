# Specs: Hermes UI — Production-Grade Multi-Agent Dashboard

> change: reef-hermes-ui
> artifact: specs
> schema: spec-driven
> created: 2026-05-05
> updated: 2026-05-05 (v2.0 — added Board/Chatroom/Decompose/Evolution/Activity specs)

---

## UI-01: Dashboard Overview

### UI-01-1: 统计卡片

**Given** the server is running with connected clients and tasks
**When** the user opens the Dashboard
**Then** the UI SHALL display five statistics cards:
- Connected Clients (count + green dot)
- Completed Tasks (count + checkmark)
- Queue Depth (count of queued tasks)
- Uptime (formatted duration)
- Server Version (version string)

### UI-01-2: 实时统计更新

**Given** the Dashboard is visible
**When** an SSE `stats_update` event is received
**Then** all statistics cards SHALL update their values within 500ms

### UI-01-3: 吞吐量折线图

**Given** the server has been running for >1 minute
**When** the Dashboard renders
**Then** a Chart.js line chart SHALL show task throughput per minute for the last 60 minutes

### UI-01-4: 负载柱状图

**Given** there are connected clients
**When** the Dashboard renders
**Then** a Chart.js bar chart SHALL show each client's load vs capacity

### UI-01-5: 状态分布饼图

**Given** there are tasks in various states
**When** the Dashboard renders
**Then** a Chart.js pie chart SHALL show percentage distribution of task statuses

### UI-01-6: Kanban 预览 + 进化状态

**Given** the Dashboard is visible
**Then** a mini Kanban preview SHALL show task counts per status column
**And** an evolution status card SHALL show current strategy, gene count, capsule count

---

## UI-02: Board — Kanban 看板 [Multica]

### UI-02-1: 四列看板

**Given** tasks exist in various states
**When** the user navigates to /board
**Then** a four-column Kanban board SHALL display: Backlog, In Progress, Review, Done
**And** each column SHALL show task cards with Agent avatar, title, priority badge

### UI-02-2: 拖拽分配

**Given** a task card is in one column
**When** the user drags it to another column
**Then** the UI SHALL call POST /api/v2/board/move with the new status
**And** the card SHALL move visually before the API responds (optimistic update)
**And** if the API fails, the card SHALL revert to its original position

### UI-02-3: Agent 头像标识

**Given** a task is assigned to an Agent
**When** the task card renders
**Then** the Agent's avatar/initials SHALL appear on the card
**And** the Agent's online status dot SHALL be shown

### UI-02-4: 筛选器

**Given** the Board is displayed
**When** the user selects filters (Agent/Role/Priority)
**Then** only matching task cards SHALL be visible

---

## UI-03: Chatroom — 团队讨论 [Ruflo + Multica]

### UI-03-1: 消息流

**Given** a task has associated chat messages
**When** the user navigates to /chatroom/{task_id}
**Then** a scrollable message list SHALL display messages with:
- Sender name + avatar
- Timestamp
- Message content (text, reasoning_content as 💭, tool_call as 🔧)
- Agent messages SHALL be visually distinguished from user messages

### UI-03-2: Agent 状态面板

**Given** the Chatroom is open
**Then** a right-side panel SHALL show each participating Agent with:
- Online/offline status
- Current activity (thinking/executing/waiting)
- Round count and token usage
- [Pause] [Resume] buttons

### UI-03-3: 任务树面板

**Given** the task has sub-tasks (decomposition)
**When** the Chatroom is open
**Then** a bottom-right panel SHALL show the task decomposition tree
**And** each sub-task SHALL show its status and assignee

### UI-03-4: 消息发送

**Given** the Chatroom is open
**When** the user types a message and clicks [Send]
**Then** the message SHALL be sent via POST /api/v2/chatroom/{task_id}/send
**And** the message SHALL appear in the message list immediately (optimistic)

### UI-03-5: 实时推送

**Given** the Chatroom is open
**When** an Agent produces new output (reasoning, tool call, message)
**Then** the message SHALL appear in the message list within 1 second via SSE

---

## UI-04: Clients

### UI-04-1: 卡片视图 [Multica]

**Given** clients are registered
**When** the user navigates to /clients
**Then** a card grid SHALL display each client with:
- ID, Role, Skills (badges), State (colored dot)
- Load (progress bar), Tasks completed count
- [View] button

### UI-04-2: 表格视图

**Given** the user clicks [Table View]
**Then** the clients SHALL switch to a table layout with sortable columns

### UI-04-3: Client 详情 — 实时执行流

**Given** a client is executing a task
**When** the user views /clients/{id}
**Then** a scrollable log SHALL display real-time execution events via SSE:
- reasoning_content as 💭 bubbles
- tool calls as 🔧 with input/output
- read_file as 📖 with filename
- exec as ⚡ with output

### UI-04-4: 操作按钮

**Given** the client detail is open
**When** the user clicks [Pause] / [Resume] / [Restart]
**Then** the corresponding API call SHALL be made

---

## UI-05: Tasks

### UI-05-1: 任务列表

**Given** tasks exist
**When** the user navigates to /tasks
**Then** a paginated table SHALL display: ID, Status (badge), Instruction, Role, Client, Created

### UI-05-2: 筛选与分页

**Given** the task list is displayed
**When** the user selects filters (status/role/client)
**Then** the table SHALL update with filtered results

### UI-05-3: 任务详情

**Given** the user clicks a task row
**Then** a modal SHALL display: full instruction, timeline, attempt history, result/error

### UI-05-4: 提交任务

**Given** the user navigates to /tasks/new
**Then** a form SHALL accept: instruction, required role, skills, max retries, timeout

---

## UI-06: Task Decomposition [MetaGPT + CrewAI]

### UI-06-1: 分解树

**Given** a task has sub-tasks
**When** the user navigates to /tasks/{id}/decompose
**Then** a tree view SHALL display the task hierarchy with:
- Indented sub-tasks
- Assignee badge per node
- Status icon (✅ Done / 🟡 InProgress / ⏳ Queued / 🔴 Blocked)

### UI-06-2: 添加子任务

**Given** the decomposition view is open
**When** the user clicks [Add Sub-task]
**Then** a form SHALL accept: instruction, assignee (dropdown of online Agents)
**And** POST /api/v2/tasks/{id}/decompose SHALL create the sub-task

### UI-06-3: 甘特图时间线

**Given** sub-tasks have start/end times
**When** the decomposition view renders
**Then** a horizontal timeline SHALL show task durations as bars
**And** the overall progress percentage SHALL be displayed

### UI-06-4: 重新分配

**Given** a sub-task is assigned to an Agent
**When** the user clicks [Reassign] and selects a different Agent
**Then** the sub-task's assignee SHALL update via API

---

## UI-07: Evolution Dashboard [Evolver]

### UI-07-1: Gene 列表

**Given** genes exist in the system
**When** the user navigates to /evolution
**Then** a table SHALL display: Gene ID, Role, Control Signal (bar), Status (badge), Actions

### UI-07-2: 进化策略选择器

**Given** the Evolution page is open
**Then** a dropdown SHALL allow selecting: balanced / innovate / harden / repair-only
**And** the current strategy SHALL be highlighted
**And** selecting a new strategy SHALL call PUT /api/v2/evolution/strategy

### UI-07-3: 进化时间线

**Given** evolution events exist
**When** the Evolution page renders
**Then** a vertical timeline SHALL show version milestones with:
- Gene activations
- Signal changes
- Accuracy improvements
- Token cost trajectories

### UI-07-4: 审计追踪

**Given** an evolution event exists
**When** the user clicks on an event
**Then** a detail panel SHALL show: event type, gene, mutation, result, timestamp
**And** a [Rollback] button SHALL be available for reversible events

### UI-07-5: Capsule 商店

**Given** capsules are available
**When** the user views the Capsule section
**Then** a card grid SHALL display: name, role, skill count, rating
**And** [Install] [View] buttons SHALL be available

### UI-07-6: Gene 审批

**Given** a gene is in "submitted" status
**When** the user clicks [Approve] or [Reject]
**Then** the corresponding API call SHALL update the gene status

---

## UI-08: Hermes Configuration

### UI-08-1: 模式切换

**Given** the user navigates to /hermes
**Then** three radio buttons SHALL be displayed: Full / Coordinator / Executor
**And** the current mode SHALL be pre-selected

### UI-08-2: 工具白名单

**Given** Coordinator mode is selected
**When** the page renders
**Then** a checkbox list SHALL show all available tools with Coordinator-allowed tools pre-checked

### UI-08-3: Fallback 配置

**Given** the Hermes page is open
**Then** a toggle SHALL control fallback enabled/disabled
**And** a number input SHALL set fallback timeout in milliseconds

---

## UI-09: Configuration

### UI-09-1: Server 配置

**Given** the user navigates to /config
**Then** a form SHALL display: Store Type, Store Path, WebSocket Address, Admin Address

### UI-09-2: Notify 配置

**Given** the config page is open
**Then** a multi-entry form SHALL allow adding/removing notification channels

### UI-09-3: TLS 配置

**Given** the config page is open
**Then** a TLS section SHALL contain: enabled toggle, cert/key file paths

---

## UI-10: Activity Timeline [Multica + Evolver]

### UI-10-1: 全局事件流

**Given** events have occurred (task state changes, Agent actions, evolution events)
**When** the user navigates to /activity
**Then** a chronological event list SHALL display events with:
- Timestamp
- Icon (🟢 Agent / 📋 Task / 🧬 Evolution / ⚡ System)
- Actor (Agent ID or "system")
- Description

### UI-10-2: 过滤

**Given** the Activity page is displayed
**When** the user selects a filter (All/Agent/Task/Evolution/System)
**Then** only matching events SHALL be visible

### UI-10-3: 搜索与导出

**Given** the Activity page is open
**Then** a search box SHALL filter events by keyword
**And** an [Export] button SHALL download events as JSON

---

## UI-11: Monitoring

### UI-11-1: 实时日志

**Given** the user navigates to /monitoring
**Then** a scrollable log viewer SHALL display system log entries in real-time via SSE
**And** level filters SHALL allow showing only INFO / WARN / ERROR

### UI-11-2: 性能图表

**Given** the Monitoring page is open
**Then** charts SHALL show: task throughput, dispatch latency, queue depth over time

---

## UI-12: Theme & Accessibility

### UI-12-1: 暗色/亮色切换

**Given** the UI is open
**When** the user toggles the theme switch
**Then** all CSS variables SHALL switch between light and dark values
**And** the preference SHALL be stored in localStorage

### UI-12-2: 默认主题

**Given** no theme preference is stored
**When** the UI loads
**Then** the theme SHALL default to dark mode

### UI-12-3: 响应式布局

**Given** the viewport is < 768px
**When** the UI renders
**Then** the sidebar SHALL collapse to a hamburger menu
**And** tables SHALL scroll horizontally
**And** Kanban columns SHALL stack vertically

### UI-12-4: SSE 重连

**Given** the SSE connection drops
**When** the EventSource fires onerror
**Then** the UI SHALL attempt reconnection with exponential backoff (1s→2s→4s, max 30s)

### UI-12-5: 事件防抖

**Given** SSE events arrive faster than 500ms
**When** a stats_update is received
**Then** rendering SHALL be throttled to at most 2 updates per second
