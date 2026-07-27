// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent tests for Healer — self-healing retry engine.

package agent

import (
	"testing"
	"time"
)

// ============================================================================
// Test: NewHealer construction
// ============================================================================

func TestNewHealer(t *testing.T) {
	policy := DefaultRetryPolicy()
	h := NewHealer(policy)

	if h == nil {
		t.Fatal("NewHealer returned nil")
	}
	if h.policy.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", h.policy.MaxRetries)
	}
	if h.QueueDepth() != 0 {
		t.Errorf("initial QueueDepth = %d, want 0", h.QueueDepth())
	}
}

// ============================================================================
// Test: OnTaskFailed — retry decision (below max retries)
// ============================================================================

func TestHealerOnTaskFailedRetry(t *testing.T) {
	h := NewHealer(DefaultRetryPolicy())

	planTask := &PlannedTask{
		ID:           "task-1",
		AttemptCount: 0,
		MaxRetries:   3,
		ErrorMessage: "timeout",
	}

	action := h.OnTaskFailed("t1", planTask)
	if action != HealRetry {
		t.Errorf("OnTaskFailed action = %v, want HealRetry (attempt 0 < max 3)", action)
	}

	// Queue should have 1 operation
	if d := h.QueueDepth(); d != 1 {
		t.Errorf("QueueDepth after retry = %d, want 1", d)
	}
}

// ============================================================================
// Test: OnTaskFailed — abort decision (exceeds max retries)
// ============================================================================

func TestHealerOnTaskFailedAbort(t *testing.T) {
	h := NewHealer(DefaultRetryPolicy())

	planTask := &PlannedTask{
		ID:           "task-2",
		AttemptCount: 5,
		MaxRetries:   3,
		ErrorMessage: "oom",
	}

	action := h.OnTaskFailed("t2", planTask)
	if action != HealAbort {
		t.Errorf("OnTaskFailed action = %v, want HealAbort (attempt 5 >= max 3)", action)
	}
}

// ============================================================================
// Test: Exponential backoff calculation
// ============================================================================

func TestHealerExponentialBackoff(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:   5,
		RetryDelay:   1 * time.Second,
		BackoffFactor: 2.0,
	}
	h := NewHealer(policy)

	planTask := &PlannedTask{
		ID:           "task-3",
		AttemptCount: 0,
		MaxRetries:   5,
	}

	h.OnTaskFailed("t3", planTask)
	op := h.Dequeue()
	if op == nil {
		t.Fatal("Dequeue returned nil")
	}

	// Attempt 0 → delay = 1s * 2^0 = 1s
	expectedDelay := 1 * time.Second
	actualDelay := op.NextRetryAt.Sub(op.CreatedAt)
	if actualDelay < expectedDelay-50*time.Millisecond || actualDelay > expectedDelay+100*time.Millisecond {
		t.Errorf("backoff delay ≈ %v, want %v", actualDelay, expectedDelay)
	}
	if op.RetryCount != 1 {
		t.Errorf("RetryCount = %d, want 1", op.RetryCount)
	}
}

// ============================================================================
// Test: OnClientStale
// ============================================================================

func TestHealerOnClientStale(t *testing.T) {
	h := NewHealer(DefaultRetryPolicy())

	action := h.OnClientStale("client-99")
	if action != HealRetry {
		t.Errorf("OnClientStale action = %v, want HealRetry", action)
	}
	if d := h.QueueDepth(); d != 1 {
		t.Errorf("QueueDepth after stale = %d, want 1", d)
	}

	op := h.Dequeue()
	if op == nil {
		t.Fatal("Dequeue returned nil")
	}
	if op.ClientID != "client-99" {
		t.Errorf("ClientID = %q, want client-99", op.ClientID)
	}
	if op.FailReason != "client stale / unresponsive" {
		t.Errorf("FailReason = %q", op.FailReason)
	}
}

// ============================================================================
// Test: Healer queue — Enqueue, Dequeue, QueueDepth
// ============================================================================

func TestHealerQueue(t *testing.T) {
	h := NewHealer(DefaultRetryPolicy())

	// Initially empty
	if d := h.QueueDepth(); d != 0 {
		t.Errorf("QueueDepth = %d, want 0", d)
	}
	if op := h.Dequeue(); op != nil {
		t.Errorf("Dequeue on empty = %v, want nil", op)
	}

	// Enqueue two operations
	now := time.Now()
	h.Enqueue(&HealOperation{TaskID: "a", NextRetryAt: now.Add(-1 * time.Hour), CreatedAt: now})
	h.Enqueue(&HealOperation{TaskID: "b", NextRetryAt: now.Add(1 * time.Hour), CreatedAt: now})

	if d := h.QueueDepth(); d != 2 {
		t.Errorf("QueueDepth = %d, want 2", d)
	}
}

// ============================================================================
// Test: ProcessPending returns only elapsed operations
// ============================================================================

func TestHealerProcessPending(t *testing.T) {
	h := NewHealer(DefaultRetryPolicy())

	now := time.Now()

	// Operation with backoff already expired
	h.Enqueue(&HealOperation{
		TaskID:      "ready-1",
		NextRetryAt: now.Add(-10 * time.Second),
		CreatedAt:   now.Add(-1 * time.Minute),
	})

	// Operation still in backoff
	h.Enqueue(&HealOperation{
		TaskID:      "waiting-1",
		NextRetryAt: now.Add(10 * time.Minute),
		CreatedAt:   now,
	})

	// Operation just expired
	h.Enqueue(&HealOperation{
		TaskID:      "ready-2",
		NextRetryAt: now.Add(-1 * time.Millisecond),
		CreatedAt:   now.Add(-5 * time.Second),
	})

	ops := h.ProcessPending(nil) //nolint:staticcheck

	if len(ops) != 2 {
		t.Fatalf("ProcessPending returned %d ops, want 2", len(ops))
	}
	if ops[0].TaskID != "ready-1" {
		t.Errorf("ops[0].TaskID = %q, want ready-1", ops[0].TaskID)
	}
	if ops[1].TaskID != "ready-2" {
		t.Errorf("ops[1].TaskID = %q, want ready-2", ops[1].TaskID)
	}

	// Queue should still have the waiting op
	if d := h.QueueDepth(); d != 1 {
		t.Errorf("QueueDepth after ProcessPending = %d, want 1", d)
	}
}

// ============================================================================
// Test: ProcessPending on empty queue
// ============================================================================

func TestHealerProcessPendingEmpty(t *testing.T) {
	h := NewHealer(DefaultRetryPolicy())
	ops := h.ProcessPending(nil) //nolint:staticcheck
	if len(ops) != 0 {
		t.Errorf("ProcessPending on empty queue = %d ops, want 0", len(ops))
	}
}

// ============================================================================
// Test: Multiple retries with escalating backoff
// ============================================================================

func TestHealerMultipleRetries(t *testing.T) {
	policy := RetryPolicy{
		MaxRetries:    4,
		RetryDelay:    1 * time.Second,
		BackoffFactor: 2.0,
	}
	h := NewHealer(policy)

	planTask := &PlannedTask{
		ID:           "task-r",
		AttemptCount: 0,
		MaxRetries:   4,
	}

	// First retry
	h.OnTaskFailed("t-r-1", planTask)
	op1 := h.Dequeue()
	if op1.RetryCount != 1 {
		t.Errorf("retry 1 count = %d, want 1", op1.RetryCount)
	}

	// Second retry
	planTask.AttemptCount = 1
	h.OnTaskFailed("t-r-2", planTask)
	op2 := h.Dequeue()
	if op2.RetryCount != 2 {
		t.Errorf("retry 2 count = %d, want 2", op2.RetryCount)
	}
	// Backoff should be larger: 1s * 2^1 = 2s
	delay2 := op2.NextRetryAt.Sub(op2.CreatedAt)
	if delay2 < 2*time.Second-50*time.Millisecond || delay2 > 2100*time.Millisecond {
		t.Errorf("retry 2 backoff ≈ %v, want ~2s", delay2)
	}

	// Fourth retry (last)
	planTask.AttemptCount = 3
	h.OnTaskFailed("t-r-3", planTask)
	op3 := h.Dequeue()
	if op3.RetryCount != 4 {
		t.Errorf("retry 4 count = %d, want 4", op3.RetryCount)
	}
	// Backoff: 1s * 2^3 = 8s
	delay3 := op3.NextRetryAt.Sub(op3.CreatedAt)
	if delay3 < 8*time.Second-50*time.Millisecond || delay3 > 8100*time.Millisecond {
		t.Errorf("retry 4 backoff ≈ %v, want ~8s", delay3)
	}

	// Fifth attempt should abort
	planTask.AttemptCount = 4
	action := h.OnTaskFailed("t-r-4", planTask)
	if action != HealAbort {
		t.Errorf("5th attempt action = %v, want HealAbort", action)
	}
}
