package reef

import (
	"context"
	"fmt"
	"time"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskCreated     TaskStatus = "Created"
	TaskQueued      TaskStatus = "Queued"
	TaskAssigned    TaskStatus = "Assigned"
	TaskRunning     TaskStatus = "Running"
	TaskPaused      TaskStatus = "Paused"
	TaskBlocked     TaskStatus = "Blocked"
	TaskRecovering  TaskStatus = "Recovering"
	TaskAggregating TaskStatus = "Aggregating"
	TaskCompleted   TaskStatus = "Completed"
	TaskFailed      TaskStatus = "Failed"
	TaskCancelled   TaskStatus = "Cancelled"
	TaskEscalated   TaskStatus = "Escalated"
)

// IsTerminal returns true if the status is a terminal state.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskCompleted, TaskFailed, TaskCancelled:
		return true
	}
	return false
}

// IsBlocked returns true if the task is in a blocked state.
func (s TaskStatus) IsBlocked() bool {
	return s == TaskBlocked
}

// CanTransitionTo returns true if a transition from the current status
// to the target status is valid according to the state machine rules.
func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	valid := map[TaskStatus][]TaskStatus{
		TaskCreated:     {TaskQueued, TaskAssigned, TaskFailed, TaskBlocked, TaskAggregating, TaskCancelled},
		TaskQueued:      {TaskAssigned, TaskFailed, TaskAggregating},
		TaskAssigned:    {TaskRunning, TaskFailed, TaskQueued},
		TaskRunning:     {TaskCompleted, TaskFailed, TaskPaused, TaskCancelled, TaskQueued},
		TaskPaused:      {TaskRunning, TaskFailed, TaskCancelled},
		TaskBlocked:     {TaskQueued, TaskFailed, TaskCancelled, TaskRecovering},
		TaskRecovering:  {TaskQueued, TaskFailed, TaskCancelled, TaskBlocked},
		TaskAggregating: {TaskCompleted, TaskFailed, TaskCancelled},
		TaskCompleted:   {},
		TaskFailed:      {TaskQueued, TaskEscalated},
		TaskEscalated:   {TaskQueued, TaskFailed, TaskCancelled},
		TaskCancelled:   {},
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
}

// TaskError holds details about a task failure.
type TaskError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// BlockReport captures diagnostic information when a task enters the Blocked state.
type BlockReport struct {
	Type    string `json:"type"`    // "tool_error", "context_corruption", "resource_unavailable", "unknown"
	Message string `json:"message"` // Human-readable description of the block
	Context string `json:"context"` // Additional diagnostic context (tool name, stack trace snippet, etc.)
}

// IsValid returns false if Type is empty, Message is empty, or Type is not one of the 4 known values.
// Context field is optional and may be empty.
func (br BlockReport) IsValid() bool {
	if br.Type == "" || br.Message == "" {
		return false
	}
	switch br.Type {
	case "tool_error", "context_corruption", "resource_unavailable", "unknown":
		return true
	}
	return false
}

// TaskQuality aggregates quality signals about a task's execution.
// It is set by the EvolutionRecorder after task completion and used by
// the LocalGeneEvolver to calculate event importance.
type TaskQuality struct {
	Score        float64 `json:"score"`         // 0.0-1.0 overall quality score
	SignalsCount int     `json:"signals_count"` // Number of evolution signals extracted
	Evolved      bool    `json:"evolved"`       // Whether a Gene was generated from this task
}

// IsZero returns true if Score == 0, SignalsCount == 0, and Evolved == false.
// Used to skip serialization of zero-valued TaskQuality.
// Note: SignalsCount may be 0 even if Evolved is true (evolver ran but produced no usable signal).
func (tq TaskQuality) IsZero() bool {
	return tq.Score == 0 && tq.SignalsCount == 0 && !tq.Evolved
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
	MaxRetries      int
	TimeoutMs       int64
	ModelHint       string   // optional: preferred model for execution
	Priority        int      // 1-10, higher = more urgent, default 5
	ParentTaskID    string   // ID of parent task (empty if root)
	SubTaskIDs      []string // IDs of child sub-tasks
	Dependencies    []string // IDs of tasks this task depends on
	ReplyTo         *ReplyToContext // routing info for result delivery
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
	ProgressPercent int    // 0-100, updated by task_progress messages

	// BlockReport captures diagnostic info when a task is blocked.
	// It is set by the caller after Transition(TaskBlocked).
	// When non-nil but Status != TaskBlocked, it serves as an audit trail
	// (e.g., after recovery the report remains for post-mortem analysis).
	BlockReport *BlockReport `json:"block_report,omitempty"`

	// Quality aggregates evolution quality signals set by EvolutionRecorder
	// after task completion.
	Quality *TaskQuality `json:"quality,omitempty"`
}

// NewTask creates a new task with default values.
// TaskOptions is an optional configuration object for task creation.
type TaskOptions struct {
	MaxRetries int            `json:"max_retries,omitempty"`
	TimeoutMs  int64          `json:"timeout_ms,omitempty"`
	ModelHint  string         `json:"model_hint,omitempty"`
	ReplyTo    *ReplyToContext `json:"reply_to,omitempty"`
}

func NewTask(id, instruction, requiredRole string, requiredSkills []string) *Task {
	return &Task{
		ID:             id,
		Status:         TaskCreated,
		Instruction:    instruction,
		RequiredRole:   requiredRole,
		RequiredSkills: requiredSkills,
		MaxRetries:     3,
		TimeoutMs:      600_000, // 10 minutes
		Priority:       5,       // default medium priority
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

// TaskSnapshot is a read-only snapshot of a task's current state for API queries.
type TaskSnapshot struct {
	TaskID          string          `json:"task_id"`
	Status          string          `json:"status"`
	Instruction     string          `json:"instruction"`
	AssignedClient  string          `json:"assigned_client,omitempty"`
	Result          *TaskResult     `json:"result,omitempty"`
	Error           *TaskError      `json:"error,omitempty"`
	AttemptHistory  []AttemptRecord `json:"attempt_history"`
	EscalationCount int             `json:"escalation_count"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}
