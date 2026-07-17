//go:build !mipsle && !netbsd && !(freebsd && arm)

package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/zhazhaku/reef/pkg/compressor"
	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/providers"
	"github.com/zhazhaku/reef/pkg/providers/protocoltypes"
	"github.com/zhazhaku/reef/pkg/seahorse"
	"github.com/zhazhaku/reef/pkg/session"
	"github.com/zhazhaku/reef/pkg/tokenizer"
)

// compressPrefix is prepended to tool-output content that has been compressed
// by the IngestHook. Messages carrying this prefix are decompressed on Assemble.
const compressPrefix = "[COMPRESSED]"

// seahorseConfig is the JSON configuration for the seahorse context manager.
// It is parsed from the raw json.RawMessage passed to newSeahorseContextManager.
type seahorseConfig struct {
	// CompressToolOutput enables lossy compression of tool-role messages via
	// the compressor package's IngestHook. Defaults to false (no compression).
	CompressToolOutput bool `json:"compress_tool_output,omitempty"`
}

// seahorseContextManager adapts seahorse.Engine to agent.ContextManager.
type seahorseContextManager struct {
	engine             *seahorse.Engine
	sessions           session.SessionStore // for startup bootstrap
	ingestHook         *compressor.IngestHook
	compressToolOutput bool
}

// newSeahorseContextManager creates a seahorse-backed ContextManager.
func newSeahorseContextManager(cfgRaw json.RawMessage, al *AgentLoop) (ContextManager, error) {
	if al == nil {
		return nil, fmt.Errorf("seahorse: AgentLoop is required")
	}

	// Parse seahorse-specific config
	var cfg seahorseConfig
	if len(cfgRaw) > 0 {
		if err := json.Unmarshal(cfgRaw, &cfg); err != nil {
			return nil, fmt.Errorf("seahorse: parse config: %w", err)
		}
	}

	// Resolve workspace for DB path
	// DB stores session data, so it goes in sessions/ directory
	agent := al.registry.GetDefaultAgent()
	dbPath := agent.Workspace + "/sessions/seahorse.db"

	// Create CompleteFn from provider
	completeFn := providerToCompleteFn(agent.Provider, agent.Model)

	// Create engine
	engine, err := seahorse.NewEngine(seahorse.Config{
		DBPath: dbPath,
	}, completeFn)
	if err != nil {
		return nil, fmt.Errorf("seahorse: create engine: %w", err)
	}

	mgr := &seahorseContextManager{
		engine:             engine,
		sessions:           agent.Sessions,
		compressToolOutput: cfg.CompressToolOutput,
	}

	// Initialize compressor hook for tool output compression
	if cfg.CompressToolOutput {
		router := compressor.NewContentRouter()
		// Register compressors from the global registry into the router
		for _, name := range compressor.Names() {
			c := compressor.Get(name)
			if c != nil {
				// Map compressor names to content types for the router
				switch name {
				case "smart_crusher":
					router.Register("application/json", c)
				case "code_compressor":
					router.Register("text/code", c)
				case "log_compressor":
					router.Register("text/plain", c)
				}
			}
		}
		mgr.ingestHook = compressor.NewIngestHook(router)
		logger.InfoCF("agent", "seahorse: compressor ingest hook enabled", nil)
	}

	// Register seahorse tools with the agent's tool registry
	retrieval := mgr.engine.GetRetrieval()
	al.RegisterTool(seahorse.NewGrepTool(retrieval))
	al.RegisterTool(seahorse.NewExpandTool(retrieval))

	// Bootstrap all existing sessions at startup
	if agent.Sessions != nil {
		ctx := context.Background()
		for _, sessionKey := range agent.Sessions.ListSessions() {
			mgr.bootstrapSession(ctx, sessionKey)
		}
	}

	return mgr, nil
}

// DB returns the underlying SQLite database for shared use by other
// components (e.g., ModeStore for per-conversation mode persistence).
func (m *seahorseContextManager) DB() *sql.DB {
	return m.engine.GetRetrieval().Store().DB()
}

// providerToCompleteFn wraps providers.LLMProvider as a seahorse.CompleteFn.
func providerToCompleteFn(provider providers.LLMProvider, model string) seahorse.CompleteFn {
	return func(ctx context.Context, prompt string, opts seahorse.CompleteOptions) (string, error) {
		resp, err := provider.Chat(
			ctx,
			[]providers.Message{{Role: "user", Content: prompt}},
			nil, // no tools for summarization
			model,
			map[string]any{
				"max_tokens":       opts.MaxTokens,
				"temperature":      opts.Temperature,
				"prompt_cache_key": "seahorse",
			},
		)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}

// Assemble builds budget-aware context from seahorse SQLite.
func (m *seahorseContextManager) Assemble(ctx context.Context, req *AssembleRequest) (*AssembleResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("seahorse assemble: nil request")
	}

	budget := req.Budget
	if budget <= 0 {
		budget = 100000
	}

	// Reserve space for model response (spec lines 1400-1410)
	effectiveBudget := budget - req.MaxTokens
	if effectiveBudget <= 0 {
		// MaxTokens >= budget is a configuration problem
		// Use 50% as minimum to avoid guaranteed overflow
		logger.WarnCF("agent", "MaxTokens >= budget, using 50% fallback",
			map[string]any{"budget": budget, "max_tokens": req.MaxTokens})
		effectiveBudget = budget / 2
	}

	result, err := m.engine.Assemble(ctx, req.SessionKey, seahorse.AssembleInput{
		Budget: effectiveBudget,
	})
	if err != nil {
		return nil, fmt.Errorf("seahorse assemble: %w", err)
	}

	history := seahorseToProviderMessages(result)

	// Decompress any tool outputs that were compressed during Ingest.
	// Structural compressors (SmartCrusher, CodeCompressor, etc.) are lossy —
	// decompression is a pass-through that returns the already-compressed form.
	// Future CCR-backed decompression will restore the original from the CCR store.
	for i := range history {
		if strings.HasPrefix(history[i].Content, compressPrefix) {
			history[i].Content = strings.TrimPrefix(history[i].Content, compressPrefix)
			// Content is now in its compressed (lossy) form — this is the
			// intended state for structural compressors that cannot reverse.
		}
	}

	// Summary is already formatted as XML with system prompt addition by assembler
	return &AssembleResponse{
		History: history,
		Summary: result.Summary,
	}, nil
}

// Compact compresses conversation history via seahorse summarization.
func (m *seahorseContextManager) Compact(ctx context.Context, req *CompactRequest) error {
	if req == nil {
		return nil
	}

	// Create checkpoint before compaction to preserve active context.
	// Scans recent assistant messages for files/tasks/decisions so the
	// LLM doesn't forget what it was doing after compression kicks in.
	m.createAndInjectCheckpoint(ctx, req.SessionKey)

	// For retry (LLM overflow) or proactive (pre-LLM budget check),
	// use CompactUntilUnder to guarantee context shrinks below budget.
	// Plain Compact() treats Budget as advisory only and may skip compression
	// when its own tokenizer disagrees with our estimate.
	if req.Budget > 0 && (req.Reason == ContextCompressReasonRetry || req.Reason == ContextCompressReasonProactive) {
		_, err := m.engine.CompactUntilUnder(ctx, req.SessionKey, req.Budget)
		return err
	}

	_, err := m.engine.Compact(ctx, req.SessionKey, seahorse.CompactInput{
		Force:  req.Reason == ContextCompressReasonRetry,
		Budget: &req.Budget,
	})
	return err
}

// createAndInjectCheckpoint scans recent assistant messages for active
// context (files, tasks, decisions) and persists a checkpoint summary
// so the LLM retains critical awareness after compaction.
func (m *seahorseContextManager) createAndInjectCheckpoint(ctx context.Context, sessionKey string) {
	store := m.engine.GetRetrieval().Store()
	conv, err := store.GetConversationBySessionKey(ctx, sessionKey)
	if err != nil || conv == nil {
		return
	}

	// Get recent messages (last 12, scanning up to 6 assistant messages)
	msgs, err := store.GetMessages(ctx, conv.ConversationID, 30, 0)
	if err != nil || len(msgs) == 0 {
		return
	}

	// Collect assistant messages from the tail
	type extract struct {
		files     []string
		tasks     []string
		decisions []string
	}
	var e extract
	seenFiles := map[string]bool{}
	scanned := 0

	for i := len(msgs) - 1; i >= 0 && scanned < 8; i-- {
		if msgs[i].Role != "assistant" {
			continue
		}
		scanned++
		content := msgs[i].Content
		// Also check parts for tool_use details
		for _, p := range msgs[i].Parts {
			if p.Type == "tool_use" {
				content += " " + p.Name + " " + p.Arguments
			}
		}

		// Extract file references
		for _, fn := range extractFilesFromContent(content) {
			if !seenFiles[fn] {
				e.files = append(e.files, fn)
				seenFiles[fn] = true
			}
		}

		// Extract tasks (TODO, FIXME, working on, need to)
		e.tasks = append(e.tasks, extractTasksFromContent(content)...)

		// Extract decisions
		e.decisions = append(e.decisions, extractDecisionsFromContent(content)...)
	}

	if len(e.files) == 0 && len(e.tasks) == 0 && len(e.decisions) == 0 {
		return
	}

	// Format checkpoint
	var sb strings.Builder
	sb.WriteString("## Checkpoint (pre-compaction)\\n\\n")
	if len(e.files) > 0 {
		sb.WriteString("**Active Files:**\\n")
		for _, f := range e.files {
			sb.WriteString("- `" + f + "`\\n")
		}
		sb.WriteString("\\n")
	}
	if len(e.tasks) > 0 {
		sb.WriteString("**Active Tasks:**\\n")
		for _, t := range e.tasks {
			sb.WriteString("- " + t + "\\n")
		}
		sb.WriteString("\\n")
	}
	if len(e.decisions) > 0 {
		sb.WriteString("**Active Decisions:**\\n")
		for _, d := range e.decisions {
			sb.WriteString("- " + d + "\\n")
		}
		sb.WriteString("\\n")
	}

	// Ingest checkpoint as a system message
	checkpointMsg := seahorse.Message{
		Role:       "system",
		Content:    strings.TrimSpace(sb.String()),
		TokenCount: tokenizer.EstimateMessageTokens(providers.Message{Content: sb.String()}),
	}
	if _, err := m.engine.Ingest(ctx, sessionKey, []seahorse.Message{checkpointMsg}); err != nil {
		logger.WarnCF("seahorse", "checkpoint: ingest failed", map[string]any{
			"session": sessionKey,
			"error":   err.Error(),
		})
	} else {
		logger.InfoCF("seahorse", "checkpoint: created", map[string]any{
			"session":   sessionKey,
			"files":     len(e.files),
			"tasks":     len(e.tasks),
			"decisions": len(e.decisions),
		})
	}
}

// extractFilesFromContent extracts file paths from assistant message content.
// Matches: write_file("..."), edit_file("..."), append_file("..."), read_file("...")
func extractFilesFromContent(content string) []string {
	// Match file operations with quoted paths
	re := regexp.MustCompile(`(?:write_file|edit_file|append_file|read_file|load_image)\s*\(\s*"(?P<path>[^"]+)"`)
	matches := re.FindAllStringSubmatch(content, -1)
	var files []string
	for _, m := range matches {
		if len(m) >= 2 && m[1] != "" {
			// Take basename for readability
			fn := filepath.Base(m[1])
			if fn != "." && fn != "/" {
				files = append(files, fn)
			}
		}
	}
	return files
}

// extractTasksFromContent extracts task-related phrases from content.
func extractTasksFromContent(content string) []string {
	var tasks []string
	patterns := []string{
		`(?i)(?:TODO|FIXME|HACK|WORKAROUND)[:\s]+(.+?)(?:\n|$)`,
		`(?i)(?:need to|working on|implementing|fixing|debugging|adding|updating|refactoring)\s+(.+?)(?:\.|;|\n|$)`,
		`(?i)(?:I'll|I will|let me|going to)\s+(.+?)(?:\.|;|\n|$)`,
	}
	for _, pat := range patterns {
		re := regexp.MustCompile(pat)
		matches := re.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) >= 2 {
				task := strings.TrimSpace(m[1])
				if len(task) > 3 && len(task) < 200 {
					tasks = append(tasks, task)
				}
			}
		}
	}
	return uniqueStrings(tasks)
}

// extractDecisionsFromContent extracts decision phrases from content.
func extractDecisionsFromContent(content string) []string {
	var decisions []string
	patterns := []string{
		`(?i)(?:decided to|decision:|choose|I'll use|let's go with|use\s+\S+\s+(?:over|instead of))\s+(.+?)(?:\.|;|\n|$)`,
		`(?i)(?:better to|prefer|recommend)\s+(.+?)(?:\.|;|\n|$)`,
	}
	for _, pat := range patterns {
		re := regexp.MustCompile(pat)
		matches := re.FindAllStringSubmatch(content, -1)
		for _, m := range matches {
			if len(m) >= 2 {
				dec := strings.TrimSpace(m[1])
				if len(dec) > 3 && len(dec) < 200 {
					decisions = append(decisions, dec)
				}
			}
		}
	}
	return uniqueStrings(decisions)
}

func uniqueStrings(ss []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}

// Ingest records a message into seahorse SQLite. Tool-role messages are
// compressed via IngestHook when CompressToolOutput is enabled.
// All existing sessions are bootstrapped at startup, so this only ingests new messages.
func (m *seahorseContextManager) Ingest(ctx context.Context, req *IngestRequest) error {
	if req == nil {
		return nil
	}

	msg := req.Message

	// Compress tool outputs before storing
	if m.compressToolOutput && m.ingestHook != nil && msg.Role == "tool" && msg.Content != "" {
		compressed, err := m.ingestHook.Process(ctx, []byte(msg.Content))
		if err != nil {
			// Compression failure is non-fatal — store uncompressed original
			logger.WarnCF("agent", "seahorse ingest: compression failed, storing original",
				map[string]any{"error": err.Error()})
		} else {
			// Store compressed content with marker for detection on retrieval
			msg.Content = compressPrefix + compressed.Content
			logger.DebugCF("agent", "seahorse ingest: tool output compressed",
				map[string]any{
					"role":           msg.Role,
					"original_sz":    compressed.OriginalSz,
					"compressed_sz":  compressed.FinalSz,
					"ratio":          fmt.Sprintf("%.2f", compressed.Ratio),
				})
		}
	}

	seahorseMsg := providerToSeahorseMessage(msg)
	_, err := m.engine.Ingest(ctx, req.SessionKey, []seahorse.Message{seahorseMsg})
	return err
}

// Clear removes all stored context for a session (seahorse DB + JSONL).
func (m *seahorseContextManager) Clear(ctx context.Context, sessionKey string) error {
	if err := m.engine.ClearSession(ctx, sessionKey); err != nil {
		return err
	}
	if m.sessions != nil {
		m.sessions.SetHistory(sessionKey, []providers.Message{})
		m.sessions.SetSummary(sessionKey, "")
		return m.sessions.Save(sessionKey)
	}
	return nil
}

// bootstrapSession reconciles JSONL session history into seahorse SQLite.
func (m *seahorseContextManager) bootstrapSession(ctx context.Context, sessionKey string) {
	if m.sessions == nil {
		return
	}

	history := m.sessions.GetHistory(sessionKey)
	if len(history) == 0 {
		return
	}

	// Convert provider messages to seahorse messages
	msgs := make([]seahorse.Message, len(history))
	for i, h := range history {
		msgs[i] = providerToSeahorseMessage(h)
	}

	if err := m.engine.Bootstrap(ctx, sessionKey, msgs); err != nil {
		logger.WarnCF("seahorse", "bootstrap", map[string]any{
			"session": sessionKey,
			"error":   err.Error(),
		})
	}
}

// providerToSeahorseMessage converts a providers.Message to a seahorse.Message.
func providerToSeahorseMessage(msg protocoltypes.Message) seahorse.Message {
	hasToolCalls := len(msg.ToolCalls) > 0
	reasoningContent := msg.ReasoningContent
	if !hasToolCalls {
		// Strip reasoning_content for non-tool-call messages.
		// DeepSeek V4 rule: only tool-call turns need reasoning round-tripped.
		reasoningContent = ""
	}

	result := seahorse.Message{
		Role:                    msg.Role,
		Content:                 msg.Content,
		ReasoningContent:        reasoningContent,
		ReasoningContentPresent: hasToolCalls && msg.ReasoningContentPresent,
		TokenCount:              tokenizer.EstimateMessageTokens(msg),
	}

	// Convert ToolCalls → MessageParts
	for _, tc := range msg.ToolCalls {
		part := seahorse.MessagePart{
			Type:       "tool_use",
			Name:       tc.Function.Name,
			Arguments:  tc.Function.Arguments,
			ToolCallID: tc.ID,
		}
		result.Parts = append(result.Parts, part)
	}

	// Convert tool result
	if msg.ToolCallID != "" {
		part := seahorse.MessagePart{
			Type:       "tool_result",
			ToolCallID: msg.ToolCallID,
			Text:       msg.Content,
		}
		result.Parts = append(result.Parts, part)
	}

	// Convert media attachments
	for _, mediaURI := range msg.Media {
		part := seahorse.MessagePart{
			Type:     "media",
			MediaURI: mediaURI,
		}
		result.Parts = append(result.Parts, part)
	}

	return result
}

// seahorseToProviderMessages converts a seahorse.AssembleResult to []providers.Message.
func seahorseToProviderMessages(result *seahorse.AssembleResult) []protocoltypes.Message {
	messages := make([]protocoltypes.Message, 0, len(result.Messages))

	// Convert assembled messages (which already include summary XML messages)
	for _, msg := range result.Messages {
		pm := protocoltypes.Message{
			Role:                    msg.Role,
			Content:                 msg.Content,
			ReasoningContent:        msg.ReasoningContent,
			ReasoningContentPresent: msg.ReasoningContentPresent,
		}

		// Reconstruct ToolCalls from parts
		for _, part := range msg.Parts {
			if part.Type == "tool_use" {
				pm.ToolCalls = append(pm.ToolCalls, protocoltypes.ToolCall{
					ID:   part.ToolCallID,
					Type: "function", // Required by OpenAI-compatible APIs (GLM, etc.)
					Function: &protocoltypes.FunctionCall{
						Name:      part.Name,
						Arguments: part.Arguments,
					},
				})
			}
			if part.Type == "tool_result" {
				pm.ToolCallID = part.ToolCallID
				if pm.Content == "" && part.Text != "" {
					pm.Content = part.Text
				}
			}
			if part.Type == "media" && part.MediaURI != "" {
				pm.Media = append(pm.Media, part.MediaURI)
			}
		}

		// Strip reasoning_content when message has no tool_calls.
		// DeepSeek V4 thinking mode rule: only messages with tool_calls
		// must round-trip reasoning_content. Stripping it for non-tool-call
		// messages prevents context bloat (60k+ tokens wasted on reasoning).
		if len(pm.ToolCalls) == 0 {
			pm.ReasoningContent = ""
			pm.ReasoningContentPresent = false
		}

		messages = append(messages, pm)
	}

	return messages
}

func init() {
	if err := RegisterContextManager("seahorse", newSeahorseContextManager); err != nil {
		panic(fmt.Sprintf("register seahorse context manager: %v", err))
	}
}
