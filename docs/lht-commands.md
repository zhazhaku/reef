# /lht 命令使用指南

> LHT 长时自主任务引擎 — 命令参考
>
> 版本: v1 | 更新: 2026-08-04

---

## 命令总表

### 消息通道 `/lht` 命令族

在消息通道（Telegram / Feishu / CLI 等）内以 `/lht` 前缀发送。每个子命令均有短别名。

| 全称 | 别名 | 说明 | 参数 |
|------|:---:|------|------|
| `new` | `n` | 创建新长程任务 | `<目标描述>` |
| `list` | `ls` | 列出任务 | `[状态过滤]` |
| `status` | `st` | 查看任务详情 | `<id>` |
| `plan` | `p` | 查看计划 DAG 与预算建议书 | `<id>` |
| `approve` | `ok` | 批准计划/终审 | `<id>` |
| `reject` | `no` | 打回计划/终审 | `<id> <意见>` |
| `pause` | `pz` | 暂停任务 | `<id>` |
| `resume` | `go` | 恢复暂停/升级任务 | `<id>` |
| `stop` | `x` | 终止任务 | `<id>` |
| `insert` | `add` | 插入新要求 | `<id> <要求>` |
| `budget` | `b` | 查看预算使用明细 | `<id>` |
| `escalate` | `esc` | 查看升级求助报告 | `<id>` |
| `logs` | `log` | 查看执行日志 | `<id> [条数]` |
| `artifacts` | `art` | 列出任务工件 | `<id>` |
| `review` | `rev` | 查看三方评审记录 | `<id>` |
| `history` | `h` | 查看历史任务 | *(无)* |
| `help` | `?` | 显示命令帮助 | *(无)* |

> **注意**: 短别名（`ok`、`no`、`pz` 等）仅在以 `/lht` 开头的命令中解析（如 `/lht ok g-001`），单独输入不会触发 LHT 命令，避免聊天中误触发。

### 服务端 `reef lht` 命令族

在服务器终端执行，服务端管理视角：

| 命令 | 说明 |
|------|------|
| `reef lht list` | 列出所有任务（服务端视角） |
| `reef lht status <id>` | 查看任务状态 |
| `reef lht resume <id>` | 进程重启后从 checkpoint 恢复任务 |
| `reef lht wake` | 唤醒所有未完成跨天/周任务 |
| `reef lht gc` | 清理已完成/已终止任务的持久化目录 |

---

## 命令详解

### `/lht new` / `/lht n`

创建新的长程任务并立即进入 GROUNDING 强制提问对齐。

```
/lht new 搭建一个电商平台
/lht n 实现用户认证模块
```

创建后引擎将提出 4 个对齐问题（目标颗粒度 / 范围 / 约束 / 验收标准），用户需回答。

---

### `/lht list` / `/lht ls`

查看当前所有任务列表。可附加状态过滤。

```
/lht list
/lht ls running
/lht ls paused
```

返回卡片式列表：目标标题 + 状态徽标 + 进度 + 最后更新。

---

### `/lht status` / `/lht st`

查看指定任务的完整状态。

```
/lht status g-abc123
/lht st g-xyz789
```

返回：状态机位置、当前阶段、子任务进度、预算使用（已用/剩余轮次）、最近日志摘要。

---

### `/lht plan` / `/lht p`

查看任务的计划 DAG 与预算建议书。

```
/lht plan g-abc123
/lht p g-xyz789
```

返回：阶段 DAG（gsd 六阶段 / openspec 五阶段 等）、能力清单（L1/L2 标注）、预算建议书（估算时间 / token / 轮次）。

---

### `/lht approve` / `/lht ok`

批准计划与预算，使任务进入 EXECUTING 执行阶段。

```
/lht approve g-abc123
/lht ok g-xyz789
```

仅在 WAIT_APPROVAL（计划审批）或 FINAL_APPROVAL（终审）状态有效。

---

### `/lht reject` / `/lht no`

打回计划或终审，附带反馈意见。任务回到 PLANNING 重新规划（计划阶段）或 EXECUTING 修复（终审阶段）。

```
/lht reject g-abc123 预算估算偏低，增加测试覆盖
/lht no g-xyz789 缺少安全审查步骤
```

引擎收到后依据意见重规划，再提交确认。打回超限（N=5 次）自动升级 ESCALATED。

---

### `/lht pause` / `/lht pz`

暂停任务执行。记录当前状态为 PreviousState 锚点，恢复时回到该状态。

```
/lht pause g-abc123
/lht pz g-xyz789
```

暂停期间引擎暂停一切子任务执行，但继续响应命令。

---

### `/lht resume` / `/lht go`

恢复暂停或升级的任务。

```
/lht resume g-abc123
/lht go g-xyz789
```

从 PreviousState（暂停前）或 EscalatedFrom（升级前）锚点恢复。

---

### `/lht stop` / `/lht x`

终止任务，标记为 ABORTED。不可恢复。

```
/lht stop g-abc123
/lht x g-xyz789
```

---

### `/lht insert` / `/lht add`

在任务执行中插入新要求，引擎将重规划受影响部分。

```
/lht insert g-abc123 增加移动端适配
/lht add g-xyz789 支持多语言
```

新要求将触发 PLANNING 阶段重规划，可能影响预算建议书。

---

### `/lht budget` / `/lht b`

查看预算使用明细。

```
/lht budget g-abc123
/lht b g-xyz789
```

返回：已用轮次 / 总额、已用 token（估算）/ 总额、修复次数 / 上限、策略切换 / 上限。

---

### `/lht escalate` / `/lht esc`

查看升级求助报告。当任务因死循环检测、修复超限、策略切换超限等原因进入 ESCALATED 状态时，引擎生成求助报告。

```
/lht escalate g-abc123
/lht esc g-xyz789
```

返回：升级原因、停滞状态、已尝试修复次数、建议操作。

---

### `/lht logs` / `/lht log`

查看任务的执行日志。

```
/lht logs g-abc123
/lht log g-xyz789 20
```

可选参数指定返回条数（默认 10）。

---

### `/lht artifacts` / `/lht art`

列出任务生成的工件文件。

```
/lht artifacts g-abc123
/lht art g-xyz789
```

返回工件文件名与类型列表。

---

### `/lht review` / `/lht rev`

查看三方评审记录（lht-gen / lht-eval / lht-rev）。

```
/lht review g-abc123
/lht rev g-xyz789
```

返回：每轮评审的评审者角色、判定（PASS/FAIL）、评分、问题列表。

---

### `/lht history` / `/lht h`

查看历史任务列表（已完成的或已终止的）。

```
/lht history
/lht h
```

---

### `/lht help` / `/lht ?`

显示命令帮助。

```
/lht help
/lht ?
```

返回所有子命令的全称与短别名对照表。

---

## 端到端使用流程

```
# 1. 创建任务
/lht new 实现用户登录认证模块

# 引擎进入 GROUNDING，提问：
#   Q1: 目标颗粒度 —— 完整 auth 模块还是仅登录接口？
#   Q2: 技术约束 —— Go + JWT？语言/框架？
#   Q3: 范围 —— 含注册/密码重置/2FA 吗？
#   Q4: 验收标准 —— 单元测试覆盖率？集成测试？

# 2. 回答对齐问题（通过 reply API 或 UI 面板）
/lht reply g-abc123 完整 auth 模块，Go + JWT + bcrypt，含注册和密码重置，80% 测试覆盖

# 3. 引擎进入 PLANNING，自动拆解 DAG + 生成预算
#    完成后进入 WAIT_APPROVAL，推送通知

# 4. 查看计划
/lht plan g-abc123
# 返回 openspec 五阶段 DAG: proposal → specs → design → tasks → implementation

# 5. 批准计划
/lht approve g-abc123
# 或 /lht ok g-abc123

# 6. 引擎进入 EXECUTING，自动执行子任务
#    lht-gen 生成代码 → lht-eval 评估 → lht-rev 评审
#    实时通过 SSE 推送日志流 + 状态变更

# 7. 执行中可能的状态变化：
#    - 发现问题 → FAIL → EXECUTING 修复（最多 3 次）
#    - 评审打回 → EXECUTING 修复重做
#    - 修复超限 → ESCALATED 升级求助

# 8. 执行中可随时介入：
/lht pause g-abc123   # 暂停
/lht insert g-abc123 增加 2FA 支持  # 插入新要求
/lht resume g-abc123  # 恢复

# 9. 评审通过 → FINAL_APPROVAL → 确认 → COMPLETED
/lht approve g-abc123

# 10. 查看成果
/lht artifacts g-abc123   # 工件列表
/lht review g-abc123       # 评审记录
```
