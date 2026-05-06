# Research: Multi-Agent UI Design Patterns Analysis

> change: reef-hermes-ui
> artifact: research
> phase: research
> created: 2026-05-05
> updated: 2026-05-05 (added Multica + Evolver, removed n8n)

---

## 1. 研究项目概览

| 项目 | 类型 | 关键 UI 特性 | 来源 |
|------|------|------------|:---:|
| **Multica** ⭐24.9k | Managed Agents 平台 | Agent 即队友看板、Issue 分配、实时进度流、技能复用、多工作区 | [GitHub](https://github.com/multica-ai/multica) |
| **Evolver** ⭐7.2k | GEP 自进化引擎 | Gene/Capsule 管理、进化策略选择、审计追踪、进化排行榜 | [GitHub](https://github.com/EvoMap/evolver) |
| **Ruflo** | Agent 编排平台 | Swarm 拓扑可视化、角色路由、Web UI Beta、多模型聊天 | [GitHub](https://github.com/ruvnet/ruflo) |
| **AutoGen Studio** | 低代码多 Agent | 可视化 Workflow 画布、Agent 画廊、技能管理、会话回溯 | [GitHub](https://github.com/microsoft/autogen) |
| **Langflow** | 低代码 AI 应用 | 拖拽式 Agent 编排、RAG 流水线、组件市场 | [GitHub](https://github.com/langflow-ai/langflow) |
| **CrewAI** | 多 Agent 协作 | 角色定义 YAML、任务分解逻辑、流程可视化 | [GitHub](https://github.com/crewAIInc/crewAI) |
| **MetaGPT** | 多 Agent 协作 | 角色扮演（PM/架构师/工程师）、文档输出 | [GitHub](https://github.com/geekan/MetaGPT) |

---

## 2. Multica 深度分析

### 2.1 核心理念

> "Your next 10 hires won't be human."

Multica 将编码 Agent 变成真正的队友。像分配给同事一样分配给 Agent——它们会自主接手工作、编写代码、报告阻塞问题、更新状态。

命名来源：Multiplexed Information and Computing Agent（致敬 Multics 分时系统）。

### 2.2 关键 UI 特性

```
┌─────────────────────────────────────────────────────────────┐
│ Multica Dashboard                                           │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──── Kanban Board ──────────────────────────────────────┐ │
│  │  Backlog        │  In Progress     │  Done             │ │
│  │ ┌────────────┐  │ ┌────────────┐   │ ┌────────────┐   │ │
│  │ │ Issue #42  │  │ │ Issue #38  │   │ │ Issue #35  │   │ │
│  │ │ → coder-1  │  │ │ → analyst-1│   │ │ → tester-1 │   │ │
│  │ │ [Assign]   │  │ │ 🟡 Running │   │ │ ✅ Complete │   │ │
│  │ └────────────┘  │ │ Progress:60%│   │ └────────────┘   │ │
│  │ ┌────────────┐  │ └────────────┘   │                   │ │
│  │ │ Issue #43  │  │                   │                   │ │
│  │ │ → (none)   │  │                   │                   │ │
│  │ └────────────┘  │                   │                   │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌──── Agent Profiles ────────────────────────────────────┐ │
│  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐  │ │
│  │  │ coder-1  │ │analyst-1 │ │tester-1  │ │reviewer-1│  │ │
│  │  │ 🟢 Active│ │ 🟡 Busy  │ │ 🟢 Active│ │ 🔴 Idle  │  │ │
│  │  │ Skills:  │ │ Skills:  │ │ Skills:  │ │ Skills:  │  │ │
│  │  │ go,bash  │ │ python,sql│ │ jest,go  │ │ review   │  │ │
│  │  │ Issues:3 │ │ Issues:1 │ │ Issues:2 │ │ Issues:0 │  │ │
│  │  │ Skills:12│ │ Skills:8 │ │ Skills:5 │ │ Skills:3 │  │ │
│  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘  │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌──── Activity Timeline ─────────────────────────────────┐ │
│  │  14:32  coder-1  started Issue #38                     │ │
│  │  14:28  analyst-1  commented on Issue #38: "需要数据"    │ │
│  │  14:25  tester-1  completed Issue #35 ✅                │ │
│  │  14:20  coder-1  created Issue #42                     │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 2.3 对 Reef 的借鉴价值

| Multica 特性 | Reef 现状 | 借鉴方向 |
|-------------|:--------:|---------|
| **Kanban 看板** | 无 | 任务按状态分列展示，Agent 头像标识 |
| **Agent 即队友** | Client 列表 | Agent 有个人档案、技能徽标、活跃度 |
| **Issue 分配** | 手动提交任务 | 拖拽分配任务给 Agent |
| **实时进度流** | SSE 基础 | WebSocket 双向 + 进度条 |
| **技能复用** | 无 | 每个解决方案沉淀为可复用技能 |
| **活动时间线** | 无 | 类似 GitHub Activity 的事件流 |
| **多工作区** | 无 | 按团队/项目隔离 |

### 2.4 Multica 架构

```
┌──────────────┐     ┌──────────────┐     ┌──────────────────┐
│ Next.js 16   │────>│ Go 后端      │────>│ PostgreSQL 17    │
│ 前端         │<────│ (Chi + WS)   │<────│ (pgvector)       │
└──────────────┘     └──────┬───────┘     └──────────────────┘
                            │
                     ┌──────┴───────┐
                     │ Agent Daemon │  运行在用户机器上
                     └──────────────┘  (Claude Code, Codex, Hermes...)
```

---

## 3. Evolver 深度分析

### 3.1 核心理念

> "进化不是可选项，而是生存法则。"

Evolver 是基于 GEP（Gene Expression Programming）协议的 AI 智能体自进化引擎。它把零散的 prompt 调优变成可审计、可复用的进化资产。

### 3.2 关键概念

| 概念 | 说明 |
|------|------|
| **Gene** | 紧凑的策略表示（≤200行），比 Skill 文档更稳定的进化载体 |
| **Capsule** | 封装的进化资产包，可复用、可共享 |
| **EvolutionEvent** | 可审计的进化事件记录 |
| **Mutation** | 每次进化必须显式声明的突变类型 |
| **PersonalityState** | 可进化的人格状态 |

### 3.3 进化策略

| 策略 | 创新 | 优化 | 修复 | 适用场景 |
|:---:|:---:|:---:|:---:|---------|
| `balanced` | 50% | 30% | 20% | 日常运行 |
| `innovate` | 80% | 15% | 5% | 快速出新功能 |
| `harden` | 20% | 40% | 40% | 大改动后稳定 |
| `repair-only` | 0% | 20% | 80% | 紧急修复 |

### 3.4 Evolver UI 模式（推断自 CLI + EvoMap 网络）

```
┌─────────────────────────────────────────────────────────────┐
│ 🧬 Evolution Dashboard                                      │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Strategy: [balanced ▾]  Status: 🟢 Evolving               │
│                                                             │
│  ┌──── Gene Library ──────────────────────────────────────┐ │
│  │  ID        │ Role     │ Signal  │ Status    │ Actions  │ │
│  │  gene-001  │ coder    │ 0.85    │ ✅ Active  │ [View]   │ │
│  │  gene-002  │ tester   │ 0.72    │ ✅ Active  │ [View]   │ │
│  │  gene-003  │ coder    │ 0.45    │ 🟡 Draft   │ [Edit]   │ │
│  │  gene-004  │ reviewer │ 0.91    │ ✅ Active  │ [View]   │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌──── Evolution Timeline ────────────────────────────────┐ │
│  │  v2.2 ──── gene-001 activated ──── +9.4% accuracy      │ │
│  │  v2.1 ──── gene-004 merged ─────── signal: 0.91        │ │
│  │  v2.0 ──── gene-002 created ────── from error pattern  │ │
│  │  v1.9 ──── repair-only mode ───── 3 fixes applied      │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌──── Audit Trail ───────────────────────────────────────┐ │
│  │  [EvolutionEvent #42]                                   │ │
│  │  Time: 2026-05-05 14:30                                │ │
│  │  Type: gene_activation                                  │ │
│  │  Gene: gene-001 (coder, signal: 0.85)                  │ │
│  │  Mutation: optimize                                     │ │
│  │  Result: token_cost ↓ 15%, accuracy ↑ 9.4%             │ │
│  │  [View Diff] [Rollback]                                │ │
│  └─────────────────────────────────────────────────────────┘ │
│                                                             │
│  ┌──── Capsule Store ─────────────────────────────────────┐ │
│  │  📦 deploy-automation  │ coder  │ 5 skills │ ⭐ 4.8    │ │
│  │  📦 db-migration       │ coder  │ 3 skills │ ⭐ 4.5    │ │
│  │  📦 code-review-v2     │reviewer│ 4 skills │ ⭐ 4.9    │ │
│  └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────┘
```

### 3.5 对 Reef 的借鉴价值

| Evolver 特性 | Reef 现状 | 借鉴方向 |
|-------------|:--------:|---------|
| **Gene 管理面板** | Evolution Hub API | 可视化 Gene 库 + 信号强度 + 状态 |
| **进化策略选择** | StrategyBalanced 等 | UI 下拉切换策略 + 实时生效 |
| **审计追踪** | 无 | 每次进化事件的时间线 + 可回滚 |
| **Capsule 商店** | 无 | 可复用技能包的浏览/安装/发布 |
| **进化排行榜** | 无 | Agent 进化得分排名 |
| **Token 成本曲线** | 无 | 展示"token 先升后降"的进化特征 |

---

## 4. Ruflo 深度分析

### 4.1 核心架构

```
User → Ruflo (CLI/MCP) → Router → Swarm → Agents → Memory → LLM Providers
                       ↑                          ↓
                       └──── Learning Loop ←──────┘
```

### 4.2 关键 UI 特性

- **Swarm 拓扑可视化**：mesh/hierarchical/ring/star 四种拓扑
- **角色路由**：Q-Learning Router + MoE (8 Experts)
- **Web UI Beta**：flo.ruv.io 多模型聊天界面
- **100+ Agent 角色**：coder/tester/reviewer/architect/security...
- **27 Hooks**：生命周期事件钩子
- **RuVector 智能层**：SONA/EWC++/Flash Attention/HNSW

### 4.3 对 Reef 的借鉴价值

| Ruflo 特性 | Reef 现状 | 借鉴方向 |
|-----------|:--------:|---------|
| **拓扑可视化** | 无 | 图形化展示 Agent 连接关系 |
| **角色路由** | Scheduler 基础 | 可视化路由决策过程 |
| **Learning Loop** | Evolution Hub | 自优化循环的可视化 |
| **Hook 系统** | 无 | 生命周期事件的可视化配置 |

---

## 5. AutoGen Studio 深度分析

### 5.1 核心 UI 特性

- **Agent Gallery**：预设 Agent 模板，一键导入
- **Workflow 画布**：可视化拖拽编排多 Agent 工作流
- **技能管理**：上传/启用/禁用/版本管理
- **会话回溯**：查看任意历史会话的完整消息链
- **Prompt 编辑器**：在线编辑 Agent 系统提示词

### 5.2 对 Reef 的借鉴价值

| AutoGen 特性 | Reef 现状 | 借鉴方向 |
|-------------|:--------:|---------|
| **Agent Gallery** | 无 | 预设角色模板库 |
| **会话回溯** | 无 | 历史会话完整回放 |
| **Prompt 编辑** | 无 | 在线编辑系统提示词 |
| **技能管理** | 无 | 技能的 CRUD + 版本 |

---

## 6. CrewAI + MetaGPT + Langflow

### 6.1 CrewAI

- **角色定义 YAML**：每个 Agent 有 role/goal/backstory
- **任务分解**：复杂任务自动拆解为子任务
- **流程可视化**：sequential/hierarchical 两种流程

### 6.2 MetaGPT

- **角色扮演**：PM/Architect/Engineer/QA 各司其职
- **文档驱动**：PRD → Design → Code → Test → Review
- **标准化输出**：每个角色产出标准化文档

### 6.3 Langflow

- **拖拽式编排**：节点连线构建 Agent 流水线
- **组件市场**：社区贡献的预设组件
- **实时预览**：编排即预览，所见即所得

---

## 7. Reef Hermes UI 核心缺欠（更新）

| 功能 | 当前 Reef UI | Multica | Evolver | Ruflo | AutoGen | 差距 |
|------|:----------:|:-------:|:-------:|:-----:|:-------:|:----:|
| **Agent 即队友** | 列表 | ✅ 看板 | — | — | ✅ 画廊 | 🔴 |
| **Kanban 看板** | 无 | ✅ | — | — | — | 🔴 |
| **任务分配** | 手动 | ✅ 拖拽 | — | — | — | 🔴 |
| **并行讨论** | 无 | ✅ 评论 | — | ✅ 聊天 | — | 🔴 |
| **任务分解** | 列表 | ✅ Issue | — | — | — | 🔴 |
| **Gene 管理** | API | — | ✅ | — | — | 🔴 |
| **进化策略** | 代码 | — | ✅ CLI | — | — | 🔴 |
| **审计追踪** | 无 | — | ✅ | — | — | 🔴 |
| **Swarm 拓扑** | 无 | — | — | ✅ | — | 🔴 |
| **技能复用** | 无 | ✅ | ✅ Capsule | — | ✅ | 🔴 |
| **活动时间线** | 无 | ✅ | ✅ Events | — | — | 🔴 |
| **会话监控** | SSE | ✅ WS | — | ✅ | ✅ | 🟡 |

---

## 8. 推荐新增 UI 页面（更新）

### P1 — Team Board（团队看板，借鉴 Multica）

```
/board
```

- Kanban 看板：Backlog → In Progress → Review → Done
- Agent 头像标识任务归属
- 拖拽分配任务给 Agent
- 实时状态更新

### P1 — Team Chatroom（团队聊天室，借鉴 Ruflo + Multica）

```
/team/{task_id}
```

- 中央消息流（统一 chatlog + 每个 Agent 的回答分轨）
- 右侧 Agent 状态面板（谁在干什么、实时思考链）
- Agent 互评/提及 (@coder-1)
- 任务上下文自动关联

### P1 — Task Decomposition（任务分解，借鉴 MetaGPT + CrewAI）

```
/tasks/:id/decompose
```

- 树形图层层展开
- 每个子任务 Assignee 分配
- 状态标识 (Todo/InProgress/Done/Blocked)
- 时序甘特图

### P1 — Evolution Dashboard（进化面板，借鉴 Evolver）

```
/evolution
```

- Gene 库管理（ID/Role/Signal/Status）
- 进化策略选择器（balanced/innovate/harden/repair-only）
- 进化时间线（版本 → 事件 → 结果）
- 审计追踪（每次进化可追溯、可回滚）
- Capsule 商店（可复用技能包）

### P2 — Agent Builder（Agent 构建器，借鉴 AutoGen Studio）

```
/agents/new
/agents/:id/edit
```

- Role 选择/创建
- System Prompt 编辑器 (带模板)
- Skills 选择 (多选)
- Model 参数配置
- 预设模板库（coder/tester/reviewer/analyst...）

### P2 — Swarm Topology（Swarm 拓扑，借鉴 Ruflo）

```
/swarm
```

- 图形化展示所有 Agent 及其连接关系
- Role 标识 (Coordinator/Executor/Full)
- 实时流量/负载指示
- 拓扑切换 (mesh/hierarchical/ring/star)

### P2 — Activity Timeline（活动时间线，借鉴 Multica + Evolver）

```
/activity
```

- 全局事件流（类似 GitHub Activity）
- 按 Agent/Task/类型过滤
- 可搜索、可导出

---

## 9. 增量设计修改

| 现有页面 | 新增内容（借鉴来源） |
|---------|---------|
| Dashboard | Kanban 概览卡片 (Multica)、进化状态卡片 (Evolver)、活跃团队概览 |
| Client 详情 | Agent 个人档案 (Multica)、技能徽标、思考分轨视图 |
| Tasks | Kanban 视图 (Multica)、分解树 (MetaGPT)、甘特图 |

---

## 10. 与 Reef 现有架构的融合点

| Reef 组件 | 融合方式 |
|----------|---------|
| `pkg/reef/server/ui/` | 扩展现有 Handler，新增 API 端点 |
| `pkg/reef/evolution/` | Gene/Capsule 数据直接对接 Evolution Dashboard |
| `pkg/reef/server/scheduler.go` | 任务状态变更推送到 Kanban SSE |
| `pkg/agent/hermes.go` | Hermes 模式切换对接 UI 配置页 |
| `EventBus` | 扩展为支持多频道（task/client/evolution/activity） |

---

*研究版本: 2.0 | 来源: Multica/Evolver/Ruflo/AutoGen Studio/CrewAI/MetaGPT/Langflow*
*更新: 移除 n8n，新增 Multica (⭐24.9k) 和 Evolver (⭐7.2k) 深度分析*
