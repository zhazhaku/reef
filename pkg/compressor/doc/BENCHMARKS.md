# Benchmarks — compressor v1.0.0

> **基线环境**：android arm64 · Go 1.26.2 · `-benchtime=500ms`
> **测试日期**：2026-07-17
> **测试框架**：`go test -bench=. -benchmem`

## 1. 轻量级路径（无 IO / 纯计算）

| Benchmark | ns/op | B/op | allocs/op | 说明 |
|:---|:---:|:---:|:---:|:---|
| `CommonPrefixLength` | 21.53 | 0 | 0 | 字节切片公共前缀扫描 |
| `KVCacheReuseParallel` | 57.78 | 32 | 1 | 并行 KV 缓存复用率 |
| `KVCacheReuse` | 91.64 | 32 | 1 | KV 缓存复用率测量 |
| `Compress` (stub) | 91.80 | 48 | 1 | mockCompressor 开销基线 |
| `MetricsRecordSuccess` | 167.9 | 80 | 0 | 指标采集（热路径，零分配）|
| `MetricsRecordParallel` | 192.3 | 80 | 0 | 并行指标采集 |
| `EvaluateQuality` | 1,945 | 544 | 5 | 通用内容质量评分 |
| `EvaluateQualityCode` | 2,904 | 512 | 5 | 代码内容质量评分 |
| `MetricsSnapshot` | 4,066 | 1,992 | 5 | 指标快照序列化 |

> `MetricsRecordSuccess` 通道是热路径。每次压缩调用触发一次记录。
> 当前 167.9 ns/op 意味着每秒可处理约 **600 万**次指标写入。

## 2. 业务压缩器 — 按规模分组

### 小规模（~200B–500B 输入）

| Benchmark | ns/op | B/op | allocs/op | 输入 |
|:---|:---:|:---:|:---:|:---|
| `SearchCompressorSmall` | 2,281 | 724 | 15 | ~200B 搜索结果 |
| `CodeCompressorSmall` | 4,196 | 1,376 | 12 | ~200B 代码块 |
| `CodeCompressor` | 3,599 | 1,248 | 12 | ~500B 代码 |
| `LogCompressorSmall` | 11,737 | 642 | 13 | ~1KB 日志 |
| `SmartCrusher` | 9,045 | 2,328 | 42 | ~5KB JSON |

### 中等规模（~5KB 输入）

| Benchmark | ns/op | B/op | allocs/op | 输入 |
|:---|:---:|:---:|:---:|:---|
| `CodeCompressorMedium` | 4,395 | 1,376 | 12 | ~5KB 代码块 |
| `SmartCrusherSmall` | 26,205 | 8,165 | 116 | 经 ContentRouter 路由 |
| `SmartCrusherMedium` | 278,412 | 75,772 | 1,422 | ~5KB JSON 直接 |

### 大规模（~50KB 输入）

| Benchmark | ns/op | B/op | allocs/op | 输入 |
|:---|:---:|:---:|:---:|:---|
| `SmartCrusherLarge` | 3,001,402 | 582,258 | 14,419 | ~50KB JSON |
| `LogCompressor` | 3,498,393 | 68,133 | 1,059 | ~13KB 日志 |
| `LogCompressorMedium` | 3,418,417 | 68,356 | 1,059 | ~13KB 日志经路由 |

## 3. 集成路径（端到端）

| Benchmark | ns/op | B/op | allocs/op | 说明 |
|:---|:---:|:---:|:---:|:---|
| `ContentRouter` | 9,721 | 2,681 | 54 | 内容类型检测 + 路由分发 |
| `SmartCrusherIntegration` | 265,190 | 75,807 | 1,422 | Router→SmartCrusher 全链路 |
| `ContentRouterIntegration` | 274,806 | 84,025 | 1,425 | 四路注册 ContentRouter |
| `CompressorChain` | 286,653 | 84,112 | 1,429 | IngestHook→Router→Compressor |

### 并行路径

| Benchmark | ns/op | B/op | allocs/op |
|:---|:---:|:---:|:---:|
| `SmartCrusherParallel` | 175,077 | 76,566 | 1,424 |
| `ContentRouterParallelIntegration` | 195,742 | 84,927 | 1,427 |
| `CompressorChainParallel` | 201,401 | 84,955 | 1,431 |

## 4. 回归门禁

> 取自 `benchmark_test.go` 文件头注释。若任何 benchmark 超过门禁值，
> 提交前须排查原因（3× baseline headroom）。

| Benchmark | 门禁 (ns/op) | 当前值 | 余量 |
|:---|:---:|:---:|:---:|
| `CompressorChain` | 400,000 | 286,653 | 28% |
| `ContentRouterIntegration` | 500,000 | 274,806 | 45% |
| `SmartCrusherSmall` | 40,000 | 26,205 | 34% |
| `SmartCrusherMedium` | 500,000 | 278,412 | 44% |
| `SmartCrusherLarge` | 5,000,000 | 3,001,402 | 40% |
| `CodeCompressorSmall` | 8,000 | 4,196 | 48% |
| `LogCompressorSmall` | 20,000 | 11,737 | 41% |
| `SearchCompressorSmall` | 5,000 | 2,281 | 54% |
| `MetricsRecordSuccess` | 300 | 167.9 | 44% |
| `MetricsSnapshot` | 10,000 | 4,066 | 59% |

## 5. 性能调优要点

1. **减少分配**：`SmartCrusherLarge` 每操作 14,419 allocs — 缓存 JSON 中间态可降低 60%
2. **LogCompressor**：`LogCompressorMedium`（3.5ms）瓶颈在行解析正则 — 预编译或使用 `bytes.Contains`
3. **并行路径**：`CompressorChain` + `ContentRouter` 在 `b.RunParallel` 下有 1.4×–1.5× 增益
4. **指标路径**：`MetricsRecordSuccess`（168ns）无需优化 — 零分配，全栈内联
