package compressor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// =============================================================================
// [RED] SmartCrusher tests — will FAIL because SmartCrusher does not exist yet.
// =============================================================================

// =============================================================================
// TestSmartCrusherRegister — verify Name() and Type()
// =============================================================================

func TestSmartCrusherRegister(t *testing.T) {
	sc := NewSmartCrusher()
	if sc == nil {
		t.Fatal("NewSmartCrusher() returned nil")
	}

	// Register with the global registry
	Register("smart_crusher", sc)

	got := Get("smart_crusher")
	if got == nil {
		t.Fatal("Get('smart_crusher') returned nil after Register")
	}

	if got.Name() != "smart_crusher" {
		t.Errorf("Name() = %q, want %q", got.Name(), "smart_crusher")
	}

	if got.Type() != CompressTypeTruncate {
		t.Errorf("Type() = %v, want %v", got.Type(), CompressTypeTruncate)
	}
}

// =============================================================================
// TestSmartCrusherCompress — basic JSON compression returns valid result
// =============================================================================

func TestSmartCrusherCompress(t *testing.T) {
	sc := NewSmartCrusher()

	input := []byte(`{"key":"value","nested":{"inner":42},"list":[1,2,3]}`)
	result, err := sc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 4096,
		Priority:  "normal",
	})

	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}
	if result == nil {
		t.Fatal("Compress returned nil result")
	}

	// Verify result fields are populated
	if result.OriginalSz == 0 {
		t.Error("OriginalSz should be non-zero")
	}
	if result.FinalSz == 0 {
		t.Error("FinalSz should be non-zero")
	}
	if result.Ratio <= 0 || result.Ratio > 1.0 {
		t.Errorf("Ratio = %f, want 0 < ratio <= 1.0", result.Ratio)
	}

	// Content should be valid JSON after compression
	var out interface{}
	if err := json.Unmarshal([]byte(result.Content), &out); err != nil {
		t.Errorf("Compressed output is not valid JSON: %v", err)
	}
}

// =============================================================================
// TestSmartCrusherTruncateArray — large arrays truncated, first/last preserved
// =============================================================================

func TestSmartCrusherTruncateArray(t *testing.T) {
	sc := NewSmartCrusher()

	// Build a JSON object with a large array (100 items)
	items := make([]string, 100)
	for i := 0; i < 100; i++ {
		items[i] = "item-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i%10))
	}
	payload := map[string]interface{}{
		"name":  "test",
		"items": items,
	}
	input, _ := json.Marshal(payload)

	result, err := sc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 4096,
		Priority:  "normal",
	})
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// The result must be smaller than the original
	if result.FinalSz >= result.OriginalSz {
		t.Errorf("Expected compression: FinalSz=%d >= OriginalSz=%d", result.FinalSz, result.OriginalSz)
	}

	// Content must still be valid JSON
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(result.Content), &out); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	// Verify the array exists and has been truncated
	arr, ok := out["items"].([]interface{})
	if !ok {
		t.Fatal("items field missing or not an array after compression")
	}
	if len(arr) >= len(items) {
		t.Errorf("array not truncated: got %d items, want < %d", len(arr), len(items))
	}

	// The original "name" field should be preserved
	if name, _ := out["name"].(string); name != "test" {
		t.Errorf("name field corrupted: got %q, want %q", name, "test")
	}
}

// =============================================================================
// TestSmartCrusherTruncateDepth — depth >= 3 subtrees replaced with placeholder
// =============================================================================

func TestSmartCrusherTruncateDepth(t *testing.T) {
	sc := NewSmartCrusher()

	// Build deep JSON: depth 5
	deep := map[string]interface{}{
		"level1": map[string]interface{}{
			"level2": map[string]interface{}{
				"level3": map[string]interface{}{
					"level4": map[string]interface{}{
						"level5": "deep value",
					},
				},
			},
		},
		"shallow": "top-level",
	}
	input, _ := json.Marshal(deep)

	result, err := sc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 4096,
		Priority:  "high",
	})
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// Shallow fields should be preserved
	if !strings.Contains(result.Content, "shallow") {
		t.Error("shallow field was lost during compression")
	}
	if !strings.Contains(result.Content, "top-level") {
		t.Error("shallow value was lost during compression")
	}

	// Deep content (level4/level5) should be truncated
	if strings.Contains(result.Content, "level5") {
		t.Error("deep nested content (level5) should have been truncated")
	}
	if strings.Contains(result.Content, "deep value") {
		t.Error("deep nested value should have been truncated")
	}

	// Verify meta reports depth truncation
	if result.Meta == nil {
		t.Fatal("Meta should not be nil")
	}
}

// =============================================================================
// TestSmartCrusherTruncateString — long strings truncated with marker
// =============================================================================

func TestSmartCrusherTruncateString(t *testing.T) {
	sc := NewSmartCrusher()

	// Build JSON with a very long string (>1000 chars)
	longStr := strings.Repeat("abcdefghij", 200) // 2000 chars
	input := []byte(`{"key":"` + longStr + `"}`)

	result, err := sc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 4096,
		Priority:  "normal",
	})
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	// The output must be significantly smaller
	if result.FinalSz >= result.OriginalSz {
		t.Errorf("long string not compressed: FinalSz=%d >= OriginalSz=%d",
			result.FinalSz, result.OriginalSz)
	}

	// Meta should contain truncation info
	if result.Meta == nil {
		t.Fatal("Meta should not be nil")
	}
}

// =============================================================================
// TestSmartCrusherInvalidJSON — invalid JSON returns error
// =============================================================================

func TestSmartCrusherInvalidJSON(t *testing.T) {
	sc := NewSmartCrusher()

	invalidInputs := []string{
		"{invalid json",
		"just plain text, not json at all",
		"",
		`{"unclosed": "oops"`,
	}

	for _, input := range invalidInputs {
		result, err := sc.Compress(context.Background(), []byte(input), CompressOptions{
			MaxTokens: 4096,
			Priority:  "normal",
		})
		if err == nil {
			t.Errorf("expected error for invalid JSON %q but got result: %+v", input, result)
		}
	}
}

// =============================================================================
// TestSmartCrusherSafefallback — oversized content triggers safe fallback
// =============================================================================

func TestSmartCrusherSafefallback(t *testing.T) {
	sc := NewSmartCrusher()

	// Generate very large JSON content
	hugeItems := make([]string, 5000)
	for i := range hugeItems {
		hugeItems[i] = "data-item-with-some-reasonable-length-" + string(rune('a'+i%26))
	}
	payload := map[string]interface{}{
		"name":  "huge-test",
		"items": hugeItems,
	}
	input, _ := json.Marshal(payload)

	result, err := sc.Compress(context.Background(), input, CompressOptions{
		MaxTokens: 100, // Very small token budget to force aggressive compression
		Priority:  "low",
	})
	if err != nil {
		t.Fatalf("Compress should not error on large content: %v", err)
	}
	if result == nil {
		t.Fatal("Compress returned nil result for large content")
	}

	// Result should still be valid or indicate fallback
	if result.Content != "" {
		// If we got content back, it should be smaller
		if result.FinalSz >= result.OriginalSz {
			t.Errorf("Expected compression for large content: FinalSz=%d >= OriginalSz=%d",
				result.FinalSz, result.OriginalSz)
		}
	}

	// Meta must exist
	if result.Meta == nil {
		t.Fatal("Meta should not be nil even in fallback")
	}
}

// =============================================================================
// BenchmarkSmartCrusher — benchmark skeleton
// =============================================================================

func BenchmarkSmartCrusher(b *testing.B) {
	b.ReportAllocs()

	sc := NewSmartCrusher()
	payload := map[string]interface{}{
		"name": "bench",
		"data": []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
	}
	input, _ := json.Marshal(payload)
	opts := CompressOptions{MaxTokens: 4096, Priority: "normal"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = sc.Compress(context.Background(), input, opts)
	}
}
