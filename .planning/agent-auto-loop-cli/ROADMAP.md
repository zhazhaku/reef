# Agent Auto Loop Orchestrator — GSD 路线图（V2 修正版）

> 基于 openspec `agent-auto-loop-cli`
> 修正: 2026-07-23 — 根据代码审查结论优化：TDD 先行 + 工作负载重平衡 + 时间修正

---

## 项目规模估算（修正后）

| 维度 | 原始 | 修正后 |
|------|:----:|:------:|
| 新增 Go 文件 | 7 个 | 7 个（不变） |
| 修改 Go 文件 | 4 个 | 4 个（不变） |
| 总实施任务 | ~135 项 | ~130 项（合并+精简） |
| 测试文件 | ~6 个 | ~8 个（TDD 强化） |
| **预计工期** | **5 天** | **8 天**（Android arm64 编译慢 + TDD 时间） |

---

## Wave 架构：5 波并行（TDD 嵌入）

```
Wave 1 ── Foundations + 测试 ────────────── 1 developer
Wave 2 ── Parallel A ──┬── ClientPool ──── 1 developer (含测试)
                        └── TaskPlanner ─── 1 developer (含测试)
Wave 3 ── Merge ─────── Orchestrator Core   1 developer (含测试)
Wave 4 ── Parallel B ──┬── Healer+Scaler ── 1 developer (合并, 含测试)
                        ├── CLI Commands ─── 1 developer (含测试)
                        └── AgentLoop Int ── 1 developer (含测试)
Wave 5 ── Quality ─────┬── 收敛测试 ─────── 1 developer
                        ├── 边界/并发测试 ── 1 developer
                        └── Docs + Polish ── 1 developer
```

**峰值并行：3 个开发 client（Wave 4，比原计划减少 1 个）**

**TDD 硬性规则**：
1. 每个 Wave 必须先写 `*_test.go` 文件（测试函数骨架），再写实现代码
2. 测试覆盖率目标：核心逻辑 ≥80%，边界场景 ≥60%
3. 所有 Wave 交付时必须 `go test ./pkg/agent/ -run Auto -v` 全部 PASS

---

## Wave 详细分配（修正版）

### Wave 1: Foundations (1 开发 client) — 2 天

**前置条件**: 无（零依赖）
**产出**: 可编译的包 + 可运行的测试，其他 Wave 可导入

| 子任务 | 文件 | 测试文件 | 依赖 |
|--------|------|----------|------|
| **[TDD]** 类型/枚举/构造测试骨架 | — | `auto_orchestrator_test.go` | — |
| ConversationMode 扩展 (ModeManual, ModeAuto) | `conversation_mode.go` | — | — |
| 会话 key 隔离 (conv:{id}:auto, conv:{id}:manual) | `conversation_mode.go` | — | — |
| `/auto` 命令检测 (detectModeCommand) | `conversation_mode.go` | — | — |
| Orchestrator 核心类型 (OrchestratorState, AutoMode, LoopConfig, RetryPolicy) | `auto_orchestrator.go` | — | — |
| ExecutionPlan + PlannedTask + 状态枚举 | `auto_orchestrator.go` | — | — |
| AutoLoopOrchestrator 结构体 + New/GetMode/SetMode/Status/Stop | `auto_orchestrator.go` | auto_orchestrator_test.go | — |
| 环境兼容性 Gate (Phase 1.5) | `auto_state.go` + 验证脚本 | env_gate_test.go | — |

**Wave 1 交付标准**：
- [ ] `go build ./pkg/agent/` 通过
- [ ] `go vet ./pkg/agent/` 零告警
- [ ] `go test ./pkg/agent/ -run "(Auto|EnvGate|ConversationMode)" -v` 全部 PASS
- [ ] 环境验证报告输出

### Wave 2A: ClientPool (1 开发 client) — 2.5 天

**前置条件**: Wave 1（数据类型 + 环境验证）
**与 Wave 2B 并行**: 无交叉依赖

| 子任务 | 文件 | 测试文件 | 依赖 |
|--------|------|----------|------|
| **[TDD]** ClientPool 测试骨架 | — | `client_pool_test.go` | — |
| ClientPoolOptions / ManagedClient / ManagedState | `client_pool.go` | — | Phase 1 |
| NewClientPool(选项) | `client_pool.go` | — | Phase 1 |
| EnsureClient / getClientFromRegistry | `client_pool.go` | — | Phase 1.5 |
| SpawnClient（创建临时目录→setsid→轮询） | `client_pool.go` | client_pool_test.go | Phase 1.5 |
| KillClient（Draining→SIGTERM→SIGKILL→清理 + 导出错误类型）| `client_pool.go` | client_pool_test.go | Phase 1.5 |
| ListIdle / GetActive / Monitor（MonitorInterval 可配置） | `client_pool.go` | — | Phase 1 |
| 编译期接口断言 `var _ ClientPoolInterface` | `client_pool.go` | — | — |

**Wave 2A 交付标准**：
- [ ] `go build` / `go vet` 通过
- [ ] 模拟 Registry 测试 PASS
- [ ] 超时+强制终止测试 PASS
- [ ] 竞态检测: `go test -race ./pkg/agent/ -run ClientPool` PASS

### Wave 2B: TaskPlanner (1 开发 client) — 2 天

**前置条件**: Wave 1（Orchestrator 核心类型）
**与 Wave 2A 并行**: 无交叉依赖

| 子任务 | 文件 | 测试文件 | 依赖 |
|--------|------|----------|------|
| **[TDD]** TaskPlanner 测试骨架 | — | `auto_planner_test.go` | — |
| TaskPlanner 结构体 + New() | `auto_planner.go` | — | Phase 1 |
| Parse(ctx, instruction) → ExecutionPlan | `auto_planner.go` | auto_planner_test.go | Phase 1 |
| 简单/并行/串行四种拆分（串行≥3步支持） | `auto_planner.go` | auto_planner_test.go | Phase 1 |
| PlanDAG / DetectCycles Kahn / TopologicalSort BFS | `auto_planner.go` | auto_planner_test.go | Phase 1 |
| checkDAG 依赖解锁 | `auto_planner.go` | auto_planner_test.go | Phase 1 |
| InferRoleSkills（规则匹配） | `auto_planner.go` | auto_planner_test.go | Phase 1 |
| **UUID 任务 ID**（`github.com/google/uuid` 或 `time+rand`） | `auto_planner.go` | — | — |

**Wave 2B 交付标准**：
- [ ] `go build` / `go vet` 通过
- [ ] 4 种拆分策略测试 PASS
- [ ] DAG 环检测/拓扑排序测试 PASS（含环和无环场景）
- [ ] 角色推断测试 PASS

### Wave 3: Orchestrator Core (1 开发 client) — 2 天

**前置条件**: Wave 2A + Wave 2B（ClientPool + TaskPlanner 接口）
**角色**: 集成者——将 Wave 2 两个组件编排到一起

| 子任务 | 文件 | 测试文件 | 依赖 |
|--------|------|----------|------|
| **[TDD]** Orchestrator 测试骨架 | — | `auto_orchestrator_test.go`（增量） | — |
| PollQueue(ctx) 主调度循环 | `auto_orchestrator.go` | auto_orchestrator_test.go | Phase 2+3 |
| findReadyTasks / applyParallelismLimit | `auto_orchestrator.go` | — | Phase 2+3 |
| submitPlanTask — EnsureClient + Scheduler.Submit | `auto_orchestrator.go` | auto_orchestrator_test.go | Phase 2 |
| watchTask — goroutine 事件监听 | `auto_orchestrator.go` | — | Phase 2 |
| checkDAG / onTaskCompleted / onTaskFailed | `auto_orchestrator.go` | auto_orchestrator_test.go | Phase 3 |
| EnqueueMessage / Poll(ctx) / Trigger / Tick / Done / QueueDepth | `auto_orchestrator.go` | — | Phase 1 |
| EventBus 集成 | `auto_orchestrator.go` | — | — |

**Wave 3 交付标准**：
- [ ] `go build` / `go vet` 通过
- [ ] mock Scheduler + mock ClientPool 的 Poll 循环测试 PASS
- [ ] DAG 依赖解锁/完成/失败测试 PASS
- [ ] `go test -race` 无竞态

### Wave 4A: Healer + AutoScaler (1 开发 client) — 2 天

**前置条件**: Wave 3（Orchestrator + ClientPool + Scheduler API）
**与 Wave 4B/C 并行**

| 子任务 | 文件 | 测试文件 | 依赖 |
|--------|------|----------|------|
| **[TDD]** Healer+Scaler 测试骨架 | — | `auto_healer_test.go`+`auto_scaler_test.go` | — |
| HealOperation/HealAction/Healer 结构体 | `auto_healer.go` | — | Phase 3 |
| NewHealer / OnTaskFailed | `auto_healer.go` | auto_healer_test.go | Phase 3 |
| healRetry（排除失败 client + 指数退避）| `auto_healer.go` | auto_healer_test.go | Phase 2 |
| healRestartClient / healEscalate / healAbort | `auto_healer.go` | auto_healer_test.go | Phase 2+3 |
| ProcessPending / Enqueue / Dequeue | `auto_healer.go` | — | Phase 3 |
| ScaleAction / ScaleType / AutoScaler 结构体 | `auto_scaler.go` | — | Phase 2 |
| NewAutoScaler / cooldown 管理 | `auto_scaler.go` | — | Phase 2 |
| EvaluateScaleUp / evaluateScaleDown | `auto_scaler.go` | auto_scaler_test.go | Phase 2+3 |
| Execute / Evaluate 完整决策 | `auto_scaler.go` | auto_scaler_test.go | Phase 2 |

**Wave 4A 交付标准**：
- [ ] `go build` / `go vet` 通过
- [ ] Healer 重试/断线/升级/指数退避测试 PASS
- [ ] Scaler 扩容/缩容/冷却期测试 PASS
- [ ] `go test -race` 无竞态

### Wave 4B: CLI 命令 (1 开发 client) — 1.5 天

**前置条件**: Wave 3（Orchestrator API + Runtime 结构）
**与 Wave 4A/C 并行**

| 子任务 | 文件 | 测试文件 | 依赖 |
|--------|------|----------|------|
| **[TDD]** 命令解析测试骨架 | — | `cmd_auto_test.go` | — |
| autoCommand() 完整命令树 | `cmd_auto.go` | — | Phase 1 |
| handleAutoMode/Run/Step/Loop/Stop/Status/Queue/History/Config | `cmd_auto.go` | cmd_auto_test.go | Phase 3 |
| 命令别名 (/autotask, /autostep, /autoloop) | `cmd_auto.go` | cmd_auto_test.go | Phase 1 |
| 命令注册到 builtin.go + commands.go | `builtin.go`, `commands.go` | cmd_auto_test.go | Phase 1 |

**Wave 4B 交付标准**：
- [ ] `go build` / `go vet` 通过
- [ ] 命令解析+handler 路由+别名测试 PASS

### Wave 4C: AgentLoop 集成 (1 开发 client) — 1.5 天

**前置条件**: Wave 3（Orchestrator 核心循环）
**与 Wave 4A/B 并行**

| 子任务 | 文件 | 测试文件 | 依赖 |
|--------|------|----------|------|
| AgentLoop.Orchestrator 字段 | `agent.go` | — | Phase 3 |
| Run() select 块: Tick/Done channel | `agent.go` | — | Phase 3 |
| 非命令消息路由 → EnqueueMessage | `agent.go` | — | Phase 1 |
| Manual 模式下等待提示 | `agent.go` | — | Phase 1 |
| NewAgentLoop() 初始化 Orchestrator | `agent_init.go` | — | Phase 3 |

**Wave 4C 交付标准**：
- [ ] `go build` / `go vet` 通过
- [ ] 集成后 smoke test: `/auto mode` → 切换成功

### Wave 5: 质量收敛 (2 开发 client) — 2 天

**前置条件**: Wave 3 + Wave 4A/B/C 全部完成

| 子任务 | 测试范围 | 说明 |
|--------|----------|------|
| 收敛测试 | 全部 auto-* 模块 | 端到端集成测试 |
| 并发安全测试 | client_pool + orchestrator | `go test -race` 强化 |
| 边界测试 | TaskPlanner（空指令/超长/特殊字符） | — |
| 性能测试 | ClientPool（批量 spawn/kill） | BENCHMARKS 记录 |

---

## 依赖图（修正后）

```
        Wave 1 (Foundations)
       /         \
Wave 2A (ClientPool)  Wave 2B (TaskPlanner)   ← 并行无交叉
       \         /
      Wave 3 (Orchestrator Core)
       /    |    \
Wave 4A   4B   4C  ← 并行（Healer+Scaler / CLI / AgentLoop）
(Healer+Scaler) (CLI) (AgentLoop)
       \    |    /
      Wave 5 (Quality)
```

---

## 接口契约清单（不变）

| 接口 | 方法 | 所属 Client |
|------|------|:----------:|
| `ModeStore` | GetMode/SetMode | Client 1 → 所有 |
| `ClientPoolInterface` | EnsureClient/SpawnClient/KillClient/ListIdle/GetActiveClients/GetActiveCount/GetByClientID/Monitor | Client 2 → Client 3,4 |
| `TaskPlannerInterface` | Parse/InferRoleSkills | Client 3 → Client 3 |
