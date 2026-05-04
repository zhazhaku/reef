package e2e

import (
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/server"
)

// TestE2E_TwoClients_DifferentRoles verifies that tasks are routed to
// the correct Client based on role and skill requirements.
func TestE2E_TwoClients_DifferentRoles(t *testing.T) {
	// --- Setup Server ---
	reg := server.NewRegistry(nil)
	queue := server.NewTaskQueue(10, time.Hour)
	sched := server.NewScheduler(reg, queue, server.SchedulerOptions{MaxEscalations: 2})

	// Register a coder client
	reg.Register(&reef.ClientInfo{
		ID:          "coder-1",
		Role:        "coder",
		Skills:      []string{"go", "docker"},
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	// Register an analyst client
	reg.Register(&reef.ClientInfo{
		ID:          "analyst-1",
		Role:        "analyst",
		Skills:      []string{"sql", "python"},
		Capacity:    2,
		CurrentLoad: 0,
		State:       reef.ClientConnected,
	})

	// --- Submit a coding task ---
	codingTask := reef.NewTask("t1", "Write a REST API in Go", "coder", []string{"go"})
	if err := sched.Submit(codingTask); err != nil {
		t.Fatalf("submit coding task: %v", err)
	}
	if codingTask.AssignedClient != "coder-1" {
		t.Errorf("coding task assigned to %s, want coder-1", codingTask.AssignedClient)
	}
	if codingTask.Status != reef.TaskRunning {
		t.Errorf("coding task status = %s, want Running", codingTask.Status)
	}

	// --- Submit an analysis task ---
	analysisTask := reef.NewTask("t2", "Analyze Q1 sales data", "analyst", []string{"sql"})
	if err := sched.Submit(analysisTask); err != nil {
		t.Fatalf("submit analysis task: %v", err)
	}
	if analysisTask.AssignedClient != "analyst-1" {
		t.Errorf("analysis task assigned to %s, want analyst-1", analysisTask.AssignedClient)
	}

	// --- Complete the coding task ---
	if err := sched.HandleTaskCompleted("t1", &reef.TaskResult{Text: "API code generated"}); err != nil {
		t.Fatalf("complete coding task: %v", err)
	}
	if codingTask.Status != reef.TaskCompleted {
		t.Errorf("coding task status = %s, want Completed", codingTask.Status)
	}
	if reg.Get("coder-1").CurrentLoad != 0 {
		t.Errorf("coder load = %d, want 0", reg.Get("coder-1").CurrentLoad)
	}

	// --- Fail the analysis task with escalation ---
	if err := sched.HandleTaskFailed("t2", &reef.TaskError{Type: "execution_error", Message: "db timeout"}, nil); err != nil {
		t.Fatalf("fail analysis task: %v", err)
	}
	// analyst-1 is the only analyst, so no reassignment possible → Failed
	if analysisTask.Status != reef.TaskFailed {
		t.Errorf("analysis task status = %s, want Failed", analysisTask.Status)
	}
}

// TestE2E_ReassignAfterFailure verifies that a failed task is reassigned
// to another available Client with the same role.
func TestE2E_ReassignAfterFailure(t *testing.T) {
	reg := server.NewRegistry(nil)
	queue := server.NewTaskQueue(10, time.Hour)
	sched := server.NewScheduler(reg, queue, server.SchedulerOptions{MaxEscalations: 2})

	reg.Register(&reef.ClientInfo{
		ID:          "coder-1", Role: "coder", Skills: []string{"go"},
		Capacity: 2, CurrentLoad: 0, State: reef.ClientConnected,
	})
	reg.Register(&reef.ClientInfo{
		ID:          "coder-2", Role: "coder", Skills: []string{"go"},
		Capacity: 2, CurrentLoad: 0, State: reef.ClientConnected,
	})

	task := reef.NewTask("t1", "Write tests", "coder", []string{"go"})
	_ = sched.Submit(task)
	firstClient := task.AssignedClient

	// Simulate failure on the first client
	_ = sched.HandleTaskFailed("t1", &reef.TaskError{Type: "execution_error", Message: "oom"}, nil)

	// Task should be queued for reassignment
	if task.Status != reef.TaskQueued {
		t.Errorf("status = %s, want Queued", task.Status)
	}
	if task.EscalationCount != 1 {
		t.Errorf("escalation count = %d, want 1", task.EscalationCount)
	}

	// Trigger re-dispatch manually
	sched.TryDispatch()

	// Should be assigned to the other coder
	if task.AssignedClient == firstClient {
		t.Errorf("task reassigned to same client %s", task.AssignedClient)
	}
	if task.Status != reef.TaskRunning {
		t.Errorf("status = %s, want Running", task.Status)
	}
}

// TestE2E_QueueAndDispatch verifies queuing behavior when no client is
// initially available, then dispatch when one registers.
func TestE2E_QueueAndDispatch(t *testing.T) {
	reg := server.NewRegistry(nil)
	queue := server.NewTaskQueue(10, time.Hour)
	sched := server.NewScheduler(reg, queue, server.SchedulerOptions{})

	task := reef.NewTask("t1", "Deploy service", "coder", []string{"docker"})
	_ = sched.Submit(task)

	// No client available → queued
	if task.Status != reef.TaskQueued {
		t.Errorf("status = %s, want Queued", task.Status)
	}
	if queue.Len() != 1 {
		t.Errorf("queue len = %d, want 1", queue.Len())
	}

	// Register a matching client
	reg.Register(&reef.ClientInfo{
		ID:          "coder-1", Role: "coder", Skills: []string{"docker", "go"},
		Capacity: 2, CurrentLoad: 0, State: reef.ClientConnected,
	})
	sched.HandleClientAvailable("coder-1")
	time.Sleep(50 * time.Millisecond)

	if task.Status != reef.TaskRunning {
		t.Errorf("status = %s, want Running", task.Status)
	}
	if queue.Len() != 0 {
		t.Errorf("queue len = %d, want 0", queue.Len())
	}
}
