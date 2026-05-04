package server

import (
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server/store"
)

func newPQTestTask(id string, status reef.TaskStatus) *reef.Task {
	return &reef.Task{
		ID:           id,
		Status:       status,
		Instruction:  "test",
		RequiredRole: "coder",
		MaxRetries:   3,
		TimeoutMs:    300000,
		CreatedAt:    time.Now(),
	}
}

func TestPersistentQueue_EnqueueDequeue(t *testing.T) {
	s := store.NewMemoryStore()
	q := NewPersistentQueue(s, 100, 10*time.Minute, nil)

	task := newPQTestTask("t1", reef.TaskQueued)
	if err := q.Enqueue(task); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if q.Len() != 1 {
		t.Fatalf("expected len 1, got %d", q.Len())
	}

	got := q.Dequeue()
	if got == nil || got.ID != "t1" {
		t.Fatalf("expected task t1, got %v", got)
	}
	if q.Len() != 0 {
		t.Fatalf("expected len 0 after dequeue, got %d", q.Len())
	}

	// Verify persisted in store
	stored, _ := s.GetTask("t1")
	if stored == nil {
		t.Fatal("expected task in store")
	}
}

func TestPersistentQueue_DequeueEmpty(t *testing.T) {
	s := store.NewMemoryStore()
	q := NewPersistentQueue(s, 100, 10*time.Minute, nil)

	got := q.Dequeue()
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestPersistentQueue_EnqueueFull(t *testing.T) {
	s := store.NewMemoryStore()
	q := NewPersistentQueue(s, 2, 10*time.Minute, nil)

	_ = q.Enqueue(newPQTestTask("t1", reef.TaskQueued))
	_ = q.Enqueue(newPQTestTask("t2", reef.TaskQueued))
	err := q.Enqueue(newPQTestTask("t3", reef.TaskQueued))
	if err != ErrQueueFull {
		t.Fatalf("expected ErrQueueFull, got %v", err)
	}
}

func TestPersistentQueue_Restore(t *testing.T) {
	s := store.NewMemoryStore()

	// Pre-populate store with tasks in various states
	queued := newPQTestTask("t1", reef.TaskQueued)
	running := newPQTestTask("t2", reef.TaskRunning)
	assigned := newPQTestTask("t3", reef.TaskAssigned)
	completed := newPQTestTask("t4", reef.TaskCompleted)
	paused := newPQTestTask("t5", reef.TaskPaused)

	_ = s.SaveTask(queued)
	_ = s.SaveTask(running)
	_ = s.SaveTask(assigned)
	_ = s.SaveTask(completed)
	_ = s.SaveTask(paused)

	// Create queue — should restore non-terminal tasks (queued, running→queued, assigned→queued, paused)
	q := NewPersistentQueue(s, 100, 10*time.Minute, nil)

	if q.Len() != 4 {
		t.Fatalf("expected 4 restored tasks (queued, running→queued, assigned→queued, paused), got %d", q.Len())
	}

	// Running and Assigned should be reset to Queued
	t2, _ := s.GetTask("t2")
	if t2.Status != reef.TaskQueued {
		t.Errorf("expected t2 reset to Queued, got %s", t2.Status)
	}
	t3, _ := s.GetTask("t3")
	if t3.Status != reef.TaskQueued {
		t.Errorf("expected t3 reset to Queued, got %s", t3.Status)
	}

	// Completed should NOT be restored
	t4, _ := s.GetTask("t4")
	if t4 == nil {
		t.Error("expected t4 to still exist in store")
	}
}

func TestPersistentQueue_Expire(t *testing.T) {
	s := store.NewMemoryStore()
	q := NewPersistentQueue(s, 100, 1*time.Second, nil)

	old := newPQTestTask("old", reef.TaskQueued)
	old.CreatedAt = time.Now().Add(-5 * time.Second)
	_ = q.Enqueue(old)

	recent := newPQTestTask("recent", reef.TaskQueued)
	_ = q.Enqueue(recent)

	expired := q.Expire(time.Now())
	if len(expired) != 1 || expired[0].ID != "old" {
		t.Fatalf("expected 1 expired task (old), got %v", expired)
	}
	if q.Len() != 1 {
		t.Fatalf("expected 1 remaining, got %d", q.Len())
	}

	// Store should have old task marked as failed
	stored, _ := s.GetTask("old")
	if stored.Status != reef.TaskFailed {
		t.Errorf("expected old task failed in store, got %s", stored.Status)
	}
}

func TestPersistentQueue_Snapshot(t *testing.T) {
	s := store.NewMemoryStore()
	q := NewPersistentQueue(s, 100, 10*time.Minute, nil)

	_ = q.Enqueue(newPQTestTask("t1", reef.TaskQueued))
	_ = q.Enqueue(newPQTestTask("t2", reef.TaskQueued))

	snap := q.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2, got %d", len(snap))
	}

	// Modifying snapshot shouldn't affect queue
	snap[0] = nil
	if q.Peek() == nil {
		t.Error("snapshot modification affected queue")
	}
}
