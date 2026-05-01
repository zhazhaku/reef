package store

import (
	"path/filepath"
	"testing"
)

// TestMigration tests the full migration lifecycle:
// fresh DB → version=0 → migrate → version=2
// second migrate on same DB → no-op
// verify all 3 evolution tables exist with correct columns
func TestMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.db")

	// Create fresh store.
	s, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	// Verify schema_version = 2.
	var ver int
	if err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&ver); err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if ver != CurrentSchemaVersion {
		t.Errorf("expected schema version %d, got %d", CurrentSchemaVersion, ver)
	}

	// Close and re-open — migration should be no-op.
	s.Close()

	s2, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer s2.Close()

	if err := s2.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&ver); err != nil {
		t.Fatalf("query schema_version after re-open: %v", err)
	}
	if ver != CurrentSchemaVersion {
		t.Errorf("expected schema version %d after re-open, got %d", CurrentSchemaVersion, ver)
	}

	// Verify all 3 evolution tables exist.
	expectedTables := []string{
		"evolution_events",
		"genes",
		"skill_drafts",
	}
	for _, tbl := range expectedTables {
		var name string
		err := s2.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name = ?", tbl).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", tbl, err)
		}
	}

	// Verify evolution_events columns.
	eeCols := mustGetTableColumns(t, s2, "evolution_events")
	expectedEECols := []string{"id", "task_id", "client_id", "event_type", "signal", "root_cause", "gene_id", "strategy", "importance", "created_at", "processed"}
	for _, col := range expectedEECols {
		if !contains(eeCols, col) {
			t.Errorf("evolution_events missing column: %s", col)
		}
	}

	// Verify genes columns.
	gCols := mustGetTableColumns(t, s2, "genes")
	expectedGCols := []string{"id", "strategy_name", "role", "skills", "match_condition", "control_signal", "failure_warnings", "source_events", "source_client_id", "version", "status", "stagnation_count", "use_count", "success_rate", "created_at", "updated_at", "approved_at"}
	for _, col := range expectedGCols {
		if !contains(gCols, col) {
			t.Errorf("genes missing column: %s", col)
		}
	}

	// Verify skill_drafts columns.
	sdCols := mustGetTableColumns(t, s2, "skill_drafts")
	expectedSDCols := []string{"id", "role", "skill_name", "content", "source_gene_ids", "status", "review_comment", "created_at", "reviewed_at", "published_at"}
	for _, col := range expectedSDCols {
		if !contains(sdCols, col) {
			t.Errorf("skill_drafts missing column: %s", col)
		}
	}

	// Verify indexes exist.
	expectedIndexes := []string{
		"idx_ee_task_id",
		"idx_ee_client_id",
		"idx_ee_event_type",
		"idx_ee_created_at",
		"idx_gene_role",
		"idx_gene_status",
		"idx_sd_role",
		"idx_sd_status",
	}
	for _, idx := range expectedIndexes {
		var name string
		err := s2.db.QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name = ?", idx).Scan(&name)
		if err != nil {
			t.Errorf("index %s not found: %v", idx, err)
		}
	}
}

// TestFullSchemaCreation is the integration test that verifies:
// - All 3 evolution tables exist
// - schema_version is 2
// - All CRUD operations work end-to-end
func TestFullSchemaCreation(t *testing.T) {
	s, _ := newTestSQLiteStore(t)
	defer s.Close()

	// Verify schema_version.
	var ver int
	if err := s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&ver); err != nil {
		t.Fatalf("query schema_version: %v", err)
	}
	if ver != CurrentSchemaVersion {
		t.Errorf("expected version %d, got %d", CurrentSchemaVersion, ver)
	}

	// Verify all tables exist.
	tables := []string{"evolution_events", "genes", "skill_drafts", "tasks", "task_attempts", "task_relations"}
	for _, tbl := range tables {
		var name string
		if err := s.db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name = ?", tbl).Scan(&name); err != nil {
			t.Errorf("table %s not found: %v", tbl, err)
		}
	}

	// Insert and verify an evolution event (in test below).
	// Insert and verify a gene (in test below).
	// Insert and verify a skill draft (in test below).
	// These are covered by TestEvolutionEventsCRUD, TestGenesCRUD, TestSkillDraftsCRUD.
}

func mustGetTableColumns(t *testing.T, s *SQLiteStore, table string) []string {
	t.Helper()
	rows, err := s.db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()

	var cols []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols = append(cols, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	return cols
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
