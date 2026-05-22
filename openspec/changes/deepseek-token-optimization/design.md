# Design: 联合优化方案

## 0. 不丢记忆的核心保证

```
seahorse SQLite                 JSONL                          API 请求
(完整)                           (完整)                         (优化后)

Messages: full content          messages.jsonl: full          [system] static identity
  ├── content: "..."              ├── role: "user"            [system] static tools      
  ├── reasoning_content: "..."    ├── content: "long..."      [user] history msg 1       
  ├── parts[]:                    ├── ...                      [assistant] [truncated 4096]
  │   ├── tool_use: {...}                                     [user] current msg         
  │   └── tool_result: "FULL"                                (+ 动态上下文后置)          
  └── tokenCount: 5432

     short_expand() ← 随时可检索完整原始内容
                                                ↑ 优化截断只在这里
```

**截断原则**：只截断 `tool_result` 类型的消息 content（API 请求中），`tool_use`/`assistant`/`user` 消息保持完整。seahorse 和 JSONL 存储从不截断。

---

## 1. P1: Stable Prefix Partitioning（缓存命中率核心）

### 1.1 当前 vs 优化后

```
【当前 — 前缀缓存 0% 命中】
messages: [
  {role: "system", content: "You are reef...\nCurrent Time: 2026-05-08 03:13\nChannel: feishu\nChat ID: oc_64...\nSUMMARY: <summary id=...>"}
  // ↑ 全部混在一起 → 每次时间变了 → 整个 msg hash 变 → cache miss
  {role: "user", content: "hi"}
  ...
]

【优化后 — 前缀缓存 60%+ 命中】
messages: [
  {role: "system", content: "You are reef. ## Identity\n...[静态不变部分]..."}
  // ↑ 纯静态，每轮完全一致 → DeepSeek 前缀缓存命中! ¥0.02/1M
  {role: "user", content: "hi"}
  {role: "assistant", content: "Hello..."}
  ...
  {role: "user", content: "new question\n\n<RUNTIME>\nCurrent Time: 2026-05-08 03:13\nChannel: feishu\n<SUMMARY>\n<summary id=...>\n..."}
  // ↑ 动态内容+摘要注入最后一条 user msg → 不影响前缀
]
```

### 1.2 实现位置

**`prompt.go:BuildMessages()`**

当前逻辑：建 1 个 system message，把所有静态+动态内容串到一起。

改为：

```
func BuildMessages(...) []Message {
    // 1. 构建稳定 system message (静态身份 + rules + memory + skills)
    staticSystem := buildStaticSystemPrefix()

    // 2. 构建消息列表: [static system] + history + [user msg]
    msgs := []Message{staticSystem}
    msgs = append(msgs, history...)

    // 3. 动态内容注入最后一条 user message (summary + time + channel)
    lastUserMsg := msgs[len(msgs)-1]
    lastUserMsg.Content = injectDynamicContext(lastUserMsg.Content, dynamicContext)

    return msgs
}
```

### 1.3 `buildStaticSystemPrefix()` 内容

只包含不随时间/会话变化的 PromptPart：
| Layer | Slot | Source | Stable? |
|-------|------|--------|---------|
| Kernel | Identity | `PromptSourceKernel` | ✅ 静态 |
| Kernel | Hierarchy | `PromptSourceHierarchy` | ✅ 静态 |
| Instruction | Workspace | `PromptSourceWorkspace` | ✅ 静态 (文件 mtime 缓存) |
| Capability | Tooling | `PromptSourceToolDiscovery` | ✅ 静态 |
| Capability | SkillCatalog | `PromptSourceSkillCatalog` | ✅ 静态 |
| Context | Memory | `PromptSourceMemory` | ✅ 静态 (文件 mtime 缓存) |

不包含：
| Layer | Slot | Source | Why |
|-------|------|--------|-----|
| Context | Runtime | `PromptSourceRuntime` | Current Time 每轮变化 |
| Context | Summary | `PromptSourceSummary` | Summary 每轮变化 |
| Context | Output | `PromptSourceOutputPolicy` | 可以静态但量小可选 |

---

## 2. P2: 动态内容后置

### 2.1 注入策略

将所有动态内容注入到**最后一条 user message** 的 content 末尾：

```
user message content:
  "用户原始输入\n\n"
  + "<CONTEXT>\n"
  + "## Current Time\n2026-05-08 03:13 (Friday)\n\n"
  + "## Current Session\nChannel: feishu\nChat ID: oc_64...\n\n"
  + "## Current Sender\nSender: ou_8fba...\n\n"
  + "## CONTEXT_SUMMARY\n" + summary + "\n"
  + "</CONTEXT>"
```

### 2.2 备选方案

如果多 system message 方案可行（DeepSeek 接受），则将 CONTEXT_SUMMARY 单独作为最后一个 system message：

```
messages: [
  {role: "system", content: static},
  {role: "user", content: "hi"},
  {role: "assistant", content: "..."},
  {role: "system", content: "<CONTEXT>\nCurrent Time: ...\n<SUMMARY>\n..."},
  {role: "user", content: "new question"}
]
```

**优先验证方案 A（注入最后 user msg），更保守安全。**

---

## 3. P3: Tool Output Truncation（Token 削减核心）

### 3.1 截断点

在 `seahorseToProviderMessages()`（`context_seahorse.go`）中，构建 `providers.Message` 时对 tool_result 类型消息的 Content 做截断。

或者在 `prepareMessagesForRequest`（`provider.go`）中对所有 tool 类型的消息做截断。

### 3.2 截断策略

| Tool | 策略 | 保留量 | 标记 |
|------|------|--------|------|
| `web_fetch` | 保留前 N chars | 2000 chars | `[truncated X→2000, 用 web_fetch 获取完整]` |
| `exec` | 保留尾部 | 4096 chars | `[truncated, 保留尾部 4096]` |
| `read_file` | **不截断**（已有 offset/length） | 完整 | — |
| `short_grep` | **不截断**（已是摘要） | 完整 | — |
| `short_expand` | **不截断**（正在展开） | 完整 | — |
| `list_dir` | **不截断**（通常 < 500 chars） | 完整 | — |
| 其他工具 | 保留前 N | 2000 chars | `[truncated]` |

### 3.3 实现

```go
// pkg/agent/tool_truncate.go (新文件)

var toolTruncateLimits = map[string]int{
    "web_fetch": 2000,
    "exec":      4096,
    "web_search": 0, // 不截断
}

var fullPassTools = map[string]struct{}{
    "read_file": {}, "short_grep": {}, "short_expand": {},
    "list_dir": {}, "write_file": {}, "edit_file": {},
}

func truncateToolResult(msg providers.Message, toolName string) providers.Message {
    if _, full := fullPassTools[toolName]; full {
        return msg
    }
    limit, ok := toolTruncateLimits[toolName]
    if !ok {
        limit = 2000 // default
    }
    if len(msg.Content) > limit {
        msg.Content = msg.Content[:limit] +
            fmt.Sprintf("\n[truncated: %d→%d chars, use short_expand to recover]",
                len(msg.Content), limit)
    }
    return msg
}
```

### 3.4 安全措施

- seahorse SQLite **始终存储完整消息**（`providerToSeahorseMessage` 在 ingest 时调用，此时尚未截断）
- JSONL **始终存储完整消息**（`AddFullMessage` 在 ingest 前调用）
- 截断仅在 `seahorseToProviderMessages` → API 请求序列化这条链路上生效
- 用户始终可以通过 `short_expand` 恢复完整内容

---

## 4. P4: Reasoning Content 截断

### 4.1 问题

DeepSeek thinking mode 下 assistant 消息携带 `reasoning_content`，每条可达数千 chars。这些必须在后续请求中回传（否则 400 error）。

### 4.2 策略

在 `filterDeepSeekReasoningTurn`（`provider.go`）中，对 reasoning_content 超过阈值（4096 chars）的消息做截断：

```go
if len(cloned.ReasoningContent) > 4096 {
    cloned.ReasoningContent = cloned.ReasoningContent[:4096] +
        " [reasoning truncated: " + fmt.Sprint(len(cloned.ReasoningContent)) + "→4096 chars]"
}
```

**注意**：`ReasoningContentPresent` flag 必须保持为 true，否则 DeepSeek 会返回 400。

### 4.3 存储

seahorse 的 `seahorse.Message` 有独立字段存储完整 `ReasoningContent`，截断不影响。

---

## 5. 实现节点总览

| 节点 | 文件 | 改动类型 |
|------|------|----------|
| N1 | `prompt.go::BuildMessages` | 重构：稳定 system msg + 动态后置 |
| N2 | `prompt.go` 新增 `buildStaticSystemPrefix()` | 新函数 |
| N3 | `context_seahorse.go::seahorseToProviderMessages` | 新增截断调用 |
| N4 | `pkg/agent/tool_truncate.go` (新文件) | 工具分类型截断 |
| N5 | `provider.go::filterDeepSeekReasoningTurn` | 新增 reasoning 截断 |
| N6 | `context_usage.go::computeContextUsage` | 增强统计(静态/动态/缓存命中估算) |
| N7 | `context_cache_test.go` | 新增分区+截断测试 |
| N8 | 回归测试 | 验证所有现有测试通过 |

## 6. 数据流（优化后）

```
用户消息到达
    │
    ▼
SetupTurn:
    ├── seahorse.Assemble() → history (完整消息) + summary (XML)
    ├── BuildMessages(history, summary, ...):
    │   ├── staticSystemPrefix = buildStaticSystemPrefix()  ← N1/N2
    │   │   (Identity, Rules, Memory, Skills - 永不变化)
    │   ├── msgs = [staticSystem] + history + [userMsg]
    │   └── userMsg.Content += "<CONTEXT>" + time + summary + "</CONTEXT>"
    │
    ▼
CallLLM:
    ├── prepareMessagesForRequest(msgs):                     ← N3/N4/N5
    │   ├── 对 tool_result 消息按工具类型截断
    │   ├── 对 reasoning_content 超过 4096 的消息截断
    │   └── 确保 ReasoningContentPresent = true
    └── SerializeMessages → JSON → API POST
        │
        ├── [system] static prefix    ← 缓存命中!  ¥0.02/1M
        ├── [user/assistant] history  ← 部分未命中
        └── [user] msg + context      ← 未命中
```

## 7. 预期效果

```
优化前场景（10 轮对话，每轮 1 次 tool call）：
  System prompt:         8000 tokens  (45% static, 55% dynamic)
  History:               5000 tokens  (expand per turn)
  Current turn + tools:  3000 tokens
  Total per turn:        16000 tokens
  Cost per turn:         ¥0.016 (all cache miss)

优化后：
  System static:         3600 tokens  → 缓存命中 ¥0.00007
  System dynamic:        1800 tokens  → 未命中 ¥0.0018
  History (truncated):   2500 tokens  → 未命中 ¥0.0025
  Current turn:          2000 tokens  → 未命中 ¥0.0020
  Total input:           9900 tokens  (↓38%)
  Cost per turn:         ¥0.0064     (↓60%)
  Cache hit rate:        ~36% of input tokens

如果动态内容注入 user msg 而不是 system:
  Cache hit rate:        ~45% of input tokens
  Cost per turn:         ~¥0.0055     (↓66%)
```
