package server

import (
	"log/slog"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server/store"
)

// PersistentQueue is a queue backed by a TaskStore with an in-memory cache.
// On startup it restores non-terminal tasks from the store.
type PersistentQueue struct {
	mu      sync.Mutex
	store   store.TaskStore
	cache   []*reef.Task
	maxLen  int
	maxAge  time.Duration
	logger  *slog.Logger
}

// NewPersistentQueue creates a queue backed by the given TaskStore.
// It restores non-terminal tasks on creation.
func NewPersistentQueue(s store.TaskStore, maxLen int, maxAge time.Duration, logger *slog.Logger) *PersistentQueue {
	if maxLen <= 0 {
		maxLen = 1000
	}
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}
	q := &PersistentQueue{
		store:  s,
		maxLen: maxLen,
		maxAge: maxAge,
		logger: logger,
	}
	q.restore()
	return q
}

// restore loads non-terminal tasks from the store into the cache.
// Running/Assigned tasks are reset to Queued since they can't be in-progress after restart.
func (q *PersistentQueue) restore() {
	tasks, err := q.store.ListTasks(store.TaskFilter{
		Statuses: []reef.TaskStatus{
			reef.TaskQueued, reef.TaskRunning, reef.TaskAssigned, reef.TaskPaused,
		},
	})
	if err != nil {
		q.logger.Error("failed to restore tasks from store", slog.String("error", err.Error()))
		return
	}

	restored := 0
	for _, t := range tasks {
		if t.Status == reef.TaskRunning || t.Status == reef.TaskAssigned {
			t.Status = reef.TaskQueued
			if err := q.store.UpdateTask(t); err != nil {
				q.logger.Warn("failed to reset task status during restore",
					slog.String("task_id", t.ID), slog.String("error", err.Error()))
			}
		}
		q.cache = append(q.cache, t)
		restored++
	}

	if restored > 0 {
		q.logger.Info("restored tasks from store", slog.Int("count", restored))
	}
}

// Enqueue adds a task to the queue and persists it.
func (q *PersistentQueue) Enqueue(task *reef.Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.cache) >= q.maxLen {
		return ErrQueueFull
	}

	// Persist to store
	if err := q.store.SaveTask(task); err != nil {
		return err
	}

	q.cache = append(q.cache, task)
	return nil
}

// Dequeue removes and returns the task at the head of the queue.
func (q *PersistentQueue) Dequeue() *reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.cache) == 0 {
		return nil
	}

	task := q.cache[0]
	q.cache = q.cache[1:]
	return task
}

// Peek returns the task at the head without removing it.
func (q *PersistentQueue) Peek() *reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.cache) == 0 {
		return nil
	}
	return q.cache[0]
}

// Len returns the current number of queued tasks.
func (q *PersistentQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.cache)
}

// Snapshot returns a copy of all queued tasks.
func (q *PersistentQueue) Snapshot() []*reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*reef.Task, len(q.cache))
	copy(out, q.cache)
	return out
}

// Expire removes tasks that have exceeded maxAge and updates the store.
func (q *PersistentQueue) Expire(now time.Time) []*reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var expired []*reef.Task
	var kept []*reef.Task
	for _, t := range q.cache {
		if now.Sub(t.CreatedAt) > q.maxAge {
			expired = append(expired, t)
			// Update store: mark as failed
			_ = t.Transition(reef.TaskFailed)
			if err := q.store.UpdateTask(t); err != nil {
				q.logger.Warn("failed to update expired task in store",
					slog.String("task_id", t.ID), slog.String("error", err.Error()))
			}
		} else {
			kept = append(kept, t)
		}
	}
	q.cache = kept
	return expired
}

// Store exposes the underlying TaskStore for direct access by the scheduler.
func (q *PersistentQueue) Store() store.TaskStore {
	return q.store
}
