package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sipeed/reef/pkg/reef"
)

// ---------------------------------------------------------------------------
// runWithRetry — delay cap at 30s
// ---------------------------------------------------------------------------

func TestTaskRunner_runWithRetry_DelayCap(t *testing.T) {
	// Use a very large retry delay (31s) so the cap triggers on the first retry
	// 1<<0 * 31s = 31s > 30s → capped to 30s — the cap line executes immediately
	conn := NewConnector(ConnectorOptions{})
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector:  conn,
		RetryDelay: 31 * time.Second, // > 30s so cap triggers on attempt 0
		MaxRetries: 2,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "", errors.New("fail fast")
		},
	})

	ctx, cancel := context.WithCancel(context.Background())

	rt := &RunningTask{
		TaskID:      "t-cap",
		Instruction: "test",
		Ctx:         ctx,
		CancelFunc:  cancel,
		TaskCtx:     reef.NewTaskContext("t-cap", cancel),
		Status:      "running",
	}

	done := make(chan struct{})
	go func() {
		runner.runWithRetry(rt, 2)
		close(done)
	}()

	// Give time for first exec to fail and hit the delay cap
	time.Sleep(50 * time.Millisecond)

	// Cancel to abort the 30s delay
	cancel()

	select {
	case <-done:
		// The cap line (delay = 30 * time.Second) was executed
	case <-time.After(2 * time.Second):
		t.Error("runWithRetry did not exit after cancel")
	}
}

// ---------------------------------------------------------------------------
// runOnce — pause path (PauseCh already signaled)
// ---------------------------------------------------------------------------

func TestTaskRunner_runOnce_PausePath(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "executed", nil
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt := &RunningTask{
		TaskID:      "t-pause",
		Instruction: "test",
		Ctx:         ctx,
		CancelFunc:  cancel,
		TaskCtx:     reef.NewTaskContext("t-pause", cancel),
		Status:      "running",
	}

	// Pre-signal pause so runOnce enters the pause branch
	rt.TaskCtx.PauseCh <- struct{}{}

	// Resume after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		rt.TaskCtx.ResumeCh <- struct{}{}
	}()

	result, err := runner.runOnce(rt)
	if err != nil {
		t.Errorf("runOnce() returned error: %v", err)
	}
	if result != "executed" {
		t.Errorf("runOnce() = %q, want 'executed'", result)
	}
}

// ---------------------------------------------------------------------------
// runOnce — reports "running" after resume
// ---------------------------------------------------------------------------

func TestTaskRunner_runOnce_PauseThenResume(t *testing.T) {
	sendCh := make(chan reef.Message, 10)
	conn := &Connector{sendCh: sendCh}
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
		Exec: func(ctx context.Context, instruction string) (string, error) {
			return "done", nil
		},
	})

	ctx := context.Background()
	rt := &RunningTask{
		TaskID:      "t-pause2",
		Instruction: "test",
		Ctx:         ctx,
		CancelFunc:  func() {},
		TaskCtx:     reef.NewTaskContext("t-pause2", func() {}),
		Status:      "running",
	}

	// Pre-signal pause
	rt.TaskCtx.PauseCh <- struct{}{}

	// Resume after delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		rt.TaskCtx.ResumeCh <- struct{}{}
	}()

	_, err := runner.runOnce(rt)
	if err != nil {
		t.Errorf("runOnce() error: %v", err)
	}

	// Should have at least "paused" and "running" progress messages
	var msgs []reef.Message
	for {
		select {
		case msg := <-sendCh:
			msgs = append(msgs, msg)
		default:
			goto done
		}
	}
done:

	hasPaused := false
	hasRunning := false
	for _, m := range msgs {
		if m.MsgType == reef.MsgTaskProgress {
			var p reef.TaskProgressPayload
			if err := m.DecodePayload(&p); err == nil {
				if p.Status == "paused" {
					hasPaused = true
				}
				if p.Status == "running" && p.Message == "resumed" {
					hasRunning = true
				}
			}
		}
	}
	if !hasPaused {
		t.Error("did not find 'paused' progress message")
	}
	if !hasRunning {
		t.Error("did not find 'running' progress message after resume")
	}
}

// ---------------------------------------------------------------------------
// CancelTask — non-existent task
// ---------------------------------------------------------------------------

func TestTaskRunner_CancelTask_NotFound(t *testing.T) {
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: NewConnector(ConnectorOptions{}),
	})

	if runner.CancelTask("nonexistent") {
		t.Error("CancelTask should return false for non-existent task")
	}
}

// ---------------------------------------------------------------------------
// PauseTask — non-existent task
// ---------------------------------------------------------------------------

func TestTaskRunner_PauseTask_NotFound(t *testing.T) {
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: NewConnector(ConnectorOptions{}),
	})

	if runner.PauseTask("nonexistent") {
		t.Error("PauseTask should return false for non-existent task")
	}
}

// ---------------------------------------------------------------------------
// PauseTask — already paused (channel full)
// ---------------------------------------------------------------------------

func TestTaskRunner_PauseTask_AlreadyPaused(t *testing.T) {
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: NewConnector(ConnectorOptions{}),
	})

	// Manually register a task
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := reef.NewTaskContext("t-already-paused", cancel)

	// Fill the PauseCh (capacity 1)
	tc.PauseCh <- struct{}{}

	rt := &RunningTask{
		TaskID:      "t-already-paused",
		Instruction: "test",
		Ctx:         ctx,
		CancelFunc:  cancel,
		TaskCtx:     tc,
		Status:      "paused",
	}

	runner.mu.Lock()
	runner.tasks["t-already-paused"] = rt
	runner.mu.Unlock()

	// Try to pause again — should fail because channel is full
	if runner.PauseTask("t-already-paused") {
		t.Error("PauseTask should return false when already paused (channel full)")
	}
}

// ---------------------------------------------------------------------------
// ResumeTask — non-existent task
// ---------------------------------------------------------------------------

func TestTaskRunner_ResumeTask_NotFound(t *testing.T) {
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: NewConnector(ConnectorOptions{}),
	})

	if runner.ResumeTask("nonexistent") {
		t.Error("ResumeTask should return false for non-existent task")
	}
}

// ---------------------------------------------------------------------------
// ResumeTask — not paused (channel empty, no receiver)
// ---------------------------------------------------------------------------

func TestTaskRunner_ResumeTask_NotPaused(t *testing.T) {
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: NewConnector(ConnectorOptions{}),
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := reef.NewTaskContext("t-running", cancel)

	// Pre-fill ResumeCh so the send in ResumeTask fails (channel full)
	tc.ResumeCh <- struct{}{}

	rt := &RunningTask{
		TaskID:      "t-running",
		Instruction: "test",
		Ctx:         ctx,
		CancelFunc:  cancel,
		TaskCtx:     tc,
		Status:      "running",
	}

	runner.mu.Lock()
	runner.tasks["t-running"] = rt
	runner.mu.Unlock()

	// Resume when ResumeCh is already full — should fail
	if runner.ResumeTask("t-running") {
		t.Error("ResumeTask should return false when ResumeCh is already full")
	}
}

// ---------------------------------------------------------------------------
// PauseTask then ResumeTask — full success paths
// ---------------------------------------------------------------------------

func TestTaskRunner_PauseThenResume_Success(t *testing.T) {
	conn := NewConnector(ConnectorOptions{})
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc := reef.NewTaskContext("t-pr", cancel)

	rt := &RunningTask{
		TaskID:      "t-pr",
		Instruction: "test",
		Ctx:         ctx,
		CancelFunc:  cancel,
		TaskCtx:     tc,
		Status:      "running",
	}

	runner.mu.Lock()
	runner.tasks["t-pr"] = rt
	runner.mu.Unlock()

	// Pause should succeed
	if !runner.PauseTask("t-pr") {
		t.Error("PauseTask should return true for running task")
	}
	if rt.Status != "paused" {
		t.Errorf("status = %s, want paused", rt.Status)
	}

	// Consume the PauseCh so ResumeCh can be used
	<-tc.PauseCh

	// Resume should succeed
	if !runner.ResumeTask("t-pr") {
		t.Error("ResumeTask should return true for paused task")
	}
	if rt.Status != "running" {
		t.Errorf("status = %s, want running", rt.Status)
	}
}

// ---------------------------------------------------------------------------
// GetTask — non-existent returns nil
// ---------------------------------------------------------------------------

func TestTaskRunner_GetTask_NonExistent(t *testing.T) {
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: NewConnector(ConnectorOptions{}),
	})

	rt := runner.GetTask("nonexistent")
	if rt != nil {
		t.Error("GetTask should return nil for non-existent task")
	}
}

// ---------------------------------------------------------------------------
// Report messages — verify payload content
// ---------------------------------------------------------------------------

func TestTaskRunner_ReportProgress_PayloadContent(t *testing.T) {
	sendCh := make(chan reef.Message, 10)
	conn := &Connector{sendCh: sendCh}
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector:  conn,
		MaxRetries: 1,
	})

	runner.reportProgress("t-rpt", "started", 50, "half done")

	select {
	case msg := <-sendCh:
		if msg.MsgType != reef.MsgTaskProgress {
			t.Errorf("MsgType = %s, want %s", msg.MsgType, reef.MsgTaskProgress)
		}
		var p reef.TaskProgressPayload
		if err := msg.DecodePayload(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if p.TaskID != "t-rpt" {
			t.Errorf("TaskID = %s, want t-rpt", p.TaskID)
		}
		if p.Status != "started" {
			t.Errorf("Status = %s, want started", p.Status)
		}
		if p.ProgressPercent != 50 {
			t.Errorf("ProgressPercent = %d, want 50", p.ProgressPercent)
		}
		if p.Message != "half done" {
			t.Errorf("Message = %s, want 'half done'", p.Message)
		}
	default:
		t.Error("expected progress message in sendCh")
	}
}

func TestTaskRunner_ReportCompleted_PayloadContent(t *testing.T) {
	sendCh := make(chan reef.Message, 10)
	conn := &Connector{sendCh: sendCh}
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
	})

	runner.reportCompleted("t-comp", "result-text", 1234, 3)

	select {
	case msg := <-sendCh:
		if msg.MsgType != reef.MsgTaskCompleted {
			t.Errorf("MsgType = %s, want %s", msg.MsgType, reef.MsgTaskCompleted)
		}
		var p reef.TaskCompletedPayload
		if err := msg.DecodePayload(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if p.TaskID != "t-comp" {
			t.Errorf("TaskID = %s, want t-comp", p.TaskID)
		}
		if p.ExecutionTimeMs != 1234 {
			t.Errorf("ExecutionTimeMs = %d, want 1234", p.ExecutionTimeMs)
		}
		if text, ok := p.Result["text"]; !ok || text != "result-text" {
			t.Errorf("Result = %v, want {'text': 'result-text'}", p.Result)
		}
	default:
		t.Error("expected completed message in sendCh")
	}
}

func TestTaskRunner_ReportFailed_PayloadContent(t *testing.T) {
	sendCh := make(chan reef.Message, 10)
	conn := &Connector{sendCh: sendCh}
	runner := NewTaskRunner(TaskRunnerOptions{
		Connector: conn,
	})

	err := errors.New("something broke")
	attempts := []reef.AttemptRecord{
		{AttemptNumber: 0, Status: "failed", ErrorMessage: "broke"},
	}

	runner.reportFailed("t-fail", err, attempts)

	select {
	case msg := <-sendCh:
		if msg.MsgType != reef.MsgTaskFailed {
			t.Errorf("MsgType = %s, want %s", msg.MsgType, reef.MsgTaskFailed)
		}
		var p reef.TaskFailedPayload
		if err := msg.DecodePayload(&p); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if p.TaskID != "t-fail" {
			t.Errorf("TaskID = %s, want t-fail", p.TaskID)
		}
		if p.ErrorType != "escalated" {
			t.Errorf("ErrorType = %s, want escalated", p.ErrorType)
		}
		if p.ErrorMessage != "something broke" {
			t.Errorf("ErrorMessage = %s, want 'something broke'", p.ErrorMessage)
		}
		if len(p.AttemptHistory) != 1 {
			t.Errorf("AttemptHistory len = %d, want 1", len(p.AttemptHistory))
		}
	default:
		t.Error("expected failed message in sendCh")
	}
}
