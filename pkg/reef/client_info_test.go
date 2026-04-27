package reef

import (
	"testing"
	"time"
)

func TestClientInfo_IsAvailable(t *testing.T) {
	c := &ClientInfo{
		ID:          "c1",
		Role:        "coder",
		Capacity:    2,
		CurrentLoad: 0,
		State:       ClientConnected,
	}
	if !c.IsAvailable() {
		t.Error("expected available when connected and load < capacity")
	}

	c.CurrentLoad = 2
	if c.IsAvailable() {
		t.Error("expected not available when at full capacity")
	}

	c.CurrentLoad = 0
	c.State = ClientStale
	if c.IsAvailable() {
		t.Error("expected not available when stale")
	}
}

func TestClientInfo_Matches(t *testing.T) {
	c := &ClientInfo{
		Role:   "coder",
		Skills: []string{"go", "docker", "github"},
	}

	// Exact role match, no skill requirements
	if !c.Matches("coder", nil) {
		t.Error("expected match for same role with nil skills")
	}
	if !c.Matches("coder", []string{}) {
		t.Error("expected match for same role with empty skills")
	}

	// Role mismatch
	if c.Matches("analyst", nil) {
		t.Error("expected no match for different role")
	}

	// Skill subset match
	if !c.Matches("coder", []string{"go"}) {
		t.Error("expected match for skill subset")
	}
	if !c.Matches("coder", []string{"go", "docker"}) {
		t.Error("expected match for multiple skills")
	}

	// Missing skill
	if c.Matches("coder", []string{"go", "kubernetes"}) {
		t.Error("expected no match when missing required skill")
	}
}

func TestClientInfo_RemainingCapacity(t *testing.T) {
	c := &ClientInfo{Capacity: 5, CurrentLoad: 2}
	if c.RemainingCapacity() != 3 {
		t.Errorf("RemainingCapacity = %d, want 3", c.RemainingCapacity())
	}

	c.CurrentLoad = 10
	if c.RemainingCapacity() != 0 {
		t.Errorf("RemainingCapacity = %d, want 0 (clamped)", c.RemainingCapacity())
	}
}

func TestClientState_String(t *testing.T) {
	states := []ClientState{ClientConnected, ClientDisconnected, ClientStale}
	for _, s := range states {
		if s == "" {
			t.Errorf("state should not be empty")
		}
	}
}

func TestClientInfo_HeartbeatAge(t *testing.T) {
	c := &ClientInfo{
		ID:            "c1",
		LastHeartbeat: time.Now().Add(-10 * time.Second),
	}
	age := time.Since(c.LastHeartbeat)
	if age < 9*time.Second || age > 11*time.Second {
		t.Errorf("heartbeat age = %v, expected ~10s", age)
	}
}
