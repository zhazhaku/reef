# compressor — v1.0.0

## Release v1.0.0 (2026-07-17)

### Quick Stats
- 37 .go files · 7,175 lines · 121 tests · 29 benchmarks
- 0 TODO/FIXME · go build/vet/test all PASS

### Included Compressors
| 压缩器 | 策略 | 适用内容 |
|:---|:---|:---|
| **SmartCrusher** | JSON 结构截断（数组/深度/字符串） | `application/json` |
| **CodeCompressor** | 代码感知行压缩 | `text/code` |
| **LogCompressor** | 日志去重 + 时间戳折叠 | `text/plain` |
| **SearchCompressor** | 搜索结果排序 + 截断 | `application/search` |
| **CacheAligner** | 时间戳/UUID/IP/Email 规范化 | 含缓存键内容 |
| **ContentRouter** | 内容类型检测 + 路由 | 任意类型 |

### Hooks
- **IngestHook** — 工具输出压缩（零侵入）
- **AssembleHook** — 预算感知上下文组装
- **CompactHook** — 溢出触发二次压缩

### Infrastructure
- **CCR (Compressed Content Registry)** — SHA-256 + 引用计数 + TTL + Zstd
- **QualityScorer** — 语义保留率 + 保真度评分
- **KVCacheReuse** — KV 缓存复用率测量
- **MetricsCollector** — P50/P95/P99 延迟 + 压缩比

### Breaking Changes
- `Compressor` 接口新增 `Decompress(ctx, []byte) ([]byte, error)` 方法
- `CompressOptions` 新增 `Priority` 和 `PreserveKeys` 字段

### Documentation
- [doc/BENCHMARKS.md](doc/BENCHMARKS.md) — 完整性能基准基线
- [doc/DEVELOPER.md](doc/DEVELOPER.md) — 开发者指南 + TDD 工作流
- [CHANGELOG.md](../../CHANGELOG.md) — 全量变更日志
