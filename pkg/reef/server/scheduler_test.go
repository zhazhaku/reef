package server

import (
	"testing"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

func TestScheduler_SubmitAndDispatch(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)

	// Register a client
	reg.Register(&reef.ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Skills:      []string{"go"},
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	var dispatchedTaskID, dispatchedClientID string
	sch := NewScheduler(reg, queue, SchedulerOptions{
		OnDispatch: func(taskID, clientID string) error {
			dispatchedTaskID = taskID
			dispatchedClientID = clientID
			return nil
		},
	})

	task := reef.NewTask("t1", "write code", "coder", []string{"go"})
	if err := sch.Submit(task); err != nil {
		t.Fatalf("submit: %v", err)
	}

	// Task should be dispatched immediately
	if task.Status != reef.TaskRunning {
		t.Errorf("status = %s, want Running", task.Status)
	}
	if task.AssignedClient != "c1" {
		t.Errorf("assigned = %s, want c1", task.AssignedClient)
	}
	if dispatchedTaskID != "t1" {
		t.Errorf("dispatched task = %s, want t1", dispatchedTaskID)
	}
	if dispatchedClientID != "c1" {
		t.Errorf("dispatched client = %s, want c1", dispatchedClientID)
	}
}

func TestScheduler_QueueWhenNoMatch(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sch := NewScheduler(reg, queue, SchedulerOptions{})

	task := reef.NewTask("t1", "write code", "coder", nil)
	if err := sch.Submit(task); err != nil {
		t.Fatalf("submit: %v", err)
	}

	if task.Status != reef.TaskQueued {
		t.Errorf("status = %s, want Queued", task.Status)
	}
	if queue.Len() != 1 {
		t.Errorf("queue len = %d, want 1", queue.Len())
	}
}

func TestScheduler_DispatchOnClientAvailable(t *testing.T) {
	reg := NewRegistry(nil)
	queue := NewTaskQueue(10, time.Hour)
	sch := NewScheduler(reg, queue, SchedulerOptions{})

	// Submit task before any client exists
	task := reef.NewTask("t1", "write code", "coder", nil)
	_ = sch.Submit(task)

	// Now register a matching client
	reg.Register(&reef.ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	// Trigger dispatch
	sch.HandleClientAvailable("c1")
	time.Sleep(100 * time.Millisecond)

	if task.Status != reef.TaskRunning {
		t.Errorf("status = %s, want Running", task.Status)
	}
}

func TestScheduler_HandleTaskCompleted(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	queue := NewTaskQueue(10, time.Hour)
	sch := NewScheduler(reg, queue, SchedulerOptions{})

	task := reef.NewTask("t1", "write code", "coder", nil)
	_ = sch.Submit(task)

	if err := sch.HandleTaskCompleted("t1", &reef.TaskResult{Text: "done"}); err != nil {
		t.Fatalf("handle completed: %v", err)
	}

	if task.Status != reef.TaskCompleted {
		t.Errorf("status = %s, want Completed", task.Status)
	}
	if reg.Get("c1").CurrentLoad != 0 {
		t.Errorf("load = %d, want 0", reg.Get("c1").CurrentLoad)
	}
}

func TestScheduler_HandleTaskFailed_EscalationReassign(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})
	reg.Register(&reef.ClientInfo{
		ID:          "c2",
		Role:        "coder",
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	queue := NewTaskQueue(10, time.Hour)
	sch := NewScheduler(reg, queue, SchedulerOptions{MaxEscalations: 2})

	task := reef.NewTask("t1", "write code", "coder", nil)
	_ = sch.Submit(task)

	// Simulate task failure from c1
	err := sch.HandleTaskFailed("t1", &reef.TaskError{Type: "execution_error", Message: "oom"}, nil)
	if err != nil {
		t.Fatalf("handle failed returned error: %v", err)
	}

	// Should be queued for reassignment since c2 is available
	if task.Status != reef.TaskQueued {
		t.Errorf("status = %s, want Queued (for reassignment)", task.Status)
	}
	if task.EscalationCount != 1 {
		t.Errorf("escalation count = %d, want 1", task.EscalationCount)
	}
	// Load on c1 should be decremented
	if reg.Get("c1").CurrentLoad != 0 {
		t.Errorf("c1 load = %d, want 0", reg.Get("c1").CurrentLoad)
	}
}

func TestScheduler_HandleTaskFailed_Terminate(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	queue := NewTaskQueue(10, time.Hour)
	// MaxEscalations > 0 so it doesn't immediately go to ToAdmin;
	// only one client exists so matchClient returns nil -> Terminate.
	sch := NewScheduler(reg, queue, SchedulerOptions{MaxEscalations: 1})

	task := reef.NewTask("t1", "write code", "coder", nil)
	_ = sch.Submit(task)

	err := sch.HandleTaskFailed("t1", &reef.TaskError{Type: "execution_error", Message: "fail"}, nil)
	if err != nil {
		t.Fatalf("handle failed returned error: %v", err)
	}

	if task.Status != reef.TaskFailed {
		t.Errorf("status = %s, want Failed", task.Status)
	}
}

func TestScheduler_HandleTaskFailed_EscalateToAdmin(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	queue := NewTaskQueue(10, time.Hour)
	// MaxEscalations: 0 forces immediate ToAdmin decision.
	sch := NewScheduler(reg, queue, SchedulerOptions{MaxEscalations: 0})

	task := reef.NewTask("t1", "write code", "coder", nil)
	_ = sch.Submit(task)

	err := sch.HandleTaskFailed("t1", &reef.TaskError{Type: "execution_error", Message: "fail"}, nil)
	if err != nil {
		t.Fatalf("handle failed returned error: %v", err)
	}

	if task.Status != reef.TaskEscalated {
		t.Errorf("status = %s, want Escalated", task.Status)
	}
}

func TestScheduler_MatchClient_LoadBalancing(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Capacity:    3,
		CurrentLoad: 2,
		State:       reef.ClientConnected,
	})
	reg.Register(&reef.ClientInfo{
		ID:          "c2",
		Role:        "coder",
		Capacity:    3,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})
	reg.Register(&reef.ClientInfo{
		ID:          "c3",
		Role:        "analyst",
		Capacity:    3,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	queue := NewTaskQueue(10, time.Hour)
	sch := NewScheduler(reg, queue, SchedulerOptions{})

	task := reef.NewTask("t1", "write code", "coder", nil)
	_ = sch.Submit(task)

	// Should dispatch to c2 because it has lower load
	if task.AssignedClient != "c2" {
		t.Errorf("assigned = %s, want c2 (load balancing)", task.AssignedClient)
	}
}

func TestScheduler_MatchClient_Exclusion(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID: "c1", Role: "coder", Capacity: 2, CurrentLoad: 0, State: reef.ClientConnected,
	})

	queue := NewTaskQueue(10, time.Hour)
	sch := NewScheduler(reg, queue, SchedulerOptions{})

	task := reef.NewTask("t1", "write code", "coder", nil)
	// matchClient with exclusion should return nil when c1 is excluded
	matched := sch.matchClient(task, "c1")
	if matched != nil {
		t.Errorf("expected nil when excluding c1, got %s", matched.ID)
	}
	// Without exclusion should match c1
	matched = sch.matchClient(task, "")
	if matched == nil || matched.ID != "c1" {
		t.Errorf("expected c1 without exclusion, got %v", matched)
	}
}

func TestScheduler_MatchClient_SkillFilter(t *testing.T) {
	reg := NewRegistry(nil)
	reg.Register(&reef.ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Skills:      []string{"go"},
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})
	reg.Register(&reef.ClientInfo{
		ID:          "c2",
		Role:        "coder",
		Skills:      []string{"go", "docker"},
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	queue := NewTaskQueue(10, time.Hour)
	sch := NewScheduler(reg, queue, SchedulerOptions{})

	// Task requiring docker should only match c2
	task := reef.NewTask("t1", "deploy", "coder", []string{"docker"})
	_ = sch.Submit(task)

	if task.AssignedClient != "c2" {
		t.Errorf("assigned = %s, want c2 (skill filter)", task.AssignedClient)
	}
}
