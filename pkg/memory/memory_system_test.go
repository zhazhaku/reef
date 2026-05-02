package memory

import (
	"testing"
)

func TestNewMemorySystem(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ms := NewMemorySystem(db)
	if ms.Episodic == nil {
		t.Error("Episodic is nil")
	}
	if ms.Semantic == nil {
		t.Error("Semantic is nil")
	}
}

func TestMemorySystem_ExtractAndRetrieve(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ms := NewMemorySystem(db)

	_, err := ms.Episodic.ExtractEpisodic("t-1", "success",
		"Round 1: exec -> ok\nRound 2: write -> done", "go")
	if err != nil {
		t.Fatal(err)
	}

	episodes, err := ms.Episodic.Retrieve("t-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Errorf("retrieved = %d, want 1", len(episodes))
	}
}

func TestMemorySystem_NamespaceIsolation(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	ms := NewMemorySystem(db)

	ms.Episodic.ExtractEpisodic("task-A", "success", "A summary")
	ms.Episodic.ExtractEpisodic("task-B", "success", "B summary")

	aEpisodes, _ := ms.Episodic.Retrieve("task-A")
	bEpisodes, _ := ms.Episodic.Retrieve("task-B")

	if len(aEpisodes) != 1 {
		t.Errorf("task-A episodes = %d", len(aEpisodes))
	}
	if len(bEpisodes) != 1 {
		t.Errorf("task-B episodes = %d", len(bEpisodes))
	}
	if aEpisodes[0].Summary != "A summary" {
		t.Error("wrong summary for task-A")
	}
}

func TestSemanticRetriever_CreateTable(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	sr := NewSemanticRetriever(db)
	if err := sr.CreateGenesTable(); err != nil {
		t.Fatal(err)
	}
}

func TestSemanticRetriever_Retrieve_Stub(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	sr := NewSemanticRetriever(db)
	genes, err := sr.Retrieve("coder", []string{"go"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if genes == nil {
		t.Error("genes should not be nil")
	}
}

func TestGene_Fields(t *testing.T) {
	g := Gene{
		Role:    "coder",
		Content: "use terse comments",
		Weight:  1.5,
	}
	if g.Role != "coder" {
		t.Errorf("Role = %s", g.Role)
	}
}
