package client

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

func TestObserver_SuccessPathCreatesEvent(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: true}, nil)

	task := reef.NewTask("task-1", "fix the bug in auth module", "engineer", []string{"go"})
	task.AssignedClient = "client-1"
	signal := &evolution.EvolutionSignal{
		Task:   task,
		Result: &reef.TaskResult{Text: "Bug fixed: updated auth middleware to handle edge case"},
		ToolCallSummary: []evolution.ToolCallRecord{
			{ToolName: "read_file", Parameters: "{}", Result: "found bug"},
			{ToolName: "edit_file", Parameters: "{}", Result: "fixed"},
		},
		AttemptHistory: []reef.AttemptRecord{
			{AttemptNumber: 0, Status: "success"},
		},
	}

	events, err := obs.ObserveTaskCompleted(context.Background(), signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.EventType != evolution.EventSuccessPattern {
		t.Errorf("expected EventSuccessPattern, got %s", e.EventType)
	}
	if e.TaskID != "task-1" {
		t.Errorf("expected task-1, got %s", e.TaskID)
	}
	if !e.IsValid() {
		t.Errorf("event is not valid")
	}
}

func TestObserver_FailurePathCreatesEvent(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: false}, nil)

	task := reef.NewTask("task-2", "deploy to production", "engineer", []string{"devops"})
	task.AssignedClient = "client-1"
	signal := &evolution.EvolutionSignal{
		Task: task,
		TaskErr: &reef.TaskError{
			Type:    "runtime_error",
			Message: "connection refused: dial tcp 127.0.0.1:443",
		},
		ToolCallSummary: []evolution.ToolCallRecord{
			{ToolName: "exec", Parameters: "{}", Result: "connection error"},
		},
		AttemptHistory: []reef.AttemptRecord{
			{AttemptNumber: 0, Status: "failed", ErrorMessage: "connection refused"},
			{AttemptNumber: 1, Status: "failed", ErrorMessage: "connection refused"},
		},
	}

	events, err := obs.ObserveTaskFailed(context.Background(), signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.EventType != evolution.EventFailurePattern {
		t.Errorf("expected EventFailurePattern, got %s", e.EventType)
	}
	if e.TaskID != "task-2" {
		t.Errorf("expected task-2, got %s", e.TaskID)
	}
	if e.RootCause != "" {
		t.Errorf("expected empty root cause (analysis disabled), got %s", e.RootCause)
	}
	if !e.IsValid() {
		t.Errorf("event is not valid")
	}
}

func TestObserver_BlockingErrorCreatesBlockingEvent(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: false}, nil)

	task := reef.NewTask("task-3", "process data", "engineer", []string{"data"})
	task.AssignedClient = "client-1"
	signal := &evolution.EvolutionSignal{
		Task: task,
		TaskErr: &reef.TaskError{
			Type:    "escalated",
			Message: "task escalated to human operator",
		},
		AttemptHistory: []reef.AttemptRecord{
			{AttemptNumber: 0, Status: "failed"},
		},
	}

	events, err := obs.ObserveTaskFailed(context.Background(), signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	e := events[0]
	if e.EventType != evolution.EventBlockingPattern {
		t.Errorf("expected EventBlockingPattern, got %s", e.EventType)
	}
}

func TestObserver_UnrecoverableMessageCreatesBlockingEvent(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: false}, nil)

	task := reef.NewTask("task-3b", "critical operation", "engineer", nil)
	task.AssignedClient = "client-1"
	signal := &evolution.EvolutionSignal{
		Task: task,
		TaskErr: &reef.TaskError{
			Type:    "permanent",
			Message: "unrecoverable disk corruption detected",
		},
		AttemptHistory: []reef.AttemptRecord{
			{AttemptNumber: 0, Status: "failed"},
		},
	}

	events, err := obs.ObserveTaskFailed(context.Background(), signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := events[0]
	if e.EventType != evolution.EventBlockingPattern {
		t.Errorf("expected EventBlockingPattern for unrecoverable message, got %s", e.EventType)
	}
}

func TestObserver_NilResultReturnsError(t *testing.T) {
	obs := NewObserver(ObserverConfig{}, nil)

	task := reef.NewTask("task-4", "do something", "engineer", nil)
	signal := &evolution.EvolutionSignal{
		Task:   task,
		Result: nil,
	}

	_, err := obs.ObserveTaskCompleted(context.Background(), signal)
	if err == nil {
		t.Fatal("expected error for nil result, got nil")
	}
}

func TestObserver_NilTaskErrReturnsError(t *testing.T) {
	obs := NewObserver(ObserverConfig{}, nil)

	task := reef.NewTask("task-5", "do something", "engineer", nil)
	signal := &evolution.EvolutionSignal{
		Task:    task,
		TaskErr: nil,
	}

	_, err := obs.ObserveTaskFailed(context.Background(), signal)
	if err == nil {
		t.Fatal("expected error for nil task error, got nil")
	}
}

func TestObserver_NilSignalReturnsError(t *testing.T) {
	obs := NewObserver(ObserverConfig{}, nil)

	_, err := obs.ObserveTaskCompleted(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil signal, got nil")
	}

	_, err = obs.ObserveTaskFailed(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil signal, got nil")
	}
}

func TestObserver_LLMUnavailableStillReturnsEvent(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: true}, slog.Default())
	obs.SetRootCauseAnalyzer(func(ctx context.Context, prompt string) (string, error) {
		return "", context.DeadlineExceeded
	})

	task := reef.NewTask("task-6", "complex task", "engineer", nil)
	task.AssignedClient = "client-1"
	signal := &evolution.EvolutionSignal{
		Task: task,
		TaskErr: &reef.TaskError{
			Type:    "runtime_error",
			Message: "something went wrong",
		},
		AttemptHistory: []reef.AttemptRecord{
			{AttemptNumber: 0, Status: "failed"},
		},
	}

	events, err := obs.ObserveTaskFailed(context.Background(), signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event even with LLM failure, got %d", len(events))
	}

	e := events[0]
	if e.RootCause != "llm_unavailable" {
		t.Errorf("expected root cause 'llm_unavailable', got '%s'", e.RootCause)
	}
	if e.Importance != 0.5 {
		t.Errorf("expected importance 0.5 on LLM failure, got %f", e.Importance)
	}
	if !e.IsValid() {
		t.Errorf("event should still be valid after LLM failure")
	}
}

func TestObserver_LLMSuccessSetsImportance(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: true}, slog.Default())
	obs.SetRootCauseAnalyzer(func(ctx context.Context, prompt string) (string, error) {
		if !strings.Contains(prompt, "Analyze this task failure") {
			t.Errorf("prompt should contain analysis instruction")
		}
		return "The auth module has a nil pointer dereference in the middleware chain", nil
	})

	task := reef.NewTask("task-7", "fix auth", "engineer", nil)
	task.AssignedClient = "client-1"
	signal := &evolution.EvolutionSignal{
		Task: task,
		TaskErr: &reef.TaskError{
			Type:    "runtime_error",
			Message: "nil pointer in auth middleware",
		},
		AttemptHistory: []reef.AttemptRecord{
			{AttemptNumber: 0, Status: "failed"},
		},
	}

	events, err := obs.ObserveTaskFailed(context.Background(), signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := events[0]
	if e.RootCause == "" || e.RootCause == "llm_unavailable" {
		t.Errorf("expected LLM root cause, got '%s'", e.RootCause)
	}
	if e.Importance != 0.9 {
		t.Errorf("expected importance 0.9 on LLM success, got %f", e.Importance)
	}
}

func TestObserver_DefaultConfig(t *testing.T) {
	obs := NewObserver(ObserverConfig{}, nil)
	if obs.config.MaxOpsOnFailure != 5 {
		t.Errorf("expected default MaxOpsOnFailure=5, got %d", obs.config.MaxOpsOnFailure)
	}
}

func TestObserver_NilLoggerDefaults(t *testing.T) {
	obs := NewObserver(ObserverConfig{}, nil)
	if obs.logger == nil {
		t.Error("logger should default to slog.Default()")
	}
}

// --------------------------------------------------------------------------
// Signal quality tests (Task 5)
// --------------------------------------------------------------------------

func TestObserverSignalLength(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: false}, nil)

	// Test success signal length
	task := reef.NewTask("task-sig-1", "fix the auth module with a very detailed instruction that explains everything", "engineer", []string{"go", "auth"})
	task.AssignedClient = "client-1"
	signal := &evolution.EvolutionSignal{
		Task:   task,
		Result: &reef.TaskResult{Text: "Successfully completed the fix for the auth module. The bug was in the middleware chain where a nil pointer dereference happened. Fixed by adding a nil check before the dereference. All tests pass now."},
		ToolCallSummary: []evolution.ToolCallRecord{
			{ToolName: "read_file", Parameters: "{}", Result: "found"},
			{ToolName: "edit_file", Parameters: "{}", Result: "fixed"},
			{ToolName: "exec", Parameters: "{}", Result: "tests pass"},
			{ToolName: "read_file", Parameters: "{}", Result: "verified"},
			{ToolName: "edit_file", Parameters: "{}", Result: "finalized"},
		},
	}

	events, err := obs.ObserveTaskCompleted(context.Background(), signal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	if len(events[0].Signal) > 500 {
		t.Errorf("success signal exceeds 500 chars: %d", len(events[0].Signal))
	}

	// Test failure signal length
	failSignal := &evolution.EvolutionSignal{
		Task: task,
		TaskErr: &reef.TaskError{
			Type:    "runtime_error",
			Message: "a very long error message that goes on and on about many different things that could have gone wrong during the execution of this particularly complex task in the system",
		},
		ToolCallSummary: []evolution.ToolCallRecord{
			{ToolName: "tool_a", Parameters: "{}", Result: "x"},
			{ToolName: "tool_b", Parameters: "{}", Result: "x"},
			{ToolName: "tool_c", Parameters: "{}", Result: "x"},
			{ToolName: "tool_d", Parameters: "{}", Result: "x"},
			{ToolName: "tool_e", Parameters: "{}", Result: "x"},
			{ToolName: "tool_f", Parameters: "{}", Result: "x"},
			{ToolName: "tool_g", Parameters: "{}", Result: "x"},
		},
		AttemptHistory: []reef.AttemptRecord{
			{AttemptNumber: 0, Status: "failed"},
			{AttemptNumber: 1, Status: "failed"},
			{AttemptNumber: 2, Status: "failed"},
		},
	}

	events, err = obs.ObserveTaskFailed(context.Background(), failSignal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	if len(events[0].Signal) > 500 {
		t.Errorf("failure signal exceeds 500 chars: %d", len(events[0].Signal))
	}
}

func TestObserverSignalNotEmpty(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: false}, nil)

	// Success
	task := reef.NewTask("task-sig-2", "do it", "engineer", nil)
	task.AssignedClient = "client-1"
	successSignal := &evolution.EvolutionSignal{
		Task:   task,
		Result: &reef.TaskResult{Text: "done"},
	}

	events, err := obs.ObserveTaskCompleted(context.Background(), successSignal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events[0].Signal == "" {
		t.Error("success signal should not be empty")
	}

	// Failure
	failSignal := &evolution.EvolutionSignal{
		Task: task,
		TaskErr: &reef.TaskError{
			Type:    "error",
			Message: "something failed",
		},
	}

	events, err = obs.ObserveTaskFailed(context.Background(), failSignal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events[0].Signal == "" {
		t.Error("failure signal should not be empty")
	}
}

func TestObserverImportanceRange(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: false}, nil)

	// Success importance
	task := reef.NewTask("task-sig-3", "test", "engineer", nil)
	task.AssignedClient = "client-1"
	successSignal := &evolution.EvolutionSignal{
		Task:   task,
		Result: &reef.TaskResult{Text: "done"},
	}

	events, _ := obs.ObserveTaskCompleted(context.Background(), successSignal)
	imp := events[0].Importance
	if imp < 0.0 || imp > 1.0 {
		t.Errorf("success importance out of range: %f", imp)
	}

	// Failure importance
	failSignal := &evolution.EvolutionSignal{
		Task: task,
		TaskErr: &reef.TaskError{
			Type:    "error",
			Message: "failed",
		},
	}

	events, _ = obs.ObserveTaskFailed(context.Background(), failSignal)
	imp = events[0].Importance
	if imp < 0.0 || imp > 1.0 {
		t.Errorf("failure importance out of range: %f", imp)
	}
}

func TestObserverBlockingDetection(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: false}, nil)

	task := reef.NewTask("task-sig-4", "critical", "engineer", nil)
	task.AssignedClient = "client-1"

	// "escalated" error type → blocking
	signal1 := &evolution.EvolutionSignal{
		Task:    task,
		TaskErr: &reef.TaskError{Type: "escalated", Message: "escalated to human"},
	}
	events, _ := obs.ObserveTaskFailed(context.Background(), signal1)
	if events[0].EventType != evolution.EventBlockingPattern {
		t.Errorf("escalated type should produce blocking event, got %s", events[0].EventType)
	}

	// "unrecoverable" in message → blocking
	signal2 := &evolution.EvolutionSignal{
		Task:    reef.NewTask("task-sig-5", "other", "engineer", nil),
		TaskErr: &reef.TaskError{Type: "runtime", Message: "unrecoverable state corruption"},
	}
	signal2.Task.AssignedClient = "client-1"
	events, _ = obs.ObserveTaskFailed(context.Background(), signal2)
	if events[0].EventType != evolution.EventBlockingPattern {
		t.Errorf("unrecoverable message should produce blocking event, got %s", events[0].EventType)
	}

	// Regular error → failure pattern (not blocking)
	signal3 := &evolution.EvolutionSignal{
		Task:    reef.NewTask("task-sig-6", "normal", "engineer", nil),
		TaskErr: &reef.TaskError{Type: "timeout", Message: "request timed out"},
	}
	signal3.Task.AssignedClient = "client-1"
	events, _ = obs.ObserveTaskFailed(context.Background(), signal3)
	if events[0].EventType != evolution.EventFailurePattern {
		t.Errorf("regular error should produce failure pattern, got %s", events[0].EventType)
	}
}

func TestObserverSignal_EmptyToolCallSummary(t *testing.T) {
	obs := NewObserver(ObserverConfig{MaxOpsOnFailure: 5, EnableRootCause: false}, nil)

	task := reef.NewTask("task-sig-7", "simple task", "engineer", nil)
	task.AssignedClient = "client-1"

	// Success with empty tool calls
	successSignal := &evolution.EvolutionSignal{
		Task:   task,
		Result: &reef.TaskResult{Text: "completed"},
	}

	events, err := obs.ObserveTaskCompleted(context.Background(), successSignal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events[0].Signal == "" {
		t.Error("signal should not be empty when tool calls are empty")
	}

	// Failure with empty tool calls
	failSignal := &evolution.EvolutionSignal{
		Task:    task,
		TaskErr: &reef.TaskError{Type: "error", Message: "something broke"},
	}

	events, err = obs.ObserveTaskFailed(context.Background(), failSignal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if events[0].Signal == "" {
		t.Error("signal should not be empty when tool calls are empty")
	}
}
