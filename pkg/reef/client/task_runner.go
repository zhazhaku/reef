package client

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/reef"
)

// ExecFunc is the user-provided function that executes a task.
// It receives the task instruction and a context that carries TaskContext.
// It should return the result text or an error.
type ExecFunc func(ctx context.Context, instruction string) (string, error)

// TaskRunner manages the execution lifecycle of tasks on a Client node.
type TaskRunner struct {
	connector   *Connector
	exec        ExecFunc
	mu          sync.Mutex
	tasks       map[string]*RunningTask
	maxRetries  int
	retryDelay  time.Duration
	logger      *slog.Logger
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
