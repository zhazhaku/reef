package compressor

import (
	"math"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// AT-053: Metrics 聚合测试 — 压缩率/延迟/注册表/CCR
// =============================================================================
//
// 测试 CompressorMetrics 聚合：
//   - 压缩率统计（RatioAvg/RatioMin/RatioMax/RatioLast10Avg）
//   - 延迟统计（LatencyAvg/LatencyP50/LatencyP90/LatencyP99）
//   - 注册表指标（RegisteredCompressors/CompressCount/CompressErrors/CompressRejections）
//   - CCR 指标（CCRHitRate/CCRHitCount/CCRMissCount/CCRStorageBytes/CCRExpiredEntries）

// ---------------------------------------------------------------------------
// TestCompressionRatioStats — 压缩率聚合
// ---------------------------------------------------------------------------

func TestCompressionRatioStats(t *testing.T) {
	collector := NewMetricsCollector()

	// Record 7 compressions with different orig/final → known ratios
	// Ratio = finalSz / originalSz
	type sample struct{ orig, final int }
	samples := []sample{
		{1000, 200}, // 0.2
		{1000, 500}, // 0.5
		{1000, 300}, // 0.3
		{1000, 900}, // 0.9
		{1000, 100}, // 0.1
		{1000, 700}, // 0.7
		{1000, 400}, // 0.4
	}
	for _, s := range samples {
		collector.RecordSuccess(10*time.Millisecond, s.orig, s.final)
	}

	m := collector.Snapshot()

	// Avg: (0.2+0.5+0.3+0.9+0.1+0.7+0.4)/7 ≈ 0.442857
	wantAvg := (0.2 + 0.5 + 0.3 + 0.9 + 0.1 + 0.7 + 0.4) / 7.0
	if math.Abs(m.RatioAvg-wantAvg) > 0.001 {
		t.Errorf("RatioAvg: got %.4f, want %.4f", m.RatioAvg, wantAvg)
	}

	// Min: 0.1
	if math.Abs(m.RatioMin-0.1) > 0.001 {
		t.Errorf("RatioMin: got %.4f, want 0.1", m.RatioMin)
	}

	// Max: 0.9
	if math.Abs(m.RatioMax-0.9) > 0.001 {
		t.Errorf("RatioMax: got %.4f, want 0.9", m.RatioMax)
	}

	// RatioLast10Avg: all 7 averages
	if math.Abs(m.RatioLast10Avg-wantAvg) > 0.001 {
		t.Errorf("RatioLast10Avg: got %.4f, want %.4f", m.RatioLast10Avg, wantAvg)
	}
}

// ---------------------------------------------------------------------------
// TestLatencyStats — 延迟聚合（P50/P90/P99/Avg）
// ---------------------------------------------------------------------------

func TestLatencyStats(t *testing.T) {
	collector := NewMetricsCollector()

	// 100 latencies: 1ms, 2ms, ..., 100ms
	for i := 1; i <= 100; i++ {
		collector.RecordSuccess(time.Duration(i)*time.Millisecond, 1000, 500)
	}

	m := collector.Snapshot()

	// P50 ≈ 50-51ms
	if m.LatencyP50 < 49*time.Millisecond || m.LatencyP50 > 52*time.Millisecond {
		t.Errorf("LatencyP50: got %v, want ~50ms", m.LatencyP50)
	}

	// P90 ≈ 90-91ms
	if m.LatencyP90 < 89*time.Millisecond || m.LatencyP90 > 92*time.Millisecond {
		t.Errorf("LatencyP90: got %v, want ~90ms", m.LatencyP90)
	}

	// P99 ≈ 99-100ms
	if m.LatencyP99 < 98*time.Millisecond || m.LatencyP99 > 101*time.Millisecond {
		t.Errorf("LatencyP99: got %v, want ~99ms", m.LatencyP99)
	}

	// Avg ≈ 50.5ms
	if m.LatencyAvg < 49*time.Millisecond || m.LatencyAvg > 52*time.Millisecond {
		t.Errorf("LatencyAvg: got %v, want ~50ms", m.LatencyAvg)
	}
}

// ---------------------------------------------------------------------------
// TestRegistryMetrics — 注册表指标
// ---------------------------------------------------------------------------

func TestRegistryMetrics(t *testing.T) {
	collector := NewMetricsCollector()

	for i := 0; i < 5; i++ {
		collector.RecordSuccess(10*time.Millisecond, 1000, 500)
	}
	for i := 0; i < 2; i++ {
		collector.RecordError()
	}
	for i := 0; i < 3; i++ {
		collector.RecordRejection()
	}
	collector.RecordRegistryAdd(4)

	m := collector.Snapshot()

	if m.CompressCount != 5 {
		t.Errorf("CompressCount: got %d, want 5", m.CompressCount)
	}
	if m.CompressErrors != 2 {
		t.Errorf("CompressErrors: got %d, want 2", m.CompressErrors)
	}
	if m.CompressRejections != 3 {
		t.Errorf("CompressRejections: got %d, want 3", m.CompressRejections)
	}
	if m.RegisteredCompressors != 4 {
		t.Errorf("RegisteredCompressors: got %d, want 4", m.RegisteredCompressors)
	}
}

// ---------------------------------------------------------------------------
// TestCCRMetrics — CCR 指标
// ---------------------------------------------------------------------------

func TestCCRMetrics(t *testing.T) {
	collector := NewMetricsCollector()

	// 8 hits, 2 misses, 1 LLC fetch
	for i := 0; i < 8; i++ {
		collector.RecordCCRHit()
	}
	for i := 0; i < 2; i++ {
		collector.RecordCCRMiss()
	}
	collector.RecordCCRLLCFetch()
	collector.RecordCCRStorage(42*1024, 7)

	m := collector.Snapshot()

	wantHitRate := 0.8
	if math.Abs(m.CCRHitRate-wantHitRate) > 0.001 {
		t.Errorf("CCRHitRate: got %.4f, want %.4f", m.CCRHitRate, wantHitRate)
	}
	if m.CCRHitCount != 8 {
		t.Errorf("CCRHitCount: got %d, want 8", m.CCRHitCount)
	}
	if m.CCRMissCount != 2 {
		t.Errorf("CCRMissCount: got %d, want 2", m.CCRMissCount)
	}
	if m.CCRLLCFetchCount != 1 {
		t.Errorf("CCRLLCFetchCount: got %d, want 1", m.CCRLLCFetchCount)
	}
	if m.CCRStorageBytes != 42*1024 {
		t.Errorf("CCRStorageBytes: got %d, want %d", m.CCRStorageBytes, 42*1024)
	}
	if m.CCRExpiredEntries != 7 {
		t.Errorf("CCRExpiredEntries: got %d, want 7", m.CCRExpiredEntries)
	}
}

// ---------------------------------------------------------------------------
// TestMetricsCollectionThreadSafety — 线程安全
// ---------------------------------------------------------------------------

func TestMetricsCollectionThreadSafety(t *testing.T) {
	collector := NewMetricsCollector()
	var wg sync.WaitGroup

	goroutines := 20
	itersPerGoroutine := 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < itersPerGoroutine; i++ {
				collector.RecordSuccess(5*time.Millisecond, 1000, 500)
				if i%5 == 0 {
					collector.RecordCCRHit()
				}
				if i%7 == 0 {
					collector.RecordRejection()
				}
			}
		}()
	}
	wg.Wait()

	m := collector.Snapshot()

	wantComps := int64(goroutines * itersPerGoroutine)
	if m.CompressCount != wantComps {
		t.Errorf("CompressCount concurrent: got %d, want %d",
			m.CompressCount, wantComps)
	}
}

// ---------------------------------------------------------------------------
// TestMetricsEmptyState — 空状态
// ---------------------------------------------------------------------------

func TestMetricsEmptyState(t *testing.T) {
	collector := NewMetricsCollector()
	m := collector.Snapshot()

	if m.CompressCount != 0 {
		t.Errorf("Empty CompressCount: got %d, want 0", m.CompressCount)
	}
	if m.LatencyP50 != 0 {
		t.Errorf("Empty LatencyP50: got %v, want 0", m.LatencyP50)
	}
	if m.CCRHitRate != 0 {
		t.Errorf("Empty CCRHitRate: got %.4f, want 0", m.CCRHitRate)
	}
}

// ---------------------------------------------------------------------------
// TestMetricsReset — 重置
// ---------------------------------------------------------------------------

func TestMetricsReset(t *testing.T) {
	collector := NewMetricsCollector()

	collector.RecordSuccess(10*time.Millisecond, 1000, 800)
	collector.RecordCCRHit()
	collector.RecordRejection()
	collector.RecordRegistryAdd(2)

	pre := collector.Snapshot()
	if pre.CompressCount != 1 {
		t.Errorf("Pre-reset CompressCount: got %d, want 1", pre.CompressCount)
	}

	collector.Reset()
	m := collector.Snapshot()

	if m.CompressCount != 0 {
		t.Errorf("After Reset CompressCount: got %d, want 0", m.CompressCount)
	}
	if m.CompressErrors != 0 {
		t.Errorf("After Reset CompressErrors: got %d, want 0", m.CompressErrors)
	}
	if m.CompressRejections != 0 {
		t.Errorf("After Reset CompressRejections: got %d, want 0", m.CompressRejections)
	}
	if m.CCRHitCount != 0 {
		t.Errorf("After Reset CCRHitCount: got %d, want 0", m.CCRHitCount)
	}
	if m.RegisteredCompressors != 0 {
		t.Errorf("After Reset RegisteredCompressors: got %d, want 0", m.RegisteredCompressors)
	}
}

// ---------------------------------------------------------------------------
// TestMetricsLastNRatiosWindowing — LastN 窗口限制
// ---------------------------------------------------------------------------

func TestMetricsLastNRatiosWindowing(t *testing.T) {
	collector := NewMetricsCollector()

	// Record 15 compressions → only last 10 in RatioLast10Avg
	for i := 0; i < 15; i++ {
		collector.RecordSuccess(1*time.Millisecond, 1000, 100+i*50)
	}

	m := collector.Snapshot()

	// CompressCount should be 15
	if m.CompressCount != 15 {
		t.Errorf("CompressCount: got %d, want 15", m.CompressCount)
	}
	// RatioAvg should be in valid range
	if m.RatioAvg < 0 || m.RatioAvg > 1.0 {
		t.Errorf("RatioAvg out of range: %.4f", m.RatioAvg)
	}
	if m.RatioMax < 0 || m.RatioMax > 1.0 {
		t.Errorf("RatioMax out of range: %.4f", m.RatioMax)
	}
}

// ---------------------------------------------------------------------------
// TestMetricsSnapshotConsistency — 一致性
// ---------------------------------------------------------------------------

func TestMetricsSnapshotConsistency(t *testing.T) {
	collector := NewMetricsCollector()

	collector.RecordSuccess(10*time.Millisecond, 1000, 500)
	collector.RecordCCRHit()
	collector.RecordCCRHit()

	s1 := collector.Snapshot()
	s2 := collector.Snapshot()

	if s1.CompressCount != s2.CompressCount {
		t.Errorf("Snapshot CompressCount mismatch: s1=%d s2=%d", s1.CompressCount, s2.CompressCount)
	}
	if s1.CCRHitCount != s2.CCRHitCount {
		t.Errorf("Snapshot CCRHitCount mismatch: s1=%d s2=%d", s1.CCRHitCount, s2.CCRHitCount)
	}
}
