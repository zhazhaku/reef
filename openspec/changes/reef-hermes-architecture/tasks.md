---
change: reef-hermes-architecture
artifact: tasks
created: 2026-04-28
updated: 2026-05-09
status: in-progress
---

# Tasks: Hermes 智能协作工作流

## Phase 0: 基础设施 ✅

- [x] HermesMode 定义（Full/Coordinator/Executor）
- [x] HermesGuard 运行时约束
- [x] hermesRoleContributor (PromptContributor)
- [x] ToolRegistry.Remove
- [x] `reef server` CLI 命令
- [x] config.json hermes 段

## Phase 1: 工作流状态机 ✅

- [x] **1.1** `hermes_workflow.go` — WorkflowPhase 状态机
- [x] **1.2** `hermes_orchestrator.go` (1607 行) — 编排逻辑
- [x] **1.3** `hermes_workflow_session.go` — SQLite 持久化 + 任务板

## Phase 2: 头脑风暴 ✅

- [x] **2.1** BrainstormSession → `handlePhaseBrainstorm` + `brainstormNextRound`
- [x] **2.2** 收敛检测 → `generateDirections` + convergeStreak
- [x] **2.3** 人类介入 → ProcessMessage 路由

## Phase 3: 风暴评审 ✅

- [x] **3.1** ReviewSession → `handlePhaseReview` + `runAllReviews`
- [x] **3.2** 多维评审 → `getReviewDimensions`

## Phase 4: 需求调研 ✅

- [x] **4.1** ResearchSession → `handlePhaseResearch` + `executeResearchItem`

## Phase 5: 需求设计 ✅

- [x] **5.1** DesignSession → `handlePhaseDesign` + `generateDesign`

## Phase 6: 设计报告 ✅

- [x] **6.1** 报告生成 → `handlePhaseReport` + `generateReport`

## Phase 7: 与 reef-scheduler-v2 融合 ✅

- [x] Client 注册时声明 role + skills
- [x] Server 端 ClientRegistry
- [x] reef_submit_task 支持 role 参数

## Phase 8: 测试 ✅

- [x] **8.1** HermesE2E_FullPipeline
- [x] **8.2** HermesE2E_AbortAtEachPhase
- [x] **8.3** HermesE2E_ModifyBackwardFlow

## 总工时: 3374 行 | 全 Phase 测试通过
