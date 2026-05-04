package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

func newTestSQLiteStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	return s, path
}

func TestSQLiteStore_SaveAndGetTask(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	task := newTestTask("t1", reef.TaskCreated, "coder")
	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	got, err := s.GetTask("t1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.ID != "t1" {
		t.Errorf("expected ID t1, got %s", got.ID)
	}
	if got.Status != reef.TaskCreated {
		t.Errorf("expected status %s, got %s", reef.TaskCreated, got.Status)
	}
	if got.RequiredRole != "coder" {
		t.Errorf("expected role coder, got %s", got.RequiredRole)
	}

	// Duplicate save should error.
	if err := s.SaveTask(task); err == nil {
		t.Error("expected error for duplicate save")
	}

	// Nil task should error.
	if err := s.SaveTask(nil); err == nil {
		t.Error("expected error for nil task")
	}
}

func TestSQLiteStore_GetTask_NotFound(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	_, err := s.GetTask("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestSQLiteStore_UpdateTask(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	task := newTestTask("t1", reef.TaskCreated, "coder")
	s.SaveTask(task)

	task.Status = reef.TaskRunning
	if err := s.UpdateTask(task); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	got, _ := s.GetTask("t1")
	if got.Status != reef.TaskRunning {
		t.Errorf("expected status %s, got %s", reef.TaskRunning, got.Status)
	}

	// Update nonexistent should error.
	bad := newTestTask("nope", reef.TaskCreated, "coder")
	if err := s.UpdateTask(bad); err == nil {
		t.Error("expected error for updating nonexistent task")
	}

	// Nil should error.
	if err := s.UpdateTask(nil); err == nil {
		t.Error("expected error for nil task")
	}
}

func TestSQLiteStore_DeleteTask(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	task := newTestTask("t1", reef.TaskCreated, "coder")
	s.SaveTask(task)

	if err := s.DeleteTask("t1"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	_, err := s.GetTask("t1")
	if err == nil {
		t.Error("expected error after delete")
	}

	// Delete nonexistent should error.
	if err := s.DeleteTask("nope"); err == nil {
		t.Error("expected error for deleting nonexistent task")
	}
}

func TestSQLiteStore_ListTasks(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	s.SaveTask(newTestTask("t1", reef.TaskCreated, "coder"))
	s.SaveTask(newTestTask("t2", reef.TaskRunning, "coder"))
	s.SaveTask(newTestTask("t3", reef.TaskCompleted, "reviewer"))
	s.SaveTask(newTestTask("t4", reef.TaskFailed, "coder"))

	// No filter — all tasks.
	all, err := s.ListTasks(TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("expected 4 tasks, got %d", len(all))
	}

	// Filter by status.
	running, _ := s.ListTasks(TaskFilter{Statuses: []reef.TaskStatus{reef.TaskRunning}})
	if len(running) != 1 || running[0].ID != "t2" {
		t.Errorf("expected 1 running task (t2), got %d", len(running))
	}

	// Filter by role.
	reviewers, _ := s.ListTasks(TaskFilter{Roles: []string{"reviewer"}})
	if len(reviewers) != 1 || reviewers[0].ID != "t3" {
		t.Errorf("expected 1 reviewer task (t3), got %d", len(reviewers))
	}

	// Filter by status + role.
	both, _ := s.ListTasks(TaskFilter{
		Statuses: []reef.TaskStatus{reef.TaskCreated},
		Roles:    []string{"coder"},
	})
	if len(both) != 1 || both[0].ID != "t1" {
		t.Errorf("expected 1 task, got %d", len(both))
	}

	// Limit.
	limited, _ := s.ListTasks(TaskFilter{Limit: 2})
	if len(limited) != 2 {
		t.Errorf("expected 2 tasks with limit, got %d", len(limited))
	}

	// Offset.
	offset, _ := s.ListTasks(TaskFilter{Offset: 2})
	if len(offset) != 2 {
		t.Errorf("expected 2 tasks with offset 2, got %d", len(offset))
	}

	// Offset beyond range.
	empty, _ := s.ListTasks(TaskFilter{Offset: 100})
	if len(empty) != 0 {
		t.Errorf("expected 0 tasks with large offset, got %d", len(empty))
	}

	// Limit + offset.
	page, _ := s.ListTasks(TaskFilter{Limit: 1, Offset: 1})
	if len(page) != 1 {
		t.Errorf("expected 1 task with limit=1 offset=1, got %d", len(page))
	}
}

func TestSQLiteStore_Attempts(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	task := newTestTask("t1", reef.TaskCreated, "coder")
	s.SaveTask(task)

	// No attempts yet.
	attempts, err := s.GetAttempts("t1")
	if err != nil {
		t.Fatalf("GetAttempts: %v", err)
	}
	if len(attempts) != 0 {
		t.Errorf("expected 0 attempts, got %d", len(attempts))
	}

	// Save an attempt.
	now := time.Now()
	a := reef.AttemptRecord{
		AttemptNumber: 1,
		StartedAt:     now,
		EndedAt:       now.Add(5 * time.Second),
		Status:        "success",
		ClientID:      "client-1",
	}
	if err := s.SaveAttempt("t1", a); err != nil {
		t.Fatalf("SaveAttempt: %v", err)
	}

	attempts, _ = s.GetAttempts("t1")
	if len(attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(attempts))
	}
	if attempts[0].Status != "success" {
		t.Errorf("expected status success, got %s", attempts[0].Status)
	}
	if attempts[0].ClientID != "client-1" {
		t.Errorf("expected client_id client-1, got %s", attempts[0].ClientID)
	}

	// Save second attempt.
	a2 := reef.AttemptRecord{
		AttemptNumber: 2,
		StartedAt:     now.Add(10 * time.Second),
		EndedAt:       now.Add(15 * time.Second),
		Status:        "failed",
		ErrorMessage:  "timeout",
		ClientID:      "client-2",
	}
	s.SaveAttempt("t1", a2)

	attempts, _ = s.GetAttempts("t1")
	if len(attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(attempts))
	}

	// Save attempt for nonexistent task.
	if err := s.SaveAttempt("nope", a); err == nil {
		t.Error("expected error for nonexistent task")
	}

	// GetAttempts for nonexistent task.
	_, err = s.GetAttempts("nope")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestSQLiteStore_TaskWithResultAndError(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	// Task with result.
	task := newTestTask("t1", reef.TaskCompleted, "coder")
	task.Result = &reef.TaskResult{
		Text:     "done",
		Files:    []string{"out.txt"},
		Metadata: map[string]any{"score": 0.95},
	}
	s.SaveTask(task)

	got, _ := s.GetTask("t1")
	if got.Result == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Result.Text != "done" {
		t.Errorf("expected result text 'done', got %s", got.Result.Text)
	}
	if len(got.Result.Files) != 1 || got.Result.Files[0] != "out.txt" {
		t.Errorf("expected files [out.txt], got %v", got.Result.Files)
	}

	// Task with error.
	task2 := newTestTask("t2", reef.TaskFailed, "coder")
	task2.Error = &reef.TaskError{
		Type:    "timeout",
		Message: "task timed out",
		Detail:  "no response in 5m",
	}
	s.SaveTask(task2)

	got2, _ := s.GetTask("t2")
	if got2.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if got2.Error.Type != "timeout" {
		t.Errorf("expected error type timeout, got %s", got2.Error.Type)
	}
}

func TestSQLiteStore_Close(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	s.SaveTask(newTestTask("t1", reef.TaskCreated, "coder"))

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// After close, operations should fail.
	_, err := s.GetTask("t1")
	if err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_WALMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal.db")
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected WAL mode, got %s", mode)
	}
}

func TestSQLiteStore_AutoDirectoryCreation(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b", "c", "test.db")
	s, err := NewSQLiteStore(nested)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(nested); os.IsNotExist(err) {
		t.Error("expected database file to exist")
	}
}

func TestSQLiteStore_ConcurrentAccess(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	var wg sync.WaitGroup
	n := 50

	// Concurrent writes.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("t%d", i)
			task := newTestTask(id, reef.TaskCreated, "coder")
			_ = s.SaveTask(task)
		}(i)
	}
	wg.Wait()

	all, _ := s.ListTasks(TaskFilter{})
	if len(all) != n {
		t.Errorf("expected %d tasks after concurrent writes, got %d", n, len(all))
	}

	// Concurrent reads.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("t%d", i)
			_, _ = s.GetTask(id)
		}(i)
	}
	wg.Wait()

	// Concurrent updates.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("t%d", i)
			t := newTestTask(id, reef.TaskRunning, "coder")
			_ = s.UpdateTask(t)
		}(i)
	}
	wg.Wait()

	// Concurrent deletes.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("t%d", i)
			_ = s.DeleteTask(id)
		}(i)
	}
	wg.Wait()

	all, _ = s.ListTasks(TaskFilter{})
	if len(all) != 0 {
		t.Errorf("expected 0 tasks after concurrent deletes, got %d", len(all))
	}
}

// Ensure SQLiteStore implements TaskStore.
var _ TaskStore = (*SQLiteStore)(nil)

// ---------------------------------------------------------------------------
// Evolution Events CRUD Tests
// ---------------------------------------------------------------------------

func TestEvolutionEventsCRUD(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	now := time.Now().UTC()

	// Insert events.
	e1 := &evolution.EvolutionEvent{
		ID:         "evt-1",
		TaskID:     "task-1",
		ClientID:   "client-a",
		EventType:  evolution.EventFailurePattern,
		Signal:     "timeout on file_read",
		RootCause:  "network latency",
		GeneID:     "",
		Strategy:   string(evolution.StrategyBalanced),
		Importance: 0.8,
		CreatedAt:  now,
	}
	if err := s.InsertEvolutionEvent(e1); err != nil {
		t.Fatalf("InsertEvolutionEvent: %v", err)
	}

	e2 := &evolution.EvolutionEvent{
		ID:         "evt-2",
		TaskID:     "task-1",
		ClientID:   "client-a",
		EventType:  evolution.EventSuccessPattern,
		Signal:     "fast response on code_gen",
		RootCause:  "",
		GeneID:     "",
		Strategy:   string(evolution.StrategyInnovate),
		Importance: 0.9,
		CreatedAt:  now.Add(1 * time.Minute),
	}
	if err := s.InsertEvolutionEvent(e2); err != nil {
		t.Fatalf("InsertEvolutionEvent e2: %v", err)
	}

	e3 := &evolution.EvolutionEvent{
		ID:         "evt-3",
		TaskID:     "task-2",
		ClientID:   "client-b",
		EventType:  evolution.EventFailurePattern,
		Signal:     "build failure",
		RootCause:  "missing dep",
		GeneID:     "",
		Strategy:   string(evolution.StrategyRepairOnly),
		Importance: 0.5,
		CreatedAt:  now.Add(2 * time.Minute),
	}
	if err := s.InsertEvolutionEvent(e3); err != nil {
		t.Fatalf("InsertEvolutionEvent e3: %v", err)
	}

	// Nil event should error.
	if err := s.InsertEvolutionEvent(nil); err == nil {
		t.Error("expected error for nil event")
	}

	// GetRecentEvents for client-a (should return 2, newest first).
	events, err := s.GetRecentEvents("client-a", 10)
	if err != nil {
		t.Fatalf("GetRecentEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].ID != "evt-2" {
		t.Errorf("expected evt-2 first (newest), got %s", events[0].ID)
	}
	if events[1].ID != "evt-1" {
		t.Errorf("expected evt-1 second, got %s", events[1].ID)
	}

	// GetRecentEvents with limit=0 returns empty slice.
	empty, err := s.GetRecentEvents("client-a", 0)
	if err != nil {
		t.Fatalf("GetRecentEvents limit=0: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("expected empty slice for limit=0, got %d", len(empty))
	}

	// MarkEventsProcessed.
	if err := s.MarkEventsProcessed([]string{"evt-1"}, "gene-1"); err != nil {
		t.Fatalf("MarkEventsProcessed: %v", err)
	}

	// Verify evt-1 is now processed (not returned by GetRecentEvents).
	events, _ = s.GetRecentEvents("client-a", 10)
	if len(events) != 1 || events[0].ID != "evt-2" {
		t.Errorf("expected only evt-2 after marking evt-1 processed, got %d", len(events))
	}

	// MarkEventsProcessed with empty slice is no-op.
	if err := s.MarkEventsProcessed([]string{}, "gene-x"); err != nil {
		t.Errorf("MarkEventsProcessed empty: %v", err)
	}

	// CountEventsByType.
	count, err := s.CountEventsByType("client-a", string(evolution.EventFailurePattern))
	if err != nil {
		t.Fatalf("CountEventsByType: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 unprocessed failures for client-a, got %d", count)
	}

	count, err = s.CountEventsByType("client-b", string(evolution.EventFailurePattern))
	if err != nil {
		t.Fatalf("CountEventsByType client-b: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 unprocessed failure for client-b, got %d", count)
	}

	// DeleteEventsBefore.
	n, err := s.DeleteEventsBefore(now.Add(90 * time.Second))
	if err != nil {
		t.Fatalf("DeleteEventsBefore: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted (evt-1, evt-2), got %d", n)
	}

	// Only evt-3 should remain.
	events, _ = s.GetRecentEvents("client-b", 10)
	if len(events) != 1 || events[0].ID != "evt-3" {
		t.Errorf("expected evt-3 to remain, got %d", len(events))
	}

	// KeepTopNEventsPerTask.
	// Insert 3 more events for task-2.
	for i := 0; i < 3; i++ {
		e := &evolution.EvolutionEvent{
			ID:         fmt.Sprintf("evt-extra-%d", i),
			TaskID:     "task-2",
			ClientID:   "client-b",
			EventType:  evolution.EventStagnation,
			Signal:     "test",
			RootCause:  "",
			Strategy:   string(evolution.StrategyBalanced),
			Importance: 0.1,
			CreatedAt:  now.Add(time.Duration(3+i) * time.Minute),
		}
		if err := s.InsertEvolutionEvent(e); err != nil {
			t.Fatalf("Insert extra event: %v", err)
		}
	}

	// Keep top 2 per task.
	if err := s.KeepTopNEventsPerTask(2); err != nil {
		t.Fatalf("KeepTopNEventsPerTask: %v", err)
	}

	// Should have at most 2 events for task-2 now.
	var count2 int
	s.db.QueryRow("SELECT COUNT(*) FROM evolution_events WHERE task_id = 'task-2'").Scan(&count2)
	if count2 != 2 {
		t.Errorf("expected 2 events for task-2, got %d", count2)
	}
}

// ---------------------------------------------------------------------------
// Genes CRUD Tests
// ---------------------------------------------------------------------------

func TestGenesCRUD(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	now := time.Now().UTC()

	// Insert a gene.
	g1 := &evolution.Gene{
		ID:              "gene-1",
		StrategyName:    "efficient-code-gen",
		Role:            "coder",
		Skills:          []string{"code-gen", "refactor"},
		MatchCondition:  "task.instruction contains 'generate'",
		ControlSignal:   "use context-aware generation",
		FailureWarnings: []string{"token_limit", "timeout"},
		SourceEvents:    []string{"evt-2"},
		SourceClientID:  "client-a",
		Version:         1,
		Status:          evolution.GeneStatusDraft,
		StagnationCount: 0,
		UseCount:        5,
		SuccessRate:     0.95,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if err := s.InsertGene(g1); err != nil {
		t.Fatalf("InsertGene: %v", err)
	}

	// Nil gene should error.
	if err := s.InsertGene(nil); err == nil {
		t.Error("expected error for nil gene")
	}

	// GetGene should return the inserted gene.
	got, err := s.GetGene("gene-1")
	if err != nil {
		t.Fatalf("GetGene: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil gene")
	}
	if got.ID != "gene-1" {
		t.Errorf("expected gene-1, got %s", got.ID)
	}
	if got.Status != evolution.GeneStatusDraft {
		t.Errorf("expected status draft, got %s", got.Status)
	}
	if len(got.Skills) != 2 || got.Skills[0] != "code-gen" {
		t.Errorf("unexpected skills: %v", got.Skills)
	}
	if len(got.FailureWarnings) != 2 {
		t.Errorf("unexpected failure_warnings: %v", got.FailureWarnings)
	}
	if len(got.SourceEvents) != 1 || got.SourceEvents[0] != "evt-2" {
		t.Errorf("unexpected source_events: %v", got.SourceEvents)
	}

	// GetGene for nonexistent returns nil, nil.
	notFound, err := s.GetGene("nonexistent")
	if err != nil {
		t.Fatalf("GetGene nonexistent: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for nonexistent gene")
	}

	// Update gene.
	g1.Status = evolution.GeneStatusApproved
	g1.UseCount = 10
	g1.SuccessRate = 0.97
	approvedTime := now.Add(1 * time.Hour)
	g1.ApprovedAt = &approvedTime
	g1.Skills = []string{"code-gen", "refactor", "test-gen"}
	g1.FailureWarnings = []string{"token_limit"}
	if err := s.UpdateGene(g1); err != nil {
		t.Fatalf("UpdateGene: %v", err)
	}

	got, _ = s.GetGene("gene-1")
	if got.Status != evolution.GeneStatusApproved {
		t.Errorf("expected approved, got %s", got.Status)
	}
	if got.UseCount != 10 {
		t.Errorf("expected use_count 10, got %d", got.UseCount)
	}
	if got.ApprovedAt == nil {
		t.Error("expected non-nil approved_at")
	}
	if len(got.Skills) != 3 {
		t.Errorf("expected 3 skills, got %d", len(got.Skills))
	}

	// Update nil should error.
	if err := s.UpdateGene(nil); err == nil {
		t.Error("expected error for nil gene update")
	}

	// Update nonexistent should error.
	badGene := &evolution.Gene{ID: "nonexistent", StrategyName: "x", ControlSignal: "y", Role: "coder", CreatedAt: now, UpdatedAt: now}
	if err := s.UpdateGene(badGene); err == nil {
		t.Error("expected error for updating nonexistent gene")
	}

	// Insert more genes for filtering tests.
	g2 := &evolution.Gene{
		ID:              "gene-2",
		StrategyName:    "smart-review",
		Role:            "reviewer",
		Skills:          []string{"code-review"},
		MatchCondition:  "",
		ControlSignal:   "review with depth",
		FailureWarnings: []string{},
		SourceEvents:    []string{},
		SourceClientID:  "client-b",
		Version:         1,
		Status:          evolution.GeneStatusApproved,
		StagnationCount: 0,
		UseCount:        3,
		SuccessRate:     0.8,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.InsertGene(g2)

	g3 := &evolution.Gene{
		ID:              "gene-3",
		StrategyName:    "stagnant-strategy",
		Role:            "coder",
		Skills:          []string{"coding"},
		MatchCondition:  "",
		ControlSignal:   "stagnant signal",
		FailureWarnings: []string{},
		SourceEvents:    []string{},
		SourceClientID:  "client-c",
		Version:         1,
		Status:          evolution.GeneStatusStagnant,
		StagnationCount: 5,
		UseCount:        0,
		SuccessRate:     0.0,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.InsertGene(g3)

	// GetApprovedGenes for coder.
	approved, err := s.GetApprovedGenes("coder", 10)
	if err != nil {
		t.Fatalf("GetApprovedGenes: %v", err)
	}
	if len(approved) != 1 || approved[0].ID != "gene-1" {
		t.Errorf("expected only gene-1 approved for coder, got %d", len(approved))
	}

	// CountApprovedGenes.
	count, err := s.CountApprovedGenes("coder")
	if err != nil {
		t.Fatalf("CountApprovedGenes: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 approved coder gene, got %d", count)
	}

	count, err = s.CountApprovedGenes("reviewer")
	if err != nil {
		t.Fatalf("CountApprovedGenes reviewer: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 approved reviewer gene, got %d", count)
	}

	// GetTopGenes.
	top, err := s.GetTopGenes("coder", 10)
	if err != nil {
		t.Fatalf("GetTopGenes: %v", err)
	}
	if len(top) < 2 {
		t.Errorf("expected at least 2 coder genes, got %d", len(top))
	}
	// Verify order by success_rate DESC.
	if top[0].SuccessRate < 1.0 {
		// gene-1 has 0.97, gene-3 has 0.0
		if top[0].ID != "gene-1" {
			t.Errorf("expected gene-1 with highest success_rate, got %s", top[0].ID)
		}
	}

	// DeleteStagnantGenes.
	n, err := s.DeleteStagnantGenes()
	if err != nil {
		t.Fatalf("DeleteStagnantGenes: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 stagnant deleted, got %d", n)
	}

	// gene-3 should be gone.
	g, _ := s.GetGene("gene-3")
	if g != nil {
		t.Error("expected gene-3 to be deleted")
	}

	// gene-1 and gene-2 should still exist.
	g, _ = s.GetGene("gene-1")
	if g == nil {
		t.Error("expected gene-1 to still exist")
	}
	g, _ = s.GetGene("gene-2")
	if g == nil {
		t.Error("expected gene-2 to still exist")
	}

	// KeepTopGenesPerRole - keep only 1 per role.
	n, err = s.KeepTopGenesPerRole(1)
	if err != nil {
		t.Fatalf("KeepTopGenesPerRole: %v", err)
	}
	// gene-1 should stay (coder, highest success_rate)
	// gene-2 should stay (reviewer, only one)
	topCoder, _ := s.GetTopGenes("coder", 10)
	if len(topCoder) != 1 || topCoder[0].ID != "gene-1" {
		t.Errorf("expected only gene-1 for coder, got %d", len(topCoder))
	}
	topReviewer, _ := s.GetTopGenes("reviewer", 10)
	if len(topReviewer) != 1 || topReviewer[0].ID != "gene-2" {
		t.Errorf("expected only gene-2 for reviewer, got %d", len(topReviewer))
	}
}

// ---------------------------------------------------------------------------
// Skill Drafts CRUD Tests
// ---------------------------------------------------------------------------

func TestSkillDraftsCRUD(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	now := time.Now().UTC()

	// Save a skill draft.
	d1 := &evolution.SkillDraft{
		ID:            "sd-1",
		Role:          "coder",
		SkillName:     "code-gen-v2",
		Content:       "# Code Generation Skill\n\nOptimized for Go projects.",
		SourceGeneIDs: []string{"gene-1"},
		Status:        evolution.SkillDraftPendingReview,
		ReviewComment: "",
		CreatedAt:     now,
	}
	if err := s.SaveSkillDraft(d1); err != nil {
		t.Fatalf("SaveSkillDraft: %v", err)
	}

	// Nil draft should error.
	if err := s.SaveSkillDraft(nil); err == nil {
		t.Error("expected error for nil draft")
	}

	// GetSkillDraft.
	got, err := s.GetSkillDraft("sd-1")
	if err != nil {
		t.Fatalf("GetSkillDraft: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil draft")
	}
	if got.ID != "sd-1" {
		t.Errorf("expected sd-1, got %s", got.ID)
	}
	if got.Status != evolution.SkillDraftPendingReview {
		t.Errorf("expected pending_review, got %s", got.Status)
	}
	if got.SkillName != "code-gen-v2" {
		t.Errorf("expected code-gen-v2, got %s", got.SkillName)
	}
	if len(got.SourceGeneIDs) != 1 || got.SourceGeneIDs[0] != "gene-1" {
		t.Errorf("unexpected source_gene_ids: %v", got.SourceGeneIDs)
	}
	if got.ReviewedAt != nil {
		t.Error("expected nil reviewed_at for pending draft")
	}
	if got.PublishedAt != nil {
		t.Error("expected nil published_at for pending draft")
	}

	// GetSkillDraft for nonexistent returns nil, nil.
	notFound, err := s.GetSkillDraft("nonexistent")
	if err != nil {
		t.Fatalf("GetSkillDraft nonexistent: %v", err)
	}
	if notFound != nil {
		t.Error("expected nil for nonexistent draft")
	}

	// Update skill draft.
	reviewTime := now.Add(30 * time.Minute)
	d1.Status = evolution.SkillDraftApproved
	d1.ReviewComment = "looks good"
	d1.ReviewedAt = &reviewTime
	d1.SourceGeneIDs = []string{"gene-1", "gene-2"}
	if err := s.UpdateSkillDraft(d1); err != nil {
		t.Fatalf("UpdateSkillDraft: %v", err)
	}

	got, _ = s.GetSkillDraft("sd-1")
	if got.Status != evolution.SkillDraftApproved {
		t.Errorf("expected approved, got %s", got.Status)
	}
	if got.ReviewComment != "looks good" {
		t.Errorf("expected review comment, got %s", got.ReviewComment)
	}
	if got.ReviewedAt == nil {
		t.Error("expected non-nil reviewed_at")
	}
	if len(got.SourceGeneIDs) != 2 {
		t.Errorf("expected 2 source_gene_ids, got %d", len(got.SourceGeneIDs))
	}

	// Update nil should error.
	if err := s.UpdateSkillDraft(nil); err == nil {
		t.Error("expected error for nil draft update")
	}

	// Update nonexistent should error.
	badDraft := &evolution.SkillDraft{ID: "nonexistent", Role: "coder", SkillName: "x", Content: "y", SourceGeneIDs: []string{"a"}, CreatedAt: now}
	if err := s.UpdateSkillDraft(badDraft); err == nil {
		t.Error("expected error for updating nonexistent draft")
	}

	// Publish the draft.
	publishTime := now.Add(1 * time.Hour)
	d1.Status = evolution.SkillDraftPublished
	d1.PublishedAt = &publishTime
	if err := s.UpdateSkillDraft(d1); err != nil {
		t.Fatalf("UpdateSkillDraft publish: %v", err)
	}

	got, _ = s.GetSkillDraft("sd-1")
	if got.Status != evolution.SkillDraftPublished {
		t.Errorf("expected published, got %s", got.Status)
	}
	if got.PublishedAt == nil {
		t.Error("expected non-nil published_at")
	}
	// reviewed_at should still be set.
	if got.ReviewedAt == nil {
		t.Error("expected reviewed_at to still be set after publish")
	}
}
