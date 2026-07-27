# Agent Auto Loop Orchestrator — 开发 Client 工作分配（V2 修正版）

> 基于方案 B（4 clients 缩减并行）
> V2 变更: TDD 先行 + Client 1 负载减轻 + Healer+Scaler 合并 + 时间修正
> 接口契约见 ROADMAP.md

---

## Client 1: Foundations (Wave 1 独占)

**角色**: 地基建造者
**启动条件**: 立即启动（零依赖）
**工期**: 2 天
**输出**: 可被其他 4 个 client 导入的完整数据包 + 测试

### 任务清单

```
[TDD] Phase 0 — 测试骨架（先写测试，后写实现）
  ☐ auto_orchestrator_test.go: 类型/枚举/构造/GetMode/SetMode/Status/Stop 测试
  ☐ env_gate_test.go: 环境兼容性验证测试

[Phase 1.1 — ConversationMode 扩展（3 任务）]
  ☐ 新增 ModeManual/ModeAuto 常量
  ☐ 会话 key 隔离（conv:{id}:auto, conv:{id}:manual）
  ☐ detectModeCommand() 增加 /auto 识别
  ☐ [测试] ConversationMode 常量 + 方法测试

[Phase 1.2 — Orchestrator 核心类型（6 任务）]
  ☐ OrchestratorState 枚举（Idle/Planning/Dispatching/Monitoring/Healing/Scaling）
  ☐ AutoMode 枚举（Once/LoopN/Infinite/UntilCond）
  ☐ LoopConfig + RetryPolicy + DefaultLoopConfig
  ☐ ExecutionPlan + PlanStatus
  ☐ PlannedTask + TaskNodeStatus
  ☐ AutoLoopOrchestrator 结构体骨架 + New/GetMode/SetMode/Status/Stop
  ☐ [测试] 构造/模式切换/状态查询/Stop 测试

[Phase 1.5 — 环境兼容性验证 Gate（6 任务）]
  ☐ setsid 可用性验证（Android arm64）
  ☐ PID 回收 + 僵尸进程处理
  ☐ 临时目录权限隔离（os.MkdirTemp）
  ☐ SpawnClient 闭环测试（启动→注册验证→kill→清理）
  ☐ KillClient 超时强制终止测试
  ☐ 凭证隔离（不暴露完整 .security.yml）
```

### 预计输出
| 文件 | 行数估计 | 变更 |
|------|:--------:|:----:|
| `pkg/agent/conversation_mode.go` | +40 行 | 微增 |
| `pkg/agent/auto_orchestrator.go` | +250 行（类型+骨架） | 不变 |
| `pkg/agent/auto_state.go` | +60 行 | 不变 |
| `pkg/agent/auto_orchestrator_test.go` | +200 行 | ✅ **新增（TDD）** |
| `pkg/agent/env_gate_test.go` | +100 行 | ✅ **新增（TDD）** |

**V1→V2 变更**:
- ❌ 移除 Phase 9.1（基础测试）→ 移动到 Wave 5
- ❌ 移除 Phase 10（并发安全）→ 移动到 Wave 5
- ✅ 新增 Phase 0 TDD 测试骨架
- 📉 任务数: 20→15

### 依赖交付（给 Client 2-4 的契约）
- `pkg/agent/conversation_mode.go` 中的常量枚举
- `pkg/agent/auto_orchestrator.go` 中的 ExecutionPlan / PlannedTask / OrchestratorState
- `pkg/agent/auto_state.go` 中的状态结构体
- 环境兼容性验证报告

---

## Client 2: ClientPool (Wave 2A)

**角色**: 客户端进程管理器
**启动条件**: 等待 Client 1 完成 Phase 1 + Phase 1.5
**前置依赖**: OrchestratorState / ManagedState 类型 + setsid 已验证
**工期**: 2.5 天

### 任务清单

```
[TDD] Phase 0 — ClientPool 测试骨架
  ☐ client_pool_test.go: Spawn/Kill/EnsureClient/Monitor 测试（模拟 Registry）

[Phase 2 — ClientPool（18 个任务）]

  【2.1 核心结构】
  ☐ ClientPoolOptions（ServerURL, Token, ReefBin（**绝对路径**）, BaseDir,
     IdleTimeout, MaxClients, SpawnTimeout, **MonitorInterval**）
  ☐ ManagedClient（ClientID, PID, Role, Skills, State, StartedAt,
     LastActive, HomeDir, StopCh）
  ☐ ManagedState（Starting/Running/Draining/Stopped）
  ☐ ClientPool 结构体 + NewClientPool()
  ☐ [测试] 选项/结构体构造测试

  【2.2 启动功能】
  ☐ EnsureClient(ctx, role, skills) (clientID string, err error)
  ☐ getClientFromRegistry(role, skills) (*reef.ClientInfo, bool)
  ☐ SpawnClient(role, skills) (*ManagedClient, error)
    → 创建临时 REEF_HOME
    → 复制最小配置模板
    → 构建绝对路径命令（`/root/reef_server/reef` 而非 `reef`）
    → 启动进程
    → 轮询 Registry（SpawnTimeout 可配置）
  ☐ [测试] Spawn 流程测试（模拟 Registry）

  【2.3 销毁功能】
  ☐ KillClient(clientID) error — **导出 ErrClientNotFound / ErrKillTimeout**
    → 标记 Draining
    → 等待 load=0 (30s)
    → SIGTERM → 2s → SIGKILL
    → Registry.Unregister
    → 清理临时目录
  ☐ [测试] 超时 + 强制终止测试

  【2.4 监控功能】
  ☐ ListIdle(timeout) → idle clients
  ☐ GetActiveClients / GetActiveCount / GetByClientID
  ☐ Monitor(ctx) goroutine — **MonitorInterval 从选项读取**
  ☐ [测试] 并发安全测试（竞态检测）
```

### 预计输出
| 文件 | 行数估计 | 变更 |
|------|:--------:|:----:|
| `pkg/agent/client_pool.go` | ~520 行 | ✅ 增加导出错误类型 |
| `pkg/agent/client_pool_test.go` | ~400 行 | ✅ **新增（TDD）** |

**V1→V2 变更**:
- ✅ 新增 TDD 测试骨架（Phase 0）
- ✅ ClientPoolOptions 增加 MonitorInterval 字段
- ✅ KillClient 导出 ErrClientNotFound / ErrKillTimeout
- ✅ SpawnClient 使用绝对路径 `/root/reef_server/reef`
- 📈 行数+20（错误类型+配置字段）

### 依赖交付（给 Client 4 的契约）
- `ClientPool` 接口实现（EnsureClient/SpawnClient/KillClient/ListIdle）
- 可以 mock 或直接注入

---

## Client 3: TaskPlanner (Wave 2B)

**角色**: 指令解析 + DAG 编排引擎
**启动条件**: 等待 Client 1 完成 Phase 1
**前置依赖**: ExecutionPlan + PlannedTask 类型
**工期**: 2 天

### 任务清单

```
[TDD] Phase 0 — TaskPlanner 测试骨架
  ☐ auto_planner_test.go: 解析/DAG/环检测/角色推断测试

[Phase 3 — TaskPlanner（12 个任务）]

  【3.1 基本解析】
  ☐ TaskPlanner 结构体 + New()
  ☐ Parse(ctx, instruction) (*ExecutionPlan, error)
  ☐ 简单拆分（单任务）
  ☐ 并行拆分（"并"/"和"/"同时"/"以及"）
  ☐ 串行拆分（"先...再..."/"然后"/"之后"/"接着" → **支持 ≥3 步**）
  ☐ LLM 辅助解析（可选 P2，先留空）
  ☐ [测试] 4 种拆分策略测试

  【3.2 DAG 管理】
  ☐ PlanDAG(tasks) → DAG 构建
  ☐ DetectCycles — Kahn 算法环检测
  ☐ TopologicalSort — Kahn BFS 拓扑排序
  ☐ checkDAG 依赖解锁逻辑（pending→ready→blocked）
  ☐ [测试] DAG 构建+环检测+排序+解锁测试

  【3.3 角色/技能推断】
  ☐ InferRoleSkills（规则匹配: 编译→coder, 部署→ops）
  ☐ InferRoleSkillsLLM（可选 P2）
  ☐ [测试] 角色推断测试

  【3.4 任务 ID 唯一性】
  ☐ UUID 或 `time.Now().UnixNano()` 确保跨调用唯一
```

### 预计输出
| 文件 | 行数估计 | 变更 |
|------|:--------:|:----:|
| `pkg/agent/auto_planner.go` | ~380 行 | ✅ 串行拆分扩展 + UUID |
| `pkg/agent/auto_planner_test.go` | ~350 行 | ✅ **新增（TDD）** |

**V1→V2 变更**:
- ✅ 新增 TDD 测试骨架
- ✅ trySerialSplit 升级为支持 ≥3 步（循环而非单关键词返回）
- ✅ 任务 ID 使用 `time.Now().UnixNano()` 确保唯一
- 📈 行数+30

### 依赖交付（给 Client 4 的契约）
- `TaskPlanner` 接口实现（Parse/InferRoleSkills）
- `ExecutionPlan` 包含 `Tasks []*PlannedTask` 完整状态

---

## Client 4: Orchestrator + Healer + Scaler (Wave 3 + Wave 4A 合并)

**角色**: 核心编排引擎 + 自愈系统 + 弹性伸缩
**启动条件**: 等待 Client 2 + Client 3 完成
**前置依赖**: ClientPool 接口 + TaskPlanner 接口
**工期**: 3 天（合并后工作包大，但消除了交叉依赖）

### 任务清单

```
[TDD] Phase 0 — 测试骨架
  ☐ auto_orchestrator_test.go（增量）: Poll/Submit/Healing/Scaling 测试

[Phase 4 — Orchestrator Poll 循环（14 个任务）]

  【4.1 PollQueue】
  ☐ PollQueue(ctx) — 主调度循环
  ☐ findReadyTasks() — 找出 ready 任务
  ☐ applyParallelismLimit(tasks) — 并行度限制
  ☐ submitPlanTask(ctx, planTask) — EnsureClient + Scheduler.Submit
  ☐ watchTask(taskID, planTaskID) — goroutine 监听
  ☐ checkDAG() — 依赖解锁
  ☐ onTaskCompleted / onTaskFailed — 回调
  ☐ onPlanComplete — 计划完成回调
  ☐ EnqueueMessage(content) — 非命令消息入队

  【4.2-4.4 主流程】
  ☐ Poll(ctx) — 一次完整轮询（Queue + Healing + Scale）
  ☐ Trigger / Tick / Done / QueueDepth
  ☐ [测试] Poll 循环测试（mock Scheduler + mock ClientPool）

[Phase 5 — Healer（8 个任务）]

  ☐ HealOperation / HealAction / Healer 结构体
  ☐ NewHealer(policy) *Healer
  ☐ OnTaskFailed(task, planTask) HealAction
  ☐ healRetry — 排除失败 client + 指数退避 + 重提交
  ☐ healEscalate / healAbort — 升级/放弃
  ☐ OnClientDisconnected(clientID) / healRestartClient
  ☐ ProcessPending / Enqueue / Dequeue
  ☐ [测试] Healer 重试/断线/升级/指数退避测试

[Phase 6 — AutoScaler（7 个任务）]

  ☐ AutoScaler 结构体 + NewAutoScaler()
  ☐ EvaluateScaleUp — 队列深度 > parallelism×2 + client < max
  ☐ evaluateScaleDown — 空闲 > idle_timeout + client > min
  ☐ Execute / Evaluate 完整决策入口
  ☐ cooldown 管理
  ☐ [测试] 扩容/缩容/冷却期测试
  ☐ [测试] Healer+Scaler 集成测试

[EventBus 集成（1 任务）]
  ☐ 注册 TaskCompleted/TaskFailed/ClientDisconnected 事件
```

### 预计输出
| 文件 | 行数估计 | 变更 |
|------|:--------:|:----:|
| `pkg/agent/auto_orchestrator.go` | +550 行（增量） | 不变 |
| `pkg/agent/auto_healer.go` | ~350 行 | 不变 |
| `pkg/agent/auto_scaler.go` | ~250 行 | 不变 |
| `pkg/agent/auto_orchestrator_test.go` | +500 行 | ✅ **增量（TDD）** |
| `pkg/agent/auto_healer_test.go` | ~300 行 | ✅ **新增（TDD）** |
| `pkg/agent/auto_scaler_test.go` | ~250 行 | ✅ **新增（TDD）** |

**V1→V2 变更**:
- ✅ Healer + Scaler 合并到 Client 4，消除交叉依赖耦合
- ✅ 新增 3 个测试文件
- 📈 任务数: 12+10+6=28→30（含测试）

---

## Client 5: CLI + AgentLoop + Quality (Wave 4B + 4C + Wave 5)

**角色**: 全方位集成者 + 质量看护者
**启动条件**: 等待 Client 4 完成
**前置依赖**: 完整的 AutoLoopOrchestrator
**工期**: 3 天

### 任务清单

```
[TDD] Phase 0 — 测试骨架
  ☐ cmd_auto_test.go: 命令解析/handler 路由/别名测试

[Phase 7 — CLI 命令（8 个任务）]

  ☐ autoCommand() 命令树定义
  ☐ handleAutoMode / handleAutoRun / handleAutoStep
  ☐ handleAutoLoop / handleAutoStop / handleAutoStatus
  ☐ handleAutoQueue / handleAutoHistory / handleAutoConfig
  ☐ 命令别名 (/autotask, /autostep, /autoloop)
  ☐ 命令注册到 builtin.go + commands.go
  ☐ [测试] 命令解析测试
  ☐ [测试] handler 路由 + 别名测试

[Phase 8 — AgentLoop 集成（5 个任务）]

  ☐ AgentLoop.Orchestrator 字段
  ☐ Run() select 块: Tick/Done channel
  ☐ 非命令消息路由 → EnqueueMessage
  ☐ Manual 模式下等待提示
  ☐ NewAgentLoop() 初始化 Orchestrator

[Phase 9 — 收敛测试（5 个任务）]

  ☐ 端到端集成测试（mock Scheduler + mock ClientPool + TaskPlanner）
  ☐ conversation_mode_test.go（基础测试）
  ☐ 并发安全测试（`go test -race` 强化）
  ☐ 边界测试（TaskPlanner: 空指令/超长/特殊字符）
  ☐ 性能基线记录（BENCHMARKS.md）

[Phase 10 — 文档（4 个任务）]

  ☐ DEVELOPER.md（架构说明 + 添加新命令流程）
  ☐ CHANGELOG.md
  ☐ 使用示例（README 节）
```

### 预计输出
| 文件 | 行数估计 | 变更 |
|------|:--------:|:----:|
| `pkg/commands/cmd_auto.go` | ~400 行 | 不变 |
| `pkg/agent/agent.go` | +10 行 | 不变 |
| `pkg/agent/agent_init.go` | +15 行 | 不变 |
| `pkg/commands/_builtin.go_` | +2 行 | 不变 |
| `pkg/commands/cmd_auto_test.go` | ~350 行 | ✅ **新增（TDD）** |
| `pkg/agent/auto_orchestrator_test.go` | +200 行 | ✅ **增量** |
| `DEVELOPER.md` | ~60 行 | ✅ **新增** |
| `CHANGELOG.md` | ~30 行 | ✅ **新增** |

**V1→V2 变更**:
- ✅ Client 5 新增 Phase 0 TDD 测试骨架
- ✅ Client 5 吸收 Phase 9.1（基础测试）+ Phase 10（并发安全）
- 📈 任务数: 略增（测试任务迁移）

---

## 时间线总览

| Wave | Client | 任务数 | 工期 | 并行度 |
|:----:|:------:|:------:|:----:|:------:|
| 1 | Client 1 Foundations | 15 项 | **2 天** | 1 |
| 2A | Client 2 ClientPool | 18 项 | **2.5 天** | 2（与 2B 并行） |
| 2B | Client 3 TaskPlanner | 12 项 | **2 天** | 2（与 2A 并行） |
| 3 | Client 4 Orchestrator | 14 项 | **2 天** | 1（依赖 2A+2B） |
| 4A | Client 4 Healer+Scaler | 15 项 | **2 天** | 1（与 4B/4C 并行） |
| 4B | Client 5 CLI | 8 项 | **1.5 天** | 1（与 4A/4C 并行） |
| 4C | Client 5 AgentLoop | 5 项 | **1.5 天** | 1（与 4A/4B 并行） |
| 5 | Client 5 Quality | 9 项 | **2 天** | 1 |
| | **总计** | **~96 项** | **8 天** | **峰值 3 并行** |

> V1→V2 总任务数缩减: 135→96 项（合并重复 + 精简非核心）
> 工期: 5 天→8 天（Android arm64 编译慢 + TDD 时间）
> 峰值并行: 4→3 个 client（Healer+Scaler 合并）
