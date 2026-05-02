package agent

import (
	"fmt"
	"sync"
)

// CorruptionConfig holds thresholds for corruption detection.
type CorruptionConfig struct {
	LoopThreshold  int // consecutive same-tool calls before flagging (default 5)
	BlankThreshold int // consecutive empty rounds before flagging (default 3)
}

// DefaultCorruptionConfig returns sensible defaults.
func DefaultCorruptionConfig() CorruptionConfig {
	return CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	}
}

// CorruptionReport describes a detected corruption event.
type CorruptionReport struct {
	Type    string // "tool_loop", "blank_rounds", "goal_drift"
	Tool    string // only for tool_loop
	Count   int    // number of consecutive occurrences
	Message string // human-readable description
}

// CorruptionGuard monitors agent execution for signs of context corruption:
// tool loops (same tool called repeatedly), blank rounds (no action taken),
// and goal drift (task objective changed mid-execution).
type CorruptionGuard struct {
	config       CorruptionConfig
	mu           sync.Mutex
	lastTool     string
	sameToolCount int
	blankCount   int
	goal         string
}

// NewCorruptionGuard creates a guard with the given config.
func NewCorruptionGuard(cfg CorruptionConfig) *CorruptionGuard {
	if cfg.LoopThreshold == 0 {
		cfg.LoopThreshold = 5
	}
	if cfg.BlankThreshold == 0 {
		cfg.BlankThreshold = 3
	}
	return &CorruptionGuard{config: cfg}
}

// SetGoal records the current task goal for drift detection.
func (cg *CorruptionGuard) SetGoal(goal string) {
	cg.mu.Lock()
	defer cg.mu.Unlock()
	cg.goal = goal
}

// FeedRound ingests a single execution round: tool call, output, thought.
// Returns a CorruptionReport if a loop or blank pattern is detected.
func (cg *CorruptionGuard) FeedRound(tool, output, thought string) *CorruptionReport {
	cg.mu.Lock()
	defer cg.mu.Unlock()

	// Blank round detection: all three fields empty
	if tool == "" && output == "" && thought == "" {
		cg.blankCount++
		cg.lastTool = ""
		cg.sameToolCount = 0
		if cg.blankCount >= cg.config.BlankThreshold {
			return &CorruptionReport{
				Type:    "blank_rounds",
				Count:   cg.blankCount,
				Message: fmt.Sprintf("%d consecutive blank rounds detected", cg.blankCount),
			}
		}
		return nil
	}

	// Non-blank round: reset blank counter
	cg.blankCount = 0

	// Tool loop detection
	if tool != "" && tool == cg.lastTool {
		cg.sameToolCount++
	} else {
		cg.lastTool = tool
		cg.sameToolCount = 1
	}

	if cg.sameToolCount >= cg.config.LoopThreshold {
		return &CorruptionReport{
			Type:    "tool_loop",
			Tool:    cg.lastTool,
			Count:   cg.sameToolCount,
			Message: fmt.Sprintf("Tool '%s' called %d times consecutively", cg.lastTool, cg.sameToolCount),
		}
	}

	return nil
}

// CheckGoalDrift compares the current goal to the recorded goal.
// Returns a report if the goals differ.
func (cg *CorruptionGuard) CheckGoalDrift(newGoal string) *CorruptionReport {
	cg.mu.Lock()
	defer cg.mu.Unlock()

	if cg.goal != "" && newGoal != "" && cg.goal != newGoal {
		return &CorruptionReport{
			Type:    "goal_drift",
			Message: fmt.Sprintf("Goal drifted from '%s' to '%s'", cg.goal, newGoal),
		}
	}
	return nil
}

// Check examines a full ContextLayers and runs all corruption checks.
// Iterates over Working rounds to detect loops and blanks.
func (cg *CorruptionGuard) Check(layers *ContextLayers) *CorruptionReport {
	layers.mu.RLock()
	working := make([]WorkingRound, len(layers.Working))
	copy(working, layers.Working)
	layers.mu.RUnlock()

	// Replay all rounds through FeedRound
	cg.mu.Lock()
	// Use a temporary guard to avoid mutating the real guard state
	tmp := &CorruptionGuard{config: cg.config}
	cg.mu.Unlock()

	for _, wr := range working {
		if report := tmp.FeedRound(wr.Call, wr.Output, wr.Thought); report != nil {
			return report
		}
	}
	return nil
}

// Reset clears all internal counters and the stored goal.
func (cg *CorruptionGuard) Reset() {
	cg.mu.Lock()
	defer cg.mu.Unlock()
	cg.lastTool = ""
	cg.sameToolCount = 0
	cg.blankCount = 0
	cg.goal = ""
}
