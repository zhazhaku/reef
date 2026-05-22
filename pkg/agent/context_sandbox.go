package agent

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// MaxToolResultChars is the maximum number of characters retained from a tool
// result before truncation. Long tool results (e.g. file reads, command output)
// contribute to context bloat and dilute the effective conversation.
// 4096 chars ≈ 1000 tokens, sufficient for most tool results.
const MaxToolResultChars = 4096

// sandboxDir returns the directory for storing sandboxed full outputs.
func sandboxDir() string {
	// Use the .reef workspace root for sandbox storage
	dir := filepath.Join(".", ".sandbox")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// TruncateToolResult truncates a tool result using the tool-specific limit,
// falling back to MaxToolResultChars when no limit is configured for the tool.
// When truncation occurs, the full output is stored in a sandbox file and a
// recovery note is appended to the truncated content.
// The LLM can recover the full output with read_file on the sandbox path.
func TruncateToolResult(toolName, content string) string {
	// Check per-tool limits first.
	if limit, ok := toolTruncateLimits[toolName]; ok {
		if limit <= 0 || len(content) <= limit {
			return content
		}
		return truncateToolResult(toolName, content)
	}

	// No per-tool limit: use default MaxToolResultChars.
	if len(content) <= MaxToolResultChars {
		return content
	}

	// Store full output in sandbox for recovery
	hash := sha256.Sum256([]byte(content))
	hashHex := fmt.Sprintf("%x", hash[:8])
	sandboxFile := filepath.Join(sandboxDir(), "tool_output_"+hashHex+".txt")
	_ = os.WriteFile(sandboxFile, []byte(content), 0644)

	return content[:MaxToolResultChars] + fmt.Sprintf(
		"\n... [truncated: %d -> %d chars, %d chars omitted. "+
			"Full output stored at %s. Use read_file to recover.]",
		len(content), MaxToolResultChars, len(content)-MaxToolResultChars,
		sandboxFile,
	)
}
