package compressor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ContentRouter routes content to the appropriate Compressor based on detected
// content type. It maintains a registry mapping content-type strings to
// Compressor instances. An optional MetricsCollector may be attached to
// record compression metrics.
type ContentRouter struct {
	mu       sync.RWMutex
	registry map[string]Compressor
	metrics  *MetricsCollector
}

// NewContentRouter creates an empty ContentRouter.
func NewContentRouter() *ContentRouter {
	return &ContentRouter{
		registry: make(map[string]Compressor),
	}
}

// SetMetricsCollector attaches a MetricsCollector to the router. If mc is nil,
// metrics collection is disabled.
func (r *ContentRouter) SetMetricsCollector(mc *MetricsCollector) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.metrics = mc
}

// Register associates a Compressor with a content-type string (e.g.
// "application/json", "text/code").  Later registrations overwrite earlier
// ones for the same content type.
func (r *ContentRouter) Register(contentType string, c Compressor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.registry[contentType] = c
}

// Route detects the content type of content, selects the registered
// Compressor for that type, and calls its Compress method.  If no compressor
// is registered for the detected type, an error is returned.
// If a MetricsCollector is attached, latency and compression ratio are
// recorded automatically.
func (r *ContentRouter) Route(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
	contentType := DetectContentType(content)

	r.mu.RLock()
	c, ok := r.registry[contentType]
	mc := r.metrics
	r.mu.RUnlock()

	if !ok {
		if mc != nil {
			mc.RecordRejection()
		}
		return nil, fmt.Errorf("content_router: no compressor registered for content type %q", contentType)
	}

	start := time.Now()
	result, err := c.Compress(ctx, content, opts)
	elapsed := time.Since(start)

	if mc != nil {
		if err != nil {
			mc.RecordError()
		} else {
			mc.RecordSuccess(elapsed, len(content), len(result.Content))
		}
	}

	if err != nil {
		return nil, err
	}

	// Annotate the result with which compressor produced it
	if result.Meta == nil {
		result.Meta = make(map[string]interface{})
	}
	result.Meta["compressor"] = c.Name()
	result.Meta["content_type"] = contentType

	return result, nil
}

// ---------------------------------------------------------------------------
// DetectContentType — heuristic content-type classifier
// ---------------------------------------------------------------------------

// DetectContentType classifies content by examining its structure. It uses
// simple heuristics (no ML):
//
//  1. JSON:  starts with '{' or '[' after trimming whitespace
//  2. Code:  contains fenced code blocks (```) or common code keywords
//  3. Plain: everything else
func DetectContentType(content []byte) string {
	trimmed := strings.TrimSpace(string(content))

	if len(trimmed) == 0 {
		return "text/plain"
	}

	// JSON detection: starts with { or [
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return "application/json"
	}

	// Code detection: fenced code blocks or language keywords
	if strings.Contains(trimmed, "```") {
		return "text/code"
	}
	codeKeywords := []string{"func ", "def ", "class ", "import ", "package ", "fn ", "let ", "const "}
	for _, kw := range codeKeywords {
		if strings.Contains(trimmed, kw) {
			return "text/code"
		}
	}

	return "text/plain"
}
