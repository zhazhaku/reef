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
| M5: Wave 5 (Quality) | 🔴 进行中 | — | 收敛测试/并发/文档 |

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

## 下一步: Wave 5 (Quality)

1. **收敛测试**: end-to-end 集成测试 / auto 各类命令路径
2. **并发安全**: race condition 检测 + 修复
3. **边角情况**: 空队列、非法参数、超时、cancel
4. **文档**: SKILL.md / README 更新
