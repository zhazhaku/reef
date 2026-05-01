// Package client provides the client-side components of the Reef evolution engine:
// ExecutionObserver, EvolutionRecorder, LocalGeneEvolver, Gatekeeper, and Submitter.
package client

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// EvolutionObserver captures success/failure/blocking patterns from task execution.
// It is the first stage of the GEP (Gene-Evolve-Publish) pipeline on the client side.
type EvolutionObserver interface {
	// ObserveTaskCompleted is called when a task finishes successfully.
	// Returns up to 3 EvolutionEvents capturing success patterns.
	ObserveTaskCompleted(ctx context.Context, signal *evolution.EvolutionSignal) ([]*evolution.EvolutionEvent, error)

	// ObserveTaskFailed is called when a task fails.
	// Returns up to 3 EvolutionEvents capturing failure/blocking patterns.
	ObserveTaskFailed(ctx context.Context, signal *evolution.EvolutionSignal) ([]*evolution.EvolutionEvent, error)
}

// ObserverConfig configures the DefaultObserver.
type ObserverConfig struct {
	// MaxOpsOnFailure is the max tool calls to include in the signal summary for failures.
	// Defaults to 5.
	MaxOpsOnFailure int

	// EnableRootCause controls whether LLM root cause analysis is performed on failures.
	// Defaults to true.
	EnableRootCause bool
}

// DefaultObserver is the concrete implementation of EvolutionObserver.
type DefaultObserver struct {
	config ObserverConfig
	logger *slog.Logger

	// rootCauseAnalyzer is an optional function for LLM-based root cause analysis.
	// When nil (or EnableRootCause is false), root cause analysis is skipped.
	// The function receives a context and a compact prompt and returns a ≤500 char analysis.
	rootCauseAnalyzer func(ctx context.Context, prompt string) (string, error)
}

// NewObserver creates a new DefaultObserver with the given config.
func NewObserver(config ObserverConfig, logger *slog.Logger) *DefaultObserver {
	if config.MaxOpsOnFailure <= 0 {
		config.MaxOpsOnFailure = 5
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultObserver{
		config: config,
		logger: logger,
	}
}

// SetRootCauseAnalyzer sets the LLM-based root cause analysis function.
// When set, ObserveTaskFailed will call this function if EnableRootCause is true.
func (o *DefaultObserver) SetRootCauseAnalyzer(fn func(ctx context.Context, prompt string) (string, error)) {
	o.rootCauseAnalyzer = fn
}

// ObserveTaskCompleted extracts success patterns from a completed task.
// It builds a compact signal (≤ 500 chars) from the result text and tool call summary,
// then creates 1 EvolutionEvent with Type=EventSuccessPattern.
func (o *DefaultObserver) ObserveTaskCompleted(ctx context.Context, signal *evolution.EvolutionSignal) ([]*evolution.EvolutionEvent, error) {
	if signal == nil || signal.Task == nil {
		return nil, fmt.Errorf("observer: nil signal or task")
	}
	if signal.Result == nil {
		return nil, fmt.Errorf("observer: nil result in success signal")
	}

	signalStr := o.buildSuccessSignal(signal)
	importance := 0.7
	if o.isNovelPattern(signal) {
		importance = 0.9
	}

	event := &evolution.EvolutionEvent{
		ID:         fmt.Sprintf("evt-%s-%d", signal.Task.ID, time.Now().UnixNano()),
		TaskID:     signal.Task.ID,
		ClientID:   signal.Task.AssignedClient,
		EventType:  evolution.EventSuccessPattern,
		Signal:     signalStr,
		RootCause:  "",
		GeneID:     "",
		Strategy:   string(evolution.StrategyBalanced),
		Importance: importance,
		CreatedAt:  time.Now().UTC(),
	}

	return []*evolution.EvolutionEvent{event}, nil
}

// ObserveTaskFailed extracts failure/blocking patterns from a failed task.
// It builds a compact failure signal, optionally performs LLM root cause analysis,
// and returns 1 EvolutionEvent.
func (o *DefaultObserver) ObserveTaskFailed(ctx context.Context, signal *evolution.EvolutionSignal) ([]*evolution.EvolutionEvent, error) {
	if signal == nil || signal.Task == nil {
		return nil, fmt.Errorf("observer: nil signal or task")
	}
	if signal.TaskErr == nil {
		return nil, fmt.Errorf("observer: nil task error in failure signal")
	}

	signalStr := o.buildFailureSignal(signal)
	eventType := o.determineFailureEventType(signal)
	importance := 0.7
	rootCause := ""

	// Optional: LLM root cause analysis
	if o.config.EnableRootCause && o.rootCauseAnalyzer != nil {
		prompt := o.buildRootCausePrompt(signal)
		analysisCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		result, err := o.rootCauseAnalyzer(analysisCtx, prompt)
		if err != nil {
			o.logger.Warn("root cause analysis failed, falling back",
				slog.String("task_id", signal.Task.ID),
				slog.String("error", err.Error()))
			rootCause = "llm_unavailable"
			importance = 0.5
		} else {
			rootCause = truncateToMaxChars(result, 500)
			importance = 0.9
		}
	}

	event := &evolution.EvolutionEvent{
		ID:         fmt.Sprintf("evt-%s-%d", signal.Task.ID, time.Now().UnixNano()),
		TaskID:     signal.Task.ID,
		ClientID:   signal.Task.AssignedClient,
		EventType:  eventType,
		Signal:     signalStr,
		RootCause:  rootCause,
		GeneID:     "",
		Strategy:   string(evolution.StrategyBalanced),
		Importance: importance,
		CreatedAt:  time.Now().UTC(),
	}

	return []*evolution.EvolutionEvent{event}, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// buildSuccessSignal constructs a ≤500-char success signal summary.
func (o *DefaultObserver) buildSuccessSignal(signal *evolution.EvolutionSignal) string {
	taskID := signal.Task.ID
	resultText := signal.Result.Text
	toolCount := len(signal.ToolCallSummary)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task %s completed via %d tool calls. ", taskID, toolCount))

	// Extract key pattern from result text
	pattern := extractKeyPattern(resultText)
	if pattern != "" {
		sb.WriteString(fmt.Sprintf("Key pattern: %s. ", pattern))
	}

	// Add tool call summary if available
	if toolCount > 0 {
		sb.WriteString("Tools: ")
		for i, tc := range signal.ToolCallSummary {
			if i >= 3 {
				sb.WriteString(fmt.Sprintf("(+%d more)", toolCount-3))
				break
			}
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(tc.ToolName)
		}
	}

	return truncateToMaxChars(sb.String(), 500)
}

// buildFailureSignal constructs a ≤500-char failure signal summary.
func (o *DefaultObserver) buildFailureSignal(signal *evolution.EvolutionSignal) string {
	taskID := signal.Task.ID
	errType := signal.TaskErr.Type
	errMsg := signal.TaskErr.Message
	attemptNum := len(signal.AttemptHistory)

	maxOps := o.config.MaxOpsOnFailure
	if maxOps <= 0 {
		maxOps = 5
	}

	toolNames := extractToolNames(signal.ToolCallSummary, maxOps)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Task %s failed: %s. ", taskID, errType))

	if errMsg != "" {
		sb.WriteString(fmt.Sprintf("Error: %s. ", truncateToMaxChars(errMsg, 200)))
	}

	if len(toolNames) > 0 {
		sb.WriteString(fmt.Sprintf("Last tools: %s. ", strings.Join(toolNames, ", ")))
	}

	sb.WriteString(fmt.Sprintf("Attempt #%d.", attemptNum))

	return truncateToMaxChars(sb.String(), 500)
}

// determineFailureEventType determines whether the failure is a blocking or regular failure pattern.
func (o *DefaultObserver) determineFailureEventType(signal *evolution.EvolutionSignal) evolution.EventType {
	if signal.TaskErr == nil {
		return evolution.EventFailurePattern
	}

	errType := strings.ToLower(signal.TaskErr.Type)
	errMsg := strings.ToLower(signal.TaskErr.Message)

	if errType == "escalated" || strings.Contains(errMsg, "unrecoverable") {
		return evolution.EventBlockingPattern
	}

	return evolution.EventFailurePattern
}

// isNovelPattern checks whether the task result contains novel patterns
// not seen in the attempt history.
func (o *DefaultObserver) isNovelPattern(signal *evolution.EvolutionSignal) bool {
	if len(signal.AttemptHistory) <= 1 {
		return true
	}
	// If the task succeeded on the first attempt, it's not a "new" pattern
	// from a recovery perspective. Novelty means the pattern changed significantly.
	for _, a := range signal.AttemptHistory[:len(signal.AttemptHistory)-1] {
		if a.Status == "success" {
			return false
		}
	}
	return true
}

// buildRootCausePrompt creates a compact LLM prompt for root cause analysis.
func (o *DefaultObserver) buildRootCausePrompt(signal *evolution.EvolutionSignal) string {
	var sb strings.Builder
	sb.WriteString("Analyze this task failure and provide a root cause in ≤500 chars. ")
	sb.WriteString(fmt.Sprintf("Task: %s. ", signal.Task.Instruction))
	sb.WriteString(fmt.Sprintf("Error: %s. ", signal.TaskErr.Message))

	toolNames := extractToolNames(signal.ToolCallSummary, o.config.MaxOpsOnFailure)
	if len(toolNames) > 0 {
		sb.WriteString(fmt.Sprintf("Tools used: %s.", strings.Join(toolNames, ", ")))
	}

	return sb.String()
}

// ---------------------------------------------------------------------------
// Utility functions
// ---------------------------------------------------------------------------

// extractKeyPattern looks for success-related keywords in the result text
// and returns a compact pattern summary.
func extractKeyPattern(text string) string {
	text = strings.ToLower(text)
	keywords := []string{"fixed", "resolved", "completed", "implemented", "tested"}
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			// Return the keyword in context: find surrounding words
			idx := strings.Index(text, kw)
			start := idx
			if start > 50 {
				start = idx - 50
			}
			end := idx + len(kw) + 50
			if end > len(text) {
				end = len(text)
			}
			return strings.TrimSpace(text[start:end])
		}
	}
	if len(text) > 100 {
		return text[:100]
	}
	return text
}

// extractToolNames extracts up to maxOps tool names from tool call summary.
func extractToolNames(summary []evolution.ToolCallRecord, maxOps int) []string {
	if len(summary) == 0 {
		return nil
	}
	// Take the last maxOps entries
	start := len(summary) - maxOps
	if start < 0 {
		start = 0
	}
	names := make([]string, 0, maxOps)
	for _, tc := range summary[start:] {
		names = append(names, tc.ToolName)
	}
	return names
}

// truncateToMaxChars truncates s to maxLen characters.
// If truncated, it replaces the last 3 characters with "...".
func truncateToMaxChars(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
