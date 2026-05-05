# Design: Hermes UI — Production-Grade Multi-Agent Dashboard

> change: reef-hermes-ui
> artifact: design
> phase: design
> created: 2026-05-05

---

## 1. 架构概览

```
┌─────────────────────────────────────────────────────────┐
│                     Browser                              │
│  ┌───────────────────────────────────────────────────┐  │
│  │              Hermes UI (SPA, go:embed)             │  │
│  │  ┌─────────┐ ┌──────────┐ ┌─────────┐ ┌────────┐  │  │
│  │  │Dashboard│ │ Clients  │ │  Tasks   │ │ Config │  │  │
│  │  │ (概览)   │ │ (客户端)  │ │ (任务)   │ │ (配置) │  │  │
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
│  │  /api/v2/client/:id/session → Client 会话内容      │   │
│  │  /api/v2/config → 配置 CRUD                       │   │
│  └──────────────┬───────────────────────────────────┘   │
│                 │                                        │
│  ┌──────────────┴───────────────────────────────────┐   │
│  │   Registry / Scheduler / Queue / Store            │   │
│  │   HermesGuard / EvolutionHub / NotificationMgr   │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
                       │
              ┌────────┴────────┐
              │                 │
         ┌────┴────┐      ┌────┴────┐
         │ Client 1│      │ Client 2│
         │ (coder) │      │(analyst)│
         └─────────┘      └─────────┘
```

## 2. 页面详细设计

### 2.1 Dashboard（总览）

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
│ │ tasks/min             │ │ │ coder-1    ████████ 4/5 │ │
│ │  8 ┤    ╭─╮          │ │ │ analyst-1  ██       1/3 │ │
│ │  6 ┤  ╭─╯ ╰─╮        │ │ │ reviewer-1 ██████   3/3 │ │
│ │  4 ┤╭─╯     ╰─╮      │ │ └──────────────────────────┘ │
│ │  2 ┤╯         ╰────  │ │                              │
│ │  0 └┬──┬──┬──┬──┬──  │ │ ┌──────────────────────────┐ │
│ │    12  1  2  3  4    │ │ │ 任务状态分布 (饼图)       │ │
│ └──────────────────────┘ │ │  Queued    ████ 30%      │ │
│                          │ │  Running   ████ 35%      │ │
│ ┌──────────────────────┐ │ │  Completed ██   20%      │ │
│ │ 最近任务 (5条)        │ │ │  Failed    █    10%      │ │
│ │ task-abc 写代码     ✅│ │ │  Escalated █     5%      │ │
│ │ task-def 数据分析   🔄│ │ └──────────────────────────┘ │
│ │ task-ghi 代码审查   ⏳│ │                              │
│ └──────────────────────┘ │ ┌──────────────────────────┐ │
│                          │ │ 在线 Client 列表          │ │
│ ┌──────────────────────┐ │ │ 🟢 coder-1 (4/5)        │ │
│ │ Client 实时拓扑       │ │ │ 🟢 analyst-1 (1/3)     │ │
│ │  [Server] ─┬─ coder-1 │ │ │ 🟡 reviewer-1 (3/3)    │ │
│ │            ├─ analyst │ │ │ 🔴 tester-1 (offline)  │ │
│ │            └─ review  │ │ └──────────────────────────┘ │
│ └──────────────────────┘ │                              │
└──────────────────────────┴──────────────────────────────┘
```

### 2.2 Clients（客户端管理）

#### Client 列表

| ID | Role | Skills | State | Load | Actions |
|----|------|--------|-------|------|---------|
| coder-1 | coder | go,bash,docker | 🟢 Online | 4/5 | [View] [Pause] [Config] |
| analyst-1 | analyst | python,sql | 🟢 Online | 1/3 | [View] [Pause] [Config] |

#### Client 详情页（实时监控）

```
┌─────────────────────────────────────────────────────────┐
│ ← Back    Client: coder-1  🟢  Role: coder  Load: 4/5 │
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

### 2.3 Hermes 配置页

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

### 2.4 Configuration（配置管理）

- Server: Store Type, Store Path, WS Addr, TLS
- Client: Role, Skills, Model, Temperature, Capacity
- Notify: Type (webhook/slack/smtp/feishu/wecom), URL/Token
- TLS: Enabled, Cert File, Key File

### 2.5 Evolution（进化引擎）

- Gene 列表 (Status: Draft/Submitted/Approved/Rejected)
- 审批面板 (Approve/Reject with reason)
- Skill Draft 列表

### 2.6 Monitoring（监控）

- 实时日志流 (SSE, filterable: INFO/WARN/ERROR)
- 性能指标: Task throughput, Dispatch latency, Queue depth

## 3. API 设计

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
| GET | `/api/v2/logs` | 日志流 (SSE) |

## 4. 前端架构

```
ui/static/
├── index.html          # SPA entry
├── css/
│   ├── theme.css       # dark/light variables
│   ├── layout.css      # grid, sidebar
│   └── components.css  # cards, tables, forms
├── js/
│   ├── app.js          # router, SSE, state
│   ├── dashboard.js    # overview
│   ├── clients.js      # list + detail
│   ├── tasks.js        # list + submit
│   ├── hermes.js       # mode config
│   ├── config.js       # system config
│   ├── evolution.js    # gene management
│   └── monitoring.js   # logs + metrics
└── lib/
    └── chart.js        # Chart.js (60KB)
```

### 路由

```
/                    → dashboard
/clients             → clientList
/clients/:id         → clientDetail
/tasks               → taskList
/tasks/new           → taskSubmit
/hermes              → hermesConfig
/config              → systemConfig
/evolution           → evolution
/monitoring          → monitoring
```

## 5. 实施计划

| Phase | 内容 | 预估 |
|:---:|------|:---:|
| 1 | 后端 API 扩展 (client详情/会话SSE/config CRUD/hermes CRUD) | 2 days |
| 2 | 前端重构 (SPA路由/模块化JS/暗色主题/Client监控/Hermes切换) | 3 days |
| 3 | 高级功能 (配置页/Evolution页/监控日志/Chart.js图表) | 2 days |
| 4 | 测试与优化 (UI测试/兼容性/性能/文档) | 1 day |

---

*设计版本: 1.0 | 2026-05-05*
