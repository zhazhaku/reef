---
change: reef-scheduler-v2
artifact: specs
---

# Specs: Reef Scheduler v2

## 0. 数据目录统一

### Requirement: GetHome 优先级调整
系统 SHALL 按以下优先级解析数据目录：PICOCLAW_HOME 环境变量 > 可执行文件目录 > ~/.picoclaw。

#### Scenario: PICOCLAW_HOME 显式设置
- GIVEN 环境变量 PICOCLAW_HOME=/data/picoclaw
- WHEN GetHome() 被调用
- THEN SHALL 返回 /data/picoclaw

#### Scenario: 可执行文件目录可写
- GIVEN 未设置 PICOCLAW_HOME
- AND picoclaw 可执行文件位于 /opt/picoclaw/picoclaw
- AND /opt/picoclaw/ 目录可写
- WHEN GetHome() 被调用
- THEN SHALL 返回 /opt/picoclaw

#### Scenario: 可执行文件目录不可写（如 /usr/bin）
- GIVEN 未设置 PICOCLAW_HOME
- AND picoclaw 可执行文件位于 /usr/bin/picoclaw
- AND /usr/bin/ 目录不可写
- WHEN GetHome() 被调用
- THEN SHALL 降级返回 ~/.picoclaw

#### Scenario: 无可执行文件信息
- GIVEN 未设置 PICOCLAW_HOME
- AND os.Executable() 失败
- WHEN GetHome() 被调用
- THEN SHALL 降级返回 ~/.picoclaw

### Requirement: 目录可写性检查
GetHome() SHALL 在使用可执行文件目录前验证其可写性。

#### Scenario: 目录可写
- GIVEN 目录存在且可写
- WHEN isWritableDir() 检查
- THEN SHALL 返回 true

#### Scenario: 目录不可写
- GIVEN 目录为只读文件系统
- WHEN isWritableDir() 检查
- THEN SHALL 返回 false

### Requirement: Reef Server 默认数据路径
Reef Server SHALL 使用 GetHome() 解析的目录作为数据根目录。

#### Scenario: SQLite 默认路径
- GIVEN 未显式指定 StorePath
- AND GetHome() 返回 /opt/picoclaw
- WHEN Server 启动
- THEN SQLite 数据库 SHALL 位于 /opt/picoclaw/reef.db

#### Scenario: 显式指定 StorePath
- GIVEN 配置 StorePath=/data/reef.db
- WHEN Server 启动
- THEN SQLite 数据库 SHALL 位于 /data/reef.db

### Requirement: 其他数据文件路径统一
以下文件 SHALL 基于 GetHome() 解析路径：

| 文件 | 旧路径 | 新路径（基于 GetHome） |
|------|--------|----------------------|
| config.json | ~/.picoclaw/config.json | {GetHome()}/config.json |
| .security.yml | ~/.picoclaw/.security.yml | {GetHome()}/.security.yml |
| .picoclaw.pid | ~/.picoclaw/.picoclaw.pid | {GetHome()}/.picoclaw.pid |
| auth.json | ~/.picoclaw/auth.json | {GetHome()}/auth.json |
| workspace/ | ~/.picoclaw/workspace/ | {GetHome()}/workspace/ |
| skills/ | ~/.picoclaw/skills/ | {GetHome()}/skills/ |
| logs/ | ~/.picoclaw/logs/ | {GetHome()}/logs/ |
| reef.db | — | {GetHome()}/reef.db |
| wecom/reqid-store.json | ~/.picoclaw/wecom/ | {GetHome()}/wecom/ |

---

## 1. 全状态 TaskStore 持久化

### Requirement: Task 全生命周期存储
系统 SHALL 将所有任务状态持久化到 SQLite。

#### Scenario: 新任务提交后持久化
- GIVEN 新任务被 Submit
- THEN 任务 SHALL 写入 SQLite tasks 表

#### Scenario: 任务状态变更后持久化
- GIVEN 任务状态变更
- THEN SQLite SHALL 同步更新

#### Scenario: 任务结果持久化
- GIVEN 任务完成
- THEN Result SHALL 持久化到 task_results 表

#### Scenario: Server 重启后恢复
- GIVEN Server 重启
- THEN Queued 任务 SHALL 重新入队
- AND Running 任务 SHALL 标记为 Recovering，等 Client 确认
- AND Blocked 任务 SHALL 保持，等待依赖
- AND Aggregating 任务 SHALL 检查子任务状态

### Requirement: TaskStore 接口扩展（保留旧方法）
- `SaveTask(task) error` — 创建或更新
- `GetTask(id) (*Task, error)` — 查询
- `UpdateTaskStatus(id, status, fields) error` — 更新状态
- `ListTasks(filter) ([]*Task, error)` — 按条件查询
- `ListActiveTasks() ([]*Task, error)` — 查询非终态任务
- `SaveResult(taskID, result) error` — 保存结果
- `SaveAttempt(taskID, attempt) error` — 保存尝试记录
- `GetAttempts(taskID) ([]AttemptRecord, error)` — 查询尝试记录
- `SaveReplyTo(taskID, replyTo) error` — 保存来源上下文
- `GetReplyTo(taskID) (*ReplyToContext, error)` — 查询来源上下文
- `SaveRelation(parentID, childID, dependency string) error` — 保存父子关系
- `GetSubTaskIDs(parentID string) ([]string, error)` — 查询子任务ID
- `DeleteOldTasks(before time) error` — 清理过期任务

---

## 2. 非阻塞优先级调度器

### Requirement: 优先级队列
- Priority 1-10，默认 5
- 同优先级 FIFO
- 饥饿检测动态提升（每分钟 +1，最高提升到 8）

### Requirement: 非阻塞调度
- 队首不匹配时跳过，继续扫描后续
- 全部不匹配时停止，不报错

### Requirement: 可插拔 Client 匹配策略
- least-load（默认）：选 CurrentLoad 最小
- round-robin：轮询分配
- affinity：选历史成功率最高

### Requirement: 任务执行超时
- 默认 300000ms
- TimeoutScanner 每 10s 扫描 Running 任务
- 超时标记 Failed(timeout)

### Requirement: Client 断连任务回收
- 心跳超时后标记 Failed(client_disconnect) 并重入队

### Requirement: 幂等结果提交
- 终态任务收到重复 task_completed/task_failed SHALL 静默忽略

---

## 3. Server Gateway 集成

### Requirement: Server 可选启用 picoclaw Gateway
- 配置 gateway.enabled=true 时，Server 启动频道 + LLM + AgentLoop
- 配置 gateway.enabled=false 时，Server 仅启动 WebSocket + Admin + UI

### Requirement: 用户消息进入 Server AgentLoop
- 用户通过飞书/微信发消息 → 频道 → MessageBus → AgentLoop
- AgentLoop 分析消息，判断是否需要分发到 Client

### Requirement: 简单问题 Server 直接回复
- AgentLoop 判断不需要分发 → 直接生成回复 → MessageBus outbound → 频道回传

### Requirement: 复杂任务分发到 Client
- AgentLoop 调用 ReefSwarmTool → Scheduler.Submit → 分发到 Client

### Requirement: 任务来源追踪
- InboundMessage 已含来源上下文（Channel/ChatID/MessageID/SenderID）
- AgentLoop 调用 ReefSwarmTool 时自动携带
- Task.ReplyTo 持久化到 task_reply_to 表

### Requirement: 结果回传
- 单任务完成 → AgentLoop 生成回复 → MessageBus outbound → 频道回传
- 子任务全部完成 → AgentLoop 聚合 → MessageBus outbound → 频道回传
- 任务失败 → AgentLoop 生成失败通知 → 频道回传

---

## 4. DAG Engine + 结果聚合

### Requirement: 父子任务关系
- Planner 生成子任务列表 → DAG Engine 创建子任务
- 每个子任务记录 parent_task_id
- 父任务记录 sub_task_ids

### Requirement: 依赖等待
- 有依赖的子任务标记为 Blocked
- 依赖全部完成 → 自动变为 Queued

### Requirement: 并行执行
- 无依赖的子任务并行调度到不同 Client

### Requirement: 子任务失败传播
- 依赖的任务自动标记 Failed(dependency_failed)
- 父任务触发升级决策

### Requirement: 结果聚合
- 所有子任务完成 → 构造聚合 InboundMessage → AgentLoop + LLM 聚合
- LLM 聚合失败 → 回退为简单拼接
- 聚合结果通过 MessageBus outbound 回传

---

## 5. Web UI 统一

### Requirement: Reef 仪表盘融入 picoclaw Web UI
- 在 picoclaw 前端侧边栏新增 "Reef" 导航组
- 包含 Overview/Tasks/Clients 子页面
- 复用 picoclaw 前端的 UI 组件库（shadcn/ui）

### Requirement: Reef API 端点注册到 picoclaw Web 后端
- /api/reef/status → 服务器状态
- /api/reef/tasks → 任务列表（含分页/过滤）
- /api/reef/tasks/:id → 任务详情
- /api/reef/tasks/:id/subtasks → 子任务列表
- /api/reef/clients → Client 列表
- /api/reef/events → SSE 事件流

### Requirement: 保留独立 /ui 入口
- 现有 /ui 路径保留作为降级方案
- 不依赖 picoclaw Web 前端构建时仍可使用

---

## 6. 配置扩展

- `gateway.enabled` — 默认 false
- `gateway.model_name` — Server 端 LLM 模型名
- `gateway.channels` — 频道配置（复用 picoclaw 格式）
- `planner.max_subtasks` — 默认 10
- `planner.timeout_ms` — 默认 30000
- `scheduler.strategy` — 默认 least-load
- `scheduler.default_timeout_ms` — 默认 300000
- `scheduler.timeout_scan_interval` — 默认 10s
- `scheduler.starvation_threshold_ms` — 默认 300000
- `store.cleanup_age_ms` — 默认 86400000
