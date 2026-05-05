# Specs: Hermes UI — Production-Grade Multi-Agent Dashboard

> change: reef-hermes-ui
> artifact: specs
> schema: spec-driven
> created: 2026-05-05

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

---

## UI-02: Client List

### UI-02-1: 客户端列表

**Given** one or more clients are registered
**When** the user navigates to /clients
**Then** a table SHALL display columns: ID, Role, Skills (badges), State (colored dot), Load (progress bar), Actions (buttons)

### UI-02-2: 状态过滤

**Given** the client list is displayed
**When** the user selects a state filter (All/Online/Offline/Stale)
**Then** the table SHALL show only clients matching the filter

### UI-02-3: 空状态

**Given** no clients are connected
**When** the user navigates to /clients
**Then** the UI SHALL display an empty state message: "No clients connected"

---

## UI-03: Client Detail

### UI-03-1: 基本信息面板

**Given** a client exists
**When** the user navigates to /clients/{id}
**Then** a panel SHALL display: Client ID, Role, Skills, State, Current Load / Capacity, Last Heartbeat, Current Task (if any)

### UI-03-2: 实时执行流

**Given** the client detail page is open
**When** the client is executing a task
**Then** a scrollable log SHALL display real-time execution events via SSE:
- reasoning_content (thinking chain) as 💭 bubbles
- tool calls as 🔧 with input/output
- read_file as 📖 with filename
- exec as ⚡ with output

### UI-03-3: 执行历史

**Given** the client has completed previous rounds
**When** the user views the client detail
**Then** a history list SHALL show past rounds with tool name, duration, truncated output

### UI-03-4: 操作按钮

**Given** the client detail is open
**When** the user clicks [Pause]
**Then** a POST request SHALL be sent to /api/v2/client/{id}/pause

**Given** the client is paused
**When** the user clicks [Resume]
**Then** a POST request SHALL be sent to /api/v2/client/{id}/resume

**Given** the user clicks [Restart]
**Then** a confirmation dialog SHALL appear before POST to /api/v2/client/{id}/restart

---

## UI-04: Tasks

### UI-04-1: 任务列表

**Given** tasks exist
**When** the user navigates to /tasks
**Then** a paginated table SHALL display: ID, Status (badge), Instruction (truncated 60 chars), Role, Assigned Client, Created At

### UI-04-2: 筛选

**Given** the task list is displayed
**When** the user selects filters (status/role/client)
**Then** the table SHALL update with filtered results

### UI-04-3: 任务详情

**Given** the user clicks a task row
**Then** a modal SHALL display: full instruction, timeline (Created→Queued→Assigned→Running→Completed), attempt history, result/error

### UI-04-4: 提交任务

**Given** the user navigates to /tasks/new
**Then** a form SHALL accept: instruction (textarea), required role (dropdown), skills (multi-select), max retries (number), timeout (number)

### UI-04-5: 任务操作

**Given** a task is in a cancellable state
**When** the user clicks [Cancel]
**Then** a POST request SHALL be sent to cancel the task

---

## UI-05: Hermes Configuration

### UI-05-1: 模式切换

**Given** the user navigates to /hermes
**Then** three radio buttons SHALL be displayed: Full / Coordinator / Executor
The current mode SHALL be pre-selected.

### UI-05-2: 工具白名单

**Given** Coordinator mode is selected
**When** the page renders
**Then** a checkbox list SHALL show all available tools with Coordinator-allowed tools pre-checked

### UI-05-3: Fallback 配置

**Given** the Hermes page is open
**Then** a toggle SHALL control fallback enabled/disabled
**And** a number input SHALL set fallback timeout in milliseconds

### UI-05-4: 团队成员状态

**Given** the Hermes page is open
**Then** a list SHALL show all registered clients with online/offline indicators

---

## UI-06: Configuration

### UI-06-1: Server 配置

**Given** the user navigates to /config
**Then** a form SHALL display: Store Type (memory/sqlite), Store Path, WebSocket Address, Admin Address

### UI-06-2: Notify 配置

**Given** the config page is open
**Then** a multi-entry form SHALL allow adding/removing notification channels with: type (dropdown), URL, token

### UI-06-3: TLS 配置

**Given** the config page is open
**Then** a TLS section SHALL contain: enabled toggle, cert file path, key file path, CA file path (optional)

---

## UI-07: Evolution

### UI-07-1: Gene 列表

**Given** genes exist in the system
**When** the user navigates to /evolution
**Then** a table SHALL display: Gene ID, Role, Control Signal, Status (badge), Submitted At, Actions

### UI-07-2: 审批

**Given** a gene is in "submitted" status
**When** the user clicks [Approve]
**Then** the gene status SHALL change to "approved" via POST /api/v2/evolution/genes/{id}/approve

---

## UI-08: Monitoring

### UI-08-1: 实时日志

**Given** the user navigates to /monitoring
**Then** a scrollable log viewer SHALL display system log entries in real-time via SSE
**And** level filters SHALL allow showing only INFO / WARN / ERROR

### UI-08-2: SSE 事件历史

**Given** the monitoring page is open
**Then** the last 100 SSE events SHALL be visible in a collapsible list

---

## UI-09: Theme

### UI-09-1: 暗色/亮色切换

**Given** the UI is open
**When** the user toggles the theme switch
**Then** all CSS variables SHALL switch between light and dark values
**And** the preference SHALL be stored in localStorage

### UI-09-2: 默认主题

**Given** no theme preference is stored
**When** the UI loads
**Then** the theme SHALL default to dark mode

---

## UI-10: Accessibility & Performance

### UI-10-1: 响应式布局

**Given** the viewport is < 768px
**When** the UI renders
**Then** the sidebar SHALL collapse to a hamburger menu
**And** tables SHALL scroll horizontally

### UI-10-2: SSE 重连

**Given** the SSE connection drops
**When** the EventSource fires onerror
**Then** the UI SHALL attempt reconnection with exponential backoff (1s→2s→4s, max 30s)

### UI-10-3: 事件防抖

**Given** SSE events arrive faster than 500ms
**When** a stats_update is received
**Then** rendering SHALL be throttled to at most 2 updates per second
