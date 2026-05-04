package reef

import (
	"context"
	"testing"
	"time"
)

func TestTaskStatus_IsTerminal(t *testing.T) {
	terminal := []TaskStatus{TaskCompleted, TaskFailed, TaskCancelled}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	nonTerminal := []TaskStatus{TaskCreated, TaskQueued, TaskAssigned, TaskRunning, TaskPaused, TaskEscalated}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%s should not be terminal", s)
		}
	}
}

func TestTaskStatus_CanTransitionTo(t *testing.T) {
	valid := []struct {
		from, to TaskStatus
	}{
		{TaskCreated, TaskQueued},
		{TaskCreated, TaskAssigned},
		{TaskCreated, TaskFailed},
		{TaskQueued, TaskAssigned},
		{TaskQueued, TaskFailed},
		{TaskAssigned, TaskRunning},
		{TaskAssigned, TaskFailed},
		{TaskAssigned, TaskQueued},
		{TaskRunning, TaskCompleted},
		{TaskRunning, TaskFailed},
		{TaskRunning, TaskPaused},
		{TaskRunning, TaskCancelled},
		{TaskPaused, TaskRunning},
		{TaskPaused, TaskFailed},
		{TaskPaused, TaskCancelled},
		{TaskFailed, TaskQueued},
		{TaskFailed, TaskEscalated},
		{TaskEscalated, TaskQueued},
		{TaskEscalated, TaskFailed},
		{TaskEscalated, TaskCancelled},
	}
	for _, tt := range valid {
		if !tt.from.CanTransitionTo(tt.to) {
			t.Errorf("expected %s -> %s to be valid", tt.from, tt.to)
		}
	}

	invalid := []struct {
		from, to TaskStatus
	}{
		{TaskCompleted, TaskRunning},
		{TaskCancelled, TaskRunning},
		{TaskCreated, TaskCompleted},
		{TaskRunning, TaskAssigned},
		{TaskQueued, TaskRunning},
	}
	for _, tt := range invalid {
		if tt.from.CanTransitionTo(tt.to) {
			t.Errorf("expected %s -> %s to be invalid", tt.from, tt.to)
		}
	}
}

func TestNewTask(t *testing.T) {
	task := NewTask("t1", "write a function", "coder", []string{"go"})
	if task.ID != "t1" {
		t.Errorf("ID = %s, want t1", task.ID)
	}
	if task.Status != TaskCreated {
		t.Errorf("Status = %s, want Created", task.Status)
	}
	if task.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", task.MaxRetries)
	}
	if task.TimeoutMs != 600_000 {
		t.Errorf("TimeoutMs = %d, want 600000", task.TimeoutMs)
	}
}

func TestTask_Transition(t *testing.T) {
	task := NewTask("t1", "do something", "coder", nil)

	if err := task.Transition(TaskQueued); err != nil {
		t.Fatalf("Created->Queued: %v", err)
	}
	if task.Status != TaskQueued {
		t.Errorf("status = %s", task.Status)
	}

	if err := task.Transition(TaskAssigned); err != nil {
		t.Fatalf("Queued->Assigned: %v", err)
	}
	if task.AssignedAt == nil {
		t.Error("AssignedAt should be set")
	}

	if err := task.Transition(TaskRunning); err != nil {
		t.Fatalf("Assigned->Running: %v", err)
	}
	if task.StartedAt == nil {
		t.Error("StartedAt should be set")
	}

	if err := task.Transition(TaskCompleted); err != nil {
		t.Fatalf("Running->Completed: %v", err)
	}
	if task.CompletedAt == nil {
		t.Error("CompletedAt should be set")
	}

	// Invalid transition
	if err := task.Transition(TaskRunning); err == nil {
		t.Error("expected error for invalid transition Completed->Running")
	}
}

func TestTask_AddAttempt(t *testing.T) {
	task := NewTask("t1", "test", "coder", nil)
	task.AddAttempt(AttemptRecord{AttemptNumber: 1, Status: "failed"})
	task.AddAttempt(AttemptRecord{AttemptNumber: 2, Status: "success"})
	if len(task.AttemptHistory) != 2 {
		t.Errorf("len(AttemptHistory) = %d, want 2", len(task.AttemptHistory))
	}
}

func TestTaskContext_WithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTaskContext("task-99", cancel)
	wrapped := tc.WithContext(ctx)

	extracted := TaskContextFrom(wrapped)
	if extracted == nil {
		t.Fatal("expected TaskContext to be extracted")
	}
	if extracted.TaskID != "task-99" {
		t.Errorf("TaskID = %s, want task-99", extracted.TaskID)
	}

	// nil context returns nil
	if TaskContextFrom(context.Background()) != nil {
		t.Error("expected nil for context without TaskContext")
	}
}

func TestTaskContext_PauseResumeChannels(t *testing.T) {
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := NewTaskContext("task-1", cancel)

	// Test non-blocking channel reads (channels are empty buffered)
	select {
	case <-tc.IsPaused():
		t.Error("PauseCh should be empty initially")
	default:
	}

	select {
	case <-tc.IsResumed():
		t.Error("ResumeCh should be empty initially")
	default:
	}

	// Write to channels and verify reads
	go func() { tc.PauseCh <- struct{}{} }()
	select {
	case <-tc.IsPaused():
		// ok
	case <-time.After(time.Second):
		t.Error("timeout waiting for PauseCh")
	}

	go func() { tc.ResumeCh <- struct{}{} }()
	select {
	case <-tc.IsResumed():
		// ok
	case <-time.After(time.Second):
		t.Error("timeout waiting for ResumeCh")
	}
}
