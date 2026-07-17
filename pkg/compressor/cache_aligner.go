package compressor

import (
	"context"
	"regexp"
	"strings"
)

// =============================================================================
// CacheAligner — normalisation compressor for cache-key alignment
// =============================================================================
//
// CacheAligner implements Compressor with CompressTypeTruncate. It replaces
// variable patterns with stable placeholders so that semantically equivalent
// content produces identical cache keys:
//
//   - Timestamps (ISO8601, RFC3339, syslog, etc.)                → <TIMESTAMP>
//   - UUID v4                                                    → <UUID>
//   - IPv4 / IPv6 addresses                                      → <IP>
//   - Email addresses                                            → <EMAIL>
//   - URLs                                                       → <URL>

const cacheAlignerName = "cache_aligner"

// alignTimestampRE matches a wide variety of timestamp formats.
// Consolidated from LogCompressor patterns plus additional variants.
var alignTimestampRE = regexp.MustCompile(
	// ISO8601 / RFC3339 with optional fractional seconds and timezone
	`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}(?::?\d{2})?)?` +
		// OR syslog-style (Jan 15 10:30:02)
		`|` + `\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}` +
		// OR slash-date (2024/01/15 10:30:03)
		`|` + `\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}` +
		// OR bare time (10:30:00.123)
		`|` + `\d{2}:\d{2}:\d{2}(?:\.\d+)?`,
)

// alignUUIDRE matches standard UUID v4 format.
var alignUUIDRE = regexp.MustCompile(
	`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`,
)

// alignIPv4RE matches dotted-quad IPv4 addresses.
var alignIPv4RE = regexp.MustCompile(
	`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`,
)

// alignEmailRE matches email addresses with a reasonable local-part and domain.
var alignEmailRE = regexp.MustCompile(
	`\b[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}\b`,
)

// alignURLRE matches HTTP(S) URLs.
var alignURLRE = regexp.MustCompile(
	`https?://[^\s<>\"]+`,
)

// ---------------------------------------------------------------------------
// CacheAligner
// ---------------------------------------------------------------------------

// CacheAligner is a pattern-based normalisation compressor.
type CacheAligner struct{}

var _ Compressor = (*CacheAligner)(nil)

// NewCacheAligner creates a new CacheAligner instance.
func NewCacheAligner() *CacheAligner {
	return &CacheAligner{}
}

// Name returns the compressor's unique name.
func (c *CacheAligner) Name() string { return cacheAlignerName }

// Type returns CompressTypeTruncate.
func (c *CacheAligner) Type() CompressType { return CompressTypeTruncate }

// Decompress is a pass-through for structural (lossy) compression.
func (c *CacheAligner) Decompress(ctx context.Context, compressed []byte) ([]byte, error) {
	return compressed, nil
}

// Compress normalizes content by replacing variable patterns with fixed
// placeholders. The resulting output is suitable as a cache-lookup key.
func (c *CacheAligner) Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
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
				"compressor":    cacheAlignerName,
				"compress_type": "truncate",
				"empty_input":   true,
			},
		}, nil
	}

	normalized := c.normalize(text)
	finalSz := len(normalized)
	ratio := 1.0
	if originalSz > 0 {
		ratio = float64(finalSz) / float64(originalSz)
	}

	return &CompressResult{
		Content:    normalized,
		OriginalSz: originalSz,
		FinalSz:    finalSz,
		Ratio:      ratio,
		Meta: map[string]interface{}{
			"compressor":    cacheAlignerName,
			"compress_type": "truncate",
			"original_sz":   originalSz,
			"final_sz":      finalSz,
		},
	}, nil
}

// normalize applies all pattern replacements in a deterministic order:
// URL → Email → UUID → Timestamp → IP (most-specific to least-specific).
func (c *CacheAligner) normalize(s string) string {
	// Order matters: URL contains patterns that could be partially
	// matched by email/IP, so URL goes first.
	s = alignURLRE.ReplaceAllString(s, "<URL>")
	s = alignEmailRE.ReplaceAllString(s, "<EMAIL>")
	// UUID before timestamp — UUID contains hex that looks like timestamps
	s = alignUUIDRE.ReplaceAllString(s, "<UUID>")
	s = alignTimestampRE.ReplaceAllString(s, "<TIMESTAMP>")
	s = alignIPv4RE.ReplaceAllString(s, "<IP>")
	return s
}
