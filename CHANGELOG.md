# Changelog

All notable changes to the Reef distributed multi-agent swarm system are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

## [compressor v1.0.0] — 2026-07-17

### Added — 压缩器包（pkg/compressor）

- **SmartCrusher** — JSON 结构截断压缩器。按数组长度、嵌套深度、字符串长度三级截断，支持 `PreserveKeys` 白名单。
- **CodeCompressor** — 代码感知行压缩器。保留 import/函数签名，截断函数体；`Priority="low"` 时行长度限制减半。
- **LogCompressor** — 日志去重压缩器。逐行解析时间戳+级别+模块，去重后折叠为计数摘要。
- **SearchCompressor** — 搜索结果压缩器。按 `relevance` 排序，截断到 `MaxTokens`，保留高相关结果。
- **CacheAligner** — 缓存对齐器。规范化时间戳/UUID/IP/Email 格式以提高 KV 缓存命中率。
- **ContentRouter** — 内容类型检测 + 压缩器自动路由。`application/json`→SmartCrusher, `text/code`→CodeCompressor, `text/plain`→LogCompressor, `application/search`→SearchCompressor.
- **CCR (Compressed Content Registry)** — 基于 SHA-256 的引用计数+TTL 存储。支持 Zstd 压缩，256 MiB 防 zip-bomb。
- **QualityScorer** — 语义保留度（JSON key / 代码关键字保留率）+ 保真度评分。
- **KVCacheReuse** — KV 缓存复用率测量工具（公共前缀长度 + 命中率估算）。
- **MetricsCollector** — 压缩比、P50/P95/P99 延迟百分位滑动窗口采集。
- **IngestHook** — 零侵入工具输出压缩 Hook。在 Seahorse Ingest 点压缩 oversize 工具结果。
- **AssembleHook** — 预算感知上下文组装 Hook。按 token 预算分配消息。
- **CompactHook** — 溢出触发二次压缩 Hook。Assemble 超预算时的最后手段。

### Added — Agent 层集成

- **`CompressToolOutput` 配置项** — Seahorse ContextManager 支持 `compress_tool_output` 开关（默认关闭）。
- **`Compressor.Decompress()` 接口方法** — 不可逆压缩器返回 pass-through；CCR Zstd 完整还原。
- **压缩标记协议** — 压缩后内容以 `[COMPRESSED]` 前缀标记，Assemble 时自动剥离。

### Changed — 接口变更

- **Compressor 接口** 新增 `Decompress(ctx, []byte) ([]byte, error)` 方法（向后不兼容）。
- **CompressOptions** 新增 `Priority` 和 `PreserveKeys` 字段。

### Performance (arm64, Go 1.26.2)

| 路径 | ns/op | B/op | allocs/op |
|:---|:---:|:---:|:---:|
| SmartCrusher (5KB JSON) | 8,627 | 2,329 | 42 |
| CodeCompressor | 3,025 | 1,248 | 12 |
| LogCompressor (small) | 11,550 | 645 | 13 |
| SearchCompressor (small) | 2,313 | 724 | 15 |
| ContentRouter | 9,586 | 2,681 | 54 |
| CompressorChain | 279,692 | 84,092 | 1,429 |
| MetricsRecordSuccess | 163.3 | 80 | 0 |

### Code Statistics

- **37 个 .go 源文件** / 7,175 行
- **121 个单元测试** / 29 个性能回归基准
- **0 TODO/FIXME**（全部清理干净）

## [0.3.0] — Reef v2.0 (Phase 1: Persistent Queue)

### Added

- **Persistent task queue** — `TaskStore` interface with `MemoryStore` and `SQLiteStore` implementations
- **SQLite WAL mode** — Server restarts automatically restore non-terminal tasks from SQLite database
- **`PersistentQueue`** — wraps `TaskStore` with in-memory cache for high-performance reads
- **`Queue` interface** — abstracts queue operations, enabling both in-memory and persistent implementations
- **Store configuration** — `store_type` (`memory` | `sqlite`) and `store_path` fields in `SwarmSettings`
- **CLI flags** — `--store-type` and `--store-path` for `picoclaw reef-server` command
- **Auto-directory creation** — SQLite store creates parent directories automatically
- **Comprehensive store tests** — 19 unit tests covering MemoryStore and SQLiteStore (CRUD, concurrent access, WAL mode, auto-directory)

### Added (Phase 2: TLS)

- **TLS native support** — Server WebSocket and Admin API support TLS via `tls.NewListener`
- **Client wss:// support** — Connector automatically configures TLS for `wss://` URLs
- **Custom CA certificates** — Client can specify `tls_ca_file` for self-signed servers
- **Mutual TLS (mTLS)** — Client can present certificates via `tls_cert_file` + `tls_key_file`
- **TLS skip verify** — `tls_skip_verify` option for development environments
- **TLSConfig struct** — Reusable TLS configuration with validation and cert loading
- **TLS configuration fields** — `tls_enabled`, `tls_cert_file`, `tls_key_file`, `tls_ca_file`, `tls_skip_verify` in SwarmSettings

### Added (Phase 3: Multi-channel Notifications)

- **NotificationManager** — fans out alerts to multiple channels concurrently with fault isolation
- **Notifier interface** — extensible notification channel abstraction
- **WebhookNotifier** — HTTP POST notifications (migrated from legacy webhook)
- **SlackNotifier** — Slack Incoming Webhook with Block Kit formatting
- **SMTPNotifier** — HTML email alerts via SMTP
- **FeishuNotifier** — Feishu (飞书) interactive card messages
- **WeComNotifier** — WeCom (企业微信) Markdown messages
- **Notification configuration** — `notifications` array in SwarmSettings with per-channel config
- **8 notification tests** — Manager fanout, fault isolation, all channel types

### Added (Phase 4: Web UI Dashboard)

- **Embedded Web UI** — SPA served at `/ui/` with `go:embed` (zero external dependencies)
- **Dark theme** — Responsive layout with CSS variables (#1a1a2e background, #0f3460 accents)
- **Hash-based routing** — `#/` (Overview), `#/tasks` (Tasks), `#/clients` (Clients)
- **SSE real-time events** — `/api/v2/events` pushes task/client/stats updates
- **REST API v2** — `/api/v2/status`, `/api/v2/tasks` (paginated), `/api/v2/clients`
- **EventBus** — Pub/sub for real-time UI updates with non-blocking publish
- **Task management** — Submit tasks, cancel tasks, pagination controls
- **16 UI tests** — Redirect, static serving, APIs, SSE connection, EventBus

### Added (Phase 5: Performance Baselines)

- **Performance test framework** — `test/perf/` with Report/RegressionReport structures
- **Percentile calculator** — p50, p95, p99 latency computation
- **Regression detection** — p99 +20% or throughput -15% triggers regression flag
- **9 baseline reports** — Task submit, status query, task list × concurrency 1/10/50
- **JSON report persistence** — Reports saved to `test/perf/results/`

### Fixed

- **Webhook backward compatibility** — Legacy `webhook_urls` auto-creates WebhookNotifier when `notifications[]` is empty
- **E2E test updated** — `TestE2E_Webhook_TaskEscalation` uses `notify.Alert` instead of `WebhookPayload`

## [0.4.0] — Reef v2.0 (Phase 3-5: Scheduler, DAG, Web UI)

### Added

- **Priority scheduler** — heap-based PriorityQueue with FIFO tie-breaking, default priority 5 (range 1-10)
- **Match strategies** — LeastLoad (default), RoundRobin, Affinity-based client selection
- **Timeout scanner** — periodic scanning of Running tasks with configurable timeout, starvation boost
- **DAG Engine** — parent/subtask orchestration with TaskBlocked/TaskRecovering/TaskAggregating states
- **GatewayBridge** — connects Scheduler to Gateway channel system for result routing
- **Result delivery** — task completion/failure results route back to originating chat channel via ReplyTo
- **ReplyTo context** — task carries reply channel/chat/message metadata for bidirectional result routing
- **Reef coordination tools** — reef_submit_task accepts reply_to_channel/chat_id/message_id params
- **Web UI** — full Reef swarm dashboard: overview cards, task table with filtering/priority, client list, task detail sheet, SSE event stream
- **Admin API** — /admin/tasks supports ?priority=N filtering

### Changed

- **Task model** — added Priority, ParentTaskID, SubTaskIDs, Dependencies, ReplyTo fields
- **Scheduler** — refactored from FIFO Dequeue to Peek-first non-blocking dispatch with match strategies
- **Store** — extended with task_relations table for DAG support
- **Server** — timeout scanner integrated into heartbeat goroutine, starvation boost support

### Fixed

- **PersistentQueue restore** — restored tasks now properly registered in scheduler in-memory map
- **State persistence** — task state changes (Completed/Failed) now persisted to SQLite immediately

## [0.2.0] — Reef v1.1

### Added

- **Config-driven Server mode** — `SwarmSettings.Mode` field (`"server"` | `"client"`) enables starting Reef Server via `config.json` without CLI flags
- **Docker Compose deployment** — `docker/docker-compose.reef.yml` with pre-configured Server + Coder + Analyst clients
- **Admin API authentication** — Bearer token protection for all `/admin/*` and `/tasks` endpoints (skipped when token is empty)
- **Admin webhook alerts** — `webhook_urls` config triggers POST notifications when tasks escalate to admin
- **Model routing hint** — `model_hint` field on task submission and dispatch payload for explicit model selection
- **Scheduler logger** — Scheduler now has its own structured logger for webhook and escalation events

### Changed

- `SwarmSettings` struct expanded with `Mode`, `WSAddr`, `AdminAddr`, `MaxQueue`, `MaxEscalations`, `WebhookURLs` fields
- `NewAdminServer()` now requires a `token` parameter
- `SchedulerOptions` includes `Logger` and `WebhookURLs`
- `msgTaskDispatch()` now accepts full `*Task` to populate all dispatch payload fields
- `OnDispatch` callback signature changed from `(taskID, clientID)` to `(task, clientID)`

### Fixed

- Documentation config examples now match actual code (`mode` field previously documented but not implemented)

## [0.1.0] — Reef v1.0

### Added

- **Reef v1.0.0** — Distributed multi-agent swarm orchestration system
  - WebSocket-based hub-and-spoke topology for Server-Client communication
  - Role-based task routing (`coder`, `analyst`, `tester`)
  - Skill-based client matching with load balancing
  - Task lifecycle management: dispatch, progress, completion, cancellation, pause/resume
  - Automatic failure retry with escalation policy (max 2 retries by default)
  - Client heartbeat and stale detection
  - Connection resilience: buffered control messages, reconnection support
  - HTTP Admin API: `/admin/status`, `/admin/tasks`, `POST /tasks`
  - YAML-based custom role configuration in `skills/roles/`
  - CLI command: `picoclaw reef-server`
  - Comprehensive E2E integration test suite (17 scenarios)
  - Full documentation: architecture, deployment, API reference, protocol spec

### Fixed

- WebSocket handshake now calls `scheduler.HandleClientAvailable()` after client registration, ensuring queued tasks are dispatched to newly connected clients.
