package reef

import (
	"context"
	"fmt"
	"time"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskCreated   TaskStatus = "Created"
	TaskQueued    TaskStatus = "Queued"
	TaskAssigned  TaskStatus = "Assigned"
	TaskRunning   TaskStatus = "Running"
	TaskPaused    TaskStatus = "Paused"
	TaskCompleted TaskStatus = "Completed"
	TaskFailed    TaskStatus = "Failed"
	TaskCancelled TaskStatus = "Cancelled"
	TaskEscalated TaskStatus = "Escalated"
)

// IsTerminal returns true if the status is a terminal state.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskCompleted, TaskFailed, TaskCancelled:
		return true
	}
	return false
}

// CanTransitionTo returns true if a transition from the current status
// to the target status is valid according to the state machine rules.
func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	valid := map[TaskStatus][]TaskStatus{
		TaskCreated:   {TaskQueued, TaskAssigned, TaskFailed},
		TaskQueued:    {TaskAssigned, TaskFailed},
		TaskAssigned:  {TaskRunning, TaskFailed, TaskQueued},
		TaskRunning:   {TaskCompleted, TaskFailed, TaskPaused, TaskCancelled, TaskQueued},
		TaskPaused:    {TaskRunning, TaskFailed, TaskCancelled},
		TaskCompleted: {},
		TaskFailed:    {TaskQueued, TaskEscalated}, // reassign via escalation
		TaskEscalated: {TaskQueued, TaskFailed, TaskCancelled},
		TaskCancelled: {},
	}
	allowed, ok := valid[s]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == target {
			return true
		}
	}
	return false
}

// TaskResult holds the output of a successfully completed task.
type TaskResult struct {
	Text     string         `json:"text,omitempty"`
	Files    []string       `json:"files,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`

	// P8 cognitive metadata (set by TaskRunner when sandbox is active)
	RoundsExecuted      int    `json:"rounds_executed,omitempty"`
	CorruptionsDetected int    `json:"corruptions_detected,omitempty"`
	WorkingSummary      string `json:"working_summary,omitempty"`
	TokenUsed           int    `json:"token_used,omitempty"`
}

// TaskError holds details about a task failure.
type TaskError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`

	// P8 cognitive metadata
	RoundsExecuted int `json:"rounds_executed,omitempty"`
	AttemptCount   int `json:"attempt_count,omitempty"`
}

// AttemptRecord tracks a single execution attempt.
type AttemptRecord struct {
	AttemptNumber int       `json:"attempt_number"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	Status        string    `json:"status"` // "success" or "failed"
	ErrorMessage  string    `json:"error_message,omitempty"`
	ClientID      string    `json:"client_id,omitempty"`
}

// Task is the domain object representing a unit of work in Reef.
type Task struct {
	ID              string
	Status          TaskStatus
	Instruction     string
	RequiredRole    string
	RequiredSkills  []string
	Priority        int            // 1-10, higher = more urgent. ≤5 goes to claim board.
	MaxRetries      int
	TimeoutMs       int64
	AssignedClient  string
	Result          *TaskResult
	Error           *TaskError
	AttemptHistory  []AttemptRecord
	CreatedAt       time.Time
	AssignedAt      *time.Time
	StartedAt       *time.Time
	CompletedAt     *time.Time
	EscalationCount int
	PauseReason     string // e.g. "user_request", "disconnect"
}

// NewTask creates a new task with default values.
func NewTask(id, instruction, requiredRole string, requiredSkills []string) *Task {
	return &Task{
		ID:             id,
		Status:         TaskCreated,
		Instruction:    instruction,
		RequiredRole:   requiredRole,
		RequiredSkills: requiredSkills,
		MaxRetries:     3,
		TimeoutMs:      300_000, // 5 minutes
		CreatedAt:      time.Now(),
	}
}

// Transition attempts to move the task to a new status.
// It returns an error if the transition is invalid.
func (t *Task) Transition(to TaskStatus) error {
	if !t.Status.CanTransitionTo(to) {
		return fmt.Errorf("invalid transition from %s to %s", t.Status, to)
	}
	t.Status = to
	now := time.Now()
	switch to {
	case TaskQueued:
		// no-op
	case TaskAssigned:
		t.AssignedAt = &now
	case TaskRunning:
		t.StartedAt = &now
	case TaskCompleted, TaskFailed, TaskCancelled:
		t.CompletedAt = &now
	}
	return nil
}

// AddAttempt appends a new attempt record.
func (t *Task) AddAttempt(a AttemptRecord) {
	t.AttemptHistory = append(t.AttemptHistory, a)
}

// ---------------------------------------------------------------------------
// TaskContext — injected into AgentLoop execution
// ---------------------------------------------------------------------------

// taskContextKey is the type-safe key for context values.
type taskContextKey struct{}

// TaskContext carries per-task control signals into the AgentLoop.
type TaskContext struct {
	TaskID     string
	CancelFunc context.CancelFunc
	PauseCh    chan struct{}
	ResumeCh   chan struct{}
}

// NewTaskContext creates a TaskContext with initialized channels.
func NewTaskContext(taskID string, cancel context.CancelFunc) *TaskContext {
	return &TaskContext{
		TaskID:     taskID,
		CancelFunc: cancel,
		PauseCh:    make(chan struct{}, 1),
		ResumeCh:   make(chan struct{}, 1),
	}
}

// WithContext embeds the TaskContext into a context.Context.
func (tc *TaskContext) WithContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, taskContextKey{}, tc)
}

// TaskContextFrom extracts a TaskContext from a context.Context.
// Returns nil if not present.
func TaskContextFrom(ctx context.Context) *TaskContext {
	if tc, ok := ctx.Value(taskContextKey{}).(*TaskContext); ok {
		return tc
	}
	return nil
}

// IsPaused returns a channel that receives when pause is requested.
func (tc *TaskContext) IsPaused() <-chan struct{} {
	return tc.PauseCh
}

// IsResumed returns a channel that receives when resume is requested.
func (tc *TaskContext) IsResumed() <-chan struct{} {
	return tc.ResumeCh
}
