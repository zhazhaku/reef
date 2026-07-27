# Agent Auto Loop Orchestrator — 功能规格

## S1: 手动模式 (ModeManual)

### 描述
用户逐条输入指令，orchestrator 执行完一次后自动等待，不继续自动循环。

### 触发方式
- 用户输入 `/auto step <instruction>`
- 用户输入 `/auto mode manual` 后每次正常聊天作为一步
- 别名 `/autostep <instruction>`

### 行为
```
用户: /auto step 检查服务器状态
Agent: [通过 Scheduler 派发]
       ✓ 内存: 45% ✓ CPU: 12% ✓ 磁盘: 67%
       [等待下一条指令...]

用户: /auto step 清理 /tmp 目录
Agent: [通过 Scheduler 派发]
       ✓ 已清理 23MB 临时文件
       [等待下一条指令...]
```

### 给定/当/则
```
Given: 用户处于 ModeManual
When:  用户发送正常消息（非命令）
Then:  Orchestrator 将消息转为单次任务 → Scheduler.Submit()
       等待任务完成后返回结果，继续等待

Given: 用户处于 ModeManual
When:  用户发送 /auto step <指令>
Then:  Orchestrator 强制按手动方式提交一次任务

Given: Orchestrator 正在手动模式执行
When:  用户发送 /auto stop
Then:  Orchestrator 取消当前任务，重置状态

Given: 手动模式下无可用 client
When:  提交任务
Then:  ClientPool.EnsureClient("executor") 自动启动新 client
       等待注册完成后才提交任务
```

## S2: 自动模式 (ModeAuto)

### 描述
Orchestrator 自动从任务队列取出任务，通过 Scheduler 派发给最佳 client，支持循环次数设定、DAG 依赖执行、并行度控制。

### 触发方式
- 用户输入 `/auto run <task>` — 提交并自动执行
- 用户输入 `/auto loop <N> <task>` — 自动循环 N 次
- 用户输入 `/auto mode auto` — 进入自动模式后依次执行队列

### 行为
```
用户: /auto run 每 30 秒采集一次系统指标，持续 5 分钟
Agent: 【Orchestrator 启动】
       [plan] 任务拆分为 10 次采集
       [client] 检测到无 executor → 自动启动进程...
       [client] executor-1 已注册 ✓
       [task #1] 采集系统指标 → 内存 45% CPU 12% ✓
       等待 30 秒...
       [task #2] 采集系统指标 → 内存 44% CPU 8% ✓
       ...
       [task #10/10] 采集系统指标 ✓
       ✅ 全部完成，释放空闲 client
```

### 给定/当/则
```
Given: 用户处于 ModeAuto
When:  任务队列非空
Then:  Orchestrator 持续处理队列，通过 Scheduler 派发任务

Given: 自动模式有 DAG 任务
When:  某任务的所有依赖已完成
Then:  自动将该任务加入可调度队列

Given: 自动模式正在运行
When:  用户发送 /auto stop
Then:  Orchestrator 等待当前任务完成后停止循环

Given: 自动模式 loop=N
When:  N 次循环完成
Then:  Orchestrator 自动退出自动模式
```

## S3: CLI 命令入口

### 描述
通过 `/auto` 前缀的统一命令入口控制 Orchestrator 行为。

### 命令列表
| 命令 | 参数 | 说明 |
|------|------|------|
| `/auto` | — | 显示完整帮助菜单 |
| `/auto mode` | `chat\|manual\|auto` | 切换运行模式 |
| `/auto run` | `<instruction>` | 提交并自动执行任务 |
| `/auto step` | `<instruction>` | 手动执行一步 |
| `/auto loop` | `<N> <instruction>` | 自动循环 N 次 |
| `/auto stop` | — | 停止执行 |
| `/auto status` | — | 查看 orchestrator/client/队列状态 |
| `/auto queue` | `[add\|list\|clear\|remove <id>]` | 管理任务队列 |
| `/auto history` | `[N]` | 显示最近 N 条执行历史 |
| `/auto config` | `[key=value]` | 查看/修改运行时配置 |

### 别名
- `/autotask` ≡ `/auto run`
- `/autostep` ≡ `/auto step`
- `/autoloop` ≡ `/auto loop`

### 给定/当/则
```
Given: 用户在任意模式
When:  输入 /auto
Then:  显示 Auto 模式的完整帮助菜单

Given: 用户在任意模式
When:  输入 /auto mode auto
Then:  切换到自动模式，显示当前队列和 client 状态

Given: 用户在自动模式
When:  输入 /auto stop
Then:  当前任务完成后停止循环，启动 Scaler 空闲清理
```

## S4: 任务队列 + DAG 依赖

### 描述
Orchestrator 维护一个 FIFO 任务队列，支持 DAG 依赖编排、优先级、并行度控制。

### 结构
```go
type PlannedTask struct {
    ID          string          `json:"id"`
    Instruction string          `json:"instruction"`
    RequiredRole string         `json:"required_role"`
    RequiredSkills []string     `json:"required_skills,omitempty"`
    Dependencies []string       `json:"dependencies,omitempty"` // PlanTask ID 列表
    Status      AutoTaskStatus  `json:"status"`                 // pending/ready/running/done/failed
    Priority    int             `json:"priority"`               // 0=normal, 1=high
    CreatedAt   time.Time       `json:"created_at"`
    Result      string          `json:"result,omitempty"`
    Error       string          `json:"error,omitempty"`
}
```

### 队列管理
| 命令 | 功能 |
|------|------|
| `/auto queue add <task>` | 追加任务到队列 |
| `/auto queue list` | 列出所有任务及其依赖状态 |
| `/auto queue clear` | 清空队列 |
| `/auto queue remove <id>` | 移除指定任务 |

### DAG 执行规则
- Task 的 Dependencies 全部完成 → 标记为 ready → 可被 Scheduler 调度
- 无依赖的 task 立即 ready → 可并行调度（受 MaxClients 限制）
- 某依赖 Failed → 下游任务标记为 blocked（不自动跳过）

### 给定/当/则
```
Given: 任务 A 依赖任务 B
When:  任务 B 未完成
Then:  任务 A 处于 pending 状态，不被提交给 Scheduler

Given: DAG 中某依赖任务失败
When:  该任务不可恢复
Then:  下游任务标记为 blocked，通知用户
```

## S5: 客户端生命周期管理 (Client Pool)

### 描述
Orchestrator 自动管理 reef client 进程的启动/销毁，确保任务有可用的执行环境。

### 能力
- 按需启动：根据任务所需 role/skills 自动创建 client 进程
- 保活监控：定期检查 client 心跳，断线自动重启
- 空闲回收：超过 `idle_timeout` 无任务的 client 自动销毁
- 并发限制：遵循 MaxClients + client capacity 限制

### 配置参数
| 参数 | 默认值 | 说明 |
|------|--------|------|
| `min_clients` | 1 | 最少保留的 idle client 数 |
| `max_clients` | 5 | 最多并发 client 数 |
| `idle_timeout` | 5m | 空闲 client 超时回收 |
| `client_heartbeat` | 30s | client 心跳检查间隔 |

### 启动流程
```
ClientPool.SpawnClient("coder", ["go", "docker"])
  1. 创建临时 REEF_HOME: /tmp/reef-client-xxx/
  2. 生成最小配置（不含完整 config.json 和 .security.yml）
  3. 通过环境变量 `REEF_API_KEY` 传递 API Key（而非复制整个 .security.yml）
  4. setsid reef client --role coder --skills go,docker --server ws://...
  5. 轮询 Registry.Get(clientID) → ClientConnected (30s 超时)
  6. 返回 ManagedClient{ClientID, PID, State, StartedAt}
```

### 销毁流程
```
ClientPool.KillClient(clientID)
  1. 标记 ManagedDraining（不再接受新任务）
  2. 等待当前 load=0（最长 30s）
  3. Kill(pid) → SIGTERM → 2s → SIGKILL
  4. Registry.Unregister(clientID)
  5. 清理临时 REEF_HOME
```

## S6: 自愈机制 (Healer)

### 描述
自动检测并恢复任务/客户端异常，减少人工介入。

### 自愈场景

| 场景 | 检测方式 | 恢复动作 |
|------|----------|----------|
| 任务执行失败 | Scheduler.HandleTaskFailed | 重试（排除失败 client）最多 3 次 |
| 客户端心跳超时 | Registry.ScanStale (30s) | 标记 Stale → 暂停该 client 任务 → 启动新 client |
| 客户端进程崩溃 | ClientPool 监控 PID 退出 | 同心跳超时 |
| DAG 任务阻塞 | 依赖任务失败且不可恢复 | 标记 downstream blocked → 通知用户 |
| 客户端过载 | capacity 耗尽 | 自动 ScaleUp 新 client |

### 重试策略
```go
type RetryPolicy struct {
    MaxRetries      int           // 默认 3
    RetryDelay      time.Duration // 默认 5s
    BackoffFactor   float64       // 默认 2.0 (指数退避)
    ExcludeFailedClient bool      // 默认 true (不重试同一 client)
    AutoSpawnOnRetry bool         // 默认 true (重试时自动启动新 client)
}
```

### 升级策略
- 重试 3 次失败 → EscalateToAdmin（通知管理员）
- 连续 5 次 client 崩溃 → 停止自动重启，通知管理员

## S7: 长时任务守护

### 描述
支持长时间运行的任务（>5min），提供专用 client + 心跳守护。

### 能力
- PersistentClient: 更长的心跳超时（5min vs 30s）
- 守护 goroutine: 每 30s 检查任务状态
- 断线恢复: 检测 TaskPaused → 新建 PersistentClient → 重新调度
- 超时配置: task 级可配置 timeout（默认 600s → 长任务可设为 3600s）

### 触发条件
```
当 TaskPlanner 解析到长时指令时，自动标记为 long_running=true
或用户显式指定: /auto run --timeout 3600 "执行全量数据迁移"
```

## S8: 扩缩容策略 (AutoScaler)

### 描述
根据队列深度和客户端负载，动态扩缩 client 数量。

### 扩容条件
- 队列深度 > `scale_up_threshold`（默认 3）
- 所有可用 client 的 load == capacity（满载）
- 当前 client 数 < `max_clients`

### 缩容条件
- client 空闲时间 > `idle_timeout`（默认 5min）
- 当前 client 数 > `min_clients`

### 决策表
| 队列深度 | 可用 client 负载 | 动作 |
|----------|-----------------|------|
| 0 | <50% | 缩容至 min_clients |
| 1-3 | <80% | 维持现状 |
| >3 | >=80% | 扩容 1 个 |
| >10 | >=80% | 扩容到 max_clients |

## S9: /auto 与 /hermes 命令交互

### 描述
定义 `/auto` 命令家族与现有 `/hermes` 命令家族之间的交互规则。

### 规则
1. **模式互斥**：ConversationMode 同时只能有一个值（chat / hermes / manual / auto）
2. **切换规则**：切换到某模式时，前一个模式的状态被挂起（队列保留，不丢失）
3. **命令优先级**：`/auto` 和 `/hermes` 命令在所有模式下均可执行（包括 chat 模式）
4. **权限穿透**：在 auto/manual 模式下，非 `/auto` 的普通命令（如 `/switch`, `/help`）仍然可执行

### 给定/当/则
```
Given: 当前处于 ModeAuto（自动模式）
When:  用户输入 /hermes
Then:  Orchestrator 停止当前队列处理（保留队列）
       切换到 ModeHermes
       进入 6 阶段协作工作流

Given: 当前处于 ModeHermes（六阶段协作模式）
When:  用户输入 /auto mode auto
Then:  Hermes 工作流被暂停（可恢复）
       切换到 ModeAuto
       Orchestrator 恢复队列处理

Given: 当前处于 ModeAuto
When:  用户输入普通命令如 /switch model xxx
Then:  该命令直接执行（不进入队列），执行完成后回到 auto 模式
```

## S10: ModeManual 非命令消息行为

### 描述
澄清手动模式下，用户发送的非命令消息（如"今天天气怎么样"）的处理行为。

### 规则
1. 在 ModeManual 下，**所有**非命令消息被视为"需要执行的任务"，进入队列
2. 每条消息生成一个单次 ExecutionPlan（含 1 个 PlannedTask）
3. 执行完后自动等待下一条指令
4. 如果用户想聊天而非执行任务，应切换至 ModeChat: `/auto mode chat`

### 给定/当/则
```
Given: 用户处于 ModeManual
When:  用户发送"今天天气怎么样"（非命令）
Then:  Orchestrator 创建一个 ExecutionPlan{instruction:"今天天气怎么样"}
       → 通过 Scheduler 派发 → 等待结果 → 继续等待

Given: 用户处于 ModeManual
When:  用户想聊天而非执行任务
Then:  应输入 /auto mode chat 切换到聊天模式
```

## S11: 非功能需求

### 性能
| 指标 | 目标 | 测量方式 |
|------|------|----------|
| Client spawn 延迟 | < 30s（含注册） | 从 SpawnClient 调用到 Registry 标记 Connected |
| Poll 循环延迟 | < 200ms（从 Tick 到完成一轮 Poll） | 单次 Poll 函数耗时 |
| 任务提交到派发延迟 | < 5s（提交后到 TryDispatch 成功） | reef.Task.CreatedAt 到 AssignedAt |
| Orchestrator 内存开销 | < 50MB（1000 队列任务 + 10 client 管理） | runtime.ReadMemStats |

### 可用性
| 场景 | 要求 |
|------|------|
| Orchestrator 崩溃 | 状态丢失可接受（重启后重置为 Idle），正在执行的 task 由 Scheduler 恢复（TaskPaused→TryDispatch） |
| 客户端断线 | Healer 负责重建，最长恢复时间 < 60s |
| Server 重启 | Orchestrator 状态重置，ManagedClient 状态从 Registry 重建 |

### 安全
| 需求 | 达标条件 |
|------|----------|
| 凭证隔离 | Spawn 的子进程只能访问其任务的 API Key，不能读取父进程完整 .security.yml |
| 进程隔离 | 子进程不能 kill 父进程或其他子进程 |
| 文件清理 | 子进程退出后临时 REEF_HOME 目录被彻底删除（含 config/.security） |
