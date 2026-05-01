package evolution

import (
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// EventType classifies the source of an evolution signal.
type EventType string

const (
	EventSuccessPattern  EventType = "success_pattern"
	EventFailurePattern  EventType = "failure_pattern"
	EventBlockingPattern EventType = "blocking_pattern"
	EventStagnation      EventType = "stagnation"
)

// EvolutionEvent records an analyzed execution signal.
// It is the output of the Observer → Recorder stage in the GEP protocol.
type EvolutionEvent struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"task_id"`
	ClientID   string    `json:"client_id"`
	EventType  EventType `json:"event_type"`
	Signal     string    `json:"signal"`     // Structured summary (≤ 500 chars by convention)
	RootCause  string    `json:"root_cause"` // LLM root cause analysis (for failures, ≤ 500 chars)
	GeneID     string    `json:"gene_id"`    // Associated Gene (empty if not yet evolved)
	Strategy   string    `json:"strategy"`   // Evolution strategy used (balanced/innovate/...)
	Importance float64   `json:"importance"` // Importance score (0.0-1.0)
	CreatedAt  time.Time `json:"created_at"`
}

// IsValid returns false for structurally invalid events.
func (e *EvolutionEvent) IsValid() bool {
	if e.ID == "" || e.TaskID == "" || e.ClientID == "" {
		return false
	}
	if e.CreatedAt.IsZero() {
		return false
	}
	if e.Importance < 0.0 || e.Importance > 1.0 {
		return false
	}
	return true
}

// EvolutionSignal is the Observer's input: a raw signal not yet persisted.
// It bridges observer output to recorder input without shipping the full
// reef.Task through the evolution pipeline.
type EvolutionSignal struct {
	Task           *reef.Task
	Result         *reef.TaskResult     // Non-nil on success
	TaskErr        *reef.TaskError      // Non-nil on failure
	AttemptHistory []reef.AttemptRecord // Full attempt log
	ToolCallSummary []ToolCallRecord     // Recent tool call summary
}

// ToolCallRecord captures a compact summary of a single tool invocation.
type ToolCallRecord struct {
	ToolName   string `json:"tool_name"`
	Parameters string `json:"parameters"` // Compact JSON summary
	Result     string `json:"result"`     // Compact result summary
}
