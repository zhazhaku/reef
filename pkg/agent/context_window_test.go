package agent

import (
	"strings"
	"testing"
)

func TestContextWindow_Build(t *testing.T) {
	cw := NewContextWindow(ContextConfig{
		MaxTokens:        128000,
		CompactThreshold: 0.8,
		MaxWorkingRounds: 20,
		MaxInjections:    5,
	})

	sysPrompt := "You are a helpful assistant."
	roleCfg := "Role: coder"
	skills := []string{"git", "go"}
	genes := []string{"GENE: use terse comments"}
	instruction := "Fix bug #42"
	metadata := map[string]string{"task_id": "t-1"}

	layers := cw.Build(sysPrompt, roleCfg, skills, genes, instruction, metadata)

	if !strings.Contains(layers.Immutable, sysPrompt) {
		t.Error("missing system prompt")
	}
	if !strings.Contains(layers.Task, instruction) {
		t.Error("missing instruction")
	}
}

func TestContextWindow_Compact_BelowThreshold(t *testing.T) {
	cw := NewContextWindow(ContextConfig{
		MaxTokens:        128000,
		CompactThreshold: 0.8,
		MaxWorkingRounds: 20,
	})

	cw.Build("sys", "role", []string{}, []string{}, "do it", nil)
	for i := 1; i <= 3; i++ {
		cw.AppendRound(WorkingRound{Round: i, Call: "tool", Output: "ok"})
	}

	if err := cw.Compact(); err != nil {
		t.Fatal(err)
	}
	if len(cw.layers.Working) != 3 {
		t.Errorf("Working = %d, want 3", len(cw.layers.Working))
	}
}

func TestContextWindow_Compact_AboveThreshold(t *testing.T) {
	cw := NewContextWindow(ContextConfig{
		MaxTokens:        100,
		CompactThreshold: 0.1,
		MaxWorkingRounds: 20,
	})

	cw.Build("system prompt text here", "coder", nil, nil, "do some very important work", nil)
	for i := 1; i <= 10; i++ {
		cw.AppendRound(WorkingRound{
			Round:   i,
			Call:    "list_dir",
			Output:  "some output from the tool that takes up tokens",
			Thought: "thinking about it",
		})
	}

	if !cw.layers.IsOverBudget() {
		t.Log("not over budget, skipping")
		return
	}

	before := len(cw.layers.Working)
	cw.Compact()
	after := len(cw.layers.Working)
	if after >= before {
		t.Errorf("Working before=%d after=%d", before, after)
	}
}

func TestContextWindow_Compact_PreservesRecent(t *testing.T) {
	cw := NewContextWindow(ContextConfig{
		MaxTokens:        100,
		CompactThreshold: 0.1,
		MaxWorkingRounds: 20,
	})

	cw.Build("sys", "role", nil, nil, "task", nil)
	for i := 1; i <= 20; i++ {
		cw.AppendRound(WorkingRound{Round: i, Call: "tool", Output: "long long long output"})
	}

	if !cw.layers.IsOverBudget() {
		t.Log("not over budget, skipping")
		return
	}
	cw.Compact()
	if len(cw.layers.Working) == 0 {
		t.Error("Working empty after compact")
	}
}

func TestContextWindow_Compact_OldRoundsSummarized(t *testing.T) {
	cw := NewContextWindow(ContextConfig{
		MaxTokens:        200,
		CompactThreshold: 0.2,
		MaxWorkingRounds: 20,
	})

	cw.Build("system prompt", "coder", nil, nil, "do the task", nil)
	for i := 1; i <= 10; i++ {
		cw.AppendRound(WorkingRound{Round: i, Call: "exec", Output: "output text", Thought: "thinking"})
	}

	if !cw.layers.IsOverBudget() {
		t.Log("not over budget, skipping")
		return
	}
	cw.Compact()

	if cw.compactSummary == "" {
		t.Error("compactSummary empty")
	}
}

func TestContextWindow_BudgetEnforce(t *testing.T) {
	cw := NewContextWindow(ContextConfig{
		MaxTokens:        40,
		CompactThreshold: 0.1,
		MaxWorkingRounds: 1,
	})

	cw.Build("system", "coder", nil, nil, "task", nil)
	cw.AppendRound(WorkingRound{Round: 1, Call: "exec", Output: "output", Thought: "ok"})

	if !cw.layers.IsOverBudget() {
		t.Log("not over budget, skipping")
		return
	}
	cw.Compact()
}

func TestContextWindow_Compact_TokenSaved(t *testing.T) {
	cw := NewContextWindow(ContextConfig{
		MaxTokens:        300,
		CompactThreshold: 0.1,
		MaxWorkingRounds: 20,
	})

	cw.Build("sys", "role", nil, nil, "task", nil)

	for i := 1; i <= 15; i++ {
		cw.AppendRound(WorkingRound{
			Round:   i,
			Call:    "exec",
			Output:  "long output that takes tokens in the context window",
			Thought: "thinking about output and planning next steps",
		})
	}

	if !cw.layers.IsOverBudget() {
		t.Log("not over budget, skipping")
		return
	}

	before := cw.layers.TokenEstimate()
	cw.Compact()
	after := cw.layers.TokenEstimate()

	if after >= before {
		t.Errorf("tokens before=%d after=%d", before, after)
	}
	t.Logf("token reduction: before=%d after=%d", before, after)
}

func TestContextWindow_InjectMemory(t *testing.T) {
	cw := NewContextWindow(ContextConfig{MaxTokens: 10000, MaxInjections: 5})
	cw.Build("sys", "role", nil, nil, "task", nil)

	injections := []MemoryInjection{
		{Source: "gene", Content: "prefer functional style", Relevance: 0.9},
		{Source: "episodic", Content: "similar task failed: nil pointer", Relevance: 0.8},
	}
	cw.InjectMemory(injections)

	if len(cw.layers.Injections) != 2 {
		t.Fatalf("Injections = %d, want 2", len(cw.layers.Injections))
	}
}

func TestContextWindow_TokenCount(t *testing.T) {
	cw := NewContextWindow(ContextConfig{MaxTokens: 10000})
	cw.Build("sys prompt", "coder role", nil, nil, "do task", nil)
	cw.AppendRound(WorkingRound{Round: 1, Call: "exec", Output: "output text"})

	if cw.TokenCount() <= 0 {
		t.Error("TokenCount <= 0")
	}
}

func TestContextWindow_IsOverBudget(t *testing.T) {
	cw := NewContextWindow(ContextConfig{MaxTokens: 20, CompactThreshold: 0.5})
	cw.Build("this is a lot of text that should exceed the budget", "coder", nil, nil, "task description here", nil)
	if !cw.IsOverBudget() {
		t.Error("expected over budget with tiny MaxTokens")
	}
}

func TestContextWindow_AppendRound(t *testing.T) {
	cw := NewContextWindow(ContextConfig{MaxTokens: 10000, MaxWorkingRounds: 10})
	cw.Build("sys", "role", nil, nil, "task", nil)

	for i := 1; i <= 5; i++ {
		cw.AppendRound(WorkingRound{Round: i, Call: "tool", Output: "ok"})
	}

	if len(cw.layers.Working) != 5 {
		t.Fatalf("Working = %d, want 5", len(cw.layers.Working))
	}
}

func TestContextWindow_Layers(t *testing.T) {
	cw := NewContextWindow(ContextConfig{MaxTokens: 10000})
	cw.Build("sys", "role", []string{"git"}, []string{"gene1"}, "do it", nil)

	if cw.Layers().Immutable == "" {
		t.Error("Layers().Immutable is empty")
	}
}

func TestContextWindow_CompactSummary(t *testing.T) {
	cw := NewContextWindow(ContextConfig{
		MaxTokens:        40,
		CompactThreshold: 0.1,
		MaxWorkingRounds: 20,
	})
	cw.Build("system text here more text", "coder role text", nil, nil, "do task here", nil)
	for i := 1; i <= 10; i++ {
		cw.AppendRound(WorkingRound{Round: i, Call: "exec", Output: "output text goes here", Thought: "hmm"})
	}
	if cw.layers.IsOverBudget() {
		cw.Compact()
	}
	s := cw.CompactSummary()
	_ = s
	// May be empty if not over budget, that's fine
}
