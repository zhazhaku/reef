package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/zhazhaku/reef/pkg/reef"
)

func TestServerBridge_SubmitTask(t *testing.T) {
	registry := NewRegistry(nil)
	queue := NewTaskQueue(100, 0)
	scheduler := NewScheduler(registry, queue, SchedulerOptions{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	bridge := NewServerBridge(scheduler, registry)

	// Submit a task
	taskID, err := bridge.SubmitTask("do something", "executor", []string{"web_search"}, reef.TaskOptions{
		MaxRetries: 2,
		TimeoutMs:  60000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected non-empty task ID")
	}

	// Query the task
	snapshot, err := bridge.QueryTask(taskID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snapshot.TaskID != taskID {
		t.Fatalf("expected task ID %s, got %s", taskID, snapshot.TaskID)
	}
	if snapshot.Status != "Queued" {
		t.Fatalf("expected Queued status, got %s", snapshot.Status)
	}
	if snapshot.Instruction != "do something" {
		t.Fatalf("expected instruction 'do something', got %s", snapshot.Instruction)
	}
}

func TestServerBridge_SubmitTaskValidation(t *testing.T) {
	registry := NewRegistry(nil)
	queue := NewTaskQueue(100, 0)
	scheduler := NewScheduler(registry, queue, SchedulerOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	bridge := NewServerBridge(scheduler, registry)

	// Missing instruction
	_, err := bridge.SubmitTask("", "executor", nil, reef.TaskOptions{})
	if err == nil {
		t.Fatal("expected error for empty instruction")
	}

	// Missing role
	_, err = bridge.SubmitTask("do something", "", nil, reef.TaskOptions{})
	if err == nil {
		t.Fatal("expected error for empty role")
	}
}

func TestServerBridge_QueryTaskNotFound(t *testing.T) {
	registry := NewRegistry(nil)
	queue := NewTaskQueue(100, 0)
	scheduler := NewScheduler(registry, queue, SchedulerOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	bridge := NewServerBridge(scheduler, registry)

	_, err := bridge.QueryTask("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestServerBridge_Status(t *testing.T) {
	registry := NewRegistry(nil)
	queue := NewTaskQueue(100, 0)
	scheduler := NewScheduler(registry, queue, SchedulerOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	bridge := NewServerBridge(scheduler, registry)

	// Register a client
	registry.Register(&reef.ClientInfo{
		ID:       "client-1",
		Role:     "executor",
		Skills:   []string{"web_search"},
		Capacity: 3,
		State:    reef.ClientConnected,
	})

	status, err := bridge.Status()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.ConnectedClients != 1 {
		t.Fatalf("expected 1 connected client, got %d", status.ConnectedClients)
	}
	if len(status.Clients) != 1 {
		t.Fatalf("expected 1 client in list, got %d", len(status.Clients))
	}
	if status.Clients[0].ClientID != "client-1" {
		t.Fatalf("expected client-1, got %s", status.Clients[0].ClientID)
	}
}

func TestServerBridge_StatusWithTasks(t *testing.T) {
	registry := NewRegistry(nil)
	queue := NewTaskQueue(100, 0)
	scheduler := NewScheduler(registry, queue, SchedulerOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	bridge := NewServerBridge(scheduler, registry)

	// Submit some tasks
	_, _ = bridge.SubmitTask("task 1", "executor", nil, reef.TaskOptions{})
	_, _ = bridge.SubmitTask("task 2", "executor", nil, reef.TaskOptions{})

	status, err := bridge.Status()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status.QueuedTasks != 2 {
		t.Fatalf("expected 2 queued tasks, got %d", status.QueuedTasks)
	}
}

func TestServerBridge_SubmitAndQueryFullCycle(t *testing.T) {
	registry := NewRegistry(nil)
	queue := NewTaskQueue(100, 0)
	scheduler := NewScheduler(registry, queue, SchedulerOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	bridge := NewServerBridge(scheduler, registry)

	// Register a client so the task can be dispatched
	registry.Register(&reef.ClientInfo{
		ID:       "client-1",
		Role:     "executor",
		Skills:   []string{"web_search"},
		Capacity: 3,
		State:    reef.ClientConnected,
	})

	// Submit task
	taskID, err := bridge.SubmitTask("search for Go tutorials", "executor", []string{"web_search"}, reef.TaskOptions{
		MaxRetries: 2,
		TimeoutMs:  60000,
		ModelHint:  "gpt-4o",
	})
	if err != nil {
		t.Fatalf("submit error: %v", err)
	}

	// Query task — should be dispatched since a matching client exists
	snapshot, err := bridge.QueryTask(taskID)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if snapshot.TaskID != taskID {
		t.Fatalf("task ID mismatch: %s vs %s", snapshot.TaskID, taskID)
	}
	// Status could be Running (if dispatched) or Queued (if not yet dispatched)
	if snapshot.Status != "Running" && snapshot.Status != "Queued" {
		t.Fatalf("unexpected status: %s", snapshot.Status)
	}
}
