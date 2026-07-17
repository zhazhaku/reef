# Developer Guide — compressor v1.0.0

## 架构概览

```
pkg/compressor/  (37 .go 文件, 7,175 行, 121 测试, 29 benchmark)
│
├─ Layer 0: 接口定义
│   ├── compressor.go       — Compressor 接口、CompressOptions、CompressResult、Registry
│   └── versions.go         — 版本兼容性标记
│
├─ Layer 1: 业务压缩器（5 个实现）
│   ├── smart_crusher.go    — JSON 结构截断（数组/深度/字符串）
│   ├── code_compressor.go  — 代码感知行压缩（函数体折叠）
│   ├── log_compressor.go   — 日志去重和时间戳折叠
│   ├── search_compressor.go— 搜索结果排序截断
│   └── cache_aligner.go    — 时间戳/UUID/IP 规范化
│
├─ Layer 2: CCR（压缩内容注册表）
│   ├── ccr.go              — CCRStore 接口 + MemoryCCRStore
│   ├── ccr_zstd.go         — Zstd 压缩存储 + zip-bomb 防护
│   └── ccr_record.go       — CCRRecord 数据结构（RefCount/TTL/Tags）
│
├─ Layer 3: Hooks & 路由
│   ├── content_router.go   — 内容类型检测 + 压缩器路由
│   ├── content_router_ext.go— 预注册扩展路由
│   ├── ingest_hook.go      — 工具输出压缩 Hook（SHA-256）
│   ├── assemble_hook.go     — 预算感知上下文组装
│   └── compact_hook.go      — 溢出触发的强制压缩
│
├─ Layer 4: 质量 & 指标
│   ├── quality.go          — QualityScorer（语义保留/保真度）
│   ├── kv_cache.go         — KV 缓存复用率测量
│   └── metrics.go          — MetricsCollector（P50/P95/P99）
│
└─ 测试文件（*_test.go, 11 个文件, 121 测试）
    ├── benchmark_test.go   — 29 项性能回归基准（含门禁注释）
    ├── smart_crusher_test.go— SmartCrusher 表驱动测试
    ├── code_compressor_test.go
    ├── content_router_test.go
    ├── ccr_test.go          — CCR 表驱动测试（RefCount/TTL/CRUD）
    ├── ccr_zstd_test.go
    ├── log_compressor_test.go
    ├── search_compressor_test.go
    ├── cache_aligner_test.go
    ├── compressor_test.go
    ├── kv_cache_measure_test.go
    └── quality_test.go
```

## TDD 工作流（RED → GREEN → REFACTOR）

本项目严格遵循 TDD 三阶段：

### 1. RED — 先写失败的测试
```go
func TestNewCompressor(t *testing.T) {
    c := NewMyCompressor()
    if c.Name() != "my-compressor" {
        t.Errorf("expected 'my-compressor', got %q", c.Name())
    }
}
```

### 2. GREEN — 最小实现通过测试
```go
type MyCompressor struct{}

func NewMyCompressor() *MyCompressor { return &MyCompressor{} }
func (m *MyCompressor) Name() string { return "my-compressor" }
func (m *MyCompressor) Type() CompressType { return CompressTypeTruncate }
func (m *MyCompressor) Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
    return &CompressResult{Content: string(content)}, nil
}
func (m *MyCompressor) Decompress(ctx context.Context, compressed []byte) ([]byte, error) {
    return compressed, nil
}
```

### 3. REFACTOR — 优化但不改变行为
```bash
go test -count=1 ./pkg/compressor/   # 确认全部 121 测试仍然通过
go vet ./pkg/compressor/             # 确认零警告
```

## 如何添加新压缩器

### Step 1: 创建压缩器文件
在 `pkg/compressor/` 下创建 `my_compressor.go`：

```go
package compressor

import "context"

// MyCompressor implements Compressor for [domain-specific content].
type MyCompressor struct{}

func NewMyCompressor() *MyCompressor { return &MyCompressor{} }

// Compress implements Compressor interface.
func (m *MyCompressor) Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
    originalSz := len(content)
    // ... 压缩逻辑 ...
    return &CompressResult{
        Content:    compressed,
        OriginalSz: originalSz,
        FinalSz:    len(compressed),
        Ratio:      float64(len(compressed)) / float64(originalSz),
    }, nil
}

func (m *MyCompressor) Name() string              { return "my-compressor" }
func (m *MyCompressor) Type() CompressType        { return CompressTypeTruncate }
func (m *MyCompressor) Decompress(ctx context.Context, compressed []byte) ([]byte, error) {
    return compressed, nil // 有损压缩 = pass-through
}

// 编译期接口检查
var _ Compressor = (*MyCompressor)(nil)
```

### Step 2: 注册到 ContentRouter

在 `content_router_ext.go` 中注册：
```go
func init() {
    router := GetDefaultRouter()
    router.Register("application/x-my-content", NewMyCompressor())
}
```

### Step 3: 编写测试

在 `my_compressor_test.go` 中：
```go
func TestMyCompressor_Compress(t *testing.T) {
    tests := []struct {
        name    string
        content []byte
        wantRatio float64 // ratio should be <= 1.0
    }{
        {"empty", []byte{}, 0},
        {"trivial", []byte("hello"), 1.0},
        {"large", generateLargeInput(), 0.5},
    }
    c := NewMyCompressor()
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := c.Compress(context.Background(), tt.content, CompressOptions{})
            if err != nil {
                t.Fatal(err)
            }
            if result.Ratio > 1.0 {
                t.Errorf("ratio > 1.0: %f", result.Ratio)
            }
        })
    }
}

func BenchmarkMyCompressor(b *testing.B) {
    c := NewMyCompressor()
    content := generateBenchContent()
    ctx := context.Background()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        c.Compress(ctx, content, CompressOptions{})
    }
}
```

### Step 4: 验证
```bash
go build ./pkg/compressor/
go vet ./pkg/compressor/
go test -count=1 -run TestMyCompressor ./pkg/compressor/
go test -bench=BenchmarkMyCompressor -benchmem ./pkg/compressor/
```

## 性能回归门禁

`benchmark_test.go` 文件头定义了 10 项回归门禁（见 BENCHMARKS.md §4）。任何提交导致 benchmark 超过门禁值时，CI 应当阻断。

当前门禁（arm64 baseline）：
```
CompressorChain          < 400,000 ns/op
ContentRouterIntegration < 500,000 ns/op
SmartCrusherSmall        <  40,000 ns/op
SmartCrusherMedium       < 500,000 ns/op
SmartCrusherLarge        < 5,000,000 ns/op
CodeCompressorSmall      <   8,000 ns/op
LogCompressorSmall       <  20,000 ns/op
SearchCompressorSmall    <   5,000 ns/op
MetricsRecordSuccess     <     300 ns/op
MetricsSnapshot          <  10,000 ns/op
```

## Compressor 接口规范

```go
type Compressor interface {
    Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error)
    Decompress(ctx context.Context, compressed []byte) ([]byte, error)
    Name() string
    Type() CompressType
}
```

| 方法 | 说明 |
|:---|:---|
| `Compress` | 核心压缩方法。接收原始内容+选项，返回压缩结果。错误仅用于系统故障（非内容不可压缩）。|
| `Decompress` | 解压方法。有损压缩器返回 pass-through（原样返回）；CCR 无损压缩返回原始内容。|
| `Name` | 全局唯一名称。重复名称被 Register 拒绝（panic）。|
| `Type` | `CompressTypeTruncate` / `CompressTypeSummarise` / `CompressTypeDrop`。|

## 常见问题排查

### Q: `go vet` 报 "does not implement Compressor"
A: 确保实现了全部 4 个方法（新增 `Decompress`）。运行：
```bash
grep 'var _ Compressor' pkg/compressor/your_file.go
```

### Q: 测试失败 "expected ratio X, got Y"
A: 检查 `CompressOptions` 是否正确传递 `PreserveKeys` 和 `Priority`。运行单个测试：
```bash
go test -v -run TestYourCompressor ./pkg/compressor/
```

### Q: Benchmark 超过门禁值
A: 先确认非环境因素（CPU throttling、其他进程）。若确认是代码回归：
```bash
go test -bench=YourBenchmark -benchmem -count=5 ./pkg/compressor/ | tee perf.txt
```

### Q: 如何在 seahorse 中使用 IngestHook？
A: 在 context_seahorse.go 中：
```go
router := compressor.NewContentRouter()
router.Register("application/json", compressor.NewSmartCrusher())
hook := compressor.NewIngestHook(router)
result, err := hook.Process(ctx, toolOutput)
// 使用 result.Content（压缩后内容）
```

### Q: 如何禁用压缩？
A: 在 ContextManager 配置中设置 `"compress_tool_output": false`（默认值，压缩完全关闭）。

## 版本信息

- **当前版本**：`v1.0.0`（`const Version = "v1.0.0"`）
- **发布文档**：[VERSION.md](../VERSION.md)
- **变更日志**：[../../../CHANGELOG.md](../../../CHANGELOG.md) § compressor v1.0.0
