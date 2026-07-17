package compressor

import (
	"context"
	"testing"
)

// =============================================================================
// Helpers
// =============================================================================

func newKVCacheTestInput() (before string, after string) {
	// Simulate two similar messages that differ only in dynamic content
	before = "2024-01-15T10:30:00Z [ERROR] module-a: connection timeout for 192.168.1.100\n"
	after = "2024-01-15T10:30:05Z [ERROR] module-a: connection timeout for 192.168.1.101\n"

	ca := NewCacheAligner()
	result1, err := ca.Compress(context.Background(), []byte(before), CompressOptions{})
	if err == nil {
		before = result1.Content
	}
	result2, err := ca.Compress(context.Background(), []byte(after), CompressOptions{})
	if err == nil {
		after = result2.Content
	}
	return
}

// =============================================================================
// TestKVCacheMeasure
// =============================================================================

func TestKVCacheMeasure(t *testing.T) {
	before, after := newKVCacheTestInput()

	m := MeasureKVCacheReuse(before, after)
	if m == nil {
		t.Fatal("MeasureKVCacheReuse returned nil")
	}

	if m.ReuseRate < 0 {
		t.Errorf("ReuseRate should be >= 0, got %.4f", m.ReuseRate)
	}
	if m.ReuseRate > 1.0 {
		t.Errorf("ReuseRate should be <= 1.0, got %.4f", m.ReuseRate)
	}
	// After stabilization, two similar messages should share a prefix
	if m.CommonPrefixLen <= 0 {
		t.Errorf("CommonPrefixLen should be > 0 after stabilization, got %d", m.CommonPrefixLen)
	}
}

// =============================================================================
// TestKVCacheStabilization
// =============================================================================

func TestKVCacheStabilization(t *testing.T) {
	before, after := newKVCacheTestInput()

	// Common prefix length after stabilization
	commonLen := CommonPrefixLength(before, after)
	if commonLen <= 0 {
		t.Errorf("expected common prefix > 0 after stabilization, got %d", commonLen)
	}

	// The stabilized content should share a prefix
	prefixRatio := float64(commonLen) / float64(len(before))
	if prefixRatio < 0.1 {
		t.Errorf("prefix ratio should be > 0.1 after stabilization, got %.4f", prefixRatio)
	}
}

// =============================================================================
// TestKVCacheBaseline
// =============================================================================

func TestKVCacheBaseline(t *testing.T) {
	// Baseline: two identical contents should have 100% reuse
	content := "This is a stable prefix that should be shared across messages."
	ca := NewCacheAligner()
	result, err := ca.Compress(context.Background(), []byte(content), CompressOptions{})
	if err == nil {
		content = result.Content
	}

	m := MeasureKVCacheReuse(content, content)
	if m.ReuseRate < 0.9 {
		t.Errorf("identical content should have reuse rate >= 0.9, got %.4f", m.ReuseRate)
	}
	if m.CommonPrefixLen != int64(len(content)) {
		t.Errorf("identical content: CommonPrefixLen=%d, want %d", m.CommonPrefixLen, len(content))
	}
}

// =============================================================================
// TestKVCacheMeasureEmpty
// =============================================================================

func TestKVCacheMeasureEmpty(t *testing.T) {
	m := MeasureKVCacheReuse("", "")
	if m == nil {
		t.Fatal("MeasureKVCacheReuse returned nil for empty input")
	}
	if m.ReuseRate != 0 {
		t.Errorf("empty input reuse rate should be 0, got %.4f", m.ReuseRate)
	}
}

// =============================================================================
// TestKVCacheMeasureSingular
// =============================================================================

func TestKVCacheMeasureSingular(t *testing.T) {
	m := MeasureKVCacheReuse("hello", "")
	if m == nil {
		t.Fatal("MeasureKVCacheReuse returned nil")
	}
	// Single side empty should have 0 reuse
	if m.ReuseRate != 0 {
		t.Errorf("single-side content reuse rate should be 0, got %.4f", m.ReuseRate)
	}
}

// =============================================================================
// BenchmarkKVCacheReuse
// =============================================================================

func BenchmarkKVCacheReuse(b *testing.B) {
	before, after := newKVCacheTestInput()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = MeasureKVCacheReuse(before, after)
	}
}

func BenchmarkKVCacheReuseParallel(b *testing.B) {
	before, after := newKVCacheTestInput()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = MeasureKVCacheReuse(before, after)
		}
	})
}

func BenchmarkCommonPrefixLength(b *testing.B) {
	before, after := newKVCacheTestInput()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CommonPrefixLength(before, after)
	}
}
