package compressor

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestSearchCompressorRegister
// ---------------------------------------------------------------------------

func TestSearchCompressorRegister(t *testing.T) {
	sc := NewSearchCompressor()
	if sc == nil {
		t.Fatal("NewSearchCompressor returned nil")
	}
	if sc.Name() != "search_compressor" {
		t.Fatalf("expected name 'search_compressor', got %q", sc.Name())
	}
	if sc.Type() != CompressTypeTruncate {
		t.Fatalf("expected type CompressTypeTruncate, got %v", sc.Type())
	}
}

// ---------------------------------------------------------------------------
// TestSearchCompress
// ---------------------------------------------------------------------------

func TestSearchCompress(t *testing.T) {
	sc := NewSearchCompressor()
	ctx := context.Background()

	// 20 results, each with title/url/snippet
	var parts []string
	for i := 1; i <= 20; i++ {
		parts = append(parts, formatSearchResult(i, "Title", "URL", "Snippet"))
	}
	input := []byte(strings.Join(parts, "\n"))

	opts := CompressOptions{MaxTokens: 500}
	result, err := sc.Compress(ctx, input, opts)
	if err != nil {
		t.Fatalf("Compress: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Compress: result is nil")
	}

	content := result.Content
	// For 20 results with MaxTokens=500, we expect fewer results
	lineCount := strings.Count(content, "\n") + 1

	// Should have compressed: fewer lines than input
	if lineCount >= 20 {
		t.Errorf("Compress: expected <20 lines after compression, got %d", lineCount)
	}
	// Title and URL should be preserved in each kept result
	if !strings.Contains(content, "Title") {
		t.Errorf("Compress: title not preserved")
	}
	if !strings.Contains(content, "URL") {
		t.Errorf("Compress: URL not preserved")
	}
	if result.Ratio >= 1.0 {
		t.Errorf("Compress: expected compression ratio < 1.0, got %.2f", result.Ratio)
	}

	// Verify meta
	if name, ok := result.Meta["compressor"].(string); !ok || name != "search_compressor" {
		t.Errorf("Compress: expected meta.compressor='search_compressor', got %q", name)
	}
}

// ---------------------------------------------------------------------------
// TestSearchCompressSmallResult
// ---------------------------------------------------------------------------

func TestSearchCompressSmallResult(t *testing.T) {
	sc := NewSearchCompressor()
	ctx := context.Background()

	// Only 3 results
	var parts []string
	for i := 1; i <= 3; i++ {
		parts = append(parts, formatSearchResult(i, "Title", "URL", "Snippet"))
	}
	input := []byte(strings.Join(parts, "\n"))

	opts := CompressOptions{MaxTokens: 1000}
	result, err := sc.Compress(ctx, input, opts)
	if err != nil {
		t.Fatalf("Compress small: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Compress small: result is nil")
	}

	// All 3 results should be kept when there are few
	if !strings.Contains(result.Content, "Result-1") {
		t.Errorf("Compress small: Result-1 not preserved")
	}
	if !strings.Contains(result.Content, "Result-2") {
		t.Errorf("Compress small: Result-2 not preserved")
	}
	if !strings.Contains(result.Content, "Result-3") {
		t.Errorf("Compress small: Result-3 not preserved")
	}
}

// ---------------------------------------------------------------------------
// TestSearchCompressFieldCompact
// ---------------------------------------------------------------------------

func TestSearchCompressFieldCompact(t *testing.T) {
	sc := NewSearchCompressor()
	ctx := context.Background()

	input := []byte(
		"title: Test Title\n" +
			"url: https://example.com\n" +
			"snippet: This is the search result snippet\n" +
			"timestamp: 2024-01-15T10:30:00Z\n" +
			"rank_score: 0.95\n" +
			"metadata: {some meta}\n\n" +
			"title: Another\n" +
			"url: https://another.com\n" +
			"snippet: Another snippet\n" +
			"timestamp: 2024-01-15T10:31:00Z\n" +
			"rank_score: 0.87\n",
	)

	opts := CompressOptions{MaxTokens: 200}
	result, err := sc.Compress(ctx, input, opts)
	if err != nil {
		t.Fatalf("Compress field compact: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Compress field compact: result is nil")
	}

	content := result.Content
	// Essential fields must be preserved
	if !strings.Contains(content, "title:") || !strings.Contains(content, "url:") ||
		!strings.Contains(content, "snippet:") {
		t.Errorf("Compress field compact: essential fields (title/url/snippet) not preserved")
	}
	// Redundant fields should be removed
	if strings.Contains(content, "timestamp:") {
		t.Errorf("Compress field compact: timestamp field not removed")
	}
	if strings.Contains(content, "rank_score:") {
		t.Errorf("Compress field compact: rank_score field not removed")
	}
	if strings.Contains(content, "metadata:") {
		t.Errorf("Compress field compact: metadata field not removed")
	}
}

// ---------------------------------------------------------------------------
// TestSearchCompressEmpty
// ---------------------------------------------------------------------------

func TestSearchCompressEmpty(t *testing.T) {
	sc := NewSearchCompressor()
	ctx := context.Background()

	result, err := sc.Compress(ctx, []byte(""), CompressOptions{MaxTokens: 100})
	if err != nil {
		t.Fatalf("Compress empty: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Compress empty: result is nil")
	}
	if result.Content != "" {
		t.Errorf("Compress empty: expected empty content, got %q", result.Content)
	}
}

// ---------------------------------------------------------------------------
// Helper: format a single search result
// ---------------------------------------------------------------------------

func formatSearchResult(i int, title, url, snippet string) string {
	return "--- Result-" + itoa(i) + " ---\n" +
		"title: " + title + " " + itoa(i) + "\n" +
		"url: " + url + "/" + itoa(i) + "\n" +
		"snippet: " + snippet + " for result " + itoa(i) + "\n" +
		"timestamp: 2024-01-15T10:30:00Z\n" +
		"rank_score: 0.95\n"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}
