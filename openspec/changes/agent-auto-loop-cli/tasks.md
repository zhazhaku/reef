# Agent Auto Loop Orchestrator — 实施任务清单

## Phase 1: 核心数据类型 + ConversationMode 扩展

### 1.1 ConversationMode 扩展
- [ ] `pkg/agent/conversation_mode.go`：增加 `ModeManual` 和 `ModeAuto` 常量
- [ ] `SessionKey()` 中增加 manual/auto 的 session key 构造（`conv:{id}:auto`, `conv:{id}:manual`）
- [ ] `String()` 中增加 manual/auto 显示
- [ ] `detectModeCommand()` 中增加 `/auto` 命令识别

### 1.2 Orchestrator 基本结构
- [ ] 创建 `pkg/agent/auto_orchestrator.go`
- [ ] 定义 `OrchestratorState` (Idle/Planning/Dispatching/Monitoring/Healing/Scaling)
- [ ] 定义 `AutoMode` (Once/LoopN/Infinite/UntilCond)
- [ ] 定义 `LoopConfig` + `RetryPolicy` + `DefaultLoopConfig`
- [ ] 定义 `ExecutionPlan` + `PlanStatus` (active/completed/failed/cancelled)
- [ ] 定义 `PlannedTask` + `TaskNodeStatus` (pending/ready/running/done/failed/blocked)
- [ ] 定义 `AutoLoopOrchestrator` 结构体（含 planner/clientPool/healer/scaler/scheduler/registry）
- [ ] 实现 `NewAutoLoopOrchestrator()` 构造函数
- [ ] 实现 `GetMode()`/`SetMode()` — 模式管理
- [ ] 实现 `Status()` — 状态报告
- [ ] 实现 `Stop()` — 停止执行 + 清理

## Phase 1.5: 环境兼容性验证（Gate — 阻塞后续 Phase）

### 1.5.1 在目标环境验证基础能力
- [ ] 验证 `os/exec.Command` + `SysProcAttr.Setsid` 在当前环境可用（测试 Android arm64）
- [ ] 验证 PID 回收和僵尸进程处理（SIGTERM → 2s → SIGKILL 是否按预期工作）
- [ ] 验证 `REEF_HOME` 临时目录的权限隔离（`os.MkdirTemp` + `defer os.RemoveAll`）
- [ ] 验证 `setsid` 是否可用（如果不可用 → 回退到 `nohup` + 直接 exec）

### 1.5.2 验证 spawn/kill 闭环
- [ ] 测试 SpawnClient: 启动一个临时 client → 检查 Registry 注册成功
- [ ] 测试 KillClient: 发送 SIGTERM → 等待 → 验证 Registry.Unregister
- [ ] 测试 KillClient 超时: 模拟 client 不响应 SIGTERM → SIGKILL 强制终止

### 1.5.3 验证凭证安全
- [ ] 确保 spawn 时不暴露完整 .security.yml，仅传递最小必要 API Key 子集
- [ ] 验证临时 REEF_HOME 目录不包含父进程的 channel tokens

## Phase 2: ClientPool — 客户端进程生命周期管理

### 2.1 核心结构
- [ ] 创建 `pkg/agent/client_pool.go`
- [ ] 定义 `ClientPoolOptions`（ServerURL, Token, ReefBin, BaseDir, IdleTimeout, MaxClients, SpawnTimeout）
- [ ] 定义 `ManagedClient`（ClientID, PID, Role, Skills, State, StartedAt, LastActive, HomeDir, StopCh）
- [ ] 定义 `ManagedState` (Starting/Running/Draining/Stopped)
- [ ] 定义 `ClientPool` 结构体
- [ ] 实现 `NewClientPool()` 构造函数

### 2.2 启动功能
- [ ] 实现 `EnsureClient(ctx, role, skills) (clientID string, err error)` — 检查 Registry + 按需启动
- [ ] 实现 `SpawnClient(role, skills) (*ManagedClient, error)` — 完整启动流程：
  - 创建临时 REEF_HOME 目录
  - 复制 config.json + .security.yml 模板
  - 构建 setsid 命令
  - 启动进程
  - 轮询 Registry 直到 ClientConnected (30s timeout)
- [ ] 实现 `getClientFromRegistry(role, skills) (*reef.ClientInfo, bool)` — 查找可用 client

### 2.3 销毁功能
- [ ] 实现 `KillClient(clientID) error` — 完整销毁流程：
  - 标记 Draining
  - 等待 load=0 (30s timeout)
  - SIGTERM → 2s → SIGKILL
  - Registry.Unregister
  - 清理临时目录

### 2.4 监控功能
- [ ] 实现 `ListIdle(timeout) []*ManagedClient` — 空闲 client 列表
- [ ] 实现 `GetActiveClients() []*ManagedClient` — 活跃 client 列表
- [ ] 实现 `GetActiveCount() int` — 活跃 client 数
- [ ] 实现 `GetByClientID(clientID) *ManagedClient` — 单 client 查询
- [ ] 实现 `Monitor(ctx)` — goroutine 监控 client 进程退出 + 心跳

## Phase 3: TaskPlanner — 任务解析 + DAG 编排

### 3.1 基本解析
- [ ] 创建 `pkg/agent/auto_planner.go`
- [ ] 定义 `TaskPlanner` 结构体
- [ ] 实现 `Parse(ctx, instruction) (*ExecutionPlan, error)` — 主入口
- [ ] 实现简单指令解析（单任务）
- [ ] 实现并行拆分（含"并"/"和"/"同时"等关键词）
- [ ] 实现串行拆分（含"先...再..."/"然后"/"之后"等关键词）
- [ ] 实现复杂指令 → LLM 辅助解析（可选, P2）

### 3.2 DAG 管理
- [ ] 实现 `PlanDAG(tasks) (*ExecutionPlan, error)` — 根据依赖构建 DAG
- [ ] 实现 `DetectCycles(tasks) error` — 环检测（Kahn 算法：计算入度，入度为 0 入队，循环移除，最终有剩余节点则存在环）
- [ ] 实现 `TopologicalSort(tasks) []*PlannedTask` — 拓扑排序（Kahn 算法 BFS 实现，输出执行顺序）

### DAG 算法描述

```
DetectCycles (Kahn 算法):
  1. 计算每个节点的入度（依赖该节点的前置节点数）
  2. 将所有入度为 0 的节点加入队列
  3. 循环：从队列弹出节点 → 减少其下游节点的入度
     → 下游节点入度变为 0 时加入队列
  4. 如果处理完的节点数 == 总节点数 → 无环
     否则 → 存在环，返回剩余节点 ID 列表

TopologicalSort:
  基于 Kahn 算法 BFS 实现，按入度 0 → 入队顺序输出

checkDAG 依赖解锁（Orchestrator.PollQueue 调用）:
  每次 Poll 循环中：
  1. 遍历所有 Pending 状态的 PlanTask
  2. 对每个 Pending 任务，检查其 Dependencies 列表中所有 ID
  3. 如果所有依赖的 Status == NodeDone → 标记为 Ready
  4. 如果有依赖 Status == NodeFailed → 标记为 Blocked
  5. Ready 任务进入下一轮调度（受 Parallelism 限制）

### 3.3 角色/技能推断
- [ ] 实现 `InferRoleSkills(instruction) (role string, skills []string)` — 简单匹配（"编译"→coder, "部署"→ops）
- [ ] 实现 `InferRoleSkillsLLM(instruction) (role string, skills []string)` — LLM 增强（可选,P2）

## Phase 4: Orchestrator Poll 循环

### 4.1 PollQueue — 队列调度
- [ ] 实现 `PollQueue(ctx)` — 主调度循环
- [ ] 实现 `findReadyTasks() []*PlannedTask` — 找出所有 ready 任务
- [ ] 实现 `applyParallelismLimit(tasks) []*PlannedTask` — 应用并行度限制
- [ ] 实现 `submitPlanTask(ctx, planTask) error` — 单任务调度（EnsureClient + Scheduler.Submit）
- [ ] 实现 `watchTask(taskID, planTaskID)` — 事件监听 goroutine
- [ ] 实现 `checkDAG()` — DAG 依赖解锁（pending→ready）
- [ ] 实现 `onTaskCompleted(planTaskID, result)` — 任务完成回调
- [ ] 实现 `onTaskFailed(planTaskID, err)` — 任务失败回调 → 触发 Healer
- [ ] 实现 `onPlanComplete()` — 计划完成回调（统计 + Scaler 清理）
- [ ] 实现 `EnqueueMessage(content)` — 非命令消息入队

### 4.2 PollHealing — 自愈轮询
- [ ] 实现 `PollHealing(ctx)` — 委托 healer.ProcessPending

### 4.3 PollScale — 扩缩容轮询
- [ ] 实现 `PollScale(ctx)` — 委托 scaler.Evaluate + scaler.Execute

### 4.4 主流程
- [ ] 实现 `Poll(ctx)` — 一次完整的轮询（Queue + Healing + Scale）
- [ ] 实现 `Trigger()` — 手动触发轮询
- [ ] 实现 `Tick() <-chan time.Time` — ticker channel
- [ ] 实现 `Done() <-chan struct{}` — done channel
- [ ] 实现 `QueueDepth() int` — 队列深度

## Phase 5: Healer — 自愈机制

### 5.1 核心结构
- [ ] 创建 `pkg/agent/auto_healer.go`
- [ ] 定义 `HealOperation`（TaskID, PlanTaskID, Action, Attempts, LastAttempt, Reason）
- [ ] 定义 `HealAction` 枚举 (Retry/Escalate/RestartClient/Abort/None)
- [ ] 定义 `Healer` 结构体 (pending queue, policy)
- [ ] 实现 `NewHealer(policy RetryPolicy) *Healer`

### 5.2 失败恢复
- [ ] 实现 `OnTaskFailed(task, planTask) HealAction` — 失败事件处理
- [ ] 实现 `healRetry(ctx, op) error` — 重试策略：
  - 排除失败 client
  - 按需启动新 client
  - 指数退避延迟
  - 重构建新 Task → Scheduler.Submit
- [ ] 实现 `healEscalate(op) error` — 升级通知（EventBus 告警）
- [ ] 实现 `healAbort(op) error` — 放弃（标记 PlanTask = failed）

### 5.3 客户端断线恢复
- [ ] 实现 `OnClientStale(clientID) HealAction` — 处理 Registry 的 stale 回调
- [ ] 实现 `healRestartClient(clientID) error` — 重启流程：
  - 获取 client 运行中 tasks
  - Scheduler.HandleTaskPaused
  - KillClient + SpawnClient
  - 触发 TryDispatch

### 5.4 轮询处理
- [ ] 实现 `ProcessPending(ctx)` — 遍历 pending 队列
- [ ] 实现 `Enqueue(op HealOperation)` — 入队恢复操作
- [ ] 实现 `Dequeue()` — 出队

## Phase 6: AutoScaler — 扩缩容决策

### 6.1 核心结构
- [ ] 创建 `pkg/agent/auto_scaler.go`
- [ ] 定义 `ScaleAction`（Type, Count, Role, Skills, ClientID）
- [ ] 定义 `ScaleType` (None/Up/Down)
- [ ] 定义 `AutoScaler` 结构体 (lastScaleUp, lastScaleDown, cooldown)
- [ ] 实现 `NewAutoScaler() *AutoScaler`

### 6.2 扩容决策
- [ ] 实现 `EvaluateScaleUp(orch, config, registry, pool) ScaleAction`
  - 队列深度 > parallelism*2 && client 数 < max_clients → ScaleUp
- [ ] 实现 `evaluateScaleDown(orch, config, pool) ScaleAction`
  - 空闲 client > idle_timeout && client 数 > min_clients → ScaleDown

### 6.3 执行
- [ ] 实现 `Execute(action ScaleAction)` — 执行扩缩容
  - ScaleUp → pool.SpawnClient
  - ScaleDown → pool.KillClient
  - ScaleNone → noop
- [ ] 实现 `Evaluate(orch, config, registry, pool) ScaleAction` — 完整决策入口

## Phase 7: CLI 命令 (/auto 家族)

### 7.1 创建 cmd_auto.go
- [ ] 创建 `pkg/commands/cmd_auto.go`
- [ ] 实现 `autoCommand()` — 完整命令树定义
- [ ] 实现 `handleAutoMode()` — 切换 chat/manual/auto 模式
- [ ] 实现 `handleAutoRun()` — 提交并自动执行（调用 orchestrator.SubmitTask）
- [ ] 实现 `handleAutoStep()` — 手动单步执行（调用 orchestrator.SubmitRawTask）
- [ ] 实现 `handleAutoLoop()` — 自动循环（解析 N + 指令）
- [ ] 实现 `handleAutoStop()` — 停止执行
- [ ] 实现 `handleAutoStatus()` — 显示 orchestrator/client/队列状态表
- [ ] 实现 `handleAutoQueue()` — 管理队列（add/list/clear/remove）
- [ ] 实现 `handleAutoHistory()` — 查看执行历史
- [ ] 实现 `handleAutoConfig()` — 查看/修改运行时配置

### 7.2 命令注册
- [ ] `pkg/commands/builtin.go`：注册 `autoCommand()`
- [ ] `pkg/commands/commands.go`：Runtime struct 增加 `Orchestrator *AutoLoopOrchestrator`

### 7.3 别名
- [ ] `/autotask` → `/auto run`
- [ ] `/autostep` → `/auto step`
- [ ] `/autoloop` → `/auto loop`

## Phase 8: AgentLoop 集成

### 8.1 AgentLoop 结构扩展
- [ ] `pkg/agent/agent.go`：`AgentLoop` struct 增加 `orchestrator *AutoLoopOrchestrator`

### 8.2 Run() 修改
- [ ] Run() 中增加 `case <-al.orchestrator.Tick()` — 1s 定时轮询
- [ ] Run() 中增加 `case <-al.orchestrator.Done()` — 完成回调
- [ ] Inbound 消息处理路由：Auto/Manual 模式非命令消息 → orchestrator.EnqueueMessage
- [ ] Manual 模式下发送等待提示

### 8.3 初始化
- [ ] `pkg/agent/agent_init.go`：`NewAgentLoop()` 中创建 AutoLoopOrchestrator
- [ ] 注入 scheduler, registry, eventBus 依赖

## Phase 9: 测试

### 9.1 单元测试
- [ ] `auto_orchestrator_test.go`：状态管理 / 模式切换 / DAG 依赖解锁 / Poll 循环
- [ ] `client_pool_test.go`：Spawn/Kill/Ensure/ListIdle / 并发安全
- [ ] `auto_planner_test.go`：指令解析 / DAG 构建 / 环检测
- [ ] `auto_healer_test.go`：失败重试 / client 恢复 / 升级策略 / 指数退避
- [ ] `auto_scaler_test.go`：扩容条件 / 缩容条件 / 冷却期
- [ ] `cmd_auto_test.go`：CLI 命令解析 / handler 路由 / 别名

### 9.2 Given/When/Then 测试用例映射
- [ ] **S1-1**: Given=ModeManual, When=普通消息 → Then=消息转为单次任务 (runTurn 验证)
- [ ] **S1-2**: Given=ModeManual, When=`/auto step<指令>` → Then=强制手动提交一次
- [ ] **S1-3**: Given=手动执行中, When=`/auto stop` → Then=取消当前任务
- [ ] **S1-4**: Given=ModeManual无可用client, When=提交任务 → Then=ClientPool.EnsureClient("executor")
- [ ] **S2-1**: Given=ModeAuto, When=队列非空 → Then=持续处理队列
- [ ] **S2-2**: Given=Auto模式有DAG任务, When=依赖完成 → Then=加入可调度队列
- [ ] **S2-3**: Given=Auto模式运行中, When=`/auto stop` → Then=当前任务完成后停止
- [ ] **S2-4**: Given=loop=N, When=N次完成 → Then=退出自动模式
- [ ] **S3-1**: Given=任意模式, When=`/auto` → Then=显示帮助菜单
- [ ] **S3-2**: Given=任意模式, When=`/auto mode auto` → Then=切换并显示状态
- [ ] **S9-1**: Given=ModeAuto, When=`/hermes` → Then=停止队列+切换到Hermes+保留队列
- [ ] **S9-2**: Given=ModeHermes, When=`/auto mode auto` → Then=暂停Hermes+恢复队列
- [ ] **S10-1**: Given=ModeManual, When=非命令 → Then=创建ExecutionPlan并派发

### 9.3 集成测试
- [ ] 完整端到端流程：/auto run "简单指令" → Scheduler 派发 → 完成
- [ ] DAG 流程：串行依赖 → 自动顺序执行
- [ ] 自愈流程：模拟 TaskFailed → Healer 重试 → 成功
- [ ] Client 断线：模拟 client 断开 → Healer 恢复 → 任务重新调度
- [ ] 扩缩容：模拟队列堆积 → Scaler 扩容 → 空闲后缩容
- [ ] 模式切换：chat→auto→manual→chat
- [ ] /auto 与 /hermes 互切：auto→hermes(保留队列)→auto(恢复队列)

## Phase 10: 边界处理 + 文档

### 10.1 并发安全
- [ ] 所有 Orchestrator/ClientPool/Healer/Scaler 操作使用 sync.Mutex 保护
- [ ] Poll 循环中避免死锁（先 unlock 再调用外部组件）
- [ ] watchTask goroutine 的安全退出（context cancellation）
- [ ] Ticker channel 防阻塞（select + default）

### 10.2 状态恢复
- [ ] Server 重启后 orchestrator 状态重置为 Idle
- [ ] 正在执行的 task 通过 Scheduler 恢复（TaskPaused→TryDispatch）
- [ ] ManagedClient 状态重建（检查 Registry 中的存活 client）

### 10.3 错误处理
- [ ] SpawnClient 超时（30s）→ 清理并返回 error
- [ ] KillClient 超时（30s）→ SIGKILL 强制终止
- [ ] EnsureClient 无可用 client + max_clients 已满 → 等待队列
- [ ] Healer 连续失败（5 次）→ 停止自愈，升级告警
- [ ] Scaler 冷却期内不重复触发

### 10.4 回滚与降级
- [ ] Orchestrator 关闭时，正在运行的 Plan 标记为 Cancelled，已提交的 Task 不受影响（Scheduler 继续执行）
- [ ] 降级路径：AutoMode 不可用 → 降级为 ManualMode → 降级为 ChatMode（用户仍可正常对话）
- [ ] ClientPool 无法 spawn 时 → 降级为使用已有 client（如果有），无 client 时提示用户手动启动
- [ ] TaskPlanner LLM 不可用时 → 降级为纯规则解析（单任务模式，不拆分 DAG）

### 10.5 文档
- [ ] `docs/auto_loop_orchestrator.md`：架构设计文档
- [ ] `docs/auto_cli.md`：/auto 命令参考手册
- [ ] `docs/client_pool.md`：客户端生命周期管理
- [ ] `docs/healer.md`：自愈策略说明
