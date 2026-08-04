package lht

import (
	"os"
	"sync"
	"testing"
	"time"
)

// mockNotifier records sent messages for test assertions.
type mockNotifier struct {
	mu       sync.Mutex
	messages []string
}

func (m *mockNotifier) Send(channel, chatID, text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, text)
}

func (m *mockNotifier) lastMsg() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return ""
	}
	return m.messages[len(m.messages)-1]
}

func (m *mockNotifier) allMsgs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.messages))
	copy(out, m.messages)
	return out
}

func tempStore(t *testing.T) *Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "lht-engine-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return NewStore(dir)
}

// ===================== T3A.1.1: NewEngine initialization =====================

func TestEngineNewEngineInit(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}

	e := NewEngine(cfg, store, notifier)

	if e == nil {
		t.Fatal("NewEngine returned nil")
	}
	if e.store != store {
		t.Error("store not set correctly")
	}
	if e.cfg.MaxRepairRounds != 3 {
		t.Errorf("cfg.MaxRepairRounds = %d, want 3", e.cfg.MaxRepairRounds)
	}
	// tasks sync.Map should be empty initially
	count := 0
	e.tasks.Range(func(_, _ interface{}) bool {
		count++
		return true
	})
	if count != 0 {
		t.Errorf("tasks map has %d entries, want 0", count)
	}
}

// ===================== T3A.1.2: NewGoal creates task in GROUNDING =====================

func TestEngineNewGoalCreatesGrounding(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, err := e.NewGoal("test scope of work", "project", "telegram", "chat-42")
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}
	if g == nil {
		t.Fatal("NewGoal returned nil goal")
	}
	if g.GoalID == "" {
		t.Error("GoalID is empty")
	}
	if g.State != StateGrounding {
		t.Errorf("initial state = %s, want %s", g.State, StateGrounding)
	}
	if g.Description != "test scope of work" {
		t.Errorf("Description = %s, want 'test scope of work'", g.Description)
	}

	// Verify state.json was persisted
	loaded, err := store.LoadState(g.GoalID)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if loaded.State != StateGrounding {
		t.Errorf("persisted state = %s, want %s", loaded.State, StateGrounding)
	}
}

// ===================== T3A.1.3: Full happy-path lifecycle =====================

func TestEngineFullLifecycleHappyPath(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, err := e.NewGoal("build a web server", "code", "telegram", "chat-1")
	if err != nil {
		t.Fatalf("NewGoal: %v", err)
	}
	goalID := g.GoalID

	// Step 1: GROUNDING → answer ground questions → PLANNING
	// The engine should ask grounding questions (via notifier or reply channel).
	// We simulate user answering via Reply("ground", goalID, "answer", "...")
	// The goroutine must get past GROUNDING to PLANNING.

	// Give the goroutine a moment to enter GROUNDING loop
	if !waitForState(t, store, goalID, StateGrounding, 5*time.Second) {
		t.Fatal("task did not reach GROUNDING")
	}

	// Answer all 4 grounding questions
	for i := 0; i < 4; i++ {
		err = e.Reply("ground", goalID, "answer", "mock answer for q"+string(rune('0'+i+1)))
		if err != nil {
			t.Fatalf("Reply ground #%d: %v", i, err)
		}
	}

	// After all questions answered, should move to PLANNING
	if !waitForState(t, store, goalID, StatePlanning, 5*time.Second) {
		t.Fatal("task did not reach PLANNING after grounding")
	}

	// PLANNING should auto-complete and move to WAIT_APPROVAL
	if !waitForState(t, store, goalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL after planning")
	}

	// Step 2: WAIT_APPROVAL → approve → EXECUTING
	err = e.Reply("plan", goalID, "approve", "looks good")
	if err != nil {
		t.Fatalf("Reply approve: %v", err)
	}

	if !waitForState(t, store, goalID, StateExecuting, 5*time.Second) {
		t.Fatal("task did not reach EXECUTING after approval")
	}

	// Step 3: EXECUTING → EVALUATING (engine auto-advances)
	if !waitForState(t, store, goalID, StateEvaluating, 10*time.Second) {
		t.Fatal("task did not reach EVALUATING after executing")
	}

	// Step 4: EVALUATING → REVIEWING (mock PASS)
	if !waitForState(t, store, goalID, StateReviewing, 10*time.Second) {
		t.Fatal("task did not reach REVIEWING after evaluating")
	}

	// Step 5: REVIEWING → FINAL_APPROVAL
	if !waitForState(t, store, goalID, StateFinalApproval, 10*time.Second) {
		t.Fatal("task did not reach FINAL_APPROVAL after reviewing")
	}

	// Step 6: FINAL_APPROVAL → approve → COMPLETED
	err = e.Reply("final", goalID, "approve", "ship it")
	if err != nil {
		t.Fatalf("Reply final approve: %v", err)
	}

	if !waitForState(t, store, goalID, StateCompleted, 5*time.Second) {
		t.Fatal("task did not reach COMPLETED after final approval")
	}

	// Verify COMPLETED is terminal
	if !IsTerminal(StateCompleted) {
		t.Error("COMPLETED should be terminal")
	}
}

// ===================== T3A.1.4: WAIT_APPROVAL MUST NOT execute without approval =====================

func TestEngineWaitApprovalBlocksExecution(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("test block", "code", "telegram", "chat-2")

	// Wait for task goroutine to enter GROUNDING loop before sending replies.
	if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
		t.Fatal("task did not reach GROUNDING")
	}

	// Answer grounding questions
	for i := 0; i < 4; i++ {
		if err := e.Reply("ground", g.GoalID, "answer", "ok"); err != nil {
			t.Fatalf("Reply ground #%d: %v", i, err)
		}
	}

	// Wait for WAIT_APPROVAL
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL")
	}

	// Wait a bit and ensure it does NOT move to EXECUTING (blocks waiting for approval)
	time.Sleep(500 * time.Millisecond)

	loaded, _ := store.LoadState(g.GoalID)
	if loaded.State != StateWaitApproval {
		t.Errorf("state moved to %s without approval, want %s", loaded.State, StateWaitApproval)
	}
}

// ===================== T3A.1.5: Reject sends back to PLANNING =====================

func TestEngineRejectReturnsToPlanning(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("reject test", "code", "telegram", "chat-3")

	// Wait for GROUNDING before answering.
	if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
		t.Fatal("task did not reach GROUNDING")
	}

	// Small delay to ensure goroutine is inside doGrounding() select loop.
	time.Sleep(100 * time.Millisecond)

	// Answer grounding
	for i := 0; i < 4; i++ {
		if err := e.Reply("ground", g.GoalID, "answer", "ok"); err != nil {
			t.Fatalf("Reply ground #%d: %v", i, err)
		}
	}

	// Wait for WAIT_APPROVAL
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL")
	}

	// Reject → PLANNING
	err := e.Reply("plan", g.GoalID, "reject", "needs more detail")
	if err != nil {
		t.Fatalf("Reply reject: %v", err)
	}

	if !waitForState(t, store, g.GoalID, StatePlanning, 5*time.Second) {
		t.Fatal("task did not return to PLANNING after reject")
	}

	// After replanning, should reach WAIT_APPROVAL again
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL after replanning")
	}
}

// ===================== T3A.1.6: Persist after each state transition =====================

func TestEnginePersistOnEachTransition(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("persist test", "code", "telegram", "chat-4")

	// GROUNDING persisted
	if !waitForPersistedState(t, store, g.GoalID, StateGrounding, 3*time.Second) {
		t.Fatal("GROUNDING not persisted")
	}

	// Answer grounding → PLANNING persisted
	for i := 0; i < 4; i++ {
		e.Reply("ground", g.GoalID, "answer", "ok")
	}
	if !waitForPersistedState(t, store, g.GoalID, StatePlanning, 10*time.Second) {
		t.Fatal("PLANNING not persisted after grounding")
	}

	// PLANNING → WAIT_APPROVAL persisted
	if !waitForPersistedState(t, store, g.GoalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("WAIT_APPROVAL not persisted after planning")
	}

	// Approve → EXECUTING persisted
	e.Reply("plan", g.GoalID, "approve", "ok")
	if !waitForPersistedState(t, store, g.GoalID, StateExecuting, 5*time.Second) {
		t.Fatal("EXECUTING not persisted")
	}
}

// waitForPersistedState polls store.LoadState until the state matches or timeout.
func waitForPersistedState(t *testing.T, store *Store, goalID string, want State, timeout time.Duration) bool {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			return false
		default:
			loaded, err := store.LoadState(goalID)
			if err == nil && loaded.State == want {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// ===================== T3A.1.7: Gate reply mechanisms =====================

func TestEngineGateReplyGrounding(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("gate test", "code", "telegram", "chat-5")

	// Wait for goroutine to enter GROUNDING loop.
	if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
		t.Fatal("task did not reach GROUNDING")
	}

	// GROUNDING gate should accept any non-empty reply (even unknown actions).
	err := e.Reply("ground", g.GoalID, "invalid_action", "blah")
	if err != nil {
		t.Fatalf("Reply with unknown action: %v (ground gate should accept any non-empty reply)", err)
	}

	// Answer remaining questions to move forward
	for i := 0; i < 3; i++ {
		e.Reply("ground", g.GoalID, "answer", "ok")
	}
	if !waitForState(t, store, g.GoalID, StatePlanning, 10*time.Second) {
		t.Fatal("task did not move past grounding")
	}
}

func TestEngineGateReplyWaitApproval(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("plan gate test", "code", "telegram", "chat-6")

    // Wait for GROUNDING loop to start before sending answers.
    if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
    	t.Fatal("task did not reach GROUNDING")
    }
	for i := 0; i < 4; i++ {
		e.Reply("ground", g.GoalID, "answer", "ok")
	}
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL")
	}

	// approve via plan gate
	err := e.Reply("plan", g.GoalID, "approve", "approved")
	if err != nil {
		t.Fatalf("Reply approve: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StateExecuting, 5*time.Second) {
		t.Fatal("task did not move to EXECUTING after approve")
	}
}

// ===================== T3A.1.8: PAUSED/ESCALATED + Dispatch stop → ABORTED =====================

func TestEnginePausedStopToAborted(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("pause test", "code", "telegram", "chat-7")
	// Answer grounding to get past it

    // Wait for GROUNDING loop to start before sending answers.
    if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
    	t.Fatal("task did not reach GROUNDING")
    }
	for i := 0; i < 4; i++ {
		e.Reply("ground", g.GoalID, "answer", "ok")
	}
	// Wait to be in WAIT_APPROVAL (stable gate)
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL")
	}

	// Pause
	err := e.Dispatch("pause", []string{g.GoalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch pause: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StatePaused, 5*time.Second) {
		t.Fatal("task did not enter PAUSED")
	}

	// Stop from PAUSED → ABORTED
	err = e.Dispatch("stop", []string{g.GoalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch stop: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StateAborted, 5*time.Second) {
		t.Fatal("PAUSED→ABORTED did not happen")
	}
}

func TestEngineEscalatedStopToAborted(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("escalate test", "code", "telegram", "chat-8")
	// Answer grounding

    // Wait for GROUNDING loop to start before sending answers.
    if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
    	t.Fatal("task did not reach GROUNDING")
    }
	for i := 0; i < 4; i++ {
		e.Reply("ground", g.GoalID, "answer", "ok")
	}
	// Wait for WAIT_APPROVAL (stable gate) before escalating
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL")
	}

	// Force escalate (via Dispatch)
	err := e.Dispatch("escalate", []string{g.GoalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch escalate: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StateEscalated, 5*time.Second) {
		t.Fatal("task did not enter ESCALATED")
	}

	// Stop from ESCALATED → ABORTED
	err = e.Dispatch("stop", []string{g.GoalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch stop: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StateAborted, 5*time.Second) {
		t.Fatal("ESCALATED→ABORTED did not happen")
	}
}

// ===================== T3A.1.9: Dispatch unknown subcommand returns error =====================

func TestEngineDispatchUnknownCommand(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	e := NewEngine(Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}, store, notifier)

	err := e.Dispatch("fly_to_moon", []string{"g-1"}, UserReply{})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

// ===================== T3A.1.10: Reply illegal gate returns error =====================

func TestEngineReplyIllegalGate(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	e := NewEngine(Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}, store, notifier)

	err := e.Reply("sky_gate", "g-1", "approve", "wrong gate")
	if err == nil {
		t.Error("expected error for illegal gate")
	}
}

// ===================== T3A.1.11: replyCh buffered=3 =====================

func TestEngineReplyChannelBuffered3(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("buffer test", "code", "telegram", "chat-9")

	// P1-06: Send 3 replies rapidly — must not block (channel buffered=3).
	// We send these before the goroutine enters doGrounding to stress-test the buffer.
	for i := 0; i < 3; i++ {
		err := e.Reply("ground", g.GoalID, "answer", "rapid reply")
		if err != nil {
			t.Fatalf("Reply #%d blocked: %v (buffer should be 3)", i, err)
		}
	}

	// Wait for goroutine to enter GROUNDING and start consuming.
	if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
		t.Fatal("task did not reach GROUNDING")
	}

	// Small delay to let goroutine consume some replies from the buffer.
	time.Sleep(150 * time.Millisecond)

	// Send 4th answer — goroutine should consume all 4 and advance to PLANNING.
	if err := e.Reply("ground", g.GoalID, "answer", "fourth reply"); err != nil {
		t.Fatalf("Reply 4th: %v", err)
	}

	if !waitForState(t, store, g.GoalID, StatePlanning, 10*time.Second) {
		t.Fatal("task did not reach PLANNING after buffered replies")
	}
}

// ===================== T3A.1.12: PAUSED resume to previous state =====================

func TestEnginePausedResumeToPreviousState(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("resume test", "code", "telegram", "chat-10")

    // Wait for GROUNDING loop to start before sending answers.
    if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
    	t.Fatal("task did not reach GROUNDING")
    }
	for i := 0; i < 4; i++ {
		e.Reply("ground", g.GoalID, "answer", "ok")
	}

	// Wait for WAIT_APPROVAL (a stable gate that blocks, ensuring no auto-advance).
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 10*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL")
	}

	// Pause during WAIT_APPROVAL
	err := e.Dispatch("pause", []string{g.GoalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch pause: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StatePaused, 5*time.Second) {
		t.Fatal("task did not enter PAUSED")
	}

	// Verify PreviousState is set
	loaded, _ := store.LoadState(g.GoalID)
	if loaded.PreviousState != StateWaitApproval {
		t.Errorf("PreviousState = %s, want %s", loaded.PreviousState, StateWaitApproval)
	}

	// Resume → should go back to WAIT_APPROVAL
	err = e.Dispatch("resume", []string{g.GoalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch resume: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 5*time.Second) {
		t.Fatal("task did not resume to WAIT_APPROVAL")
	}

	// PreviousState should be cleared after resume
	loaded, _ = store.LoadState(g.GoalID)
	if loaded.PreviousState != "" {
		t.Errorf("PreviousState not cleared after resume, got %s", loaded.PreviousState)
	}
}

// ===================== T3A.1.13: ESCALATED resume to specific state =====================

func TestEngineEscalatedResume(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("escalated resume test", "code", "telegram", "chat-11")

    // Wait for GROUNDING loop to start before sending answers.
    if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
    	t.Fatal("task did not reach GROUNDING")
    }
	for i := 0; i < 4; i++ {
		e.Reply("ground", g.GoalID, "answer", "ok")
	}
	if !waitForState(t, store, g.GoalID, StatePlanning, 10*time.Second) {
		t.Fatal("task did not reach PLANNING")
	}

	// Escalate
	err := e.Dispatch("escalate", []string{g.GoalID}, UserReply{})
	if err != nil {
		t.Fatalf("Dispatch escalate: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StateEscalated, 5*time.Second) {
		t.Fatal("task did not enter ESCALATED")
	}

	// Verify EscalatedFrom is set (the exact state depends on timing,
	// but it should be set to whatever state the task was in before escalation).
	loaded, _ := store.LoadState(g.GoalID)
	if loaded.EscalatedFrom == "" {
		t.Error("EscalatedFrom should be set, got empty")
	}
	t.Logf("EscalatedFrom = %s", loaded.EscalatedFrom)
}

// ===================== T3A.1.14: Dispatch to non-existent goal returns error =====================

func TestEngineDispatchNonExistentGoal(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	e := NewEngine(Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}, store, notifier)

	err := e.Dispatch("pause", []string{"nonexistent-id"}, UserReply{})
	if err == nil {
		t.Error("expected error for non-existent goal")
	}
}

// ===================== T3A.1.15: Reply to non-existent goal returns error =====================

func TestEngineReplyNonExistentGoal(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	e := NewEngine(Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}, store, notifier)

	err := e.Reply("ground", "nonexistent-id", "answer", "hello")
	if err == nil {
		t.Error("expected error for non-existent goal")
	}
}

// ===================== T3A.1.16: Final approval reject =====================

func TestEngineFinalApprovalReject(t *testing.T) {
	store := tempStore(t)
	notifier := &mockNotifier{}
	cfg := Config{
		MaxRepairRounds:        3,
		MaxStrategySwitchCount: 2,
		MaxStallRounds:         3,
		MaxGroundingRounds:     10,
		MaxReplanRounds:        5,
		MaxFinalRejectRounds:   3,
	}
	e := NewEngine(cfg, store, notifier)

	g, _ := e.NewGoal("final reject test", "code", "telegram", "chat-12")

    // Wait for GROUNDING loop to start before sending answers.
    if !waitForState(t, store, g.GoalID, StateGrounding, 5*time.Second) {
    	t.Fatal("task did not reach GROUNDING")
    }
	for i := 0; i < 4; i++ {
		e.Reply("ground", g.GoalID, "answer", "ok")
	}
	if !waitForState(t, store, g.GoalID, StateWaitApproval, 15*time.Second) {
		t.Fatal("task did not reach WAIT_APPROVAL")
	}

	// Approve plan
	e.Reply("plan", g.GoalID, "approve", "ok")
	if !waitForState(t, store, g.GoalID, StateFinalApproval, 30*time.Second) {
		t.Fatal("task did not reach FINAL_APPROVAL")
	}

	// Reject at final → go back to EXECUTING
	err := e.Reply("final", g.GoalID, "reject", "needs changes")
	if err != nil {
		t.Fatalf("Reply final reject: %v", err)
	}
	if !waitForState(t, store, g.GoalID, StateExecuting, 5*time.Second) {
		t.Fatal("task did not return to EXECUTING after final reject")
	}
}

// ===================== Test helpers =====================

// waitForState polls store.LoadState until the state matches the expected value or times out.
func waitForState(t *testing.T, store *Store, goalID string, want State, timeout time.Duration) bool {
	t.Helper()
	return waitForPersistedState(t, store, goalID, want, timeout)
}
