# Agent Auto Loop Orchestrator — 项目状态

> 基于 openspec `agent-auto-loop-cli` | V2（TDD 修正版）
> 更新: 2026-07-27 05:40

## 里程碑

| 里程碑 | 状态 | 日期 | 说明 |
|--------|:----:|:----:|------|
| M0: 审查闭环完成 | ✅ 完成 | 2026-07-20 | 7 项改进已纳入 V2 计划 |
| M1: Wave 1 (Foundations) | ✅ 完成 | 2026-07-23 | conversation_mode.go + auto_orchestrator.go |
| M1.5: Wave 1 TDD 测试 | ✅ 完成 | 2026-07-23 | auto_orchestrator_test.go (20 tests) |
| M2A: Wave 2A (ClientPool) | ✅ 完成 | 2026-07-23 | client_pool.go 585 行 |
| M2A.5: ClientPool TDD 测试 | ✅ 完成 | 2026-07-23 | client_pool_test.go (12 tests) |
| M2B: Wave 2B (TaskPlanner) | ✅ 完成 | 2026-07-23 | auto_planner.go 548 行 |
| M2B.5: TaskPlanner TDD 测试 | ✅ 完成 | 2026-07-23 | auto_planner_test.go (18 tests) |
| M3: Wave 3 (Orchestrator Core) | ✅ 完成 | 2026-07-23 | PollQueue/checkDAG/Trigger/EnqueueMessage |
| M4A: Wave 4A (Healer + AutoScaler) | ✅ 完成 | 2026-07-24 | auto_healer.go (9 tests) + auto_scaler.go (7 tests) |
| **M4B: Wave 4B (CLI Commands)** | **✅ 完成** | **2026-07-27** | **cmd_auto.go — 8 子命令** |
| **M4C: Wave 4C (AgentLoop Integration)** | **✅ 完成** | **2026-07-27** | **agent.go + agent_command.go wiring** |
| M5: Wave 5 (Quality) | ✅ 完成 | 2026-07-27 | 收敛测试+并发安全+边角情形+文档全部完成 |

## 代码修复清单

| # | 问题 | 状态 |
|---|------|:----:|
| F1 | exec.Command("reef") → 绝对路径 | ✅ 已修复 |
| F2 | KillClient 缺导出错误类型 | ✅ 已修复 |
| F3 | trySerialSplit 仅 2 步 → 循环拆分 | ✅ 已修复 |
| F4 | 任务 ID 硬编码 → UnixNano | ✅ 已修复 |
| F5 | Monitor 硬编码 → 选项读取 | ✅ 已修复 |

## Wave 4B/4C 文件变更

### 新增文件
| 文件 | 行数 | 说明 |
|------|:----:|------|
| `pkg/commands/cmd_auto.go` | ~310 | 8 个 CLI 子命令 (mode/run/step/loop/stop/status/queue/history) |
| `pkg/agent/auto_orchestrator.go` | +~80 | 新增 ListQueue/ListHistory/ProcessQueue/SetLoopConfig/HistoryEntry |

### 修改文件
| 文件 | 变更 |
|------|------|
| `pkg/commands/runtime.go` | 新增 AutoHistoryEntry 类型 + 8 个 auto 回调字段 |
| `pkg/commands/builtin.go` | 注册 autoCommand() |
| `pkg/agent/agent.go` | AgentLoop 新增 orchestrator 字段 |
| `pkg/agent/agent_command.go` | buildCommandsRuntime 中注入 auto 回调 |

## CLI 命令表

| 命令 | 别名 | 功能 |
|------|------|------|
| `/auto mode [auto\|manual\|chat\|hermes]` | — | 显示/切换执行模式 |
| `/auto run <指令>` | `/autotask run` | 向队列添加任务 |
| `/auto step` | — | 手动执行一步 |
| `/auto loop <次数\|infinite>` | `/autoloop` | 设置循环策略 |
| `/auto stop` | — | 停止 orchestrator |
| `/auto status` | — | 显示 orchestrator 状态 |
| `/auto queue` | — | 显示任务队列 |
| `/auto history` | — | 显示执行历史 |

## 文件统计

| 文件 | 类型 | 行数 | 测试数 |
|------|------|:----:|:------:|
| `conversation_mode.go` | 核心 | 196 | — |
| `auto_orchestrator.go` | 核心 | 700+ | — |
| `auto_orchestrator_test.go` | 测试 | 436 | 20 |
| `client_pool.go` | 核心 | 585 | — |
| `client_pool_test.go` | 测试 | 322 | 12 |
| `auto_planner.go` | 核心 | 548 | — |
| `auto_planner_test.go` | 测试 | 417 | 18 |
| `auto_healer.go` | 核心 | 218 | — |
| `auto_healer_test.go` | 测试 | 280 | 9 |
| `auto_scaler.go` | 核心 | 167 | — |
| `auto_scaler_test.go` | 测试 | 146 | 7 |
| `cmd_auto.go` | CLI | ~310 | — |
| **合计** | — | **~4,300** | **66** |

## 服务器状态

- Server: 运行中 (uptime ~43h), ws :9999, admin :8081
- 客户端: **2 coders** 已连接
- 状态: Wave 1-4C 全部完成，准备 Wave 5

### 测试统计更新 (2026-07-27)

| 测试类别 | 测试数 | 状态 |
|----------|:------:|:----:|
| 单元测试 (orchestrator) | 20 | ✅ PASS |
| 单元测试 (client_pool) | 12 | ✅ PASS |
| 单元测试 (planner) | 18 | ✅ PASS |
| 单元测试 (healer) | 9 | ✅ PASS |
| 单元测试 (scaler) | 7 | ✅ PASS |
| CLI 收敛测试 (cmd_auto_test.go) | 34 | ✅ PASS |
| E2E 集成测试 (auto_e2e_test.go) | 9 | ✅ PASS |
| **并发安全测试 (auto_concurrency_test.go)** | **9** | **✅ PASS — 新增** |
| **边角情况测试 (auto_concurrency_test.go)** | **6** | **✅ PASS — 新增** |
| **全部 auto 测试合计** | **124** | **✅ PASS** |

### E2E 覆盖场景

| 测试 | 场景 |
|------|------|
| TestAutoE2E_FullLifecycle | 模式变化 → 入队 3 任务 → 3 步处理 → 空队列 → 停止后继续入队 |
| TestAutoE2E_ModeSwitching | manual → chat → hermes → auto → invalid error |
| TestAutoE2E_LoopConfiguration | count=3 配置 → infinite 配置 |
| TestAutoE2E_QueueManagement | 空队列 → 单任务 → FIFO 3 任务顺序验证 |
| TestAutoE2E_Aliases | /autotask run ✓ /autoloop loop ✓ |
| TestAutoE2E_StepAndQueue | step 前后队列深度变化 + 剩余任务验证 |
| TestAutoE2E_ConcurrentEnqueue | 5 任务连续入队 → FIFO 正确性 |
| TestAutoE2E_StatusOutput | status 必须包含 mode/state 字段 |
| TestAutoE2E_QueueCommand | queue 空/非空输出正确性 |

## Wave 5 — Quality ✅ (2026-07-27)

### 并发安全测试 — `auto_concurrency_test.go` (9 测试)

| 测试 | 场景 | 并发维度 |
|------|------|:--------:|
| TestConcurrentGetSetMode | 20 goroutine × 50 次 Get/SetMode | mode 锁 |
| TestConcurrentEnqueueAndProcessQueue | 生产者 + 消费者 + 监控 3 线并行 | taskQueue 锁 |
| TestConcurrentStopAndStatus | 10 × Stop + 10 × Status 同时 | state 锁 + doneCh |
| TestConcurrentSetLoopConfigAndStatus | 10 × SetLoopConfig + 10 × Status | loopConfig 锁 |
| TestConcurrentTriggerAndPollQueue | 20 × Trigger + 5 × PollQueue | tickerCh 并发安全 |
| TestConcurrentAllMethods | 30 goroutine × 19 方法混跑 | 全家桶无死锁 |
| TestConcurrentHistoryAccess | 10 goroutine × 30 次 ListHistory | history/queue 读锁 |
| TestConcurrentBuildSystemPromptWithCache | 已有缓存并发构建 | system prompt 缓存锁 |

### 边角情形测试 — `auto_concurrency_test.go` (6 测试)

| 测试 | 场景 |
|------|------|
| TestStopWhenAlreadyStopped | 3 次连续 Stop，验证 state Idle 处理 |
| TestSetModeIdempotent | 100 次 SetMode 同一值 + 1 次切换 |
| TestQueueEdgeCases | 空串/8KB 长串入队、空队列 ProcessQueue、多轮入队出队 |
| TestChannelOverflow | tickerCh 容量 10，发送 50 次 Trigger（验证 non-blocking select）|
| TestModeRapidSwitching | 100 次快速 ModeAuto↔ModeManual 切换 |
| TestSetLoopConfigZeroValues | 零值 LoopConfig{} + ModeOnce，验证 SetLoopConfig |
| TestHistoryEdgeCases | 无 plan 时 ListHistory → nil |

### 文档

- `skills/auto-loop/SKILL.md` — 完整 CLI 命令表、架构组件、测试统计
- `STATE.md` — 里程碑更新、测试统计更新
