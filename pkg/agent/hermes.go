// PicoClaw - Ultra-lightweight personal AI agent
//
// Package agent provides the Hermes capability architecture for
// constraining AgentLoop behavior based on its operational role.
//
// Three modes are defined:
//   - Full:        default single-client mode, no constraints
//   - Coordinator: server mode, only coordination tools allowed
//   - Executor:    client mode, all tools but no external delegation

package agent

// HermesMode defines the operational role of an AgentLoop in the
// multi-agent Hermes architecture.
type HermesMode string

const (
	// HermesFull is the default mode — single-client, no constraints.
	// Triggered by: picoclaw / picoclaw gateway
	HermesFull HermesMode = "full"

	// HermesCoordinator is the server/coordinator mode — only
	// coordination tools (reef_submit, reef_query, reef_status,
	// message, reaction, cron) are available. The LLM acts as a
	// team coordinator that delegates work to connected clients.
	// Triggered by: picoclaw server
	HermesCoordinator HermesMode = "coordinator"

	// HermesExecutor is the client/executor mode — all tools are
	// available, but reef_submit_task is not registered (executors
	// don't delegate externally). The LLM acts as a task executor.
	// Triggered by: Client connecting to Reef Server (SwarmChannel)
	HermesExecutor HermesMode = "executor"
)

// ParseHermesMode parses a string into a HermesMode.
// Returns HermesFull for empty or unrecognized values.
func ParseHermesMode(s string) HermesMode {
	switch s {
	case "coordinator":
		return HermesCoordinator
	case "executor":
		return HermesExecutor
	default:
		return HermesFull
	}
}

// String returns the string representation of the mode.
func (m HermesMode) String() string {
	return string(m)
}

// IsConstrained returns true if the mode imposes any tool constraints.
func (m HermesMode) IsConstrained() bool {
	return m == HermesCoordinator
}

// CoordinatorAllowedTools returns the set of tool names allowed in
// Coordinator mode. All other tools should not be registered.
func CoordinatorAllowedTools() map[string]struct{} {
	return map[string]struct{}{
		"reef_submit_task": {},
		"reef_query_task":  {},
		"reef_status":      {},
		"message":          {},
		"reaction":         {},
		"cron":             {},
	}
}
