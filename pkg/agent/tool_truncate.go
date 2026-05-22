package agent

import (
	"fmt"
	"strings"

	"github.com/zhazhaku/reef/pkg/providers"
)

// toolTruncateLimits defines max character limits for tool result content
// that is sent to the LLM API. Tool results are stored in full in seahorse
// SQLite and JSONL for memory preservation; truncation only affects the
// API request payload to reduce token consumption.
var toolTruncateLimits = map[string]int{
	"web_fetch":  2000,
	"exec":       4096,
	"web_search": 0, // already summarized, no truncation needed
}

// fullPassTools are tools whose results should never be truncated.
// These either have built-in limits (read_file offset/length),
// are already summaries (short_grep), or are structured/metadata.
var fullPassTools = map[string]struct{}{
	"read_file":     {}, // user controls size via offset/length
	"short_grep":    {}, // already summarized search results
	"short_expand":  {}, // retrieving full content on demand
	"list_dir":      {}, // typically < 500 chars, structured
	"write_file":    {}, // confirmation message, small
	"edit_file":     {}, // confirmation message, small
	"append_file":   {}, // confirmation message, small
	"send_file":     {}, // path references, small
	"send_tts":      {}, // audio path, small
	"message":       {}, // confirmation, small
	"reaction":      {}, // confirmation, small
	"cron":          {}, // confirmation, small
	"reef_status":   {}, // structured status output
	"reef_submit_task": {}, // task submission result, small
}

const defaultTruncateLimit = 2000

// truncateToolResult returns a truncated version of the content for the given
// tool, or the original content if truncation is not needed (tool result is
// small enough, or tool is exempt from truncation).
//
// The truncation marker includes a short message telling the LLM that full
// content is available via short_expand.
func truncateToolResult(toolName string, content string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return content
	}

	// Full-pass tools are never truncated.
	if _, full := fullPassTools[toolName]; full {
		return content
	}

	// Get limit for this tool (0 = no limit).
	limit, ok := toolTruncateLimits[toolName]
	if !ok {
		limit = defaultTruncateLimit
	}

	if limit <= 0 || len(content) <= limit {
		return content
	}

	return content[:limit] + fmt.Sprintf(
		"\n\n[output truncated: %d→%d chars; use short_expand tool to recover full content]",
		len(content), limit,
	)
}

// truncateToolResultMessage applies tool result truncation to a single
// providers.Message. Returns the message unchanged if it's not a tool result
// or if truncation is not needed.
//
// The message MUST carry PromptSource or other metadata indicating which tool
// produced it, OR caller must pass the tool name explicitly via the
// ToolCallID→name mapping.
func truncateToolResultMessage(msg providers.Message, toolName string) providers.Message {
	if msg.Role != "tool" {
		return msg
	}
	msg.Content = truncateToolResult(toolName, msg.Content)
	return msg
}

// resolveToolName looks up the tool name for a given tool_call_id from the
// assistant message's ToolCalls list. Returns the tool name or empty string.
func resolveToolName(toolCalls []providers.ToolCall, toolCallID string) string {
	for _, tc := range toolCalls {
		if tc.ID == toolCallID {
			if tc.Function != nil && tc.Function.Name != "" {
				return tc.Function.Name
			}
			return tc.Name
		}
	}
	return ""
}
