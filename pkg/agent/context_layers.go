package agent

import (
	"fmt"
	"strings"
	"sync"
)

// ContextConfig controls context window budget and compaction behavior.
type ContextConfig struct {
	MaxTokens        int     `json:"max_tokens"`
	CompactThreshold float64 `json:"compact_threshold"`
	MaxWorkingRounds int     `json:"max_working_rounds"`
	MaxInjections    int     `json:"max_injections"`
}

// DefaultContextConfig returns sensible defaults.
func DefaultContextConfig() ContextConfig {
	return ContextConfig{
		MaxTokens:        128000,
		CompactThreshold: 0.8,
		MaxWorkingRounds: 20,
		MaxInjections:    5,
	}
}

// WorkingRound captures a single tool-call round in the Working layer (L2).
type WorkingRound struct {
	Round   int    // Turn number
	Call    string // Tool or function called
	Output  string // Tool output
	Thought string // LLM reasoning / plan
}

// MemoryInjection holds a retrieved memory injected into L3.
type MemoryInjection struct {
	Source    string  // "gene" or "episodic"
	Content   string  // The memory snippet
	Relevance float64 // 0.0–1.0 relevance score
}

// ContextLayers represents the four-layer context model.
//
//	L0 Immutable  — System Prompt + Role Config + Skills + Genes (never compressed)
//	L1 Task       — Task instruction + metadata (fixed for the task duration)
//	L2 Working    — Sliding window of recent tool calls, outputs, and reasoning
//	L3 Injections — Dynamically injected memories (genes + episodic snippets)
type ContextLayers struct {
	Immutable  string            // L0
	Task       string            // L1
	Working    []WorkingRound    // L2
	Injections []MemoryInjection // L3
	config     ContextConfig
	mu         sync.RWMutex
}

// NewContextLayers creates an empty four-layer context.
func NewContextLayers(cfg ContextConfig) *ContextLayers {
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 128000
	}
	if cfg.CompactThreshold == 0 {
		cfg.CompactThreshold = 0.8
	}
	if cfg.MaxWorkingRounds == 0 {
		cfg.MaxWorkingRounds = 20
	}
	if cfg.MaxInjections == 0 {
		cfg.MaxInjections = 5
	}
	return &ContextLayers{
		config: cfg,
	}
}

// SetImmutable populates L0: system prompt, role config, skills, and genes.
// This layer is never compacted.
func (cl *ContextLayers) SetImmutable(systemPrompt, roleConfig string, skills, genes []string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	var parts []string
	if systemPrompt != "" {
		parts = append(parts, systemPrompt)
	}
	if roleConfig != "" {
		parts = append(parts, roleConfig)
	}
	if len(skills) > 0 {
		parts = append(parts, fmt.Sprintf("[Skills] %s", strings.Join(skills, ", ")))
	}
	if len(genes) > 0 {
		parts = append(parts, "[Genes]")
		for _, g := range genes {
			parts = append(parts, "- "+g)
		}
	}
	cl.Immutable = strings.Join(parts, "\n")
}

// SetTask populates L1: task instruction and optional metadata.
func (cl *ContextLayers) SetTask(instruction string, metadata map[string]string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	var parts []string
	parts = append(parts, instruction)
	if len(metadata) > 0 {
		parts = append(parts, "[Task Metadata]")
		for k, v := range metadata {
			parts = append(parts, fmt.Sprintf("%s: %s", k, v))
		}
	}
	cl.Task = strings.Join(parts, "\n")
}

// AppendRound adds a working round to L2, enforcing the MaxWorkingRounds limit.
func (cl *ContextLayers) AppendRound(round WorkingRound) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	cl.Working = append(cl.Working, round)
	// Slide window: drop oldest rounds when over the limit
	if len(cl.Working) > cl.config.MaxWorkingRounds {
		overflow := len(cl.Working) - cl.config.MaxWorkingRounds
		cl.Working = cl.Working[overflow:]
	}
}

// InjectMemory adds memory injections to L3, enforcing the MaxInjections limit.
func (cl *ContextLayers) InjectMemory(injections []MemoryInjection) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	cl.Injections = append(cl.Injections, injections...)
	if len(cl.Injections) > cl.config.MaxInjections {
		// Keep only the most recent MaxInjections
		overflow := len(cl.Injections) - cl.config.MaxInjections
		cl.Injections = cl.Injections[overflow:]
	}
}

// TokenEstimate returns a rough estimate of total token count across all layers.
// Uses a simple heuristic: ~1.3 tokens per character (for English text).
func (cl *ContextLayers) TokenEstimate() int {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	charCount := len(cl.Immutable) + len(cl.Task)

	for _, wr := range cl.Working {
		charCount += len(wr.Call) + len(wr.Output) + len(wr.Thought)
	}

	for _, inj := range cl.Injections {
		charCount += len(inj.Content)
	}

	// Rough heuristic: 1 token ≈ 4 characters for English
	return charCount / 4
}

// IsOverBudget returns true when estimated tokens exceed MaxTokens * CompactThreshold.
func (cl *ContextLayers) IsOverBudget() bool {
	estimated := cl.TokenEstimate()
	threshold := int(float64(cl.config.MaxTokens) * cl.config.CompactThreshold)
	return estimated > threshold
}

// BuildPrompt assembles all four layers into a single LLM prompt string.
func (cl *ContextLayers) BuildPrompt() string {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	var parts []string

	// L0: Immutable
	if cl.Immutable != "" {
		parts = append(parts, cl.Immutable)
	}

	// L1: Task
	if cl.Task != "" {
		parts = append(parts, cl.Task)
	}

	// L2: Working rounds
	if len(cl.Working) > 0 {
		parts = append(parts, "[Recent Actions]")
		for _, wr := range cl.Working {
			parts = append(parts, fmt.Sprintf("Round %d: %s → %s (thought: %s)",
				wr.Round, wr.Call, wr.Output, wr.Thought))
		}
	}

	// L3: Memory injections
	if len(cl.Injections) > 0 {
		parts = append(parts, "[Relevant Memories]")
		for _, inj := range cl.Injections {
			parts = append(parts, fmt.Sprintf("[%s] %s", inj.Source, inj.Content))
		}
	}

	return strings.Join(parts, "\n\n")
}

// Clear resets all layers to empty.
func (cl *ContextLayers) Clear() {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	cl.Immutable = ""
	cl.Task = ""
	cl.Working = nil
	cl.Injections = nil
}

// WorkingSummary returns a compressed summary of the working layer content.
// Used by ContextManager.Compact().
func (cl *ContextLayers) WorkingSummary() string {
	cl.mu.RLock()
	defer cl.mu.RUnlock()

	if len(cl.Working) == 0 {
		return ""
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("[Summary of %d previous rounds]\n", len(cl.Working)))
	for _, wr := range cl.Working {
		// Compact each round to a single line
		output.WriteString(fmt.Sprintf("Round %d: %s → %s\n",
			wr.Round, wr.Call, truncateStr(wr.Output, 100)))
	}
	return output.String()
}

// truncateStr shortens a string to maxLen characters, appending "…" if needed.
func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}
