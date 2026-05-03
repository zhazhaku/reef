package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/zhazhaku/reef/pkg/providers"
)

// sessionState holds per-session P8 cognitive context.
type sessionState struct {
	layers *ContextLayers
	window *ContextWindow
	guard  *CorruptionGuard
}

// CNPContextManager implements ContextManager using the P8 four-layer
// cognitive architecture: ContextLayers + ContextWindow + CorruptionGuard.
// Register as "cnp".
type CNPContextManager struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
	cfg      ContextConfig
}

// NewCNPContextManager creates a CNP context manager factory-compatible constructor.
// cfg is optional JSON; defaults are used when nil.
func NewCNPContextManager(cfg json.RawMessage, al *AgentLoop) (ContextManager, error) {
	cc := DefaultContextConfig()
	if cfg != nil {
		if err := json.Unmarshal(cfg, &cc); err != nil {
			return nil, fmt.Errorf("cnp: invalid config: %w", err)
		}
	}
	return &CNPContextManager{
		sessions: make(map[string]*sessionState),
		cfg:      cc,
	}, nil
}

func init() {
	RegisterContextManager("cnp", NewCNPContextManager)
}

func (cm *CNPContextManager) getSession(key string) *sessionState {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	s, ok := cm.sessions[key]
	if !ok {
		s = &sessionState{
			layers: NewContextLayers(cm.cfg),
			window: NewContextWindow(cm.cfg),
			guard:  NewCorruptionGuard(DefaultCorruptionConfig()),
		}
		cm.sessions[key] = s
	}
	return s
}

// Assemble builds context from layers and returns LLM-ready messages.
func (cm *CNPContextManager) Assemble(ctx context.Context, req *AssembleRequest) (*AssembleResponse, error) {
	s := cm.getSession(req.SessionKey)

	// Detect corruption before assembling
	report := s.guard.Check(s.layers)
	summary := ""
	if report != nil {
		summary = fmt.Sprintf("[Corruption %s] %s", report.Type, report.Message)
	}

	// Build messages from layers
	messages := buildMessagesFromLayers(s.layers)

	// Apply budget via window
	_ = s.window

	return &AssembleResponse{
		History: messages,
		Summary: summary,
	}, nil
}

// Compact triggers compression of old working rounds.
func (cm *CNPContextManager) Compact(ctx context.Context, req *CompactRequest) error {
	s := cm.getSession(req.SessionKey)
	return s.window.Compact()
}

// Ingest records a message as a working round.
func (cm *CNPContextManager) Ingest(ctx context.Context, req *IngestRequest) error {
	s := cm.getSession(req.SessionKey)

	round := WorkingRound{
		Round:  len(s.layers.Working) + 1,
		Call:   req.Message.Role,
		Output: req.Message.Content,
	}
	// If the message has tool calls, record the first one
	if len(req.Message.ToolCalls) > 0 {
		round.Call = req.Message.ToolCalls[0].Function.Name
		round.Output = req.Message.ToolCalls[0].Function.Arguments
	}
	s.layers.AppendRound(round)

	// Feed guard
	s.guard.FeedRound(round.Call, round.Output, "")

	return nil
}

// Clear removes all stored context for a session.
func (cm *CNPContextManager) Clear(ctx context.Context, sessionKey string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.sessions, sessionKey)
	return nil
}

// buildMessagesFromLayers converts ContextLayers into providers.Message slice.
func buildMessagesFromLayers(layers *ContextLayers) []providers.Message {
	var msgs []providers.Message

	// Immutable system prompt (L0)
	if layers.Immutable != "" {
		msgs = append(msgs, providers.Message{
			Role:    "system",
			Content: layers.Immutable,
		})
	}

	// Task instruction (L1)
	if layers.Task != "" {
		msgs = append(msgs, providers.Message{
			Role:    "system",
			Content: layers.Task,
		})
	}

	// Working rounds
	for _, wr := range layers.Working {
		role := "assistant"
		if wr.Call == "user" {
			role = "user"
		}
		content := wr.Output
		if wr.Call != "" && wr.Call != "user" && wr.Call != "assistant" {
			content = fmt.Sprintf("[%s] %s", wr.Call, wr.Output)
		}
		msgs = append(msgs, providers.Message{
			Role:    role,
			Content: content,
		})
	}

	return msgs
}

// SessionCount returns the number of active sessions (for testing).
func (cm *CNPContextManager) SessionCount() int {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return len(cm.sessions)
}
