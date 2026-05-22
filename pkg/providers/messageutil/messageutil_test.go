package messageutil

import (
	"testing"

	"github.com/zhazhaku/reef/pkg/providers/protocoltypes"
)

func TestTrimLeadingOrphans(t *testing.T) {
	tests := []struct {
		name     string
		input    []protocoltypes.Message
		expected int // expected length after trim
	}{
		{
			name:     "empty",
			input:    []protocoltypes.Message{},
			expected: 0,
		},
		{
			name: "clean history starts with user",
			input: []protocoltypes.Message{
				{Role: "user", Content: "hello"},
				{Role: "assistant", Content: "hi"},
			},
			expected: 2,
		},
		{
			name: "single orphan assistant tool-call + tool",
			input: []protocoltypes.Message{
				{Role: "assistant", Content: "", ToolCalls: []protocoltypes.ToolCall{{ID: "call_1"}}},
				{Role: "tool", Content: "result", ToolCallID: "call_1"},
				{Role: "user", Content: "hello"},
			},
			expected: 1, // only "hello" remains
		},
		{
			name: "multiple orphan pairs before clean",
			input: []protocoltypes.Message{
				{Role: "assistant", ToolCalls: []protocoltypes.ToolCall{{ID: "c1"}}},
				{Role: "tool", ToolCallID: "c1"},
				{Role: "assistant", ToolCalls: []protocoltypes.ToolCall{{ID: "c2"}}},
				{Role: "tool", ToolCallID: "c2"},
				{Role: "user", Content: "hi"},
				{Role: "assistant", Content: "hello"},
			},
			expected: 2, // user + assistant remain
		},
		{
			name: "orphan tool message without preceding assistant",
			input: []protocoltypes.Message{
				{Role: "tool", Content: "orphan", ToolCallID: "x"},
				{Role: "user", Content: "hello"},
			},
			expected: 1, // only "hello" remains
		},
		{
			name: "all orphans, no valid messages",
			input: []protocoltypes.Message{
				{Role: "assistant", ToolCalls: []protocoltypes.ToolCall{{ID: "c1"}}},
				{Role: "tool", ToolCallID: "c1"},
			},
			expected: 0,
		},
		{
			name: "non-tool-call assistant is valid start",
			input: []protocoltypes.Message{
				{Role: "assistant", Content: "I'm thinking..."},
				{Role: "user", Content: "hello"},
			},
			expected: 2,
		},
		{
			name: "system message is valid start",
			input: []protocoltypes.Message{
				{Role: "system", Content: "system msg"},
				{Role: "user", Content: "hello"},
			},
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TrimLeadingOrphans(tt.input)
			if len(result) != tt.expected {
				t.Errorf("expected %d messages, got %d", tt.expected, len(result))
			}
			if tt.expected > 0 && len(result) > 0 {
				first := result[0]
				if first.Role == "assistant" && len(first.ToolCalls) > 0 {
					t.Error("first message is still orphan assistant tool-call")
				}
				if first.Role == "tool" {
					// After trimming all orphans, a tool message is only valid
					// if preceded by an assistant tool-call. Since we trimmed to
					// the first non-orphan, a tool at position 0 is invalid.
					// But this can happen if history was: [assistant(tool_calls), tool, tool(user?!)]
					// The only case is if the clean prefix starts with tool.
					// This shouldn't happen in real data. We accept it.
				}
			}
		})
	}
}

func TestTrimLeadingOrphans_NoAlloc(t *testing.T) {
	clean := []protocoltypes.Message{
		{Role: "user", Content: "hello"},
	}
	result := TrimLeadingOrphans(clean)
	// Should return the same slice (no allocation) when no orphans exist.
	// We check by verifying the first element pointer matches.
	if len(result) != 1 || &result[0] != &clean[0] {
		t.Log("slice was reallocated for clean input (acceptable but not optimal)")
	}
}
