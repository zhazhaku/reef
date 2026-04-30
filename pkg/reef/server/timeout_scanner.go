package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server/store"
)

// TimeoutScanner periodically checks for tasks that have been running
// longer than their timeout and marks them as failed(timeout).
type TimeoutScanner struct {
	mu       sync.Mutex
	interval time.Duration
	logger   *slog.Logger

	// Dependencies
	getTasks  func() []*reef.Task            // returns all tasks (for scanning)
	onTimeout func(task *reef.Task)          // called when a task times out
	store     store.TaskStore                // optional persistent store

	stopped chan struct{}
	cancel  context.CancelFunc
}

// NewTimeoutScanner creates a timeout scanner with the given interval.
// getTasks should return a snapshot of all known tasks.
// onTimeout is called for each timed-out task.
func NewTimeoutScanner(
	interval time.Duration,
	logger *slog.Logger,
	getTasks func() []*reef.Task,
	onTimeout func(task *reef.Task),
	s store.TaskStore,
) *TimeoutScanner {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &TimeoutScanner{
		interval:  interval,
		logger:    logger,
		getTasks:  getTasks,
		onTimeout: onTimeout,
		store:     s,
		stopped:   make(chan struct{}),
	}
}

// Run starts the timeout scanner. It blocks until ctx is cancelled.
func (ts *TimeoutScanner) Run(ctx context.Context) {
	ts.mu.Lock()
	ctx, ts.cancel = context.WithCancel(ctx)
	ts.mu.Unlock()

	ticker := time.NewTicker(ts.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			close(ts.stopped)
			return
		case now := <-ticker.C:
			ts.scan(now)
		}
	}
}

// Stop signals the scanner to stop and waits for it to finish.
func (ts *TimeoutScanner) Stop() {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.cancel != nil {
		ts.cancel()
	}
}

// Stopped returns a channel that closes when the scanner has stopped.
func (ts *TimeoutScanner) Stopped() <-chan struct{} {
	return ts.stopped
}

// scan checks all running tasks for timeout.
func (ts *TimeoutScanner) scan(now time.Time) {
	tasks := ts.getTasks()
	for _, task := range tasks {
		if task.Status != reef.TaskRunning {
			continue
		}
		if task.StartedAt == nil {
			continue
		}

		deadline := task.StartedAt.Add(time.Duration(task.TimeoutMs) * time.Millisecond)
		if now.After(deadline) {
			ts.logger.Warn("task timed out",
				slog.String("task_id", task.ID),
				slog.String("instruction", task.Instruction),
				slog.Duration("elapsed", now.Sub(*task.StartedAt)))

			// Mark as failed with timeout error
			task.Error = &reef.TaskError{
				Type:    "timeout",
				Message: "task exceeded timeout",
				Detail:  "elapsed: " + now.Sub(*task.StartedAt).String(),
			}
			_ = task.Transition(reef.TaskFailed)

			// Persist if store is available
			if ts.store != nil {
				if err := ts.store.UpdateTask(task); err != nil {
					ts.logger.Warn("failed to persist timeout",
						slog.String("task_id", task.ID),
						slog.String("error", err.Error()))
				}
			}

			if ts.onTimeout != nil {
				ts.onTimeout(task)
			}
		}
	}
}
