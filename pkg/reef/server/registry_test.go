package server

import (
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/reef"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry(nil)
	c := &reef.ClientInfo{ID: "c1", Role: "coder", Capacity: 2}
	r.Register(c)

	got := r.Get("c1")
	if got == nil {
		t.Fatal("expected client c1")
	}
	if got.Role != "coder" {
		t.Errorf("role = %s, want coder", got.Role)
	}
}

func TestRegistry_Unregister(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&reef.ClientInfo{ID: "c1"})
	r.Unregister("c1")
	if r.Get("c1") != nil {
		t.Error("expected c1 to be removed")
	}
}

func TestRegistry_UpdateHeartbeat(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&reef.ClientInfo{ID: "c1", LastHeartbeat: time.Now().Add(-1 * time.Hour)})

	ok := r.UpdateHeartbeat("c1")
	if !ok {
		t.Error("expected successful heartbeat update")
	}
	c := r.Get("c1")
	if time.Since(c.LastHeartbeat) > time.Second {
		t.Error("heartbeat should be recent")
	}
	if c.State != reef.ClientConnected {
		t.Errorf("state = %s, want connected", c.State)
	}
}

func TestRegistry_ScanStale(t *testing.T) {
	staleCalled := false
	r := NewRegistry(func(id string) {
		staleCalled = true
	})

	r.Register(&reef.ClientInfo{
		ID:            "fresh",
		LastHeartbeat: time.Now(),
		State:         reef.ClientConnected,
	})
	r.Register(&reef.ClientInfo{
		ID:            "old",
		LastHeartbeat: time.Now().Add(-2 * time.Minute),
		State:         reef.ClientConnected,
	})

	staleIDs := r.ScanStale(1 * time.Minute)
	if len(staleIDs) != 1 || staleIDs[0] != "old" {
		t.Errorf("stale IDs = %v, want [old]", staleIDs)
	}
	if !staleCalled {
		t.Error("expected onStale callback")
	}
	if r.Get("old").State != reef.ClientStale {
		t.Error("expected old client to be stale")
	}
	if r.Get("fresh").State != reef.ClientConnected {
		t.Error("expected fresh client to remain connected")
	}
}

func TestRegistry_LoadTracking(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&reef.ClientInfo{ID: "c1", Capacity: 2, CurrentLoad: 0})

	r.IncrementLoad("c1")
	if r.Get("c1").CurrentLoad != 1 {
		t.Errorf("load = %d, want 1", r.Get("c1").CurrentLoad)
	}

	r.DecrementLoad("c1")
	if r.Get("c1").CurrentLoad != 0 {
		t.Errorf("load = %d, want 0", r.Get("c1").CurrentLoad)
	}

	// Decrement should not go below 0
	r.DecrementLoad("c1")
	if r.Get("c1").CurrentLoad != 0 {
		t.Errorf("load = %d, want 0 (clamped)", r.Get("c1").CurrentLoad)
	}
}

func TestRegistry_ListByRole(t *testing.T) {
	r := NewRegistry(nil)
	r.Register(&reef.ClientInfo{ID: "c1", Role: "coder"})
	r.Register(&reef.ClientInfo{ID: "c2", Role: "analyst"})
	r.Register(&reef.ClientInfo{ID: "c3", Role: "coder"})

	coders := r.ListByRole("coder")
	if len(coders) != 2 {
		t.Errorf("len(coders) = %d, want 2", len(coders))
	}
}
