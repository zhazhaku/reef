# LHT Engine — 开发 Client 工作分配（V1）

> 基于 GSD 路线图（`.planning/lht-engine/ROADMAP.md`）
> 方案：峰值 4 客户端并行（C1-C4），C5 参与 W5B；W6 集成阶段 C1+C2 回归
> 客户端复用：`/root/reef_c1..c5`（role=coder，skills=exec,go,bash,github，capacity=2，model=deepseek-v4-pro）

---

## 0. 分配总览

| Client | 角色 | Wave | 负责人文件 | 交付判据 |
|--------|------|------|-----------|---------|
| **C1** | 内核开发 | W1 → W3A → W6 | model.go, state.go, engine.go, lht_e2e_test.go | 引擎循环全绿 |
| **C2** | 持久化+协作 | W2A → W4A → W6 | store.go, roles.go, executor.go | 存储+三角色全绿 |
| **C3** | 预算+防死循环 | W2B → W4B | budget.go, grounding.go, planning.go, anti_stall.go, notifier.go | 预算+防死全绿 |
| **C4** | 命令+API | W3B → W5A | cmd_lht.go, api.go | 命令+API 全绿 |
| **C5** | UI+能力 | W5B | capability.go, lht.html, lht.js | 能力测试+UI smoke |

**串行依赖**：W2A/W2B 需 W1 完成（model/state 定稿）；W3A 需 W2A/W2B 完成；W4A/W4B 需 W3A；W5A 需 W3A/W4B；W5B 需 W5A；W6 需全部。

**TDD 硬规则（每个任务）**：
1. 先写 `_test.go` 测试（RED）
2. 再写实现（GREEN）
3. `go test` 通过 + `go vet` 通过（REFACTOR）
4. 每个 Wave 结束时 `go build ./...` 回归

---

## 1. C1 — 内核开发（W1 → W3A → W6）

### W1: Foundations（第 1 天）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C1-01 | state_test.go：状态枚举/转换表/非法拒绝/PAUSED/ESCALATED→ABORTED | state_test.go | TDD RED |
| C1-02 | model_test.go：Goal/TaskNode/Plan/Budget/ReviewRecord/UserReply | model_test.go | TDD RED |
| C1-03 | model.go 实现（含 json tag、计数器字段） | — | GREEN |
| C1-04 | state.go 实现（transitions map + CanTransition） | — | GREEN |
| C1-05 | 运行 `go test -run "TestState|TestModel"` + vet | — | REFACTOR |

### W3A: 引擎核心循环（第 4-5 天，需 W1/W2A/W2B）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C1-06 | engine_test.go：NewEngine/NewGoal/完整生命周期/门机制/stop→ABORTED | engine_test.go | TDD RED |
| C1-07 | engine.go：Engine 结构/NewGoal/Task.run/Dispatch/Reply | — | GREEN |
| C1-08 | 状态转换后 persist(state.json) 挂钩 | — | GREEN |
| C1-09 | PAUSED 退出主循环 + ESCALATED 等待指示 | — | GREEN |
| C1-10 | replyCh buffered=3（P1-06 落地） | engine_test.go 补充 | GREEN |
| C1-11 | 运行 `go test -run "TestEngine"` + vet | — | REFACTOR |

### W6: 集成测试 + 文档（第 11-12 天，需全部）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C1-12 | lht_e2e_test.go：完整生命周期 E2E（mock gen/eval/rev） | lht_e2e_test.go | TDD |
| C1-13 | lht_e2e_test.go：暂停/恢复/终止/插入/预算耗尽/停滞升级全路径 | lht_e2e_test.go | TDD |
| C1-14 | crash 恢复测试：模拟进程重启 + `reef lht resume` | lht_e2e_test.go | TDD |
| C1-15 | 防死循环专项测试（tasks 10.4） | lht_e2e_test.go | TDD |
| C1-16 | 文档：README/REEF_SYSTEM LHT 说明 + 命令示例 | — | 交付 |
| C1-17 | `go test ./pkg/reef/lht/...` 全绿 + `openspec validate` | — | 验收 |

---

## 2. C2 — 持久化+协作（W2A → W4A → W6）

### W2A: 持久化存储（第 2-3 天，需 W1）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C2-01 | store_test.go：目录结构/往返/原子写/多文件顺序/崩溃模拟 | store_test.go | TDD RED |
| C2-02 | store_test.go：checkpoint 审计职责 + plan_version 校验 | store_test.go | TDD RED |
| C2-03 | store.go 实现（atomicWrite 临时文件+rename） | — | GREEN |
| C2-04 | SaveGoal/SavePlan/SaveState/SaveBudget/SaveCheckpoint/AppendLog/SaveReview | — | GREEN |
| C2-05 | LoadTask（state.json 权威恢复 + plan_version 校验） | — | GREEN |
| C2-06 | goalID 路径穿越防护 | store_test.go 补充 | GREEN |
| C2-07 | 运行 `go test -run "TestStore"` + vet | — | REFACTOR |

### W4A: 三角色 + 执行（第 6-7 天，需 W3A）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C2-08 | roles_test.go：YAML 加载/映射/拉起命令/隔离/回收 | roles_test.go | TDD RED |
| C2-09 | 三个 YAML：skills/roles/lht-{gen,eval,rev}.yaml | — | 交付 |
| C2-10 | roles.go：RoleManager（EnsureRole/Release，exec 拉起） | — | GREEN |
| C2-11 | executor_test.go：提交/eval 解析/重试/评审裁决/终审/持久化 | executor_test.go | TDD RED |
| C2-12 | executor.go：submitSubtask/evalSubtask/reviewSubtask/finalApprove | — | GREEN |
| C2-13 | 评审 Schema 解析器（{verdict,issues,score} + ≤2 次重试） | — | GREEN |
| C2-14 | 运行 `go test -run "TestRoles|TestExecutor"` + vet | — | REFACTOR |

### W6: 回归配合（第 11-12 天）
| # | 任务 | 说明 |
|---|------|------|
| C2-15 | `go build ./...` + `go vet ./...` 全量回归 | 确保不破坏现有包 |
| C2-16 | 协助 C1 集成测试中三角色 mock 部分 | 协作 |

---

## 3. C3 — 预算+防死循环（W2B → W4B）

### W2B: 预算+GROUNDING/PLANNING（第 2-3 天，需 W1）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C3-01 | budget_test.go：基准表/复杂度系数/重试系数/最大迭代/建议书 | budget_test.go | TDD RED |
| C3-02 | grounding_test.go：强制提问/循环/暂停恢复/问答超限 | grounding_test.go | TDD RED |
| C3-03 | planning_test.go：分类路由/DAG/能力清单/打回超限 | planning_test.go | TDD RED |
| C3-04 | budget.go 实现（基准表+系数+建议书） | — | GREEN |
| C3-05 | grounding.go 实现（问题集+循环+goal.json） | — | GREEN |
| C3-06 | planning.go 实现（路由+DAG+capabilities） | — | GREEN |
| C3-07 | 运行三组测试 + vet | — | REFACTOR |

### W4B: 防死循环+通知（第 8-9 天，需 W3A）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C3-08 | anti_stall_test.go：repair N=3/strategy M=2/stall K=3/预算耗尽/求助报告/交互上限 | anti_stall_test.go | TDD RED |
| C3-09 | notifier_test.go：关键节点通知 | notifier_test.go | TDD RED |
| C3-10 | anti_stall.go 实现（计数器持久化到 state.json） | — | GREEN |
| C3-11 | Escalate 求助报告 + 预算硬上限检查 | — | GREEN |
| C3-12 | notifier.go 实现（接入消息管线，回调式） | — | GREEN |
| C3-13 | 运行 `go test -run "TestAntiStall|TestNotifier"` + vet | — | REFACTOR |

---

## 4. C4 — 命令+API（W3B → W5A）

### W3B: /lht 命令族（第 4 天，需 W1 引擎骨架）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C4-01 | cmd_lht_test.go：子命令路由/短别名等价/仅 /lht 前缀生效/参数解析 | cmd_lht_test.go | TDD RED |
| C4-02 | cmd_lht_test.go：reef lht CLI（list/status/resume/wake/gc） | cmd_lht_test.go | TDD RED |
| C4-03 | cmd_lht.go：ParseLhtCommand + 短别名映射 | — | GREEN |
| C4-04 | cmd_lht.go：各子命令 handler 绑定 engine | — | GREEN |
| C4-05 | cmd_lht.go：reef lht cobra 子命令注册 + help 输出 | — | GREEN |
| C4-06 | 运行 `go test -run "TestCmdLht"` + vet + build | — | REFACTOR |

### W5A: REST API + SSE（第 10 天，需 W3A/W4B）
| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C4-07 | api_test.go：list/detail/logs 分页/控制操作/SSE 事件 | api_test.go | TDD RED |
| C4-08 | api_test.go：404/400 错误路径 | api_test.go | TDD RED |
| C4-09 | api.go：注册 /api/v2/lht/* 路由 | — | GREEN |
| C4-10 | api.go：list/detail/logs handler | — | GREEN |
| C4-11 | api.go：pause/resume/stop/insert/approve/reject/reply handler | — | GREEN |
| C4-12 | SSE：engine 状态转换处 EventBus.Publish(lht_state/lht_log/lht_milestone) | — | GREEN |
| C4-13 | 运行 `go test -run "TestAPI"` + vet + 路由 smoke | — | REFACTOR |

---

## 5. C5 — UI+能力（W5B，需 W5A）

| # | 任务 | 测试文件 | 说明 |
|---|------|---------|------|
| C5-01 | capability_test.go：L1/L2 分级/三角色豁免 | capability_test.go | TDD RED |
| C5-02 | planning_routing_test.go：gsd/openspec/研究/通用路由 | planning_routing_test.go | TDD RED |
| C5-03 | capability.go 实现（L1/L2 表+豁免） | — | GREEN |
| C5-04 | lht.html：列表页（卡片式）+ 详情页三栏布局 | — | 前端 |
| C5-05 | lht.js：SSE 实时流 + 控制区 + 回答输入框 + hash routing | — | 前端 |
| C5-06 | 菜单入口 + go:embed 静态资源嵌入 | — | 交付 |
| C5-07 | 运行 `go test -run "TestCapability|TestPlanningRouting"` + vet + build | — | REFACTOR |

---

## 6. 依赖与交接点

| 交接 | 内容 | 时机 |
|------|------|------|
| W1 → W2A/W2B | model.go/state.go 定稿（类型签名） | W1 完成日 |
| W2A/W2B → W3A | store 接口 + budget/grounding/planning 接口 | W2 完成日 |
| W3A → W4A/W4B | engine.Dispatch/Reply + Task 生命周期接口 | W3A 完成日 |
| W3A/W4B → W5A | engine 控制接口 + 防死循环事件 | W4B 完成日 |
| W5A → W5B | REST/SSE 端点清单 | W5A 完成日 |
| W1-W5 → W6 | 全部接口 + 测试基线 | W5 完成日 |

## 7. 工时预估

| Client | W1 | W2 | W3 | W4 | W5 | W6 | 合计 |
|--------|:--:|:--:|:--:|:--:|:--:|:--:|:----:|
| C1 | 1 | — | 1.5 | — | — | 1.5 | 4 天 |
| C2 | — | 1.5 | — | 2 | — | 0.5 | 4 天 |
| C3 | — | 1.5 | — | 1.5 | — | — | 3 天 |
| C4 | — | — | 1 | — | 1 | — | 2 天 |
| C5 | — | — | — | — | 1.5 | — | 1.5 天 |
| **总** | 1 | 3 | 2.5 | 3.5 | 2.5 | 2 | **14.5 人天 ≈ 12 日历天** |
