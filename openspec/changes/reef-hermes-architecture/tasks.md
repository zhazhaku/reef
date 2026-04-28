---
change: reef-hermes-architecture
artifact: tasks
phase: research
created: 2026-04-28
---

# Tasks: Hermes 能力架构研究计划

## Phase 1: 代码走读与现状分析（Day 1）

### Task 1.1: AgentLoop 消息处理全链路走读
- **目标**: 梳理从用户消息到 LLM 决策的完整链路
- **方法**: 代码走读 + 画流程图
- **关键文件**:
  - `pkg/agent/agent.go` — AgentLoop 主循环
  - `pkg/agent/agent_message.go` — 消息处理入口
  - `pkg/agent/agent_steering.go` — Steering 机制
- **产出**: AgentLoop 消息处理流程图（含 Hermes 注入点标注）
- **验收**: 流程图覆盖 processMessage → LLM → Tool → Response 全链路

### Task 1.2: PromptStack 架构分析
- **目标**: 理解现有 Prompt 分层机制，确定 Hermes 角色注入点
- **方法**: 代码走读 + 实验验证
- **关键文件**:
  - `pkg/agent/prompt.go` — PromptStack / PromptLayer / PromptSlot / PromptSource
  - `pkg/agent/context.go` — BuildSystemPromptParts
- **产出**: PromptStack 完整结构图 + Hermes 注入方案对比
- **验收**: 明确推荐方案（A/B/C）及理由

### Task 1.3: Tool 注册机制分析
- **目标**: 梳理 Tool 注册全流程，确定条件注册方案
- **方法**: 代码走读
- **关键文件**:
  - `pkg/agent/agent_init.go` — registerSharedTools
  - `pkg/tools/` — 各 Tool 实现
  - `pkg/reef/server/reef_tool.go` — 新增 ReefSwarmTool（设计稿）
- **产出**: Tool 注册清单 + 按角色分类表
- **验收**: 每种角色的允许/禁止 Tool 列表明确

### Task 1.4: SubTurn 与 Steering 机制分析
- **目标**: 理解内部并发机制，确定与 Hermes 的关系
- **方法**: 代码走读
- **关键文件**:
  - `pkg/agent/steering.go` — SteeringQueue
  - `pkg/agent/agent_init.go` — SubTurn/Spawn 相关
- **产出**: SubTurn vs Hermes 分工定义
- **验收**: 两种并发机制的使用场景和边界明确

---

## Phase 2: 对比分析（Day 2）

### Task 2.1: OpenAI Swarm 角色约束机制研究
- **目标**: 学习 Swarm 的 handoff 和 agent 角色切换机制
- **方法**: 文档阅读 + 源码分析
- **关注点**:
  - agent 之间的 handoff 机制
  - 如何防止 agent 越界
  - 角色定义方式
- **产出**: Swarm 角色约束机制总结
- **验收**: 提取可借鉴的设计模式

### Task 2.2: AutoGen/CrewAI 角色约束机制研究
- **目标**: 学习多 Agent 框架的角色定义和约束方式
- **方法**: 文档阅读
- **关注点**:
  - AutoGen 的 GroupChat + role 定义
  - CrewAI 的 Agent role + delegation
  - 如何约束 agent 只做自己角色的事
- **产出**: 多框架角色约束对比表
- **验收**: 提炼出适用于 PicoClaw 的约束模式

### Task 2.3: Hermes 能力模型规格定义
- **目标**: 基于 Task 1-2 的分析，定义 Hermes 三角色模型
- **方法**: 综合分析
- **产出**: Hermes 能力模型规格文档
- **验收**: 三种角色的能力边界、Tool 集、Prompt 模板完整定义

---

## Phase 3: 设计方案制定（Day 3）

### Task 3.1: PromptStack 扩展方案确定
- **目标**: 确定 Hermes 角色注入的 PromptStack 方案
- **方法**: 方案对比 + 代码影响分析
- **产出**: 
  - 新增 PromptSlotHermesRole 定义
  - PromptSourceHermesRole 注册
  - BuildSystemPromptParts 修改方案
- **验收**: 代码变更点清单 + 对现有功能无破坏

### Task 3.2: Tool 策略方案确定
- **目标**: 确定按模式切换 Tool 集的方案
- **方法**: 方案对比 + 性能分析
- **产出**:
  - HermesToolPolicy 定义
  - registerSharedTools 修改方案
  - 动态注册/注销方案（降级用）
- **验收**: 三种模式下的 Tool 集明确 + 降级切换可行

### Task 3.3: HermesGuard 运行时约束方案
- **目标**: 设计运行时 Guard 防止越界
- **方法**: 接口设计
- **产出**:
  - HermesGuard 接口定义
  - 与 Tool 执行的集成点
  - 降级/恢复触发机制
- **验收**: Guard 能在 Tool 调用前拦截越界请求

### Task 3.4: 降级策略决策树
- **目标**: 设计完整的降级/恢复流程
- **方法**: 决策树设计
- **产出**:
  - 降级触发条件
  - 降级行为（工具注册 + Prompt 更新 + 用户通知）
  - 恢复条件
- **验收**: 覆盖所有降级场景

### Task 3.5: CLI 参数 → 行为模式链路设计
- **目标**: 设计从 CLI 到行为模式的完整映射
- **方法**: 链路设计
- **产出**:
  - `--server` 参数解析
  - HermesMode 传递链路
  - 配置文件 hermes 段设计
- **验收**: 从 CLI 到 AgentLoop 行为的完整链路

---

## Phase 4: 融合评审（Day 4）

### Task 4.1: 与 reef-scheduler-v2 设计方案融合评审
- **目标**: 确保 Hermes 架构与 reef-scheduler-v2 设计方案无冲突
- **方法**: 交叉审查
- **产出**:
  - 融合点清单
  - design.md 修订建议
  - 新增文件清单
- **验收**: 两个设计方案的融合点明确、无冲突

### Task 4.2: 研究报告编写
- **目标**: 汇总所有研究成果
- **方法**: 文档编写
- **产出**: 
  - `openspec/changes/reef-hermes-architecture/RESEARCH_REPORT.md`
  - 包含：问题分析、方案对比、推荐设计、代码变更点、风险评估
- **验收**: 报告完整、可指导后续实施

### Task 4.3: reef-scheduler-v2 设计方案更新
- **目标**: 将 Hermes 架构融入 reef-scheduler-v2 设计方案
- **方法**: 更新 design.md / proposal.md / specs
- **产出**:
  - proposal.md 新增 §Hermes 能力架构
  - design.md 新增 §Hermes 约束链
  - specs 新增 §Hermes 能力约束规格
  - tasks.md 更新实施任务
- **验收**: 设计方案完整包含 Hermes 架构

---

## 时间线

| Day | Phase | 产出 |
|-----|-------|------|
| Day 1 | Phase 1: 代码走读 | AgentLoop 流程图 + PromptStack 分析 + Tool 清单 |
| Day 2 | Phase 2: 对比分析 | 多框架对比 + Hermes 能力模型规格 |
| Day 3 | Phase 3: 设计方案 | PromptStack/Tool/Guard/降级/CLI 方案 |
| Day 4 | Phase 4: 融合评审 | 研究报告 + reef-scheduler-v2 更新 |

## 依赖

- Phase 2 依赖 Phase 1 的代码走读结果
- Phase 3 依赖 Phase 2 的能力模型定义
- Phase 4 依赖 Phase 3 的设计方案
