package server

import (
	"testing"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

func TestTaskQueue_EnqueueDequeue(t *testing.T) {
	q := NewTaskQueue(10, time.Hour)
	task := reef.NewTask("t1", "test", "coder", nil)

	if err := q.Enqueue(task); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if q.Len() != 1 {
		t.Errorf("len = %d, want 1", q.Len())
	}

	got := q.Dequeue()
	if got == nil || got.ID != "t1" {
		t.Errorf("dequeued task = %v, want t1", got)
	}
	if q.Len() != 0 {
		t.Errorf("len = %d, want 0", q.Len())
	}
}

func TestTaskQueue_Full(t *testing.T) {
	q := NewTaskQueue(2, time.Hour)
	q.Enqueue(reef.NewTask("t1", "", "coder", nil))
	q.Enqueue(reef.NewTask("t2", "", "coder", nil))

	err := q.Enqueue(reef.NewTask("t3", "", "coder", nil))
	if err != ErrQueueFull {
		t.Errorf("err = %v, want ErrQueueFull", err)
	}
}

func TestTaskQueue_Peek(t *testing.T) {
	q := NewTaskQueue(10, time.Hour)
	q.Enqueue(reef.NewTask("t1", "", "coder", nil))

	peeked := q.Peek()
	if peeked == nil || peeked.ID != "t1" {
		t.Errorf("peek = %v, want t1", peeked)
	}
	if q.Len() != 1 {
		t.Error("peek should not remove item")
	}
}

func TestTaskQueue_FIFOPreserveOrder(t *testing.T) {
	q := NewTaskQueue(10, time.Hour)
	for i := 1; i <= 3; i++ {
		q.Enqueue(reef.NewTask("t"+string(rune('0'+i)), "", "coder", nil))
	}

	for i := 1; i <= 3; i++ {
		task := q.Dequeue()
		expected := "t" + string(rune('0'+i))
		if task.ID != expected {
			t.Errorf("dequeue %d = %s, want %s", i, task.ID, expected)
		}
	}
}

func TestTaskQueue_Expire(t *testing.T) {
	q := NewTaskQueue(10, time.Minute)
	old := reef.NewTask("old", "", "coder", nil)
	old.CreatedAt = time.Now().Add(-2 * time.Minute)
	fresh := reef.NewTask("fresh", "", "coder", nil)
	fresh.CreatedAt = time.Now()

	q.Enqueue(old)
	q.Enqueue(fresh)

	expired := q.Expire(time.Now())
	if len(expired) != 1 || expired[0].ID != "old" {
		t.Errorf("expired = %v, want [old]", expired)
	}
	if q.Len() != 1 {
		t.Errorf("len = %d, want 1", q.Len())
	}
}

func TestTaskQueue_Snapshot(t *testing.T) {
	q := NewTaskQueue(10, time.Hour)
	q.Enqueue(reef.NewTask("t1", "", "coder", nil))
	q.Enqueue(reef.NewTask("t2", "", "coder", nil))

	snap := q.Snapshot()
	if len(snap) != 2 {
		t.Errorf("len(snapshot) = %d, want 2", len(snap))
	}
}
