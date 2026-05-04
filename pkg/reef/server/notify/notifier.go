package notify

import (
	"context"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// Alert represents a notification event.
type Alert struct {
	Event           string              `json:"event"`
	TaskID          string              `json:"task_id"`
	Status          string              `json:"status"`
	Instruction     string              `json:"instruction"`
	RequiredRole    string              `json:"required_role"`
	Error           *reef.TaskError     `json:"error,omitempty"`
	AttemptHistory  []reef.AttemptRecord `json:"attempt_history,omitempty"`
	EscalationCount int                 `json:"escalation_count"`
	MaxEscalations  int                 `json:"max_escalations"`
	Timestamp       time.Time           `json:"timestamp"`
}

// Notifier is the interface for notification channels.
type Notifier interface {
	// Name returns the notifier type name (e.g., "webhook", "slack", "smtp").
	Name() string
	// Notify sends an alert to the channel.
	Notify(ctx context.Context, alert Alert) error
}
