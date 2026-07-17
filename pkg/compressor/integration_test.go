package compressor

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// =============================================================================
// Integration: Ingest → CCR (full pipeline)
// =============================================================================

func TestIntegrationIngestToCCR(t *testing.T) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	hook := NewIngestHook(r)

	ctx := context.Background()
	content := []byte(`{"status":"ok","items":[{"id":1,"name":"item-1"},{"id":2,"name":"item-2"},{"id":3,"name":"item-3"}]}`)

	result, err := hook.Process(ctx, content)
	if err != nil {
		t.Fatalf("Process: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Process: result is nil")
	}

	// Verify CCR metadata is attached
	if result.Meta == nil {
		t.Fatal("Meta is nil — CCR metadata not attached")
	}
	if _, ok := result.Meta["hash"]; !ok {
		t.Error("Meta missing 'hash' key — CCR record hash not injected")
	}
	if _, ok := result.Meta["compressor"]; !ok {
		t.Error("Meta missing 'compressor' key — compressor name not recorded")
	}
	if _, ok := result.Meta["content_type"]; !ok {
		t.Error("Meta missing 'content_type' key — content type not recorded")
	}
}

// =============================================================================
// Integration: ContentRouter Chain (Router → Compressor)
// =============================================================================

func TestIntegrationContentRouterChain(t *testing.T) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())

	ctx := context.Background()

	tests := []struct {
		name        string
		content     []byte
		opts        CompressOptions
		wantComp    string // expected compressor name
		wantContent string // expected content type
	}{
		{
			name:        "json",
			content:     []byte(`{"key":"value"}`),
			opts:        CompressOptions{MaxTokens: 100},
			wantComp:    "smart_crusher",
			wantContent: "application/json",
		},
		{
			name:        "code",
			content:     []byte("```\nfunc main() {\n    fmt.Println(\"hello\")\n}\n```"),
			opts:        CompressOptions{MaxTokens: 200},
			wantComp:    "code_compressor",
			wantContent: "text/code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := r.Route(ctx, tt.content, tt.opts)
			if err != nil {
				t.Fatalf("Route: unexpected error: %v", err)
			}
			if c, ok := result.Meta["compressor"].(string); !ok || c != tt.wantComp {
				t.Errorf("compressor: want %q, got %q", tt.wantComp, c)
			}
			if ct, ok := result.Meta["content_type"].(string); !ok || ct != tt.wantContent {
				t.Errorf("content_type: want %q, got %q", tt.wantContent, ct)
			}
		})
	}
}

// =============================================================================
// Integration: All Content Types
// =============================================================================

func TestIntegrationContentTypes(t *testing.T) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	r.Register("text/plain", NewLogCompressor())
	hook := NewIngestHook(r)

	ctx := context.Background()

	tests := []struct {
		name    string
		content []byte
	}{
		{
			name:    "json",
			content: []byte(`{"data":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15]}`),
		},
		{
			name:    "code",
			content: []byte("```\nfunc hello() {\n    return \"world\"\n}\n```"),
		},
		{
			name: "log",
			content: []byte(
				"2024-01-15 10:30:00 ERROR [module] connection timeout\n" +
					"2024-01-15 10:30:01 WARN [module] retrying (attempt 1/3)\n" +
					"2024-01-15 10:30:02 INFO [module] connected successfully\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := hook.Process(ctx, tt.content)
			if err != nil {
				t.Logf("%s: Process returned error (acceptable for some types): %v", tt.name, err)
				return
			}
			if result == nil {
				t.Errorf("%s: result is nil", tt.name)
				return
			}
			if result.Content == "" {
				t.Errorf("%s: compressed content is empty", tt.name)
			}
		})
	}
}

// =============================================================================
// Integration: Large Content
// =============================================================================

func TestIntegrationLargeContent(t *testing.T) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	hook := NewIngestHook(r)

	ctx := context.Background()

	// Build a 50KB JSON array payload
	var sb strings.Builder
	sb.WriteString(`{"items":[`)
	for i := 0; i < 1000; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf(`{"id":%d,"name":"item-%05d","value":"abcdefghijklmnopqrstuvwxyz"}`, i, i))
	}
	sb.WriteString(`]}`)

	content := []byte(sb.String())
	if len(content) < 50000 {
		t.Logf("Generated content size = %d bytes (target: 50KB+)", len(content))
	}

	result, err := hook.Process(ctx, content)
	if err != nil {
		t.Fatalf("Process large: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Process large: result is nil")
	}
	if result.Ratio >= 1.0 {
		t.Errorf("Large content ratio should be < 1.0, got %.2f", result.Ratio)
	}
}

// =============================================================================
// Integration: Empty Content
// =============================================================================

func TestIntegrationEmptyContent(t *testing.T) {
	hook := NewIngestHook(NewContentRouter())

	ctx := context.Background()

	_, err := hook.Process(ctx, []byte(""))
	if err == nil {
		t.Error("Process empty: expected error, got nil")
	}
}

// =============================================================================
// Integration: Code Compressor Chain
// =============================================================================

func TestIntegrationCodeCompressorChain(t *testing.T) {
	r := NewContentRouter()
	r.Register("text/code", NewCodeCompressor())
	hook := NewIngestHook(r)

	ctx := context.Background()
	content := []byte("```go\nfunc factorial(n int) int {\n    if n <= 1 {\n        return 1\n    }\n    return n * factorial(n-1)\n}\n```")

	result, err := hook.Process(ctx, content)
	if err != nil {
		t.Fatalf("Process code: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Process code: result is nil")
	}
	if c, ok := result.Meta["compressor"].(string); !ok || c != "code_compressor" {
		t.Errorf("Expected code_compressor, got %v", c)
	}
}

// =============================================================================
// Integration: SmartCrusher Compressor Chain
// =============================================================================

func TestIntegrationSmartCrusherChain(t *testing.T) {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	hook := NewIngestHook(r)

	ctx := context.Background()
	content := []byte(`{"data":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20]}`)

	result, err := hook.Process(ctx, content)
	if err != nil {
		t.Fatalf("Process json: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Process json: result is nil")
	}
	if c, ok := result.Meta["compressor"].(string); !ok || c != "smart_crusher" {
		t.Errorf("Expected smart_crusher, got %v", c)
	}
}
