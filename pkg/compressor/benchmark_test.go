package compressor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// Benchmark Helpers — Payload Generators
// =============================================================================

// benchContentSmall returns a tiny JSON payload (~200 bytes)
func benchContentSmall() []byte {
	return []byte(`{"status":"ok","count":3,"items":[{"id":1,"val":"a"},{"id":2,"val":"b"},{"id":3,"val":"c"}]}`)
}

// benchContentMedium returns a medium JSON array (~5KB)
func benchContentMedium() []byte {
	var sb strings.Builder
	sb.WriteString(`{"data":[`)
	for i := 0; i < 100; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf(`{"id":%d,"name":"item-%d","value":"abcdefghijklmnopqrstuvwxyz0123456789"}`, i, i))
	}
	sb.WriteString(`]}`)
	return []byte(sb.String())
}

// benchContentLarge returns a large JSON payload (~50KB)
func benchContentLarge() []byte {
	var sb strings.Builder
	sb.WriteString(`{"data":[`)
	for i := 0; i < 1000; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf(`{"id":%d,"name":"benchmark-item-%d","value":"abcdefghijklmnopqrstuvwxyz0123456789","extra":"padding-content-%d"}`, i, i, i))
	}
	sb.WriteString(`]}`)
	return []byte(sb.String())
}

func benchContentJSON() []byte {
	return benchContentMedium()
}

// benchContentCode returns a code block in markdown fence
func benchContentCode() []byte {
	return []byte("```go\nfunc fibonacci(n int) int {\n    if n <= 1 {\n        return n\n    }\n    return fibonacci(n-1) + fibonacci(n-2)\n}\n\nfunc main() {\n    for i := 0; i < 20; i++ {\n        fmt.Printf(\"fib(%d) = %d\\n\", i, fibonacci(i))\n    }\n}\n```")
}

// benchContentLog returns multi-line log output
func benchContentLog() []byte {
	var sb strings.Builder
	for i := 0; i < 50; i++ {
		sb.WriteString("2024-01-15 10:30:00 ERROR [module] connection timeout for endpoint /api/v1/status\n")
		sb.WriteString("2024-01-15 10:30:01 WARN [module] retrying (attempt 1/3)\n")
		sb.WriteString("2024-01-15 10:30:02 INFO [module] connected successfully\n")
	}
	return []byte(sb.String())
}

// =============================================================================
// Integration Benchmarks — Regression Gates
// =============================================================================
//
// These benchmarks cover the end-to-end compression pipeline.
// REGRESSION-GATE thresholds below represent the maximum acceptable ns/op
// (2× current baseline). Any commit that exceeds these gates should be
// investigated before merging.
//
// Current baseline: arm64, Go 1.26.2, 2026-07-17

// =============================================================================
// Integration Benchmarks
// =============================================================================
//
// REGRESSION GATES (arm64 baseline, 2026-07-17):
//   CompressorChain         <  400,000 ns/op  (current: ~199,404)
//   ContentRouterIntegration <  500,000 ns/op  (current: ~230,862)
//   SmartCrusherSmall       <   40,000 ns/op  (current: ~18,652)
//   SmartCrusherMedium      < 1,200,000 ns/op  (current: ~729,984)
//   SmartCrusherLarge       < 5,000,000 ns/op  (current: ~2,122,018)
//   CodeCompressorSmall     <    8,000 ns/op  (current: ~3,360)
//   LogCompressorSmall      <   20,000 ns/op  (current: ~8,514)
//   SearchCompressorSmall   <    5,000 ns/op  (current: ~2,033)
//   MetricsRecordSuccess    <      300 ns/op  (current: ~148)
//   MetricsSnapshot         <   10,000 ns/op  (current: ~3,738)
// =============================================================================

// ---------------------------------------------------------------------------
// BenchmarkCompressorChain — full router + compressor pipeline
// ---------------------------------------------------------------------------
// REGRESSION-GATE: 400,000 ns/op

func BenchmarkCompressorChain(b *testing.B) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	hook := NewIngestHook(r)
	ctx := context.Background()
	content := benchContentJSON()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = hook.Process(ctx, content)
	}
}

// ---------------------------------------------------------------------------
// BenchmarkContentRouter — routing dispatch overhead
// ---------------------------------------------------------------------------

func BenchmarkContentRouterIntegration(b *testing.B) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	r.Register("text/plain", NewLogCompressor())
	r.Register("application/search", NewSearchCompressor())
	ctx := context.Background()
	content := benchContentJSON()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Route(ctx, content, opts)
	}
}

// ---------------------------------------------------------------------------
// BenchmarkAllCompressors — individual compressor benchmarks
// ---------------------------------------------------------------------------

func BenchmarkSmartCrusherIntegration(b *testing.B) {
	c := NewSmartCrusher()
	ctx := context.Background()
	content := benchContentJSON()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkCodeCompressorIntegration(b *testing.B) {
	c := NewCodeCompressor()
	ctx := context.Background()
	content := benchContentCode()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkLogCompressor(b *testing.B) {
	c := NewLogCompressor()
	ctx := context.Background()
	content := benchContentLog()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkSearchCompressor(b *testing.B) {
	c := NewSearchCompressor()
	ctx := context.Background()
	content := []byte("Title: Result 1\nURL: https://example.com/1\nSnippet: This is a test result\n\nTitle: Result 2\nURL: https://example.com/2\nSnippet: Another result here\n")
	opts := CompressOptions{MaxTokens: 200}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

// ---------------------------------------------------------------------------
// Parallel Benchmarks
// ---------------------------------------------------------------------------

func BenchmarkCompressorChainParallel(b *testing.B) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	hook := NewIngestHook(r)
	ctx := context.Background()
	content := benchContentJSON()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = hook.Process(ctx, content)
		}
	})
}

func BenchmarkContentRouterParallelIntegration(b *testing.B) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	ctx := context.Background()
	content := benchContentJSON()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = r.Route(ctx, content, opts)
		}
	})
}

func BenchmarkSmartCrusherParallel(b *testing.B) {
	c := NewSmartCrusher()
	ctx := context.Background()
	content := benchContentJSON()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = c.Compress(ctx, content, opts)
		}
	})
}

// =============================================================================
// Payload Size Benchmarks — Small / Medium / Large
// =============================================================================

func BenchmarkSmartCrusherSmall(b *testing.B) {
	c := NewSmartCrusher()
	ctx := context.Background()
	content := benchContentSmall()
	opts := CompressOptions{MaxTokens: 100}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkSmartCrusherMedium(b *testing.B) {
	c := NewSmartCrusher()
	ctx := context.Background()
	content := benchContentMedium()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkSmartCrusherLarge(b *testing.B) {
	c := NewSmartCrusher()
	ctx := context.Background()
	content := benchContentLarge()
	opts := CompressOptions{MaxTokens: 2000}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkCodeCompressorSmall(b *testing.B) {
	c := NewCodeCompressor()
	ctx := context.Background()
	content := benchContentCode()
	opts := CompressOptions{MaxTokens: 100}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkCodeCompressorMedium(b *testing.B) {
	c := NewCodeCompressor()
	ctx := context.Background()
	content := benchContentCode()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkLogCompressorSmall(b *testing.B) {
	c := NewLogCompressor()
	ctx := context.Background()
	content := []byte("2024-01-15 10:30:00 ERROR timeout\n")
	opts := CompressOptions{MaxTokens: 100}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkLogCompressorMedium(b *testing.B) {
	c := NewLogCompressor()
	ctx := context.Background()
	content := benchContentLog()
	opts := CompressOptions{MaxTokens: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

func BenchmarkSearchCompressorSmall(b *testing.B) {
	c := NewSearchCompressor()
	ctx := context.Background()
	content := []byte("Title: One\nSnippet: test\n")
	opts := CompressOptions{MaxTokens: 50}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = c.Compress(ctx, content, opts)
	}
}

// =============================================================================
// Metrics Benchmarks
// =============================================================================

func BenchmarkMetricsSnapshot(b *testing.B) {
	mc := NewMetricsCollector()
	for i := 0; i < 100; i++ {
		mc.RecordSuccess(10*time.Microsecond, 1000, 500)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mc.Snapshot()
	}
}

func BenchmarkMetricsRecordSuccess(b *testing.B) {
	mc := NewMetricsCollector()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mc.RecordSuccess(10*time.Microsecond, 1000, 500)
	}
}

func BenchmarkMetricsRecordParallel(b *testing.B) {
	mc := NewMetricsCollector()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mc.RecordSuccess(10*time.Microsecond, 1000, 500)
		}
	})
}
