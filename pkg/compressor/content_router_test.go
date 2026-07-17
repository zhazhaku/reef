package compressor

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestRouter() *ContentRouter {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	return r
}

// ---------------------------------------------------------------------------
// TestContentRouterRegister
// ---------------------------------------------------------------------------

func TestContentRouterRegister(t *testing.T) {
	r := NewContentRouter()
	if r == nil {
		t.Fatal("NewContentRouter returned nil")
	}

	sc := NewSmartCrusher()
	r.Register("application/json", sc)

	got, ok := r.registry["application/json"]
	if !ok {
		t.Fatal("SmartCrusher not found in registry after Register")
	}
	if got.Name() != sc.Name() {
		t.Fatalf("expected compressor %q, got %q", sc.Name(), got.Name())
	}
}

func TestContentRouterRegisterOverwrite(t *testing.T) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("application/json", NewCodeCompressor()) // overwrite

	got, ok := r.registry["application/json"]
	if !ok {
		t.Fatal("compressor not found after overwrite")
	}
	if got.Name() != "code_compressor" {
		t.Fatalf("expected code_compressor after overwrite, got %q", got.Name())
	}
}

// ---------------------------------------------------------------------------
// TestContentRouterRouteJSON
// ---------------------------------------------------------------------------

func TestContentRouterRouteJSON(t *testing.T) {
	r := newTestRouter()
	ctx := context.Background()

	// Large enough JSON that SmartCrusher will actually reduce
	content := []byte(`{"status":"ok","result":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20]}`)
	opts := CompressOptions{MaxTokens: 100}

	result, err := r.Route(ctx, content, opts)
	if err != nil {
		t.Fatalf("Route JSON: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Route JSON: result is nil")
	}
	// Should identify the compressor used
	name, ok := result.Meta["compressor"].(string)
	if !ok || name != "smart_crusher" {
		t.Errorf("Route JSON: expected meta.compressor='smart_crusher', got %q", name)
	}
	// Small payloads may not compress below 1.0; just verify it didn't error
	if result.Ratio < 1.0 {
		t.Logf("Route JSON: compressed, ratio=%.2f", result.Ratio)
	}
}

// ---------------------------------------------------------------------------
// TestContentRouterRouteCode
// ---------------------------------------------------------------------------

func TestContentRouterRouteCode(t *testing.T) {
	r := newTestRouter()
	ctx := context.Background()

	content := []byte("Here is some code:\n```\nfunc hello() {\n    fmt.Println(\"hi\")\n}\n```\n")
	opts := CompressOptions{MaxTokens: 200}

	result, err := r.Route(ctx, content, opts)
	if err != nil {
		t.Fatalf("Route code: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Route code: result is nil")
	}
	name, ok := result.Meta["compressor"].(string)
	if !ok || name != "code_compressor" {
		t.Errorf("Route code: expected meta.compressor='code_compressor', got %q", name)
	}
}

// ---------------------------------------------------------------------------
// TestContentRouterRouteUnknown
// ---------------------------------------------------------------------------

func TestContentRouterRouteUnknown(t *testing.T) {
	r := newTestRouter()
	ctx := context.Background()

	content := []byte("This is just plain text with no JSON or code markers.")
	opts := CompressOptions{MaxTokens: 200}

	_, err := r.Route(ctx, content, opts)
	if err == nil {
		t.Fatal("Route unknown: expected error for unregistered content type, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestContentRouterNoCompressor
// ---------------------------------------------------------------------------

func TestContentRouterNoCompressor(t *testing.T) {
	r := NewContentRouter() // empty registry
	ctx := context.Background()

	content := []byte(`{"key":"value"}`)
	opts := CompressOptions{MaxTokens: 100}

	_, err := r.Route(ctx, content, opts)
	if err == nil {
		t.Fatal("Route no compressor: expected error for empty router, got nil")
	}
}

// ---------------------------------------------------------------------------
// TestDetectContentType
// ---------------------------------------------------------------------------

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name     string
		content  []byte
		expected string
	}{
		{"json object", []byte(`{"a":1}`), "application/json"},
		{"json array", []byte(`[1,2,3]`), "application/json"},
		{"code block", []byte("```\nfunc x() {}\n```\n"), "text/code"},
		{"code def", []byte("def hello():\n    pass\n"), "text/code"},
		{"plain text", []byte("hello world"), "text/plain"},
		{"empty", []byte(""), "text/plain"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectContentType(tt.content)
			if got != tt.expected {
				t.Errorf("DetectContentType(%q) = %q, want %q", tt.content, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BenchmarkContentRouter
// ---------------------------------------------------------------------------

func BenchmarkContentRouter(b *testing.B) {
	r := newTestRouter()
	ctx := context.Background()
	content := []byte(`{"status":"ok","data":["a","b","c","d","e","f","g","h","i","j"]}`)
	opts := CompressOptions{MaxTokens: 100}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.Route(ctx, content, opts)
	}
}
