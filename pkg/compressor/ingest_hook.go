package compressor

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// IngestHook is the compression hook that wraps a ContentRouter.  It is
// injected into seahorse's Ingest() pipeline at the tool-output processing
// point (short_engine.go:~234), compressing oversized tool results before they
// are written to the SQLite store.
//
// Architecture (ADR-002): Hooks are zero-intrusion — if the hook is nil or
// fails, the original content passes through unchanged.
type IngestHook struct {
	router *ContentRouter
}

// NewIngestHook creates an IngestHook backed by the given ContentRouter.
func NewIngestHook(router *ContentRouter) *IngestHook {
	return &IngestHook{router: router}
}

// Process detects the content type, routes to the appropriate compressor, and
// returns the CompressResult.  On any error (unknown type, compressor failure)
// it returns the error — the caller (seahorse Ingest) is responsible for
// falling back to the original content.
func (h *IngestHook) Process(ctx context.Context, content []byte) (*CompressResult, error) {
	if h.router == nil {
		return nil, fmt.Errorf("ingest_hook: router is nil")
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("ingest_hook: empty content")
	}

	// Prepare CCR metadata on the side (not blocking compression)
	hash := hashContent(content)

	result, err := h.router.Route(ctx, content, CompressOptions{})
	if err != nil {
		return nil, err
	}

	// Annotate result with hash for CCR tracking
	if result.Meta == nil {
		result.Meta = make(map[string]interface{})
	}
	result.Meta["hash"] = hash

	return result, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// hashContent returns a short hex digest (first 16 chars of SHA256) suitable
// for use as a CCR lookup key.
func hashContent(content []byte) string {
	h := sha256.Sum256(content)
	return fmt.Sprintf("%x", h[:8])
}
