package agent

import (
	"strings"
	"testing"
)

func TestContextLayers_New(t *testing.T) {
	cfg := ContextConfig{
		MaxTokens:        128000,
		CompactThreshold: 0.8,
		MaxWorkingRounds: 20,
	}

	layers := NewContextLayers(cfg)

	if layers.config.MaxTokens != 128000 {
		t.Errorf("MaxTokens = %d, want 128000", layers.config.MaxTokens)
	}
	if len(layers.Working) != 0 {
		t.Errorf("Working rounds = %d, want 0", len(layers.Working))
	}
	if len(layers.Injections) != 0 {
		t.Errorf("Injections = %d, want 0", len(layers.Injections))
	}
	if layers.Immutable != "" {
		t.Errorf("Immutable = %q, want empty", layers.Immutable)
	}
	if layers.Task != "" {
		t.Errorf("Task = %q, want empty", layers.Task)
	}
}

func TestContextLayers_SetImmutable(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})

	sysPrompt := "You are an AI assistant."
	roleCfg := "Role: coder"
	skills := []string{"git", "go", "docker"}
	genes := []string{"GENE: prefer terse code comments"}

	layers.SetImmutable(sysPrompt, roleCfg, skills, genes)

	if !strings.Contains(layers.Immutable, sysPrompt) {
		t.Error("Immutable missing system prompt")
	}
	if !strings.Contains(layers.Immutable, roleCfg) {
		t.Error("Immutable missing role config")
	}
	if !strings.Contains(layers.Immutable, "git") {
		t.Error("Immutable missing skills")
	}
	if !strings.Contains(layers.Immutable, genes[0]) {
		t.Error("Immutable missing genes")
	}
	// Immutable should NOT be empty after SetImmutable
	if layers.Immutable == "" {
		t.Error("Immutable still empty after SetImmutable")
	}
}

func TestContextLayers_SetTask(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})

	instruction := "Write a function that sorts an array"
	metadata := map[string]string{"task_id": "task-001", "priority": "high"}
	layers.SetTask(instruction, metadata)

	if !strings.Contains(layers.Task, instruction) {
		t.Error("Task missing instruction")
	}
	if !strings.Contains(layers.Task, "task-001") {
		t.Error("Task missing metadata")
	}
}

func TestContextLayers_AppendRound(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000, MaxWorkingRounds: 5})

	layers.AppendRound(WorkingRound{Round: 1, Call: "list_dir", Output: "file1.go, file2.go", Thought: "need to read files"})
	layers.AppendRound(WorkingRound{Round: 2, Call: "read_file", Output: "package main...", Thought: "understood code"})

	if len(layers.Working) != 2 {
		t.Fatalf("Working rounds = %d, want 2", len(layers.Working))
	}
	if layers.Working[0].Round != 1 {
		t.Errorf("Round[0] = %d", layers.Working[0].Round)
	}
	if layers.Working[1].Round != 2 {
		t.Errorf("Round[1] = %d", layers.Working[1].Round)
	}
}

func TestContextLayers_AppendRound_Overflow(t *testing.T) {
	// MaxWorkingRounds=3, append 5 rounds → should keep only last 3
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000, MaxWorkingRounds: 3})

	for i := 1; i <= 5; i++ {
		layers.AppendRound(WorkingRound{Round: i, Call: "tool"})
	}

	if len(layers.Working) != 3 {
		t.Fatalf("Working rounds = %d, want 3", len(layers.Working))
	}
	// Should keep rounds 3,4,5
	if layers.Working[0].Round != 3 {
		t.Errorf("first kept round = %d, want 3", layers.Working[0].Round)
	}
	if layers.Working[2].Round != 5 {
		t.Errorf("last kept round = %d, want 5", layers.Working[2].Round)
	}
}

func TestContextLayers_InjectMemory(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})

	injections := []MemoryInjection{
		{Source: "gene", Content: "GENE: use error wrapping", Relevance: 0.9},
		{Source: "episodic", Content: "EP: similar task failed due to nil check", Relevance: 0.8},
	}

	layers.InjectMemory(injections)

	if len(layers.Injections) != 2 {
		t.Fatalf("Injections = %d, want 2", len(layers.Injections))
	}
	if layers.Injections[0].Source != "gene" {
		t.Errorf("Injection[0].Source = %s", layers.Injections[0].Source)
	}
}

func TestContextLayers_InjectMemory_Limit(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})

	// Inject 10 items → should keep only last MaxInjections (default 5)
	injections := make([]MemoryInjection, 10)
	for i := range injections {
		injections[i] = MemoryInjection{Source: "gene", Content: "gene-" + string(rune('a'+i)), Relevance: float64(i) / 10}
	}

	layers.InjectMemory(injections)

	maxInject := layers.config.MaxInjections
	if maxInject == 0 {
		maxInject = 5 // default
	}
	if len(layers.Injections) > maxInject {
		t.Errorf("Injections = %d, want ≤ %d", len(layers.Injections), maxInject)
	}
}

func TestContextLayers_TokenEstimate(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})
	layers.SetImmutable("sys prompt", "role: coder", []string{}, []string{})
	layers.SetTask("do stuff", nil)
	layers.AppendRound(WorkingRound{Round: 1, Call: "read_file", Output: "hello world", Thought: "ok"})

	tokens := layers.TokenEstimate()
	if tokens <= 0 {
		t.Errorf("TokenEstimate = %d, want > 0", tokens)
	}
	// With small inputs, should be small
	if tokens > 200 {
		t.Logf("TokenEstimate = %d (rough estimate, ok if reasonable)", tokens)
	}
}

func TestContextLayers_TokenEstimate_Empty(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})
	tokens := layers.TokenEstimate()
	if tokens != 0 {
		t.Errorf("TokenEstimate empty = %d, want 0", tokens)
	}
}

func TestContextLayers_IsOverBudget(t *testing.T) {
	// Tiny budget → everything triggers
	layers := NewContextLayers(ContextConfig{MaxTokens: 10, CompactThreshold: 0.8})
	layers.SetImmutable("this is a lot of text that should exceed the budget easily", "coder", nil, nil)
	layers.SetTask("write code", nil)

	if !layers.IsOverBudget() {
		t.Error("IsOverBudget should be true with tiny MaxTokens")
	}
}

func TestContextLayers_IsOverBudget_False(t *testing.T) {
	// Huge budget → nothing triggers
	layers := NewContextLayers(ContextConfig{MaxTokens: 999999, CompactThreshold: 0.8})
	layers.SetImmutable("small text", "coder", nil, nil)

	if layers.IsOverBudget() {
		t.Error("IsOverBudget should be false with huge MaxTokens")
	}
}

func TestContextLayers_Clear(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})
	layers.SetImmutable("sys", "role", nil, nil)
	layers.SetTask("do it", nil)
	layers.AppendRound(WorkingRound{Round: 1, Call: "tool"})
	layers.InjectMemory([]MemoryInjection{{Source: "gene", Content: "tip"}})

	layers.Clear()

	if layers.Immutable != "" {
		t.Error("Immutable not cleared")
	}
	if layers.Task != "" {
		t.Error("Task not cleared")
	}
	if len(layers.Working) != 0 {
		t.Error("Working not cleared")
	}
	if len(layers.Injections) != 0 {
		t.Error("Injections not cleared")
	}
}

func TestContextLayers_BuildPrompt(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})
	layers.SetImmutable("SYSTEM: be helpful", "role: coder", nil, nil)
	layers.SetTask("TASK: fix bug #42", map[string]string{"id": "42"})
	layers.AppendRound(WorkingRound{Round: 1, Call: "list_dir", Output: "foo.go", Thought: "checking"})
	layers.InjectMemory([]MemoryInjection{{Source: "gene", Content: "TIP: use context", Relevance: 0.9}})

	prompt := layers.BuildPrompt()

	if !strings.Contains(prompt, "SYSTEM: be helpful") {
		t.Error("prompt missing L0 Immutable")
	}
	if !strings.Contains(prompt, "TASK: fix bug #42") {
		t.Error("prompt missing L1 Task")
	}
	if !strings.Contains(prompt, "list_dir") {
		t.Error("prompt missing L2 Working tool call")
	}
	if !strings.Contains(prompt, "TIP: use context") {
		t.Error("prompt missing L3 Injection")
	}
}

func TestWorkingRound_Fields(t *testing.T) {
	wr := WorkingRound{
		Round:   3,
		Call:    "exec",
		Output:  "success",
		Thought: "ran the command",
	}
	if wr.Round != 3 {
		t.Errorf("Round = %d", wr.Round)
	}
	if wr.Call != "exec" {
		t.Errorf("Call = %s", wr.Call)
	}
}

func TestMemoryInjection_Fields(t *testing.T) {
	mi := MemoryInjection{
		Source:    "episodic",
		Content:   "remember to check nil",
		Relevance: 0.75,
	}
	if mi.Source != "episodic" {
		t.Errorf("Source = %s", mi.Source)
	}
	if mi.Relevance < 0 || mi.Relevance > 1 {
		t.Errorf("Relevance = %f, want [0,1]", mi.Relevance)
	}
}

func TestDefaultContextConfig(t *testing.T) {
	cfg := DefaultContextConfig()
	if cfg.MaxTokens != 128000 {
		t.Errorf("MaxTokens = %d", cfg.MaxTokens)
	}
	if cfg.CompactThreshold != 0.8 {
		t.Errorf("CompactThreshold = %f", cfg.CompactThreshold)
	}
	if cfg.MaxWorkingRounds != 20 {
		t.Errorf("MaxWorkingRounds = %d", cfg.MaxWorkingRounds)
	}
	if cfg.MaxInjections != 5 {
		t.Errorf("MaxInjections = %d", cfg.MaxInjections)
	}
}

func TestContextLayers_New_ZeroDefaults(t *testing.T) {
	// Zero values should get defaults
	layers := NewContextLayers(ContextConfig{})
	if layers.config.MaxTokens != 128000 {
		t.Errorf("MaxTokens = %d", layers.config.MaxTokens)
	}
}

func TestContextLayers_WorkingSummary(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})
	layers.AppendRound(WorkingRound{Round: 1, Call: "exec", Output: "ok", Thought: "ran"})
	layers.AppendRound(WorkingRound{Round: 2, Call: "read_file", Output: "file content here", Thought: "checked"})

	summary := layers.WorkingSummary()
	if summary == "" {
		t.Error("WorkingSummary returned empty")
	}
	if !contains(summary, "Summary of 2 previous rounds") {
		t.Error("missing summary header")
	}
}

func TestContextLayers_WorkingSummary_Empty(t *testing.T) {
	layers := NewContextLayers(ContextConfig{MaxTokens: 10000})
	if s := layers.WorkingSummary(); s != "" {
		t.Errorf("empty summary = %q", s)
	}
}

func TestTruncateStr(t *testing.T) {
	if s := truncateStr("hello", 10); s != "hello" {
		t.Errorf("short = %s", s)
	}
	if s := truncateStr("hello world", 5); s != "hello…" {
		t.Errorf("truncated = %s", s)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
