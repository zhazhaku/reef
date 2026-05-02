package agent

import (
	"fmt"
	"sync"
)

// ContextWindow manages the lifecycle of a four-layer context:
// builds, compacts when over budget, injects memories.
type ContextWindow struct {
	layers         *ContextLayers
	config         ContextConfig
	compactSummary string
	mu             sync.RWMutex
}

func NewContextWindow(cfg ContextConfig) *ContextWindow {
	return &ContextWindow{
		layers: NewContextLayers(cfg),
		config: cfg,
	}
}

func (cw *ContextWindow) Build(
	systemPrompt, roleConfig string,
	skills, genes []string,
	instruction string,
	metadata map[string]string,
) *ContextLayers {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	cw.layers = NewContextLayers(cw.config)
	cw.layers.SetImmutable(systemPrompt, roleConfig, skills, genes)
	cw.layers.SetTask(instruction, metadata)
	cw.compactSummary = ""
	return cw.layers
}

func (cw *ContextWindow) Compact() error {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if !cw.layers.IsOverBudget() {
		return nil
	}
	working := cw.layers.Working
	if len(working) == 0 {
		return nil
	}
	keepCount := 5
	if len(working) <= keepCount {
		keepCount = len(working)
	}
	oldCount := len(working) - keepCount
	if oldCount > 0 {
		cw.compactSummary += fmt.Sprintf("[Compacted %d rounds]\n", oldCount)
		cw.layers.Working = working[oldCount:]
	}
	return nil
}

func (cw *ContextWindow) AppendRound(round WorkingRound) {
	cw.layers.AppendRound(round)
}

func (cw *ContextWindow) InjectMemory(injections []MemoryInjection) {
	cw.layers.InjectMemory(injections)
}

func (cw *ContextWindow) TokenCount() int {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.layers.TokenEstimate() + len(cw.compactSummary)/4
}

func (cw *ContextWindow) IsOverBudget() bool {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	totalTokens := cw.layers.TokenEstimate() + len(cw.compactSummary)/4
	threshold := int(float64(cw.config.MaxTokens) * cw.config.CompactThreshold)
	return totalTokens > threshold
}

func (cw *ContextWindow) Layers() *ContextLayers { return cw.layers }

func (cw *ContextWindow) CompactSummary() string {
	cw.mu.RLock()
	defer cw.mu.RUnlock()
	return cw.compactSummary
}
