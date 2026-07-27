// Reef - Distributed multi-agent swarm orchestration system
//
// Package agent tests for AutoScaler — elastic scaling engine.

package agent

import (
	"testing"
	"time"
)

// ============================================================================
// Test: NewAutoScaler construction
// ============================================================================

func TestNewAutoScaler(t *testing.T) {
	as := NewAutoScaler(1, 5, 5*time.Minute, 30*time.Second)
	if as == nil {
		t.Fatal("NewAutoScaler returned nil")
	}
	if as.minClients != 1 {
		t.Errorf("minClients = %d, want 1", as.minClients)
	}
	if as.maxClients != 5 {
		t.Errorf("maxClients = %d, want 5", as.maxClients)
	}
}

// ============================================================================
// Test: Scale up when queue depth exceeds threshold
// ============================================================================

func TestScaleUp(t *testing.T) {
	as := NewAutoScaler(1, 5, 5*time.Minute, 30*time.Second)

	// Queue depth = 15, maxClients=5 → threshold=10, queue 15 > 10
	// Scale up: 15/2 = 7, capacity = 5-1 = 4 → 4 new clients
	decision := as.Evaluate(15, 0)

	if decision.Action != "scale_up" {
		t.Errorf("Action = %q, want scale_up", decision.Action)
	}
	if decision.Count != 4 {
		t.Errorf("Count = %d, want 4 (min(15/2, 5-1))", decision.Count)
	}
}

// ============================================================================
// Test: Scale up at max capacity
// ============================================================================

// ============================================================================
// Test: Scale up at max capacity — all clients busy
// ============================================================================

func TestScaleUpAtMax(t *testing.T) {
	as := NewAutoScaler(1, 5, 5*time.Minute, 30*time.Second)

	// Evaluate estimates totalClients = idle + 1.
	// With idle=4, total=5=max: scaleUp rejects, scaleDown fires.
	// This is correct because if most clients are idle, we should
	// scale down to free resources even at max capacity.
	decision := as.Evaluate(20, 4)

	if decision.Action != "scale_down" {
		t.Errorf("Action = %q, want scale_down (4 idle at max → shrink)", decision.Action)
	}
}

// ============================================================================
// Test: Scale down when idle clients exist
// ============================================================================

func TestScaleDown(t *testing.T) {
	as := NewAutoScaler(1, 5, 5*time.Minute, 30*time.Second)

	// 3 idle clients, currentTotal=4, minClients=1
	// Scale down: min(3, 4-1) = 3
	decision := as.Evaluate(0, 3)

	if decision.Action != "scale_down" {
		t.Errorf("Action = %q, want scale_down", decision.Action)
	}
	if decision.Count != 3 {
		t.Errorf("Count = %d, want 3", decision.Count)
	}
}

// ============================================================================
// Test: Scale down at minimum capacity
// ============================================================================

func TestScaleDownAtMin(t *testing.T) {
	as := NewAutoScaler(1, 5, 5*time.Minute, 30*time.Second)

	// No idle clients → nothing to scale down
	decision := as.Evaluate(0, 0)

	if decision.Action != "none" {
		t.Errorf("Action = %q, want none (no idle clients)", decision.Action)
	}
}

// ============================================================================
// Test: Cooldown period
// ============================================================================

func TestCooldown(t *testing.T) {
	as := NewAutoScaler(1, 5, 5*time.Minute, 100*time.Millisecond)

	// Execute a scale-up to trigger cooldown
	as.Execute(ScaleDecision{Action: "scale_up", Count: 1, Reason: "test"})

	// Should be in cooldown immediately after execute
	if !as.InCooldown() {
		t.Error("InCooldown = false after Execute, want true")
	}

	// Evaluate should return none during cooldown
	decision := as.Evaluate(100, 0)
	if decision.Action != "none" {
		t.Errorf("Action during cooldown = %q, want none", decision.Action)
	}

	// Wait for cooldown to expire
	time.Sleep(150 * time.Millisecond)

	if as.InCooldown() {
		t.Error("InCooldown = true after cooldown expiry, want false")
	}
}

// ============================================================================
// Test: Evaluate no action when system is stable
// ============================================================================

func TestEvaluateNoAction(t *testing.T) {
	as := NewAutoScaler(1, 5, 5*time.Minute, 30*time.Second)

	// Queue is low, no idle clients
	decision := as.Evaluate(2, 0)

	if decision.Action != "none" {
		t.Errorf("Action = %q, want none (stable state)", decision.Action)
	}
}
