package tools

import (
	"context"
	"fmt"
	"time"
)

// ReefExecuteTool is a sandboxed alternative to the "exec" tool.
//
// Unlike exec, which returns raw command output directly to the LLM context,
// reef_execute is designed for commands that produce large output. It:
//  1. Executes the command via the normal exec tool
//  2. Stores full output in seahorse (FTS5 indexed) via the sandbox hook
//  3. Returns a head+tail summary + retrieval instructions to the LLM
//
// The LLM can use short_grep to search past outputs and short_expand to
// retrieve full content on demand.
//
// Parameters:
//   - command (required): the shell command to execute
//   - intent (optional): what the command is trying to accomplish (helps
//     with output interpretation and search)
//   - timeout_ms (optional, default 30000): max execution time in milliseconds
//   - keep_tail (optional, default 500): max chars of tail output to include in summary
type ReefExecuteTool struct {
	registry     *ToolRegistry
	timeout      time.Duration
	defaultTail  int
}

// NewReefExecuteTool creates a new ReefExecuteTool.
func NewReefExecuteTool(registry *ToolRegistry) *ReefExecuteTool {
	return &ReefExecuteTool{
		registry:    registry,
		timeout:     30 * time.Second,
		defaultTail: 500,
	}
}

// Name returns the tool name.
func (t *ReefExecuteTool) Name() string {
	return "reef_execute"
}

// Description returns the tool description.
func (t *ReefExecuteTool) Description() string {
	return "Execute a shell command and return a sandboxed summary. " +
		"Use for commands expected to produce large output (builds, tests, " +
		"file listings, log analysis). Full output is stored and searchable. " +
		"Use short_grep to search past outputs, short_expand to retrieve full content. " +
		"Preferred over exec for commands with >500 chars of expected output."
}

// Parameters returns the parameter schema.
func (t *ReefExecuteTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute.",
			},
			"intent": map[string]any{
				"type":        "string",
				"description": "Brief description of what this command is trying to accomplish. Helps with output interpretation.",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Maximum execution time in milliseconds. Default: 30000 (30 seconds).",
				"default":     30000,
			},
			"keep_tail": map[string]any{
				"type":        "integer",
				"description": "Characters of tail output to include in the summary. Default: 500.",
				"default":     500,
			},
		},
		"required": []string{"command"},
	}
}

// Execute runs the command via the exec tool and returns a sandboxed summary.
//
// The full output is preserved — the ToolSandboxHook (if registered) will
// store the complete result in seahorse and replace the LLM context with a
// summary. This method delegates to exec, trusting the hook to handle sandboxing.
func (t *ReefExecuteTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	command, _ := args["command"].(string)
	if command == "" {
		return ErrorResult("reef_execute: required parameter 'command' is missing or empty")
	}

	// Delegate to the exec tool. The ToolSandboxHook will intercept
	// the result and replace it with a sandboxed summary.
	execResult := t.registry.Execute(ctx, "exec", map[string]any{
		"action":  "run",
		"command": command,
		"timeout": t.getTimeout(args),
	})

	if execResult == nil {
		return ErrorResult("reef_execute: exec tool returned nil")
	}

	// If the result is an error, return it as-is.
	if execResult.IsError {
		return execResult
	}

	// Add intent annotation to the ForLLM if provided.
	if intent, ok := args["intent"].(string); ok && intent != "" {
		execResult.ForLLM = fmt.Sprintf("[intent: %s]\n\n%s", intent, execResult.ForLLM)
	}

	return execResult
}

// getTimeout extracts the timeout from args, falling back to the default.
func (t *ReefExecuteTool) getTimeout(args map[string]any) float64 {
	if timeoutMs, ok := args["timeout_ms"].(float64); ok && timeoutMs > 0 {
		return timeoutMs / 1000.0 // Convert ms to seconds
	}
	return t.timeout.Seconds()
}
