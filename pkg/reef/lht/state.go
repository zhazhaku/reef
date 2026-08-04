package lht

import "fmt"

// State represents a lifecycle state in the LHT engine state machine.
// Using string type for human-readable debugging and JSON serialization.
type State string

// The 11 lifecycle states as defined in the LHT spec.
const (
	StateGrounding    State = "GROUNDING"
	StatePlanning     State = "PLANNING"
	StateWaitApproval State = "WAIT_APPROVAL"
	StateExecuting    State = "EXECUTING"
	StateEvaluating   State = "EVALUATING"
	StateReviewing    State = "REVIEWING"
	StateFinalApproval State = "FINAL_APPROVAL"
	StateCompleted    State = "COMPLETED"
	StatePaused       State = "PAUSED"
	StateEscalated    State = "ESCALATED"
	StateAborted      State = "ABORTED"
)

// ErrIllegalTransition is returned when a state transition is not allowed.
var ErrIllegalTransition = fmt.Errorf("illegal state transition")

// transitions defines all legal state transitions.
// Terminal states (COMPLETED, ABORTED) have no entries.
// PAUSED only lists ABORTED as a direct transition; the pre-pause state
// is tracked separately by the engine (see PreviousState / ResumeToState).
var transitions = map[State][]State{
	StateGrounding:    {StatePlanning, StatePaused, StateAborted},
	StatePlanning:     {StateWaitApproval, StatePaused, StateAborted},
	StateWaitApproval: {StateExecuting, StatePlanning, StatePaused, StateAborted},
	StateExecuting:    {StateEvaluating, StatePaused, StateEscalated, StateAborted},
	StateEvaluating:   {StateReviewing, StateExecuting, StatePaused, StateEscalated, StateAborted},
	StateReviewing:    {StateFinalApproval, StateExecuting, StateEscalated, StatePaused, StateAborted},
	StateFinalApproval: {StateCompleted, StateExecuting, StatePaused, StateAborted},
	// COMPLETED: terminal, no transitions
	StatePaused: {StateAborted},
	StateEscalated: {StateExecuting, StateEvaluating, StateReviewing, StateAborted},
	// ABORTED: terminal, no transitions
}

// CanTransition checks whether a transition from one state to another is legal.
// Returns nil if legal, or ErrIllegalTransition if not.
func CanTransition(from, to State) error {
	if from == to {
		return fmt.Errorf("%w: cannot transition from %s to itself", ErrIllegalTransition, from)
	}

	dests, ok := transitions[from]
	if !ok {
		// Terminal state (COMPLETED, ABORTED) — no transitions allowed.
		return fmt.Errorf("%w: %s is a terminal state, cannot transition to %s", ErrIllegalTransition, from, to)
	}

	for _, d := range dests {
		if d == to {
			return nil
		}
	}

	return fmt.Errorf("%w: cannot transition from %s to %s", ErrIllegalTransition, from, to)
}

// IsTerminal returns true if the state is a terminal state (COMPLETED or ABORTED).
func IsTerminal(s State) bool {
	return s == StateCompleted || s == StateAborted
}

// AllStates returns all 11 lifecycle states in declaration order.
func AllStates() []State {
	return []State{
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
}
