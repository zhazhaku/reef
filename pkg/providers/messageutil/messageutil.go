package messageutil

import (
	"strings"

	"github.com/zhazhaku/reef/pkg/providers/protocoltypes"
)

// IsTransientAssistantThoughtMessage reports whether msg is an invalid
// reasoning-only assistant history record. These "hanging" thought messages
// are not a canonical persisted format and should be discarded instead of
// replayed or reconstructed.
//
// IMPORTANT: Messages with ReasoningContentPresent=true are NOT transient.
// ReasoningContentPresent means the API explicitly returned reasoning_content
// (DeepSeek thinking mode). Such messages must survive for round-trip.
func IsTransientAssistantThoughtMessage(msg protocoltypes.Message) bool {
	if msg.ReasoningContentPresent {
		return false
	}
	return msg.Role == "assistant" &&
		strings.TrimSpace(msg.Content) == "" &&
		strings.TrimSpace(msg.ReasoningContent) != "" &&
		len(msg.ToolCalls) == 0 &&
		len(msg.Media) == 0 &&
		len(msg.Attachments) == 0 &&
		strings.TrimSpace(msg.ToolCallID) == ""
}

// FilterInvalidHistoryMessages removes invalid persisted history records such
// as transient assistant thought-only messages.
func FilterInvalidHistoryMessages(history []protocoltypes.Message) []protocoltypes.Message {
	if len(history) == 0 {
		return []protocoltypes.Message{}
	}

	filtered := make([]protocoltypes.Message, 0, len(history))
	for _, msg := range history {
		if IsTransientAssistantThoughtMessage(msg) {
			continue
		}
		filtered = append(filtered, msg)
	}
	return filtered
}

// TrimLeadingOrphans removes orphan tool-call pairs from the front of history.
// An orphan is an assistant(tool_calls) message with no preceding user message,
// and its associated tool result messages. These occur when a session's first
// persisted messages are mid-turn tool calls (e.g., from interrupted sessions
// or Hermes mode).
//
// Removing them here avoids the downstream sanitizeHistoryForProvider having
// to drop dozens of messages on every turn, saving CPU cycles and eliminating
// verbose debug logging per request.
func TrimLeadingOrphans(history []protocoltypes.Message) []protocoltypes.Message {
	if len(history) == 0 {
		return history
	}
	i := 0
	for i < len(history) {
		msg := history[i]
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Orphan tool-call assistant: skip it and all following tool messages.
			i++
			for i < len(history) && history[i].Role == "tool" {
				i++
			}
			continue
		}
		if msg.Role == "tool" {
			// Orphan tool message without preceding assistant tool-call: skip.
			i++
			continue
		}
		// First valid message found (user, system, or non-tool-call assistant).
		break
	}
	if i == 0 {
		return history
	}
	return history[i:]
}
