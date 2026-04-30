package store

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
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
