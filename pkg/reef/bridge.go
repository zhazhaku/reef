// PicoClaw - Ultra-lightweight personal AI agent
//
// Package reef provides the ReefBridge interface that connects the
// Hermes Coordinator's AgentLoop with the Reef Server's Scheduler.
// This allows coordination tools (reef_submit_task, reef_query_task,
// reef_status) to interact with the task scheduling system.

package reef

import "time"

// ReefBridge provides an interface for the AgentLoop's coordination tools
// to interact with the Reef Server. The Server implements this interface;
// tools consume it.
type ReefBridge interface {
	// SubmitTask submits a new task to the Reef Server for execution.
	// Returns the task ID and initial status.
	SubmitTask(instruction string, requiredRole string, requiredSkills []string, opts TaskOptions) (taskID string, err error)

	// QueryTask returns the current status and result of a task.
	QueryTask(taskID string) (*TaskSnapshot, error)

	// Status returns the overall system status.
	Status() (*SystemStatus, error)
}

// TaskOptions provides optional parameters for task submission.
type TaskOptions struct {
	MaxRetries int
	TimeoutMs  int64
	ModelHint  string
	Context    map[string]any
	ReplyTo    *ReplyToContext
}

// TaskSnapshot is a point-in-time view of a task's state.
type TaskSnapshot struct {
	TaskID          string         `json:"task_id"`
	Status          string         `json:"status"`
	Instruction     string         `json:"instruction"`
	AssignedClient  string         `json:"assigned_client,omitempty"`
	Result          *TaskResult    `json:"result,omitempty"`
	Error           *TaskError     `json:"error,omitempty"`
	AttemptHistory  []AttemptRecord `json:"attempt_history,omitempty"`
	EscalationCount int            `json:"escalation_count"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
}

// SystemStatus represents the overall Reef system status.
type SystemStatus struct {
	ConnectedClients  int              `json:"connected_clients"`
	DisconnectedCount int              `json:"disconnected_count"`
	QueuedTasks       int              `json:"queued_tasks"`
	RunningTasks      int              `json:"running_tasks"`
	CompletedTasks    int              `json:"completed_tasks"`
	FailedTasks       int              `json:"failed_tasks"`
	Clients           []ClientSnapshot `json:"clients,omitempty"`
}

// ClientSnapshot is a point-in-time view of a client's state.
type ClientSnapshot struct {
	ClientID    string   `json:"client_id"`
	Role        string   `json:"role"`
	Skills      []string `json:"skills"`
	State       string   `json:"state"`
	CurrentLoad int      `json:"current_load"`
	Capacity    int      `json:"capacity"`
}
