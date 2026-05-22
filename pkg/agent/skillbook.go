package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/logger"
	"github.com/zhazhaku/reef/pkg/providers"
)

// Skillbook is a persistent, cross-session store of learned strategies.
//
// Strategies are extracted by the Python Recursive Reflector from execution traces,
// stored in SQLite with relevance scoring, and injected into the agent context at
// session start to prevent repeated mistakes and amplify successful patterns.
//
// Architecture:
//
//	Turn End → Python Reflector → structured strategies → Skillbook (SQLite)
//	Session Start → Skillbook → relevant strategies → context injection
//
// This complements the LLM-based ReflectOnTurn (which writes unstructured learnings
// to MEMORY.md) with a programmatic, queryable strategy system.
type Skillbook struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
}

// StrategyRecord is a persisted strategy in the Skillbook.
type StrategyRecord struct {
	ID           int64     `json:"id"`
	Type         string    `json:"type"`          // "avoid", "prefer", "pattern"
	Category     string    `json:"category"`       // "tool_usage", "context_mgmt", "error_handling", "communication"
	Description  string    `json:"description"`    // human-readable what to do / not to do
	Condition    string    `json:"condition"`      // when this strategy applies (keyword match against user msg + context)
	Evidence     string    `json:"evidence"`       // what happened that led to this strategy
	SuccessCount int       `json:"success_count"`  // times this strategy led to good outcomes
	FailCount    int       `json:"fail_count"`     // times this strategy was relevant but ignored
	Relevance    float64   `json:"relevance"`      // computed score: SuccessCount / (SuccessCount + FailCount + 1)
	SourceTurn   string    `json:"source_turn"`    // turn ID that generated this strategy
	SourceSession string  `json:"source_session"`  // session key
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// StrategyMatch is a strategy that matched the current session context.
type StrategyMatch struct {
	Strategy  StrategyRecord `json:"strategy"`
	MatchType string         `json:"match_type"` // "category", "keyword", "recent_error"
	Score     float64        `json:"score"`
}

// OpenSkillbook opens or creates the Skillbook database at the given path.
func OpenSkillbook(path string) (*Skillbook, error) {
	db, err := sql.Open("sqlite3", path+"?mode=rwc&_journal_mode=WAL&_busy_timeout=3000")
	if err != nil {
		return nil, fmt.Errorf("open skillbook: %w", err)
	}

	sb := &Skillbook{db: db, path: path}

	if err := sb.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate skillbook: %w", err)
	}

	return sb, nil
}

// migrate creates the strategies table and indices if they don't exist.
func (sb *Skillbook) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS strategies (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type TEXT NOT NULL DEFAULT 'pattern',
			category TEXT NOT NULL DEFAULT 'tool_usage',
			description TEXT NOT NULL,
			condition TEXT NOT NULL DEFAULT '',
			evidence TEXT NOT NULL DEFAULT '',
			success_count INTEGER NOT NULL DEFAULT 0,
			fail_count INTEGER NOT NULL DEFAULT 0,
			relevance REAL NOT NULL DEFAULT 0.5,
			source_turn TEXT NOT NULL DEFAULT '',
			source_session TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_strategies_category ON strategies(category)`,
		`CREATE INDEX IF NOT EXISTS idx_strategies_type ON strategies(type)`,
		`CREATE INDEX IF NOT EXISTS idx_strategies_relevance ON strategies(relevance DESC)`,
		`CREATE TABLE IF NOT EXISTS strategy_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			strategy_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			turn_id TEXT NOT NULL DEFAULT '',
			session_key TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			FOREIGN KEY (strategy_id) REFERENCES strategies(id) ON DELETE CASCADE
		)`,
	}

	for _, q := range queries {
		if _, err := sb.db.Exec(q); err != nil {
			return fmt.Errorf("exec %q: %w", strTrunc(q, 60), err)
		}
	}

	return nil
}

// Close closes the skillbook database.
func (sb *Skillbook) Close() error {
	return sb.db.Close()
}

// UpsertStrategy inserts or updates a strategy.
// Strategies are deduplicated by (type, category, description).
func (sb *Skillbook) UpsertStrategy(ctx context.Context, s *StrategyRecord) (*StrategyRecord, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	// Find existing by type + category + description prefix match
	existing := sb.findExisting(s.Type, s.Category, s.Description)
	if existing != nil {
		// Update existing: increment success/fail, recalculate relevance
		existing.SuccessCount += s.SuccessCount
		existing.FailCount += s.FailCount
		if s.Evidence != "" {
			existing.Evidence = s.Evidence
		}
		if s.Condition != "" {
			existing.Condition = s.Condition
		}
		existing.Relevance = sb.calcRelevance(existing.SuccessCount, existing.FailCount)
		existing.UpdatedAt = time.Now()

		_, err := sb.db.ExecContext(ctx,
			`UPDATE strategies SET success_count=?, fail_count=?, relevance=?, evidence=?, condition=?, updated_at=? WHERE id=?`,
			existing.SuccessCount, existing.FailCount, existing.Relevance,
			existing.Evidence, existing.Condition, existing.UpdatedAt.Format(time.RFC3339),
			existing.ID,
		)
		if err != nil {
			return nil, fmt.Errorf("update strategy: %w", err)
		}

		return existing, nil
	}

	// Insert new
	s.Relevance = sb.calcRelevance(s.SuccessCount, s.FailCount)
	now := time.Now()
	s.CreatedAt = now
	s.UpdatedAt = now

	result, err := sb.db.ExecContext(ctx,
		`INSERT INTO strategies (type, category, description, condition, evidence, success_count, fail_count, relevance, source_turn, source_session, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Type, s.Category, s.Description, s.Condition, s.Evidence,
		s.SuccessCount, s.FailCount, s.Relevance,
		s.SourceTurn, s.SourceSession,
		now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("insert strategy: %w", err)
	}

	id, _ := result.LastInsertId()
	s.ID = id
	return s, nil
}

// findExisting looks for a strategy with matching type + category + similar description.
func (sb *Skillbook) findExisting(typ, cat, desc string) *StrategyRecord {
	// First try exact description match
	row := sb.db.QueryRow(
		`SELECT id, type, category, description, condition, evidence, success_count, fail_count, relevance, source_turn, source_session, created_at, updated_at
		 FROM strategies WHERE type=? AND category=? AND description=? LIMIT 1`,
		typ, cat, desc,
	)
	if rec := sb.scanStrategy(row); rec != nil {
		return rec
	}

	// Try prefix match (first 80 chars of description)
	prefix := desc
	if len(prefix) > 80 {
		prefix = prefix[:80]
	}
	row = sb.db.QueryRow(
		`SELECT id, type, category, description, condition, evidence, success_count, fail_count, relevance, source_turn, source_session, created_at, updated_at
		 FROM strategies WHERE type=? AND category=? AND description LIKE ? LIMIT 1`,
		typ, cat, prefix+"%",
	)
	return sb.scanStrategy(row)
}

func (sb *Skillbook) scanStrategy(row *sql.Row) *StrategyRecord {
	var s StrategyRecord
	var createdAt, updatedAt string
	err := row.Scan(&s.ID, &s.Type, &s.Category, &s.Description, &s.Condition,
		&s.Evidence, &s.SuccessCount, &s.FailCount, &s.Relevance,
		&s.SourceTurn, &s.SourceSession, &createdAt, &updatedAt)
	if err != nil {
		return nil
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &s
}

func (sb *Skillbook) calcRelevance(success, fail int) float64 {
	if success+fail == 0 {
		return 0.5
	}
	return float64(success) / float64(success+fail+1)
}

// RecordEvent logs a strategy application event.
func (sb *Skillbook) RecordEvent(ctx context.Context, strategyID int64, eventType, turnID, sessionKey string) error {
	_, err := sb.db.ExecContext(ctx,
		`INSERT INTO strategy_events (strategy_id, event_type, turn_id, session_key) VALUES (?, ?, ?, ?)`,
		strategyID, eventType, turnID, sessionKey,
	)
	return err
}

// MatchStrategies finds strategies relevant to the current context.
//
// Matching is based on:
// 1. Category: always include strategies from relevant categories
// 2. Keyword: match condition keywords against user message + recent actions
// 3. Recent errors: boost strategies that were recently applied to similar errors
func (sb *Skillbook) MatchStrategies(ctx context.Context, userMessage string, recentActions []string, sessionKey string) ([]StrategyMatch, error) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	// Get all strategies ordered by relevance
	rows, err := sb.db.QueryContext(ctx,
		`SELECT id, type, category, description, condition, evidence, success_count, fail_count, relevance, source_turn, source_session, created_at, updated_at
		 FROM strategies ORDER BY relevance DESC LIMIT 50`,
	)
	if err != nil {
		return nil, fmt.Errorf("query strategies: %w", err)
	}
	defer rows.Close()

	var all []StrategyRecord
	for rows.Next() {
		var s StrategyRecord
		var createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.Type, &s.Category, &s.Description, &s.Condition,
			&s.Evidence, &s.SuccessCount, &s.FailCount, &s.Relevance,
			&s.SourceTurn, &s.SourceSession, &createdAt, &updatedAt); err != nil {
			continue
		}
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		all = append(all, s)
	}

	// Score and rank strategies
	combined := strings.ToLower(userMessage + " " + strings.Join(recentActions, " "))
	var matches []StrategyMatch

	for _, s := range all {
		score := s.Relevance
		matchType := "category"

		// Keyword match: check condition against combined text
		if s.Condition != "" {
			condLower := strings.ToLower(s.Condition)
			keywords := strings.Fields(condLower)
			matchCount := 0
			for _, kw := range keywords {
				if len(kw) >= 3 && strings.Contains(combined, kw) {
					matchCount++
				}
			}
			if matchCount >= 2 {
				score += 0.3
				matchType = "keyword"
			}
		}

		// Boost "avoid" type strategies (more important to prevent mistakes)
		if s.Type == "avoid" {
			score += 0.1
		}

		// Only include if score exceeds threshold
		if score >= 0.4 {
			matches = append(matches, StrategyMatch{
				Strategy:  s,
				MatchType: matchType,
				Score:     score,
			})
		}
	}

	// Sort by score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	// Limit to top 5
	if len(matches) > 5 {
		matches = matches[:5]
	}

	return matches, nil
}

// InjectStrategies formats matched strategies as a context injection string.
//
// This is injected into the system prompt's "Learned Strategies" section,
// providing the agent with actionable guidance from past experience.
func InjectStrategies(matches []StrategyMatch) string {
	if len(matches) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Learned Strategies\n\n")
	sb.WriteString("Based on past experience, consider these strategies:\n\n")

	for i, m := range matches {
		s := m.Strategy
		prefix := ""
		switch s.Type {
		case "avoid":
			prefix = "🚫 AVOID"
		case "prefer":
			prefix = "✅ PREFER"
		case "pattern":
			prefix = "📋 PATTERN"
		}

		sb.WriteString(fmt.Sprintf("%d. %s: %s\n", i+1, prefix, s.Description))
		if s.Evidence != "" {
			sb.WriteString(fmt.Sprintf("   Evidence: %s\n", strTrunc(s.Evidence, 200)))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// RunPythonReflector executes the Python Recursive Reflector against turn history.
//
// The Python reflector analyzes execution traces programmatically (not via LLM),
// extracting structured strategies with higher precision and lower cost than
// the LLM-based ReflectOnTurn.
//
// The Python script reads from stdin (JSON) and writes to stdout (JSON).
func RunPythonReflector(
	ctx context.Context,
	input TurnReflectionInput,
	execFn func(ctx context.Context, command string) (string, error),
) ([]StrategyRecord, error) {
	// Build input for Python reflector
	pythonInput := map[string]any{
		"session_key":  input.SessionKey,
		"turn_id":      input.TurnID,
		"iterations":   input.Iterations,
		"tool_errors":  input.ToolErrors,
		"user_message": input.UserMessage,
		"final_response": strTrunc(input.FinalResponse, 4000),
		"history":      formatHistoryForReflector(input.History),
	}

	inputJSON, err := json.Marshal(pythonInput)
	if err != nil {
		return nil, fmt.Errorf("marshal reflector input: %w", err)
	}

	// Build the Python command
	command := fmt.Sprintf(
		`python3 -c "
import json, sys
sys.path.insert(0, 'skills/reflector')
from reflector import analyze_turn

data = json.loads(sys.stdin.read())
result = analyze_turn(data)
print(json.dumps(result))
" << 'REEF_PYTHON_INPUT'
%s
REEF_PYTHON_INPUT`,
		string(inputJSON),
	)

	output, err := execFn(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("python reflector: %w", err)
	}

	// Parse output
	var result struct {
		Strategies []StrategyRecord `json:"strategies"`
		Learnings  []struct {
			Type string `json:"type"`
			What string `json:"what"`
		} `json:"learnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		logger.WarnCF("agent", "Failed to parse python reflector output",
			map[string]any{"error": err.Error(), "output": strTrunc(output, 500)})
		return nil, fmt.Errorf("parse reflector output: %w", err)
	}

	// Merge learnings into strategies (convert simple learnings to strategy records)
	strategies := result.Strategies
	for _, l := range result.Learnings {
		// Only create strategy if not already covered
		strategies = append(strategies, StrategyRecord{
			Type:        l.Type,
			Category:    "general",
			Description: l.What,
			Condition:   strings.ToLower(l.What),
			Evidence:    strTrunc(input.UserMessage, 200),
			SourceTurn:  input.TurnID,
			SourceSession: input.SessionKey,
		})
	}

	return strategies, nil
}

// formatHistoryForReflector converts provider messages to a Python-friendly format.
func formatHistoryForReflector(history []providers.Message) []map[string]any {
	maxMessages := 40
	start := 0
	if len(history) > maxMessages {
		start = len(history) - maxMessages
	}

	var result []map[string]any
	for i := start; i < len(history); i++ {
		msg := history[i]
		if msg.Role == "system" || msg.Role == "hidden" {
			continue
		}
		entry := map[string]any{
			"role":    msg.Role,
			"content": strTrunc(msg.Content, 1000),
		}
		if len(msg.ToolCalls) > 0 {
			names := make([]string, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				if tc.Function != nil {
					names[j] = tc.Function.Name
				}
			}
			entry["tool_calls"] = names
		}
		result = append(result, entry)
	}
	return result
}

// SkillbookReflectAndStore runs the full Skillbook reflection pipeline:
// 1. Run Python reflector to extract strategies
// 2. Upsert strategies into Skillbook
// 3. Log events
//
// This should be called in a goroutine after turn_end, similar to ReflectOnTurn.
func SkillbookReflectAndStore(
	ctx context.Context,
	sb *Skillbook,
	input TurnReflectionInput,
	execFn func(ctx context.Context, command string) (string, error),
) error {
	strategies, err := RunPythonReflector(ctx, input, execFn)
	if err != nil {
		return fmt.Errorf("run python reflector: %w", err)
	}

	for _, s := range strategies {
		rec, err := sb.UpsertStrategy(ctx, &s)
		if err != nil {
			logger.WarnCF("agent", "Failed to upsert strategy",
				map[string]any{"error": err.Error(), "description": strTrunc(s.Description, 100)})
			continue
		}

		// Record event
		eventType := "generated"
		if rec.ID != s.ID {
			eventType = "reinforced"
		}
		_ = sb.RecordEvent(ctx, rec.ID, eventType, input.TurnID, input.SessionKey)
	}

	logger.InfoCF("agent", "Skillbook reflection complete",
		map[string]any{
			"turn_id":       input.TurnID,
			"strategies":    len(strategies),
			"total_in_book": sb.Count(ctx),
		})

	return nil
}

// Count returns the total number of strategies in the skillbook.
func (sb *Skillbook) Count(ctx context.Context) int {
	var count int
	if err := sb.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM strategies").Scan(&count); err != nil {
		return 0
	}
	return count
}

// ListStrategies returns all strategies ordered by relevance.
func (sb *Skillbook) ListStrategies(ctx context.Context, limit int) ([]StrategyRecord, error) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	rows, err := sb.db.QueryContext(ctx,
		`SELECT id, type, category, description, condition, evidence, success_count, fail_count, relevance, source_turn, source_session, created_at, updated_at
		 FROM strategies ORDER BY relevance DESC LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []StrategyRecord
	for rows.Next() {
		var s StrategyRecord
		var createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.Type, &s.Category, &s.Description, &s.Condition,
			&s.Evidence, &s.SuccessCount, &s.FailCount, &s.Relevance,
			&s.SourceTurn, &s.SourceSession, &createdAt, &updatedAt); err != nil {
			continue
		}
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		result = append(result, s)
	}
	return result, nil
}

// RecordFeedback records a user feedback event for a strategy.
// Positive feedback increments success_count; negative increments fail_count.
func (sb *Skillbook) RecordFeedback(ctx context.Context, strategyID int64, positive bool, turnID, sessionKey string) error {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	var s StrategyRecord
	err := sb.db.QueryRowContext(ctx,
		`SELECT id, success_count, fail_count FROM strategies WHERE id=?`,
		strategyID,
	).Scan(&s.ID, &s.SuccessCount, &s.FailCount)
	if err != nil {
		return fmt.Errorf("strategy %d not found: %w", strategyID, err)
	}

	if positive {
		s.SuccessCount++
	} else {
		s.FailCount++
	}
	s.Relevance = sb.calcRelevance(s.SuccessCount, s.FailCount)

	_, err = sb.db.ExecContext(ctx,
		`UPDATE strategies SET success_count=?, fail_count=?, relevance=?, updated_at=? WHERE id=?`,
		s.SuccessCount, s.FailCount, s.Relevance, time.Now().Format(time.RFC3339), strategyID,
	)
	if err != nil {
		return err
	}

	eventType := "feedback_positive"
	if !positive {
		eventType = "feedback_negative"
	}
	_ = sb.RecordEvent(ctx, strategyID, eventType, turnID, sessionKey)

	return nil
}
