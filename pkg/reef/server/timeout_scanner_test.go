package server

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
)

func TestTimeoutScanner_Timeout(t *testing.T) {
	var mu sync.Mutex
	var timedOut []string

	task := reef.NewTask("t1", "long running task", "worker", nil)
	task.Status = reef.TaskRunning
	now := time.Now()
	task.StartedAt = &now
	task.TimeoutMs = 50 // 50ms timeout

	tasks := []*reef.Task{task}
	getTasks := func() []*reef.Task {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*reef.Task, len(tasks))
		copy(out, tasks)
		return out
	}
	onTimeout := func(t *reef.Task) {
		mu.Lock()
		defer mu.Unlock()
		timedOut = append(timedOut, t.ID)
	}

	scanner := NewTimeoutScanner(20*time.Millisecond, nil, getTasks, onTimeout, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scanner.Run(ctx)

	// Wait for at least one scan cycle beyond the 50ms timeout
	time.Sleep(150 * time.Millisecond)
	scanner.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(timedOut) < 1 {
		t.Error("expected at least 1 timeout")
	}
	if task.Status != reef.TaskFailed {
		t.Errorf("expected task status Failed, got %s", task.Status)
	}
	if task.Error == nil || task.Error.Type != "timeout" {
		t.Errorf("expected timeout error, got %v", task.Error)
	}
}

func TestTimeoutScanner_NoTimeout(t *testing.T) {
	var mu sync.Mutex
	var timedOut []string

	task := reef.NewTask("t1", "fresh task", "worker", nil)
	task.Status = reef.TaskRunning
	now := time.Now()
	task.StartedAt = &now
	task.TimeoutMs = 5000 // 5s timeout — won't expire during test

	tasks := []*reef.Task{task}
	getTasks := func() []*reef.Task {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*reef.Task, len(tasks))
		copy(out, tasks)
		return out
	}
	onTimeout := func(t *reef.Task) {
		mu.Lock()
		defer mu.Unlock()
		timedOut = append(timedOut, t.ID)
	}

	scanner := NewTimeoutScanner(20*time.Millisecond, nil, getTasks, onTimeout, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scanner.Run(ctx)
	time.Sleep(80 * time.Millisecond)
	scanner.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(timedOut) != 0 {
		t.Errorf("expected 0 timeouts, got %d", len(timedOut))
	}
}

func TestTimeoutScanner_NonRunningIgnored(t *testing.T) {
	var mu sync.Mutex
	var timedOut []string

	// Queued task with start time in the past should be ignored
	task := reef.NewTask("t1", "queued task", "worker", nil)
	task.Status = reef.TaskQueued
	past := time.Now().Add(-10 * time.Minute)
	task.StartedAt = &past
	task.TimeoutMs = 100

	tasks := []*reef.Task{task}
	getTasks := func() []*reef.Task {
		mu.Lock()
		defer mu.Unlock()
		return tasks
	}
	onTimeout := func(t *reef.Task) {
		mu.Lock()
		defer mu.Unlock()
		timedOut = append(timedOut, t.ID)
	}

	scanner := NewTimeoutScanner(20*time.Millisecond, nil, getTasks, onTimeout, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scanner.Run(ctx)
	time.Sleep(80 * time.Millisecond)
	scanner.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(timedOut) != 0 {
		t.Errorf("queued task should not time out, got %d callbacks", len(timedOut))
	}
}

func TestTimeoutScanner_Stop(t *testing.T) {
	scanner := NewTimeoutScanner(10*time.Millisecond, nil,
		func() []*reef.Task { return nil },
		nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	go scanner.Run(ctx)

	select {
	case <-scanner.Stopped():
		// OK
	case <-time.After(500 * time.Millisecond):
		t.Error("scanner did not stop within 500ms")
	}
}

func TestTimeoutScanner_MultipleTasks(t *testing.T) {
	var mu sync.Mutex
	var timedOut []string

	tasks := []*reef.Task{}
	for i := 0; i < 5; i++ {
		task := reef.NewTask(string(rune('a'+i)), "task", "worker", nil)
		task.Status = reef.TaskRunning
		now := time.Now()
		task.StartedAt = &now
		task.TimeoutMs = 50
		tasks = append(tasks, task)
	}

	getTasks := func() []*reef.Task {
		mu.Lock()
		defer mu.Unlock()
		out := make([]*reef.Task, len(tasks))
		copy(out, tasks)
		return out
	}
	onTimeout := func(t *reef.Task) {
		mu.Lock()
		defer mu.Unlock()
		timedOut = append(timedOut, t.ID)
	}

	scanner := NewTimeoutScanner(20*time.Millisecond, nil, getTasks, onTimeout, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go scanner.Run(ctx)
	time.Sleep(150 * time.Millisecond)
	scanner.Stop()

	mu.Lock()
	defer mu.Unlock()

	if len(timedOut) != 5 {
		t.Errorf("expected 5 timeouts, got %d: %v", len(timedOut), timedOut)
	}
}
