package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/zhazhaku/reef/pkg/providers"
)

// ============================================================
// L5.4 Tests: Skillbook
// ============================================================

// TestSkillbook_OpenAndMigrate verifies database creation and schema migration.
func TestSkillbook_OpenAndMigrate(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"

	sb, err := OpenSkillbook(path)
	if err != nil {
		t.Fatalf("OpenSkillbook: %v", err)
	}
	defer sb.Close()

	// Verify tables exist
	var count int
	if err := sb.db.QueryRow("SELECT COUNT(*) FROM strategies").Scan(&count); err != nil {
		t.Fatalf("strategies table query: %v", err)
	}

	if err := sb.db.QueryRow("SELECT COUNT(*) FROM strategy_events").Scan(&count); err != nil {
		t.Fatalf("strategy_events table query: %v", err)
	}
}

// TestSkillbook_UpsertNew verifies inserting a new strategy.
func TestSkillbook_UpsertNew(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	s := &StrategyRecord{
		Type:        "avoid",
		Category:    "tool_usage",
		Description: "Avoid calling exec without verifying parameters",
		Condition:   "exec error param",
		Evidence:    "exec produced errors in turn t-1",
		SourceTurn:  "turn-1",
	}

	rec, err := sb.UpsertStrategy(context.Background(), s)
	if err != nil {
		t.Fatalf("UpsertStrategy: %v", err)
	}
	if rec.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if rec.Relevance <= 0 {
		t.Errorf("expected relevance > 0, got %f", rec.Relevance)
	}

	// Verify count
	if count := sb.Count(context.Background()); count != 1 {
		t.Errorf("expected 1 strategy, got %d", count)
	}
}

// TestSkillbook_UpsertDuplicate verifies deduplication on update.
func TestSkillbook_UpsertDuplicate(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	s := &StrategyRecord{
		Type:        "prefer",
		Category:    "context_mgmt",
		Description: "Use short_grep to search past outputs instead of re-running tools",
		Condition:   "context efficiency",
		Evidence:    "first occurrence",
		SuccessCount: 1,
	}

	rec1, _ := sb.UpsertStrategy(context.Background(), s)

	// Second upsert with same type+category+description should update
	s.SuccessCount = 2 // accumulated
	s.Evidence = "second occurrence"
	rec2, err := sb.UpsertStrategy(context.Background(), s)
	if err != nil {
		t.Fatalf("second UpsertStrategy: %v", err)
	}

	if rec2.ID != rec1.ID {
		t.Errorf("expected same ID on re-upsert: %d != %d", rec2.ID, rec1.ID)
	}
	if rec2.SuccessCount != 3 { // 1 + 2
		t.Errorf("expected accumulated SuccessCount=3, got %d", rec2.SuccessCount)
	}
	if rec2.Evidence != "second occurrence" {
		t.Errorf("expected updated evidence, got %s", rec2.Evidence)
	}

	if count := sb.Count(context.Background()); count != 1 {
		t.Errorf("expected still 1 strategy, got %d", count)
	}
}

// TestSkillbook_MatchStrategies verifies strategy matching by keyword and category.
func TestSkillbook_MatchStrategies(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	// Seed strategies
	seeds := []StrategyRecord{
		{
			Type: "avoid", Category: "error_handling",
			Description: "Avoid calling exec without verifying required parameters",
			Condition:   "exec error param permission denied",
			SuccessCount: 5, FailCount: 1,
		},
		{
			Type: "prefer", Category: "tool_usage",
			Description: "Use reef_execute for large outputs",
			Condition:   "large output exec verbose",
			SuccessCount: 3, FailCount: 0,
		},
		{
			Type: "avoid", Category: "communication",
			Description: "Do not provide incorrect solutions",
			Condition:   "fix wrong broken",
			SuccessCount: 10, FailCount: 2,
		},
		{
			Type: "pattern", Category: "context_mgmt",
			Description: "Limit history size by using short_grep",
			Condition:   "context long history",
			SuccessCount: 1, FailCount: 0,
		},
	}

	for i := range seeds {
		seeds[i].SourceTurn = "turn-1"
		_, err := sb.UpsertStrategy(context.Background(), &seeds[i])
		if err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}

	// Match against a user message that should match exec errors
	matches, err := sb.MatchStrategies(context.Background(),
		"the exec command failed with permission denied error",
		[]string{"exec", "write_file"},
		"session-1",
	)
	if err != nil {
		t.Fatalf("MatchStrategies: %v", err)
	}

	if len(matches) == 0 {
		t.Fatal("expected at least one match")
	}

	// The exec error strategy should be matched (highest relevance + keyword match)
	found := false
	for _, m := range matches {
		if strings.Contains(m.Strategy.Description, "Avoid calling exec") {
			found = true
			if m.MatchType != "keyword" {
				t.Errorf("expected keyword match, got %s", m.MatchType)
			}
			break
		}
	}
	if !found {
		t.Error("expected exec avoidance strategy to match")
	}
}

// TestSkillbook_InjectStrategies verifies the context injection format.
func TestSkillbook_InjectStrategies(t *testing.T) {
	matches := []StrategyMatch{
		{
			Strategy: StrategyRecord{
				Type:        "avoid",
				Description: "Avoid calling exec without verifying parameters",
				Evidence:    "exec produced errors in turn t-1",
			},
			MatchType: "keyword",
			Score:     0.9,
		},
		{
			Strategy: StrategyRecord{
				Type:        "prefer",
				Description: "Use reef_execute for large outputs to keep context lean",
				Evidence:    "",
			},
			MatchType: "category",
			Score:     0.75,
		},
	}

	result := InjectStrategies(matches)

	if !strings.Contains(result, "## Learned Strategies") {
		t.Error("expected header")
	}
	if !strings.Contains(result, "🚫 AVOID") {
		t.Error("expected avoid prefix")
	}
	if !strings.Contains(result, "✅ PREFER") {
		t.Error("expected prefer prefix")
	}
	if !strings.Contains(result, "Avoid calling exec") {
		t.Error("expected avoid description")
	}
	if !strings.Contains(result, "reef_execute") {
		t.Error("expected prefer description")
	}
}

// TestSkillbook_InjectStrategiesEmpty verifies empty input.
func TestSkillbook_InjectStrategiesEmpty(t *testing.T) {
	result := InjectStrategies(nil)
	if result != "" {
		t.Errorf("expected empty string for nil, got %q", result)
	}

	result = InjectStrategies([]StrategyMatch{})
	if result != "" {
		t.Errorf("expected empty string for empty slice, got %q", result)
	}
}

// TestSkillbook_RecordFeedback verifies feedback recording.
func TestSkillbook_RecordFeedback(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	// Create a strategy
	s := &StrategyRecord{
		Type:        "prefer",
		Category:    "tool_usage",
		Description: "Test strategy",
		SourceTurn:  "turn-1",
	}
	rec, _ := sb.UpsertStrategy(context.Background(), s)

	// Positive feedback
	err := sb.RecordFeedback(context.Background(), rec.ID, true, "turn-2", "session-1")
	if err != nil {
		t.Fatalf("RecordFeedback positive: %v", err)
	}

	// Verify success_count incremented
	updated, _ := sb.ListStrategies(context.Background(), 1)
	if len(updated) == 0 || updated[0].SuccessCount != 1 {
		t.Errorf("expected SuccessCount=1, got %v", updated)
	}

	// Negative feedback
	err = sb.RecordFeedback(context.Background(), rec.ID, false, "turn-3", "session-1")
	if err != nil {
		t.Fatalf("RecordFeedback negative: %v", err)
	}

	updated, _ = sb.ListStrategies(context.Background(), 1)
	if len(updated) == 0 || updated[0].FailCount != 1 {
		t.Errorf("expected FailCount=1, got %v", updated)
	}
}

// TestSkillbook_RecordFeedbackNotFound verifies error on unknown strategy.
func TestSkillbook_RecordFeedbackNotFound(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	err := sb.RecordFeedback(context.Background(), 99999, true, "turn-1", "session-1")
	if err == nil {
		t.Error("expected error for non-existent strategy")
	}
}

// TestSkillbook_ListStrategies verifies listing with limit.
func TestSkillbook_ListStrategies(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	// Seed 5 strategies with varying relevance
	for i := 0; i < 5; i++ {
		s := &StrategyRecord{
			Type:         "pattern",
			Category:     "tool_usage",
			Description:  "Strategy " + string(rune('A'+i)),
			SuccessCount: i + 1,
			FailCount:    0,
			SourceTurn:   "turn-1",
		}
		sb.UpsertStrategy(context.Background(), s)
	}

	// List top 3
	list, err := sb.ListStrategies(context.Background(), 3)
	if err != nil {
		t.Fatalf("ListStrategies: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("expected 3 strategies, got %d", len(list))
	}

	// Most successful should be first (Strategy E with 5 successes)
	if list[0].SuccessCount < list[1].SuccessCount {
		t.Error("expected descending relevance order")
	}
}

// TestSkillbook_MatchStrategiesLimit verifies we cap at 5 matches.
func TestSkillbook_MatchStrategiesLimit(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	// Seed 10 strategies all with high relevance
	for i := 0; i < 10; i++ {
		s := &StrategyRecord{
			Type:         "prefer",
			Category:     "tool_usage",
			Description:  "Strategy " + string(rune('A'+i)),
			Condition:    "test match exec command",
			SuccessCount: 10,
			FailCount:    0,
			SourceTurn:   "turn-1",
		}
		sb.UpsertStrategy(context.Background(), s)
	}

	matches, err := sb.MatchStrategies(context.Background(),
		"test exec command",
		[]string{"exec"},
		"session-1",
	)
	if err != nil {
		t.Fatalf("MatchStrategies: %v", err)
	}
	if len(matches) > 5 {
		t.Errorf("expected at most 5 matches, got %d", len(matches))
	}
}

// TestSkillbook_PythonReflectorInputOutput verifies the Python reflector integration.
func TestSkillbook_PythonReflectorInputOutput(t *testing.T) {
	// Test with mock exec function
	input := TurnReflectionInput{
		SessionKey:    "test-session",
		TurnID:        "turn-1",
		Iterations:    5,
		ToolErrors:    3,
		UserMessage:   "fix the bug, this is wrong",
		FinalResponse: "I fixed it using exec. exec returned error: permission denied.",
		History:       nil,
	}

	mockExec := func(ctx context.Context, command string) (string, error) {
		// Simulate the Python reflector output
		result := map[string]any{
			"strategies": []map[string]any{
				{
					"type":        "avoid",
					"category":    "error_handling",
					"description": "Avoid calling exec without verifying required parameters",
					"condition":   "exec error param",
					"evidence":    "exec produced errors in turn turn-1",
				},
			},
			"learnings": []map[string]string{
				{"type": "mistake", "what": "Tool errors with exec"},
			},
		}
		b, _ := json.Marshal(result)
		return string(b), nil
	}

	strategies, err := RunPythonReflector(context.Background(), input, mockExec)
	if err != nil {
		t.Fatalf("RunPythonReflector: %v", err)
	}

	if len(strategies) == 0 {
		t.Fatal("expected at least one strategy")
	}

	// First strategy should be from the strategies array
	if strategies[0].Type != "avoid" {
		t.Errorf("expected avoid type, got %s", strategies[0].Type)
	}
	if strategies[0].Category != "error_handling" {
		t.Errorf("expected error_handling category, got %s", strategies[0].Category)
	}

	// Should also have a learning converted to strategy
	foundLearning := false
	for _, s := range strategies {
		if strings.Contains(s.Description, "Tool errors") {
			foundLearning = true
			break
		}
	}
	if !foundLearning {
		t.Error("expected learning to be converted to strategy")
	}
}

// TestSkillbook_PythonReflectorFormatsHistoryCorrectly verifies history formatting.
func TestSkillbook_PythonReflectorFormatsHistoryCorrectly(t *testing.T) {
	history := formatHistoryForReflector(nil)
	if len(history) != 0 {
		t.Errorf("expected empty history for nil, got %d", len(history))
	}

	// Test system/hidden messages are filtered
	msg := []providers.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "hidden", Content: "hidden message"},
		{Role: "user", Content: "user message"},
	}
	result := formatHistoryForReflector(msg)
	if len(result) != 1 {
		t.Errorf("expected 1 message after filtering system/hidden, got %d", len(result))
	}
}

// TestSkillbook_ReflectAndStoreIntegration verifies the full pipeline.
func TestSkillbook_ReflectAndStoreIntegration(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	input := TurnReflectionInput{
		SessionKey:  "test-session",
		TurnID:      "turn-1",
		Iterations:  4,
		ToolErrors:  2,
		UserMessage: "the exec command failed",
		History:     nil,
	}

	mockExec := func(ctx context.Context, command string) (string, error) {
		result := map[string]any{
			"strategies": []map[string]any{
				{
					"type":        "avoid",
					"category":    "error_handling",
					"description": "Avoid calling exec without verifying permissions",
					"condition":   "exec permission denied",
					"evidence":    "exec failed in turn turn-1",
				},
			},
			"learnings": []map[string]string{},
		}
		b, _ := json.Marshal(result)
		return string(b), nil
	}

	err := SkillbookReflectAndStore(context.Background(), sb, input, mockExec)
	if err != nil {
		t.Fatalf("SkillbookReflectAndStore: %v", err)
	}

	// Verify strategy was stored
	count := sb.Count(context.Background())
	if count != 1 {
		t.Errorf("expected 1 strategy, got %d", count)
	}

	list, _ := sb.ListStrategies(context.Background(), 1)
	if len(list) != 1 {
		t.Fatal("expected 1 strategy in list")
	}
	if list[0].Type != "avoid" {
		t.Errorf("expected avoid type, got %s", list[0].Type)
	}
}

// TestSkillbook_CalcRelevance verifies relevance calculation.
func TestSkillbook_CalcRelevance(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	// 0 successes, 0 failures → 0.5 (default for no data)
	r := sb.calcRelevance(0, 0)
	if r != 0.5 {
		t.Errorf("expected 0.5 for (0,0) (default), got %f", r)
	}

	// 5 successes, 0 failures → 5/6 ≈ 0.833
	r = sb.calcRelevance(5, 0)
	if r < 0.8 || r > 0.9 {
		t.Errorf("expected ~0.833 for (5,0), got %f", r)
	}

	// 0 successes, 5 failures → 0/6 = 0
	r = sb.calcRelevance(0, 5)
	if r != 0.0 {
		t.Errorf("expected 0.0 for (0,5), got %f", r)
	}

	// 3 successes, 2 failures → 3/6 = 0.5
	r = sb.calcRelevance(3, 2)
	if r != 0.5 {
		t.Errorf("expected 0.5 for (3,2), got %f", r)
	}
}

// TestSkillbook_RecordEvent verifies event logging.
func TestSkillbook_RecordEvent(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"
	sb, _ := OpenSkillbook(path)
	defer sb.Close()

	// Create a strategy first
	s := &StrategyRecord{
		Type: "pattern", Category: "tool_usage",
		Description: "Event test", SourceTurn: "turn-1",
	}
	rec, _ := sb.UpsertStrategy(context.Background(), s)

	err := sb.RecordEvent(context.Background(), rec.ID, "applied", "turn-2", "session-1")
	if err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	// Verify event exists
	var count int
	sb.db.QueryRow("SELECT COUNT(*) FROM strategy_events WHERE strategy_id=?", rec.ID).Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 event, got %d", count)
	}
}

// TestSkillbook_CloseAndReopen verifies data persistence.
func TestSkillbook_CloseAndReopen(t *testing.T) {
	path := t.TempDir() + "/test_skillbook.db"

	// Create and seed
	sb, _ := OpenSkillbook(path)
	s := &StrategyRecord{
		Type: "avoid", Category: "error_handling",
		Description: "Persist test", SourceTurn: "turn-1",
	}
	sb.UpsertStrategy(context.Background(), s)
	sb.Close()

	// Reopen and verify
	sb2, err := OpenSkillbook(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer sb2.Close()

	if count := sb2.Count(context.Background()); count != 1 {
		t.Errorf("expected 1 strategy after reopen, got %d", count)
	}
}

// TestSkillbookOpen_CreatesNewDB verifies creating a new database file.
func TestSkillbookOpen_CreatesNewDB(t *testing.T) {
	// SQLite doesn't auto-create directories, so we ensure the parent exists.
	dir := t.TempDir() + "/newskillbook"
	os.MkdirAll(dir, 0755)
	path := dir + "/test_skillbook.db"

	// Should create database file
	sb, err := OpenSkillbook(path)
	if err != nil {
		t.Fatalf("OpenSkillbook: %v", err)
	}
	defer sb.Close()

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database file not created: %v", err)
	}
}
