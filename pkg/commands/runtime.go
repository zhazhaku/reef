package commands

import (
	"context"

	"github.com/zhazhaku/reef/pkg/config"
)

// AutoHistoryEntry represents one completed or failed task in the
// AutoLoop execution history.
type AutoHistoryEntry struct {
	ID          string `json:"id"`
	Instruction string `json:"instruction"`
	Status      string `json:"status"` // done, failed, cancelled
	Duration    string `json:"duration,omitempty"`
	Error       string `json:"error,omitempty"`
}

// MCPServerInfo describes an MCP server state.
type MCPServerInfo struct {
	Name      string
	Enabled   bool
	Deferred  bool
	Connected bool
	ToolCount int
}

type MCPToolParameterInfo struct {
	Name        string
	Type        string
	Description string
	Required    bool
}

type MCPToolInfo struct {
	Name        string
	Description string
	Parameters  []MCPToolParameterInfo
}

// ContextStats describes current session context window usage.
type ContextStats struct {
	UsedTokens       int
	TotalTokens      int // model context window
	CompressAtTokens int // compression threshold
	UsedPercent      int // 0-100
	MessageCount     int
}

// Runtime provides runtime dependencies to command handlers. It is constructed
// per-request by the agent loop so that per-request state (like session scope)
// can coexist with long-lived callbacks (like GetModelInfo).
type Runtime struct {
	Config             *config.Config
	GetModelInfo       func() (name, provider string)
	AskSideQuestion    func(ctx context.Context, question string) (string, error)
	ListAgentIDs       func() []string
	ListDefinitions    func() []Definition
	ListSkillNames     func() []string
	ListMCPServers     func(ctx context.Context) []MCPServerInfo
	ListMCPTools       func(ctx context.Context, serverName string) ([]MCPToolInfo, error)
	GetEnabledChannels func() []string
	GetActiveTurn      func() any // Returning any to avoid circular dependency with agent package
	GetContextStats    func() *ContextStats
	SwitchModel        func(value string) (oldModel string, err error)
	SwitchChannel      func(value string) error
	ClearHistory       func() error
	ReloadConfig       func() error

	// AutoLoop Orchestrator callbacks — wired by AgentLoop when
	// AutoLoopOrchestrator is present. All are nil when the
	// feature is not compiled in or not initialized.
	GetAutoStatus      func() interface{}         // returns OrchestratorStatus
	SetAutoMode        func(mode string) (string, error) // returns old mode
	SetAutoLoopCount   func(count int)             // 0 = infinite
	EnqueueAutoMessage func(instruction string)
	RunAutoStep        func()
	StopAuto           func()
	GetAutoQueue       func() []string
	GetAutoHistory     func() []AutoHistoryEntry
}
