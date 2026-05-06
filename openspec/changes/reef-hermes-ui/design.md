# Design: Hermes UI — Production-Grade Multi-Agent Dashboard

> change: reef-hermes-ui
> artifact: design
> phase: design
> created: 2026-05-05
> updated: 2026-05-05 (v2.0 — added Multica/Evolver patterns)

---

## 1. 架构概览

```
┌─────────────────────────────────────────────────────────┐
│                     Browser                              │
│  ┌───────────────────────────────────────────────────┐  │
│  │              Hermes UI (SPA, go:embed)             │  │
│  │  ┌─────────┐ ┌──────────┐ ┌─────────┐ ┌────────┐  │  │
│  │  │Dashboard│ │ Board    │ │ Chatroom│ │ Evo    │  │  │
│  │  │ (概览)   │ │ (看板)   │ │ (讨论)  │ │ (进化) │  │  │
│  │  └────┬────┘ └────┬─────┘ └────┬────┘ └───┬────┘  │  │
│  │       └───────────┴────────────┴──────────┘       │  │
│  │  ┌─────────┐ ┌──────────┐ ┌─────────┐ ┌────────┐  │  │
│  │  │ Clients │ │  Tasks   │ │ Config  │ │Monitor │  │  │
│  │  └────┬────┘ └────┬─────┘ └────┬────┘ └───┬────┘  │  │
│  │       └───────────┴────────────┴──────────┘       │  │
│  │                       │                           │  │
│  │              ┌────────┴────────┐                  │  │
│  │              │   SSE EventBus  │  ← 实时更新      │  │
│  │              └─────────────────┘                  │  │
│  └───────────────────────────────────────────────────┘  │
└──────────────────────┬──────────────────────────────────┘
                       │ HTTP + SSE
┌──────────────────────┴──────────────────────────────────┐
│                   Reef Server                            │
│  ┌──────────────────────────────────────────────────┐   │
│  │              ui.Handler                           │   │
│  │  /ui/*          → 静态文件 (go:embed)             │   │
│  │  /api/v2/*      → REST API (JSON)                 │   │
│  │  /api/v2/events → SSE 事件流                      │   │
│  └──────────────┬───────────────────────────────────┘   │
│                 │                                        │
│  ┌──────────────┴───────────────────────────────────┐   │
│  │   Registry / Scheduler / Queue / Store            │   │
│  │   HermesGuard / EvolutionHub / NotificationMgr   │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

## 2. 页面树（左侧导航）

```
Dashboard
├── 总览 (Overview)                    — 统计卡片 + Chart.js + 拓扑
├── Board (团队看板)                   — [Multica] Kanban 拖拽分配
├── Chatroom (团队讨论)               — [Ruflo+Multica] 分轨实时聊天
├── Clients (客户端管理)
│   ├── Client 列表                   — 卡片+表格双视图
│   └── Client 详情                   — 实时执行流 + 思考链 + 操作
├── Tasks (任务管理)
│   ├── 任务列表                      — 筛选/排序/分页
│   ├── 任务详情                      — 时间线/尝试历史/结果
│   ├── 任务分解                      — [MetaGPT+CrewAI] 树形分解
│   └── 提交新任务                    — 表单
├── Evolution (进化引擎)              — [Evolver] Gene 管理 + 策略 + 审计
├── Hermes (模式配置)                 — 三模式切换 + 工具白名单
├── Configuration (系统配置)          — Server/Client/Notify/TLS
├── Monitoring (监控)                 — 日志流 + 性能图表
└── Activity (活动时间线)             — [Multica+Evolver] 全局事件流
```

## 3. 页面详细设计

### 3.1 Dashboard（总览）

```
┌─────────────────────────────────────────────────────────┐
│ 🪸 Reef Dashboard                    Mode: Coordinator  │
│ [Dark/Light] [Refresh: 5s] [Hermes: Coordinator ▾]     │
├─────────────────────────────────────────────────────────┤
│ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐│
│ │Clients │ │ Tasks  │ │ Queue  │ │Uptime  │ │Version ││
│ │   3 🟢 │ │ 12 ✅  │ │   5    │ │ 2h 34m │ │ v2.1.0 ││
│ └────────┘ └────────┘ └────────┘ └────────┘ └────────┘│
├──────────────────────────┬──────────────────────────────┤
│ ┌──────────────────────┐ │ ┌──────────────────────────┐ │
│ │ 任务吞吐 (折线图)     │ │ │ Client 负载 (柱状图)     │ │
│ └──────────────────────┘ │ └──────────────────────────┘ │
│ ┌──────────────────────┐ │ ┌──────────────────────────┐ │
│ │ Kanban 预览 (3列)    │ │ │ 进化状态                 │ │
│ │ Backlog│Active│Done  │ │ │ Strategy: balanced       │ │
│ │  3     │  5   │ 12  │ │ │ Genes: 4  Capsules: 2    │ │
│ └──────────────────────┘ │ └──────────────────────────┘ │
└──────────────────────────┴──────────────────────────────┘
```

### 3.2 Board — 团队看板 [Multica]

```
┌─────────────────────────────────────────────────────────────┐
│ 📋 Team Board                          [Filter] [+ New Task]│
├──────────────┬──────────────┬──────────────┬────────────────┤
│   Backlog    │ In Progress  │   Review     │     Done       │
│              │              │              │                │
│ ┌──────────┐ │ ┌──────────┐ │ ┌──────────┐ │ ┌──────────┐  │
│ │ #42 编码  │ │ │ #38 分析  │ │ │ #35 审查  │ │ │ #30 部署  │  │
│ │ → coder-1│ │ │ →analyst │ │ │ →reviewer│ │ │ → coder-1│  │
│ │ P0 🔴    │ │ │ 🟡 60%   │ │ │ 🟡 80%   │ │ │ ✅ Done   │  │
│ └──────────┘ │ └──────────┘ │ └──────────┘ │ └──────────┘  │
│ ┌──────────┐ │              │              │ ┌──────────┐  │
│ │ #43 测试  │ │              │              │ │ #29 修复  │  │
│ │ → tester │ │              │              │ │ → coder-1│  │
│ │ P1 🟡    │ │              │              │ │ ✅ Done   │  │
│ └──────────┘ │              │              │ └──────────┘  │
└──────────────┴──────────────┴──────────────┴────────────────┘
```

### 3.3 Chatroom — 团队讨论 [Ruflo + Multica]

```
┌─────────────────────────────────────────────────────────────┐
│ 🗣️ Team Chatroom — Task #38          Participants: 3/3    │
├──────────────────────────────┬──────────────────────────────┤
│ ┌──────────────────────────┐ │ Agent Panel                 │
│ │ User: 分析这段代码        │ │ ┌──────────────────────┐   │
│ │                          │ │ │ 🟢 coder-1           │   │
│ │ ─── analyst-1 ────────   │ │ │ 正在分析代码结构       │   │
│ │ 💭 让我从架构开始分析     │ │ │ Round: 3  Token:12k  │   │
│ │ 🔧 exec: grep -r ...    │ │ │                      │   │
│ │ 📖 result: 42 matches   │ │ │ 🟢 tester-1          │   │
│ │                          │ │ │ 等待 coder 完成       │   │
│ │ ─── coder-1 ──────────   │ │ │                      │   │
│ │ 💭 发现性能瓶颈在 line..  │ │ │ 🟡 reviewer-1        │   │
│ │ ⚡ 修复进行中...          │ │ │ 审查中...             │   │
│ │                          │ │ │                      │   │
│ │ ─── reviewer-1 ───────   │ │ │ [全选] [暂停全部]     │   │
│ │ 📋 建议增加缓存层        │ │ │ [提交新任务]          │   │
│ │                          │ │ └──────────────────────┘   │
│ │ [输入消息...] [发送]      │ │                          │
│ └──────────────────────────┘ │ ┌──────────────────────┐   │
│                              │ │ Task Tree            │   │
│                              │ │ 📌 #38 分析          │   │
│                              │ │  ├─ ✅ 架构分析       │   │
│                              │ │  ├─ 🟡 性能优化       │   │
│                              │ │  └─ ⏳ 代码审查       │   │
│                              │ └──────────────────────┘   │
└──────────────────────────────┴──────────────────────────────┘
```

### 3.4 Clients（客户端管理）

#### Client 列表（卡片视图 [Multica]）

```
┌─────────────────────────────────────────────────────────┐
│ 👥 Clients                    [Card View] [Table View]  │
├─────────────────────────────────────────────────────────┤
│ ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│ │ coder-1  │ │analyst-1 │ │tester-1  │ │reviewer-1│    │
│ │ 🟢 Online│ │ 🟢 Online│ │ 🟡 Busy  │ │ 🔴 Offline│   │
│ │ Role:    │ │ Role:    │ │ Role:    │ │ Role:    │    │
│ │ coder    │ │ analyst  │ │ tester   │ │ reviewer │    │
│ │ Skills:  │ │ Skills:  │ │ Skills:  │ │ Skills:  │    │
│ │ go,bash  │ │ python   │ │ jest,go  │ │ review   │    │
│ │ Load:4/5 │ │ Load:1/3 │ │ Load:3/3 │ │ Load:0/0 │    │
│ │ Tasks: 3 │ │ Tasks: 1 │ │ Tasks: 2 │ │ Tasks: 0 │    │
│ │ [View]   │ │ [View]   │ │ [View]   │ │ [View]   │    │
│ └──────────┘ └──────────┘ └──────────┘ └──────────┘    │
└─────────────────────────────────────────────────────────┘
```

#### Client 详情页（实时监控）

```
┌─────────────────────────────────────────────────────────┐
│ ← Back    Client: coder-1  🟢  Role: coder  Load: 4/5 │
│ Skills: go, bash, docker   Tasks Completed: 23          │
│ [Pause] [Restart] [Configure]                          │
├──────────────────────────┬──────────────────────────────┤
│ 当前任务: task-abc        │ 实时执行 (SSE)              │
│ Status: Running           │                              │
│ Rounds: 3/10              │ 💭 reasoning_content live... │
│ Token: 12,450             │ 🔧 tool_call → output       │
│ [Pause] [Cancel]          │ 📖 read_file → content      │
│                           │ 💭 next thought...          │
├──────────────────────────┴──────────────────────────────┤
│ 性能: CPU 12% | Mem 256MB | Tokens 45,230 | Avg 2.3s   │
└─────────────────────────────────────────────────────────┘
```

### 3.5 Task Decomposition — 任务分解 [MetaGPT + CrewAI]

```
┌─────────────────────────────────────────────────────────┐
│ 📋 Task #38: "分析并优化登录性能"       Status: Running │
├─────────────────────────────────────────────────────────┤
│ ┌─── Decomposition Tree ────────────────────────────┐   │
│ │ 📌 分析并优化登录性能                              │   │
│ │  ├─ 📐 架构分析 [analyst-1]        ✅ Done         │   │
│ │  │  ├─ 数据库查询分析                               │   │
│ │  │  └─ API 响应时间分析                              │   │
│ │  ├─ 🎨 性能优化 [coder-1]          🟡 In Progress  │   │
│ │  │  ├─ 缓存层实现                                   │   │
│ │  │  └─ 查询优化                                     │   │
│ │  ├─ 🧪 单元测试 [tester-1]         ⏳ Queued       │   │
│ │  └─ 📊 代码审查 [reviewer-1]       ⏳ Queued       │   │
│ └────────────────────────────────────────────────────┘   │
│                                                         │
│ Timeline: [████████░░░░░░░░░] 30%  ETA: 45min           │
│                                                         │
│ Assignee: [coder-1 ▾]  [Add Sub-task]  [Reassign]      │
└─────────────────────────────────────────────────────────┘
```

### 3.6 Evolution Dashboard — 进化面板 [Evolver]

```
┌─────────────────────────────────────────────────────────┐
│ 🧬 Evolution Dashboard                                   │
│ Strategy: [balanced ▾]  Status: 🟢 Evolving             │
├──────────────────────────┬──────────────────────────────┤
│ ┌──── Gene Library ─────┐│ ┌──── Evolution Timeline ──┐│
│ │ ID     │Role  │Signal ││ │ v2.2 gene-001 +9.4%     ││
│ │gene-001│coder │ 0.85  ││ │ v2.1 gene-004 merged    ││
│ │gene-002│tester│ 0.72  ││ │ v2.0 gene-002 created   ││
│ │gene-003│coder │ 0.45  ││ │ v1.9 repair-only mode   ││
│ │gene-004│revwr │ 0.91  ││ └─────────────────────────┘│
│ └────────────────────────┘│                            │
│                           │ ┌──── Audit Trail ────────┐│
│ ┌──── Capsule Store ─────┐│ │ #42 gene_activation     ││
│ │ 📦 deploy-auto  ⭐4.8  ││ │ gene-001, +9.4% acc     ││
│ │ 📦 db-migration ⭐4.5  ││ │ [View Diff] [Rollback]  ││
│ │ 📦 code-review  ⭐4.9  ││ └─────────────────────────┘│
│ └────────────────────────┘│                            │
│                           │ ┌──── Strategy Config ────┐│
│ [Approve] [Reject]        │ │ balanced: 50/30/20      ││
│ [Submit New Gene]         │ │ innovate: 80/15/5       ││
│                           │ │ harden:   20/40/40      ││
│                           │ │ repair:    0/20/80      ││
│                           │ └─────────────────────────┘│
└──────────────────────────┴──────────────────────────────┘
```

### 3.7 Hermes 配置页

```
┌─────────────────────────────────────────────────────────┐
│ Hermes Mode: ○ Full  ● Coordinator  ○ Executor         │
├─────────────────────────────────────────────────────────┤
│ Fallback: [✓] Enabled  Timeout: [30000] ms              │
│                                                         │
│ Allowed Tools:  [✓] reef_submit  [✓] reef_query        │
│                 [✓] reef_status  [✓] message            │
│                 [✓] reaction     [✓] cron               │
│                 [ ] web_search (disabled)               │
│                                                         │
│ Team: 🟢 coder-1  🟢 analyst-1  🔴 reviewer-1          │
│ [Apply] [Reset]                                         │
└─────────────────────────────────────────────────────────┘
```

### 3.8 Configuration（配置管理）

- Server: Store Type, Store Path, WS Addr, TLS
- Client: Role, Skills, Model, Temperature, Capacity
- Notify: Type (webhook/slack/smtp/feishu/wecom), URL/Token
- TLS: Enabled, Cert File, Key File

### 3.9 Monitoring（监控）

- 实时日志流 (SSE, filterable: INFO/WARN/ERROR)
- 性能指标: Task throughput, Dispatch latency, Queue depth

### 3.10 Activity Timeline — 活动时间线 [Multica + Evolver]

```
┌─────────────────────────────────────────────────────────┐
│ 📊 Activity                        [Filter] [Export]    │
├─────────────────────────────────────────────────────────┤
│ 14:32  🟢 coder-1    started task #38                   │
│ 14:28  💬 analyst-1   commented on #38: "需要数据"       │
│ 14:25  ✅ tester-1    completed task #35                 │
│ 14:20  📋 coder-1    created task #42                   │
│ 14:15  🧬 evolution   gene-001 activated (signal: 0.85) │
│ 14:10  🔄 reviewer-1  started review on #35             │
│ 14:05  ⚡ system      scheduler dispatched #38 → analyst│
│ 14:00  🟢 analyst-1   came online                       │
│                                                         │
│ [Load More...]                                          │
└─────────────────────────────────────────────────────────┘
```

## 4. API 设计

### 4.1 现有 API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v2/status` | 系统状态 |
| GET | `/api/v2/tasks` | 任务列表 |
| GET | `/api/v2/clients` | 客户端列表 |
| GET | `/api/v2/events` | SSE 事件流 |

### 4.2 新增 API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v2/client/{id}` | Client 详情 |
| GET | `/api/v2/client/{id}/session` | 会话实时内容 (SSE) |
| POST | `/api/v2/client/{id}/pause` | 暂停 Client |
| POST | `/api/v2/client/{id}/resume` | 恢复 Client |
| POST | `/api/v2/client/{id}/restart` | 重启 Client |
| GET | `/api/v2/config` | 获取配置 |
| PUT | `/api/v2/config` | 更新配置 |
| GET | `/api/v2/hermes` | Hermes 配置 |
| PUT | `/api/v2/hermes` | 更新 Hermes |
| GET | `/api/v2/evolution/genes` | 基因列表 |
| POST | `/api/v2/evolution/genes/{id}/approve` | 审批基因 |
| POST | `/api/v2/evolution/genes/{id}/reject` | 拒绝基因 |
| GET | `/api/v2/evolution/strategy` | 进化策略 |
| PUT | `/api/v2/evolution/strategy` | 更新进化策略 |
| GET | `/api/v2/evolution/capsules` | Capsule 列表 |
| GET | `/api/v2/board` | 看板数据 (按状态分组) |
| POST | `/api/v2/board/move` | 移动任务到新状态 |
| GET | `/api/v2/chatroom/{task_id}` | 聊天室消息 |
| POST | `/api/v2/chatroom/{task_id}/send` | 发送消息 |
| GET | `/api/v2/tasks/{id}/decompose` | 任务分解树 |
| POST | `/api/v2/tasks/{id}/decompose` | 创建子任务 |
| GET | `/api/v2/activity` | 活动时间线 |
| GET | `/api/v2/logs` | 日志流 (SSE) |

## 5. 前端架构

```
ui/static/
├── index.html          # SPA entry
├── css/
│   ├── theme.css       # dark/light variables
│   ├── layout.css      # grid, sidebar
│   └── components.css  # cards, tables, forms, kanban
├── js/
│   ├── app.js          # router, SSE, state
│   ├── utils.js        # API client, formatTime
│   ├── dashboard.js    # overview
│   ├── board.js        # kanban board [Multica]
│   ├── chatroom.js     # team chatroom [Ruflo]
│   ├── clients.js      # card + table view
│   ├── tasks.js        # list + submit + decompose
│   ├── evolution.js    # genes + strategy + audit [Evolver]
│   ├── hermes.js       # mode config
│   ├── config.js       # system config
│   ├── activity.js     # activity timeline [Multica]
│   └── monitoring.js   # logs + metrics
└── lib/
    └── chart.js        # Chart.js (60KB)
```

### 路由

```
/                    → dashboard
/board               → kanbanBoard
/chatroom/:task_id   → teamChatroom
/clients             → clientList (card/table toggle)
/clients/:id         → clientDetail
/tasks               → taskList
/tasks/new           → taskSubmit
/tasks/:id/decompose → taskDecompose
/evolution           → evolutionDashboard
/hermes              → hermesConfig
/config              → systemConfig
/monitoring          → monitoring
/activity            → activityTimeline
```

## 6. 实施计划

| Phase | 内容 | 预估 |
|:---:|------|:---:|
| 1 | 后端 API 扩展 (22 个新端点) | 3 days |
| 2 | 前端核心 (SPA/主题/Dashboard/Board/Chatroom/Client) | 4 days |
| 3 | 前端高级 (Tasks分解/Evolution/Hermes/Config/Activity) | 3 days |
| 4 | 测试与优化 | 1 day |
| **Total** | | **11 days** |

---

*设计版本: 2.0 | 2026-05-05 | 借鉴: Multica/Evolver/Ruflo/AutoGen Studio/CrewAI/MetaGPT*
