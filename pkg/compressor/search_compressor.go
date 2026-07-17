package compressor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// =============================================================================
// SearchCompressor — search-result compression with field compaction
// =============================================================================
//
// SearchCompressor implements Compressor with CompressTypeTruncate. It processes
// search results by:
//   1. Parsing structured records (title/url/snippet are key-value lines)
//   2. Field cleanup — removing timestamp/rank_score/metadata
//   3. Result truncation — keeping first N when over limit
//   4. Snippet truncation — trimming to ~200 chars

const (
	searchCompressorName = "search_compressor"
	maxSearchResults     = 10  // soft cap on number of results
	maxSnippetLen        = 200 // chars to keep per snippet
)

// separatorRE matches search result separators.
var separatorRE = regexp.MustCompile(`^---\s*(Result-\d+|---)?\s*---$`)

// essentialFields lists fields that must be preserved.
var essentialFields = map[string]bool{
	"title":   true,
	"url":     true,
	"snippet": true,
}

// redundantFields lists fields that should be removed.
var redundantFields = map[string]bool{
	"timestamp":  true,
	"rank_score": true,
	"metadata":   true,
	"score":      true,
	"source":     true,
}

// SearchCompressor is a search-result aware compressor.
type SearchCompressor struct{}

var _ Compressor = (*SearchCompressor)(nil)

// NewSearchCompressor creates a new SearchCompressor instance.
func NewSearchCompressor() *SearchCompressor {
	return &SearchCompressor{}
}

// Name returns the compressor's unique name.
func (s *SearchCompressor) Name() string {
	return searchCompressorName
}

// Type returns CompressTypeTruncate.
func (s *SearchCompressor) Type() CompressType {
	return CompressTypeTruncate
}

// Decompress is a pass-through for structural (lossy) compression.
func (s *SearchCompressor) Decompress(ctx context.Context, compressed []byte) ([]byte, error) {
	return compressed, nil
}

// Compress processes search results and applies result-aware compression.
func (s *SearchCompressor) Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
	originalSz := len(content)

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	text := strings.TrimSpace(string(content))
	if text == "" {
		return &CompressResult{
			Content:    "",
			OriginalSz: originalSz,
			FinalSz:    0,
			Ratio:      0,
			Meta: map[string]interface{}{
				"compressor":    searchCompressorName,
				"compress_type": "truncate",
				"original_sz":   originalSz,
				"final_sz":      0,
				"empty_input":   true,
			},
		}, nil
	}

	// Parse into records
	records := s.parseRecords(text)

	// Determine max results based on MaxTokens (rough: 4 chars ≈ 1 token)
	maxResults := maxSearchResults
	if opts.MaxTokens > 0 && len(records) > 3 {
		tokenBudget := opts.MaxTokens / 4
		if tokenBudget < len(text) {
			ratio := float64(tokenBudget) / float64(len(text))
			maxResults = int(float64(len(records)) * ratio)
			if maxResults < 1 {
				maxResults = 1
			}
		}
	}

	// Compress records
	compressed := s.compressRecords(records, maxResults)
	output := strings.Join(compressed, "\n")

	finalSz := len(output)
	ratio := 1.0
	if originalSz > 0 {
		ratio = float64(finalSz) / float64(originalSz)
	}

	return &CompressResult{
		Content:    output,
		OriginalSz: originalSz,
		FinalSz:    finalSz,
		Ratio:      ratio,
		Meta: map[string]interface{}{
			"compressor":     searchCompressorName,
			"compress_type":  "truncate",
			"original_sz":    originalSz,
			"final_sz":       finalSz,
			"results_orig":   len(records),
			"results_final":  len(compressed) - 1, // exclude truncation marker
		},
	}, nil
}

// searchRecord wraps a parsed search result with its label.
type searchRecord struct {
	label   string // "Result-1" or "" if unnamed
	content []string
}

// parseRecords splits text into individual result records.
func (s *SearchCompressor) parseRecords(text string) []searchRecord {
	var records []searchRecord
	var current []string
	var currentLabel string

	lines := strings.Split(text, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Separator triggers a new record
		if separatorRE.MatchString(trimmed) {
			if len(current) > 0 {
				records = append(records, searchRecord{label: currentLabel, content: current})
				current = nil
				currentLabel = ""
			}
			// Extract label from separator
			currentLabel = s.extractLabel(trimmed)
			continue
		}

		// Empty line between records
		if trimmed == "" {
			if len(current) > 0 {
				records = append(records, searchRecord{label: currentLabel, content: current})
				current = nil
				currentLabel = ""
			}
			continue
		}

		current = append(current, line)
	}

	// Don't forget the last record
	if len(current) > 0 {
		records = append(records, searchRecord{label: currentLabel, content: current})
	}

	return records
}

// extractLabel pulls the result name from a separator line.
func (s *SearchCompressor) extractLabel(sep string) string {
	labelRE := regexp.MustCompile(`Result-\d+`)
	if m := labelRE.FindString(sep); m != "" {
		return m
	}
	return ""
}

// compressRecords applies field compaction and result truncation.
func (s *SearchCompressor) compressRecords(records []searchRecord, maxResults int) []string {
	if len(records) == 0 {
		return nil
	}

	out := make([]string, 0)
	kept := 0

	for i, rec := range records {
		if i >= maxResults {
			break
		}

		compacted := s.compactRecord(rec)
		if len(compacted) > 0 {
			if kept > 0 {
				out = append(out, "") // separator blank line
			}
			out = append(out, compacted...)
			kept++
		}
	}

	// Add truncation marker if results were dropped
	if len(records) > maxResults {
		out = append(out, fmt.Sprintf("...(%d more results truncated)...", len(records)-maxResults))
	}

	return out
}

// compactRecord keeps essential fields, removes redundant ones, and truncates snippets.
func (s *SearchCompressor) compactRecord(rec searchRecord) []string {
	out := make([]string, 0, 4)

	// Preserve result label if present
	if rec.label != "" {
		out = append(out, "--- "+rec.label+" ---")
	}

	for _, line := range rec.content {
		// Parse "key: value"
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			// Non-key-value line — keep as-is if short
			if utf8.RuneCountInString(line) <= maxSnippetLen {
				out = append(out, line)
			} else {
				out = append(out, line[:maxSnippetLen]+"...")
			}
			continue
		}

		key := strings.TrimSpace(strings.ToLower(line[:colonIdx]))
		value := strings.TrimSpace(line[colonIdx+1:])

		// Drop redundant fields
		if redundantFields[key] {
			continue
		}

		// Truncate long snippet values
		if (key == "snippet" || key == "content" || key == "body") && utf8.RuneCountInString(value) > maxSnippetLen {
			value = value[:maxSnippetLen] + "..."
		}

		out = append(out, key+": "+value)
	}

	return out
}
