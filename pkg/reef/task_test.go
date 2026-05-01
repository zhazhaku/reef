package reef

import (
	"bytes"
	"context"
	"encoding/json"
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

// ---------------------------------------------------------------------------
// BlockReport tests
// ---------------------------------------------------------------------------

func TestBlockReport_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		br    BlockReport
		valid bool
	}{
		{
			name:  "valid tool_error",
			br:    BlockReport{Type: "tool_error", Message: "tool crashed", Context: "tool_x"},
			valid: true,
		},
		{
			name:  "valid context_corruption",
			br:    BlockReport{Type: "context_corruption", Message: "context overflow", Context: ""},
			valid: true,
		},
		{
			name:  "valid resource_unavailable",
			br:    BlockReport{Type: "resource_unavailable", Message: "GPU OOM"},
			valid: true,
		},
		{
			name:  "valid unknown with empty Context",
			br:    BlockReport{Type: "unknown", Message: "something went wrong"},
			valid: true,
		},
		{
			name:  "empty Type fails",
			br:    BlockReport{Type: "", Message: "msg"},
			valid: false,
		},
		{
			name:  "empty Message fails",
			br:    BlockReport{Type: "tool_error", Message: ""},
			valid: false,
		},
		{
			name:  "unknown Type fails",
			br:    BlockReport{Type: "bad_type", Message: "msg"},
			valid: false,
		},
		{
			name:  "both empty fails",
			br:    BlockReport{Type: "", Message: ""},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.br.IsValid()
			if got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TaskQuality tests
// ---------------------------------------------------------------------------

func TestTaskQuality_IsZero(t *testing.T) {
	tests := []struct {
		name   string
		tq     TaskQuality
		isZero bool
	}{
		{
			name:   "zero-valued returns true",
			tq:     TaskQuality{},
			isZero: true,
		},
		{
			name:   "non-zero Score",
			tq:     TaskQuality{Score: 0.5},
			isZero: false,
		},
		{
			name:   "non-zero SignalsCount",
			tq:     TaskQuality{SignalsCount: 1},
			isZero: false,
		},
		{
			name:   "Evolved=true",
			tq:     TaskQuality{Evolved: true},
			isZero: false,
		},
		{
			name:   "Evolved=true with SignalsCount=0 (valid edge case)",
			tq:     TaskQuality{Evolved: true, SignalsCount: 0, Score: 0},
			isZero: false,
		},
		{
			name:   "all non-zero",
			tq:     TaskQuality{Score: 0.9, SignalsCount: 3, Evolved: true},
			isZero: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tq.IsZero()
			if got != tt.isZero {
				t.Errorf("IsZero() = %v, want %v", got, tt.isZero)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Extended Task JSON round-trip tests
// ---------------------------------------------------------------------------

func TestTaskWithEvolutionFields(t *testing.T) {
	// Test 1: Task with BlockReport and Quality set
	t.Run("with_evolution_fields", func(t *testing.T) {
		task := NewTask("ev-1", "evolve test", "coder", []string{"go"})
		task.BlockReport = &BlockReport{
			Type:    "tool_error",
			Message: "OOM during execution",
			Context: "tool: code_exec",
		}
		task.Quality = &TaskQuality{
			Score:        0.85,
			SignalsCount: 4,
			Evolved:      true,
		}

		data, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		// Verify keys are present
		if !bytes.Contains(data, []byte(`"block_report"`)) {
			t.Error("expected block_report key in JSON")
		}
		if !bytes.Contains(data, []byte(`"quality"`)) {
			t.Error("expected quality key in JSON")
		}

		// Unmarshal back
		var restored Task
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if restored.BlockReport == nil {
			t.Fatal("BlockReport should not be nil")
		}
		if restored.BlockReport.Type != "tool_error" {
			t.Errorf("BlockReport.Type = %q, want tool_error", restored.BlockReport.Type)
		}
		if restored.BlockReport.Message != "OOM during execution" {
			t.Errorf("BlockReport.Message = %q", restored.BlockReport.Message)
		}
		if restored.BlockReport.Context != "tool: code_exec" {
			t.Errorf("BlockReport.Context = %q", restored.BlockReport.Context)
		}

		if restored.Quality == nil {
			t.Fatal("Quality should not be nil")
		}
		if restored.Quality.Score != 0.85 {
			t.Errorf("Quality.Score = %f, want 0.85", restored.Quality.Score)
		}
		if restored.Quality.SignalsCount != 4 {
			t.Errorf("Quality.SignalsCount = %d, want 4", restored.Quality.SignalsCount)
		}
		if !restored.Quality.Evolved {
			t.Error("Quality.Evolved should be true")
		}
	})

	// Test 2: Task without evolution fields — keys absent
	t.Run("without_evolution_fields", func(t *testing.T) {
		task := NewTask("plain-1", "plain task", "coder", nil)

		data, err := json.Marshal(task)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		// Verify keys are absent
		if bytes.Contains(data, []byte(`"block_report"`)) {
			t.Error("block_report key should be absent when nil")
		}
		if bytes.Contains(data, []byte(`"quality"`)) {
			t.Error("quality key should be absent when nil")
		}

		// Unmarshal back
		var restored Task
		if err := json.Unmarshal(data, &restored); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if restored.BlockReport != nil {
			t.Error("BlockReport should be nil")
		}
		if restored.Quality != nil {
			t.Error("Quality should be nil")
		}
	})
}

func TestTaskStateMachineUnchanged(t *testing.T) {
	// Verify all existing state transitions still work for tasks with evolution fields set
	task := NewTask("ev-task", "test evolution fields don't break state machine", "coder", []string{"go"})
	task.BlockReport = &BlockReport{
		Type:    "resource_unavailable",
		Message: "test block",
	}
	task.Quality = &TaskQuality{
		Score:        0.75,
		SignalsCount: 2,
		Evolved:      false,
	}

	// Verify CanTransitionTo is unaffected by evolution fields
	if !TaskCreated.CanTransitionTo(TaskQueued) {
		t.Error("Created->Queued should be valid")
	}
	if !TaskCreated.CanTransitionTo(TaskFailed) {
		t.Error("Created->Failed should be valid")
	}

	// Run a full lifecycle transition chain
	transitions := []TaskStatus{TaskQueued, TaskAssigned, TaskRunning, TaskCompleted}
	for _, to := range transitions {
		if err := task.Transition(to); err != nil {
			t.Fatalf("transition to %s: %v", to, err)
		}
	}

	// Verify fields are preserved
	if task.BlockReport == nil {
		t.Error("BlockReport should be preserved after transitions")
	}
	if task.Quality == nil {
		t.Error("Quality should be preserved after transitions")
	}

	// Terminal state should block further transitions
	if err := task.Transition(TaskRunning); err == nil {
		t.Error("expected error for invalid transition from Completed")
	}

	// Verify BlockReport.Type matches protocol BlockType strings
	validBlockTypes := []string{"tool_error", "context_corruption", "resource_unavailable"}
	for _, bt := range validBlockTypes {
		br := BlockReport{Type: bt, Message: "test"}
		if !br.IsValid() {
			t.Errorf("BlockReport.Type=%q should be valid", bt)
		}
	}
}
