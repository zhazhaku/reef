package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/providers"
)

// ToolSandboxHook implements ToolInterceptor to sandbox large tool outputs.
//
// When a tool (primarily exec) produces output exceeding the sandbox threshold,
// the full output is stored as a seahorse message (indexed by FTS5 for retrieval
// via short_grep/short_expand), and the LLM context receives only a summary.
//
// This prevents context pollution from verbose tool outputs while preserving
// the ability to search and retrieve full content on demand.
//
// Architecture:
//
//	LLM calls exec → tool runs → output captured →
//	  ├─ Full output → seahorse message (FTS5 indexed)
//	  └─ Summary + retrieval hint → LLM context
//
// The LLM can later use short_grep to search past outputs and short_expand
// to recover full content.
type ToolSandboxHook struct {
	sandboxThreshold int
	keepTail         int
	contextManager   ContextManager
}

// NewToolSandboxHook creates a new ToolSandboxHook.
//
// Parameters:
//   - sandboxThreshold: minimum output chars to trigger sandbox (default: 2000)
//   - keepTail: chars to retain as tail preview in summary (default: 500)
//   - cm: the ContextManager for storing full outputs in seahorse
func NewToolSandboxHook(sandboxThreshold, keepTail int, cm ContextManager) *ToolSandboxHook {
	if sandboxThreshold <= 0 {
		sandboxThreshold = 2000
	}
	if keepTail <= 0 {
		keepTail = 500
	}
	return &ToolSandboxHook{
		sandboxThreshold: sandboxThreshold,
		keepTail:         keepTail,
		contextManager:   cm,
	}
}

// BeforeTool implements ToolInterceptor. Currently a no-op pass-through.
func (h *ToolSandboxHook) BeforeTool(
	ctx context.Context,
	call *ToolCallHookRequest,
) (*ToolCallHookRequest, HookDecision, error) {
	// Pass-through. Sandboxing happens in AfterTool.
	return call, HookDecision{Action: HookActionContinue}, nil
}

// AfterTool implements ToolInterceptor. Intercepts large tool outputs and
// sandboxes them: stores the full output in seahorse (FTS5 indexed), and
// returns a summary to the LLM context.
func (h *ToolSandboxHook) AfterTool(
	ctx context.Context,
	result *ToolResultHookResponse,
) (*ToolResultHookResponse, HookDecision, error) {
	if result == nil || result.Result == nil {
		return result, HookDecision{Action: HookActionContinue}, nil
	}

	toolName := result.Tool
	forLLM := result.Result.ForLLM

	// Only sandbox exec-type tools (the main source of verbose output).
	// Other tools like web_fetch/web_search have their own truncation.
	if toolName != "exec" && toolName != "reef_execute" {
		return result, HookDecision{Action: HookActionContinue}, nil
	}

	// Don't sandbox if output is small enough.
	if len(forLLM) <= h.sandboxThreshold {
		return result, HookDecision{Action: HookActionContinue}, nil
	}

	// Get session key from context.
	sessionKey := h.getSessionKey(result)
	if sessionKey == "" {
		logger.WarnCF("agent", "ToolSandboxHook: no session key, skipping sandbox",
			map[string]any{"tool": toolName})
		return result, HookDecision{Action: HookActionContinue}, nil
	}

	// Store full output in seahorse as a tool result message.
	// This makes it searchable via short_grep (FTS5) and retrievable via short_expand.
	toolCallID := h.getToolCallID(result)
	sandboxMsg := providers.Message{
		Role:       "tool",
		Content:    forLLM,
		ToolCallID: toolCallID,
	}

	ingestCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.contextManager.Ingest(ingestCtx, &IngestRequest{
		SessionKey: sessionKey,
		Message:    sandboxMsg,
	}); err != nil {
		logger.WarnCF("agent", "ToolSandboxHook: failed to ingest sandbox msg",
			map[string]any{"tool": toolName, "error": err.Error()})
		// Fall through: still replace with summary even if ingest fails.
	}

	// Build summary: head preview + tail preview + retrieval hint.
	summary := h.buildSummary(forLLM, toolCallID, toolName)

	// Replace the LLM content with the summary.
	result.Result.ForLLM = summary

	logger.DebugCF("agent", "ToolSandboxHook: sandboxed tool output",
		map[string]any{
			"tool":          toolName,
			"full_chars":    len(forLLM),
			"summary_chars": len(summary),
			"session":       sessionKey,
		})

	return result, HookDecision{Action: HookActionContinue}, nil
}

// getSessionKey extracts the session key from the hook response context.
func (h *ToolSandboxHook) getSessionKey(
	result *ToolResultHookResponse,
) string {
	// Use the EventMeta.SessionKey if available.
	if result.Meta.SessionKey != "" {
		return result.Meta.SessionKey
	}

	// Fallback: try to construct from scope.
	if result.Context != nil && result.Context.Scope != nil {
		scope := result.Context.Scope
		parts := []string{}
		if scope.AgentID != "" {
			parts = append(parts, scope.AgentID)
		}
		if scope.Channel != "" {
			parts = append(parts, scope.Channel)
		}
		if scope.Account != "" {
			parts = append(parts, scope.Account)
		}
		if len(parts) > 0 {
			return strings.Join(parts, ":")
		}
	}
	return ""
}

// getToolCallID extracts a stable tool call identifier from the hook response.
func (h *ToolSandboxHook) getToolCallID(result *ToolResultHookResponse) string {
	if result.Meta.TurnID != "" {
		return "sandbox_" + result.Tool + "_" + result.Meta.TurnID
	}
	return fmt.Sprintf("sandbox_%s_%d", result.Tool, time.Now().UnixNano())
}

// buildSummary constructs a summary from the full tool output.
//
// Includes:
//   - Head preview (first ~200 chars)
//   - Tail preview (last keepTail chars)
//   - Retrieval hint for short_grep/short_expand
func (h *ToolSandboxHook) buildSummary(fullOutput, toolCallID, toolName string) string {
	var sb strings.Builder

	headLen := 200
	if headLen > len(fullOutput) {
		headLen = len(fullOutput)
	}

	// Head preview.
	sb.WriteString(fullOutput[:headLen])
	sb.WriteString("\n\n...")

	// Tail preview.
	if h.keepTail > 0 && len(fullOutput) > headLen+h.keepTail {
		tailStart := len(fullOutput) - h.keepTail
		if tailStart > headLen {
			sb.WriteString("\n")
			sb.WriteString(fullOutput[tailStart:])
		}
	}

	// Retrieval hint.
	sb.WriteString(fmt.Sprintf(
		"\n\n[Sandboxed: %d chars total. Use short_grep to search past tool outputs "+
			"or short_expand to retrieve the full output.]",
		len(fullOutput),
	))

	return sb.String()
}
