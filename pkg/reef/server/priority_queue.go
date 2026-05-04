package server

import (
	"container/heap"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

// pqItem wraps a task with heap metadata for the priority queue.
type pqItem struct {
	task     *reef.Task
	priority int   // cached priority; higher = more urgent
	seq      int64 // insertion sequence for FIFO tie-breaking
	index    int   // heap index maintained by container/heap
}

// priorityHeap implements container/heap.Interface.
// Higher priority items are less (popped first). Same priority → lower seq first.
type priorityHeap []*pqItem

func (h priorityHeap) Len() int { return len(h) }

func (h priorityHeap) Less(i, j int) bool {
	// Higher priority first; same priority → earlier insertion first
	if h[i].priority != h[j].priority {
		return h[i].priority > h[j].priority
	}
	return h[i].seq < h[j].seq
}

func (h priorityHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *priorityHeap) Push(x any) {
	n := len(*h)
	item := x.(*pqItem)
	item.index = n
	*h = append(*h, item)
}

func (h *priorityHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// PriorityQueue is a thread-safe, heap-based priority queue for tasks.
// It implements the Queue interface and adds non-blocking Scan/Remove/Boost.
type PriorityQueue struct {
	mu       sync.Mutex
	heap     priorityHeap
	seq      atomic.Int64
	maxLen   int
	maxAge   time.Duration
	byID     map[string]*pqItem // task ID → heap item for fast lookup
}

// NewPriorityQueue creates an empty priority queue with capacity and age limits.
func NewPriorityQueue(maxLen int, maxAge time.Duration) *PriorityQueue {
	if maxLen <= 0 {
		maxLen = 1000
	}
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	return &PriorityQueue{
		maxLen: maxLen,
		maxAge: maxAge,
		byID:   make(map[string]*pqItem),
	}
}

// Enqueue adds a task to the priority queue. Returns ErrQueueFull if at capacity.
func (q *PriorityQueue) Enqueue(task *reef.Task) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.heap) >= q.maxLen {
		return ErrQueueFull
	}

	// Resolve priority: use task.Priority if set (1-10), else default 5.
	p := task.Priority
	if p < 1 {
		p = 5
	}

	item := &pqItem{
		task:     task,
		priority: p,
		seq:      q.seq.Add(1),
	}
	heap.Push(&q.heap, item)
	q.byID[task.ID] = item
	return nil
}

// Dequeue removes and returns the highest-priority task, or nil if empty.
func (q *PriorityQueue) Dequeue() *reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.heap) == 0 {
		return nil
	}

	item := heap.Pop(&q.heap).(*pqItem)
	delete(q.byID, item.task.ID)
	return item.task
}

// Peek returns the highest-priority task without removing it, or nil if empty.
func (q *PriorityQueue) Peek() *reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.heap) == 0 {
		return nil
	}
	return q.heap[0].task
}

// Len returns the current number of tasks in the queue.
func (q *PriorityQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.heap)
}

// Snapshot returns a copy of all queued tasks in heap order.
func (q *PriorityQueue) Snapshot() []*reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	out := make([]*reef.Task, len(q.heap))
	for i, item := range q.heap {
		out[i] = item.task
	}
	return out
}

// Expire removes tasks that have exceeded maxAge and returns them.
func (q *PriorityQueue) Expire(now time.Time) []*reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var expired []*reef.Task
	for _, item := range q.heap {
		if now.Sub(item.task.CreatedAt) > q.maxAge {
			expired = append(expired, item.task)
		}
	}

	for _, task := range expired {
		q.removeByID(task.ID)
	}
	return expired
}

// Scan returns all queued tasks that match the given predicate, without removing them.
// This is a non-blocking operation suitable for TryDispatch's client-matching loop.
func (q *PriorityQueue) Scan(matchFn func(*reef.Task) bool) []*reef.Task {
	q.mu.Lock()
	defer q.mu.Unlock()

	var matches []*reef.Task
	for _, item := range q.heap {
		if matchFn(item.task) {
			matches = append(matches, item.task)
		}
	}
	return matches
}

// Remove removes a task by ID from the queue. Returns true if found and removed.
func (q *PriorityQueue) Remove(taskID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.removeByID(taskID)
}

// BoostStarvation increases the priority of tasks that have been waiting
// longer than the threshold. Boost is added up to maxPriority.
// Returns the number of boosted tasks.
func (q *PriorityQueue) BoostStarvation(threshold time.Duration, boost int, maxPriority int) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := time.Now()
	boosted := 0
	for _, item := range q.heap {
		if now.Sub(item.task.CreatedAt) > threshold {
			newPriority := item.priority + boost
			if newPriority > maxPriority {
				newPriority = maxPriority
			}
			if newPriority != item.priority {
				item.priority = newPriority
				item.task.Priority = newPriority
				heap.Fix(&q.heap, item.index)
				boosted++
			}
		}
	}
	return boosted
}

// removeByID removes an item by task ID. Must be called with mu held.
func (q *PriorityQueue) removeByID(taskID string) bool {
	item, ok := q.byID[taskID]
	if !ok {
		return false
	}
	heap.Remove(&q.heap, item.index)
	delete(q.byID, taskID)
	return true
}

// restoreFromStore bulk-loads restored tasks into the heap (must hold mu).
func (q *PriorityQueue) restoreFromStore(task *reef.Task) {
	item := &pqItem{
		task:     task,
		priority: task.Priority,
		seq:      q.seq.Add(1),
	}
	heap.Push(&q.heap, item)
	q.byID[task.ID] = item
}

// Ensure PriorityQueue implements Queue.
var _ Queue = (*PriorityQueue)(nil)
