# Reef v1 分布式多智能体编排系统 — 项目提案

## 1. Intent（意图）

### 1.1 问题陈述

PicoClaw 是一个单节点、超轻量的 Go AI Agent 框架（~10MB RAM），内置 EventBus、Skill Registry、Multi-Provider LLM Routing 和 Channel 抽象。然而，在面对以下场景时，单节点架构存在根本限制：

- **计算密集型流水线**：代码编写、审查、测试需要不同能力模型和运行环境，单节点无法并行化
- **跨域协作**：数据分析与可视化需要不同工具链和角色视角
- **边缘-中心协同**：Sipeed 等 ARM64 边缘设备算力有限，需要与中心节点协同调度

### 1.2 解决目标

Reef 将 PicoClaw 从单节点 Agent 扩展为 **Hub-and-Spoke 分布式多智能体集群**：

- 中央 **Reef Server** 负责任务调度、状态管理和生命周期控制
- 多个 **Reef Client** 节点作为 PicoClaw 实例，承载特定角色（coder、analyst、tester 等）
- 通过 **WebSocket** 实现低延迟、全双工通信
- 复用 PicoClaw 核心组件（EventBus、AgentLoop、Skills、Channels），最小化架构侵入

### 1.3 核心价值

| 价值维度 | 具体收益 |
|---------|---------|
| 角色专业化 | 每个 Client 节点预加载角色相关的 Skill 子集，避免全量加载 |
| 水平扩展 | 新增 Client 节点即可扩展集群吞吐，Server 自动发现并调度 |
| 故障隔离 | 单 Client 故障不阻塞整体系统，Server 可重分配任务 |
| 离线容忍 | Client 断线时任务暂停而非失败，恢复后自动续跑 |
| 统一管控 | 单二进制双模式部署，降低运维复杂度 |

---

## 2. Scope（范围边界）

### 2.1 In Scope（v1 包含）

- **Swarm Protocol**：基于 WebSocket 的双向消息协议（register、heartbeat、task、progress、control）
- **Server 核心**：Client 注册表、心跳驱逐、任务调度器、内存任务队列、HTTP Admin 端点
- **Client 核心**：WebSocket 连接器、任务注入 AgentLoop、进度报告、指数退避重连
- **生命周期控制**：任务取消（context.Cancel）、暂停/恢复
- **失败处理**：Client 本地重试、耗尽后上报 Server，Server 决策（重分配/终止/人工介入）
- **角色化技能**：静态角色到 Skill 子集的映射、系统提示词覆盖
- **单二进制双模式**：`--mode=server` 或 `--mode=client` 运行时切换

### 2.2 Out of Scope（v1 排除）

- 动态角色发现 / 自动技能学习（v2）
- Client-to-Client 直连通信（v2）
- 多 Server 联邦 / 分片（v2）
- 磁盘持久化任务队列（v2，SQLite/WAL）
- Web UI 仪表盘（v2，v1 仅 JSON Admin 端点）
- 每 Client API Key / JWT 认证（v1 仅用共享 `x-reef-token`）
- gRPC 或 HTTP 轮询传输（WebSocket Only）

---

## 3. Approach（技术路径）

### 3.1 总体策略：Fork & Extend（路径 B）

采用 **Fork PicoClaw 源码 + 扩展接口** 的策略，而非从零构建或外部包装：

```
PicoClaw 源码
├── pkg/bus/          ← 复用 EventBus、MessageBus
├── pkg/skills/       ← 复用 Skill Registry，扩展角色过滤加载
├── pkg/channels/     ← 新增 pkg/channels/swarm/ 实现 Channel 接口
├── pkg/agent/        ← 扩展 AgentLoop 注入 TaskContext
├── pkg/providers/    ← 复用 LLM Provider 路由
└── cmd/reef/         ← 新增主入口，支持 --mode=server|client
```

### 3.2 关键设计选择

| 决策 | 选择 | 理由 |
|-----|------|------|
| 架构拓扑 | Hub-and-Spoke | Server 中心化调度，避免分布式一致性复杂度 |
| 传输协议 | WebSocket (gorilla/websocket) | PicoClaw 已有依赖，支持全双工、低延迟 |
| 部署模式 | 单二进制，双模式 | 同一 artifact 可跑 Server 或 Client，简化 CI/CD |
| 角色模型 | 静态角色 | 启动时绑定，降低动态发现复杂度；足够覆盖 v1 场景 |
| 状态所有权 | Server 拥有任务状态机 | Client 仅维护 in-flight 任务；重试/重分配逻辑集中 |
| 重连策略 | 指数退避 + 任务暂停 | 断线不失败，恢复后续跑，提升边缘场景鲁棒性 |

### 3.3 技术栈

- **语言**：Go 1.25+
- **网络**：gorilla/websocket（已有依赖）
- **并发**：原生 goroutine + sync.RWMutex + context.Context
- **配置**：YAML/JSON（与 PicoClaw config 兼容）
- **日志**：结构化 JSON/文本日志（与 PicoClaw 一致）
- **目标平台**：ARM64（Sipeed 板卡）+ AMD64

---

## 4. Constraints（架构约束）

### 4.1 硬约束

- **WebSocket Only**：所有 Server-Client 通信必须通过 WebSocket，禁止 gRPC、HTTP 轮询
- **Go 1.25+**：利用最新标准库优化（如 `slices`、`maps` 包）
- **ARM64 兼容**：二进制体积敏感，禁止 CGO 依赖（纯 Go 实现）
- **复用 PicoClaw 组件**：EventBus、Skill Registry、AgentLoop、Provider Router 必须直接复用
- **单二进制**：`go build` 产出单一可执行文件，运行时通过 flag 区分模式

### 4.2 软约束

- **内存占用**：Client 节点目标 < 20MB RSS（PicoClaw 基础 ~10MB + Reef 开销）
- **延迟目标**：任务调度延迟 < 100ms（局域网内）
- **并发规模**：v1 目标支持 32 Clients、1000 并发任务（内存队列上限）

---

## 5. Risk Assessment（风险评估）

| 风险 ID | 风险描述 | 概率 | 影响 | 缓解方案 |
|---------|---------|------|------|---------|
| R01 | WebSocket 长连接在边缘网络（WiFi/4G）下频繁断开 | 高 | 中 | 指数退避重连 + 任务暂停续跑；心跳超时窗口可配置 |
| R02 | Server 单点故障导致全集群不可用 | 中 | 高 | v1 接受单点；通过外部监控快速重启；v2 规划联邦 |
| R03 | PicoClaw AgentLoop 扩展 TaskContext 引入回归 | 中 | 高 | 抽象 `processOptions` hook，不修改核心循环逻辑；完整单元测试覆盖 |
| R04 | 任务状态机并发竞争导致状态不一致 | 中 | 高 | Server 端每个 Task 独立 goroutine + channel 驱动状态机；Client 端 context 传播 |
| R05 | 内存队列溢出导致 OOM | 低 | 高 | 设置队列上限（默认 1000），超限返回 429；Admin 端点暴露队列深度 |
| R06 | 角色-Skill 映射配置错误导致任务无法匹配 | 低 | 中 | 启动时校验角色配置，Server 注册时校验 Skill 清单非空；Admin 端点暴露 Client 能力 |

---

## 6. Success Criteria（成功标准）

### 6.1 功能标准

1. Server 可接受 ≥4 个 Client 并发连接，维护准确的能力注册表
2. 任务可按角色+技能匹配调度到正确 Client
3. Client 断线 30 秒内重连，in-flight 任务状态为 Paused 而非 Failed
4. Server 可在任务运行时发送 Cancel，Client 在 5 秒内终止执行
5. Client 本地重试 3 次失败后，Server 可重分配到另一 Client

### 6.2 性能标准

1. 端到端任务调度+执行延迟 < 500ms（本地模拟，零 LLM 调用）
2. Server 内存占用 < 128MB（32 Clients、100 并发任务）
3. Client 内存占用 < 20MB（单角色、5 Skills）

### 6.3 质量标准

1. 单元测试覆盖率 ≥ 80%（pkg/reef/ 包）
2. E2E 测试覆盖完整链路：Server → 2 Clients（不同角色）→ 任务提交 → 执行 → 结果返回
3. 所有公开接口文档化（Go doc）
4. README 包含部署指南、角色添加指南、故障排查

---

*提案版本：v1.0*
*日期：2026-04-27*
*作者：Reef Research & Design Agent*
