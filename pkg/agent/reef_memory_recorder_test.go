package agent

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/zhazhaku/reef/pkg/memory"
)

func TestReefMemoryRecorder_RecordComplete(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec("CREATE TABLE task_episodes (id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL, timestamp INTEGER NOT NULL, event_type TEXT NOT NULL, summary TEXT NOT NULL, tags TEXT, created_at TEXT NOT NULL DEFAULT (datetime('now')))")

	store := memory.NewEpisodicStore(db)
	rec := NewReefMemoryRecorder(store)

	rec.RecordComplete("t-1", "fix bug", "done", 5, 100*time.Millisecond, 0)

	episodes, _ := store.GetByTask("t-1")
	if len(episodes) != 1 {
		t.Fatalf("episodes = %d, want 1", len(episodes))
	}
	if episodes[0].EventType != "task_completed" {
		t.Errorf("event_type = %s", episodes[0].EventType)
	}
}

func TestReefMemoryRecorder_RecordFailed(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	db.Exec("CREATE TABLE task_episodes (id INTEGER PRIMARY KEY AUTOINCREMENT, task_id TEXT NOT NULL, timestamp INTEGER NOT NULL, event_type TEXT NOT NULL, summary TEXT NOT NULL, tags TEXT, created_at TEXT NOT NULL DEFAULT (datetime('now')))")

	store := memory.NewEpisodicStore(db)
	rec := NewReefMemoryRecorder(store)

	rec.RecordFailed("t-1", "fix bug", "db timeout", 3, 2, 0)

	episodes, _ := store.GetByTask("t-1")
	if len(episodes) != 1 {
		t.Fatalf("episodes = %d, want 1", len(episodes))
	}
	if episodes[0].EventType != "task_failed" {
		t.Errorf("event_type = %s", episodes[0].EventType)
	}
}

func TestReefMemoryRecorder_Truncate(t *testing.T) {
	// Test that long results are truncated
	long := ""
	for i := 0; i < 500; i++ {
		long += "x"
	}

	s := trunc(long, 200)
	if len(s) != 200 {
		t.Errorf("len = %d, want 200", len(s))
	}
}
