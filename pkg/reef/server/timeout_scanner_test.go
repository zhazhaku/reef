package server

import (
	"context"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

func TestTimeoutScanner_Create(t *testing.T) {
	scanner := NewTimeoutScanner(10*time.Millisecond, nil, nil, nil)
	if scanner == nil {
		t.Fatal("expected non-nil scanner")
	}
}

func TestTimeoutScanner_Stop(t *testing.T) {
	scanner := NewTimeoutScanner(10*time.Millisecond, nil, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	go scanner.Run(ctx)

	select {
	case <-scanner.Stopped():
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Error("scanner did not stop within 500ms")
	}
}

func TestTimeoutScanner_WithScheduler(t *testing.T) {
	reg := NewRegistry(nil)
	q := NewPriorityQueue(10, 5*time.Minute)
	sched := NewScheduler(reg, q, SchedulerOptions{})

	task := reef.NewTask("t1", "test task", "worker", nil)
	task.Status = reef.TaskRunning
	now := time.Now()
	task.StartedAt = &now
	task.TimeoutMs = 50

	sched.RegisterTask(task)

	scanner := NewTimeoutScanner(20*time.Millisecond, nil, sched, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scanner.Run(ctx)
	time.Sleep(150 * time.Millisecond)
	scanner.Stop()

	if task.Status != reef.TaskFailed {
		t.Errorf("expected task status Failed, got %s", task.Status)
	}
}
