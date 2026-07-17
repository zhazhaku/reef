package compressor

import (
	"context"
	"testing"
)

// =============================================================================
// Helpers
// =============================================================================

func newTestQualityInput() (original []byte, compressed []byte) {
	// Simple JSON that SmartCrusher compresses (array truncation)
	original = []byte(`{"status":"ok","error":null,"items":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30]}`)
	sc := NewSmartCrusher()
	result, err := sc.Compress(context.Background(), original, CompressOptions{MaxTokens: 200})
	if err != nil {
		panic("helper: smart_crusher failed: " + err.Error())
	}
	compressed = []byte(result.Content)
	return
}

func newTestQualityInputCode() (original []byte, compressed []byte) {
	original = []byte("```go\nfunc main() {\n    // Critical: status check\n    if err != nil {\n        return\n    }\n    fmt.Println(\"hello\")\n}\n```")
	cc := NewCodeCompressor()
	result, err := cc.Compress(context.Background(), original, CompressOptions{MaxTokens: 200})
	if err != nil {
		panic("helper: code_compressor failed: " + err.Error())
	}
	compressed = []byte(result.Content)
	return
}

// =============================================================================
// TestCompressionQuality
// =============================================================================

func TestCompressionQuality(t *testing.T) {
	original, compressed := newTestQualityInput()

	q := EvaluateQuality(original, compressed, "application/json")
	if q == nil {
		t.Fatal("EvaluateQuality returned nil")
	}

	if q.OriginalSz <= 0 {
		t.Errorf("OriginalSz should be > 0, got %d", q.OriginalSz)
	}
	if q.FinalSz <= 0 {
		t.Errorf("FinalSz should be > 0, got %d", q.FinalSz)
	}
	// Ratio can exceed 1.0 for small payloads (truncation markers add overhead)
	if q.Ratio <= 0 {
		t.Errorf("Ratio should be > 0, got %.2f", q.Ratio)
	}
}

// =============================================================================
// TestCompressionQualityJSONKeyPreservation
// =============================================================================

func TestCompressionQualityJSONKeyPreservation(t *testing.T) {
	original, compressed := newTestQualityInput()

	q := EvaluateQuality(original, compressed, "application/json")

	// Critical JSON keys should be preserved
	if q.KeyPreservation == nil {
		t.Fatal("KeyPreservation map is nil")
	}

	// "status" key must be preserved in compressed output
	preserved, ok := q.KeyPreservation["status"]
	if !ok {
		t.Error("KeyPreservation missing 'status' key")
	}
	if !preserved {
		t.Error("expected 'status' key to be preserved in compressed output")
	}

	// "error" key (even if null) should be tracked
	if _, ok := q.KeyPreservation["error"]; !ok {
		t.Error("KeyPreservation missing 'error' key")
	}

	// At least one critical key from original should be tracked
	foundAny := false
	for _, v := range q.KeyPreservation {
		if v {
			foundAny = true
			break
		}
	}
	if !foundAny {
		t.Error("at least one critical key should be preserved")
	}
}

// =============================================================================
// TestCompressionQualitySemanticRetention
// =============================================================================

func TestCompressionQualitySemanticRetention(t *testing.T) {
	original, compressed := newTestQualityInput()

	q := EvaluateQuality(original, compressed, "application/json")

	if q.SemanticRetention < 0 {
		t.Errorf("SemanticRetention should be >= 0, got %.4f", q.SemanticRetention)
	}
	if q.SemanticRetention > 1.0 {
		t.Errorf("SemanticRetention should be <= 1.0, got %.4f", q.SemanticRetention)
	}

	// For JSON with at least one preserved key, retention should be > 0
	if q.SemanticRetention <= 0 {
		t.Errorf("SemanticRetention should be > 0 for JSON with preserved keys, got %.4f", q.SemanticRetention)
	}
}

// =============================================================================
// TestCompressionQualityEmptyInput
// =============================================================================

func TestCompressionQualityEmptyInput(t *testing.T) {
	original := []byte("")
	compressed := []byte("")

	q := EvaluateQuality(original, compressed, "text/plain")

	if q == nil {
		t.Fatal("EvaluateQuality returned nil for empty input")
	}
	if q.OriginalSz != 0 {
		t.Errorf("OriginalSz for empty input should be 0, got %d", q.OriginalSz)
	}
	if q.FinalSz != 0 {
		t.Errorf("FinalSz for empty input should be 0, got %d", q.FinalSz)
	}
	if q.Ratio != 0 {
		t.Errorf("Ratio for empty input should be 0, got %.2f", q.Ratio)
	}
}

// =============================================================================
// TestCompressionQualityCodeContent
// =============================================================================

func TestCompressionQualityCodeContent(t *testing.T) {
	original, compressed := newTestQualityInputCode()

	q := EvaluateQuality(original, compressed, "text/code")

	if q == nil {
		t.Fatal("EvaluateQuality returned nil for code content")
	}
	if q.KeywordPreservation == nil {
		t.Fatal("KeywordPreservation map is nil")
	}
	if len(q.KeywordPreservation) == 0 {
		t.Error("KeywordPreservation should not be empty for code with keywords")
	}
}

// =============================================================================
// BenchmarkEvaluateQuality
// =============================================================================

func BenchmarkEvaluateQuality(b *testing.B) {
	original, compressed := newTestQualityInput()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EvaluateQuality(original, compressed, "application/json")
	}
}

func BenchmarkEvaluateQualityCode(b *testing.B) {
	original, compressed := newTestQualityInputCode()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = EvaluateQuality(original, compressed, "text/code")
	}
}
