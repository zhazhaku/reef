# LHT Engine（长时自主任务引擎）— GSD 路线图（V1）

> 基于 openspec `changes/lht-engine`（proposal/design/tasks + 7 specs，已通过终验 8.0/10，🟢 可直接进入实施）
> 创建: 2026-08-02
> 核心约束: **TDD 先行**（每个 Wave 必须先写测试，再实现）；计划越细越好；每个任务可单 session 完成并验证

---

## 0. 目标与原则

实现 `pkg/reef/lht/` 长时自主任务引擎子系统：目标驱动状态机 + 三方独立评审 + checkpoint 持久化 + 预算硬上限 + 防死循环 + `/lht` 命令族 + UI 长程面板。

**硬规则**：
1. **TDD 先行**：每个 Wave 第一步是写 `*_test.go`（RED），再实现（GREEN），再重构（REFACTOR）。禁止先实现后补测试。
2. **交付判据 = 测试通过**：每个 Wave 以 `go test ./pkg/reef/lht/... -run <Wave 前缀>` 全绿为完成判据。
3. **单 session 粒度**：每个任务 ≤1 session；L 级任务拆分到子步骤。
4. **回归防线**：Wave 完成后必须 `go build ./... && go vet ./pkg/reef/lht/...` 通过；不得破坏现有包（pkg/agent、pkg/commands、pkg/reef/server 等）。
5. **文件结构**：新建代码全部落在 `pkg/reef/lht/`，命名遵循 design D1。
6. **不复用不存在接口**：不调用 `registry.ListByRole()`（首版不存在）；三角色走 YAML 预定义 + `role.Load` + 自管理 roleClients 映射。

**目标源码文件清单（design D1）**：

| 文件 | 职责 | 依赖 |
|------|------|------|
| `model.go` | Goal/TaskNode/Plan/Budget/ReviewRecord/StateSnapshot | 无 |
| `state.go` | State 枚举 + transitions 转换表 + canTransition | model |
| `store.go` | workspace/longhorizon/{goal_id}/ 原子写持久化 | model |
| `budget.go` | 预算估算模板 v1 + 建议书 | model |
| `grounding.go` | GROUNDING 提问流程 + goal.json | model, store |
| `planning.go` | DAG 拆解 + 流程路由 + 能力清单 | model, store |
| `engine.go` | Engine 主类型 + Task.run 状态机循环 | 全部 |
| `roles.go` | lht-gen/eval/rev 定义 + roleClients 映射 + 回收 | engine |
| `executor.go` | 子任务执行 + EVALUATING + REVIEWING + FINAL_APPROVAL | engine, roles |
| `anti_stall.go` | repair_count / strategy_switch / 停滞检测 / 求助报告 | engine |
| `notifier.go` | 关键节点通知 | engine |
| `cmd_lht.go` | /lht 命令族解析 + reef lht CLI | engine |
| `api.go` | /api/v2/lht/* REST handler | engine |
| `capability.go` | L1/L2 能力分级 + 三角色豁免 | engine |

**对应 openspec 规格映射**：
- 状态机 → `specs/lht-lifecycle`
- 防死循环 → `specs/lht-anti-stall`
- 三方评审 → `specs/lht-triple-review`
- 持久化 → `specs/lht-persistence`
- 命令 → `specs/lht-commands`
- GROUNDING/PLANNING/预算 → `specs/lht-grounding-planning`
- UI/能力审批 → `specs/lht-ui-capabilities`

---

## 1. 里程碑与 Wave 结构

```
M1 引擎内核（Wave 1-3）    第 1-5 天   数据模型+状态机+持久化+预算+引擎循环+命令族
M2 质量与协作（Wave 4）    第 6-8 天   三角色+执行+防死循环+通知
M3 产品化（Wave 5-6）      第 9-12 天  REST/SSE/UI+能力审批+gsd融合+集成测试+文档
```

| Wave | 名称 | 客户端 | 依赖 | 预计 |
|------|------|--------|------|------|
| **W1** | Foundations：模型 + 状态机 | C1 | 无 | 1 天 |
| **W2A** | 持久化存储 | C2 | W1 | 1.5 天 |
| **W2B** | 预算模板 + GROUNDING/PLANNING | C3 | W1 | 1.5 天 |
| **W3A** | 引擎核心状态机循环 | C1 | W1,W2A,W2B | 1.5 天 |
| **W3B** | /lht 命令族 + reef lht CLI | C4 | W1（引擎骨架） | 1 天 |
| **W4A** | 三角色 + 执行 + 评估/评审 | C2 | W3A | 2 天 |
| **W4B** | 防死循环 + 升级求助 + 通知 | C3 | W3A | 1.5 天 |
| **W5A** | REST API + SSE 事件 | C4 | W3A,W4B | 1 天 |
| **W5B** | UI 长程面板 + 能力审批 + gsd/openspec 融合 | C5 | W5A | 1.5 天 |
| **W6** | 集成测试 + 防死循环专项 + 文档 | C1+C2 | W1-W5 | 1.5 天 |

**并行度**：峰值 4 客户端（W3 期间 C1+C2+C3+C4），W4 后 C2/C3 空闲可复用做集成。

**接口契约（Wave 间）——必须先定死**：
```go
// model.go 核心类型（W1 定稿）
type State string // 11 个状态常量
type Goal struct { ID, Title, Scope, Constraints, Acceptance, CreatedAt }
type TaskNode struct { ID, ParentGoalID, DependsOn []string, Status, Acceptance, RepairCount, StrategySwitchCount, StallRounds }
type Plan struct { Version int, Nodes []*TaskNode, Capabilities []string, GeneratedAt }
type Budget struct { TokenBudget, TimeBudget, MaxIterations, RepairLimit, StrategySwitchLimit int, Used map[string]int, Proposal *BudgetProposal }
type ReviewRecord struct { Stage, Verdict, Issues []Issue, Score int, At }
type StateSnapshot struct { GoalID, State, PlanVersion, SubtaskIndex, Counters, UpdatedAt }
type UserReply struct { Gate string; ID string; Channel string; ChatID string; Action string; Content string; ReplyCh chan ReplyResult }

// store.go 接口（W2A 定稿）
func (s *Store) SaveGoal(g *Goal) error
func (s *Store) SavePlan(p *Plan) error
func (s *Store) SaveState(snap *StateSnapshot) error   // 原子写（临时文件+rename）
func (s *Store) SaveBudget(b *Budget) error
func (s *Store) LoadTask(goalID string) (*RestoredTask, error)
func (s *Store) AppendLog(goalID, line string) error
func (s *Store) SaveCheckpoint(snap *StateSnapshot) error // 审计快照

// engine.go 主接口（W3A 定稿）
type Engine struct { tasks sync.Map; cfg Config; store *Store; notifier Notifier }
func NewEngine(cfg Config, store *Store, notifier Notifier) *Engine
func (e *Engine) NewGoal(title, scope string, channel, chatID string) (*Goal, error)
func (e *Engine) Dispatch(cmd string, args []string, reply UserReply) error
func (e *Engine) Reply(gate string, goalID string, action string, content string) error

// roles.go 契约（W4A 定稿）
type RoleClient struct { Role string; ClientID string; State string }
type RoleManager struct { roleClients map[string][]*RoleClient; bin string }
func (rm *RoleManager) EnsureRole(role string) (*RoleClient, error) // 匹配在线→exec 拉起
func (rm *RoleManager) Release(role, clientID string) error

// anti_stall.go 契约（W4B 定稿）
func ShouldSwitchStrategy(n *TaskNode) bool       // repair_count > N(3)
func ShouldEscalate(n *TaskNode) bool             // strategy_switch > M(2) 或 stall >= K(3)
func (e *Engine) Escalate(goalID string, report EscalationReport) error
```

---

## 2. Wave 1 — Foundations：模型 + 状态机（C1，1 天）

**目标**：定义 LHT 全部数据模型与状态机转换表，TDD 先行。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestState|TestModel"` 全绿。

### T1.1（TDD）先写 `state_test.go`（RED）
- [ ] 测试：11 个状态常量枚举值唯一且覆盖（GROUNDING/PLANNING/WAIT_APPROVAL/EXECUTING/EVALUATING/REVIEWING/FINAL_APPROVAL/COMPLETED/PAUSED/ESCALATED/ABORTED）
- [ ] 测试：`transitions` 表包含 design D2 全部合法转换
- [ ] 测试：`CanTransition` 拒绝非法转换（COMPLETED→EXECUTING 等），返回明确错误
- [ ] 测试：PAUSED→ABORTED、ESCALATED→ABORTED 合法（lifecycle spec P0-01）
- [ ] 测试：GROUNDING→PLANNING 合法；PLANNING→WAIT_APPROVAL 合法

### T1.2（TDD）先写 `model_test.go`（RED）
- [ ] 测试：Goal 结构字段完整（ID/Title/Scope/Constraints/Acceptance/CreatedAt）
- [ ] 测试：TaskNode 含 DependsOn 依赖、RepairCount/StrategySwitchCount/StallRounds 计数器（anti-stall 规格）
- [ ] 测试：Plan.Version 与 state.json plan_version 关联（persistence spec 多文件顺序）
- [ ] 测试：Budget 含 TokenBudget/TimeBudget/MaxIterations/RepairLimit(3)/StrategySwitchLimit(2)
- [ ] 测试：ReviewRecord 含 verdict/issues/score 结构（triple-review Schema）
- [ ] 测试：UserReply 三种门 reply 机制可构造

### T1.3 实现 `model.go`（GREEN）
- [ ] 定义 Goal/TaskNode/Plan/Budget/ReviewRecord/StateSnapshot/UserReply/Issue 结构体
- [ ] 每个结构体带 json tag；Plan 带 Version；Budget 带 Used 记录

### T1.4 实现 `state.go`（GREEN）
- [ ] State 类型 + 11 常量
- [ ] `transitions map[State][]State`（完整转换表，含 PAUSED/ESCALATED→ABORTED）
- [ ] `CanTransition(from,to State) error`
- [ ] `AllStates() []State` 供测试与 UI

### T1.5（REFACTOR）运行测试并修复
- [ ] `go test ./pkg/reef/lht/ -run "TestState|TestModel" -v` 全绿
- [ ] `go vet ./pkg/reef/lht/...` 通过

---

## 3. Wave 2A — 持久化存储（C2，1.5 天，依赖 W1）

**目标**：`workspace/longhorizon/{goal_id}/` 目录结构与原子写持久化，TDD 先行。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestStore"` 全绿。

### T2A.1（TDD）先写 `store_test.go`（RED）
- [ ] 测试：`NewStore(root)` 创建目录结构（goal.json/plan.json/state.json/budget.json/artifacts//logs//review//checkpoints/）
- [ ] 测试：`SaveGoal`→`LoadTask` 往返一致（原子写，临时文件+rename）
- [ ] 测试：`SavePlan`/`SaveBudget` 覆盖写原子性（N2：重规划覆盖写不产生半写文件）
- [ ] 测试：多文件写入顺序——先 plan.json/budget.json 后 state.json，state.json 记录 plan_version
- [ ] 测试：`SaveState` 崩溃模拟——写入中途进程被杀后磁盘仍是完整旧快照（可用注入失败点或直接验证原子写实现）
- [ ] 测试：`SaveCheckpoint` 保留历史版本（按周期），`LoadTask` 以 state.json 为恢复权威（P1-08）
- [ ] 测试：`AppendLog` 追加式日志
- [ ] 测试：`SaveReview` review/ 目录评审 JSON

### T2A.2 实现 `store.go`（GREEN）
- [ ] Store 结构 + 路径 helper（goalID 防路径穿越：仅允许 `[a-zA-Z0-9-]`）
- [ ] `atomicWrite(path, data)`：写临时文件 → fsync → rename
- [ ] SaveGoal/SavePlan/SaveState/SaveBudget/SaveCheckpoint/AppendLog/SaveReview
- [ ] `LoadTask(goalID)`：读 state.json（权威）+ plan.json + goal.json + budget.json
- [ ] 恢复时校验 plan_version 一致性；不一致则告警并回退旧 plan

### T2A.3（REFACTOR）运行测试并修复
- [ ] `go test ./pkg/reef/lht/ -run "TestStore" -v` 全绿
- [ ] `go vet ./pkg/reef/lht/...` 通过

---

## 4. Wave 2B — 预算模板 + GROUNDING/PLANNING（C3，1.5 天，依赖 W1）

**目标**：预算估算模板 v1、目标对齐提问流程、计划 DAG 拆解与流程路由，TDD 先行。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestBudget|TestGrounding|TestPlanning"` 全绿。

### T2B.1（TDD）先写 `budget_test.go`（RED）
- [ ] 测试：类型基准表（代码开发 150K/代码修改 80K/研究 100K/数据 120K/验证 80K/文档 60K）
- [ ] 测试：复杂度系数判定——子任务 ≤5 无外部依赖→1.0；6~15 或跨模块→1.25；>15 或深度依赖→1.5（P1-09）
- [ ] 测试：重试系数 1.3 语义——仅预留基础重试，建议书 MUST 明示「若需 N×M 全量重试，预计追加 X」（P1-05）
- [ ] 测试：最大迭代 = max(10, 子任务数×3)
- [ ] 测试：产出《预算建议书》字段完整（时间/token/最大迭代/修复轮次 3/策略切换 2）
- [ ] 测试：budget.json 已用/剩余实时更新

### T2B.2（TDD）先写 `grounding_test.go`（RED）
- [ ] 测试：新任务进入 GROUNDING 强制提问（范围/优先级/约束/验收标准）
- [ ] 测试：对齐循环——仍有疑问继续提问，无疑问生成 goal.json 转 PLANNING
- [ ] 测试：GROUNDING 中暂停保存已问答记录，恢复从未回答问题继续（P1-03）
- [ ] 测试：对齐问答超 10 轮 → ESCALATED（anti-stall 交互上限）

### T2B.3（TDD）先写 `planning_test.go`（RED）
- [ ] 测试：目标分类路由——完整项目→gsd / 代码变更→openspec / 研究→研究流程 / 其他→通用（D10）
- [ ] 测试：DAG 拆解生成 plan.json（子任务+依赖+验收标准）
- [ ] 测试：计划含能力清单（capabilities）供计划确认一并批准
- [ ] 测试：计划打回 → 回 PLANNING 重新规划，打回超 5 轮 → ESCALATED（anti-stall 交互上限）

### T2B.4 实现 `budget.go` / `grounding.go` / `planning.go`（GREEN）
- [ ] budget.go：基准表 + 系数判定 + 建议书计算
- [ ] grounding.go：问题集模板 + 循环状态机 + goal.json 生成
- [ ] planning.go：路由 + DAG 构建 + capabilities 声明

### T2B.5（REFACTOR）运行测试并修复
- [ ] 三组测试全绿；`go vet` 通过

---

## 5. Wave 3A — 引擎核心状态机循环（C1，1.5 天，依赖 W1/W2A/W2B）

**目标**：`engine.go` 主类型 + Task.run 状态机循环 + 三个等待门，TDD 先行。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestEngine"` 全绿。

### T3A.1（TDD）先写 `engine_test.go`（RED）
- [ ] 测试：`NewEngine` 初始化（tasks sync.Map、config、store、notifier）
- [ ] 测试：`NewGoal` 创建任务进入 GROUNDING 并持久化 state.json
- [ ] 测试：完整生命周期——GROUNDING→PLANNING→WAIT_APPROVAL→approve→EXECUTING→(mock 子任务)→EVALUATING→REVIEWING→FINAL_APPROVAL→approve→COMPLETED（lifecycle 正常完成路径）
- [ ] 测试：WAIT_APPROVAL 未批准 MUST NOT 执行任何子任务（关键阶段约束）
- [ ] 测试：reject 打回 → 回 PLANNING
- [ ] 测试：每状态转换后调用 `persist(state.json)`（Task.run 循环落盘）
- [ ] 测试：三种门 reply 机制——GROUNDING 提问、WAIT_APPROVAL 批准、FINAL_APPROVAL 终审
- [ ] 测试：PAUSED/ESCALATED 状态下 `Dispatch` 收到 stop → ABORTED（P0-01 全路径）
- [ ] 测试：`Dispatch` 未知子命令返回错误
- [ ] 测试：`Reply` 非法 gate 返回错误

### T3A.2 实现 `engine.go`（GREEN）
- [ ] Engine 结构 + NewEngine
- [ ] NewGoal（创建 Goal、初始化 Task、启动 goroutine）
- [ ] Task.run(ctx) 循环（design D2 伪代码：switch state → 等待/执行 → persist）
- [ ] Dispatch（命令路由到对应 handler）
- [ ] Reply（写入对应 gate 的 reply channel）
- [ ] PAUSED 退出主循环逻辑 / ESCALATED 等待用户指示逻辑

### T3A.3（REFACTOR）运行测试并修复
- [ ] `go test ./pkg/reef/lht/ -run "TestEngine" -v` 全绿
- [ ] `go vet ./pkg/reef/lht/...` 通过

---

## 6. Wave 3B — /lht 命令族 + reef lht CLI（C4，1 天，依赖 W1 引擎骨架）

**目标**：`cmd_lht.go` 消息命令解析（全称+短别名）+ `reef lht` 命令行入口，TDD 先行。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestCmdLht"` 全绿 + `go run ./cmd/reef lht --help` 正常。

### T3B.1（TDD）先写 `cmd_lht_test.go`（RED）
- [ ] 测试：子命令路由表（new/list/status/plan/approve/reject/pause/resume/stop/insert/budget/escalate/logs/artifacts/review/history/help）
- [ ] 测试：短别名等价性——`/lht ok`==`/lht approve`、`/lht no`==`/lht reject`、`/lht pz`==`/lht pause`、`/lht go`==`/lht resume`、`/lht x`==`/lht stop`、`/lht add`==`/lht insert`、`/lht b`==`/lht budget`、`/lht esc`==`/lht escalate`、`/lht ?`==`/lht help`（commands spec 短别名表）
- [ ] 测试：短别名仅在 `/lht` 前缀下生效——单独 `ok`/`no`/`pz` 不识别为 LHT 命令（P2-01）
- [ ] 测试：参数解析（list 状态过滤、logs n 分页、status id 必填）
- [ ] 测试：`reef lht` CLI 子命令（list/status/resume/wake/gc）路由
- [ ] 测试：命令 handler 调用 engine.Dispatch/Reply 正确参数

### T3B.2 实现 `cmd_lht.go`（GREEN）
- [ ] ParseLhtCommand(line) → (cmd, args) 路由 + 短别名映射
- [ ] 各子命令 handler 绑定 engine
- [ ] `reef lht` cobra 子命令注册（复用现有 cmd 结构）
- [ ] help 输出全称+短别名对照

### T3B.3（REFACTOR）运行测试并修复
- [ ] `go test ./pkg/reef/lht/ -run "TestCmdLht" -v` 全绿
- [ ] `go vet` 通过；`go build ./cmd/reef` 通过

---

## 7. Wave 4A — 三角色 + 执行 + 评估/评审（C2，2 天，依赖 W3A）

**目标**：roles.go 角色定义与 client 映射、executor 子任务执行、EVALUATING/REVIEWING/FINAL_APPROVAL 三阶段，TDD 先行。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestRoles|TestExecutor"` 全绿。

### T4A.1（TDD）先写 `roles_test.go`（RED）
- [ ] 测试：YAML 预定义加载——`skills/roles/lht-{gen,eval,rev}.yaml` 通过 `role.Load`+`Validate`（P0-04）
- [ ] 测试：roleClients 映射按 role 匹配在线 client；匹配不到时返回需拉起标记
- [ ] 测试：exec 拉起命令正确（`reef client --role lht-* ...`），不依赖 `registry.ListByRole`
- [ ] 测试：三角色上下文隔离——lht-eval/lht-rev 不共享 gen 执行上下文（triple-review spec）
- [ ] 测试：角色回收——任务 COMPLETED/ABORTED 且无占用则下线

### T4A.2（TDD）先写 `executor_test.go`（RED）
- [ ] 测试：EXECUTING 将子任务提交给 lht-gen client 并记录日志
- [ ] 测试：EVALUATING——lht-eval 输出解析为结构化 JSON `{verdict, issues, score}`；不合规输出重试 ≤2 次（P1-04）
- [ ] 测试：verdict=PASS → REVIEWING；verdict=FAIL → EXECUTING 修复（附 issues）
- [ ] 测试：REVIEWING——lht-rev 裁决：通过→FINAL_APPROVAL / 打回→EXECUTING / 升级→ESCALATED
- [ ] 测试：FINAL_APPROVAL——用户终审：通过→COMPLETED / 打回→EXECUTING
- [ ] 测试：review/ 目录评审记录持久化（review.json）

### T4A.3 实现 `roles.go` / `executor.go`（GREEN）
- [ ] 预定义三个 YAML（skills/roles/lht-{gen,eval,rev}.yaml）+ 加载逻辑
- [ ] RoleManager（roleClients 映射 + EnsureRole/Release）
- [ ] executor.go：submitSubtask / evalSubtask / reviewSubtask / finalApprove
- [ ] 评审 Schema 解析器（含重试）

### T4A.4（REFACTOR）运行测试并修复
- [ ] 两组测试全绿；`go vet` 通过；三角色 YAML 文件落盘

---

## 8. Wave 4B — 防死循环 + 升级求助 + 通知（C3，1.5 天，依赖 W3A）

**目标**：anti_stall.go 计数器与升级逻辑、notifier.go 通知、交互循环上限，TDD 先行。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestAntiStall|TestNotifier"` 全绿。

### T4B.1（TDD）先写 `anti_stall_test.go`（RED）
- [ ] 测试：repair_count 超 N=3 → 换策略（P0-02 关键：REVIEWING 打回→EXECUTING 不重置；仅首次进入 EXECUTING 初始化 0）
- [ ] 测试：strategy_switch_count 超 M=2 → 停滞信号
- [ ] 测试：连续 K 轮（默认 K=3，可配置 lht.stall_k）无实质进展 → ESCALATED（P0-03）
- [ ] 测试：预算耗尽 → 暂停 + 请求追加（不自行超限）
- [ ] 测试：求助报告生成（已完成/卡点/尝试策略/建议）
- [ ] 测试：交互循环上限——GROUNDING 10 轮 / WAIT_APPROVAL 打回 5 轮 / FINAL_APPROVAL 终审 3 轮 → ESCALATED
- [ ] 测试：任何循环有硬上限（严禁无限循环）；ESCALATED 不自行继续执行

### T4B.2（TDD）先写 `notifier_test.go`（RED）
- [ ] 测试：关键节点通知（对齐/计划待确认/里程碑/停滞/完成待终审/完成）
- [ ] 测试：通知携带 goalID + 状态 + 摘要

### T4B.3 实现 `anti_stall.go` / `notifier.go`（GREEN）
- [ ] 计数器持久化到 state.json（崩溃不丢失）
- [ ] ShouldSwitchStrategy / ShouldEscalate / 停滞检测
- [ ] Escalate 生成求助报告 + ESCALATED 转换
- [ ] 预算硬上限检查（budget.Used >= budget.TokenBudget → 暂停）
- [ ] notifier：接入现有消息管线（feishu/cli 等），回调式

### T4B.4（REFACTOR）运行测试并修复
- [ ] 两组测试全绿；`go vet` 通过

---

## 9. Wave 5A — REST API + SSE 事件（C4，1 天，依赖 W3A/W4B）

**目标**：`api.go` `/api/v2/lht/*` REST + SSE `lht_*` 事件发布，TDD 先行。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestAPI"` 全绿 + 路由注册 smoke test。

### T5A.1（TDD）先写 `api_test.go`（RED）
- [ ] 测试：`GET /api/v2/lht/list` 返回任务列表 JSON
- [ ] 测试：`GET /api/v2/lht/<id>` 返回任务详情 JSON（状态机位置/进度/子任务）
- [ ] 测试：`GET /api/v2/lht/<id>/logs?page=&size=` 分页日志
- [ ] 测试：`POST /api/v2/lht/<id>/pause` / `resume` / `stop` / `insert` / `approve` / `reject` / `reply` 执行对应操作并返回结果
- [ ] 测试：SSE 事件发布——状态机转换时 `EventBus.Publish(lht_state)`；日志流 `lht_log`；里程碑 `lht_milestone`
- [ ] 测试：非法 goalID 返回 404；非法操作返回 400

### T5A.2 实现 `api.go`（GREEN）
- [ ] api.go 注册 `/api/v2/lht/*` 路由（复用 server/ui 路由注册点）
- [ ] list/detail/logs handler
- [ ] pause/resume/stop/insert/approve/reject/reply handler（调 engine.Dispatch/Reply）
- [ ] SSE：复用 ui.EventBus 发布 lht_* 事件（在 engine 状态转换处挂钩）

### T5A.3（REFACTOR）运行测试并修复
- [ ] 测试全绿；`go vet` 通过；确认路由与现有 UI 路由无冲突

---

## 10. Wave 5B — UI 长程面板 + 能力审批 + gsd/openspec 融合（C5，1.5 天，依赖 W5A）

**目标**：`lht.html`/`lht.js` 面板、capability.go L1/L2 分级、D10 流程路由落地，TDD 先行（前端冒烟测试 + 后端单测）。
**交付判据**：`go test ./pkg/reef/lht/ -run "TestCapability"` 全绿；静态资源嵌入 smoke test；手动打开 UI 面板。

### T5B.1（TDD）先写 `capability_test.go`（RED）
- [ ] 测试：L1 白名单能力直接可用无需确认
- [ ] 测试：L2 新能力（新技能/插件/新 client）需计划确认时一并批准（ui-capabilities spec）
- [ ] 测试：三角色（lht-gen/eval/rev）内置豁免——创建不触发 L2 确认；技能集扩展仍需确认
- [ ] 测试：计划能力清单与 L1/L2 判定联动

### T5B.2（TDD）先写 `planning_routing_test.go`（RED，补充 Wave 2B）
- [ ] 测试：PLANNING 路由到 gsd 技能（完整项目）——生成 must_haves 作为 Evaluator 验收标准
- [ ] 测试：PLANNING 路由到 openspec 技能（代码变更）——spec/design 作为 Review 依据
- [ ] 测试：研究流程 / 通用 plan-and-execute 路由

### T5B.3 实现 `capability.go` + UI（GREEN）
- [ ] capability.go：L1/L2 分级表 + 判定逻辑 + 三角色豁免
- [ ] lht.html/lht.js：任务列表页（卡片式）+ 详情页三栏布局（步骤列表/实时执行流/评审+工件）+ 底部控制区
- [ ] 复用现有 go:embed 静态资源管线；URL hash routing `/#/lht`、`/#/lht/:goalID`（P1-11）
- [ ] 菜单增加「长程任务」入口

### T5B.4（REFACTOR）运行测试并修复
- [ ] 测试全绿；`go vet` 通过；`go build ./...` 通过；UI 静态资源嵌入正常

---

## 11. Wave 6 — 集成测试 + 防死循环专项 + 文档（C1+C2，1.5 天，依赖 W1-W5）

**目标**：端到端集成测试、crash 恢复、防死循环专项、文档更新，确保可交付。
**交付判据**：`go test ./pkg/reef/lht/...` 全绿 + `openspec validate lht-engine --json` 通过。

### T6.1（C1）集成测试 `lht_e2e_test.go`（TDD）
- [ ] 端到端：`/lht new` → GROUNDING（mock 回答）→ PLANNING → WAIT_APPROVAL → approve → EXECUTING（mock gen/eval/rev）→ FINAL_APPROVAL → approve → COMPLETED（tasks 10.1）
- [ ] 暂停/恢复/终止/插入/预算耗尽/停滞升级全路径（tasks 10.2，含 PAUSED→ABORTED、ESCALATED→ABORTED、修复超限升级、交互循环超限升级）
- [ ] 进程 crash 后 `reef lht resume <id>` 恢复（tasks 10.3）
- [ ] 防死循环专项：repair_count 不因评审打回重置；REVIEWING→EXECUTING 嵌套有界；N/M/K 超限必转 ESCALATED（tasks 10.4，P1-12）
- [ ] 多任务并发互不干扰（sync.Map 隔离）

### T6.2（C2）配套测试与验证
- [ ] `go test ./pkg/reef/lht/...` 全部通过（tasks 10.5）
- [ ] `openspec validate lht-engine --json` 验证 artifacts 完整性（tasks 10.6）
- [ ] 回归：`go build ./...`、`go vet ./...`、现有测试（pkg/agent、pkg/commands）不被破坏

### T6.3（C1）文档与示例
- [ ] README/REEF_SYSTEM 增加 LHT 引擎说明（tasks 10.7）
- [ ] `/lht` 命令使用示例（全称+短别名表）
- [ ] 更新 STATE.md 为完成态；git commit + tag v3.0.0

---

## 12. 风险与对策

| 风险 | 等级 | 对策 |
|------|:---:|------|
| 三角色 exec 拉起在 Android/ARM64 不稳定 | 中 | Wave 1 增加环境验证 gate（参考 agent-auto-loop-cli Phase 1.5）；失败降级复用现有 executor client |
| mock 客户端不足导致 W4A 测试受限 | 中 | executor 层做依赖注入（client 接口），单测用 mock；集成测试用真实 reviewer client |
| 与现有 server/ui 路由冲突 | 低 | api.go 路由前缀 `/api/v2/lht/` 独立；集成时跑全量 build |
| 状态机并发竞态（Task 多 goroutine） | 中 | 每 Task 独立 goroutine + sync.Mutex；计数器仅经 engine 方法修改；race 测试（可用 GOARCH=amd64 跑 -race） |
| 预算模板系数不准 | 低 | 模板 v1 固定默认值 + 建议书明示追加逻辑；后续按任务类型校准 |
| 依赖 W2B 路由调用 gsd/openspec 技能 | 低 | 首版调用技能入口函数；技能不存在时降级通用 plan-and-execute |

## 13. 验收清单（对应终验 8.0/10 结论）

- [ ] P0-01 PAUSED/ESCALATED→ABORTED 全路径测试通过
- [ ] P0-02 repair_count per-subtask 不重置测试通过
- [ ] P0-03 K=3 可配置（lht.stall_k）测试通过
- [ ] P0-04 三角色 YAML 预定义 + role.Load + 自管理映射（无 registry.ListByRole）验证通过
- [ ] N2 plan.json/budget.json 原子写顺序测试通过
- [ ] N3 ESCALATED 恢复决策规则测试通过
- [ ] P1-06 replyCh buffered=3 或 select-default 防护落地（实现 engine.go 时附加）
- [ ] 集成测试全绿 + openspec validate 通过
