package memory

import (
	"context"
	"testing"
)

func TestMemoryLifecycle_ExtractEpisodic(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ml := NewMemoryLifecycle(NewEpisodicStore(db))

	entry, err := ml.ExtractEpisodic("task-001", "success",
		"Round 1: exec -> ok (ran)\nRound 2: write_file -> done (saved)",
		"go", "cli",
	)
	if err != nil {
		t.Fatal(err)
	}
	if entry.TaskID != "task-001" {
		t.Errorf("TaskID = %s", entry.TaskID)
	}
	if entry.EventType != "success" {
		t.Errorf("EventType = %s", entry.EventType)
	}

	episodes, _ := ml.Retrieve("task-001")
	if len(episodes) != 1 {
		t.Fatalf("retrieved = %d, want 1", len(episodes))
	}
}

func TestMemoryLifecycle_Prune(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ml := NewMemoryLifecycle(NewEpisodicStore(db))

	oldEntry := NewEpisodicEntry("old-task", "success", "old")
	oldEntry.Timestamp = 1000
	ml.store.Save(oldEntry)

	newEntry := NewEpisodicEntry("new-task", "success", "new")
	ml.store.Save(newEntry)

	if err := ml.Prune(context.Background(), 1, 1000); err != nil {
		t.Fatal(err)
	}

	count, _ := ml.store.Count()
	if count != 1 {
		t.Errorf("count after prune = %d, want 1", count)
	}
}

func TestMemoryLifecycle_Prune_Defaults(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ml := NewMemoryLifecycle(NewEpisodicStore(db))

	entry := NewEpisodicEntry("t-1", "success", "recent")
	ml.store.Save(entry)

	if err := ml.Prune(context.Background(), 0, 0); err != nil {
		t.Fatal(err)
	}

	count, _ := ml.store.Count()
	if count != 1 {
		t.Errorf("recent entry should survive, count = %d", count)
	}
}

func TestMemoryLifecycle_Search(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ml := NewMemoryLifecycle(NewEpisodicStore(db))

	ml.ExtractEpisodic("task-001", "failure",
		"Round 1: exec -> nil pointer error (crashed)",
		"bug",
	)

	results, err := ml.Search("nil pointer", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search results = %d", len(results))
	}
}

func TestMemoryLifecycle_Retrieve_Empty(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ml := NewMemoryLifecycle(NewEpisodicStore(db))

	episodes, err := ml.Retrieve("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 0 {
		t.Errorf("retrieved = %d, want 0", len(episodes))
	}
}
