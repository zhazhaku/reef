package memory

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Create the schema needed for episodic store
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS task_episodes (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id    TEXT NOT NULL,
			timestamp  INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			summary    TEXT NOT NULL,
			tags       TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestEpisodicStore_Save(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	entry := NewEpisodicEntry("task-001", "success", "Task completed successfully", "go", "refactor")
	if err := store.Save(entry); err != nil {
		t.Fatal(err)
	}
	if entry.ID == 0 {
		t.Error("ID should be set after Save")
	}
}

func TestEpisodicStore_GetByTask(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	store.Save(NewEpisodicEntry("t-1", "success", "done", "go"))
	store.Save(NewEpisodicEntry("t-2", "failure", "failed: nil pointer", "bug"))
	store.Save(NewEpisodicEntry("t-1", "checkpoint", "saved at round 5"))

	episodes, err := store.GetByTask("t-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 {
		t.Fatalf("count = %d, want 2", len(episodes))
	}
	if episodes[0].EventType != "success" {
		t.Errorf("first event = %s", episodes[0].EventType)
	}
	if episodes[1].EventType != "checkpoint" {
		t.Errorf("second event = %s", episodes[1].EventType)
	}
}

func TestEpisodicStore_Search(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	store.Save(NewEpisodicEntry("t-1", "failure", "failed: nil pointer deref", "bug"))
	store.Save(NewEpisodicEntry("t-2", "success", "refactored auth module", "go"))
	store.Save(NewEpisodicEntry("t-3", "failure", "test nil check", "test"))

	results, err := store.Search("nil", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("search count = %d, want 2", len(results))
	}
}

func TestEpisodicStore_Search_Limit(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	for i := 0; i < 5; i++ {
		store.Save(NewEpisodicEntry("t-1", "success", "common keyword in all entries", "tag"))
	}

	results, err := store.Search("common", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("count = %d, want 2", len(results))
	}
}

func TestEpisodicStore_DeleteBefore(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	// Use a known timestamp
	old := NewEpisodicEntry("t-old", "success", "old event")
	old.Timestamp = 1000
	store.Save(old)

	current := NewEpisodicEntry("t-now", "success", "current event")
	// current has time.Now() timestamp which is > 1000
	store.Save(current)

	if err := store.DeleteBefore(2000); err != nil {
		t.Fatal(err)
	}

	count, _ := store.Count()
	if count != 1 {
		t.Errorf("count after delete = %d, want 1", count)
	}
}

func TestEpisodicStore_Count(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	n, _ := store.Count()
	if n != 0 {
		t.Errorf("initial count = %d, want 0", n)
	}

	store.Save(NewEpisodicEntry("t-1", "success", "ok"))
	store.Save(NewEpisodicEntry("t-2", "success", "ok"))

	n, _ = store.Count()
	if n != 2 {
		t.Errorf("count = %d, want 2", n)
	}
}

func TestEpisodicStore_DeleteByTask(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	store.Save(NewEpisodicEntry("t-1", "success", "ok"))
	store.Save(NewEpisodicEntry("t-2", "success", "ok"))

	if err := store.DeleteByTask("t-1"); err != nil {
		t.Fatal(err)
	}

	episodes, _ := store.GetByTask("t-1")
	if len(episodes) != 0 {
		t.Errorf("t-1 still has %d episodes", len(episodes))
	}

	episodes, _ = store.GetByTask("t-2")
	if len(episodes) != 1 {
		t.Errorf("t-2 has %d episodes, want 1", len(episodes))
	}
}

func TestEpisodicEntry_Tags(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	entry := NewEpisodicEntry("t-1", "success", "done", "go", "testing", "ci")
	store.Save(entry)

	episodes, _ := store.GetByTask("t-1")
	if len(episodes[0].Tags) != 3 {
		t.Errorf("tags = %d, want 3", len(episodes[0].Tags))
	}
	if episodes[0].Tags[0] != "go" {
		t.Errorf("tags[0] = %s", episodes[0].Tags[0])
	}
}

func TestEpisodicStore_GetByTask_Empty(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	store := NewEpisodicStore(db)

	episodes, err := store.GetByTask("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 0 {
		t.Errorf("count = %d, want 0", len(episodes))
	}
}

func TestNewEpisodicEntry(t *testing.T) {
	e := NewEpisodicEntry("t-1", "checkpoint", "saved at round 3", "state")
	if e.TaskID != "t-1" {
		t.Errorf("TaskID = %s", e.TaskID)
	}
	if e.EventType != "checkpoint" {
		t.Errorf("EventType = %s", e.EventType)
	}
	if e.Timestamp == 0 {
		t.Error("Timestamp is zero")
	}
}
