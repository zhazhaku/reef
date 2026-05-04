package server

import (
	"sort"

	"github.com/zhazhaku/reef/pkg/reef"
)

// MatchStrategy selects the best client from a set of eligible candidates.
type MatchStrategy interface {
	// Select picks the best client from the slice. Returns nil if no suitable client.
	// All clients in the slice have already passed role/skill/availability checks.
	Select(candidates []*reef.ClientInfo) *reef.ClientInfo
}

// matchStrategyName returns the strategy name for configuration.
func (s *Scheduler) matchStrategyName() string {
	switch s.matchStrategy.(type) {
	case *LeastLoadStrategy:
		return "least_load"
	case *RoundRobinStrategy:
		return "round_robin"
	case *AffinityStrategy:
		return "affinity"
	default:
		return "least_load"
	}
}

// ---------------------------------------------------------------------------
// LeastLoadStrategy — picks the client with the lowest CurrentLoad.
// This is the default and mirrors the v1 behavior.
// ---------------------------------------------------------------------------

// LeastLoadStrategy selects the client with the lowest current load.
type LeastLoadStrategy struct{}

func (s *LeastLoadStrategy) Select(candidates []*reef.ClientInfo) *reef.ClientInfo {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.CurrentLoad < best.CurrentLoad {
			best = c
		}
	}
	return best
}

// ---------------------------------------------------------------------------
// RoundRobinStrategy — distributes tasks evenly across clients.
// ---------------------------------------------------------------------------

// RoundRobinStrategy cycles through clients in round-robin order.
type RoundRobinStrategy struct {
	// For simplicity, we select the candidate with fewest recent assignments
	// by comparing the client's load. In a true round-robin, we'd need
	// a persistent counter. For the Reef use case, load-based is more practical.
}

func (s *RoundRobinStrategy) Select(candidates []*reef.ClientInfo) *reef.ClientInfo {
	if len(candidates) == 0 {
		return nil
	}
	// Pseudo round-robin: pick the candidate with lowest load,
	// with tie-breaking favoring the first in alphabetical order.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CurrentLoad != candidates[j].CurrentLoad {
			return candidates[i].CurrentLoad < candidates[j].CurrentLoad
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates[0]
}

// ---------------------------------------------------------------------------
// AffinityStrategy — prefers clients with historical success for similar tasks.
// ---------------------------------------------------------------------------

// AffinityStrategy selects the client with the highest recent success rate.
// It uses a lazy task-history getter to avoid circular dependencies.
type AffinityStrategy struct {
	getTaskHistory func() []*reef.Task
}

// NewAffinityStrategy creates an affinity strategy that reads history lazily.
func NewAffinityStrategy(getTaskHistory func() []*reef.Task) *AffinityStrategy {
	return &AffinityStrategy{getTaskHistory: getTaskHistory}
}

func (s *AffinityStrategy) Select(candidates []*reef.ClientInfo) *reef.ClientInfo {
	if len(candidates) == 0 {
		return nil
	}

	// Compute success rate for each candidate from recent task history
	scores := make(map[string]float64)
	for _, t := range s.getTaskHistory() {
		if t.AssignedClient == "" {
			continue
		}
		switch t.Status {
		case reef.TaskCompleted:
			scores[t.AssignedClient] += 1.0
		case reef.TaskFailed, reef.TaskEscalated:
			scores[t.AssignedClient] -= 0.5
		}
	}

	// Select candidate with highest score; tie-break by lowest load
	var best *reef.ClientInfo
	bestScore := -9999.0
	for _, c := range candidates {
		score := scores[c.ID]
		if score > bestScore || (score == bestScore && (best == nil || c.CurrentLoad < best.CurrentLoad)) {
			bestScore = score
			best = c
		}
	}
	return best
}
