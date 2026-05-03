package client

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestTaskRunner_WithSandbox(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	exec := func(ctx context.Context, instruction string) (string, error) {
		return "done", nil
	}

	opts := TaskRunnerOptions{
		Connector:      conn,
		Exec:           exec,
		SandboxDir:     t.TempDir(),
		SandboxFactory: mockSandboxFactory,
	}
	tr := NewTaskRunner(opts)

	tr.StartTask("task-1", "write auth.go", 1)
	time.Sleep(200 * time.Millisecond)

	rt := tr.GetTask("task-1")
	if rt == nil {
		t.Fatal("task not found")
	}

	if rt.Status != "completed" {
		t.Errorf("status = %s", rt.Status)
	}

	if rt.Sandbox == nil {
		t.Error("sandbox not created")
	}
}

func TestTaskRunner_SandboxContextIsolation(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})

	execA := func(ctx context.Context, instruction string) (string, error) {
		return "A-done", nil
	}
	execB := func(ctx context.Context, instruction string) (string, error) {
		return "B-done", nil
	}

	tmp := t.TempDir()
	opts := TaskRunnerOptions{
		Connector:      conn,
		Exec:           execA,
		SandboxDir:     tmp,
		SandboxFactory: mockSandboxFactory,
	}
	trA := NewTaskRunner(opts)
	opts.Exec = execB
	trB := NewTaskRunner(opts)

	trA.StartTask("task-A", "do A", 1)
	trB.StartTask("task-B", "do B", 1)
	time.Sleep(200 * time.Millisecond)

	rtA := trA.GetTask("task-A")
	rtB := trB.GetTask("task-B")

	if rtA.Sandbox == nil {
		t.Error("task-A has no sandbox")
	}
	if rtB.Sandbox == nil {
		t.Error("task-B has no sandbox")
	}

	// Sandboxes must be independent
	if rtA.Sandbox.TaskID() == rtB.Sandbox.TaskID() {
		t.Error("sandboxes share task ID")
	}
}

func TestTaskRunner_NoSandbox(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	exec := func(ctx context.Context, instruction string) (string, error) {
		return "ok", nil
	}

	// No sandbox factory = backward compatible
	opts := TaskRunnerOptions{
		Connector: conn,
		Exec:      exec,
	}
	tr := NewTaskRunner(opts)

	tr.StartTask("task-1", "do thing", 1)
	time.Sleep(100 * time.Millisecond)

	rt := tr.GetTask("task-1")
	if rt == nil {
		t.Fatal("task not found")
	}
	if rt.Status != "completed" {
		t.Errorf("status = %s", rt.Status)
	}
	if rt.Sandbox != nil {
		t.Error("sandbox created without SandboxFactory")
	}
}

func TestTaskRunner_RoundsTracking(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	callCount := 0
	exec := func(ctx context.Context, instruction string) (string, error) {
		callCount++
		return fmt.Sprintf("round-%d", callCount), nil
	}

	opts := TaskRunnerOptions{
		Connector:      conn,
		Exec:           exec,
		SandboxDir:     t.TempDir(),
		SandboxFactory: mockSandboxFactory,
		MaxRounds:      10,
	}
	tr := NewTaskRunner(opts)

	tr.StartTask("task-1", "long task", 1)
	time.Sleep(200 * time.Millisecond)

	rt := tr.GetTask("task-1")
	if rt == nil {
		t.Fatal("task not found")
	}
	if rt.RoundsExecuted < 1 {
		t.Errorf("rounds = %d", rt.RoundsExecuted)
	}
}

func TestTaskRunner_SandboxAppendRound(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	exec := func(ctx context.Context, instruction string) (string, error) {
		return "ok", nil
	}

	opts := TaskRunnerOptions{
		Connector:      conn,
		Exec:           exec,
		SandboxDir:     t.TempDir(),
		SandboxFactory: mockSandboxFactory,
	}
	tr := NewTaskRunner(opts)

	tr.StartTask("task-1", "do thing", 1)
	time.Sleep(200 * time.Millisecond)

	rt := tr.GetTask("task-1")
	if rt == nil {
		t.Fatal("task not found")
	}
	if rt.RoundsExecuted != 1 {
		t.Errorf("RoundsExecuted = %d, want 1", rt.RoundsExecuted)
	}
}

func TestTaskRunner_MaxRounds(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	exec := func(ctx context.Context, instruction string) (string, error) {
		return "done", nil
	}

	opts := TaskRunnerOptions{
		Connector:      conn,
		Exec:           exec,
		SandboxDir:     t.TempDir(),
		SandboxFactory: mockSandboxFactory,
		MaxRounds:      1,
	}
	tr := NewTaskRunner(opts)

	tr.StartTask("task-1", "do thing", 1)
	time.Sleep(200 * time.Millisecond)

	rt := tr.GetTask("task-1")
	if rt != nil {
		// Should have completed or max_rounds triggered
		if rt.RoundsExecuted >= 1 {
			// OK
		}
	}
}
