package compressor

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TestCacheAlignerRegister
// ---------------------------------------------------------------------------

func TestCacheAlignerRegister(t *testing.T) {
	ca := NewCacheAligner()
	if ca == nil {
		t.Fatal("NewCacheAligner returned nil")
	}
	if ca.Name() != "cache_aligner" {
		t.Fatalf("expected name 'cache_aligner', got %q", ca.Name())
	}
	if ca.Type() != CompressTypeTruncate {
		t.Fatalf("expected type CompressTypeTruncate, got %v", ca.Type())
	}
}

// ---------------------------------------------------------------------------
// TestCacheAlignerTimestamp
// ---------------------------------------------------------------------------

func TestCacheAlignerTimestamp(t *testing.T) {
	ca := NewCacheAligner()
	ctx := context.Background()

	tests := []struct {
		name  string
		input string
	}{
		{"ISO8601 Z", "2024-01-15T10:30:00Z"},
		{"ISO8601 offset", "2024-01-15T10:30:00+08:00"},
		{"ISO8601 ms", "2024-01-15T10:30:00.123Z"},
		{"datetime space", "2024-01-15 10:30:00"},
		{"datetime ms", "2024-01-15 10:30:00.456"},
		{"syslog", "Jan 15 10:30:02"},
		{"slash date", "2024/01/15 10:30:03"},
		{"time only", "10:30:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ca.Compress(ctx, []byte(tt.input), CompressOptions{MaxTokens: 200})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result.Content, "<TIMESTAMP>") {
				t.Errorf("expected <TIMESTAMP> in output, got %q", result.Content)
			}
			if strings.Contains(result.Content, tt.input) && tt.input != "<TIMESTAMP>" {
				t.Errorf("raw timestamp not normalized: %q", result.Content)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCacheAlignerUUID
// ---------------------------------------------------------------------------

func TestCacheAlignerUUID(t *testing.T) {
	ca := NewCacheAligner()
	ctx := context.Background()

	input := "user 550e8400-e29b-41d4-a716-446655440000 logged in"
	result, err := ca.Compress(ctx, []byte(input), CompressOptions{MaxTokens: 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result.Content, "<UUID>") {
		t.Errorf("expected <UUID> in output, got %q", result.Content)
	}
	if strings.Contains(result.Content, "550e8400") {
		t.Errorf("raw UUID not normalized: %q", result.Content)
	}
	// Non-UUID text should be preserved
	if !strings.Contains(result.Content, "logged in") {
		t.Errorf("non-UUID content not preserved: %q", result.Content)
	}
}

// ---------------------------------------------------------------------------
// TestCacheAlignerMixed
// ---------------------------------------------------------------------------

func TestCacheAlignerMixed(t *testing.T) {
	ca := NewCacheAligner()
	ctx := context.Background()

	input := `Request 550e8400-e29b-41d4-a716-446655440000 at 2024-01-15T10:30:00Z: success
Response a1b2c3d4-e5f6-7890-abcd-ef1234567890 at 2024-01-15 10:30:01: timeout`

	result, err := ca.Compress(ctx, []byte(input), CompressOptions{MaxTokens: 500})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := result.Content
	// Both UUIDs replaced
	if strings.Count(content, "<UUID>") < 2 {
		t.Errorf("expected at least 2 <UUID> in output, got %d: %q", strings.Count(content, "<UUID>"), content)
	}
	// Both timestamps replaced
	if strings.Count(content, "<TIMESTAMP>") < 2 {
		t.Errorf("expected at least 2 <TIMESTAMP> in output, got %d: %q", strings.Count(content, "<TIMESTAMP>"), content)
	}
	// Raw values gone
	if strings.Contains(content, "2024") {
		t.Errorf("raw year not normalized: %q", content)
	}
	if strings.Contains(content, "e29b") {
		t.Errorf("raw UUID fragment not normalized: %q", content)
	}
}

// ---------------------------------------------------------------------------
// TestCacheAlignerIPAndEmail
// ---------------------------------------------------------------------------

func TestCacheAlignerIPAndEmail(t *testing.T) {
	ca := NewCacheAligner()
	ctx := context.Background()

	input := "192.168.1.100 connected, admin@example.com notified"
	result, err := ca.Compress(ctx, []byte(input), CompressOptions{MaxTokens: 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := result.Content
	if !strings.Contains(content, "<IP>") {
		t.Errorf("expected <IP> in output, got %q", content)
	}
	if !strings.Contains(content, "<EMAIL>") {
		t.Errorf("expected <EMAIL> in output, got %q", content)
	}
}

// ---------------------------------------------------------------------------
// TestCacheAlignerURL
// ---------------------------------------------------------------------------

func TestCacheAlignerURL(t *testing.T) {
	ca := NewCacheAligner()
	ctx := context.Background()

	input := "GET https://api.example.com/v1/users HTTP/1.1"
	result, err := ca.Compress(ctx, []byte(input), CompressOptions{MaxTokens: 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	content := result.Content
	if !strings.Contains(content, "<URL>") {
		t.Errorf("expected <URL> in output, got %q", content)
	}
}

// ---------------------------------------------------------------------------
// TestCacheAlignerNoMatch
// ---------------------------------------------------------------------------

func TestCacheAlignerNoMatch(t *testing.T) {
	ca := NewCacheAligner()
	ctx := context.Background()

	input := "this is plain text with 123 numbers but no patterns"
	result, err := ca.Compress(ctx, []byte(input), CompressOptions{MaxTokens: 200})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Content should be preserved essentially unchanged
	if !strings.Contains(result.Content, "plain text") {
		t.Errorf("plain text content not preserved: %q", result.Content)
	}
	// No placeholders should appear
	for _, ph := range []string{"<TIMESTAMP>", "<UUID>", "<IP>", "<EMAIL>", "<URL>"} {
		if strings.Contains(result.Content, ph) {
			t.Errorf("unexpected placeholder %q in no-match content: %q", ph, result.Content)
		}
	}
}

// ---------------------------------------------------------------------------
// TestCacheAlignerEmptyInput
// ---------------------------------------------------------------------------

func TestCacheAlignerEmptyInput(t *testing.T) {
	ca := NewCacheAligner()
	ctx := context.Background()

	result, err := ca.Compress(ctx, []byte(""), CompressOptions{MaxTokens: 100})
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
