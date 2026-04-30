package server

import (
	"errors"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

var ErrQueueFull = errors.New("task queue is full")

// TaskQueue is an in-memory FIFO queue for tasks awaiting dispatch.
type TaskQueue struct {
	mu      sync.Mutex
	tasks   []*reef.Task
	maxLen  int
	maxAge  time.Duration
}

// NewTaskQueue creates a queue with the given capacity and max age.
func NewTaskQueue(maxLen int, maxAge time.Duration) *TaskQueue {
	if maxLen <= 0 {
		maxLen = 1000
	}
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	return &TaskQueue{
		maxLen: maxLen,
		maxAge: maxAge,
	}
}

// Enqueue adds a task to the tail of the queue. Returns ErrQueueFull if at capacity.
func (q *TaskQueue) Enqueue(task *reef.Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) >= q.maxLen {
		return ErrQueueFull
	}
	q.tasks = append(q.tasks, task)
	return nil
}

// Dequeue removes and returns the task at the head of the queue, or nil if empty.
func (q *TaskQueue) Dequeue() *reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) == 0 {
		return nil
	}
	task := q.tasks[0]
	q.tasks = q.tasks[1:]
	return task
}

// Peek returns the task at the head without removing it, or nil if empty.
func (q *TaskQueue) Peek() *reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tasks) == 0 {
		return nil
	}
	return q.tasks[0]
}

// Len returns the current number of tasks in the queue.
func (q *TaskQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.tasks)
}

// Snapshot returns a copy of all queued tasks.
func (q *TaskQueue) Snapshot() []*reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*reef.Task, len(q.tasks))
	copy(out, q.tasks)
	return out
}

// Expire removes tasks that have exceeded maxAge and returns them.
func (q *TaskQueue) Expire(now time.Time) []*reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()
	var expired []*reef.Task
	var kept []*reef.Task
	for _, t := range q.tasks {
		if now.Sub(t.CreatedAt) > q.maxAge {
			expired = append(expired, t)
		} else {
			kept = append(kept, t)
		}
	}
	q.tasks = kept
	return expired
}
