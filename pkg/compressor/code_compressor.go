package compressor

import (
	"context"
	"fmt"
	"strings"
)

// =============================================================================
// CodeCompressor — code-content compression preserving structure
// =============================================================================
//
// CodeCompressor implements Compressor with CompressTypeTruncate. It processes
// code line-by-line:
//   - Code block markers (```) are always preserved.
//   - Package/import declarations and function signatures are preserved.
//   - Long lines (>200 chars) are truncated with a marker.
//   - Function bodies with 3+ lines are compressed to a placeholder comment.
//   - Respects opts.Priority: "low" uses shorter line limit and more aggressive
//     body compression.
//
// The result is valid but shortened code that still conveys the structure.

const (
	codeCompressorName     = "code_compressor"
	maxCodeLineLen         = 200 // lines longer than this get truncated
	minBodyLinesToCompress = 3   // only compress function bodies with 3+ lines
)

// CodeCompressor is a code-aware structural compressor.
type CodeCompressor struct{}

// NewCodeCompressor creates a new CodeCompressor instance.
func NewCodeCompressor() *CodeCompressor {
	return &CodeCompressor{}
}

// Name returns the compressor's unique name.
func (c *CodeCompressor) Name() string {
	return codeCompressorName
}

// Type returns CompressTypeTruncate.
func (c *CodeCompressor) Type() CompressType {
	return CompressTypeTruncate
}

// Decompress is a pass-through for lossy structural compression. CodeCompressor
// truncation is irreversible, so the compressed content is returned as-is.
func (c *CodeCompressor) Decompress(ctx context.Context, compressed []byte) ([]byte, error) {
	return compressed, nil
}

// Compress processes code content and applies structural compression.
// The opts parameter drives line-length limits and body-compression thresholds.
func (c *CodeCompressor) Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
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
				"algorithm":     "code_compressor",
				"compress_type": "truncate",
				"original_sz":   originalSz,
				"final_sz":      0,
				"empty_input":   true,
			},
		}, nil
	}

	// Split into lines and process
	lines := strings.Split(text, "\n")
	compressed := c.compressLines(lines, opts)
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
			"algorithm":      "code_compressor",
			"compress_type":  "truncate",
			"original_sz":    originalSz,
			"final_sz":       finalSz,
			"lines_original": len(lines),
			"lines_final":    len(compressed),
		},
	}, nil
}

// compressLines applies code-aware compression line by line.
// The opts parameter controls line-length limits and compression thresholds.
func (c *CodeCompressor) compressLines(lines []string, opts CompressOptions) []string {
	out := make([]string, 0, len(lines))
	inFuncBody := false
	braceDepth := 0
	bodyLines := make([]string, 0) // buffered body lines

	// Adjust thresholds based on priority
	lineMax := maxCodeLineLen
	bodyMinLines := minBodyLinesToCompress
	if opts.Priority == "low" {
		lineMax = maxCodeLineLen / 2
		bodyMinLines = 2 // more aggressive body compression
	}

	flushBody := func() {
		if len(bodyLines) >= bodyMinLines {
			out = append(out, fmt.Sprintf("\t// ...(%d lines compressed)...", len(bodyLines)))
		} else {
			out = append(out, bodyLines...)
		}
		bodyLines = bodyLines[:0]
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Always preserve code block markers
		if strings.HasPrefix(trimmed, "```") {
			if inFuncBody {
				flushBody()
				inFuncBody = false
				braceDepth = 0
			}
			out = append(out, line)
			continue
		}

		// Truncate very long lines
		if len(line) > lineMax {
			line = line[:lineMax] + fmt.Sprintf("...(%d chars truncated)", len(line)-lineMax)
			if inFuncBody {
				bodyLines = append(bodyLines, line)
			} else {
				out = append(out, line)
			}
			continue
		}

		// Preserve package declarations
		if strings.HasPrefix(trimmed, "package ") {
			out = append(out, line)
			continue
		}

		// Preserve import declarations (single and grouped)
		if strings.HasPrefix(trimmed, "import ") || trimmed == "import" || trimmed == "import (" || trimmed == ")" ||
			(strings.HasPrefix(trimmed, "\"") && strings.HasSuffix(trimmed, "\"")) {
			if !inFuncBody {
				out = append(out, line)
				continue
			}
		}

		// Detect function signatures
		isFuncSig := strings.Contains(trimmed, "func ") || strings.HasPrefix(trimmed, "func ")

		// Track brace depth
		openCount := strings.Count(line, "{")
		closeCount := strings.Count(line, "}")
		braceDepth += openCount - closeCount

		if isFuncSig && !inFuncBody {
			out = append(out, line)
			if openCount > 0 && closeCount == 0 {
				inFuncBody = true
			}
			continue
		}

		// Inside function body: buffer lines
		if inFuncBody {
			if braceDepth <= 0 {
				// End of function body
				flushBody()
				out = append(out, line)
				inFuncBody = false
				braceDepth = 0
			} else {
				bodyLines = append(bodyLines, line)
			}
			continue
		}

		// Regular lines pass through
		out = append(out, line)
	}

	// If still in function body at EOF
	if inFuncBody && len(bodyLines) > 0 {
		flushBody()
	}

	return out
}

// Ensure CodeCompressor implements Compressor.
var _ Compressor = (*CodeCompressor)(nil)
