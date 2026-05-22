package agent

import (
	"github.com/zhazhaku/reef/pkg/bus"
)

// computeContextUsage estimates current context window consumption for the
// given agent and session. Includes history, system prompt (with dynamic context,
// summary, and skills — mirroring BuildMessages composition), and tool definitions.
// The output reserve (MaxTokens) is not counted as "used" but reduces the
// effective budget, matching isOverContextBudget's compression trigger:
//
//	compress when: history + system + tools + maxTokens > contextWindow
//	equivalent to: history + system + tools > contextWindow - maxTokens
//
// Returns nil when the agent or session is unavailable.
func computeContextUsage(agent *AgentInstance, sessionKey string) *bus.ContextUsage {
	if agent == nil || agent.Sessions == nil {
		return nil
	}
	contextWindow := agent.ContextWindow
	if contextWindow <= 0 {
		return nil
	}

	// History tokens
	history := agent.Sessions.GetHistory(sessionKey)
	historyTokens := 0
	for _, m := range history {
		historyTokens += EstimateMessageTokens(m)
	}

	// System message tokens: uses EstimateSystemTokens which mirrors
	// the full system message composition in BuildMessages (static prompt,
	// dynamic context, active skills, summary with wrapping prefix).
	systemTokens := 0
	if agent.ContextBuilder != nil {
		summary := agent.Sessions.GetSummary(sessionKey)
		// Pass nil for active skills: skills are only injected when the user
		// explicitly activates them via /use, which is rare. Using nil matches
		// the common case and avoids over-counting all installed skills.
		systemTokens = agent.ContextBuilder.EstimateSystemTokens(summary, nil)
	}

	// Tool definition tokens
	toolTokens := 0
	if agent.Tools != nil {
		toolTokens = EstimateToolDefsTokens(agent.Tools.ToProviderDefs())
	}

	// Used = history + system (includes summary) + tools
	usedTokens := historyTokens + systemTokens + toolTokens

	// Populate cache metrics from the most recent BuildMessagesFromPrompt output.
	var cacheMetrics CacheMetrics
	if agent.ContextBuilder != nil {
		cacheMetrics = agent.ContextBuilder.latestCacheMetrics
	}

	// Effective budget = contextWindow minus output reserve (maxTokens)
	effectiveWindow := contextWindow - agent.MaxTokens
	if effectiveWindow < 0 {
		effectiveWindow = contextWindow
	}

	// compressAt = effectiveWindow: aligns with isOverContextBudget's
	// proactive trigger (msgTokens + toolTokens + maxTokens > contextWindow).
	compressAt := effectiveWindow

	usedPercent := 0
	if compressAt > 0 {
		usedPercent = usedTokens * 100 / compressAt
	}
	if usedPercent > 100 {
		usedPercent = 100
	}

	return &bus.ContextUsage{
		UsedTokens:       usedTokens,
		TotalTokens:      contextWindow,
		CompressAtTokens: compressAt,
		UsedPercent:      usedPercent,
		StaticChars:      cacheMetrics.StaticChars,
		DynamicChars:     cacheMetrics.DynamicChars,
		HistoryChars:     cacheMetrics.HistoryChars,
		ToolResultChars:  cacheMetrics.ToolResultChars,
		ReasoningChars:   cacheMetrics.ReasoningChars,
		CacheHitEstimate: cacheMetrics.CacheHitEstimate,
	}
}

// CacheMetrics holds DeepSeek prefix-cache estimation fields.
type CacheMetrics struct {
	StaticChars  int
	DynamicChars int
	HistoryChars int
	ToolResultChars int
	ReasoningChars  int
	CacheHitEstimate float64 // 0.0-1.0
}

// computeCacheMetrics estimates the fraction of input tokens that will hit
// DeepSeek's automatic prefix cache. The static system prefix (stable across
// turns) maps to cached tokens; dynamic suffix and tool results map to full-price.
func computeCacheMetrics(
	staticChars, dynamicChars, historyChars, toolResultChars, reasoningChars int,
) CacheMetrics {
	total := staticChars + dynamicChars + historyChars + toolResultChars + reasoningChars
	if total == 0 {
		return CacheMetrics{}
	}
	// Static system prefix is guaranteed cache hit (no dynamic content mixed in).
	// History may partially hit but we conservatively count only the static prefix.
	cacheHit := float64(staticChars) / float64(total)
	return CacheMetrics{
		StaticChars:     staticChars,
		DynamicChars:    dynamicChars,
		HistoryChars:    historyChars,
		ToolResultChars: toolResultChars,
		ReasoningChars:  reasoningChars,
		CacheHitEstimate: cacheHit,
	}
}
