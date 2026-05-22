---
change: reef-hermes-architecture
schema: spec-driven
status: research
created: 2026-04-28
updated: 2026-05-09
---

# Proposal: Hermes 能力架构 — 智能协作工作流

> 2026-05-09 更新：新增 §6 §7 §8 工作流设计 + 动态角色调用机制

## 1. 核心问题（不变）

详见 `RESEARCH_REPORT.md` §1：Server 端意图发散、模式切换缺失、无能力边界约束。

## 2. 已有研究成果（不变）

详见 `RESEARCH_REPORT.md`：
- §2 三层保障（Prompt + Tool 注册 + HermesGuard）
- §3 HermesMode / HermesGuard / PromptContributor / 降级策略

## 3. 新需求：智能协作工作流

现有 Hermes 设计解决了"Server 不直接执行"的问题，但工作流仍是**单步分发**：
```
用户消息 → Server 识别 → reef_submit_task → 1 个 Client → 返回结果
```

真实协作场景需要**多阶段、多角色、动态参与**的流水线：

```
需求接收 → 头脑风暴 → 风暴评审 → 需求调研 → 需求设计 → 设计报告
```

## 4. 工作流阶段定义

### 4.1 阶段总览

```
Phase 1  需求接收     Server 解析用户输入，识别任务类型和规模
Phase 2  头脑风暴     Server 协调多个 agent client 多轮讨论，产出方案方向
Phase 3  风暴评审     研究专家 agent client 对头脑风暴产出评分排序
Phase 4  需求调研     Server 选取 Top-N 方向，深度分析调研
Phase 5  需求设计     人类确认后，Server 调动分析 client 输出 PRD
Phase 6  设计报告     产出最终报告，人类决策是否进入开发
```

### 4.2 Phase 1: 需求接收

```
用户: "帮我设计一个分布式任务调度系统"
  │
  ▼
Server 解析:
  ├── 领域: 分布式系统
  ├── 类型: 系统设计
  ├── 复杂度: 高（多轮协作）
  └── 规模: 大型（需 4+ client 参与）
```

决策规则：
- 简单问候/元问题 → Server 直接回复
- 单一领域问题 → 单 Client 分发（现有行为）
- 跨领域/高复杂度 → 进入工作流

### 4.3 Phase 2: 头脑风暴 (Brainstorming)

**核心机制**：共享任务板 + 多轮讨论

```
Server 创建任务板 "dist-scheduler-design"
  │
  ├── 第 1 轮: 初始方向提案
  │   ├── Server → 需求分析 agent (role: requirement-analyst)
  │   │   "分析分布式调度系统的需求边界"
  │   ├── Server → 架构 agent (role: architect)
  │   │   "提出初步架构方向"
  │   └── Server → 调研 agent (role: researcher)
  │       "调研现有方案：K8s scheduler, Temporal, Airflow..."
  │
  ├── 第 2 轮: 交叉讨论（每个 agent 读取任务板完整记录）
  │   ├── 需求分析 agent 读取第 1 轮 → 补充约束条件
  │   ├── 架构 agent 读取第 1 轮 → 调整方案
  │   └── 调研 agent 读取第 1 轮 → 深入对比
  │
  ├── 第 N 轮: 收敛
  │   └── 直到产生 2-5 个有明显差异的方案方向
  │
  └── 人类可随时插入评论/引导方向
```

**关键设计**：

```
任务板 = seahorse conversation + shared context
  ├── 每个参与的 client 发言前获得完整讨论历史
  ├── client 独立 LLM 调用（不共享 context window）
  ├── 每个 client 看到其他 client 的发言 + server 的总结
  └── Server 作为主持人：控制轮次、决定何时收敛
```

**动态 Client 选择**（非均摊）：

```
Server 根据任务类型决定参与 client 和角色：

任务类型: "系统设计"
  参与: requirement-analyst(1) + architect(1) + researcher(1)
  不参与: coder, tester, devops

任务类型: "市场调研"
  参与: researcher(2) + data-analyst(1)
  不参与: architect, coder

任务类型: "代码审查"
  参与: coder(1) + reviewer(1)
  不参与: researcher, architect

规则: 不摊大饼，按需调用
```

### 4.4 Phase 3: 风暴评审 (Storm Review)

```
头脑风暴产出: [方向A, 方向B, 方向C]
  │
  ├── Server → 评审 agent-1 (role: reviewer, 专长: 可行性)
  │   评分方向 A/B/C 的技术可行性
  │
  ├── Server → 评审 agent-2 (role: reviewer, 专长: 创新性)
  │   评分方向 A/B/C 的创新性
  │
  ├── Server → 评审 agent-3 (role: reviewer, 专长: 成本)
  │   评分方向 A/B/C 的实施成本
  │
  └── Server 聚合评分 → 排序 → 推荐 Top-2
      各 Agent 可互相读取评审结果进行交叉校验
```

**评分维度**（可配置）：
- 技术可行性 (0-10)
- 创新性 (0-10)
- 实施成本 (0-10，越低越好)
- 风险 (0-10，越低越好)
- 扩展性 (0-10)

### 4.5 Phase 4: 需求调研 (Deep Research)

```
人类确认: "方向 A + B 都深入研究"
  │
  ├── Server → research-agent-1 (role: researcher)
  │   "深度调研方向 A: 基于 K8s scheduler framework"
  │   ├── web_search × 5
  │   ├── 论文/文档分析
  │   └── 产出调研报告 A
  │
  ├── Server → research-agent-2 (role: researcher)
  │   "深度调研方向 B: 自研分布式调度引擎"
  │   ├── web_search × 5
  │   ├── 开源方案对比
  │   └── 产出调研报告 B
  │
  └── Server 聚合两份报告 → 呈现人类
```

### 4.6 Phase 5: 需求设计 (Requirements Design)

```
人类确认调研结果 → 选择方向 A
  │
  ├── Server → 分析 agent (role: analyst)
  │   "根据调研报告 A 编写 PRD"
  │   产出: PRD v1
  │
  ├── Server → 架构 agent (role: architect)
  │   "根据 PRD 编写技术设计文档"
  │   产出: 技术设计 v1
  │
  └── 人类评审 → 可能多轮修改
```

### 4.7 Phase 6: 设计报告 (Final Report)

```
产出物:
  ├── 需求分析摘要
  ├── 头脑风暴记录（精简）
  ├── 评审结果 + 评分
  ├── 调研报告（完整）
  ├── PRD（如有）
  ├── 技术设计（如有）
  └── 下一步建议

格式: Markdown → 飞书消息 / 文件
后续: 人类决策 → 进入开发流程 (reef-scheduler-v2)
```

## 5. 动态 Client 选择机制

### 5.1 核心原则

```
❌ 错误: "有 N 个在线 Client → 全部参与 → 均摊任务"
✅ 正确: "分析任务类型 → 匹配角色 → 按需选择 1-N 个 Client → 精确分派"
```

### 5.2 任务类型 → 角色映射表

| 任务类型 | 关键词 | 参与角色 | 最少 Client |
|----------|--------|----------|:----------:|
| 系统设计 | 设计/架构/系统 | requirement-analyst, architect, researcher | 3 |
| 市场调研 | 调研/市场/竞品 | researcher, data-analyst | 2 |
| 代码审查 | review/审查/代码 | reviewer, coder | 2 |
| 技术方案 | 方案/选型/对比 | researcher, architect | 2 |
| 故障排查 | 报错/故障/bug | debugger, coder | 1-2 |
| 简单问答 | 什么是/如何/为什么 | researcher (1) | 1 |

### 5.3 Server 端决策逻辑

```go
// pkg/agent/hermes_workflow.go (新增)

type TaskProfile struct {
    Domain     string   // "distributed-systems"
    Type       string   // "system-design"
    Complexity int      // 1-5
    Keywords   []string // ["调度", "分布式", "架构"]
}

func (s *HermesWorkflow) SelectClients(profile TaskProfile, onlineClients []ClientInfo) []ClientSelection {
    required := s.roleMapping[profile.Type] // ["requirement-analyst", "architect", "researcher"]

    var selected []ClientSelection
    for _, role := range required {
        // 优先匹配角色完全一致的 client
        if c := findClientByRole(onlineClients, role); c != nil {
            selected = append(selected, ClientSelection{Client: c, Role: role})
            continue
        }
        // 降级: 匹配 skills 最接近的 client
        if c := findBestMatch(onlineClients, role, profile); c != nil {
            selected = append(selected, ClientSelection{Client: c, Role: role, Fallback: true})
        }
    }
    return selected
}
```

## 6. 共享任务板设计

### 6.1 实现方式

```
seahorse conversation "task-board:{taskID}"
  ├── 每个 client 的发言追加到此 conversation
  ├── 每个 client 在发言前 Assemble 整个 conversation
  ├── Server 发言 = 轮次控制 + 方向总结 + 人类评论
  └── 人类消息 = 普通 user message
```

### 6.2 轮次控制

```go
type BrainstormSession struct {
    TaskBoard   string         // seahorse conversation key
    Rounds      int            // 当前轮次
    MaxRounds   int            // 最大轮次（默认 5）
    Directions  []string       // 当前方案方向
    Converged   bool           // 是否已收敛
    HumanInput  []Message      // 人类插入的评论
}
```

Server 每轮：
1. 收集所有 client 上一轮的发言
2. 判断是否需要新轮次（是否收敛）
3. 生成新轮次的引导 Prompt
4. 分发给参与 client

收敛条件：
- 方向数 ≤ 5 且连续 2 轮无新方向
- 或达到 MaxRounds
- 或人类命令结束

## 7. 与现有 Hermes 三层保障的关系

```
Layer 1 (Prompt): Server identity = "工作流协调者"
    ├── 能: 任务分析、角色匹配、轮次控制、结果聚合
    ├── 能: reef_submit_task (分发到具体 client)
    └── 不能: 直接执行 (web_search, exec, read_file...)

Layer 2 (Tool 注册): 同现有设计
    ├── reef_submit_task, reef_query_task, reef_status
    ├── message, reaction, cron
    └── 不注册: web_search, exec, read_file, write_file...

Layer 3 (HermesGuard): 同现有设计
```

## 8. 实施路线

### Phase A: 工作流基础（本次）

```
新增文件:
  pkg/agent/hermes_workflow.go      工作流状态机
  pkg/agent/hermes_orchestrator.go  编排逻辑（选择 client、控制轮次）
  pkg/agent/hermes_taskboard.go     共享任务板（seahorse 封装）

修改文件:
  pkg/agent/hermes.go               新增 WorkflowPhase 类型
  pkg/agent/hermes_prompt.go        新增工作流阶段 Prompt
  pkg/config/config.go              新增 workflow 配置
```

### Phase B: 动态 Client 选择（依赖 reef-scheduler-v2）

```
依赖: Client 注册时声明 role + skills
产出: ClientRegistry + RoleMatchingEngine
```

### Phase C: 完整工作流

```
Phase A + Phase B → 联调 → E2E 测试
```

## 9. 风险

| 风险 | 缓解 |
|------|------|
| 多轮讨论 token 消耗大 | seahorse Compact 在每轮后总结，client 看到摘要+最近 2 轮 |
| Client 角色不匹配 | Fallback: 选择 skills 最接近的 client |
| 轮次过多不收敛 | MaxRounds 硬限制 + 人类可介入 |
| 任务板并发写入 | seahorse SQLite 单写者 + Server 串行化轮次 |
