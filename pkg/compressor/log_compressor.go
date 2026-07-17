package compressor

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// =============================================================================
// LogCompressor — log-content compression with timestamp normalisation
// =============================================================================
//
// LogCompressor implements Compressor with CompressTypeTruncate. It processes
// log output line-by-line:
//  1. Timestamp normalisation — multiple formats → <TIMESTAMP>
//  2. Log level preservation — ERROR/WARN/INFO/DEBUG/TRACE/FATAL kept
//  3. Stack trace compression — at.*\.go:\d+ → <stacktrace>
//  4. UUID/numeric-ID replacement → <UUID>
//  5. Duplicate line collapsing — consecutive identical lines merged
//  6. Line count truncation — keep first 50% + last 50% when over limit

const (
	logCompressorName     = "log_compressor"
	logMaxLines           = 200 // max lines before truncation kicks in
)

// timestampREs matches common log timestamp formats.
var timestampREs = []*regexp.Regexp{
	regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?`), // ISO8601
	regexp.MustCompile(`\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}(\.\d+)?`),                       // 2024-01-15 10:30:00
	regexp.MustCompile(`\w{3}\s+\d{1,2}\s+\d{2}:\d{2}:\d{2}`),                                   // Jan 15 10:30:02
	regexp.MustCompile(`\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}`),                                  // 2024/01/15 10:30:03
	regexp.MustCompile(`\d{2}:\d{2}:\d{2}(\.\d+)?`),                                              // 10:30:00.123
}

// stackRE matches stack-trace source file references.
var stackRE = regexp.MustCompile(`\s+at\s+.*\.\w+:\d+`)
var goroutineRE = regexp.MustCompile(`goroutine\s+\d+.*`)
var sourceFileRE = regexp.MustCompile(`^\t.*\.\w+:\d+(\s+.*)?$`)

// uuidRE matches UUID patterns.
var uuidRE = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// levelRE matches log level markers.
var levelRE = regexp.MustCompile(`\b(DEBUG|INFO|WARN(?:ING)?|ERROR|TRACE|FATAL)\b`)

// LogCompressor is a log-aware structural compressor.
type LogCompressor struct{}

var _ Compressor = (*LogCompressor)(nil)

// NewLogCompressor creates a new LogCompressor instance.
func NewLogCompressor() *LogCompressor {
	return &LogCompressor{}
}

// Name returns the compressor's unique name.
func (l *LogCompressor) Name() string {
	return logCompressorName
}

// Type returns CompressTypeTruncate.
func (l *LogCompressor) Type() CompressType {
	return CompressTypeTruncate
}

// Decompress is a pass-through for structural (lossy) compression.
func (l *LogCompressor) Decompress(ctx context.Context, compressed []byte) ([]byte, error) {
	return compressed, nil
}

// Compress processes log content and applies log-aware compression.
func (l *LogCompressor) Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
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
				"compressor":    logCompressorName,
				"compress_type": "truncate",
				"original_sz":   originalSz,
				"final_sz":      0,
				"empty_input":   true,
			},
		}, nil
	}

	lines := strings.Split(text, "\n")
	compressed := l.compressLines(lines, opts)

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
			"compressor":    logCompressorName,
			"compress_type": "truncate",
			"original_sz":   originalSz,
			"final_sz":      finalSz,
			"lines_orig":    len(lines),
			"lines_final":   len(compressed),
		},
	}, nil
}

// compressLines applies log-aware compression line by line.
func (l *LogCompressor) compressLines(lines []string, opts CompressOptions) []string {
	out := make([]string, 0, len(lines))
	var prevLine string
	repeated := 0
	inStack := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			// flush repeated before empty line
			if repeated > 0 {
				out = l.flushRepeated(out, prevLine, repeated)
				repeated = 0
			}
			out = append(out, line)
			inStack = false
			continue
		}

		// Stack trace detection
		if goroutineRE.MatchString(trimmed) || strings.HasPrefix(trimmed, "goroutine") {
			if repeated > 0 {
				out = l.flushRepeated(out, prevLine, repeated)
				repeated = 0
			}
			if !inStack {
				out = append(out, trimmed)
				inStack = true
			}
			continue
		}
		if inStack && (sourceFileRE.MatchString(trimmed) || strings.HasPrefix(trimmed, "\t")) {
			continue // compress stack frames
		}
		inStack = false

		// Apply transformations
		transformed := l.transformLine(line)

		// Deduplicate consecutive identical lines
		if transformed == prevLine {
			repeated++
			continue
		}
		if repeated > 0 {
			out = l.flushRepeated(out, prevLine, repeated)
			repeated = 0
		}

		out = append(out, transformed)
		prevLine = transformed
		_ = i
	}

	// Flush any remaining repeats
	if repeated > 0 {
		out = l.flushRepeated(out, prevLine, repeated)
	}

	// Truncate if over limit: keep first 50% and last 50%
	if len(out) > logMaxLines {
		keep := logMaxLines / 2
		truncated := make([]string, 0, logMaxLines+1)
		truncated = append(truncated, out[:keep]...)
		truncated = append(truncated, fmt.Sprintf("...(%d lines truncated)...", len(out)-logMaxLines))
		truncated = append(truncated, out[len(out)-keep:]...)
		return truncated
	}

	return out
}

// transformLine applies individual line transformations.
func (l *LogCompressor) transformLine(line string) string {
	s := line

	// Replace stack trace references
	if stackRE.MatchString(s) || sourceFileRE.MatchString(s) {
		return "<stacktrace>"
	}

	// Replace goroutine headers
	if goroutineRE.MatchString(s) {
		return "<stacktrace>"
	}

	// Normalize timestamps
	s = l.normalizeTimestamps(s)

	// Replace UUIDs
	s = uuidRE.ReplaceAllString(s, "<UUID>")

	return s
}

// normalizeTimestamps replaces all recognized timestamp formats with <TIMESTAMP>.
func (l *LogCompressor) normalizeTimestamps(s string) string {
	for _, re := range timestampREs {
		if re.MatchString(s) {
			s = re.ReplaceAllString(s, "<TIMESTAMP>")
		}
	}
	return s
}

// flushRepeated appends a repeated-line marker.
func (l *LogCompressor) flushRepeated(out []string, line string, count int) []string {
	if count > 0 {
		out = append(out, fmt.Sprintf("%s (repeated %d times)", line, count))
	}
	out = append(out, line)
	return out
}
