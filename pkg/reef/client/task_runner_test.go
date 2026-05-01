package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

func TestTaskRunner_Success(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "done: " + instruction, nil
		},
		MaxRetries: 2,
	})

	runner.StartTask("t1", "hello", 0)
	time.Sleep(100 * time.Millisecond)

	rt := runner.GetTask("t1")
	if rt == nil {
		t.Fatal("task not found")
	}
	if rt.Status != "completed" {
		t.Errorf("status = %s, want completed", rt.Status)
	}
	if rt.Result != "done: hello" {
		t.Errorf("result = %s, want 'done: hello'", rt.Result)
	}
}

func TestTaskRunner_RetryThenSuccess(t *testing.T) {
	attempts := 0
	conn := NewConnector(ConnectorOptions{})
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector:  conn,
		RetryDelay: 10 * time.Millisecond,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			attempts++
			if attempts < 2 {
				return "", errors.New("transient error")
			}
			return "success", nil
		},
		MaxRetries: 2,
	})

	runner.StartTask("t1", "test", 0)
	time.Sleep(100 * time.Millisecond)

	rt := runner.GetTask("t1")
	if rt.Status != "completed" {
		t.Errorf("status = %s, want completed", rt.Status)
	}
	if len(rt.Attempts) != 2 {
		t.Errorf("attempts = %d, want 2", len(rt.Attempts))
	}
}

func TestTaskRunner_MaxRetriesExceeded(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector:  conn,
		RetryDelay: 10 * time.Millisecond,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "", errors.New("permanent error")
		},
		MaxRetries: 1,
	})

	runner.StartTask("t1", "test", 0)
	time.Sleep(100 * time.Millisecond)

	rt := runner.GetTask("t1")
	if rt.Status != "failed" {
		t.Errorf("status = %s, want failed", rt.Status)
	}
	if rt.Error == nil {
		t.Error("expected error to be set")
	}
	if len(rt.Attempts) != 2 { // initial + 1 retry
		t.Errorf("attempts = %d, want 2", len(rt.Attempts))
	}
}

func TestTaskRunner_Cancel(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			// Block until cancelled
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Second):
				return "too late", nil
			}
		},
		MaxRetries: 1,
	})

	runner.StartTask("t1", "test", 0)
	time.Sleep(50 * time.Millisecond)

	if !runner.CancelTask("t1") {
		t.Error("cancel should return true")
	}

	time.Sleep(100 * time.Millisecond)
	rt := runner.GetTask("t1")
	if rt.Status != "failed" {
		t.Errorf("status = %s, want failed", rt.Status)
	}
}

func TestTaskRunner_PauseResume(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	resumed := make(chan struct{})

	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			// Wait for resume signal
			select {
			case <-resumed:
				return "done after pause", nil
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(2 * time.Second):
				return "timeout", nil
			}
		},
		MaxRetries: 0,
	})

	runner.StartTask("t1", "test", 0)
	time.Sleep(50 * time.Millisecond)

	if !runner.PauseTask("t1") {
		t.Error("pause should return true")
	}
	time.Sleep(50 * time.Millisecond)

	rt := runner.GetTask("t1")
	if rt.Status != "paused" {
		t.Errorf("status = %s, want paused", rt.Status)
	}

	if !runner.ResumeTask("t1") {
		t.Error("resume should return true")
	}
	close(resumed)

	time.Sleep(100 * time.Millisecond)
	rt = runner.GetTask("t1")
	if rt.Status != "completed" {
		t.Errorf("status = %s, want completed", rt.Status)
	}
}

func TestTaskRunner_ContextCarriesTaskContext(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	var extracted *reef.TaskContext

	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			extracted = reef.TaskContextFrom(ctx)
			return "ok", nil
		},
	})

	runner.StartTask("t99", "test", 0)
	time.Sleep(100 * time.Millisecond)

	if extracted == nil {
		t.Fatal("expected TaskContext in context")
	}
	if extracted.TaskID != "t99" {
		t.Errorf("task_id = %s, want t99", extracted.TaskID)
	}
	if extracted.CancelFunc == nil {
		t.Error("expected CancelFunc to be set")
	}
}

// --------------------------------------------------------------------------
// Evolution observer integration tests
// --------------------------------------------------------------------------

// mockObserver implements evolutionObserver for testing.
type mockObserver struct {
	completedCalls int
	failedCalls    int
	completedSignals []*evolution.EvolutionSignal
	failedSignals    []*evolution.EvolutionSignal
	returnErr        error // non-nil to simulate observer error
}

func (m *mockObserver) ObserveTaskCompleted(ctx context.Context, signal *evolution.EvolutionSignal) ([]*evolution.EvolutionEvent, error) {
	m.completedCalls++
	m.completedSignals = append(m.completedSignals, signal)
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return []*evolution.EvolutionEvent{
		{ID: "evt-success", TaskID: signal.Task.ID, ClientID: "client-test", EventType: evolution.EventSuccessPattern, Importance: 0.7, CreatedAt: time.Now().UTC()},
	}, nil
}

func (m *mockObserver) ObserveTaskFailed(ctx context.Context, signal *evolution.EvolutionSignal) ([]*evolution.EvolutionEvent, error) {
	m.failedCalls++
	m.failedSignals = append(m.failedSignals, signal)
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return []*evolution.EvolutionEvent{
		{ID: "evt-fail", TaskID: signal.Task.ID, ClientID: "client-test", EventType: evolution.EventFailurePattern, Importance: 0.7, CreatedAt: time.Now().UTC()},
	}, nil
}

// mockRecorder implements evolutionRecorder for testing.
type mockRecorder struct {
	events []*evolution.EvolutionEvent
}

func (m *mockRecorder) Record(event *evolution.EvolutionEvent) error {
	m.events = append(m.events, event)
	return nil
}

func TestTaskRunnerWithObserver_Success(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	obs := &mockObserver{}
	rec := &mockRecorder{}

	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "success result", nil
		},
		MaxRetries: 1,
	})
	runner.SetEvolutionObserver(obs)
	runner.SetEvolutionRecorder(rec)

	runner.StartTask("t-obs-1", "test instruction", 0)
	time.Sleep(100 * time.Millisecond)

	rt := runner.GetTask("t-obs-1")
	if rt.Status != "completed" {
		t.Fatalf("expected completed, got %s", rt.Status)
	}

	if obs.completedCalls != 1 {
		t.Errorf("expected 1 observer completed call, got %d", obs.completedCalls)
	}
	if obs.failedCalls != 0 {
		t.Errorf("expected 0 observer failed calls, got %d", obs.failedCalls)
	}
	if len(rec.events) != 1 {
		t.Errorf("expected 1 recorded event, got %d", len(rec.events))
	}
	if rec.events[0].EventType != evolution.EventSuccessPattern {
		t.Errorf("expected success event type, got %s", rec.events[0].EventType)
	}
}

func TestTaskRunnerWithObserver_Failure(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	obs := &mockObserver{}
	rec := &mockRecorder{}

	runner := NewTaskRunner(TaskRunnerOptions{
		Connector:  conn,
		RetryDelay: 10 * time.Millisecond,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "", errors.New("something broke")
		},
		MaxRetries: 1,
	})
	runner.SetEvolutionObserver(obs)
	runner.SetEvolutionRecorder(rec)

	runner.StartTask("t-obs-2", "failing task", 0)
	time.Sleep(100 * time.Millisecond)

	rt := runner.GetTask("t-obs-2")
	if rt.Status != "failed" {
		t.Fatalf("expected failed, got %s", rt.Status)
	}

	if obs.failedCalls != 1 {
		t.Errorf("expected 1 observer failed call, got %d", obs.failedCalls)
	}
	if len(rec.events) != 1 {
		t.Errorf("expected 1 recorded event, got %d", len(rec.events))
	}
}

func TestTaskRunner_NoObserverNoPanic(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})

	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "done", nil
		},
		MaxRetries: 1,
	})
	// No observer or recorder set — should not panic

	runner.StartTask("t-nop-1", "test", 0)
	time.Sleep(100 * time.Millisecond)

	rt := runner.GetTask("t-nop-1")
	if rt.Status != "completed" {
		t.Fatalf("expected completed, got %s", rt.Status)
	}
}

func TestTaskRunner_ObserverErrorStillCompletesTask(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	obs := &mockObserver{returnErr: errors.New("observer error")}
	rec := &mockRecorder{}

	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "result", nil
		},
		MaxRetries: 1,
	})
	runner.SetEvolutionObserver(obs)
	runner.SetEvolutionRecorder(rec)

	runner.StartTask("t-obs-err", "test", 0)
	time.Sleep(100 * time.Millisecond)

	rt := runner.GetTask("t-obs-err")
	if rt.Status != "completed" {
		t.Fatalf("task should still complete when observer errors, got %s", rt.Status)
	}
	// Observer was called but produced no recorded events
	if len(rec.events) != 0 {
		t.Errorf("expected 0 recorded events on observer error, got %d", len(rec.events))
	}
}

func TestTaskRunner_RecordToolCall(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	obs := &mockObserver{}
	rec := &mockRecorder{}

	toolCallsDone := make(chan struct{})

	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			// Wait for tool calls to be recorded from outside
			<-toolCallsDone
			return "done", nil
		},
		MaxRetries: 1,
	})
	runner.SetEvolutionObserver(obs)
	runner.SetEvolutionRecorder(rec)

	runner.StartTask("t-tools-1", "edit file", 0)
	time.Sleep(30 * time.Millisecond)

	// Record tool calls from the test goroutine
	runner.RecordToolCall("t-tools-1", evolution.ToolCallRecord{
		ToolName: "read_file", Parameters: `{"path":"main.go"}`, Result: "content",
	})
	runner.RecordToolCall("t-tools-1", evolution.ToolCallRecord{
		ToolName: "edit_file", Parameters: `{"path":"main.go"}`, Result: "edited",
	})
	close(toolCallsDone)

	time.Sleep(100 * time.Millisecond)

	if len(obs.completedSignals) != 1 {
		t.Fatalf("expected 1 signal, got %d", len(obs.completedSignals))
	}
	signal := obs.completedSignals[0]
	if len(signal.ToolCallSummary) != 2 {
		t.Errorf("expected 2 tool calls in signal, got %d", len(signal.ToolCallSummary))
	}
	if signal.ToolCallSummary[0].ToolName != "read_file" {
		t.Errorf("expected first tool 'read_file', got %s", signal.ToolCallSummary[0].ToolName)
	}
}
