package compressor

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestLogCompressorRegister
// ---------------------------------------------------------------------------

func TestLogCompressorRegister(t *testing.T) {
	lc := NewLogCompressor()
	if lc == nil {
		t.Fatal("NewLogCompressor returned nil")
	}
	if lc.Name() != "log_compressor" {
		t.Fatalf("expected name 'log_compressor', got %q", lc.Name())
	}
	if lc.Type() != CompressTypeTruncate {
		t.Fatalf("expected type CompressTypeTruncate, got %v", lc.Type())
	}
}

// ---------------------------------------------------------------------------
// TestLogCompressStandardLog
// ---------------------------------------------------------------------------

func TestLogCompressStandardLog(t *testing.T) {
	lc := NewLogCompressor()
	ctx := context.Background()

	input := []byte("2024-01-15 10:30:00 ERROR [module] connection timeout\n" +
		"2024-01-15 10:30:01 INFO  [module] retrying in 5s\n" +
		"2024-01-15 10:30:02 DEBUG [module] attempt 2/3\n")

	opts := CompressOptions{MaxTokens: 500}
	result, err := lc.Compress(ctx, input, opts)
	if err != nil {
		t.Fatalf("Compress: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Compress: result is nil")
	}

	content := result.Content
	// Time stamps should be normalized to <TIMESTAMP>
	if strings.Contains(content, "2024-01-15") {
		t.Errorf("Compress: expected timestamps to be normalized to <TIMESTAMP>, got raw timestamp")
	}
	// Log levels must be preserved
	if !strings.Contains(content, "ERROR") {
		t.Errorf("Compress: ERROR level not preserved")
	}
	if !strings.Contains(content, "INFO") {
		t.Errorf("Compress: INFO level not preserved")
	}
	if !strings.Contains(content, "DEBUG") {
		t.Errorf("Compress: DEBUG level not preserved")
	}
	// Error message must be preserved
	if !strings.Contains(content, "connection timeout") {
		t.Errorf("Compress: 'connection timeout' message not preserved")
	}
	// Duplicate repeated lines should be collapsed
	if result.Ratio >= 1.0 {
		t.Errorf("Compress: expected compression ratio < 1.0, got %.2f", result.Ratio)
	}

	// Verify meta
	if name, ok := result.Meta["compressor"].(string); !ok || name != "log_compressor" {
		t.Errorf("Compress: expected meta.compressor='log_compressor', got %q", name)
	}
}

// ---------------------------------------------------------------------------
// TestLogCompressTimestamp
// ---------------------------------------------------------------------------

func TestLogCompressTimestamp(t *testing.T) {
	lc := NewLogCompressor()
	ctx := context.Background()

	// Multiple timestamp formats
	input := []byte(
		"2024-01-15T10:30:00Z ERROR test1\n" +
			"2024-01-15 10:30:01 ERROR test2\n" +
			"Jan 15 10:30:02 ERROR test3\n" +
			"2024/01/15 10:30:03 ERROR test4\n",
	)

	opts := CompressOptions{MaxTokens: 500}
	result, err := lc.Compress(ctx, input, opts)
	if err != nil {
		t.Fatalf("Compress: unexpected error: %v", err)
	}

	content := result.Content
	// All raw timestamps must be gone
	if strings.Contains(content, "2024") {
		t.Errorf("Compress: raw timestamps not normalized")
	}
	// <TIMESTAMP> placeholder must appear for each line
	count := strings.Count(content, "<TIMESTAMP>")
	if count < 4 {
		t.Errorf("Compress: expected at least 4 <TIMESTAMP> placeholders, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// TestLogCompressLevelPreservation
// ---------------------------------------------------------------------------

func TestLogCompressLevelPreservation(t *testing.T) {
	lc := NewLogCompressor()
	ctx := context.Background()

	input := []byte(
		"2024-01-15 10:30:00 DEBUG debug msg\n" +
			"2024-01-15 10:30:01 INFO info msg\n" +
			"2024-01-15 10:30:02 WARN warn msg\n" +
			"2024-01-15 10:30:03 ERROR error msg\n" +
			"2024-01-15 10:30:04 TRACE trace msg\n" +
			"2024-01-15 10:30:05 FATAL fatal msg\n",
	)

	opts := CompressOptions{MaxTokens: 500}
	result, err := lc.Compress(ctx, input, opts)
	if err != nil {
		t.Fatalf("Compress: unexpected error: %v", err)
	}

	content := result.Content
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR", "TRACE", "FATAL"}
	for _, level := range levels {
		if !strings.Contains(content, level) {
			t.Errorf("Compress: log level %q not preserved", level)
		}
	}
}

// ---------------------------------------------------------------------------
// TestLogCompressStackTrace
// ---------------------------------------------------------------------------

func TestLogCompressStackTrace(t *testing.T) {
	lc := NewLogCompressor()
	ctx := context.Background()

	input := []byte(
		"2024-01-15 10:30:00 ERROR panic: something broke\n" +
			"goroutine 1 [running]:\n" +
			"main.main()\n" +
			"\t/app/main.go:42 +0x1a3\n" +
			"runtime.main()\n" +
			"\t/usr/local/go/src/runtime/proc.go:250 +0x2fe\n",
	)

	opts := CompressOptions{MaxTokens: 500}
	result, err := lc.Compress(ctx, input, opts)
	if err != nil {
		t.Fatalf("Compress: unexpected error: %v", err)
	}

	content := result.Content
	// Stack trace lines should be compressed to <stacktrace>
	if !strings.Contains(content, "<stacktrace>") {
		t.Errorf("Compress: stack trace not compressed to <stacktrace> marker")
	}
	// Source file references should be gone
	if strings.Contains(content, ".go:") {
		t.Errorf("Compress: source file references not compressed")
	}
	// Error message should still be there
	if !strings.Contains(content, "panic: something broke") {
		t.Errorf("Compress: error message not preserved")
	}
}

// ---------------------------------------------------------------------------
// TestLogCompressEmptyInput
// ---------------------------------------------------------------------------

func TestLogCompressEmptyInput(t *testing.T) {
	lc := NewLogCompressor()
	ctx := context.Background()

	result, err := lc.Compress(ctx, []byte(""), CompressOptions{MaxTokens: 100})
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
