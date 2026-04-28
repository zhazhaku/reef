---
change: reef-scheduler-v2
schema: spec-driven
status: planning
created: 2026-04-28
updated: 2026-04-28
---

# Proposal: Reef Scheduler v2 — Server 中心化智能调度（统一设计方案）

## 1. 核心链路

```
用户(飞书/微信/API/WebUI) ──消息──▶ Server
                                        │
                                 ┌──────▼──────┐
                                 │  Server 端   │
                                 │  (picoclaw)  │
                                 │              │
                                 │ 频道: 复用现有 │  ← 接收用户消息 + 回传结果
                                 │ LLM: 复用现有 │  ← 任务分解 + 结果聚合
                                 │ AgentLoop    │  ← 智能决策
                                 │ Tools/Skills │  ← 包括 ReefSwarmTool
                                 │ Scheduler    │  ← 调度 Client
                                 │ Web UI       │  ← 仪表盘融入 picoclaw Web UI
                                 └──────┬──────┘
                                        │
                          ┌─────────────┼─────────────┐
                          ▼             ▼             ▼
                     Client-A      Client-B      Client-C
                     (纯执行器)    (纯执行器)    (纯执行器)
                          │             │             │
                          └─────────────┼─────────────┘
                                        │
                                 结果返回 Server
                                 LLM 聚合 → 回传用户
```

## 2. 现有能力保留与融合

### 2.1 保留的现有能力

| 现有组件 | 保留方式 | 说明 |
|----------|----------|------|
| **WebSocket Server** | ✅ 完整保留 | Client 连接、注册、心跳、任务分发/回收 |
| **Scheduler** | ✅ 增强保留 | 新增优先级/非阻塞/策略，保留 Submit/HandleCompleted/HandleFailed API |
| **Registry** | ✅ 完整保留 | Client 注册表、心跳、负载追踪 |
| **Queue (FIFO)** | ✅ 升级为 PriorityQueue | 接口不变，内部升级为优先级堆 |
| **PersistentQueue** | ✅ 升级融入 TaskStore | 持久化能力融入全状态 TaskStore |
| **Admin API** | ✅ 扩展保留 | 保留现有端点，新增 priority/decompose/reply_to |
| **Notify (Webhook/Slack/SMTP/飞书/企微)** | ✅ 完整保留 | 升级告警仍走 NotifyManager |
| **TaskStore (Memory/SQLite)** | ✅ 扩展保留 | 扩展接口和表结构，不破坏现有方法 |
| **SwarmChannel (Client端)** | ✅ 保留+扩展 | 保留独立工作能力，扩展 reply_to 上下文 |
| **Web UI (独立仪表盘)** | ✅ 融入 picoclaw Web UI | 从独立 SPA 迁移到 picoclaw 前端的 Reef 页面 |
| **Protocol (reef-v1)** | ✅ 扩展保留 | 新增消息类型，旧类型不变 |

### 2.2 新增能力

| 新组件 | 说明 |
|--------|------|
| **GatewayBridge** | 桥接 picoclaw Gateway 到 Reef Server（可选启用） |
| **ReefSwarmTool / ReefQueryTool** | AgentLoop 调用调度器的工具 |
| **DAG Engine** | 父子任务依赖编排 |
| **PriorityQueue** | 替代 FIFO，优先级堆 + 非阻塞扫描 |
| **MatchStrategy** | 可插拔 Client 匹配策略（least-load/round-robin/affinity） |
| **TimeoutScanner** | 任务超时检测 |
| **RecoveryManager** | Server 重启恢复 |
| **task_reply_to 表** | 任务来源频道上下文持久化 |
| **task_relations 表** | 父子任务关系持久化 |

## 3. 数据目录统一（重要变更）

### 3.1 当前问题

当前 picoclaw 的数据目录为 `~/.picoclaw/`（用户主目录下），与程序可执行文件分离：

```
~/.picoclaw/                    ← 数据目录（在用户主目录下）
├── config.json
├── .security.yml
├── .picoclaw.pid
├── auth.json
├── logs/
├── workspace/
└── skills/

/usr/local/bin/picoclaw         ← 可执行文件（在 PATH 中）
```

问题：
- 数据分散在用户主目录，不方便迁移和部署
- 多实例部署时冲突（共享 ~/.picoclaw）
- 不符合嵌入式设备（如 LicheeRV Nano）的单目录部署习惯
- Reef Server 的 SQLite 数据库路径需要单独指定

### 3.2 新方案：数据目录与程序目录一致

**数据目录改为 picoclaw 可执行文件所在目录**：

```
/path/to/picoclaw/              ← 程序目录 = 数据目录
├── picoclaw                    ← 可执行文件
├── config.json                 ← 配置文件
├── .security.yml               ← 安全配置
├── .picoclaw.pid               ← PID 文件
├── auth.json                   ← 认证存储
├── logs/                       ← 日志
├── workspace/                  ← 工作空间
├── skills/                     ← 技能
├── reef.db                     ← Reef SQLite 数据库（新增）
├── channels/                   ← 频道缓存
└── ...
```

### 3.3 目录解析优先级

```
1. $PICOCLAW_HOME 环境变量（显式覆盖，最高优先级）
2. 可执行文件所在目录（os.Executable() 的 Dir）
3. ~/.picoclaw（降级回退，保持兼容）
```

### 3.4 代码变更

```go
// pkg/config/envkeys.go — GetHome() 修改

func GetHome() string {
    // 1. 显式环境变量覆盖
    if picoclawHome := os.Getenv(EnvHome); picoclawHome != "" {
        return picoclawHome
    }

    // 2. 可执行文件所在目录（新默认值）
    if exe, err := os.Executable(); err == nil {
        exeDir := filepath.Dir(exe)
        // 验证目录可写（避免 /usr/bin 等系统目录）
        if isWritableDir(exeDir) {
            return exeDir
        }
    }

    // 3. 降级回退到用户主目录（保持兼容）
    homePath, _ := os.UserHomeDir()
    if homePath != "" {
        return filepath.Join(homePath, pkg.DefaultPicoClawHome)
    }

    return "."
}

// isWritableDir 检查目录是否可写
func isWritableDir(dir string) bool {
    testFile := filepath.Join(dir, ".picoclaw.write-test")
    if err := os.WriteFile(testFile, []byte{}, 0600); err != nil {
        return false
    }
    os.Remove(testFile)
    return true
}
```

### 3.5 Reef Server 数据目录

Reef Server 启动时，数据目录与 picoclaw 一致：

```go
// pkg/reef/server/server.go

func DefaultConfig() Config {
    homeDir := config.GetHome()  // 复用 picoclaw 的目录解析逻辑
    return Config{
        // ...
        StorePath: filepath.Join(homeDir, "reef.db"),  // 默认 SQLite 路径
        // ...
    }
}
```

### 3.6 迁移兼容

- 首次使用新版本时，如果 `~/.picoclaw/` 存在但程序目录下没有 `config.json`，自动提示迁移
- `PICOCLAW_HOME=~/.picoclaw` 可保持旧行为
- 现有 `~/.picoclaw/` 不删除，降级回退仍可用

## 4. 问题陈述

### 4.1 架构问题
- Server 是无状态调度器，没有 LLM/频道/AgentLoop
- 用户 → Client → Server → Client → 结果回不来
- 无法分解任务、无法聚合结果

### 4.2 调度缺陷
- 阻塞式 FIFO、无优先级、无超时检测、无亲和性

### 4.3 持久化缺陷
- 任务不持久化，Server 重启后丢失
- 结果仅存内存

### 4.4 可靠性缺陷
- 无任务超时、无幂等保障

### 4.5 UI 问题
- Reef 仪表盘是独立 SPA，与 picoclaw Web UI 割裂
- 两套前端体系，维护成本高

### 4.6 部署问题
- 数据目录在 `~/.picoclaw/`，与程序目录分离
- 嵌入式设备多实例部署困难

## 5. 目标

1. **Server 中心化**：Server 复用 picoclaw 完整能力（频道/LLM/AgentLoop/Tools/Skills）
2. **用户直接对话 Server**：用户通过飞书/微信/API/Web UI 直接与 Server 交互
3. **LLM 智能调度**：AgentLoop 分析 → 分解 → 分发 Client → 收集结果 → 聚合 → 回传
4. **非阻塞优先级调度**：高优先级优先，队首不阻塞
5. **全状态持久化**：SQLite 持久化，重启不丢任务
6. **超时与恢复**：超时检测 + Client 断连回收
7. **Web UI 统一**：Reef 仪表盘融入 picoclaw Web UI
8. **数据目录统一**：数据目录与程序目录一致，便于部署和迁移

## 6. 范围

### In Scope
- 数据目录改为程序目录（GetHome 逻辑修改）
- Server 端集成 picoclaw Gateway（可选）
- AgentLoop + ReefSwarmTool 任务分发
- DAG Engine 依赖编排
- 结果聚合（AgentLoop + LLM）
- 频道回传（复用 MessageBus outbound）
- 优先级队列 + 非阻塞调度 + 可插拔策略
- 全状态 TaskStore 持久化
- 超时检测 + Client 断连回收 + 幂等
- Reef 仪表盘融入 picoclaw Web UI

### Out of Scope
- 分布式 Server 集群
- Client 端频道能力改造（Client 保持现有能力）
- 多租户隔离

## 7. 方案概述

### Phase 0: 数据目录统一
- 修改 GetHome() 优先级：PICOCLAW_HOME > 可执行文件目录 > ~/.picoclaw
- 修改 Reef Server 默认 StorePath
- 添加目录可写性检查
- 迁移提示（可选）

### Phase 1: 全状态 TaskStore 持久化 + 恢复
- 扩展 TaskStore 接口和 SQLite 实现
- 新增 task_reply_to、task_relations 表
- RecoveryManager 启动恢复

### Phase 2: 非阻塞优先级调度器
- PriorityQueue 替代 FIFO
- 非阻塞 Scan
- MatchStrategy 可插拔策略
- TimeoutScanner

### Phase 3: Server Gateway 集成 + 任务分发
- GatewayBridge 桥接 picoclaw Gateway
- ReefSwarmTool / ReefQueryTool
- ReplyTo 上下文追踪
- 结果回传（复用 MessageBus）

### Phase 4: DAG Engine + 结果聚合
- 父子任务创建和依赖管理
- 依赖解除和失败传播
- 子任务结果收集 → AgentLoop 聚合

### Phase 5: Web UI 统一
- Reef 仪表盘页面融入 picoclaw 前端
- 新增 Reef API 端点到 picoclaw Web 后端
- 保留独立 /ui 入口作为降级方案

## 8. 向后兼容

- **PICOCLAW_HOME 环境变量**：显式设置时优先级最高，行为不变
- **~/.picoclaw 降级**：可执行文件目录不可写时回退到 ~/.picoclaw
- 现有 Client 不受影响：WebSocket 协议扩展但不破坏
- Server 新增能力可选：不配置 Gateway 时行为与当前一致
- SwarmChannel 保留独立工作能力
- Admin API 保留现有端点
- 旧 Web UI 保留为降级入口

## 9. 风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| 可执行文件目录不可写 | 数据无法存储 | 自动降级到 ~/.picoclaw |
| Server 进程变重 | 内存/CPU 增加 | Gateway 可选启用 |
| LLM 规划延迟 | 1-5s | 简单任务跳过分解 |
| SQLite 写入瓶颈 | 高并发吞吐下降 | WAL + 批量写入 |
| 子任务爆炸 | 调度开销 | 最大子任务数限制 |
| Web UI 前端构建 | 需要安装 pnpm | 保留独立 UI 降级 |
