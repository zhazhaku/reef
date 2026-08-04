package lht

import (
	"errors"
	"testing"
)

// C1-01: State enumeration completeness and transition table correctness

func TestStateEnumCompleteness(t *testing.T) {
	// All 11 states must be defined as distinct values
	states := []State{
		StateGrounding,
		StatePlanning,
		StateWaitApproval,
		StateExecuting,
		StateEvaluating,
		StateReviewing,
		StateFinalApproval,
		StateCompleted,
		StatePaused,
		StateEscalated,
		StateAborted,
	}

	seen := make(map[State]bool)
	for i, s := range states {
		if s == "" {
			t.Errorf("state at index %d is empty string", i)
		}
		if seen[s] {
			t.Errorf("duplicate state value: %q", s)
		}
		seen[s] = true
	}

	if len(states) != 11 {
		t.Errorf("expected 11 states, got %d", len(states))
	}
}

func TestTransitionTableGrounding(t *testing.T) {
	// GROUNDING → PLANNING | PAUSED | ABORTED
	expected := []State{StatePlanning, StatePaused, StateAborted}
	assertTransitions(t, StateGrounding, expected)
}

func TestTransitionTablePlanning(t *testing.T) {
	// PLANNING → WAIT_APPROVAL | PAUSED | ABORTED
	expected := []State{StateWaitApproval, StatePaused, StateAborted}
	assertTransitions(t, StatePlanning, expected)
}

func TestTransitionTableWaitApproval(t *testing.T) {
	// WAIT_APPROVAL → EXECUTING | PLANNING(打回) | PAUSED | ABORTED
	expected := []State{StateExecuting, StatePlanning, StatePaused, StateAborted}
	assertTransitions(t, StateWaitApproval, expected)
}

func TestTransitionTableExecuting(t *testing.T) {
	// EXECUTING → EVALUATING | PAUSED | ESCALATED | ABORTED
	expected := []State{StateEvaluating, StatePaused, StateEscalated, StateAborted}
	assertTransitions(t, StateExecuting, expected)
}

func TestTransitionTableEvaluating(t *testing.T) {
	// EVALUATING → REVIEWING(PASS) | EXECUTING(FAIL修复) | PAUSED | ESCALATED | ABORTED
	expected := []State{StateReviewing, StateExecuting, StatePaused, StateEscalated, StateAborted}
	assertTransitions(t, StateEvaluating, expected)
}

func TestTransitionTableReviewing(t *testing.T) {
	// REVIEWING → FINAL_APPROVAL(通过) | EXECUTING(打回) | ESCALATED(升级) | PAUSED | ABORTED
	expected := []State{StateFinalApproval, StateExecuting, StateEscalated, StatePaused, StateAborted}
	assertTransitions(t, StateReviewing, expected)
}

func TestTransitionTableFinalApproval(t *testing.T) {
	// FINAL_APPROVAL → COMPLETED(通过) | EXECUTING(打回) | PAUSED | ABORTED
	expected := []State{StateCompleted, StateExecuting, StatePaused, StateAborted}
	assertTransitions(t, StateFinalApproval, expected)
}

func TestTransitionTableCompleted(t *testing.T) {
	// COMPLETED → (终端，不允许转出)
	assertTerminal(t, StateCompleted)
}

func TestTransitionTablePaused(t *testing.T) {
	// PAUSED → 暂停前状态 | ABORTED
	// 暂停前状态是动态的，所以只验证 ABORTED 在转换表中
	// 先验证 ABORTED 在表中
	if !canTransitionDirect(StatePaused, StateAborted) {
		t.Errorf("PAUSED should allow transition to ABORTED")
	}

	// PAUSED 应有且仅有 ABORTED 作为直接可转目标（暂停前状态由引擎层处理）
	dests := transitions[StatePaused]
	if len(dests) != 1 || dests[0] != StateAborted {
		t.Errorf("PAUSED direct transitions should be [ABORTED], got %v", dests)
	}
}

func TestTransitionTableEscalated(t *testing.T) {
	// ESCALATED → EXECUTING/EVALUATING/REVIEWING(用户指示) | ABORTED
	expected := []State{StateExecuting, StateEvaluating, StateReviewing, StateAborted}
	assertTransitions(t, StateEscalated, expected)
}

func TestTransitionTableAborted(t *testing.T) {
	// ABORTED → (终端，不允许转出)
	assertTerminal(t, StateAborted)
}

// --- Illegal transition tests ---

func TestIllegalTransitionCompletedToExecuting(t *testing.T) {
	err := CanTransition(StateCompleted, StateExecuting)
	if err == nil {
		t.Fatal("expected error for COMPLETED → EXECUTING, got nil")
	}
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("expected ErrIllegalTransition, got %v", err)
	}
}

func TestIllegalTransitionAbortedToPlanning(t *testing.T) {
	err := CanTransition(StateAborted, StatePlanning)
	if err == nil {
		t.Fatal("expected error for ABORTED → PLANNING, got nil")
	}
	if !errors.Is(err, ErrIllegalTransition) {
		t.Errorf("expected ErrIllegalTransition, got %v", err)
	}
}

func TestIllegalTransitionGroundingToCompleted(t *testing.T) {
	err := CanTransition(StateGrounding, StateCompleted)
	if err == nil {
		t.Fatal("expected error for GROUNDING → COMPLETED, got nil")
	}
}

func TestIllegalTransitionExecutingToGrounding(t *testing.T) {
	err := CanTransition(StateExecuting, StateGrounding)
	if err == nil {
		t.Fatal("expected error for EXECUTING → GROUNDING, got nil")
	}
}

func TestIllegalTransitionCompletedToAborted(t *testing.T) {
	err := CanTransition(StateCompleted, StateAborted)
	if err == nil {
		t.Fatal("expected error for COMPLETED → ABORTED, got nil")
	}
}

func TestIllegalTransitionSameState(t *testing.T) {
	// 自循环不应被允许（除非 PAUSED→PAUSED 等特殊场景，但设计中不含）
	err := CanTransition(StateExecuting, StateExecuting)
	if err == nil {
		t.Fatal("expected error for EXECUTING → EXECUTING (self-loop), got nil")
	}
}

// --- PAUSED/ESCALATED → ABORTED explicit termination ---

func TestPausedCanAbort(t *testing.T) {
	err := CanTransition(StatePaused, StateAborted)
	if err != nil {
		t.Errorf("PAUSED → ABORTED should be allowed (explicit termination path): %v", err)
	}
}

func TestEscalatedCanAbort(t *testing.T) {
	err := CanTransition(StateEscalated, StateAborted)
	if err != nil {
		t.Errorf("ESCALATED → ABORTED should be allowed (explicit termination path): %v", err)
	}
}

// --- CAN transition (positive cases) ---

func TestCanTransitionValid(t *testing.T) {
	cases := []struct {
		from, to State
	}{
		{StateGrounding, StatePlanning},
		{StateGrounding, StatePaused},
		{StateGrounding, StateAborted},
		{StatePlanning, StateWaitApproval},
		{StatePlanning, StatePaused},
		{StatePlanning, StateAborted},
		{StateWaitApproval, StateExecuting},
		{StateWaitApproval, StatePlanning}, // 打回
		{StateWaitApproval, StatePaused},
		{StateWaitApproval, StateAborted},
		{StateExecuting, StateEvaluating},
		{StateExecuting, StatePaused},
		{StateExecuting, StateEscalated},
		{StateExecuting, StateAborted},
		{StateEvaluating, StateReviewing},
		{StateEvaluating, StateExecuting}, // FAIL修复
		{StateEvaluating, StatePaused},
		{StateEvaluating, StateEscalated},
		{StateEvaluating, StateAborted},
		{StateReviewing, StateFinalApproval},
		{StateReviewing, StateExecuting}, // 打回
		{StateReviewing, StateEscalated}, // 升级
		{StateReviewing, StatePaused},
		{StateReviewing, StateAborted},
		{StateFinalApproval, StateCompleted},
		{StateFinalApproval, StateExecuting}, // 打回
		{StateFinalApproval, StatePaused},
		{StateFinalApproval, StateAborted},
		{StatePaused, StateAborted},
		{StateEscalated, StateExecuting},
		{StateEscalated, StateEvaluating},
		{StateEscalated, StateReviewing},
		{StateEscalated, StateAborted},
	}

	for _, c := range cases {
		t.Run(string(c.from)+"→"+string(c.to), func(t *testing.T) {
			err := CanTransition(c.from, c.to)
			if err != nil {
				t.Errorf("%s → %s should be valid, got: %v", c.from, c.to, err)
			}
		})
	}
}

// --- Error type check ---

func TestErrIllegalTransitionIsError(t *testing.T) {
	var err error = ErrIllegalTransition
	if err.Error() == "" {
		t.Error("ErrIllegalTransition should have a non-empty error message")
	}
}

// --- Self-transition rejection (P2-01) ---

func TestIllegalTransitionSelf(t *testing.T) {
	for _, s := range AllStates() {
		t.Run(string(s), func(t *testing.T) {
			err := CanTransition(s, s)
			if err == nil {
				t.Errorf("self-transition %s → %s should be rejected", s, s)
			}
			if !errors.Is(err, ErrIllegalTransition) {
				t.Errorf("expected ErrIllegalTransition for self-transition %s, got %v", s, err)
			}
		})
	}
}

// --- Helpers ---

func assertTransitions(t *testing.T, from State, expected []State) {
	t.Helper()
	dests, ok := transitions[from]
	if !ok {
		t.Fatalf("no transitions defined for state %q", from)
	}

	// Build set for comparison
	gotSet := make(map[State]bool)
	for _, d := range dests {
		gotSet[d] = true
	}

	if len(dests) != len(expected) {
		t.Errorf("%s: expected %d destinations, got %d (expected=%v, got=%v)",
			from, len(expected), len(dests), expected, dests)
		return
	}

	for _, e := range expected {
		if !gotSet[e] {
			t.Errorf("%s: missing expected destination %q in %v", from, e, dests)
		}
	}
}

func assertTerminal(t *testing.T, s State) {
	t.Helper()
	dests, ok := transitions[s]
	if ok && len(dests) > 0 {
		t.Errorf("%s should be terminal (no transitions), got %v", s, dests)
	}
}

func canTransitionDirect(from, to State) bool {
	for _, d := range transitions[from] {
		if d == to {
			return true
		}
	}
	return false
}
