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
