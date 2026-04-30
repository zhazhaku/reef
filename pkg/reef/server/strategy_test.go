package server

import (
	"testing"

	"github.com/zhazhaku/reef/pkg/reef"
)

func makeClients(n int) []*reef.ClientInfo {
	clients := make([]*reef.ClientInfo, n)
	for i := 0; i < n; i++ {
		clients[i] = &reef.ClientInfo{
			ID:          string(rune('A' + i)),
			CurrentLoad: i,
		}
	}
	return clients
}

// ---------------------------------------------------------------------------
// 2.2.2: LeastLoadStrategy
// ---------------------------------------------------------------------------

func TestLeastLoadStrategy(t *testing.T) {
	s := &LeastLoadStrategy{}

	// Empty → nil
	if s.Select(nil) != nil {
		t.Error("expected nil for empty candidates")
	}
	if s.Select([]*reef.ClientInfo{}) != nil {
		t.Error("expected nil for empty slice")
	}

	// Single candidate
	clients := makeClients(1)
	sel := s.Select(clients)
	if sel == nil || sel.ID != "A" {
		t.Errorf("expected A, got %v", sel)
	}

	// Multiple candidates — pick lowest load
	clients = makeClients(5)
	sel = s.Select(clients)
	if sel == nil || sel.ID != "A" {
		t.Errorf("expected A (load 0), got %s (load %d)", sel.ID, sel.CurrentLoad)
	}
}

// ---------------------------------------------------------------------------
// 2.2.3: RoundRobinStrategy
// ---------------------------------------------------------------------------

func TestRoundRobinStrategy(t *testing.T) {
	s := &RoundRobinStrategy{}

	clients := makeClients(5)
	// All loads are different (0,1,2,3,4) → lowest load wins with tie-break on ID
	sel := s.Select(clients)
	if sel == nil || sel.ID != "A" {
		t.Errorf("expected A (lowest load), got %s", sel.ID)
	}

	// Equal loads → alphabetical
	clients2 := []*reef.ClientInfo{
		{ID: "C", CurrentLoad: 1},
		{ID: "A", CurrentLoad: 1},
		{ID: "B", CurrentLoad: 1},
	}
	sel = s.Select(clients2)
	if sel.ID != "A" {
		t.Errorf("expected A (first alphabetically), got %s", sel.ID)
	}
}

// ---------------------------------------------------------------------------
// 2.2.4: AffinityStrategy
// ---------------------------------------------------------------------------

func TestAffinityStrategy(t *testing.T) {
	// Mock task history
	tasks := []*reef.Task{
		{ID: "t1", AssignedClient: "A", Status: reef.TaskCompleted},
		{ID: "t2", AssignedClient: "A", Status: reef.TaskCompleted},
		{ID: "t3", AssignedClient: "B", Status: reef.TaskFailed},
		{ID: "t4", AssignedClient: "C", Status: reef.TaskCompleted},
	}
	getHistory := func() []*reef.Task { return tasks }

	s := NewAffinityStrategy(getHistory)

	clients := []*reef.ClientInfo{
		{ID: "A", CurrentLoad: 5},
		{ID: "B", CurrentLoad: 1},
		{ID: "C", CurrentLoad: 3},
	}
	// A: +2.0 (2 completed), B: -0.5 (1 failed), C: +1.0 (1 completed)
	// A should win despite higher load
	sel := s.Select(clients)
	if sel == nil || sel.ID != "A" {
		t.Errorf("expected A (affinity score 2.0), got %s", sel.ID)
	}

	// Empty history — all scores 0, tie-break by lowest load
	tasks2 := []*reef.Task{}
	getHistory2 := func() []*reef.Task { return tasks2 }
	s2 := NewAffinityStrategy(getHistory2)

	sel2 := s2.Select(clients)
	if sel2 == nil || sel2.ID != "B" {
		t.Errorf("expected B (lowest load with 0 score), got %s", sel2.ID)
	}
}

func TestAffinityStrategy_Empty(t *testing.T) {
	getHistory := func() []*reef.Task { return nil }
	s := NewAffinityStrategy(getHistory)
	if s.Select(nil) != nil {
		t.Error("expected nil for empty candidates")
	}
}
