package memory

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestFromEvolutionGene(t *testing.T) {
	g := FromEvolutionGene("coder", "echo 'use proper error handling'", 0.8,
		[]string{"go", "testing"},
		[]string{"quality", "safety"})

	if g.Role != "coder" {
		t.Errorf("Role = %s", g.Role)
	}
	if g.Content != "echo 'use proper error handling'" {
		t.Errorf("Content = %s", g.Content)
	}
	if g.Weight != 0.8 {
		t.Errorf("Weight = %f", g.Weight)
	}
	if len(g.Tags) != 4 { // 2 tags + 2 skill tags
		t.Errorf("Tags = %v, len=%d", g.Tags, len(g.Tags))
	}
}

func TestFromEvolutionGene_FallbackContent(t *testing.T) {
	g := FromEvolutionGene("reviewer", "", 1.0,
		[]string{"review", "PR"},
		nil)

	if g.Content != "review; PR" {
		t.Errorf("Content = %s", g.Content)
	}
}

func TestFromEvolutionGene_DefaultWeight(t *testing.T) {
	g := FromEvolutionGene("helper", "be nice", 0, nil, nil)
	if g.Weight != 1.0 {
		t.Errorf("Weight = %f", g.Weight)
	}
}

func TestSemanticRetriever_SaveAndRetrieve(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sr := NewSemanticRetriever(db)
	if err := sr.CreateGenesTable(); err != nil {
		t.Fatal(err)
	}

	// Save a gene
	g := &Gene{
		Role:    "coder",
		Content: "use camelCase",
		Weight:  0.9,
		Tags:    []string{"style", "naming"},
	}
	if err := sr.SaveGene(g); err != nil {
		t.Fatal(err)
	}

	// Retrieve by role
	genes, err := sr.Retrieve("coder", nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(genes) != 1 {
		t.Fatalf("genes = %d", len(genes))
	}
	if genes[0].Role != "coder" {
		t.Errorf("role = %s", genes[0].Role)
	}
	if genes[0].Content != "use camelCase" {
		t.Errorf("content = %s", genes[0].Content)
	}
}

func TestSemanticRetriever_Retrieve_Empty(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sr := NewSemanticRetriever(db)
	sr.CreateGenesTable()

	genes, err := sr.Retrieve("nonexistent", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(genes) != 0 {
		t.Errorf("expected empty, got %d", len(genes))
	}
}

func TestSemanticRetriever_RetrieveByTag(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	sr := NewSemanticRetriever(db)
	sr.CreateGenesTable()

	sr.SaveGene(&Gene{Role: "a", Content: "a1", Weight: 1.0, Tags: []string{"critical"}})
	sr.SaveGene(&Gene{Role: "b", Content: "b1", Weight: 0.5, Tags: []string{"low"}})

	genes, err := sr.Retrieve("", []string{"critical"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(genes) != 1 {
		t.Fatalf("genes = %d", len(genes))
	}
	if genes[0].Role != "a" {
		t.Errorf("role = %s", genes[0].Role)
	}
}
