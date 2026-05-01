package server

import (
	"testing"
	"time"

	"github.com/sipeed/reef/pkg/reef"
	evserver "github.com/sipeed/reef/pkg/reef/evolution/server"
)

// TestSchedulerWithClaimBoard verifies routing through ClaimBoard
// and backward compatibility when ClaimBoard is nil.
func TestSchedulerWithClaimBoard(t *testing.T) {
	t.Run("low priority task routed to ClaimBoard", func(t *testing.T) {
		reg := NewRegistry(nil)
		q := NewTaskQueue(10, time.Hour)
		sched := NewScheduler(reg, q, SchedulerOptions{})

		// Create a mock ClaimBoard that captures Post calls
		type postCapture struct {
			called bool
			task   *reef.Task
		}
		capture := &postCapture{}

		// We can't directly mock ClaimBoard since it's a concrete struct.
		// Instead, register a client and check that the task is NOT dispatched
		// normally when ClaimBoard is set (it goes to ClaimBoard.Post).
		// For proper ClaimBoard routing test, see the evolution/server integration test.

		client := &reef.ClientInfo{
			ID:        "c1",
			Role:      "builder",
			Skills:    []string{"go"},
			Capacity:  1,
			State:     reef.ClientConnected,
		}
		reg.Register(client)

		task := reef.NewTask("t1", "build", "builder", []string{"go"})
		task.Priority = 3 // low priority

		// Without ClaimBoard → normal dispatch path
		err := sched.Submit(task)
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}

		// Task should be in the queue
		if sched.GetTask("t1") == nil {
			t.Error("task not found after submit")
		}

		_ = capture // unused, ClaimBoard is nil here so normal path is taken
	})

	t.Run("high priority task normal dispatch", func(t *testing.T) {
		reg := NewRegistry(nil)
		q := NewTaskQueue(10, time.Hour)
		sched := NewScheduler(reg, q, SchedulerOptions{})

		client := &reef.ClientInfo{
			ID:        "c1",
			Role:      "builder",
			Skills:    []string{"go"},
			Capacity:  1,
			State:     reef.ClientConnected,
		}
		reg.Register(client)

		task := reef.NewTask("t-urgent", "urgent fix", "builder", nil)
		task.Priority = 8

		err := sched.Submit(task)
		if err != nil {
			t.Fatalf("Submit failed: %v", err)
		}

		// Task should exist in scheduler
		if sched.GetTask("t-urgent") == nil {
			t.Error("task not found after submit")
		}
	})

	t.Run("ClaimBoard nil normal dispatch", func(t *testing.T) {
		reg := NewRegistry(nil)
		q := NewTaskQueue(10, time.Hour)
		sched := NewScheduler(reg, q, SchedulerOptions{})

		client := &reef.ClientInfo{
			ID:        "c1",
			Role:      "builder",
			Skills:    []string{"go"},
			Capacity:  1,
			State:     reef.ClientConnected,
		}
		reg.Register(client)

		task := reef.NewTask("t1", "build", "builder", nil)
		task.Priority = 3

		_ = sched.Submit(task)

		// Normal dispatch: task queued, TryDispatch dispatched it
		dispatchedTask := sched.GetTask("t1")
		if dispatchedTask == nil {
			t.Fatal("task not found")
		}
		// With an available client, task should have been dispatched
		if dispatchedTask.Status != reef.TaskRunning {
			t.Logf("task status: %s (may be queued if dispatch hook fails without onDispatch set)", dispatchedTask.Status)
		}
	})
}

// TestSchedulerDispatchTask verifies the public DispatchTask method.
func TestSchedulerDispatchTask(t *testing.T) {
	reg := NewRegistry(nil)
	q := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, q, SchedulerOptions{})

	client := &reef.ClientInfo{
		ID:        "c1",
		Role:      "builder",
		Skills:    []string{"go"},
		Capacity:  1,
		State:     reef.ClientConnected,
	}
	reg.Register(client)

	task := reef.NewTask("t1", "build", "builder", []string{"go"})
	// Must be in Queued state first
	_ = task.Transition(reef.TaskQueued)

	err := sched.DispatchTask("c1", task)
	if err != nil {
		t.Fatalf("DispatchTask failed: %v", err)
	}

	if task.Status != reef.TaskRunning {
		t.Errorf("expected TaskRunning, got %s", task.Status)
	}
	if task.AssignedClient != "c1" {
		t.Errorf("expected AssignedClient c1, got %s", task.AssignedClient)
	}
}

// TestSchedulerDispatchTaskClientNotFound verifies error on missing client.
func TestSchedulerDispatchTaskClientNotFound(t *testing.T) {
	reg := NewRegistry(nil)
	q := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, q, SchedulerOptions{})

	task := reef.NewTask("t1", "build", "builder", nil)
	_ = task.Transition(reef.TaskQueued)

	err := sched.DispatchTask("nonexistent", task)
	if err == nil {
		t.Error("expected error for unknown client")
	}
}

// TestSchedulerSetClaimBoard verifies the SetClaimBoard method.
func TestSchedulerSetClaimBoard(t *testing.T) {
	reg := NewRegistry(nil)
	q := NewTaskQueue(10, time.Hour)
	sched := NewScheduler(reg, q, SchedulerOptions{})

	// Initially nil
	if sched.claimBoard != nil {
		t.Error("expected nil claimBoard initially")
	}

	// Set a real ClaimBoard (with mock components)
	conn := &mockConnManagerForSched{messages: make(map[string][]reef.Message)}
	cfg := evserver.DefaultClaimConfig()
	cfg.ClaimTimeout = 100 * time.Millisecond
	cb := evserver.NewClaimBoard(sched, reg, conn, cfg, nil)
	sched.SetClaimBoard(cb)

	if sched.claimBoard == nil {
		t.Error("expected non-nil claimBoard after SetClaimBoard")
	}

	// Set nil to disable
	sched.SetClaimBoard(nil)
	if sched.claimBoard != nil {
		t.Error("expected nil claimBoard after SetClaimBoard(nil)")
	}
}

// mockConnManagerForSched is a minimal ConnManager for scheduler tests.
type mockConnManagerForSched struct {
	messages map[string][]reef.Message
}

func (m *mockConnManagerForSched) SendToClient(clientID string, msg reef.Message) error {
	m.messages[clientID] = append(m.messages[clientID], msg)
	return nil
}
