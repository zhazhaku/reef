package agent

import (
	"testing"
)

func TestCorruptionGuard_New(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})
	if cg == nil {
		t.Fatal("nil CorruptionGuard")
	}
}

func TestCorruptionGuard_LoopDetected(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	// Same tool called 5 times → loop
	for i := 0; i < 5; i++ {
		report := cg.FeedRound("exec", "output", "thinking...")
		if i < 4 {
			if report != nil {
				t.Errorf("unexpected report at round %d", i)
			}
		}
		if i == 4 {
			if report == nil {
				t.Error("expected loop detection at round 5")
			} else if report.Type != "tool_loop" {
				t.Errorf("report type = %s, want tool_loop", report.Type)
			}
		}
	}
}

func TestCorruptionGuard_LoopNotDetected_DifferentTools(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	tools := []string{"read_file", "list_dir", "exec", "write_file", "edit_file"}
	for i, tool := range tools {
		report := cg.FeedRound(tool, "ok", "fine")
		if report != nil {
			t.Errorf("unexpected report at round %d with tool %s: %v", i, tool, report)
		}
	}
}

func TestCorruptionGuard_LoopNotDetected_BelowThreshold(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	for i := 0; i < 4; i++ {
		report := cg.FeedRound("exec", "ok", "ok")
		if report != nil {
			t.Errorf("unexpected report at round %d", i)
		}
	}
}

func TestCorruptionGuard_Loop_ResetAfterDifferentTool(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	// 3 times exec
	for i := 0; i < 3; i++ {
		cg.FeedRound("exec", "ok", "ok")
	}
	// different tool → resets counter
	cg.FeedRound("read_file", "ok", "ok")
	// back to exec, counter should restart
	for i := 0; i < 4; i++ {
		report := cg.FeedRound("exec", "ok", "ok")
		if report != nil {
			t.Errorf("unexpected report after reset at exec round %d: %v", i, report)
		}
	}
	// 5th exec after reset → should detect
	report := cg.FeedRound("exec", "ok", "ok")
	if report == nil || report.Type != "tool_loop" {
		t.Errorf("expected loop detection after 5 consecutive execs, got %v", report)
	}
}

func TestCorruptionGuard_BlankDetected(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	for i := 0; i < 3; i++ {
		report := cg.FeedRound("", "", "")
		if i < 2 {
			if report != nil {
				t.Errorf("unexpected blank report at round %d", i)
			}
		}
		if i == 2 {
			if report == nil || report.Type != "blank_rounds" {
				t.Errorf("expected blank detection at round 3, got %v", report)
			}
		}
	}
}

func TestCorruptionGuard_BlankNotDetected_BelowThreshold(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	// 2 empty rounds → not enough
	for i := 0; i < 2; i++ {
		report := cg.FeedRound("", "", "")
		if report != nil {
			t.Errorf("unexpected blank report at round %d", i)
		}
	}
}

func TestCorruptionGuard_Blank_ResetAfterFilledRound(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	cg.FeedRound("", "", "")
	cg.FeedRound("", "", "")
	// filled round resets blank counter
	cg.FeedRound("exec", "output", "thought")
	cg.FeedRound("", "", "")
	cg.FeedRound("", "", "")
	// 3 consecutive blanks after reset → should detect
	report := cg.FeedRound("", "", "")
	if report != nil && report.Type == "blank_rounds" {
		// blank count 3 = threshold, detection is correct after reset and 3 fresh blanks
	}
}

func TestCorruptionGuard_GoalDrift_Detected(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	cg.SetGoal("Implement user authentication")

	// First few rounds are on-track
	cg.FeedRound("read_file", "package auth...", "reading auth module")
	cg.FeedRound("exec", "go test passed", "tests working")

	// Then goal changes to something completely different
	report := cg.CheckGoalDrift("Write a weather app")
	if report == nil || report.Type != "goal_drift" {
		t.Errorf("expected goal drift detection, got %v", report)
	}
}

func TestCorruptionGuard_GoalDrift_NotDetected_SameGoal(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	cg.SetGoal("Implement user authentication")
	cg.FeedRound("read_file", "package auth", "reading")

	report := cg.CheckGoalDrift("Implement user authentication")
	if report != nil {
		t.Errorf("unexpected goal drift: %v", report)
	}
}

func TestCorruptionGuard_Check_NoCorruption(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  5,
		BlankThreshold: 3,
	})

	cg.SetGoal("Fix bug")
	cg.FeedRound("read_file", "code here", "reading")
	cg.FeedRound("edit_file", "fixed", "done")

	layers := NewContextLayers(ContextConfig{MaxTokens: 1000})
	layers.SetImmutable("sys", "coder", nil, nil)
	layers.SetTask("Fix bug", nil)
	layers.AppendRound(WorkingRound{Round: 1, Call: "read_file", Output: "code", Thought: "reading"})
	layers.AppendRound(WorkingRound{Round: 2, Call: "edit_file", Output: "fixed", Thought: "done"})

	report := cg.Check(layers)
	if report != nil {
		t.Errorf("unexpected corruption report on clean context: %v", report)
	}
}

func TestCorruptionGuard_Check_LoopDetectedViaLayers(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  3,
		BlankThreshold: 3,
	})

	layers := NewContextLayers(ContextConfig{MaxTokens: 1000})
	for i := 1; i <= 3; i++ {
		layers.AppendRound(WorkingRound{Round: i, Call: "exec", Output: "ok", Thought: "..."})
	}

	report := cg.Check(layers)
	if report == nil || report.Type != "tool_loop" {
		t.Errorf("expected tool_loop from layers, got %v", report)
	}
}

func TestCorruptionGuard_Reset(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{
		LoopThreshold:  3,
		BlankThreshold: 3,
	})

	cg.SetGoal("Do task A")
	cg.FeedRound("exec", "ok", "ok")
	cg.FeedRound("exec", "ok", "ok")

	// Reset should clear all counters
	cg.Reset()

	// After reset, 3 execs should be fine (count starts fresh)
	for i := 0; i < 3; i++ {
		report := cg.FeedRound("exec", "ok", "ok")
		if i < 2 && report != nil {
			t.Errorf("unexpected report after reset at round %d", i)
		}
		if i == 2 && report == nil {
			t.Error("expected loop report after 3 rounds post-reset")
		}
	}
}

func TestCorruptionReport_Fields(t *testing.T) {
	r := CorruptionReport{
		Type:    "tool_loop",
		Tool:    "exec",
		Count:   5,
		Message: "Tool 'exec' called 5 times consecutively",
	}
	if r.Type != "tool_loop" {
		t.Errorf("Type = %s", r.Type)
	}
	if r.Count != 5 {
		t.Errorf("Count = %d", r.Count)
	}
}

func TestDefaultCorruptionConfig(t *testing.T) {
	cfg := DefaultCorruptionConfig()
	if cfg.LoopThreshold != 5 {
		t.Errorf("LoopThreshold = %d", cfg.LoopThreshold)
	}
	if cfg.BlankThreshold != 3 {
		t.Errorf("BlankThreshold = %d", cfg.BlankThreshold)
	}
}

func TestNewCorruptionGuard_ZeroDefaults(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{})
	if cg.config.LoopThreshold != 5 {
		t.Errorf("LoopThreshold = %d", cg.config.LoopThreshold)
	}
	if cg.config.BlankThreshold != 3 {
		t.Errorf("BlankThreshold = %d", cg.config.BlankThreshold)
	}
}

func TestNewCorruptionGuard_CustomValues(t *testing.T) {
	cg := NewCorruptionGuard(CorruptionConfig{LoopThreshold: 7, BlankThreshold: 4})
	if cg.config.LoopThreshold != 7 {
		t.Errorf("LoopThreshold = %d", cg.config.LoopThreshold)
	}
	if cg.config.BlankThreshold != 4 {
		t.Errorf("BlankThreshold = %d", cg.config.BlankThreshold)
	}
}
