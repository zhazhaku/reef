# Hermes 能力架构研究报告

**日期**: 2026-04-28
**状态**: 完成
**变更**: reef-hermes-architecture

---

## 1. 问题分析

### 1.1 核心问题：意图发散

Server 端 AgentLoop 收到用户消息后，LLM 会倾向于直接使用 web_search/exec/read_file 等工具处理任务，即使有合适的 Client 在线也不会分发到 Client → **团队作战名存实亡**。

### 1.2 根本原因

1. **SystemPrompt 是通用的**：没有"你是团队协调者"的角色定义
2. **Tool 集合是全量的**：AgentLoop 注册了所有工具，LLM 可以直接执行
3. **没有能力边界约束**：LLM 自主决策是否分发，无约束机制

### 1.3 模式切换缺失

- `picoclaw` 启动 → 单 Client 模式（现有行为，不变）
- `picoclaw server` 启动 → Server 模式（虚拟团队作战）
- 但当前两种模式下 AgentLoop 行为完全相同

---

## 2. 方案对比

### 2.1 Prompt 约束（软约束）

| 优点 | 缺点 |
|------|------|
| 实现简单 | LLM 遵从率 ~90-95% |
| 不改代码结构 | 可能被绕过 |
| 灵活可调 | 无法硬性保证 |

### 2.2 Tool 注册过滤（硬约束）

| 优点 | 缺点 |
|------|------|
| 100% 遵从率 | 降级时需动态注册 |
| 结构性隔离 | 需新增 ToolRegistry.Remove |
| LLM 看不到禁止工具 | 降级/恢复复杂度 |

### 2.3 运行时 Guard（动态约束）

| 优点 | 缺点 |
|------|------|
| 动态放行/恢复 | 性能开销（每次 Tool 调用检查） |
| 降级无缝切换 | 实现复杂度 |
| 与 Hook 系统融合 | 需要维护状态 |

### 2.4 推荐方案：三层保障（A+B+C）

```
Layer 1: PromptContributor → 注入协调者身份 (软约束, ~95%)
Layer 2: 条件注册 → 只注册允许的工具 (硬约束, 100%)
Layer 3: HermesGuard → 降级时动态放行 (动态约束)
```

---

## 3. 推荐设计

### 3.1 HermesMode 定义

```go
type HermesMode string

const (
    HermesFull        HermesMode = "full"         // 默认，单 Client 模式
    HermesCoordinator HermesMode = "coordinator"  // Server 模式，协调者
    HermesExecutor    HermesMode = "executor"     // Client 模式，执行者
)
```

### 3.2 PromptContributor 注入

```go
// pkg/agent/hermes_prompt.go
type hermesRoleContributor struct {
    mode HermesMode
    scheduler ReefScheduler  // 用于获取在线 Client 信息
}

func (c *hermesRoleContributor) PromptSource() PromptSourceDescriptor {
    return PromptSourceDescriptor{
        ID:          "hermes:role",
        Owner:       "hermes",
        Description: "Hermes role definition for multi-agent coordination",
        Allowed:     []PromptPlacement{{Layer: PromptLayerKernel, Slot: PromptSlotIdentity}},
        StableByDefault: true,
    }
}

func (c *hermesRoleContributor) ContributePrompt(ctx context.Context, req PromptBuildRequest) ([]PromptPart, error) {
    if c.mode == HermesFull {
        return nil, nil
    }
    content := buildHermesRolePrompt(c.mode, c.scheduler)
    return []PromptPart{{
        ID:      "kernel.hermes_role",
        Layer:   PromptLayerKernel,
        Slot:    PromptSlotIdentity,
        Source:  PromptSource{ID: PromptSourceHermesRole, Name: string(c.mode)},
        Title:   "Hermes role definition",
        Content: content,
        Stable:  true,
        Cache:   PromptCacheEphemeral,
    }}, nil
}
```

### 3.3 条件注册

```go
// pkg/agent/agent_init.go — registerSharedTools 修改
func registerSharedTools(al *AgentLoop, cfg *config.Config, ...) {
    hermesMode := al.hermesMode  // 新增字段

    for _, agentID := range registry.ListAgentIDs() {
        agent, _ := registry.GetAgent(agentID)

        // === Hermes Coordinator: 只注册协调者工具 ===
        if hermesMode == HermesCoordinator {
            registerCoordinatorTools(agent, cfg, msgBus, al)
            continue
        }

        // === Hermes Executor: 全部工具，但禁用 reef_submit ===
        // （reef_submit 本来就不在默认注册中，无需额外处理）

        // === Hermes Full: 全部工具 ===
        registerAllTools(agent, cfg, msgBus, al, registry, provider, ...)
    }
}

func registerCoordinatorTools(agent *AgentInstance, cfg *config.Config, ...) {
    // 只注册协调者允许的工具
    agent.Tools.Register(reefSubmitTool)
    agent.Tools.Register(reefQueryTool)
    agent.Tools.Register(reefStatusTool)
    agent.Tools.Register(messageTool)
    agent.Tools.Register(reactionTool)
    agent.Tools.Register(cronTool)
}
```

### 3.4 ToolRegistry.Remove 新增

```go
// pkg/tools/registry.go 新增
func (r *ToolRegistry) Remove(name string) bool {
    r.mu.Lock()
    defer r.mu.Unlock()
    if _, exists := r.tools[name]; !exists {
        return false
    }
    delete(r.tools, name)
    r.version.Add(1)
    return true
}
```

### 3.5 HermesGuard

```go
// pkg/agent/hermes_guard.go
type HermesGuard struct {
    mode     HermesMode
    allowed  map[string]struct{}
    fallback atomic.Bool
}

func NewHermesGuard(mode HermesMode) *HermesGuard {
    g := &HermesGuard{mode: mode}
    if mode == HermesCoordinator {
        g.allowed = map[string]struct{}{
            "reef_submit_task": {},
            "reef_query_task":  {},
            "reef_status":      {},
            "message":          {},
            "reaction":         {},
            "cron":             {},
        }
    }
    return g
}

func (g *HermesGuard) Allow(toolName string) bool {
    if g.allowed == nil || g.fallback.Load() {
        return true
    }
    _, ok := g.allowed[toolName]
    return ok
}

func (g *HermesGuard) SetFallback(enabled bool) {
    g.fallback.Store(enabled)
}
```

### 3.6 降级策略（混合策略 D）

```
用户消息 → Coordinator AgentLoop
    │
    ├── 简单问候/元问题 → 直接回复 ✅
    │
    └── 复杂任务 → 检查在线 Client
           │
           ├── 有 Client → reef_submit_task ✅
           │
           └── 无 Client → 检查 hermes.fallback 配置
                  │
                  ├── fallback=true → 降级为 Full
                  │   ├── 1. HermesGuard.SetFallback(true)
                  │   ├── 2. 动态注册缺失工具 (Register)
                  │   ├── 3. 执行任务
                  │   ├── 4. 回复用户（附带降级提示）
                  │   └── 5. Client 上线后恢复约束
                  │       ├── HermesGuard.SetFallback(false)
                  │       └── 移除降级工具 (Remove)
                  │
                  └── fallback=false → 返回等待提示
                      └── "当前没有可用的团队成员，任务已入队等待"
```

### 3.7 CLI 参数 → 行为模式

```
picoclaw                    → HermesFull (默认)
picoclaw gateway            → HermesFull (默认)
picoclaw server             → HermesCoordinator + Reef Server + Gateway
picoclaw reef-server        → Reef Server only (无 Hermes)

新增命令: picoclaw server
  ├── 启动 Reef Server (WebSocket + Admin + UI)
  ├── 启动 Gateway (频道 + LLM + AgentLoop)
  ├── 设置 HermesMode = Coordinator
  └── AgentLoop 初始化时应用 Hermes 约束
```

### 3.8 配置扩展

```json
{
  "hermes": {
    "mode": "coordinator",
    "fallback": true,
    "fallback_timeout_ms": 30000,
    "coordinator_tools": [
      "reef_submit_task", "reef_query_task", "reef_status",
      "message", "reaction", "cron"
    ]
  }
}
```

---

## 4. 代码变更点

| 文件 | 操作 | 说明 |
|------|------|------|
| `pkg/agent/hermes.go` | **新增** | HermesMode 定义 + HermesGuard |
| `pkg/agent/hermes_prompt.go` | **新增** | hermesRoleContributor (PromptContributor) |
| `pkg/agent/agent.go` | 修改 | 新增 hermesMode 字段 |
| `pkg/agent/agent_init.go` | 修改 | registerSharedTools 按 HermesMode 条件注册 |
| `pkg/tools/registry.go` | 修改 | 新增 Remove(name string) 方法 |
| `cmd/picoclaw/internal/server/command.go` | **新增** | `picoclaw server` 命令 |
| `cmd/picoclaw/main.go` | 修改 | 注册 server 子命令 |
| `pkg/config/config.go` | 修改 | 新增 HermesConfig 结构体 |
| `pkg/reef/server/gateway.go` | 修改 | GatewayBridge 启动时设置 HermesMode |
| `pkg/channels/swarm/swarm.go` | 修改 | Client 端设置 HermesExecutor |

---

## 5. 与 reef-scheduler-v2 的融合点

| 融合点 | 说明 |
|--------|------|
| **GatewayBridge** | 启动时传入 HermesMode，影响 AgentLoop 初始化 |
| **ReefSwarmTool** | Coordinator 的核心工具，相当于 Swarm 的 handoff 函数 |
| **ReefQueryTool** | Coordinator 查询任务状态 |
| **SystemPrompt** | PromptContributor 注入协调者身份 |
| **Tool 注册** | registerSharedTools 按 HermesMode 条件注册 |
| **降级策略** | 与 Scheduler 的 Client 在线状态联动 |
| **Web UI** | Reef Overview 页面显示当前 Hermes 模式 |
| **CLI** | `picoclaw server` = Gateway + Reef + HermesCoordinator |

---

## 6. 风险评估

| 风险 | 影响 | 缓解 | 残余风险 |
|------|------|------|---------|
| LLM 绕过 Prompt 约束 | Server 直接执行 | Tool 注册过滤兜底 | 低（双重保障） |
| 过度约束无法降级 | 无 Client 时无法处理 | HermesGuard 动态放行 | 低 |
| ToolRegistry.Remove 副作用 | 降级恢复时工具丢失 | Remove 后可重新 Register | 低 |
| SubTurn 与 Hermes 混淆 | 两种并发机制冲突 | Coordinator 禁用 spawn | 低 |
| 降级/恢复闪烁 | Client 频繁上下线 | 防抖机制（30s 稳定期） | 中 |
| Coordinator Prompt 过长 | Token 消耗增加 | 压缩 Prompt，动态注入 Client 列表 | 低 |

---

## 7. 建议实施顺序

```
Phase 0: 基础设施
  ├── 新增 HermesMode 定义
  ├── 新增 ToolRegistry.Remove
  └── 新增 HermesConfig

Phase 1: Prompt 约束
  ├── 新增 hermesRoleContributor
  ├── Coordinator/Executor Prompt 模板
  └── 注册到 PromptRegistry

Phase 2: Tool 注册约束
  ├── registerSharedTools 条件注册
  ├── registerCoordinatorTools 函数
  └── Coordinator Tool 集定义

Phase 3: 运行时 Guard
  ├── HermesGuard 实现
  ├── BeforeTool Hook 集成
  └── 降级/恢复逻辑

Phase 4: CLI 集成
  ├── picoclaw server 命令
  ├── HermesMode 传递链路
  └── 配置文件 hermes 段

Phase 5: 融合测试
  ├── Coordinator 模式 E2E 测试
  ├── 降级/恢复 E2E 测试
  └── 与 reef-scheduler-v2 集成测试
```
