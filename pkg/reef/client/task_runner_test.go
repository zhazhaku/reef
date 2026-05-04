package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
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
