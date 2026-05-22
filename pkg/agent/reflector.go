package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/providers"
)

// ReflectorConfig controls the async reflection behavior.
type ReflectorConfig struct {
	// MinTurns is the minimum number of turn iterations before reflection is considered.
	MinTurns int
	// MinToolErrors is the minimum number of tool errors before reflection triggers.
	MinToolErrors int
	// MaxHistoryMessages is the maximum number of recent messages to include in reflection.
	MaxHistoryMessages int
}

// DefaultReflectorConfig returns sensible defaults for the reflector.
func DefaultReflectorConfig() ReflectorConfig {
	return ReflectorConfig{
		MinTurns:            3,
		MinToolErrors:       1,
		MaxHistoryMessages:  40,
	}
}

// TurnReflectionInput contains the data needed to reflect on a turn.
type TurnReflectionInput struct {
	SessionKey    string
	TurnID        string
	Iterations    int
	ToolErrors    int
	Channel       string
	ChatID        string
	UserMessage   string
	FinalResponse string
	History       []providers.Message
}

const maxReflectionHistoryMessages = 40

// TurnReflectionOutput is the structured output of a turn reflection.
type TurnReflectionOutput struct {
	Learnings []ReflectionLearning `json:"learnings"`
}

// ReflectionLearning is a single lesson learned from a turn.
type ReflectionLearning struct {
	Type  string `json:"type"`  // "mistake", "success", "observation"
	What  string `json:"what"`  // what happened
	Cause string `json:"cause"` // why it happened (for mistakes)
	Fix   string `json:"fix"`   // how to avoid it next time (for mistakes)
	Why   string `json:"why"`   // why it worked (for successes)
}

// buildReflectionPrompt constructs the prompt for the reflector LLM.
func buildReflectionPrompt(input TurnReflectionInput) string {
	historySummary := buildHistorySummary(input.History, maxReflectionHistoryMessages)

	return fmt.Sprintf(`You are a conversation analyst. Review this agent turn and output learnings as JSON.

## Turn Context
- Turn ID: %s
- Channel: %s
- Iterations: %d
- Tool Errors: %d

## User's Request
%s

## Agent's Response
%s

## Recent Conversation
%s

## Instructions
Analyze the turn above. Output a JSON object with key "learnings" containing an array of learning objects. Each learning object has:
- "type": "mistake", "success", or "observation"
- "what": brief description
- "cause": why it happened (mistakes only)
- "fix": how to avoid next time (mistakes only)
- "why": why it worked (successes only)

Focus on: tool errors, repeated tool calls, context inefficiency, wrong assumptions.
If nothing notable, return {"learnings": []}.

Output ONLY valid JSON, no markdown or other text.`,
		input.TurnID, input.Channel, input.Iterations, input.ToolErrors,
		input.UserMessage, strTrunc(input.FinalResponse, 2000),
		historySummary,
	)
}

// buildHistorySummary creates a compact summary of recent conversation history.
func buildHistorySummary(history []providers.Message, maxMessages int) string {
	start := 0
	if len(history) > maxMessages {
		start = len(history) - maxMessages
	}

	var sb strings.Builder
	count := 0
	for i := start; i < len(history) && count < maxMessages; i++ {
		msg := history[i]
		if msg.Role == "system" || msg.Role == "hidden" {
			continue
		}

		content := strTrunc(msg.Content, 500)
		toolInfo := ""
		if len(msg.ToolCalls) > 0 {
			names := make([]string, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				if tc.Function != nil {
					names[j] = tc.Function.Name
				}
			}
			toolInfo = fmt.Sprintf(" [tools: %s]", strings.Join(names, ", "))
		}

		fmt.Fprintf(&sb, "[%s] %s%s\n", msg.Role, content, toolInfo)
		count++
	}

	return sb.String()
}

// shouldReflect checks if a turn is worth reflecting on.
func shouldReflect(input TurnReflectionInput, cfg ReflectorConfig) bool {
	if input.Iterations >= cfg.MinTurns {
		return true
	}
	if input.ToolErrors >= cfg.MinToolErrors {
		return true
	}
	// Check for user dissatisfaction signals
	if strings.Contains(input.UserMessage, "不行") ||
		strings.Contains(input.UserMessage, "错误") ||
		strings.Contains(input.UserMessage, "不要这样") ||
		strings.Contains(input.UserMessage, "no") ||
		strings.Contains(input.UserMessage, "fix") ||
		strings.Contains(input.UserMessage, "again") {
		return true
	}
	return false
}

// runReflection synchronously calls the LLM to reflect on a turn and returns learnings.
func runReflection(
	ctx context.Context,
	provider providers.LLMProvider,
	model string,
	input TurnReflectionInput,
	cfg ReflectorConfig,
) (*TurnReflectionOutput, error) {
	if !shouldReflect(input, cfg) {
		return nil, nil
	}

	prompt := buildReflectionPrompt(input)

	messages := []providers.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := provider.Chat(ctx, messages, nil, model, nil)
	if err != nil {
		return nil, fmt.Errorf("reflection chat: %w", err)
	}

	content := resp.Content
	// Try to extract JSON from response (model might wrap in backticks)
	content = extractJSON(content)

	var output TurnReflectionOutput
	if err := json.Unmarshal([]byte(content), &output); err != nil {
		return nil, fmt.Errorf("parse reflection: %w (content: %s)", err, strTrunc(content, 200))
	}

	return &output, nil
}

// ApplyLearnings writes learnings to the memory store.
func ApplyLearnings(output *TurnReflectionOutput, memStore *MemoryStore) error {
	if output == nil || len(output.Learnings) == 0 {
		return nil
	}

	existing := memStore.ReadLongTerm()

	// Build learnings section
	var sb strings.Builder
	sb.WriteString("\n## Recent Learnings\n\n")
	sb.WriteString(fmt.Sprintf("*Reflected at %s*\n\n", time.Now().Format("2006-01-02 15:04")))

	for i, l := range output.Learnings {
		if i > 0 {
			sb.WriteString("\n")
		}
		switch l.Type {
		case "mistake":
			sb.WriteString(fmt.Sprintf("- ❌ **Mistake**: %s\n  - Cause: %s\n  - Fix: %s\n", l.What, l.Cause, l.Fix))
		case "success":
			sb.WriteString(fmt.Sprintf("- ✅ **Success**: %s\n  - Why: %s\n", l.What, l.Why))
		case "observation":
			sb.WriteString(fmt.Sprintf("- ℹ️ **Observation**: %s\n", l.What))
		}
	}

	// Replace or append Recent Learnings section
	learningsBlock := sb.String()
	if strings.Contains(existing, "## Recent Learnings") {
		// Replace existing section
		parts := strings.SplitN(existing, "## Recent Learnings", 2)
		newContent := parts[0] + learningsBlock
		// Remove old learnings that might come after
		if idx := strings.Index(newContent, "\n## "); idx > len(parts[0]) {
			newContent = newContent[:idx] + "\n" + newContent[idx:]
		}
		return memStore.WriteLongTerm(newContent)
	}

	// Append to end
	newContent := strings.TrimRight(existing, "\n") + "\n" + learningsBlock
	return memStore.WriteLongTerm(newContent)
}

// strTrunc limits a string to maxLen characters.
func strTrunc(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractJSON attempts to extract JSON from a string that may contain markdown backticks.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	// Remove ```json and ``` wrappers
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

// ReflectOnTurn is the main entry point for async turn reflection.
// It should be called in a goroutine after turn_end.
func ReflectOnTurn(
	al *AgentLoop,
	provider providers.LLMProvider,
	model string,
	sessionKey string,
	turnID string,
	iterations int,
	toolErrors int,
	channel string,
	chatID string,
	userMessage string,
	finalResponse string,
	history []providers.Message,
	memStore *MemoryStore,
	cfg ReflectorConfig,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := TurnReflectionInput{
		SessionKey:    sessionKey,
		TurnID:        turnID,
		Iterations:    iterations,
		ToolErrors:    toolErrors,
		Channel:       channel,
		ChatID:        chatID,
		UserMessage:   userMessage,
		FinalResponse: finalResponse,
		History:       history,
	}

	output, err := runReflection(ctx, provider, model, input, cfg)
	if err != nil {
		logger.WarnCF("agent", "Reflection failed", map[string]any{
			"turn_id":  turnID,
			"error":    err.Error(),
		})
		return
	}

	if output == nil || len(output.Learnings) == 0 {
		logger.DebugCF("agent", "Reflection: nothing notable", map[string]any{
			"turn_id": turnID,
		})
		return
	}

	if err := ApplyLearnings(output, memStore); err != nil {
		logger.WarnCF("agent", "Failed to apply learnings", map[string]any{
			"turn_id": turnID,
			"error":   err.Error(),
		})
		return
	}

	logger.InfoCF("agent", "Reflection applied", map[string]any{
		"turn_id":   turnID,
		"learnings": len(output.Learnings),
	})
}
