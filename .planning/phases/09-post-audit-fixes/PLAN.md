# Phase 09: 审计修复 — 执行计划

> 基于 `REEF_THIRD_AUDIT.md` v1.1 的 32 个问题（9 P0 + 9 P1 + 9 P2 + 5 P3）
> GSD TDD 先行：每个任务先写测试 → 修复代码 → 验证通过

---

## 总览

| Wave | 聚焦 | 解决问题 | 预估 |
|------|------|:--------:|:----:|
| **W1: 协议层修复** | protocol.go 缺口 | 4 (P0-1,2,3,7) | 2 天 |
| **W2: 状态机 & 生命周期** | task.go + client | 3 (P0-4,5,6) | 3 天 |
| **W3: 短平快** | CLI/测试/内存泄漏 | 5 (P2-2, P3-1, P2-6, P1-7, P2-1) | 1 天 |
| **W4: Admin & 安全** | admin.go + server | 3 (P1-2, P1-5, P1-6) | 3 天 |
| **W5: 代码库统一** | 架构决策 | 5 (P0-8, P0-9, P1-1, P1-3, P1-8) | 5 天 |
| **W6: 进化-Raft 集成** | evolution ↔ raft | 1 (P1-9) + P2-8 + P2-9 | 4 天 |
| **W7: 清扫** | 剩余 P2/P3 | 9 (P2-3,4,5 + P3-2,3,4,5, P2-7,9) | 2 天 |

---

## Wave 1: 协议层修复（2 天）

### W1-Issue-1: P0-2 新增 `MsgError` 通用错误消息（0.5 天）

**TDD 步骤：**

```go
// 1. 先写测试
func TestMsgErrorRoundTrip(t *testing.T) {
    msg := NewMessage(MsgError, "t-1", ErrorPayload{
        Code: "ERR_UNKNOWN_TYPE",
        Message: "unknown message type: bogus",
        OriginalType: "bogus",
    })
    data, _ := json.Marshal(msg)
    var decoded Message
    json.Unmarshal(data, &decoded)
    assert(decoded.MsgType == MsgError)
}

func TestHandleWebSocket_UnknownType_ReturnsError(t *testing.T) {
    // 发送非法消息类型 → Server 应回复 MsgError 而非仅打 warn 日志
}
```

**文件：**
| 文件 | 操作 |
|------|------|
| `protocol.go` | MODIFY: 新增 `MsgError MessageType = "error"` + `ErrorPayload` 结构体 |
| `websocket.go` | MODIFY: 未知消息类型返回 MsgError |
| `protocol_test.go` | NEW: MsgError round-trip |
| `websocket_test.go` | NEW: 非法消息回复测试 |

---

### W1-Issue-2: P0-3 HeartbeatPayload 添加 `current_tasks`（0.5 天）

**TDD 步骤：**

```go
func TestHeartbeatPayloadWithTasks(t *testing.T) {
    pl := HeartbeatPayload{
        Timestamp:    time.Now().UnixMilli(),
        CurrentTasks: []string{"task-1", "task-2"},
    }
    data, _ := json.Marshal(pl)
    var decoded HeartbeatPayload
    json.Unmarshal(data, &decoded)
    assert(len(decoded.CurrentTasks) == 2)
}

func TestConnector_Heartbeat_IncludesCurrentTasks(t *testing.T) {
    // Connector 发送心跳时带上 TaskRunner 中的运行中任务列表
    runner.StartTask("t-1", "do", 1)
    time.Sleep(50ms)
    lastHeartbeat := captureHeartbeat(conn)
    assert.Contains(lastHeartbeat.CurrentTasks, "t-1")
}
```

**文件：**
| 文件 | 操作 |
|------|------|
| `protocol.go` | MODIFY: HeartbeatPayload 增加 `CurrentTasks []string` |
| `connector.go` | MODIFY: heartbeatLoop 获取运行中任务列表 |
| `task_runner.go` | MODIFY: 暴露 RunningTasks() 或类似方法 |

---

### W1-Issue-3: P0-7 TaskDispatchPayload 添加 `PreviousAttempts`（0.5 天）

**TDD 步骤：**

```go
func TestTaskDispatchPayloadWithAttempts(t *testing.T) {
    pl := TaskDispatchPayload{
        TaskID: "t-1",
        PreviousAttempts: []AttemptRecord{{
            AttemptNumber: 1,
            ErrorMessage: "timeout",
        }},
    }
    // round-trip
    data, _ := json.Marshal(pl)
    var decoded TaskDispatchPayload
    json.Unmarshal(data, &decoded)
    assert(len(decoded.PreviousAttempts) == 1)
}

func TestScheduler_Dispatch_WithAttemptHistory(t *testing.T) {
    // 调度器重分配时携带 attempt_history
    task := &reef.Task{ID: "t-1", Status: reef.TaskFailed}
    task.AddAttempt(AttemptRecord{ErrorMessage: "db down"})
    msg := scheduler.buildDispatch(task, "client-2")
    payload := msg.Payload.(TaskDispatchPayload)
    assert.Equal(1, len(payload.PreviousAttempts))
}
```

**文件：**
| 文件 | 操作 |
|------|------|
| `protocol.go` | MODIFY: TaskDispatchPayload 增加 `PreviousAttempts` |
| `scheduler.go` | MODIFY: dispatch 时附加 attempt_history |

---

### W1-Issue-4: P0-1 消息信封 `version` 字段（0.5 天）

**TDD 步骤：**

```go
func TestMessageEnvelopeWithVersion(t *testing.T) {
    msg := NewMessage(MsgHeartbeat, "", HeartbeatPayload{})
    assert(msg.Version == 1)  // 默认版本
    
    // round-trip 后版本保留
    data, _ := json.Marshal(msg)
    var decoded Message
    json.Unmarshal(data, &decoded)
    assert(decoded.Version == 1)
}
```

**文件：**
| 文件 | 操作 |
|------|------|
| `protocol.go` | MODIFY: Message 增加 `Version int` + 更新 NewMessage |

---

## Wave 2: 状态机 & 生命周期（3 天）

### W2-Issue-5: P0-4 状态机 `Created → Cancelled`（0.5 天）

**TDD 步骤：**

```go
func TestCanTransitionTo_Created_To_Cancelled(t *testing.T) {
    assert.True(TaskCreated.CanTransitionTo(TaskCancelled))
}

func TestTaskCancel_CreatedTask(t *testing.T) {
    task := &Task{ID: "t-1", Status: TaskCreated}
    err := task.Transition(TaskCancelled)
    assert.NoError(err)
    assert.Equal(TaskCancelled, task.Status)
}
```

**文件：**
| 文件 | 操作 |
|------|------|
| `task.go` | MODIFY: CanTransitionTo 添加 `TaskCancelled` |

---

### W2-Issue-6: P0-5 断连自动暂停任务（1.5 天）

**TDD 步骤：**

```go
func TestSwarmChannel_Disconnect_PausesRunningTasks(t *testing.T) {
    runner.StartTask("t-1", "work", 1)
    // 模拟断连
    swarm.onDisconnect()
    rt := runner.GetTask("t-1")
    assert.Equal("paused", rt.Status)
}

func TestConnector_Reconnect_ResumesPausedTasks(t *testing.T) {
    runner.PauseTask("t-1")
    // 模拟重连
    connector.onReconnect()
    rt := runner.GetTask("t-1")
    assert.Equal("running", rt.Status)
}
```

**文件：**
| 文件 | 操作 |
|------|------|
| `swarm.go` | MODIFY: 断连回调 → PauseTask |
| `connector.go` | MODIFY: 重连回调 → ResumeTask |
| `task_runner.go` | MODIFY: 暴露 ResumeAllTasks() |

---

### W2-Issue-7: P0-6 长任务定时进度心跳（1 天）

**TDD 步骤：**

```go
func TestTaskRunner_LongTask_ProgressHeartbeat(t *testing.T) {
    runner.SetProgressInterval(10 * time.Millisecond)
    runner.StartTask("t-1", "long work", 1)
    time.Sleep(25 * time.Millisecond)
    // 应至少收到 2 次 task_progress（含 progress_percent）
    progressCount := countMessages(conn, MsgTaskProgress)
    assert.True(progressCount >= 2)
}
```

**文件：**
| 文件 | 操作 |
|------|------|
| `task_runner.go` | MODIFY: 新增 progressLoop goroutine + SetProgressInterval |
| `task_runner.go` | MODIFY: reportProgress 改为由定时器触发 |

---

## Wave 3: 短平快（1 天）

### W3-Issue-8: P2-2 CLI submit 路由修复（0.5 小时）

**修复：** `command.go:208` 改为 POST `/tasks`（原 `/admin/tasks`）

### W3-Issue-9: P3-1 测试编译失败修复（1 小时）

**修复：** 删除 `server_coverage_test.go` 中对 `testError` / `setupAdminTest` 的引用，或补全定义

### W3-Issue-10: P2-6 重试错误分类器（2 小时）

**TDD 步骤：**

```go
func TestErrorClassifier_Retryable(t *testing.T) {
    assert.True(IsRetryable(context.DeadlineExceeded))
    assert.True(IsRetryable(&net.OpError{}))
}

func TestErrorClassifier_NonRetryable(t *testing.T) {
    assert.False(IsRetryable("syntax error"))
    assert.False(IsRetryable("invalid argument"))
}
```

### W3-Issue-11: P1-7 pendingControls 上限（1 小时）

**修复：** `websocket.go` 中 pendingControls Add 时检查容量（上限 100/clients），超过时丢弃旧消息

### W3-Issue-12: P2-1 超时检测改用 N 次心跳错失（1 小时）

---

## Wave 4: Admin & 安全（3 天）

### W4-Issue-13: P1-2 Admin 控制端点（1 天）

```
POST /admin/tasks/{task_id}/control
Body: {"action": "cancel" | "pause" | "resume"}
Response: {"status": "ok"}
```

### W4-Issue-14: P1-5 Admin 安全模型统一（1 天）

统一到 `Bearer <token>`（与 picoclaw 现行方式对齐）

### W4-Issue-15: P1-6 error_type 枚举统一（1 天）

统一到规范定义的 4 种类型 + TaskRunner 使用 ErrorClassifier

---

## Wave 5: 代码库统一（5 天 — 需架构决策）

### W5-Issue-16: P0-8 双代码库合并策略

**选项 A：** 以 picoclaw 为完整实现，移除主仓库重复文件（server/、cmd/、evolution/）
**选项 B：** 将 Raft 层移植到 picoclaw，统一用 `github.com/sipeed/reef` 导入路径
**选项 C：** 提取共享类型到独立子模块

### W5-Issue-17: P0-9 SkillsLoader 实现（1 天）

### W5-Issue-18: P1-8 AgentLoop → SwarmChannel 集成（2 天）

### W5-Issue-19: P1-3 角色技能自动加载（1 天）

---

## Wave 6: 进化-Raft 集成（4 天）

### W6-Issue-20: P1-9 Gene 提交走 Raft 共识（2 天）

### W6-Issue-21: P2-8 进化引擎 Handler 补齐（1 天）

### W6-Issue-22: P2-9 cnp context manager 配置接入（1 天）

---

## Wave 7: 清扫（2 天）

剩余 P2/P3 问题：P2-3, P2-4, P2-5, P2-7, P3-2, P3-3, P3-4, P3-5, P2-9

---

## 依赖关系

```
W1 (协议) ───┐
              ├──► W4 (Admin) ──► W6 (进化-Raft)
W2 (生命周期) ─┘     │               │
                     │               ├──► W7 (清扫)
W3 (短平快) ─────────┘               │
                                     │
W5 (架构决策) ──────────────────────┘
```

- W1, W2, W3 可并行执行（无相互依赖）
- W4 依赖 W1（需要协议类型就绪）
- W5 独立（纯架构决策，可随时进行）
- W6 依赖 W4（需要 Admin 端点）+ W1（需要协议类型）
- W7 依赖全部

---

## TDD 约束

1. 每个问题先写 `_test.go` → 编译通过但失败 → 修复代码 → 运行通过
2. 每个 Wave 完成后 `go test -count=1 ./pkg/reef/...` 全通过
3. `go vet ./pkg/reef/...` 零警告
4. `git add [specific files]` + `git commit` 每个问题独立提交

---

## 覆盖率门禁

| 包 | 当前 | 目标 |
|----|:---:|:---:|
| pkg/reef （协议） | 95% | ≥97% |
| pkg/reef/client | 83% | ≥90% |
| pkg/reef/server | 93% | ≥95% |
| pkg/reef/raft | 81% | ≥90% |
| pkg/channels/swarm | 22% | ≥50% |

---

*计划生成时间 2026-05-03 | 待审核*
