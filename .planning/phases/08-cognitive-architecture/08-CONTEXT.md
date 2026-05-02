# Phase 08: Agent 认知架构 — Context

**Gathered:** 2026-05-02

## Decisions

### 1. PicoClaw Session 隔离边界

**决策：方案 B — 单进程多 AgentInstance（扩展已有包）**

- 每个 Task 创建独立 `AgentInstance`，不同 `workspace`（`tasks/{task_id}/`）
- 并发模型：由 `ClientConnector.Capacity` 控制（capacity=1 串行，capacity=N 并行）
- 复用的已有包：
  - `picoclaw/pkg/agent/` — AgentInstance、ContextBuilder、Pipeline（扩展，不重写）
  - `picoclaw/pkg/session/` — SessionStore、session 隔离（扩展 task namespace）
  - `picoclaw/pkg/memory/` — Store 接口（扩展 episodic methods）
- **不新建** `pkg/reef/cognition/` 独立包，而是扩展 PicoClaw 现有基础设施

### 2. Memory 存储后端

**决策：单后端 SQLite（seahorse）**

- **Episodic**: SQLite `task_episodes` 新表（seahorse schema 扩展）
- **Semantic**: SQLite `genes`（已有，seahorse schema）
- **Procedural**: 文件系统 `skills/`（不变）
- **Working**: 内存 ContextLayers（不变）
- BoltDB 仅用于 Raft 共识（raft_log, raft_state, reef_state），不混用作 memory 检索

新增 seahorse schema：
```sql
CREATE TABLE IF NOT EXISTS task_episodes (
    id          INTEGER PRIMARY KEY,
    task_id     TEXT NOT NULL,
    timestamp   INTEGER NOT NULL,
    event_type  TEXT NOT NULL,
    summary     TEXT NOT NULL,
    tags        TEXT,
    created_at  TEXT DEFAULT (datetime('now'))
);
CREATE INDEX idx_task_episodes_task ON task_episodes(task_id);
```

权威源策略：
- Genes 的权威源是 Raft FSM（BoltDB `reef_state`）
- seahorse SQLite 存 genes 副本用于 FTS5 检索（非权威源）
- 客户端从 seahorse 直接读 genes
- memory_update 通过 Raft 共识写回 FSM → 同步到 seahorse

### 3. CNP 消息共识路由

**决策：5 种走 Raft 共识，11 种点对点 WebSocket**

| 通道 | 消息类型 | 数量 |
|------|---------|------|
| **Raft 共识** | memory_update, memory_prune, checkpoint_save, context_inject, strategy_result | 5 |
| **WS 点对点** | context_corruption, context_compact_done, context_restore, memory_query, memory_inject, strategy_suggest, strategy_ack, checkpoint_restore, long_task_heartbeat, long_task_progress, long_task_complete | 11 |

路由实现：
```go
var cnpConsensusTypes = map[CNPMessageType]bool{
    MsgMemoryUpdate:   true,
    MsgMemoryPrune:    true,
    MsgCheckpointSave: true,
    MsgContextInject:  true,
    MsgStrategyResult: true,
}
```

---

## Claude's Discretion

- Sandbox 的工作目录结构：`tasks/{task_id}/workspace/`、`tasks/{task_id}/sessions/`、`tasks/{task_id}/checkpoints/`
- ContextManager 的 compact 算法细节（保最近 5 轮完整内容 + 摘要旧内容）
- CorruptionGuard 的检测阈值（工具循环 5 次、空白轮 3 次、目标漂移检测）
- 检查点自动保存间隔：5 分钟 或 5 轮，取先触发者
- Prune 策略：episodic 记忆 30 天过期，最多保留 1000 条
- ContextLayers 中 Memory Injection Layer (L3) 最多注入 3 个 Gene + 2 个 Episodic

## Deferred Ideas

- CNP 消息的批量 Propose 优化（当 task 完成频率 > 10/s 时）
- 跨 task 记忆共享（当前每 task 独立 namespace）
- CNP 协议的版本协商（v1 → v2 升级策略）

---
*Context gathered for phase planning*
