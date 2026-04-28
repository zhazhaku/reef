// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"sync/atomic"

	"github.com/sipeed/picoclaw/pkg/logger"
)

// HermesGuard provides runtime tool access control based on the current
// HermesMode. It is used to dynamically allow or deny tool execution,
// particularly during fallback (degradation) scenarios.
//
// When the guard is in fallback mode (e.g., no clients are online),
// all tool access is temporarily allowed regardless of mode.
type HermesGuard struct {
	mode     HermesMode
	allowed  map[string]struct{}
	fallback atomic.Bool
}

// NewHermesGuard creates a new guard for the given mode.
// In Coordinator mode, only tools in the allowed set are permitted.
// In Full or Executor mode, all tools are permitted (allowed is nil).
func NewHermesGuard(mode HermesMode) *HermesGuard {
	g := &HermesGuard{mode: mode}
	if mode == HermesCoordinator {
		g.allowed = CoordinatorAllowedTools()
	}
	return g
}

// Allow checks whether a tool with the given name is permitted.
// Returns true if:
//   - the mode is Full or Executor (no restrictions)
//   - the guard is in fallback mode (degraded to full access)
//   - the tool name is in the allowed set for Coordinator mode
func (g *HermesGuard) Allow(toolName string) bool {
	if g.allowed == nil || g.fallback.Load() {
		return true
	}
	_, ok := g.allowed[toolName]
	return ok
}

// SetFallback enables or disables fallback mode.
// When enabled, all tool access is temporarily allowed.
// This is used when no clients are online and the coordinator
// needs to handle tasks directly.
func (g *HermesGuard) SetFallback(enabled bool) {
	prev := g.fallback.Swap(enabled)
	if prev != enabled {
		mode := "disabled"
		if enabled {
			mode = "enabled"
		}
		logger.WarnCF("hermes", "Hermes fallback mode changed",
			map[string]any{
				"mode":     string(g.mode),
				"fallback": mode,
			})
	}
}

// IsFallback returns whether the guard is currently in fallback mode.
func (g *HermesGuard) IsFallback() bool {
	return g.fallback.Load()
}

// Mode returns the current HermesMode.
func (g *HermesGuard) Mode() HermesMode {
	return g.mode
}
