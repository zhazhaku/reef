# Research: Multi-Agent UI Design Patterns Analysis

> change: reef-hermes-ui
> artifact: research
> phase: research
> created: 2026-05-05

---

## 1. 研究项目概览

| 项目 | 类型 | 关键 UI 特性 | 开源 |
|------|------|------------|:---:|
| **Ruflo** | Agent 编排平台 | Swarm 拓扑可视化、角色路由、Web UI Beta、多模型聊天 | ✅ |
| **AutoGen Studio** | 低代码多 Agent | 可视化 Workflow 画布、Agent 画廊、技能管理、会话回溯 | ✅ |
| **Langflow** | 低代码 AI 应用 | 拖拽式 Agent 编排、RAG 流水线、组件市场 | ✅ |
| **CrewAI** | 多 Agent 协作 | 角色定义 YAML、任务分解逻辑、流程可视化 | ✅ |
| **MetaGPT** | 多 Agent 协作 | 角色扮演（PM/架构师/工程师）、文档输出 | ✅ |
| **n8n** | 工作流自动化 | 节点编排、条件分支、调试面板、版本管理 | ✅ |

---

## 2. 核心 UI 模式分析

### 2.1 多 Agent 角色定义 UI（Ruflo + AutoGen Studio）

**Ruflo 模式：**
```
┌─────────────────────────────────────────────┐
│  Swarm Orchestration            + [Create]   │
├─────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐           │
│  │ Role: coder  │  │ Role: tester│           │
│  │ Skills: go   │  │ Skills: go  │           │
│  │ Model: GPT-4 │  │ Model: GPT-4│           │
│  │ Status: 🟢   │  │ Status: 🟢  │           │
│  │ [Edit][Del]  │  │ [Edit][Del] │           │
│  └─────────────┘  └─────────────┘           │
│                                             │
│  Topology: [Mesh ▾] Consensus: [Raft ▾]     │
│                                             │
│  ┌──── Agent Topology ──────────────────┐   │
│  │    (visual graph: coder ↔ tester)    │   │
│  └──────────────────────────────────────┘   │
└─────────────────────────────────────────────┘
```

**AutoGen Studio 模式：**
```
┌─────────────────────────────────────────────┐
│  Agent Gallery                   + [New]    │
├─────────────────────────────────────────────┤
│  ┌──────────┐ ┌──────────┐ ┌──────────┐    │
│  │ Coder    │ │ Analyst  │ │ Reviewer │    │
│  │ 🟢 Ready │ │ 🟢 Ready │ │ 🟡 Busy  │    │
│  │ [Use]    │ │ [Use]    │ │ [Use]    │    │
│  └──────────┘ └──────────┘ └──────────┘    │
│                                             │
│  ┌──────────────────────────────────────┐   │
│  │ Agent Config (inline panel)          │   │
│  │ Name: [________]  Role: [________]   │   │
│  │ System Prompt: [textarea]            │   │
│  │ Skills: [multi-select]               │   │
│  │ Model: [model dropdown]              │   │
│  │ Temperature: [slider 0-1]            │   │
│  │ [Save] [Test] [Delete]               │   │
│  └──────────────────────────────────────┘   │
└─────────────────────────────────────────────┘
```

### 2.2 并行讨论/聊天 UI（Ruflo + 多 Agent 聊天室）

```
┌─────────────────────────────────────────────────────┐
│ 🗣️ Team Chatroom              Participants: 3/3    │
├──────────────────────────┬──────────────────────────┤
│ ┌──────────────────────┐ │ Agents Panel            │
│ │ User: 分析这段代码     │ │ ┌──────────────────┐  │
│ │                      │ │ │ 🟢 coder-1        │  │
│ │ ─── Consultant ────  │ │ │ 正在分析代码结构    │  │
│ │ 💭 让我从架构开始分析  │ │ │                  │  │
│ │ 🔧 exec: 代码扫描     │ │ │ 🟢 tester-1       │  │
│ │                      │ │ │ 等待 coder 完成   │  │
│ │ ─── PM ────────────  │ │ │                  │  │
│ │ 📋 任务优先级: P0     │ │ │ 🟡 reviewer-1    │  │
│ │                      │ │ │ 审查中...          │  │
│ │ ─── Coder ──────────  │ │ │                  │  │
│ │ ⚡ 修复进行中...       │ │ │ [全选] [暂停全部]  │  │
│ │                      │ │ │ [提交新任务]       │  │
│ │ [输入消息...] [发送]   │ │ └──────────────────┘  │
│ └──────────────────────┘ │                      │
└──────────────────────────┴──────────────────────────┘
```

### 2.3 任务分解可视化（MetaGPT + CrewAI）

```
┌─────────────────────────────────────────────────────┐
│ 📋 Task: "实现用户登录功能"        Status: Running   │
├─────────────────────────────────────────────────────┤
│ ┌─── Decomposition Tree ────────────────────────┐   │
│ │ 📌 用户登录功能                                │   │
│ │  ├─ 📐 架构设计 [reviewer]      ✅ Done        │   │
│ │  ├─ 🎨 前端登录页面 [coder]     🟡 In Progress │   │
│ │  │  ├─ 登录表单组件                            │   │
│ │  │  └─ 表单验证逻辑                            │   │
│ │  ├─ 🔧 后端 API [coder]         ⏳ Queued      │   │
│ │  │  ├─ POST /api/login                        │   │
│ │  │  └─ JWT Token 签发                         │   │
│ │  ├─ 🧪 单元测试 [tester]        ⏳ Queued      │   │
│ │  └─ 📊 安全审计 [security]      ⏳ Queued      │   │
│ └─────────────────────────────────────────────────┘   │
│                                                       │
│ Timeline: [████████░░░░░░░░░] 30%  ETA: 45min         │
└─────────────────────────────────────────────────────┘
```

---

## 3. Reef Hermes UI 核心缺欠

| 模式 | 当前 Reef UI | 行业最佳实践 | 差距 |
|------|:----------:|:----------:|:----:|
| **Agent 角色定义** | 列表展示 | 卡片+内联配置+画廊 | 🔴 大幅缺失 |
| **角色并行讨论** | 无 | 分屏聊天室+Agent 状态面板 | 🔴 完全缺失 |
| **任务分解视图** | 列表 | 树形分解+Assignee+状态 | 🔴 完全缺失 |
| **Swarm 拓扑** | 无 | 可视化拓扑图 | 🔴 完全缺失 |
| **Agent 会话监控** | SSE 简单流 | 分轨聊天bubbles+思考链 | 🟡 需增强 |

---

## 4. 推荐新增 UI 页面

### P1 — Team Chatroom（团队聊天室）

```
/team/{task_id}     ── 单任务的团队协作室
/team               ── 所有活跃团队概览
```

功能:
- 中央消息流（统一 chatlog + 每个 Agent 的回答分轨）
- 右侧 Agent 状态面板（谁在干什么、实时思考链）
- 任务分解树 (Task Tree)
- Agent 互评/提及 (@coder-1)

### P1 — Task Decomposition（任务分解）

```
/tasks/:id/decompose
```

功能:
- 树形图层层展开
- 每个子任务 Assignee 分配
- 状态标识 (Todo/InProgress/Done/Blocked)
- 时序甘特图

### P2 — Swarm Topology（Swarm 拓扑）

```
/swarm
```

功能:
- 图形化展示所有 Agent 及其连接关系
- Role 标识 (Coordinator/Executor/Full)
- 实时流量/负载指示

### P2 — Agent Builder（Agent 构建器）

```
/agents/new
/agents/:id/edit
```

功能:
- Role 选择/创建
- System Prompt 编辑器 (带模板)
- Skills 选择 (多选)
- Model 参数配置

---

## 5. 增量设计修改

| 现有页面 | 新增内容 |
|---------|---------|
| Dashboard | 新增活跃团队概览卡片、任务分解进度条 |
| Client 详情 | 新增 Agent 思考分轨视图（类似 ChatGPT 的思考链展开） |
| Tasks | 新增分解树视图、甘特图、多 Agent 协作 tab |

---

*研究版本: 1.0 | 来源: Ruflo/AutoGen Studio/CrewAI/MetaGPT/n8n 公开文档及源码*
