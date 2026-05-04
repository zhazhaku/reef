package client

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ExecFunc is the user-provided function that executes a task.
// It receives the task instruction and a context that carries TaskContext.
// It should return the result text or an error.
type ExecFunc func(ctx context.Context, instruction string) (string, error)

// evolutionObserver is the minimal interface for observing task execution.
// It avoids a direct import dependency on the evolution/client package.
type evolutionObserver interface {
	ObserveTaskCompleted(ctx context.Context, signal *evolution.EvolutionSignal) ([]*evolution.EvolutionEvent, error)
	ObserveTaskFailed(ctx context.Context, signal *evolution.EvolutionSignal) ([]*evolution.EvolutionEvent, error)
}

// evolutionRecorder is the minimal interface for recording evolution events.
type evolutionRecorder interface {
	Record(event *evolution.EvolutionEvent) error
}

// TaskRunner manages the execution lifecycle of tasks on a Client node.
type TaskRunner struct {
	connector   *Connector
	exec        ExecFunc
	mu          sync.Mutex
	tasks       map[string]*RunningTask
	maxRetries  int
	retryDelay  time.Duration
	logger      *slog.Logger
	observer    evolutionObserver // optional — set via SetEvolutionObserver
	recorder    evolutionRecorder // optional — set via SetEvolutionRecorder
}

// RunningTask represents an in-flight task on the Client.
type RunningTask struct {
	TaskID      string
	Instruction string
	Ctx         context.Context
	CancelFunc  context.CancelFunc
	TaskCtx     *reef.TaskContext
	Status      string // "running", "paused", "completed", "failed"
	Result      string
	Error       error
	Attempts    []reef.AttemptRecord
	ToolCalls   []evolution.ToolCallRecord // tool calls recorded during execution
	mu          sync.Mutex
}

// TaskRunnerOptions configures the runner.
type TaskRunnerOptions struct {
	Connector   *Connector
	Exec        ExecFunc
	MaxRetries  int
	RetryDelay  time.Duration // base delay for retries (default 1s)
	Logger      *slog.Logger
}

// NewTaskRunner creates a new task runner.
func NewTaskRunner(opts TaskRunnerOptions) *TaskRunner {
	if opts.MaxRetries <= 0 {
		opts.MaxRetries = 3
	}
	if opts.RetryDelay <= 0 {
		opts.RetryDelay = time.Second
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &TaskRunner{
		connector:  opts.Connector,
		exec:       opts.Exec,
		tasks:      make(map[string]*RunningTask),
		maxRetries: opts.MaxRetries,
		retryDelay: opts.RetryDelay,
		logger:     opts.Logger,
	}
}

// SetEvolutionObserver sets the optional observer for evolution signal collection.
// When nil, evolution observation is disabled (zero overhead).
func (r *TaskRunner) SetEvolutionObserver(o evolutionObserver) {
	r.observer = o
}

// SetEvolutionRecorder sets the optional recorder for evolution event persistence.
// When nil, evolution recording is disabled.
func (r *TaskRunner) SetEvolutionRecorder(rec evolutionRecorder) {
	r.recorder = rec
}

// RecordToolCall records a tool invocation for the running task.
// This is a no-op if the task is not found.
func (r *TaskRunner) RecordToolCall(taskID string, tc evolution.ToolCallRecord) {
	r.mu.Lock()
	rt, ok := r.tasks[taskID]
	r.mu.Unlock()
	if !ok {
		return
	}
	rt.mu.Lock()
	rt.ToolCalls = append(rt.ToolCalls, tc)
	rt.mu.Unlock()
}

// StartTask begins execution of a new task.
func (r *TaskRunner) StartTask(taskID, instruction string, maxRetries int) {
	if maxRetries <= 0 {
		maxRetries = r.maxRetries
	}

	ctx, cancel := context.WithCancel(context.Background())
	tc := reef.NewTaskContext(taskID, cancel)
	rt := &RunningTask{
		TaskID:      taskID,
		Instruction: instruction,
		Ctx:         tc.WithContext(ctx),
		CancelFunc:  cancel,
		TaskCtx:     tc,
		Status:      "running",
	}

	r.mu.Lock()
	r.tasks[taskID] = rt
	r.mu.Unlock()

	r.reportProgress(taskID, "started", 0, "")

	go r.runWithRetry(rt, maxRetries)
}

// runWithRetry executes the task with local retry logic.
func (r *TaskRunner) runWithRetry(rt *RunningTask, maxRetries int) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		record := reef.AttemptRecord{
			AttemptNumber: attempt,
			StartedAt:     time.Now(),
			Status:        "failed",
		}

		result, err := r.runOnce(rt)
		record.EndedAt = time.Now()

		if err == nil {
			record.Status = "success"
			rt.mu.Lock()
			rt.Status = "completed"
			rt.Result = result
			rt.Attempts = append(rt.Attempts, record)
			rt.mu.Unlock()

			// Evolution: observe and record success
			r.recordEvolutionSuccess(rt, result)

			r.reportCompleted(rt.TaskID, result, record.EndedAt.Sub(record.StartedAt).Milliseconds())
			return
		}

		lastErr = err
		record.ErrorMessage = err.Error()
		rt.mu.Lock()
		rt.Attempts = append(rt.Attempts, record)
		rt.mu.Unlock()

		// Do not retry if cancelled
		if rt.Ctx.Err() == context.Canceled {
			r.logger.Info("task cancelled, aborting retries", slog.String("task_id", rt.TaskID))
			break
		}

		if attempt < maxRetries {
			delay := time.Duration(1<<attempt) * r.retryDelay
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			r.logger.Info("retrying task",
				slog.String("task_id", rt.TaskID),
				slog.Int("attempt", attempt+1),
				slog.Duration("delay", delay))
			select {
			case <-time.After(delay):
			case <-rt.Ctx.Done():
				break
			}
		}
	}

	rt.mu.Lock()
	rt.Status = "failed"
	rt.Error = lastErr
	rt.mu.Unlock()

	// Evolution: observe and record failure
	r.recordEvolutionFailure(rt, lastErr)

	r.reportFailed(rt.TaskID, lastErr, rt.Attempts)
}

// runOnce executes a single attempt, respecting pause/resume signals.
func (r *TaskRunner) runOnce(rt *RunningTask) (string, error) {
	// Check for pause
	select {
	case <-rt.TaskCtx.IsPaused():
		r.reportProgress(rt.TaskID, "paused", 0, "")
		<-rt.TaskCtx.IsResumed()
		r.reportProgress(rt.TaskID, "running", 0, "resumed")
	default:
	}

	if r.exec == nil {
		return "", fmt.Errorf("no exec function configured")
	}
	return r.exec(rt.Ctx, rt.Instruction)
}

// CancelTask aborts a running task.
func (r *TaskRunner) CancelTask(taskID string) bool {
	r.mu.Lock()
	rt, ok := r.tasks[taskID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	rt.CancelFunc()
	return true
}

// PauseTask signals a running task to pause.
func (r *TaskRunner) PauseTask(taskID string) bool {
	r.mu.Lock()
	rt, ok := r.tasks[taskID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case rt.TaskCtx.PauseCh <- struct{}{}:
		rt.mu.Lock()
		rt.Status = "paused"
		rt.mu.Unlock()
		return true
	default:
		return false
	}
}

// ResumeTask signals a paused task to resume.
func (r *TaskRunner) ResumeTask(taskID string) bool {
	r.mu.Lock()
	rt, ok := r.tasks[taskID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case rt.TaskCtx.ResumeCh <- struct{}{}:
		rt.mu.Lock()
		rt.Status = "running"
		rt.mu.Unlock()
		return true
	default:
		return false
	}
}

// GetTask returns a snapshot of a running task.
func (r *TaskRunner) GetTask(taskID string) *RunningTask {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tasks[taskID]
}

// ---------------------------------------------------------------------------
// Reporting helpers
// ---------------------------------------------------------------------------

func (r *TaskRunner) reportProgress(taskID, status string, percent int, message string) {
	msg, _ := reef.NewMessage(reef.MsgTaskProgress, taskID, reef.TaskProgressPayload{
		TaskID:          taskID,
		Status:          status,
		ProgressPercent: percent,
		Message:         message,
	})
	_ = r.connector.Send(msg)
}

func (r *TaskRunner) reportCompleted(taskID, result string, execTimeMs int64) {
	msg, _ := reef.NewMessage(reef.MsgTaskCompleted, taskID, reef.TaskCompletedPayload{
		TaskID:          taskID,
		Result:          map[string]any{"text": result},
		ExecutionTimeMs: execTimeMs,
	})
	_ = r.connector.Send(msg)
}

func (r *TaskRunner) reportFailed(taskID string, err error, attempts []reef.AttemptRecord) {
	msg, _ := reef.NewMessage(reef.MsgTaskFailed, taskID, reef.TaskFailedPayload{
		TaskID:         taskID,
		ErrorType:      "escalated",
		ErrorMessage:   err.Error(),
		AttemptHistory: attempts,
	})
	_ = r.connector.Send(msg)
}

// ---------------------------------------------------------------------------
// Evolution hooks (best-effort, never fail the task)
// ---------------------------------------------------------------------------

// recordEvolutionSuccess builds an EvolutionSignal for a completed task,
// calls the observer and recorder if configured. Errors are logged but never
// propagated — evolution signal collection is best-effort.
func (r *TaskRunner) recordEvolutionSuccess(rt *RunningTask, result string) {
	if r.observer == nil {
		return
	}

	task := r.buildTaskFromRunning(rt)
	signal := &evolution.EvolutionSignal{
		Task:            task,
		Result:          &reef.TaskResult{Text: result},
		AttemptHistory:  rt.Attempts,
		ToolCallSummary: rt.ToolCalls,
	}

	events, err := r.observer.ObserveTaskCompleted(context.Background(), signal)
	if err != nil {
		r.logger.Warn("observer failed on task completion", slog.String("task_id", rt.TaskID), slog.String("error", err.Error()))
		return
	}

	r.recordEvents(events)
}

// recordEvolutionFailure builds an EvolutionSignal for a failed task,
// calls the observer and recorder if configured. Errors are logged but never
// propagated.
func (r *TaskRunner) recordEvolutionFailure(rt *RunningTask, taskErr error) {
	if r.observer == nil {
		return
	}

	task := r.buildTaskFromRunning(rt)
	signal := &evolution.EvolutionSignal{
		Task:            task,
		TaskErr:         &reef.TaskError{Type: "escalated", Message: taskErr.Error()},
		AttemptHistory:  rt.Attempts,
		ToolCallSummary: rt.ToolCalls,
	}

	events, err := r.observer.ObserveTaskFailed(context.Background(), signal)
	if err != nil {
		r.logger.Warn("observer failed on task failure", slog.String("task_id", rt.TaskID), slog.String("error", err.Error()))
		return
	}

	r.recordEvents(events)
}

// recordEvents sends events to the recorder. Each Record call is independent;
// errors are logged but do not fail the task.
func (r *TaskRunner) recordEvents(events []*evolution.EvolutionEvent) {
	if r.recorder == nil {
		return
	}
	for _, event := range events {
		if err := r.recorder.Record(event); err != nil {
			r.logger.Warn("recorder failed to record event",
				slog.String("event_id", event.ID),
				slog.String("error", err.Error()))
		}
	}
}

// buildTaskFromRunning constructs a minimal reef.Task from a RunningTask
// for use in EvolutionSignal. It is a shallow copy with only the fields
// needed by the observer.
func (r *TaskRunner) buildTaskFromRunning(rt *RunningTask) *reef.Task {
	return &reef.Task{
		ID:             rt.TaskID,
		Instruction:    rt.Instruction,
		Status:         reef.TaskStatus(rt.Status),
		AssignedClient: "", // filled by caller if needed
		AttemptHistory: rt.Attempts,
	}
}
