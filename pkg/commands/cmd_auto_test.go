package commands

import (
	"context"
	"fmt"
	"testing"
)

// ============================================================================
// Helpers
// ============================================================================

// newWiredAutoRT returns a Runtime with all auto callbacks wired to
// reasonable defaults. Individual tests can override specific fields.
func newWiredAutoRT() *Runtime {
	var mode string = "auto"
	var queue []string

	return &Runtime{
		GetAutoStatus: func() interface{} {
			return map[string]interface{}{
				"mode":             mode,
				"state":            "dispatching",
				"tasks_total":      3,
				"tasks_completed":  1,
				"tasks_failed":     0,
				"tasks_running":    1,
				"tasks_pending":    1,
				"clients_active":   2,
				"clients_max":      5,
				"plan_id":          "plan-1",
				"plan_status":      "active",
			}
		},
		SetAutoMode: func(newMode string) (string, error) {
			old := mode
			mode = newMode
			return old, nil
		},
		SetAutoLoopCount: func(count int) {},
		EnqueueAutoMessage: func(instruction string) {
			queue = append(queue, instruction)
		},
		RunAutoStep: func() {},
		StopAuto:    func() {},
		GetAutoQueue: func() []string {
			return queue
		},
		GetAutoHistory: func() []AutoHistoryEntry {
			return nil
		},
	}
}

// execAutoCmd is a helper that executes an auto command and returns the reply.
func execAutoCmd(t *testing.T, rt *Runtime, text string) string {
	t.Helper()
	ex := NewExecutor(NewRegistry(BuiltinDefinitions()), rt)
	var reply string
	res := ex.Execute(context.Background(), Request{
		Text: text,
		Reply: func(msg string) error {
			reply = msg
			return nil
		},
	})
	if res.Outcome != OutcomeHandled {
		t.Fatalf("command %q: outcome=%v, want=%v", text, res.Outcome, OutcomeHandled)
	}
	return reply
}

// ============================================================================
// Test: /auto mode
// ============================================================================

func TestAutoMode_DisplayCurrent(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto mode")
	want := "Current mode: auto (active)"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoMode_SwitchToManual(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto mode manual")
	want := "Switched from auto to manual"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoMode_SwitchToChat(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto mode chat")
	want := "Switched from auto to chat"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoMode_SwitchToHermes(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto mode hermes")
	want := "Switched from auto to hermes"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoMode_Invalid(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto mode bogus")
	want := "Invalid mode: bogus. Valid: auto, manual, chat, hermes"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

// ============================================================================
// Test: /auto run
// ============================================================================

func TestAutoRun_Success(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto run build the project")
	want := "Task queued: build the project"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoRun_LongInstruction(t *testing.T) {
	rt := newWiredAutoRT()
	long := "this is a very long instruction that exceeds sixty characters in length and should be truncated in the display"
	reply := execAutoCmd(t, rt, "/auto run "+long)
	expected := "Task queued: this is a very long instruction that exceeds sixty character..."
	if reply != expected {
		t.Fatalf("reply=%q, want=%q", reply, expected)
	}
}

func TestAutoRun_EmptyInstruction(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto run")
	want := "Usage: /auto run <instruction>"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoRun_WhitespaceOnly(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto run   ")
	want := "Usage: /auto run <instruction>"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

// ============================================================================
// Test: /auto step
// ============================================================================

func TestAutoStep_Success(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto step")
	want := "Step triggered. Check /auto status for results."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

// ============================================================================
// Test: /auto loop
// ============================================================================

func TestAutoLoop_PositiveCount(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto loop 5")
	want := "Loop mode set to: 5 iteration(s)"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoLoop_Single(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto loop 1")
	want := "Loop mode set to: 1 iteration(s)"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoLoop_Infinite(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto loop infinite")
	want := "Loop mode set to: infinite"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoLoop_InfiniteShortForm(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto loop inf")
	want := "Loop mode set to: infinite"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoLoop_Zero(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto loop 0")
	want := "Loop mode set to: infinite"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoLoop_Negative(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto loop -1")
	want := "Invalid count: -1. Use a positive number or 'infinite'."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoLoop_NonNumeric(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto loop abc")
	want := "Invalid count: abc. Use a positive number or 'infinite'."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoLoop_Empty(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto loop")
	want := "Usage: /auto loop <count|infinite>"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

// ============================================================================
// Test: /auto stop
// ============================================================================

func TestAutoStop_Success(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto stop")
	want := "Auto orchestrator stopped."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

// ============================================================================
// Test: /auto status
// ============================================================================

func TestAutoStatus_Success(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto status")
	// Check key fields appear in formatted output
	checks := []string{
		"mode:", "auto",
		"state:", "dispatching",
		"tasks_total:", "3",
		"tasks_completed:", "1",
		"tasks_running:", "1",
		"tasks_pending:", "1",
	}
	for i := 0; i < len(checks); i += 2 {
		if !contains(reply, checks[i]) || !contains(reply, checks[i+1]) {
			t.Errorf("status missing %s %s", checks[i], checks[i+1])
		}
	}
}

// ============================================================================
// Test: /auto queue
// ============================================================================

func TestAutoQueue_Empty(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto queue")
	want := "Queue is empty."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoQueue_NonEmpty(t *testing.T) {
	rt := newWiredAutoRT()
	rt.EnqueueAutoMessage("task one")
	rt.EnqueueAutoMessage("task two")
	rt.EnqueueAutoMessage("task three")
	reply := execAutoCmd(t, rt, "/auto queue")
	// Queue listing should contain messages
	if !contains(reply, "task one") {
		t.Errorf("queue missing 'task one': %q", reply)
	}
	if !contains(reply, "task two") {
		t.Errorf("queue missing 'task two': %q", reply)
	}
	if !contains(reply, "task three") {
		t.Errorf("queue missing 'task three': %q", reply)
	}
}

// ============================================================================
// Test: /auto history
// ============================================================================

func TestAutoHistory_Empty(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/auto history")
	want := "No task history."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoHistory_WithEntries(t *testing.T) {
	rt := newWiredAutoRT()
	rt.GetAutoHistory = func() []AutoHistoryEntry {
		return []AutoHistoryEntry{
			{ID: "t1", Instruction: "compile", Status: "done"},
			{ID: "t2", Instruction: "test", Status: "failed", Error: "timeout"},
		}
	}
	reply := execAutoCmd(t, rt, "/auto history")
	if !contains(reply, "compile") || !contains(reply, "done") {
		t.Errorf("history missing compile/done: %q", reply)
	}
	if !contains(reply, "test") || !contains(reply, "failed") {
		t.Errorf("history missing test/failed: %q", reply)
	}
}

// ============================================================================
// Test: Aliases
// ============================================================================

func TestAlias_Autoloop(t *testing.T) {
	rt := newWiredAutoRT()
	// autoloop is an alias for auto, so sub-command must still be specified
	reply := execAutoCmd(t, rt, "/autoloop loop 3")
	want := "Loop mode set to: 3 iteration(s)"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAlias_Autotask(t *testing.T) {
	rt := newWiredAutoRT()
	reply := execAutoCmd(t, rt, "/autotask run deploy to prod")
	want := "Task queued: deploy to prod"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

// ============================================================================
// Test: Not-wired fallback
// ============================================================================

func TestAutoMode_NotWired(t *testing.T) {
	rt := &Runtime{} // no callbacks
	reply := execAutoCmd(t, rt, "/auto mode")
	want := "Auto mode is not available (AutoLoopOrchestrator not wired)."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoRun_NotWired(t *testing.T) {
	rt := &Runtime{}
	reply := execAutoCmd(t, rt, "/auto run test")
	want := "Auto run is not available (AutoLoopOrchestrator not wired)."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoStep_NotWired(t *testing.T) {
	rt := &Runtime{}
	reply := execAutoCmd(t, rt, "/auto step")
	want := "Auto step is not available (AutoLoopOrchestrator not wired)."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoLoop_NotWired(t *testing.T) {
	rt := &Runtime{}
	reply := execAutoCmd(t, rt, "/auto loop 5")
	want := "Auto loop is not available (AutoLoopOrchestrator not wired)."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoStop_NotWired(t *testing.T) {
	rt := &Runtime{}
	reply := execAutoCmd(t, rt, "/auto stop")
	want := "Auto stop is not available (AutoLoopOrchestrator not wired)."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoStatus_NotWired(t *testing.T) {
	rt := &Runtime{}
	reply := execAutoCmd(t, rt, "/auto status")
	want := "Auto status is not available (GetAutoStatus not wired)."
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

// ============================================================================
// Test: Special cases
// ============================================================================

func TestAutoMode_IdleDisplay(t *testing.T) {
	rt := newWiredAutoRT()
	rt.GetAutoStatus = func() interface{} {
		return map[string]interface{}{
			"mode":  "manual",
			"state": "idle",
		}
	}
	reply := execAutoCmd(t, rt, "/auto mode")
	want := "Current mode: manual"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

func TestAutoMode_SetAutoFailure(t *testing.T) {
	rt := newWiredAutoRT()
	rt.SetAutoMode = func(mode string) (string, error) {
		return "", fmt.Errorf("cannot switch while running")
	}
	reply := execAutoCmd(t, rt, "/auto mode manual")
	want := "Failed to switch mode: cannot switch while running"
	if reply != want {
		t.Fatalf("reply=%q, want=%q", reply, want)
	}
}

// ============================================================================
// Helpers
// ============================================================================

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
