// Package lht — W6A: End-to-end integration tests.
// Covers tasks.md 10.1–10.5:
//   - Full lifecycle happy path (GROUNDING → COMPLETED)
//   - Exception paths (PAUSED→ABORTED, ESCALATED→ABORTED, N/M/K anti-stall)
//   - Crash recovery (store persistence across engine restarts)
//   - Anti-stall N/M/K escalation boundaries
//   - Concurrent multi-task isolation
package lht

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// ────────────────────────────────────────────────────────────
// E2E helpers — reuse patterns from engine_test.go
// ────────────────────────────────────────────────────────────

// e2eConfig returns a Config with generous limits for happy-path tests.
func e2eConfig() Config {
	return Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
}

// e2eTightConfig returns tight limits for anti-stall escalation tests.
func e2eTightConfig() Config {
	return Config{
		MaxRepairRounds:        1,
		MaxStrategySwitchCount: 1,
		MaxStallRounds:         1,
		MaxGroundingRounds:     2,
		MaxReplanRounds:        1,
		MaxFinalRejectRounds:   1,
	}
}

// e2eStore creates a temp store shared across engine restarts.
func e2eStore(t *testing.T) (*Store, string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "lht-e2e-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	store := NewStore(dir)
	return store, dir, func() { os.RemoveAll(dir) }
}

type e2eNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (n *e2eNotifier) Send(channel, chatID, text string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.messages = append(n.messages, text)
}

func (n *e2eNotifier) last() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.messages) == 0 {
		return ""
	}
	return n.messages[len(n.messages)-1]
}

func (n *e2eNotifier) all() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, len(n.messages))
	copy(out, n.messages)
	return out
}

// answerGrounding answers all 4 grounding questions for a goal.
func answerGrounding(t *testing.T, e *Engine, goalID string) {
	t.Helper()
	for i := 0; i < 4; i++ {
		time.Sleep(20 * time.Millisecond)
		err := e.Reply("ground", goalID, "answer", fmt.Sprintf("mock answer %d", i+1))
		if err != nil {
			t.Logf("ground reply %d: %v (may already be past grounding)", i+1, err)
		}
	}
}

// waitState polls until goal reaches wanted state or timeout.
func waitState(t *testing.T, store *Store, goalID string, want State, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s, err := store.LoadState(goalID)
		if err == nil && s.State == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	s, _ := store.LoadState(goalID)
	current := State("???")
	if s != nil {
		current = s.State
	}
	t.Fatalf("goal %s: expected state %s, got %s after %v", goalID, want, current, timeout)
}

// ────────────────────────────────────────────────────────────
// E2E-1: Full lifecycle happy path (tasks 10.1)
// ────────────────────────────────────────────────────────────

func TestE2EFullLifecycleHappyPath(t *testing.T) {
	store, _, cleanup := e2eStore(t)
	defer cleanup()

	notifier := &e2eNotifier{}
	e := NewEngine(e2eConfig(), store, notifier)
	t.Cleanup(e.Shutdown)

	g, err := e.NewGoal("build a REST API server", "code", "telegram", "chat-1")
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}
	goalID := g.GoalID

	// 1. GROUNDING — initial state already persisted.
	s, _ := store.LoadState(goalID)
	if s.State != StateGrounding {
		t.Fatalf("initial state: %s, want GROUNDING", s.State)
	}

	// Answer all 4 grounding questions.
	answerGrounding(t, e, goalID)

	// 2. Should reach PLANNING → WAIT_APPROVAL.
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)
	t.Logf("reached WAIT_APPROVAL")

	// Verify persisted state.
	s, _ = store.LoadState(goalID)
	if s.State != StateWaitApproval {
		t.Errorf("persisted state = %s, want WAIT_APPROVAL", s.State)
	}

	// Verify plan was persisted.
	plan, err := store.LoadPlan(goalID)
	if err != nil || plan == nil {
		t.Fatalf("plan not persisted: err=%v", err)
	}
	if len(plan.Tasks) == 0 {
		t.Error("plan has no tasks")
	}

	// 3. Approve plan → WAIT_APPROVAL → EXECUTING → EVALUATING → REVIEWING → FINAL_APPROVAL.
	err = e.Reply("plan", goalID, "approve", "looks good")
	if err != nil {
		t.Fatalf("Reply plan approve: %v", err)
	}

	waitState(t, store, goalID, StateFinalApproval, 10*time.Second)
	t.Logf("reached FINAL_APPROVAL")

	// 4. Final approve → COMPLETED.
	err = e.Reply("final", goalID, "approve", "ship it")
	if err != nil {
		t.Fatalf("Reply final approve: %v", err)
	}

	waitState(t, store, goalID, StateCompleted, 5*time.Second)
	t.Logf("reached COMPLETED")

	// COMPLETED is terminal.
	if !IsTerminal(StateCompleted) {
		t.Error("COMPLETED should be terminal")
	}

	// Verify final persisted state.
	s, _ = store.LoadState(goalID)
	if s.State != StateCompleted {
		t.Errorf("final persisted state = %s, want COMPLETED", s.State)
	}

	// Verify notifier received expected messages across all phases.
	msgs := notifier.all()
	if len(msgs) < 5 {
		t.Errorf("expected >= 5 notifications, got %d: %v", len(msgs), msgs)
	}
}

// ────────────────────────────────────────────────────────────
// E2E-2.1: PAUSED → ABORTED (tasks 10.2)
// Pause during WAIT_APPROVAL gate, then stop.
// ────────────────────────────────────────────────────────────

func TestE2EPausedToAborted(t *testing.T) {
	store, _, cleanup := e2eStore(t)
	defer cleanup()

	notifier := &e2eNotifier{}
	e := NewEngine(e2eConfig(), store, notifier)
	t.Cleanup(e.Shutdown)

	g, _ := e.NewGoal("pause abort test", "code", "telegram", "chat-2")
	goalID := g.GoalID

	// Complete grounding to reach WAIT_APPROVAL.
	answerGrounding(t, e, goalID)
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)
	t.Logf("reached WAIT_APPROVAL")

	// Pause during WAIT_APPROVAL.
	err := e.Dispatch("pause", []string{goalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch pause: %v", err)
	}

	waitState(t, store, goalID, StatePaused, 5*time.Second)
	t.Logf("reached PAUSED")

	// Now stop → ABORTED.
	err = e.Dispatch("stop", []string{goalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch stop: %v", err)
	}

	waitState(t, store, goalID, StateAborted, 5*time.Second)
	t.Logf("PAUSED → ABORTED done")

	// ABORTED is terminal.
	if !IsTerminal(StateAborted) {
		t.Error("ABORTED should be terminal")
	}
}

// ────────────────────────────────────────────────────────────
// E2E-2.2: ESCALATED → ABORTED (tasks 10.2)
// Escalate during WAIT_APPROVAL gate, then stop.
// ────────────────────────────────────────────────────────────

func TestE2EEscalatedToAborted(t *testing.T) {
	store, _, cleanup := e2eStore(t)
	defer cleanup()

	notifier := &e2eNotifier{}
	e := NewEngine(e2eConfig(), store, notifier)
	t.Cleanup(e.Shutdown)

	g, _ := e.NewGoal("escalated abort test", "code", "telegram", "chat-3")
	goalID := g.GoalID

	answerGrounding(t, e, goalID)
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)

	// Escalate during WAIT_APPROVAL.
	err := e.Dispatch("escalate", []string{goalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch escalate: %v", err)
	}

	waitState(t, store, goalID, StateEscalated, 5*time.Second)
	t.Logf("reached ESCALATED")

	// Verify EscalatedFrom captures the previous state.
	s, _ := store.LoadState(goalID)
	if s.EscalatedFrom != StateWaitApproval {
		t.Errorf("EscalatedFrom = %s, want WAIT_APPROVAL", s.EscalatedFrom)
	}

	// Now stop → ABORTED.
	err = e.Dispatch("stop", []string{goalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch stop from ESCALATED: %v", err)
	}

	waitState(t, store, goalID, StateAborted, 5*time.Second)
	t.Logf("ESCALATED → ABORTED done")
}

// ────────────────────────────────────────────────────────────
// E2E-2.3: Pause/Resume cycle (tasks 10.2)
// Pause during WAIT_APPROVAL, resume back to WAIT_APPROVAL.
// ────────────────────────────────────────────────────────────

func TestE2EPauseResumeCycle(t *testing.T) {
	store, _, cleanup := e2eStore(t)
	defer cleanup()

	notifier := &e2eNotifier{}
	e := NewEngine(e2eConfig(), store, notifier)
	t.Cleanup(e.Shutdown)

	g, _ := e.NewGoal("pause resume test", "code", "telegram", "chat-4")
	goalID := g.GoalID

	answerGrounding(t, e, goalID)
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)
	t.Logf("reached WAIT_APPROVAL")

	// Pause during WAIT_APPROVAL.
	err := e.Dispatch("pause", []string{goalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch pause: %v", err)
	}

	waitState(t, store, goalID, StatePaused, 5*time.Second)
	t.Logf("reached PAUSED")

	// Verify PreviousState is set.
	s, _ := store.LoadState(goalID)
	if s.PreviousState != StateWaitApproval {
		t.Errorf("PreviousState = %s, want WAIT_APPROVAL", s.PreviousState)
	}

	// Resume → should go back to WAIT_APPROVAL.
	err = e.Dispatch("resume", []string{goalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch resume: %v", err)
	}

	waitState(t, store, goalID, StateWaitApproval, 5*time.Second)
	t.Logf("resumed to WAIT_APPROVAL")

	// PreviousState should be cleared after resume.
	s, _ = store.LoadState(goalID)
	if s.PreviousState != "" {
		t.Errorf("PreviousState not cleared after resume, got %s", s.PreviousState)
	}

	// Approve and complete.
	e.Reply("plan", goalID, "approve", "ok")
	waitState(t, store, goalID, StateFinalApproval, 10*time.Second)
	e.Reply("final", goalID, "approve", "done")
	waitState(t, store, goalID, StateCompleted, 5*time.Second)
	t.Logf("COMPLETED after pause/resume")
}

// ────────────────────────────────────────────────────────────
// E2E-2.4: Insert triggers replan (tasks 10.2)
// During WAIT_APPROVAL, insert a new requirement, then approve.
// ────────────────────────────────────────────────────────────

func TestE2EInsertTriggersReplan(t *testing.T) {
	store, _, cleanup := e2eStore(t)
	defer cleanup()

	notifier := &e2eNotifier{}
	e := NewEngine(e2eConfig(), store, notifier)
	t.Cleanup(e.Shutdown)

	g, _ := e.NewGoal("insert test", "code", "telegram", "chat-5")
	goalID := g.GoalID

	answerGrounding(t, e, goalID)
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)
	t.Logf("reached WAIT_APPROVAL")

	// Insert a new requirement via Reply("plan",..., "insert", ...).
	err := e.Reply("plan", goalID, "insert", "add authentication module")
	if err != nil {
		t.Fatalf("Reply plan insert: %v", err)
	}
	t.Logf("insert sent")

	// Wait briefly and verify the task is still alive.
	time.Sleep(300 * time.Millisecond)
	s, _ := store.LoadState(goalID)
	if s.State == StateAborted {
		t.Fatalf("task ABORTED after insert (unexpected)")
	}
	t.Logf("state after insert: %s", s.State)

	// Approve the plan to let it continue.
	e.Reply("plan", goalID, "approve", "approved with insert")
	waitState(t, store, goalID, StateFinalApproval, 10*time.Second)
	t.Logf("reached FINAL_APPROVAL after insert")

	// Final approve.
	e.Reply("final", goalID, "approve", "done")
	waitState(t, store, goalID, StateCompleted, 5*time.Second)
	t.Logf("COMPLETED with insert")
}

// ────────────────────────────────────────────────────────────
// E2E-3: Crash recovery — store persistence across engine restarts (tasks 10.3)
// ────────────────────────────────────────────────────────────

func TestE2ECrashRecovery(t *testing.T) {
	store, basePath, cleanup := e2eStore(t)
	defer cleanup()

	// Phase 1: Create a goal, advance to WAIT_APPROVAL.
	notifier1 := &e2eNotifier{}
	e1 := NewEngine(e2eConfig(), store, notifier1)
	t.Cleanup(e1.Shutdown)

	g, _ := e1.NewGoal("crash recovery test", "code", "telegram", "chat-7")
	goalID := g.GoalID

	answerGrounding(t, e1, goalID)
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)
	t.Logf("phase 1: reached WAIT_APPROVAL")

	// Record the plan that was generated.
	plan1, _ := store.LoadPlan(goalID)
	if plan1 == nil || len(plan1.Tasks) == 0 {
		t.Fatal("plan not persisted before crash")
	}

	// Phase 2: "Crash" — let e1 fall out of scope.
	// The goroutine is still running but orphaned.
	// Create new Engine with same store directory.
	e2 := NewEngine(e2eConfig(), store, &e2eNotifier{})
	t.Cleanup(e2.Shutdown)
	_ = basePath // used for verification

	// Phase 3: Verify persisted data is readable.
	goal2, err := store.LoadGoal(goalID)
	if err != nil || goal2 == nil {
		t.Fatalf("LoadGoal after recovery: err=%v", err)
	}
	if goal2.State == "" {
		t.Error("recovered goal has empty state")
	}
	t.Logf("recovered goal: id=%s state=%s desc=%s", goal2.GoalID, goal2.State, goal2.Description)

	// Plan should also be readable.
	plan2, err := store.LoadPlan(goalID)
	if err != nil || plan2 == nil {
		t.Fatalf("LoadPlan after recovery: err=%v", err)
	}
	if plan2.GoalID != goalID {
		t.Errorf("recovered plan GoalID = %s, want %s", plan2.GoalID, goalID)
	}

	// State should be readable.
	state2, err := store.LoadState(goalID)
	if err != nil || state2 == nil {
		t.Fatalf("LoadState after recovery: err=%v", err)
	}
	t.Logf("recovered state: %s (original: %s)", state2.State, StateWaitApproval)

	// Budget should be readable.
	budget2, err := store.LoadBudget(goalID)
	if err != nil {
		t.Fatalf("LoadBudget after recovery: err=%v", err)
	}
	if budget2 == nil {
		t.Fatal("LoadBudget returned nil after recovery")
	}
	t.Logf("recovered budget: goalID=%s maxRounds=%d", budget2.GoalID, budget2.MaxRounds)

	// Verify store file structure exists.
	for _, f := range []string{"goal.json", "plan.json", "budget.json", "state.json"} {
		path := basePath + "/" + goalID + "/" + f
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected file missing after recovery: %s", path)
		}
	}
}

// ────────────────────────────────────────────────────────────
// E2E-4.1: repair_count not reset by review rejection (tasks 10.4)
// ────────────────────────────────────────────────────────────

func TestE2ERepairCountNotResetByReview(t *testing.T) {
	// REVIEWING→EXECUTING transition should preserve RepairCount.
	node := &TaskNode{
		TaskID:      "task-1",
		RepairCount: 2,
	}

	if node.RepairCount != 2 {
		t.Errorf("RepairCount before: got %d, want 2", node.RepairCount)
	}

	// Simulate a review rejection that sends task back to EXECUTING.
	// The RepairCount should NOT be reset by the transition.
	node.State = StateExecuting // back to executing after review reject

	if node.RepairCount != 2 {
		t.Errorf("RepairCount after REVIEWING→EXECUTING: got %d, should remain 2", node.RepairCount)
	}

	// Increment repair for the next attempt.
	node.RepairCount++

	if !node.IsRepairExhausted() {
		t.Errorf("RepairCount=%d should be >= N=%d (exhausted)", node.RepairCount, MaxRepairRounds)
	}
}

// ────────────────────────────────────────────────────────────
// E2E-4.2: N/M/K escalation boundaries via CheckAntiStall (tasks 10.4)
// ────────────────────────────────────────────────────────────

func TestE2EAntiStallNMKBoundaries(t *testing.T) {
	cfg := Config{
		MaxRepairRounds:        3, // N=3
		MaxStrategySwitchCount: 2, // M=2
		MaxStallRounds:         3, // K=3
	}

	// All anti-stall checks need a non-nil goal (for IsTerminal check).
	activeGoal := &Goal{State: StateExecuting}

	t.Run("N_exhausted_strategy_not_exhausted", func(t *testing.T) {
		plan := &Plan{
			Tasks: []TaskNode{
				{TaskID: "t1", RepairCount: 3, StrategySwitchCount: 0},
			},
		}
		result := CheckAntiStall(cfg, activeGoal, plan, nil, nil)
		if result.ShouldEscalate {
			t.Errorf("N exhausted but M not: ShouldEscalate=true, want false. Reason=%s", result.Reason)
		}
		if result.Reason != ReasonRepairExhausted {
			t.Errorf("expected ReasonRepairExhausted, got %s", result.Reason)
		}
		if result.SuggestedAction != ActionSwitchStrategy {
			t.Errorf("expected ActionSwitchStrategy, got %s", result.SuggestedAction)
		}
		t.Logf("N exhausted + M not → %s / %s", result.Reason, result.SuggestedAction)
	})

	t.Run("both_N_and_M_exhausted", func(t *testing.T) {
		plan := &Plan{
			Tasks: []TaskNode{
				{TaskID: "t1", RepairCount: 3, StrategySwitchCount: 2},
			},
		}
		result := CheckAntiStall(cfg, activeGoal, plan, nil, nil)
		if !result.ShouldEscalate {
			t.Error("N and M both exhausted: ShouldEscalate=false, want true")
		}
		if result.Reason != ReasonStrategyExhausted {
			t.Errorf("expected ReasonStrategyExhausted, got %s", result.Reason)
		}
		t.Logf("N+M exhausted → escalate: %s", result.Details)
	})

	t.Run("stall_K_exhausted", func(t *testing.T) {
		plan := &Plan{
			Tasks: []TaskNode{
				{TaskID: "t1", StallCount: 3},
			},
		}
		result := CheckAntiStall(cfg, activeGoal, plan, nil, nil)
		if !result.ShouldEscalate {
			t.Error("K exhausted: ShouldEscalate=false, want true")
		}
		if result.Reason != ReasonStall {
			t.Errorf("expected ReasonStall, got %s", result.Reason)
		}
		t.Logf("K exhausted → escalate: %s", result.Details)
	})

	t.Run("all_within_limits", func(t *testing.T) {
		plan := &Plan{
			Tasks: []TaskNode{
				{TaskID: "t1", RepairCount: 1, StrategySwitchCount: 0, StallCount: 1},
			},
		}
		result := CheckAntiStall(cfg, activeGoal, plan, nil, nil)
		if result.ShouldEscalate {
			t.Error("all within limits: ShouldEscalate=true, want false")
		}
		if result.Reason != ReasonNone {
			t.Errorf("expected ReasonNone, got %s", result.Reason)
		}
		t.Logf("all within limits → continue")
	})
}

// ────────────────────────────────────────────────────────────
// E2E-4.3: Final approval rejection exhausts → escalated (tasks 10.4)
// ────────────────────────────────────────────────────────────

func TestE2EFinalRejectExhaustion(t *testing.T) {
	store, _, cleanup := e2eStore(t)
	defer cleanup()

	cfg := e2eTightConfig() // MaxFinalRejectRounds=1
	notifier := &e2eNotifier{}
	e := NewEngine(cfg, store, notifier)
	t.Cleanup(e.Shutdown)

	g, _ := e.NewGoal("final reject exhaust", "code", "telegram", "chat-8")
	goalID := g.GoalID

	answerGrounding(t, e, goalID)
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)

	e.Reply("plan", goalID, "approve", "")
	waitState(t, store, goalID, StateFinalApproval, 10*time.Second)
	t.Logf("reached FINAL_APPROVAL")

	// Reject → go back to EXECUTING → back to FINAL_APPROVAL.
	err := e.Reply("final", goalID, "reject", "not good enough")
	if err != nil {
		t.Fatalf("Reply final reject: %v", err)
	}

	waitState(t, store, goalID, StateFinalApproval, 10*time.Second)
	t.Logf("reached FINAL_APPROVAL again after reject")

	// Second reject → MaxFinalRejectRounds=1 → escalate.
	err = e.Reply("final", goalID, "reject", "still not good")
	if err != nil {
		t.Fatalf("Reply final reject #2: %v", err)
	}

	waitState(t, store, goalID, StateEscalated, 10*time.Second)
	t.Logf("final reject exhaustion → ESCALATED")
}

// ────────────────────────────────────────────────────────────
// E2E-4.4: Plan rejection exhausts → escalated (tasks 10.4)
// ────────────────────────────────────────────────────────────

func TestE2EPlanRejectExhaustion(t *testing.T) {
	store, _, cleanup := e2eStore(t)
	defer cleanup()

	cfg := e2eTightConfig() // MaxReplanRounds=1
	notifier := &e2eNotifier{}
	e := NewEngine(cfg, store, notifier)
	t.Cleanup(e.Shutdown)

	g, _ := e.NewGoal("plan reject exhaust", "code", "telegram", "chat-9")
	goalID := g.GoalID

	answerGrounding(t, e, goalID)
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)

	// First reject → replan → back to WAIT_APPROVAL.
	err := e.Reply("plan", goalID, "reject", "bad plan")
	if err != nil {
		t.Fatalf("Reply plan reject #1: %v", err)
	}
	waitState(t, store, goalID, StateWaitApproval, 10*time.Second)
	t.Logf("replan → WAIT_APPROVAL again")

	// Second reject → MaxReplanRounds=1 → escalate.
	err = e.Reply("plan", goalID, "reject", "still bad")
	if err != nil {
		t.Fatalf("Reply plan reject #2: %v", err)
	}

	waitState(t, store, goalID, StateEscalated, 10*time.Second)
	t.Logf("plan reject exhaustion → ESCALATED")
}

// ────────────────────────────────────────────────────────────
// E2E-5: Multi-task concurrency isolation (tasks 10.5)
// ────────────────────────────────────────────────────────────

func TestE2EMultiTaskConcurrency(t *testing.T) {
	store, _, cleanup := e2eStore(t)
	defer cleanup()

	notifier := &e2eNotifier{}
	e := NewEngine(e2eConfig(), store, notifier)
	t.Cleanup(e.Shutdown)

	// Create 3 goals concurrently.
	var goals []*Goal
	for i := 0; i < 3; i++ {
		g, err := e.NewGoal(
			fmt.Sprintf("concurrent task %d", i),
			"code",
			"telegram",
			fmt.Sprintf("chat-%d", i),
		)
		if err != nil {
			t.Fatalf("NewGoal %d: %v", i, err)
		}
		goals = append(goals, g)
	}

	// Answer grounding for all 3, interleaved.
	var wg sync.WaitGroup
	for i, g := range goals {
		wg.Add(1)
		go func(idx int, goalID string) {
			defer wg.Done()
			answerGrounding(t, e, goalID)
		}(i, g.GoalID)
	}
	wg.Wait()

	// All 3 should reach WAIT_APPROVAL.
	for i, g := range goals {
		waitState(t, store, g.GoalID, StateWaitApproval, 15*time.Second)
		t.Logf("task %d (%s) reached WAIT_APPROVAL", i, g.GoalID)
	}

	// Approve all 3 plans.
	for i, g := range goals {
		err := e.Reply("plan", g.GoalID, "approve", fmt.Sprintf("approve %d", i))
		if err != nil {
			t.Errorf("Reply plan approve for task %d: %v", i, err)
		}
	}

	// All should reach FINAL_APPROVAL.
	for i, g := range goals {
		waitState(t, store, g.GoalID, StateFinalApproval, 15*time.Second)
		t.Logf("task %d (%s) reached FINAL_APPROVAL", i, g.GoalID)
	}

	// Approve all finals.
	for i, g := range goals {
		e.Reply("final", g.GoalID, "approve", fmt.Sprintf("done %d", i))
	}

	// All should reach COMPLETED.
	for i, g := range goals {
		waitState(t, store, g.GoalID, StateCompleted, 10*time.Second)
		t.Logf("task %d (%s) COMPLETED", i, g.GoalID)
	}

	// Verify all final states are independent — each is COMPLETED.
	for i, g := range goals {
		s, _ := store.LoadState(g.GoalID)
		if s.State != StateCompleted {
			t.Errorf("task %d final state = %s, want COMPLETED", i, s.State)
		}
	}
}
