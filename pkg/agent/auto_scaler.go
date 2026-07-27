// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent provides the AutoScaler — the elastic scaling engine
// for the client pool. It evaluates queue depth and idle client counts
// to decide whether to scale up or down, with a cooldown period
// between consecutive scaling actions.
//
// Client 4B — Wave 4A: Elastic scaling engine.

package agent

import (
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// ScaleDecision — result of a scaling evaluation
// ============================================================================

// ScaleDecision represents the outcome of an AutoScaler evaluation.
type ScaleDecision struct {
	// Action is one of "scale_up", "scale_down", or "none".
	Action string `json:"action"`

	// Count is the number of clients to add (scale_up) or remove
	// (scale_down). Always >= 1 for non-none actions.
	Count int `json:"count"`

	// Reason is a human-readable explanation for the decision.
	Reason string `json:"reason"`
}

// ============================================================================
// AutoScaler — elastic scaling engine
// ============================================================================

// AutoScaler evaluates queue pressure and idle client counts to make
// scaling decisions, respecting minimum/maximum client limits and a
// cooldown period between consecutive actions.
type AutoScaler struct {
	mu             sync.Mutex
	minClients     int
	maxClients     int
	idleTimeout    time.Duration
	cooldownUntil  time.Time
	cooldownPeriod time.Duration
	lastScaleTime  time.Time
}

// NewAutoScaler creates a new AutoScaler with the given bounds and
// timeouts.
func NewAutoScaler(minClients, maxClients int, idleTimeout, cooldownPeriod time.Duration) *AutoScaler {
	return &AutoScaler{
		minClients:     minClients,
		maxClients:     maxClients,
		idleTimeout:    idleTimeout,
		cooldownPeriod: cooldownPeriod,
	}
}

// ============================================================================
// Evaluation
// ============================================================================

// Evaluate examines the current system state and returns a scaling
// decision. It checks cooldown first, then evaluates scale-up, then
// scale-down. The first non-none decision wins.
func (as *AutoScaler) Evaluate(queueDepth int, idleClients int) ScaleDecision {
	if as.InCooldown() {
		return ScaleDecision{Action: "none", Reason: "in cooldown period"}
	}

	// Try scale-up first (respond to pressure)
	totalClients := idleClients + 1 // conservative estimate
	if d := as.evaluateScaleUp(queueDepth, totalClients); d != nil {
		return *d
	}

	// Try scale-down (release idle resources)
	if d := as.evaluateScaleDown(idleClients, totalClients); d != nil {
		return *d
	}

	return ScaleDecision{Action: "none", Reason: "no scaling action needed"}
}

// evaluateScaleUp checks whether the queue depth justifies adding
// more clients. Returns nil if no scale-up is warranted.
func (as *AutoScaler) evaluateScaleUp(queueDepth int, currentClients int) *ScaleDecision {
	// Threshold: queue depth exceeds maxClients * 2 and we have room
	if queueDepth <= 0 || queueDepth < as.maxClients*2 {
		return nil
	}
	if currentClients >= as.maxClients {
		return nil
	}

	// Scale up: min(half the queue, remaining capacity)
	newCount := queueDepth / 2
	capacity := as.maxClients - currentClients
	if newCount > capacity {
		newCount = capacity
	}
	if newCount <= 0 {
		return nil
	}

	return &ScaleDecision{
		Action: "scale_up",
		Count:  newCount,
		Reason: fmt.Sprintf("queue_depth=%d exceeds threshold=%d", queueDepth, as.maxClients*2),
	}
}

// evaluateScaleDown checks whether idle clients can be safely removed.
// Returns nil if no scale-down is warranted.
func (as *AutoScaler) evaluateScaleDown(idleClients int, currentClients int) *ScaleDecision {
	if idleClients <= 0 {
		return nil
	}
	if currentClients <= as.minClients {
		return nil
	}

	// Scale down: min(idleClients, excess above min)
	remove := idleClients
	excess := currentClients - as.minClients
	if remove > excess {
		remove = excess
	}
	if remove <= 0 {
		return nil
	}

	return &ScaleDecision{
		Action: "scale_down",
		Count:  remove,
		Reason: fmt.Sprintf("idle_clients=%d, can reduce to min=%d", idleClients, as.minClients),
	}
}

// ============================================================================
// Execution
// ============================================================================

// Execute records a scaling action as having been performed. It sets
// the cooldown timer and updates lastScaleTime.
func (as *AutoScaler) Execute(decision ScaleDecision) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.cooldownUntil = time.Now().Add(as.cooldownPeriod)
	as.lastScaleTime = time.Now()
}

// ============================================================================
// Cooldown
// ============================================================================

// InCooldown returns true if the scaler is currently within the
// cooldown period and should not make new scaling decisions.
func (as *AutoScaler) InCooldown() bool {
	as.mu.Lock()
	defer as.mu.Unlock()
	return time.Now().Before(as.cooldownUntil)
}
