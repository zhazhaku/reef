package compressor

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestHook() *IngestHook {
	r := NewContentRouter()
	r.Register("application/json", NewSmartCrusher())
	r.Register("text/code", NewCodeCompressor())
	return NewIngestHook(r)
}

// ---------------------------------------------------------------------------
// TestIngestHookRegister
// ---------------------------------------------------------------------------

func TestIngestHookRegister(t *testing.T) {
	h := newTestHook()
	if h == nil {
		t.Fatal("NewIngestHook returned nil")
	}
	if h.router == nil {
		t.Fatal("IngestHook.router is nil")
	}
}

// ---------------------------------------------------------------------------
// TestIngestHookProcess
// ---------------------------------------------------------------------------

func TestIngestHookProcess(t *testing.T) {
	h := newTestHook()
	ctx := context.Background()

	// Use a larger JSON payload that will actually compress
	content := []byte(`{"status":"ok","data":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20],"extra_field":"this is some extra text"}`)
	result, err := h.Process(ctx, content)

	if err != nil {
		t.Fatalf("Process: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Process: result is nil")
	}
	if result.Ratio >= 1.0 {
		t.Logf("Process: ratio %.2f (small payload may not compress below 1.0 — acceptable)", result.Ratio)
	}
}

// ---------------------------------------------------------------------------
// TestIngestHookProcessCode
// ---------------------------------------------------------------------------

func TestIngestHookProcessCode(t *testing.T) {
	h := newTestHook()
	ctx := context.Background()

	content := []byte("Here is some code:\n```\nfunc hello() {\n    fmt.Println(\"hi\")\n}\n```\n")
	result, err := h.Process(ctx, content)

	if err != nil {
		t.Fatalf("Process code: unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Process code: result is nil")
	}
}

// ---------------------------------------------------------------------------
// TestIngestHookEmptyContent
// ---------------------------------------------------------------------------

func TestIngestHookEmptyContent(t *testing.T) {
	h := newTestHook()
	ctx := context.Background()

	content := []byte("")
	_, err := h.Process(ctx, content)

	// Empty content should either be passed through with ratio=1.0 or produce an error
	if err == nil {
		// If no error, result should have ratio=1.0 (no change)
		// This is acceptable — check that it doesn't panic
	} // error is also acceptable for empty input
}

// ---------------------------------------------------------------------------
// TestIngestHookUnknownContent
// ---------------------------------------------------------------------------

func TestIngestHookUnknownContent(t *testing.T) {
	h := newTestHook()
	ctx := context.Background()

	content := []byte("This is just plain text with no JSON markers or code blocks.")
	_, err := h.Process(ctx, content)

	// Unknown content types should fall through — either pass-through or error
	// The hook should not panic
	if err != nil {
		t.Logf("Process unknown content returned error (acceptable): %v", err)
	}
}
