package server

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

func newTestTask(id string, priority int) *reef.Task {
	t := reef.NewTask(id, fmt.Sprintf("instruction-%s", id), "worker", nil)
	t.Priority = priority
	t.Status = reef.TaskQueued
	return t
}

// ---------------------------------------------------------------------------
// 2.1.2-2.1.7: Basic heap operations
// ---------------------------------------------------------------------------

func TestPriorityQueue_EnqueueDequeue_Basic(t *testing.T) {
	q := NewPriorityQueue(100, 0)

	// Enqueue 3 tasks with different priorities
	q.Enqueue(newTestTask("low", 1))
	q.Enqueue(newTestTask("high", 10))
	q.Enqueue(newTestTask("mid", 5))

	if q.Len() != 3 {
		t.Fatalf("expected 3 tasks, got %d", q.Len())
	}

	// Should dequeue highest priority first
	t1 := q.Dequeue()
	if t1.ID != "high" {
		t.Errorf("expected high(10) first, got %s", t1.ID)
	}

	t2 := q.Dequeue()
	if t2.ID != "mid" {
		t.Errorf("expected mid(5) second, got %s", t2.ID)
	}

	t3 := q.Dequeue()
	if t3.ID != "low" {
		t.Errorf("expected low(1) third, got %s", t3.ID)
	}

	if q.Len() != 0 {
		t.Errorf("expected empty queue, got %d", q.Len())
	}

	// Dequeue from empty queue
	if q.Dequeue() != nil {
		t.Error("expected nil from empty queue")
	}
}

func TestPriorityQueue_FIFO_TieBreak(t *testing.T) {
	q := NewPriorityQueue(100, 0)

	// All same priority → FIFO order
	q.Enqueue(newTestTask("a", 5))
	q.Enqueue(newTestTask("b", 5))
	q.Enqueue(newTestTask("c", 5))

	tasks := []string{}
	for q.Len() > 0 {
		tasks = append(tasks, q.Dequeue().ID)
	}

	if len(tasks) != 3 || tasks[0] != "a" || tasks[1] != "b" || tasks[2] != "c" {
		t.Errorf("expected FIFO [a b c], got %v", tasks)
	}
}

func TestPriorityQueue_Peek(t *testing.T) {
	q := NewPriorityQueue(100, 0)
	q.Enqueue(newTestTask("low", 1))
	q.Enqueue(newTestTask("high", 10))

	peek := q.Peek()
	if peek.ID != "high" {
		t.Errorf("peek expected high, got %s", peek.ID)
	}
	if q.Len() != 2 {
		t.Error("peek should not remove")
	}
}

func TestPriorityQueue_Snapshot(t *testing.T) {
	q := NewPriorityQueue(100, 0)
	q.Enqueue(newTestTask("a", 1))
	q.Enqueue(newTestTask("b", 10))
	q.Enqueue(newTestTask("c", 5))

	snap := q.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("expected 3 in snapshot, got %d", len(snap))
	}
	// Heap order: highest priority first
	if snap[0].ID != "b" {
		t.Errorf("expected b(10) first in snapshot, got %s", snap[0].ID)
	}
	// Queue should be unchanged
	if q.Len() != 3 {
		t.Error("snapshot should not modify queue")
	}
}

func TestPriorityQueue_QueueFull(t *testing.T) {
	q := NewPriorityQueue(3, 0)
	q.Enqueue(newTestTask("a", 1))
	q.Enqueue(newTestTask("b", 1))
	q.Enqueue(newTestTask("c", 1))

	err := q.Enqueue(newTestTask("d", 1))
	if err != ErrQueueFull {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 2.1.8: Scan (non-blocking match)
// ---------------------------------------------------------------------------

func TestPriorityQueue_Scan(t *testing.T) {
	q := NewPriorityQueue(100, 0)
	q.Enqueue(newTestTask("crawl-1", 5))
	q.Enqueue(newTestTask("summarize-1", 3))
	q.Enqueue(newTestTask("crawl-2", 8))

	matches := q.Scan(func(task *reef.Task) bool {
		// "instruction-crawl-X" → crawl sits at index 12
		return len(task.Instruction) > 12 && task.Instruction[12:17] == "crawl"
	})
	if len(matches) != 2 {
		t.Errorf("expected 2 crawl matches, got %d", len(matches))
	}

	// Scan should not remove tasks
	if q.Len() != 3 {
		t.Error("scan should not remove tasks")
	}
}

// ---------------------------------------------------------------------------
// 2.1.9: Remove
// ---------------------------------------------------------------------------

func TestPriorityQueue_Remove(t *testing.T) {
	q := NewPriorityQueue(100, 0)
	q.Enqueue(newTestTask("a", 1))
	q.Enqueue(newTestTask("b", 5))
	q.Enqueue(newTestTask("c", 10))

	if !q.Remove("b") {
		t.Error("should have removed b")
	}
	if q.Len() != 2 {
		t.Errorf("expected 2 remaining, got %d", q.Len())
	}
	if q.Remove("nonexistent") {
		t.Error("should return false for nonexistent")
	}

	// Verify remaining are a and c
	remaining := map[string]bool{}
	for q.Len() > 0 {
		remaining[q.Dequeue().ID] = true
	}
	if !remaining["a"] || !remaining["c"] || remaining["b"] {
		t.Errorf("unexpected remaining set: %v", remaining)
	}
}

// ---------------------------------------------------------------------------
// 2.1.10: BoostStarvation
// ---------------------------------------------------------------------------

func TestPriorityQueue_BoostStarvation(t *testing.T) {
	q := NewPriorityQueue(100, 0)

	// Create tasks with old timestamps
	old := newTestTask("old1", 1)
	old.CreatedAt = time.Now().Add(-2 * time.Minute)
	q.Enqueue(old)

	old2 := newTestTask("old2", 2)
	old2.CreatedAt = time.Now().Add(-3 * time.Minute)
	q.Enqueue(old2)

	recent := newTestTask("recent", 1)
	recent.CreatedAt = time.Now().Add(-10 * time.Second)
	q.Enqueue(recent)

	// Boost tasks waiting > 1 minute by 3, max priority 10
	boosted := q.BoostStarvation(1*time.Minute, 3, 10)
	if boosted != 2 {
		t.Errorf("expected 2 boosted, got %d", boosted)
	}

	// Dequeue: boosted tasks should come out first
	first := q.Dequeue()
	if first.ID != "old2" {
		t.Errorf("expected old2 (boosted 2→5) first, got %s (pri=%d)", first.ID, first.Priority)
	}
	second := q.Dequeue()
	if second.ID != "old1" {
		t.Errorf("expected old1 (boosted 1→4) second, got %s (pri=%d)", second.ID, second.Priority)
	}

	// Verify priorities were bumped
	if first.Priority != 5 {
		t.Errorf("old2 priority should be 5, got %d", first.Priority)
	}
	if second.Priority != 4 {
		t.Errorf("old1 priority should be 4, got %d", second.Priority)
	}
}

func TestPriorityQueue_BoostStarvation_Cap(t *testing.T) {
	q := NewPriorityQueue(100, 0)

	task := newTestTask("high", 9)
	task.CreatedAt = time.Now().Add(-2 * time.Minute)
	q.Enqueue(task)

	boosted := q.BoostStarvation(1*time.Minute, 3, 10)
	if boosted != 1 {
		t.Errorf("expected 1 boosted, got %d", boosted)
	}

	first := q.Dequeue()
	if first.Priority != 10 {
		t.Errorf("priority should be capped at 10, got %d", first.Priority)
	}
}

// ---------------------------------------------------------------------------
// 2.1.11: Concurrent safety
// ---------------------------------------------------------------------------

func TestPriorityQueue_Concurrent(t *testing.T) {
	q := NewPriorityQueue(1000, 0)
	var wg sync.WaitGroup

	// Concurrent enqueues
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			q.Enqueue(newTestTask(fmt.Sprintf("t%d", n), n%10+1))
		}(i)
	}
	wg.Wait()

	if q.Len() != 100 {
		t.Fatalf("expected 100 tasks, got %d", q.Len())
	}

	// Concurrent dequeues
	var dequeued sync.Map
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task := q.Dequeue()
			if task != nil {
				dequeued.Store(task.ID, true)
			}
		}()
	}
	wg.Wait()

	count := 0
	dequeued.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 100 {
		t.Errorf("expected 100 unique dequeued, got %d", count)
	}
	if q.Len() != 0 {
		t.Errorf("expected empty queue, got %d", q.Len())
	}
}

// ---------------------------------------------------------------------------
// 2.1.12: All tests pass check
// ---------------------------------------------------------------------------

func TestPriorityQueue_Expire(t *testing.T) {
	q := NewPriorityQueue(100, 5*time.Second)

	old := newTestTask("old", 5)
	old.CreatedAt = time.Now().Add(-10 * time.Minute)
	q.Enqueue(old)

	recent := newTestTask("recent", 5)
	q.Enqueue(recent)

	expired := q.Expire(time.Now())
	if len(expired) != 1 || expired[0].ID != "old" {
		t.Errorf("expected 1 expired (old), got %v", expired)
	}
	if q.Len() != 1 {
		t.Errorf("expected 1 remaining, got %d", q.Len())
	}
	if q.Peek().ID != "recent" {
		t.Errorf("expected recent remaining, got %s", q.Peek().ID)
	}
}

func TestPriorityQueue_Enqueue_DefaultPriority(t *testing.T) {
	q := NewPriorityQueue(100, 0)
	task := reef.NewTask("no-pri", "test", "worker", nil)
	// Priority not explicitly set — should default to 5
	q.Enqueue(task)

	first := q.Dequeue()
	if first.Priority != 5 {
		t.Errorf("default priority should be 5, got %d", first.Priority)
	}
}
