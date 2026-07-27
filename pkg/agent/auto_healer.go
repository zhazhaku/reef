// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent provides the Healer — the self-healing engine for
// failed tasks and stale clients. It manages retry queues, exponential
// backoff, and escalation decisions.
//
// Client 4A — Wave 4A: Self-healing retry engine.

package agent

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"
)

// ============================================================================
// HealAction — result of a healing decision
// ============================================================================

// HealAction represents the outcome of a healing decision.
type HealAction string

const (
	// HealRetry schedules a retry with exponential backoff.
	HealRetry HealAction = "retry"

	// HealEscalate escalates the failure to a human operator or
	// higher-level coordinator for manual intervention.
	HealEscalate HealAction = "escalate"

	// HealAbort marks the task as permanently failed — no further
	// retry attempts are warranted.
	HealAbort HealAction = "abort"
)

// ============================================================================
// HealOperation — a queued healing task
// ============================================================================

// HealOperation represents a pending heal action to be executed.
type HealOperation struct {
	TaskID      string    `json:"task_id"`
	PlanTaskID  string    `json:"plan_task_id"`
	ClientID    string    `json:"client_id"`
	FailReason  string    `json:"fail_reason"`
	RetryCount  int       `json:"retry_count"`
	NextRetryAt time.Time `json:"next_retry_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// ============================================================================
// Healer — self-healing engine
// ============================================================================

// Healer manages the lifecycle of failed tasks: evaluate retry
// eligibility, apply exponential backoff, queue healing operations,
// and process them when their backoff window expires.
type Healer struct {
	mu     sync.Mutex
	policy RetryPolicy              // retry configuration (max retries, backoff)
	queue  []*HealOperation         // pending heal operations
	active map[string]*HealOperation // actively tracked operations by task ID
}

// NewHealer creates a new Healer with the given retry policy.
func NewHealer(policy RetryPolicy) *Healer {
	return &Healer{
		policy: policy,
		queue:  make([]*HealOperation, 0),
		active: make(map[string]*HealOperation),
	}
}

// ============================================================================
// Decision Methods
// ============================================================================

// OnTaskFailed evaluates whether a failed PlannedTask should be retried,
// escalated, or aborted based on the retry policy and attempt count.
//
// Returns:
//   - HealRetry if attemptCount < policy.MaxRetries (with backoff)
//   - HealEscalate if escalation is configured and retries exhausted
//   - HealAbort otherwise
func (h *Healer) OnTaskFailed(taskID string, planTask *PlannedTask) HealAction {
	h.mu.Lock()
	defer h.mu.Unlock()

	attemptCount := planTask.AttemptCount

	// Still within retry budget
	if attemptCount < h.policy.MaxRetries {
		// Calculate backoff delay: RetryDelay * 2^RetryCount
		delay := h.policy.RetryDelay
		if h.policy.BackoffFactor > 0 {
			delay = time.Duration(float64(h.policy.RetryDelay) * math.Pow(h.policy.BackoffFactor, float64(attemptCount)))
		}

		op := &HealOperation{
			TaskID:      taskID,
			PlanTaskID:  planTask.ID,
			ClientID:    planTask.AssignedClient,
			FailReason:  planTask.ErrorMessage,
			RetryCount:  attemptCount + 1,
			NextRetryAt: time.Now().Add(delay),
			CreatedAt:   time.Now(),
		}
		h.enqueueLocked(op)

		return HealRetry
	}

	// Retry budget exhausted
	return HealAbort
}

// OnClientStale handles a client that has become unresponsive (stale).
// It enqueues a restart operation for tasks assigned to that client
// and returns HealRetry.
func (h *Healer) OnClientStale(clientID string) HealAction {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Enqueue a heal operation for the stale client
	op := &HealOperation{
		TaskID:      fmt.Sprintf("stale-%s-%d", clientID, time.Now().UnixNano()),
		ClientID:    clientID,
		FailReason:  "client stale / unresponsive",
		RetryCount:  1,
		NextRetryAt: time.Now().Add(h.policy.RetryDelay),
		CreatedAt:   time.Now(),
	}
	h.enqueueLocked(op)

	return HealRetry
}

// ============================================================================
// Queue Management — public (with locking)
// ============================================================================

// Enqueue adds a heal operation to the pending queue.
func (h *Healer) Enqueue(op *HealOperation) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.enqueueLocked(op)
}

// Dequeue removes and returns the first pending heal operation.
// Returns nil if the queue is empty.
func (h *Healer) Dequeue() *HealOperation {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dequeueLocked()
}

// QueueDepth returns the number of pending heal operations.
func (h *Healer) QueueDepth() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.queue)
}

// ============================================================================
// Queue Management — internal (lock already held)
// ============================================================================

func (h *Healer) enqueueLocked(op *HealOperation) {
	h.queue = append(h.queue, op)
	if op.PlanTaskID != "" {
		h.active[op.PlanTaskID] = op
	}
}

func (h *Healer) dequeueLocked() *HealOperation {
	if len(h.queue) == 0 {
		return nil
	}
	op := h.queue[0]
	h.queue = h.queue[1:]
	if op.PlanTaskID != "" {
		delete(h.active, op.PlanTaskID)
	}
	return op
}

// ============================================================================
// Processing
// ============================================================================

// ProcessPending returns all heal operations whose NextRetryAt has
// elapsed (i.e., their backoff window has expired). The returned
// operations are removed from the queue.
func (h *Healer) ProcessPending(ctx context.Context) []*HealOperation {
	h.mu.Lock()
	defer h.mu.Unlock()

	now := time.Now()
	var ready []*HealOperation
	var remaining []*HealOperation

	for _, op := range h.queue {
		if op.NextRetryAt.Before(now) {
			ready = append(ready, op)
			if op.PlanTaskID != "" {
				delete(h.active, op.PlanTaskID)
			}
		} else {
			remaining = append(remaining, op)
		}
	}

	h.queue = remaining
	return ready
}
