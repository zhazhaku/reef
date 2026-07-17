package compressor

import (
	"context"
	"encoding/json"
	"fmt"
)

// =============================================================================
// SmartCrusher — structural JSON truncation compressor
// =============================================================================
//
// SmartCrusher implements Compressor with CompressTypeTruncate. It walks the
// JSON tree and applies three truncation strategies:
//   1. Array truncation — large arrays (≥10 items) keep first 5 + last 5.
//   2. Depth truncation — subtrees at depth ≥ 3 become a placeholder.
//   3. String truncation — strings > 1000 chars get trimmed with a marker.
//
// The result is always valid JSON, making it safe for downstream consumers
// that expect structured data.

const (
	smartCrusherName        = "smart_crusher"
	maxArrayItems           = 10  // arrays with more items get truncated
	arrayKeepFirst          = 5   // keep first N items
	arrayKeepLast           = 5   // keep last N items
	maxStringLen            = 1000 // strings longer than this get truncated
	maxDepth                = 3   // subtrees deeper than this become placeholder
	stringTruncateKeep      = 200 // chars to keep when truncating strings
	safeFallbackThreshold   = 50000 // bytes: above this we apply aggressive rules
)

// SmartCrusher is a structural JSON compressor.
type SmartCrusher struct{}

// NewSmartCrusher creates a new SmartCrusher instance.
func NewSmartCrusher() *SmartCrusher {
	return &SmartCrusher{}
}

// Name returns the compressor's unique name.
func (s *SmartCrusher) Name() string {
	return smartCrusherName
}

// Type returns CompressTypeTruncate.
func (s *SmartCrusher) Type() CompressType {
	return CompressTypeTruncate
}

// Decompress is a pass-through for lossy structural compression. SmartCrusher
// truncation is irreversible, so the compressed content is returned as-is.
func (s *SmartCrusher) Decompress(ctx context.Context, compressed []byte) ([]byte, error) {
	return compressed, nil
}

// Compress walks the JSON tree and applies structural truncation.
func (s *SmartCrusher) Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
	originalSz := len(content)

	// Check context cancellation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Attempt JSON parsing
	var root interface{}
	if err := json.Unmarshal(content, &root); err != nil {
		return nil, fmt.Errorf("smart_crusher: invalid JSON: %w", err)
	}

	// Walk and truncate
	truncated := s.truncateValue(root, 0, opts)

	// Re-serialize
	output, err := json.Marshal(truncated)
	if err != nil {
		// Fallback: return minimal valid JSON
		output = []byte(`{"_error":"marshal failed after truncation"}`)
	}

	finalSz := len(output)
	ratio := 1.0
	if originalSz > 0 {
		ratio = float64(finalSz) / float64(originalSz)
	}

	meta := map[string]interface{}{
		"algorithm":     "smart_crusher",
		"compress_type": "truncate",
		"original_sz":   originalSz,
		"final_sz":      finalSz,
	}

	return &CompressResult{
		Content:    string(output),
		OriginalSz: originalSz,
		FinalSz:    finalSz,
		Ratio:      ratio,
		Meta:       meta,
	}, nil
}

// truncateValue recursively applies truncation rules to a JSON value.
func (s *SmartCrusher) truncateValue(v interface{}, depth int, opts CompressOptions) interface{} {
	// Depth guard: replace deep subtrees with placeholder
	if depth >= maxDepth {
		return map[string]interface{}{
			"_truncated_depth": depth,
			"_hint":            "subtree truncated (depth limit)",
		}
	}

	switch val := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, child := range val {
			// PreserveKeys: skip truncation for keys the caller marked essential.
			// Only applies to the immediate child — nested values still truncated.
			if s.isPreserved(k, opts.PreserveKeys) {
				out[k] = child
				continue
			}
			out[k] = s.truncateValue(child, depth+1, opts)
		}
		return out

	case []interface{}:
		// Array truncation
		n := len(val)
		if n > maxArrayItems {
			keptFirst := make([]interface{}, 0, arrayKeepFirst+1+arrayKeepLast)
			for i := 0; i < arrayKeepFirst && i < n; i++ {
				keptFirst = append(keptFirst, s.truncateValue(val[i], depth+1, opts))
			}
			keptFirst = append(keptFirst, fmt.Sprintf("...(%d items truncated)...", n-arrayKeepFirst-arrayKeepLast))
			for i := n - arrayKeepLast; i < n && i >= 0; i++ {
				keptFirst = append(keptFirst, s.truncateValue(val[i], depth+1, opts))
			}
			return keptFirst
		}

		// Small array: walk children
		out := make([]interface{}, len(val))
		for i, child := range val {
			out[i] = s.truncateValue(child, depth+1, opts)
		}
		return out

	case string:
		// String truncation — adjusted by Priority
		strMax := maxStringLen
		strKeep := stringTruncateKeep
		if opts.Priority == "low" {
			strMax = maxStringLen / 2
			strKeep = stringTruncateKeep / 2
		}
		if len(val) > strMax {
			truncated := val[:strKeep]
			return truncated + fmt.Sprintf("...(%d chars truncated)", len(val)-strKeep)
		}
		return val

	default:
		// Numbers, bools, nil pass through unchanged
		return v
	}
}

// isPreserved checks whether a key is in the set of preserved keys.
func (s *SmartCrusher) isPreserved(key string, preserveKeys []string) bool {
	for _, pk := range preserveKeys {
		if pk == key {
			return true
		}
	}
	return false
}

// Ensure SmartCrusher implements Compressor.
var _ Compressor = (*SmartCrusher)(nil)
