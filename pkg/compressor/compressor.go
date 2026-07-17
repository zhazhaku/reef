// Package compressor provides a pluggable content compression framework for
// reducing LLM context-window pressure. Compressors are registered by name and
// called through hook injection points in the seahorse summarization pipeline.
//
// Architecture (ADR-001, ADR-002):
//   - All compressors implement the Compressor interface.
//   - The registry is package‑level and goroutine‑safe.
//   - Compressors are selected by CompressType and wired via hooks.
package compressor

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Version is the current semantic version of the compressor package.
const Version = "v1.0.0"

// =============================================================================
// CompressType — classification of compression strategies
// =============================================================================

// CompressType indicates the compression approach a Compressor uses.
type CompressType int

const (
	// CompressTypeUnknown is the zero‑value; should not appear in production.
	CompressTypeUnknown CompressType = 0

	// CompressTypeTruncate removes content structurally (e.g. truncating arrays,
	// collapsing repeated lines, dropping low‑value fields).
	CompressTypeTruncate CompressType = 1

	// CompressTypeSummarize rewrites content via an LLM summarisation call.
	CompressTypeSummarize CompressType = 2

	// CompressTypeDrop discards content entirely when it is safe to do so
	// (e.g. after the content has been committed to the CCR store).
	CompressTypeDrop CompressType = 3
)

// =============================================================================
// CompressOptions — tuning knobs passed to every Compress call
// =============================================================================

// CompressOptions carries caller‑hints and hard constraints for compression.
type CompressOptions struct {
	// MaxTokens is the hard ceiling on the output token count. A compressor
	// should never return content that tokenises to more than MaxTokens.
	MaxTokens int

	// Priority hints at the importance of the content. Valid values:
	//   "high"   — preserve as much fidelity as possible
	//   "normal" — balanced
	//   "low"    — aggressive compression is acceptable
	Priority string

	// PreserveKeys is a list of JSON object keys that must survive compression.
	// Compressors may honour this on a best‑effort basis.
	PreserveKeys []string
}

// =============================================================================
// CompressResult — the outcome of a single Compress call
// =============================================================================

// CompressResult reports the compressed content and compression metrics.
type CompressResult struct {
	// Content is the compressed payload as a UTF‑8 string.
	Content string

	// OriginalSz is the byte length of the input before compression.
	OriginalSz int

	// FinalSz is the byte length of Content after compression.
	FinalSz int

	// Ratio = FinalSz / OriginalSz (1.0 = no change, 0.0 = empty output).
	Ratio float64

	// Meta carries compressor‑specific diagnostics (algorithm used, language
	// detected, number of items truncated, etc.).
	Meta map[string]interface{}
}

// =============================================================================
// Compressor — the interface every compressor must implement
// =============================================================================

// Compressor is the single interface that all compression implementations
// satisfy. It is intentionally small so that concrete compressors can be
// registered, called generically, and tested in isolation.
type Compressor interface {
	// Compress transforms content according to opts and returns the result.
	Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error)

	// Decompress reverses a previous compression. For lossless compressors
	// (Zstd-based CCR) this fully restores the original. For structural
	// compressors this is a best-effort pass-through.
	Decompress(ctx context.Context, compressed []byte) ([]byte, error)

	// Name returns the unique, human‑readable name of this compressor.
	// Duplicate names are rejected by Register.
	Name() string

	// Type classifies this compressor's strategy (truncate, summarise, drop).
	Type() CompressType
}

// =============================================================================
// Registry — package‑level, goroutine‑safe compressor store
// =============================================================================

var (
	reg = &registry{
		items: make(map[string]Compressor),
	}
)

type registry struct {
	mu    sync.RWMutex
	items map[string]Compressor
}

// Register stores c under name. Panics if name is empty, c is nil, or name has
// already been registered.
func Register(name string, c Compressor) {
	if name == "" {
		panic("compressor.Register: name must not be empty")
	}
	if c == nil {
		panic("compressor.Register: compressor must not be nil")
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()

	if _, exists := reg.items[name]; exists {
		panic(fmt.Sprintf("compressor.Register: %q already registered", name))
	}
	reg.items[name] = c
}

// Get returns the compressor registered under name, or nil if none is found.
func Get(name string) Compressor {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return reg.items[name]
}

// MustGet is like Get but panics when name has no registered compressor.
func MustGet(name string) Compressor {
	c := Get(name)
	if c == nil {
		panic(fmt.Sprintf("compressor.MustGet: %q not registered", name))
	}
	return c
}

// Names returns a sorted slice of every registered compressor name.
func Names() []string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	names := make([]string, 0, len(reg.items))
	for n := range reg.items {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// =============================================================================
// Metrics injection — global MetricsCollector for monitoring
// =============================================================================

var globalMetrics *MetricsCollector

// SetMetricsCollector injects a MetricsCollector into the compressor framework.
// All subsequent Compress() calls will record latency, ratio, errors, and
// rejections. Pass nil to disable monitoring.
func SetMetricsCollector(mc *MetricsCollector) {
	reg.mu.Lock()
	globalMetrics = mc
	if mc != nil {
		mc.registeredCompressors = len(reg.items)
	}
	reg.mu.Unlock()
}

// GetMetricsCollector returns the current global MetricsCollector, or nil if
// monitoring is not enabled.
func GetMetricsCollector() *MetricsCollector {
	reg.mu.RLock()
	defer reg.mu.RUnlock()
	return globalMetrics
}

// CollectorMetrics returns a point-in-time snapshot of global compressor metrics.
// Returns zero values when no MetricsCollector has been injected.
func CollectorMetrics() *Metrics {
	mc := GetMetricsCollector()
	if mc == nil {
		return &Metrics{}
	}
	return mc.Snapshot()
}
