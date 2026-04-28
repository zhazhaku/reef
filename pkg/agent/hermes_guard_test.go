// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"testing"
)

func TestNewHermesGuard_FullMode(t *testing.T) {
	guard := NewHermesGuard(HermesFull)

	// Full mode: all tools allowed
	if !guard.Allow("web_search") {
		t.Error("Full mode should allow web_search")
	}
	if !guard.Allow("exec") {
		t.Error("Full mode should allow exec")
	}
	if !guard.Allow("reef_submit_task") {
		t.Error("Full mode should allow reef_submit_task")
	}
	if guard.IsFallback() {
		t.Error("Full mode should not start in fallback")
	}
}

func TestNewHermesGuard_CoordinatorMode(t *testing.T) {
	guard := NewHermesGuard(HermesCoordinator)

	// Coordinator mode: only allowed tools
	if !guard.Allow("reef_submit_task") {
		t.Error("Coordinator should allow reef_submit_task")
	}
	if !guard.Allow("message") {
		t.Error("Coordinator should allow message")
	}
	if !guard.Allow("cron") {
		t.Error("Coordinator should allow cron")
	}

	// Forbidden tools
	if guard.Allow("web_search") {
		t.Error("Coordinator should NOT allow web_search")
	}
	if guard.Allow("exec") {
		t.Error("Coordinator should NOT allow exec")
	}
	if guard.Allow("read_file") {
		t.Error("Coordinator should NOT allow read_file")
	}
	if guard.Allow("spawn") {
		t.Error("Coordinator should NOT allow spawn")
	}
}

func TestNewHermesGuard_ExecutorMode(t *testing.T) {
	guard := NewHermesGuard(HermesExecutor)

	// Executor mode: all tools allowed (reef_submit handled by not registering it)
	if !guard.Allow("web_search") {
		t.Error("Executor should allow web_search")
	}
	if !guard.Allow("exec") {
		t.Error("Executor should allow exec")
	}
}

func TestHermesGuard_Fallback(t *testing.T) {
	guard := NewHermesGuard(HermesCoordinator)

	// Before fallback: coordinator tools only
	if guard.Allow("web_search") {
		t.Error("Coordinator should NOT allow web_search before fallback")
	}

	// Enable fallback
	guard.SetFallback(true)
	if !guard.IsFallback() {
		t.Error("Fallback should be enabled")
	}

	// After fallback: all tools allowed
	if !guard.Allow("web_search") {
		t.Error("Coordinator in fallback should allow web_search")
	}
	if !guard.Allow("exec") {
		t.Error("Coordinator in fallback should allow exec")
	}
	if !guard.Allow("reef_submit_task") {
		t.Error("Coordinator in fallback should still allow reef_submit_task")
	}

	// Disable fallback
	guard.SetFallback(false)
	if guard.IsFallback() {
		t.Error("Fallback should be disabled")
	}
	if guard.Allow("web_search") {
		t.Error("Coordinator should NOT allow web_search after fallback disabled")
	}
}

func TestHermesGuard_Mode(t *testing.T) {
	tests := []struct {
		mode HermesMode
	}{
		{HermesFull},
		{HermesCoordinator},
		{HermesExecutor},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			guard := NewHermesGuard(tt.mode)
			if guard.Mode() != tt.mode {
				t.Errorf("Mode() = %q, want %q", guard.Mode(), tt.mode)
			}
		})
	}
}
