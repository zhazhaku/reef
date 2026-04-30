package server

import (
	"log/slog"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server/store"
)

// PersistentPriorityQueue wraps PriorityQueue with TaskStore persistence.
// All queue mutations are persisted to the store. On startup, it restores
// non-terminal tasks from the store.
type PersistentPriorityQueue struct {
	*PriorityQueue
	store  store.TaskStore
	logger *slog.Logger
}

// NewPersistentPriorityQueue creates a priority queue backed by the given TaskStore.
func NewPersistentPriorityQueue(s store.TaskStore, maxLen int, maxAge time.Duration, logger *slog.Logger) *PersistentPriorityQueue {
	if logger == nil {
		logger = slog.Default()
	}
	pq := &PersistentPriorityQueue{
		PriorityQueue: NewPriorityQueue(maxLen, maxAge),
		store:         s,
		logger:        logger,
	}
	pq.restore()
	return pq
}

// restore loads non-terminal tasks from the store into the priority queue.
func (pq *PersistentPriorityQueue) restore() {
	tasks, err := pq.store.ListTasks(store.TaskFilter{
		Statuses: []reef.TaskStatus{
			reef.TaskQueued, reef.TaskRunning, reef.TaskAssigned, reef.TaskPaused,
		},
	})
	if err != nil {
		pq.logger.Error("failed to restore tasks from store", slog.String("error", err.Error()))
		return
	}

	restored := 0
	for _, t := range tasks {
		if t.Status == reef.TaskRunning || t.Status == reef.TaskAssigned {
			t.Status = reef.TaskQueued
			if err := pq.store.UpdateTask(t); err != nil {
				pq.logger.Warn("failed to reset task status during restore",
					slog.String("task_id", t.ID), slog.String("error", err.Error()))
			}
		}
		// Enqueue into the priority heap (skips persistence since it's restore)
		pq.PriorityQueue.mu.Lock()
		pq.PriorityQueue.restoreFromStore(t)
		pq.PriorityQueue.mu.Unlock()
		restored++
	}

	if restored > 0 {
		pq.logger.Info("restored tasks from store", slog.Int("count", restored))
	}
}

// Enqueue adds a task to the priority queue and persists it.
func (pq *PersistentPriorityQueue) Enqueue(task *reef.Task) error {
	// Persist first
	if err := pq.store.SaveTask(task); err != nil {
		return err
	}
	return pq.PriorityQueue.Enqueue(task)
}

// Dequeue removes the highest-priority task without persisting the dequeue.
// The caller is responsible for updating the store on state changes.
func (pq *PersistentPriorityQueue) Dequeue() *reef.Task {
	return pq.PriorityQueue.Dequeue()
}

// Expire removes expired tasks and updates the store.
func (pq *PersistentPriorityQueue) Expire(now time.Time) []*reef.Task {
	pq.PriorityQueue.mu.Lock()
	defer pq.PriorityQueue.mu.Unlock()

	var expired []*reef.Task
	for _, item := range pq.PriorityQueue.heap {
		if now.Sub(item.task.CreatedAt) > pq.PriorityQueue.maxAge {
			expired = append(expired, item.task)
		}
	}

	for _, task := range expired {
		_ = task.Transition(reef.TaskFailed)
		if err := pq.store.UpdateTask(task); err != nil {
			pq.logger.Warn("failed to update expired task in store",
				slog.String("task_id", task.ID), slog.String("error", err.Error()))
		}
		pq.PriorityQueue.removeByID(task.ID)
	}
	return expired
}

// Store exposes the underlying TaskStore.
func (pq *PersistentPriorityQueue) Store() store.TaskStore {
	return pq.store
}

// storeAccess is a helper interface for accessing the store from any Queue.
type storeAccess interface {
	Store() store.TaskStore
}

// Ensure PersistentPriorityQueue implements Queue.
var _ Queue = (*PersistentPriorityQueue)(nil)
