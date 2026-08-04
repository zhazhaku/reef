# LHT Engine — GSD 项目状态

> 更新: 2026-08-04 10:51
> 状态: 🟢 **全部完成**
> 来源: openspec `changes/lht-engine`

---

## 里程碑

| 里程碑 | 状态 | 说明 |
|--------|------|------|
| M1 契约层（W1/W2A/W2B/W3A/W3B） | ✅ 完成 | model / state / store / budget / grounding / planning / engine / cmd_lht |
| M2 执行层（W4A 三角色+executor / W4B 防死循环+通知） | ✅ 完成 | roles / executor / anti_stall / notifier |
| M3 接口层（W5A REST+SSE / W5B UI+能力审批 / W6 集成+文档） | ✅ 完成 | api / capability / planning_routing / lht.html+lht.js / 文档收尾 |

## 已交付文件（pkg/reef/lht/，全部 go vet/build/test 通过）

### 源码文件（14 文件，~4248 行）

| 文件 | 行数 | Wave | 职责 |
|------|-----:|:---:|------|
| model.go | 133 | W1 | Goal / Plan / Budget / TaskNode / ReviewRecord / UserReply |
| state.go | 87 | W1 | 11 状态 + CanTransition + ErrIllegalTransition |
| store.go | 508 | W2A | 原子写 / checkpoint / goal.json/plan.json/state.json/budget.json |
| budget.go | 238 | W2A | IsConfigured / IsExhausted / 预算估算模板 |
| grounding.go | 247 | W2B | GROUNDING 对齐问答 |
| planning.go | 327 | W2B | BuildPlan / generateTaskDAG / ValidateDAG / 目标分类路由 |
| engine.go | 824 | W3A | Engine 主类型：状态机循环 + PAUSED/ESCALATED 恢复锚点 |
| cmd_lht.go | 342 | W3B | /lht 命令族 + reef lht CLI (Cobra) |
| roles.go | 196 | W4A | 三角色 RoleManager + YAML 加载 + EnsureRole |
| executor.go | 305 | W4A | 子任务执行，复用 reef_submit_task 语义 |
| anti_stall.go | 348 | W4B | 修复限次(N=3) / 策略切换(M=2) / 停滞检测(K=3) |
| notifier.go | 200 | W4B | 关键节点通知（计划待批/升级求助/完成等） |
| api.go | 558 | W5A | /api/v2/lht/* REST handler + SSE (lht_state/lht_log/lht_milestone) |
| capability.go | 176 | W5B | L1/L2 能力审批门 + 三角色豁免 + AuditCapabilities |

### 测试文件（15 文件，~287 测试函数）

| 文件 | 测试函数数 | 覆盖范围 |
|------|:---:|------|
| model_test.go | 26 | JSON 序列化、计数器、budget exhaustion、plan_version |
| state_test.go | 23 | 状态枚举、合法/非法转换、CanTransition |
| store_test.go | 24 | 原子写、checkpoint、路径防护、ReviewRecord |
| budget_test.go | 25 | IsConfigured、IsExhausted、预算模板 |
| grounding_test.go | 15 | GROUNDING 对齐问答 |
| planning_test.go | 23 | BuildPlan、ValidateDAG、MaxDAGDepth |
| engine_test.go | 18 | 完整生命周期、PAUSED/ESCALATED 恢复、replyCh buffered=3 |
| cmd_lht_test.go | 26 | /lht 命令解析、别名映射、Dispatch |
| roles_test.go | 10 | EnsureRole、Release、三角色豁免 |
| executor_test.go | 22 | 子任务执行、失败重试、context cancel |
| anti_stall_test.go | 19 | repair_count、strategy_switch、stall detection |
| notifier_test.go | 13 | 通知发送、多 channel |
| api_test.go | 18 | REST list/detail/logs/control/SSE |
| capability_test.go | 14 | L1/L2 分类、三角色豁免、AuditCapabilities |
| planning_routing_test.go | 11 | gsd/openspec/research/general 流程路由 |

### 角色 YAML（W4A）

| 文件 | 说明 |
|------|------|
| skills/roles/lht-gen.yaml | Generator 角色 |
| skills/roles/lht-eval.yaml | Evaluator 角色 |
| skills/roles/lht-rev.yaml | Reviewer 角色 |

### UI 面板（W5B）

| 文件 | 行数 | 说明 |
|------|-----:|------|
| pkg/reef/server/ui/static/lht.html | 89 | 任务列表页 + 详情页（三栏布局） |
| pkg/reef/server/ui/static/lht.js | 433 | SSE 实时流 + 控制交互 + hash routing |

### 文档（W6）

| 文件 | 说明 |
|------|------|
| README.md | 新增 LHT Engine 章节（状态机图 + 能力表 + REST API + CLI 命令 + 代码目录） |
| docs/lht-commands.md | /lht 命令使用指南（17 子命令 + 端到端流程） |
| .planning/lht-engine/STATE.md | 本文（最终态） |

## 测试统计

```
go test ./pkg/reef/lht/ -count=1 -timeout 120s
ok  	github.com/zhazhaku/reef/pkg/reef/lht	~6.9s

~287 测试函数，全部通过，0 FAIL
go vet ./pkg/reef/lht/   → 通过
go build ./...           → 通过
```

## 验收条件

- [x] P0-01: PAUSED/ESCALATED→ABORTED 测试覆盖（W1/W3A）
- [x] P0-02: repair_count per-subtask 不重置（W1 model）
- [x] P0-03: K=3 可配置（lht.stall_k）→ W4B 已落地
- [x] P0-04: 三角色 YAML 预定义（无 registry.ListByRole 依赖）→ W4A 已落地
- [x] N2: plan.json/budget.json 原子写顺序 → W2A ✅
- [x] N3: ESCALATED 恢复决策规则 → W3A ✅
- [x] P1-06: replyCh buffered=3（W3A）
- [x] `go test ./pkg/reef/lht/...` 全绿（~287 测试，~6.9s）
- [x] `go build ./...` + `go vet ./...` 通过

## 项目状态

🟢 **LHT Engine 全部 Wave 已完成并交付。**

- W1 Foundations: model + state ✅
- W2A Persistence: store + budget ✅
- W2B Planning: grounding + planning ✅
- W3A Core Engine: engine (状态机循环 + PAUSED/ESCALATED 恢复) ✅
- W3B Commands: cmd_lht (/lht 命令族 + reef lht CLI) ✅
- W4A Execution: roles + executor ✅
- W4B Safety: anti_stall + notifier ✅
- W5A Interface: api (REST + SSE) ✅
- W5B UI: capability + lht.html/lht.js ✅
- W6 Integration: 文档收尾 ✅
