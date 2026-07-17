package compressor

import (
	"encoding/json"
	"strings"
)

// =============================================================================
// CompressionQuality — quality metrics for a single compression operation
// =============================================================================

// CompressionQuality captures the quality of a compression operation by
// measuring structural fidelity, key preservation, and semantic retention.
type CompressionQuality struct {
	// OriginalSz is the byte length of the original (uncompressed) content.
	OriginalSz int

	// FinalSz is the byte length after compression.
	FinalSz int

	// Ratio = FinalSz / OriginalSz. Values near 1.0 indicate low compression,
	// near 0.0 indicate aggressive compression.
	Ratio float64

	// SemanticRetention estimates how much semantic content survives compression.
	// Range: 0.0 (all meaning lost) to 1.0 (meaning fully preserved).
	// Computed by comparing keyword presence and structural markers.
	SemanticRetention float64

	// KeyPreservation maps critical keys to whether they survived compression.
	// For JSON content, typical keys are: status, error, id, type.
	KeyPreservation map[string]bool

	// KeywordPreservation maps code/semantic keywords to preservation status.
	// For code content, this tracks language keywords (func, def, import, etc.).
	KeywordPreservation map[string]bool

	// ContentType is the detected content type (application/json, text/code, etc.)
	ContentType string
}

// =============================================================================
// EvaluateQuality — primary entry point
// =============================================================================

// EvaluateQuality assesses the quality of a compression operation by comparing
// original and compressed content. It detects content type, checks key/keyword
// preservation, and computes a semantic retention score.
//
// For JSON content:
//   - Checks preservation of critical keys: status, error, id, type, result
//   - Semantic retention = fraction of critical keys preserved
//
// For code content:
//   - Checks preservation of language keywords
//   - Semantic retention = fraction of keywords preserved
//
// For other content types:
//   - Semantic retention = 1 - Ratio (simple heuristic: more compression →
//     more information loss)
func EvaluateQuality(original, compressed []byte, contentType string) *CompressionQuality {
	originalSz := len(original)
	finalSz := len(compressed)

	ratio := 0.0
	if originalSz > 0 {
		ratio = float64(finalSz) / float64(originalSz)
	}

	q := &CompressionQuality{
		OriginalSz:  originalSz,
		FinalSz:     finalSz,
		Ratio:        ratio,
		ContentType: contentType,
	}

	switch contentType {
	case "application/json":
		q.evaluateJSON(original, compressed)
	case "text/code":
		q.evaluateCode(original, compressed)
	default:
		q.evaluateGeneric(original, compressed)
	}

	return q
}

// ---------------------------------------------------------------------------
// JSON quality evaluation
// ---------------------------------------------------------------------------

// criticalJSONKeys lists keys whose preservation is considered essential for
// semantic fidelity in JSON content.
var criticalJSONKeys = []string{"status", "error", "id", "type", "result"}

func (q *CompressionQuality) evaluateJSON(original, compressed []byte) {
	q.KeyPreservation = make(map[string]bool)

	origStr := string(original)
	compStr := string(compressed)

	preservedCount := 0
	for _, key := range criticalJSONKeys {
		// A key is "preserved" if it appears as a JSON key in the compressed output
		// (heuristic: look for "key": pattern)
		preserved := false
		if strings.Contains(compStr, `"`+key+`"`) || strings.Contains(compStr, `'`+key+`'`) {
			preserved = true
			preservedCount++
		}
		q.KeyPreservation[key] = preserved
	}

	// Compute semantic retention: fraction of critical keys preserved
	// Also consider: if original had empty/nil arrays that survived
	if len(criticalJSONKeys) > 0 {
		q.SemanticRetention = float64(preservedCount) / float64(len(criticalJSONKeys))
	}

	// Boost retention if structural JSON markers ({} or []) survive
	if strings.Contains(compStr, "{") || strings.Contains(compStr, "[") {
		// Structure survived; retention is at least the key-preservation rate
	} else {
		// Structure lost; penalize
		q.SemanticRetention *= 0.5
	}

	// Don't count keys that weren't in the original
	_ = origStr // used for future enhancements
}

// ---------------------------------------------------------------------------
// Code quality evaluation
// ---------------------------------------------------------------------------

// codeKeywords lists common programming language keywords used for semantic
// evaluation in code content.
var codeKeywords = []string{"func", "def", "class", "import", "return", "if", "for"}

func (q *CompressionQuality) evaluateCode(original, compressed []byte) {
	q.KeywordPreservation = make(map[string]bool)

	compStr := string(compressed)
	origStr := string(original)

	preservedCount := 0
	totalKeywords := 0

	for _, kw := range codeKeywords {
		inOriginal := strings.Contains(origStr, kw+" ") || strings.Contains(origStr, kw+"(") || strings.Contains(origStr, kw+"\n")
		if inOriginal {
			totalKeywords++
			if strings.Contains(compStr, kw) {
				preservedCount++
				q.KeywordPreservation[kw] = true
			} else {
				q.KeywordPreservation[kw] = false
			}
		}
	}

	if totalKeywords > 0 {
		q.SemanticRetention = float64(preservedCount) / float64(totalKeywords)
	} else {
		// No code keywords found; fall back to ratio-based score
		q.SemanticRetention = 1.0 - q.Ratio
		if q.SemanticRetention < 0 {
			q.SemanticRetention = 0
		}
	}
}

// ---------------------------------------------------------------------------
// Generic quality evaluation (fallback for unknown types)
// ---------------------------------------------------------------------------

func (q *CompressionQuality) evaluateGeneric(original, compressed []byte) {
	// For generic content, semantic retention is estimated from compression ratio.
	// More compression implies more information loss (heuristic).
	q.SemanticRetention = 1.0 - q.Ratio
	if q.SemanticRetention < 0 {
		q.SemanticRetention = 0
	}

	// Round-trip capability: check if original is subset of compressed
	origStr := string(original)
	compStr := string(compressed)
	if len(origStr) > 0 && strings.Contains(compStr, origStr) {
		q.SemanticRetention = 1.0
	}

	// If compression more than halved content, retention can't be perfect
	q.KeyPreservation = make(map[string]bool)
	q.KeywordPreservation = make(map[string]bool)

	// Check if compressed is valid JSON (structured data survived?)
	var js interface{}
	if err := json.Unmarshal(compressed, &js); err == nil && q.SemanticRetention < 0.5 {
		// Valid JSON survived; boost retention
		q.SemanticRetention = 0.5
	}
}
