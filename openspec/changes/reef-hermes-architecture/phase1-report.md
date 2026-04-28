# Phase 1 Research Report: AgentLoop 代码走读与现状分析

## 1.1 AgentLoop 消息处理全链路

### 完整流程图

```
用户消息 (飞书/微信/CLI/API)
    │
    ▼
MessageBus.PublishInbound()
    │
    ▼
AgentLoop.Run()  ← 主循环，监听 bus.InboundChan()
    │
    ├── resolveSteeringTarget(msg)  ← 解析 session + agent
    │   └── 如果 session 已有 active turn → enqueueSteeringMessage()
    │
    ├── 占位符占位 (LoadOrStore session)
    │
    └── go func() { runTurnWithSteering(ctx, msg) }()  ← worker goroutine
            │
            ▼
        processMessage(ctx, msg)
            │
            ├── normalizeInboundMessage()
            ├── transcribeAudioInMessage()
            ├── resolveMessageRoute(msg)  ← 路由到具体 Agent
            │   └── Registry.ResolveRoute() → AgentInstance
            ├── allocateRouteSession()  ← 分配 session
            ├── handleCommand()  ← 检查是否为 /command
            └── runAgentLoop(ctx, agent, opts)
                    │
                    ▼
                Pipeline.SetupTurn()  ← 构造 messages + history
                    │   ├── ContextManager.Assemble() → history
                    │   ├── ContextBuilder.BuildMessagesFromPrompt()
                    │   │   ├── BuildSystemPromptWithCache()  ← ★ SystemPrompt 构建
                    │   │   │   └── BuildSystemPromptParts()
                    │   │   │       ├── identity (kernel)
                    │   │   │       ├── workspace (instruction)
                    │   │   │       ├── skill_catalog (capability)
                    │   │   │       ├── memory (context)
                    │   │   │       └── output_policy (context)
                    │   │   ├── PromptRegistry.Collect()  ← ★ PromptContributor 扩展点
                    │   │   ├── dynamicContext (context)
                    │   │   └── summary (context)
                    │   └── selectCandidates()  ← 模型选择
                    │
                    ▼
                Pipeline.CallLLM()  ← LLM 调用
                    │   ├── BeforeLLM Hook  ← ★ Hook 拦截点
                    │   ├── provider.Chat()  ← 实际 LLM 调用
                    │   │   └── messages + toolDefs → LLM
                    │   └── AfterLLM Hook
                    │
                    ▼
                Pipeline.ExecuteTools()  ← 工具执行循环
                    │   ├── BeforeTool Hook  ← ★ Hook 拦截点
                    │   ├── ToolRegistry.Execute()  ← 实际工具执行
                    │   ├── AfterTool Hook
                    │   └── Steering 注入检查
                    │
                    ▼
                Pipeline.Finalize()  ← 最终响应
                    └── Bus.PublishOutbound()  ← 回传用户
```

### ★ Hermes 注入点标注

| 注入点 | 位置 | 作用 |
|--------|------|------|
| **P1: BuildSystemPromptParts()** | `context.go:160` | 注入协调者身份 Prompt |
| **P2: PromptRegistry.Collect()** | `context.go:770` | 通过 PromptContributor 注入 |
| **P3: registerSharedTools()** | `agent_init.go:76` | 条件注册 Tool 集 |
| **P4: BeforeLLM Hook** | `pipeline_llm.go:83` | 运行时 Guard 拦截 |
| **P5: BeforeTool Hook** | `pipeline_execute.go:43` | 运行时 Guard 拦截 Tool 调用 |
| **P6: ToolRegistry** | `tools/registry.go` | 物理上不注册不该有的 Tool |

---

## 1.2 PromptStack 架构分析

### 完整结构图

```
┌─────────────────────────────────────────────────────────────────────────┐
│ PromptStack (5 层, 优先级从高到低)                                       │
│                                                                         │
│ Layer: kernel (最高优先级)                                                │
│   ├── Slot: identity     ← "You are PicoClaw, a helpful AI assistant"  │
│   │   Source: runtime.kernel                                              │
│   │                                                                      │
│   └── Slot: hierarchy    ← Prompt 层级规则                                │
│       Source: runtime.hierarchy                                          │
│                                                                         │
│ Layer: instruction                                                       │
│   └── Slot: workspace    ← AGENT.md / SOUL.md / USER.md                │
│       Source: workspace.definition                                       │
│                                                                         │
│ Layer: capability                                                        │
│   ├── Slot: tooling      ← Tool 发现指令                                 │
│   │   Source: tool_registry:discovery                                    │
│   ├── Slot: skill_catalog ← 已安装 Skills 列表                           │
│   │   Source: skill:index                                                │
│   └── Slot: active_skill  ← 当前激活 Skill 指令                          │
│       Source: skill:active                                               │
│                                                                         │
│ Layer: context                                                           │
│   ├── Slot: memory       ← 工作空间记忆                                  │
│   │   Source: memory:workspace                                           │
│   ├── Slot: runtime      ← 运行时上下文 (时间/平台/会话)                  │
│   │   Source: runtime.context                                            │
│   ├── Slot: summary      ← 对话摘要                                     │
│   │   Source: context.summary                                            │
│   └── Slot: output       ← 输出策略 (multi-message split)               │
│       Source: runtime.output                                             │
│                                                                         │
│ Layer: turn (最低优先级, 每轮重建)                                        │
│   ├── Slot: message      ← 当前用户消息                                  │
│   ├── Slot: steering     ← Steering 注入消息                             │
│   ├── Slot: subturn      ← SubTurn 结果                                  │
│   └── Slot: interrupt    ← 中断提示                                      │
└─────────────────────────────────────────────────────────────────────────┘
```

### PromptContributor 扩展机制

```go
// 注册自定义 Prompt 贡献者
type PromptContributor interface {
    PromptSource() PromptSourceDescriptor
    ContributePrompt(ctx context.Context, req PromptBuildRequest) ([]PromptPart, error)
}

// 已有贡献者:
// - toolDiscoveryPromptContributor → tool_registry:discovery
```

### Hermes 注入方案对比

| 方案 | 实现方式 | 侵入性 | 灵活性 | 推荐度 |
|------|---------|--------|--------|--------|
| **A: 覆盖 identity** | 替换 PromptSlotIdentity 内容 | 低 | 低（丢失原身份） | ⭐⭐ |
| **B: 新增 Slot** | 新增 PromptSlotHermesRole (kernel 层) | 中 | 高（不破坏原身份） | ⭐⭐⭐⭐⭐ |
| **C: PromptContributor** | 注册 HermesRoleContributor | 低 | 高（完全解耦） | ⭐⭐⭐⭐ |
| **D: overlay 注入** | 通过 processOptions.Overlays | 最低 | 中（每轮需传入） | ⭐⭐⭐ |

**推荐方案 C：PromptContributor**

理由：
1. **零侵入**：不需要修改 BuildSystemPromptParts()，只需注册 Contributor
2. **完全解耦**：Hermes 逻辑在独立文件，不影响现有代码
3. **动态切换**：可以运行时注册/注销 Contributor
4. **符合现有架构**：toolDiscoveryPromptContributor 已验证此模式

```go
// pkg/agent/hermes_prompt.go
type hermesRoleContributor struct {
    mode HermesToolPolicy
}

func (c *hermesRoleContributor) PromptSource() PromptSourceDescriptor {
    return PromptSourceDescriptor{
        ID:          "hermes:role",
        Owner:       "hermes",
        Description: "Hermes role definition for multi-agent coordination",
        Allowed: []PromptPlacement{
            {Layer: PromptLayerKernel, Slot: PromptSlotIdentity}, // kernel 层，identity 之后
        },
        StableByDefault: true,
    }
}

func (c *hermesRoleContributor) ContributePrompt(ctx context.Context, req PromptBuildRequest) ([]PromptPart, error) {
    if c.mode == HermesPolicyFull {
        return nil, nil // Full 模式不注入
    }
    content := buildHermesRolePrompt(c.mode)
    return []PromptPart{{
        ID:      "kernel.hermes_role",
        Layer:   PromptLayerKernel,
        Slot:    PromptSlotIdentity,
        Source:  PromptSource{ID: "hermes:role", Name: "hermes:coordinator"},
        Title:   "Hermes role definition",
        Content: content,
        Stable:  true,
        Cache:   PromptCacheEphemeral,
    }}, nil
}
```

---

## 1.3 Tool 注册机制分析

### 注册流程

```
NewAgentLoop()
    └── registerSharedTools(al, cfg, msgBus, registry, provider)
            │
            for each agent in registry:
                │
                ├── [web] web_search tool          ← 全局开关: cfg.Tools.IsToolEnabled("web")
                ├── [web_fetch] web_fetch tool      ← 全局开关
                ├── [i2c] i2c tool                  ← Linux only
                ├── [spi] spi tool                  ← Linux only
                ├── [message] message tool          ← 全局开关
                ├── [reaction] reaction tool        ← 全局开关
                ├── [send_file] send_file tool      ← 全局开关
                ├── [send_tts] send_tts tool        ← TTS provider 可用时
                ├── [load_image] load_image tool    ← 全局开关
                ├── [find_skills] find_skills tool  ← 全局开关
                ├── [install_skill] install_skill tool ← 全局开关
                └── [spawn/subagent] spawn + subagent + spawn_status ← 全局开关
```

### ToolRegistry 能力

| 操作 | 支持 | 说明 |
|------|------|------|
| Register | ✅ | 注册 Tool |
| RegisterHidden | ✅ | 注册隐藏 Tool（仅 TTL 可见） |
| Clone | ✅ | 克隆注册表（SubTurn 使用） |
| **Remove/Unregister** | ❌ | **不支持！** |
| Get | ✅ | 按名获取 |
| List | ✅ | 列出所有 |
| Execute | ✅ | 执行 |
| ToProviderDefs | ✅ | 转为 LLM 可见的 Tool 定义 |

### 关键发现：ToolRegistry 没有 Remove 方法

这意味着：
- **方案 A（条件注册）** ✅ 可行：初始化时不注册不该有的工具
- **方案 B（运行时 Guard）** 需要新增 Remove 方法或使用 Guard 机制
- **降级时动态注册** ✅ 可行：可以随时 Register 新工具
- **恢复时动态移除** ❌ 不可行：需要新增 Remove 方法

### Tool 按角色分类

| Tool | Coordinator | Executor | Full |
|------|------------|----------|------|
| web_search | ❌ | ✅ | ✅ |
| web_fetch | ❌ | ✅ | ✅ |
| exec | ❌ | ✅ | ✅ |
| read_file | ❌ | ✅ | ✅ |
| write_file | ❌ | ✅ | ✅ |
| append_file | ❌ | ✅ | ✅ |
| message | ✅ | ✅ | ✅ |
| reaction | ✅ | ✅ | ✅ |
| send_file | ❌ | ✅ | ✅ |
| send_tts | ❌ | ✅ | ✅ |
| load_image | ❌ | ✅ | ✅ |
| find_skills | ❌ | ✅ | ✅ |
| install_skill | ❌ | ✅ | ✅ |
| spawn/subagent | ❌ | ✅ | ✅ |
| spawn_status | ❌ | ✅ | ✅ |
| cron | ✅ (查询) | ✅ | ✅ |
| **reef_submit_task** | ✅ | ❌ | ❌ |
| **reef_query_task** | ✅ | ❌ | ❌ |
| **reef_status** | ✅ | ❌ | ❌ |

---

## 1.4 SubTurn 与 Steering 机制分析

### SubTurn 机制

```
AgentLoop (父 Turn)
    │
    ├── LLM 决定调用 spawn 工具
    │   └── spawnSubTurn(ctx, al, parentTS, cfg)
    │       └── 创建子 turnState (depth+1)
    │           └── runTurn() 独立执行
    │               └── 结果通过 pendingResults chan 回传
    │
    └── 父 Turn 轮询 pendingResults → 注入到消息流
```

**关键约束：**
- SubTurn 的 ToolRegistry 是父的 Clone，**移除了 spawn/spawn_status**（防止递归）
- SubTurn 有独立的 SystemPrompt："You are a subagent..."
- SubTurn 结果通过 `pendingResults` channel 异步回传

### Steering 机制

```
用户消息 A 到达 → Turn-A 开始执行
用户消息 B 到达 → Turn-A 还在执行 → B 入队 steeringQueue
Turn-A 完成 → dequeueSteeringMessages → 注入 B → Continue
```

**两种模式：**
- `one-at-a-time`：每次只弹出一个 steering 消息
- `all`：一次弹出全部

### SubTurn vs Hermes 分工

| 维度 | SubTurn | Hermes (reef_submit) |
|------|---------|---------------------|
| 执行位置 | 同一进程内 | 远程 Client 进程 |
| 并发模型 | goroutine + channel | WebSocket + Scheduler |
| Tool 共享 | Clone 父 ToolRegistry | Client 独立 ToolRegistry |
| 通信方式 | pendingResults chan | WebSocket 消息 |
| 适用场景 | 内部子任务并发 | 多 Agent 团队协作 |
| Coordinator 使用 | ❌ 禁用 | ✅ 核心工具 |
| Executor 使用 | ✅ 可用 | ❌ 禁用 |
| Full 使用 | ✅ 可用 | ❌ 不需要 |

---

## 1.5 关键发现汇总

### F1: Hermes 注入点明确

6 个注入点已标注，推荐 **PromptContributor + 条件注册** 组合方案。

### F2: ToolRegistry 缺少 Remove

降级/恢复场景需要动态增删工具。需要新增 `Remove(name string)` 方法。

### F3: Hook 系统可做运行时 Guard

BeforeTool Hook 可以拦截越界 Tool 调用，但需要在 Hook 中判断当前 Hermes 模式。

### F4: Coordinator 的最小 Tool 集需要精确定义

当前 18+ 个工具，Coordinator 只需 3-4 个。需要明确边界。

### F5: SwarmChannel 是 Client 端的完整实现

SwarmChannel.dispatchTask() → msgBus.PublishInbound() → AgentLoop 处理 → 结果回传 Server。
Client 端已有完整的 AgentLoop + Tool 执行能力。

### F6: 启动入口已分离

- `picoclaw gateway` → 启动 Gateway（单 Client 模式）
- `picoclaw reef-server` → 启动 Reef Server
- 需要新增 `picoclaw server` → 启动 Server 模式（Gateway + Reef + Hermes Coordinator）
