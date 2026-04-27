# Reef v1 分布式多智能体编排系统 — 项目提案

## Intent（意图）

PicoClaw 是一个 ultra-lightweight（~10MB RAM）的单节点 Go AI Agent 框架，拥有成熟的内部架构：EventBus、技能注册表、多 Provider LLM 路由、Channel 抽象。然而，当前 PicoClaw 仅能在单一节点上运行，无法横向扩展为多智能体协作的分布式系统。

**Reef 的目标**是将 PicoClaw 从单节点 Agent 扩展为**基于 Hub-and-Spoke 拓扑的分布式多智能体集群**。每个 Client 节点运行一个 PicoClaw 实例，携带静态角色（如 coder、analyst、tester），加载对应的技能子集，通过 WebSocket 连接到中央 Server。Server 维护实时能力注册表，基于角色和技能匹配将任务调度到最佳 Client，并管理完整的任务生命周期。

 Reef 解决的核心问题：
- **算力分散**：边缘设备（Sipeed 开发板、ARM64 服务器）各自运行 PicoClaw，但无法协同完成复杂流水线任务。
- **角色隔离**：不同 Agent 需要不同的技能集和系统提示词，当前 PicoClaw 的单一 Agent 配置难以满足。
- **任务编排**：缺乏中心化的任务调度、状态跟踪、失败重试和人工干预机制。
- **离线韧性**：边缘网络不稳定，Client 断线后任务不应直接失败，而应暂停并在重连后恢复。

## Scope（v1 范围边界）

### In Scope（v1 包含）
- WebSocket 双工通信协议（register / heartbeat / task / progress / cancel / pause / resume / failed / completed）
- Server 端：Client 注册表、心跳超时剔除、基于角色+技能的调度器、内存任务队列、HTTP Admin 端点
- Client 端：WebSocket 连接器、任务注入 AgentLoop、进度报告、指数退避重连
- 任务状态机：Created → Assigned → Running → Completed / Failed / Paused / Cancelled
- 本地重试机制：Client 在执行失败时本地重试，耗尽后上报 Server 进行 escalate 决策
- 角色化技能加载：Client 启动时根据角色配置加载技能子集和系统提示词覆盖
- 单二进制双模式：`--mode=server` 或 `--mode=client`
- 简单认证：共享 `x-reef-token` Header

### Out of Scope（v1 不包含）
- 持久化任务队列（磁盘/SQLite），v1 仅内存队列
- Web UI Dashboard，Admin 端点仅返回 JSON
- 多 Server 联邦 / Gossip 协议
- Client-to-Client 直接通信
- 动态角色发现 / 热加载技能
- 每 Client API Key / JWT 认证
- gRPC 或 HTTP 轮询传输

## Approach（技术路径）

### 路径选择：Fork PicoClaw（Path B）
Reef 不是 PicoClaw 的插件，而是其分布式扩展。需要修改：
1. 新增 `cmd/reef/` 主入口，支持 `--mode=server|client`
2. 扩展 `pkg/agent/loop.go` 的 `processOptions`，注入 `TaskContext`（taskID + cancelFunc）
3. 新增 `pkg/channels/swarm/` 实现 PicoClaw `Channel` 接口，将 Server 视为一个特殊 Channel
4. 新增 `pkg/reef/` 协议层、任务状态机、Server 和 Client 组件

### 架构概览
```
┌─────────────────────────────────────────────────────────────┐
│                        Reef Server                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │   Registry  │  │  Scheduler  │  │   Task State Machine│  │
│  │  (Client元数据)│  │ (role+skill匹配)│  │  (生命周期管理)      │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │  WS Acceptor│  │ Task Queue  │  │  HTTP Admin /status │  │
│  │  (gorilla/ws)│  │ (内存队列)    │  │  /tasks             │  │
│  └─────────────┘  └─────────────┘  └─────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                              ▲
                              │ WebSocket (JSON messages)
        ┌─────────────────────┼─────────────────────┐
        ▼                     ▼                     ▼
┌───────────────┐    ┌───────────────┐    ┌───────────────┐
│ Reef Client   │    │ Reef Client   │    │ Reef Client   │
│ (coder role)  │    │ (analyst role)│    │ (tester role) │
│  - skills:    │    │  - skills:    │    │  - skills:    │
│    github,    │    │    web_fetch, │    │    exec,      │
│    write_file │    │    summarize  │    │    spawn      │
│  - system     │    │  - system     │    │  - system     │
│    prompt     │    │    prompt     │    │    prompt     │
└───────────────┘    └───────────────┘    └───────────────┘
```

## Constraints（架构约束）

| 约束 | 说明 |
|------|------|
| WebSocket Only | 唯一传输层，使用 `gorilla/websocket`（已是 PicoClaw 依赖） |
| Go 1.25+ | 与 PicoClaw 保持一致，利用新特性 |
| ARM64 兼容 | 目标平台包括 Sipeed LicheeRV Nano / MaixCAM 等边缘设备 |
| 复用 PicoClaw 组件 | 必须复用 `pkg/bus`、`pkg/skills`、`pkg/channels`、`pkg/agent`、`pkg/providers`，不得重复造轮子 |
| 单二进制 | 同一个 `reef` 二进制通过 `--mode` 切换 Server/Client |
| Client 离线不失败 | WebSocket 断连时，Client 暂停（pause）在飞任务，重连后恢复 |
| Server 拥有状态机 | 所有 retry / escalation / reassign 逻辑集中在 Server，Client 除在飞任务外无状态 |

## Risk Assessment（风险评估）

| 风险 | 影响 | 概率 | 缓解方案 |
|------|------|------|----------|
| AgentLoop 注入 TaskContext 导致循环复杂度增加 | 高 | 中 | 使用 `processOptions` 扩展点，不改动核心循环逻辑；Hook 机制拦截 `TurnStart` 事件注入上下文 |
| WebSocket 重连期间任务状态不一致 | 高 | 中 | Server 维护 canonical 状态机，Client 断连后标记为 `Paused`，重连后同步状态 |
| 边缘设备资源限制（内存 < 256MB） | 中 | 高 | 复用 PicoClaw 轻量架构，Server 不做消息代理（仅信令），Client 仅增加一个 WebSocket goroutine |
| 调度器瓶颈（单 Server 大量 Client） | 中 | 低 | v1 设计为中小规模（<100 Client），调度器使用 O(n) 简单匹配；未来版本考虑分片 |
| 角色-技能映射配置管理复杂 | 低 | 中 | 使用 YAML manifest 文件，提供验证工具和默认模板 |

## Success Criteria（成功标准）

1. **协议正确性**：所有消息类型的 JSON marshal/unmarshal 单元测试 100% 通过，round-trip 无损。
2. **Server 功能**：能够同时接受 10+ Client 连接，维护实时注册表，心跳剔除超时 Client 的准确率 100%。
3. **调度功能**：向 Server 提交需要特定角色的任务，95% 以上在 1 秒内被正确匹配并 dispatch 到对应 Client。
4. **任务生命周期**：cancel / pause / resume / retry / escalate 所有状态路径的单元测试覆盖。
5. **离线韧性**：模拟 Client 断连 30 秒后重连，在飞任务状态为 `Paused` 而非 `Failed`，重连后正确恢复。
6. **E2E 集成**：启动 1 Server + 2 不同角色 Client → 提交 2 个不同角色任务 → 验证正确 Client 执行 → 验证结果返回，全流程 < 30 秒。
7. **资源占用**：Client 模式内存增量 < 5MB（相对于 standalone PicoClaw）。
