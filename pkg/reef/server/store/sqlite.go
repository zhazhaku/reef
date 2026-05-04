package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zhazhaku/reef/pkg/reef"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
	_ "modernc.org/sqlite"
)

// SQLiteStore is a persistent implementation of TaskStore backed by SQLite.
type SQLiteStore struct {
	mu sync.RWMutex
	db *sql.DB
}

// NewSQLiteStore opens (or creates) a SQLite database at the given path,
// enables WAL mode, and runs migrations.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	// Ensure parent directory exists.
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable WAL: %w", err)
	}

	// Enable foreign keys.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	s := &SQLiteStore{db: db}
	if err := s.createTables(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create tables: %w", err)
	}

	// Run versioned schema migrations (adds evolution tables, etc.).
	if err := s.EnsureMigrated(); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migration: %w", err)
	}

	return s, nil
}

// createTables bootstraps the core task/client/DAG schema (version 1).
// It also seeds schema_version = 1 on first run.
func (s *SQLiteStore) createTables() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS tasks (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			instruction TEXT NOT NULL,
			required_role TEXT NOT NULL,
			required_skills TEXT,
			max_retries INTEGER DEFAULT 3,
			timeout_ms INTEGER DEFAULT 300000,
			model_hint TEXT,
			assigned_client TEXT,
			result TEXT,
			error TEXT,
			escalation_count INTEGER DEFAULT 0,
			pause_reason TEXT,
			created_at INTEGER NOT NULL,
			assigned_at INTEGER,
			started_at INTEGER,
			completed_at INTEGER
		);`,
		`CREATE TABLE IF NOT EXISTS task_attempts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL REFERENCES tasks(id),
			attempt_number INTEGER NOT NULL,
			started_at INTEGER NOT NULL,
			ended_at INTEGER NOT NULL,
			status TEXT NOT NULL,
			error_message TEXT,
			client_id TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_role ON tasks(required_role);`,
		`CREATE INDEX IF NOT EXISTS idx_task_attempts_task_id ON task_attempts(task_id);`,
		`CREATE TABLE IF NOT EXISTS task_relations (
			parent_id TEXT NOT NULL,
			child_id TEXT NOT NULL,
			dependency TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (parent_id, child_id)
		);`,
	}

	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}

	// Seed schema_version = 1 if the tracking table doesn't exist yet.
	// This ensures Migrate() starts from version 2 for evolution tables.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&count); err != nil {
		return fmt.Errorf("count schema_version: %w", err)
	}
	if count == 0 {
		if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (1)`); err != nil {
			return fmt.Errorf("seed schema_version: %w", err)
		}
	}

	return nil
}

// Migrate applies all pending schema migrations within a transaction.
// It is idempotent: if the database is already at CurrentSchemaVersion,
// this method is a no-op.
func (s *SQLiteStore) Migrate() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create schema_version tracking table if it doesn't exist.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL);`); err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	// Query current version.
	var currentVersion int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&currentVersion); err != nil {
		return fmt.Errorf("query schema_version: %w", err)
	}

	// Nothing to do.
	if currentVersion >= CurrentSchemaVersion {
		return nil
	}

	// Apply migrations in order from currentVersion+1 to CurrentSchemaVersion.
	for v := currentVersion + 1; v <= CurrentSchemaVersion; v++ {
		sql, ok := SchemaMigrations[v]
		if !ok {
			return fmt.Errorf("missing migration for version %d", v)
		}

		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin tx for version %d: %w", v, err)
		}

		if _, err := tx.Exec(sql); err != nil {
			tx.Rollback() // ignore rollback error, report original
			return fmt.Errorf("apply migration version %d: %w", v, err)
		}

		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, v); err != nil {
			tx.Rollback()
			return fmt.Errorf("record schema_version %d: %w", v, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration version %d: %w", v, err)
		}
	}

	return nil
}

// EnsureMigrated calls Migrate to apply pending schema migrations.
// Callers can use this to ensure the database is at the latest version.
func (s *SQLiteStore) EnsureMigrated() error {
	return s.Migrate()
}

// SaveTask inserts a new task. Returns an error if the task already exists.
func (s *SQLiteStore) SaveTask(task *reef.Task) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	skillsJSON, err := json.Marshal(task.RequiredSkills)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}

	_, err = s.db.Exec(`INSERT INTO tasks (
		id, status, instruction, required_role, required_skills,
		max_retries, timeout_ms, model_hint, assigned_client,
		result, error, escalation_count, pause_reason,
		created_at, assigned_at, started_at, completed_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		string(task.Status),
		task.Instruction,
		task.RequiredRole,
		string(skillsJSON),
		task.MaxRetries,
		task.TimeoutMs,
		task.ModelHint,
		task.AssignedClient,
		marshalJSON(task.Result),
		marshalJSON(task.Error),
		task.EscalationCount,
		task.PauseReason,
		timeToUnix(task.CreatedAt),
		timePtrToUnix(task.AssignedAt),
		timePtrToUnix(task.StartedAt),
		timePtrToUnix(task.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

// GetTask retrieves a task by ID.
func (s *SQLiteStore) GetTask(id string) (*reef.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT
		id, status, instruction, required_role, required_skills,
		max_retries, timeout_ms, model_hint, assigned_client,
		result, error, escalation_count, pause_reason,
		created_at, assigned_at, started_at, completed_at
	FROM tasks WHERE id = ?`, id)

	task, err := scanTask(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("task %s not found", id)
		}
		return nil, fmt.Errorf("get task: %w", err)
	}

	// Load attempt history.
	attempts, err := s.getAttemptsLocked(id)
	if err != nil {
		return nil, err
	}
	task.AttemptHistory = attempts

	return task, nil
}

// UpdateTask updates an existing task. Returns an error if the task doesn't exist.
func (s *SQLiteStore) UpdateTask(task *reef.Task) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	skillsJSON, err := json.Marshal(task.RequiredSkills)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}

	result, err := s.db.Exec(`UPDATE tasks SET
		status = ?, instruction = ?, required_role = ?, required_skills = ?,
		max_retries = ?, timeout_ms = ?, model_hint = ?, assigned_client = ?,
		result = ?, error = ?, escalation_count = ?, pause_reason = ?,
		created_at = ?, assigned_at = ?, started_at = ?, completed_at = ?
	WHERE id = ?`,
		string(task.Status),
		task.Instruction,
		task.RequiredRole,
		string(skillsJSON),
		task.MaxRetries,
		task.TimeoutMs,
		task.ModelHint,
		task.AssignedClient,
		marshalJSON(task.Result),
		marshalJSON(task.Error),
		task.EscalationCount,
		task.PauseReason,
		timeToUnix(task.CreatedAt),
		timePtrToUnix(task.AssignedAt),
		timePtrToUnix(task.StartedAt),
		timePtrToUnix(task.CompletedAt),
		task.ID,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s not found", task.ID)
	}
	return nil
}

// DeleteTask removes a task and its attempts.
func (s *SQLiteStore) DeleteTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete attempts first (foreign key should cascade, but be explicit).
	_, err := s.db.Exec("DELETE FROM task_attempts WHERE task_id = ?", id)
	if err != nil {
		return fmt.Errorf("delete attempts: %w", err)
	}

	result, err := s.db.Exec("DELETE FROM tasks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s not found", id)
	}
	return nil
}

// ListTasks returns tasks matching the given filter criteria.
func (s *SQLiteStore) ListTasks(filter TaskFilter) ([]*reef.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query := "SELECT id, status, instruction, required_role, required_skills, max_retries, timeout_ms, model_hint, assigned_client, result, error, escalation_count, pause_reason, created_at, assigned_at, started_at, completed_at FROM tasks WHERE 1=1"
	var args []interface{}

	if len(filter.Statuses) > 0 {
		placeholders := ""
		for i, st := range filter.Statuses {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, string(st))
		}
		query += " AND status IN (" + placeholders + ")"
	}

	if len(filter.Roles) > 0 {
		placeholders := ""
		for i, r := range filter.Roles {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, r)
		}
		query += " AND required_role IN (" + placeholders + ")"
	}

	query += " ORDER BY created_at, id"

	if filter.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", filter.Limit)
	} else if filter.Offset > 0 {
		query += " LIMIT -1"
	}
	if filter.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*reef.Task
	for rows.Next() {
		task, err := scanTaskRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, task)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tasks: %w", err)
	}

	if tasks == nil {
		tasks = []*reef.Task{}
	}

	return tasks, nil
}

// SaveAttempt appends an attempt record for a task.
func (s *SQLiteStore) SaveAttempt(taskID string, attempt reef.AttemptRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify task exists.
	var exists int
	err := s.db.QueryRow("SELECT 1 FROM tasks WHERE id = ?", taskID).Scan(&exists)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task %s not found", taskID)
	}
	if err != nil {
		return fmt.Errorf("check task: %w", err)
	}

	_, err = s.db.Exec(`INSERT INTO task_attempts
		(task_id, attempt_number, started_at, ended_at, status, error_message, client_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		taskID,
		attempt.AttemptNumber,
		attempt.StartedAt.Unix(),
		attempt.EndedAt.Unix(),
		attempt.Status,
		attempt.ErrorMessage,
		attempt.ClientID,
	)
	if err != nil {
		return fmt.Errorf("insert attempt: %w", err)
	}
	return nil
}

// GetAttempts returns all attempt records for a task.
func (s *SQLiteStore) GetAttempts(taskID string) ([]reef.AttemptRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getAttemptsLocked(taskID)
}

// getAttemptsLocked is the internal helper (caller must hold at least RLock).
func (s *SQLiteStore) getAttemptsLocked(taskID string) ([]reef.AttemptRecord, error) {
	// Verify task exists.
	var exists int
	err := s.db.QueryRow("SELECT 1 FROM tasks WHERE id = ?", taskID).Scan(&exists)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	if err != nil {
		return nil, fmt.Errorf("check task: %w", err)
	}

	rows, err := s.db.Query(`SELECT attempt_number, started_at, ended_at, status, error_message, client_id
		FROM task_attempts WHERE task_id = ? ORDER BY attempt_number`, taskID)
	if err != nil {
		return nil, fmt.Errorf("query attempts: %w", err)
	}
	defer rows.Close()

	var attempts []reef.AttemptRecord
	for rows.Next() {
		var a reef.AttemptRecord
		var startedAt, endedAt int64
		if err := rows.Scan(&a.AttemptNumber, &startedAt, &endedAt, &a.Status, &a.ErrorMessage, &a.ClientID); err != nil {
			return nil, fmt.Errorf("scan attempt: %w", err)
		}
		a.StartedAt = time.Unix(startedAt, 0)
		a.EndedAt = time.Unix(endedAt, 0)
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attempts: %w", err)
	}

	if attempts == nil {
		attempts = []reef.AttemptRecord{}
	}

	return attempts, nil
}

// SaveRelation records a parent-child relationship for DAG tasks.
func (s *SQLiteStore) SaveRelation(parentID, childID, dependency string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO task_relations (parent_id, child_id, dependency) VALUES (?, ?, ?)`,
		parentID, childID, dependency,
	)
	return err
}

// GetSubTaskIDs returns all child task IDs for a parent.
func (s *SQLiteStore) GetSubTaskIDs(parentID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.db.Query(`SELECT child_id FROM task_relations WHERE parent_id = ?`, parentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetParentTaskID returns the parent task ID for a child.
func (s *SQLiteStore) GetParentTaskID(childID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row := s.db.QueryRow(`SELECT parent_id FROM task_relations WHERE child_id = ? LIMIT 1`, childID)
	var parentID string
	err := row.Scan(&parentID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return parentID, err
}

// ---------------------------------------------------------------------------
// Evolution Events
// ---------------------------------------------------------------------------

// InsertEvolutionEvent inserts a new evolution event into the database.
// It sets processed=0 and uses RFC3339 for the created_at TEXT column.
func (s *SQLiteStore) InsertEvolutionEvent(event *evolution.EvolutionEvent) error {
	if event == nil {
		return fmt.Errorf("event cannot be nil")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`INSERT INTO evolution_events (
		id, task_id, client_id, event_type, signal, root_cause,
		gene_id, strategy, importance, created_at, processed
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)`,
		event.ID,
		event.TaskID,
		event.ClientID,
		string(event.EventType),
		event.Signal,
		event.RootCause,
		event.GeneID,
		event.Strategy,
		event.Importance,
		event.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("insert evolution event: %w", err)
	}
	return nil
}

// GetRecentEvents returns unprocessed events for a client, ordered by created_at DESC.
// limit=0 returns an empty slice (not an error). limit > 1000 is clamped to 1000.
func (s *SQLiteStore) GetRecentEvents(clientID string, limit int) ([]*evolution.EvolutionEvent, error) {
	if limit <= 0 {
		return []*evolution.EvolutionEvent{}, nil
	}
	if limit > 1000 {
		limit = 1000
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT
		id, task_id, client_id, event_type, signal, root_cause,
		gene_id, strategy, importance, created_at
	FROM evolution_events
	WHERE client_id = ? AND processed = 0
	ORDER BY created_at DESC LIMIT ?`, clientID, limit)
	if err != nil {
		return nil, fmt.Errorf("get recent events: %w", err)
	}
	defer rows.Close()

	var events []*evolution.EvolutionEvent
	for rows.Next() {
		e, err := scanEvolutionEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scan evolution event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate events: %w", err)
	}
	if events == nil {
		events = []*evolution.EvolutionEvent{}
	}
	return events, nil
}

// MarkEventsProcessed marks events as processed and links them to a gene.
// Uses parameterized queries for all values, including the IN clause.
func (s *SQLiteStore) MarkEventsProcessed(eventIDs []string, geneID string) error {
	if len(eventIDs) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Build parameterized IN clause.
	query := "UPDATE evolution_events SET processed = 1, gene_id = ? WHERE id IN ("
	args := []interface{}{geneID}
	for i, id := range eventIDs {
		if i > 0 {
			query += ","
		}
		query += "?"
		args = append(args, id)
	}
	query += ")"

	_, err := s.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("mark events processed: %w", err)
	}
	return nil
}

// CountEventsByType counts unprocessed events for a client by event type.
func (s *SQLiteStore) CountEventsByType(clientID string, eventType string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM evolution_events WHERE client_id = ? AND event_type = ? AND processed = 0`,
		clientID, eventType,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count events by type: %w", err)
	}
	return count, nil
}

// DeleteEventsBefore deletes all events older than the given cutoff time.
// Returns the number of deleted rows.
func (s *SQLiteStore) DeleteEventsBefore(cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(
		`DELETE FROM evolution_events WHERE created_at < ?`,
		cutoff.Format(time.RFC3339),
	)
	if err != nil {
		return 0, fmt.Errorf("delete events before: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// KeepTopNEventsPerTask keeps only the newest N events per task.
// Uses a subquery to identify which rows to keep and deletes the rest.
func (s *SQLiteStore) KeepTopNEventsPerTask(n int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec(`DELETE FROM evolution_events WHERE id NOT IN (
		SELECT id FROM evolution_events e2
		WHERE e2.task_id = evolution_events.task_id
		ORDER BY e2.created_at DESC LIMIT ?
	)`, n)
	if err != nil {
		return fmt.Errorf("keep top n events: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Genes
// ---------------------------------------------------------------------------

// InsertGene inserts a new gene into the database.
// Slices (Skills, FailureWarnings, SourceEvents) are serialized as JSON strings.
func (s *SQLiteStore) InsertGene(gene *evolution.Gene) error {
	if gene == nil {
		return fmt.Errorf("gene cannot be nil")
	}

	skillsJSON, err := json.Marshal(gene.Skills)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}
	failureWarningsJSON, err := json.Marshal(gene.FailureWarnings)
	if err != nil {
		return fmt.Errorf("marshal failure_warnings: %w", err)
	}
	sourceEventsJSON, err := json.Marshal(gene.SourceEvents)
	if err != nil {
		return fmt.Errorf("marshal source_events: %w", err)
	}

	var approvedAt interface{}
	if gene.ApprovedAt != nil {
		approvedAt = gene.ApprovedAt.Format(time.RFC3339)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.Exec(`INSERT INTO genes (
		id, strategy_name, role, skills, match_condition, control_signal,
		failure_warnings, source_events, source_client_id, version,
		status, stagnation_count, use_count, success_rate,
		created_at, updated_at, approved_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gene.ID,
		gene.StrategyName,
		gene.Role,
		string(skillsJSON),
		gene.MatchCondition,
		gene.ControlSignal,
		string(failureWarningsJSON),
		string(sourceEventsJSON),
		gene.SourceClientID,
		gene.Version,
		string(gene.Status),
		gene.StagnationCount,
		gene.UseCount,
		gene.SuccessRate,
		gene.CreatedAt.Format(time.RFC3339),
		gene.UpdatedAt.Format(time.RFC3339),
		approvedAt,
	)
	if err != nil {
		return fmt.Errorf("insert gene: %w", err)
	}
	return nil
}

// GetGene retrieves a gene by ID.
// Returns nil, nil when no gene is found with the given ID.
func (s *SQLiteStore) GetGene(geneID string) (*evolution.Gene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT
		id, strategy_name, role, skills, match_condition, control_signal,
		failure_warnings, source_events, source_client_id, version,
		status, stagnation_count, use_count, success_rate,
		created_at, updated_at, approved_at
	FROM genes WHERE id = ?`, geneID)

	gene, err := scanGene(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get gene: %w", err)
	}
	return gene, nil
}

// UpdateGene updates all fields of an existing gene by ID.
func (s *SQLiteStore) UpdateGene(gene *evolution.Gene) error {
	if gene == nil {
		return fmt.Errorf("gene cannot be nil")
	}

	skillsJSON, err := json.Marshal(gene.Skills)
	if err != nil {
		return fmt.Errorf("marshal skills: %w", err)
	}
	failureWarningsJSON, err := json.Marshal(gene.FailureWarnings)
	if err != nil {
		return fmt.Errorf("marshal failure_warnings: %w", err)
	}
	sourceEventsJSON, err := json.Marshal(gene.SourceEvents)
	if err != nil {
		return fmt.Errorf("marshal source_events: %w", err)
	}

	var approvedAt interface{}
	if gene.ApprovedAt != nil {
		approvedAt = gene.ApprovedAt.Format(time.RFC3339)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`UPDATE genes SET
		strategy_name = ?, role = ?, skills = ?, match_condition = ?,
		control_signal = ?, failure_warnings = ?, source_events = ?,
		source_client_id = ?, version = ?, status = ?,
		stagnation_count = ?, use_count = ?, success_rate = ?,
		created_at = ?, updated_at = ?, approved_at = ?
	WHERE id = ?`,
		gene.StrategyName,
		gene.Role,
		string(skillsJSON),
		gene.MatchCondition,
		gene.ControlSignal,
		string(failureWarningsJSON),
		string(sourceEventsJSON),
		gene.SourceClientID,
		gene.Version,
		string(gene.Status),
		gene.StagnationCount,
		gene.UseCount,
		gene.SuccessRate,
		gene.CreatedAt.Format(time.RFC3339),
		gene.UpdatedAt.Format(time.RFC3339),
		approvedAt,
		gene.ID,
	)
	if err != nil {
		return fmt.Errorf("update gene: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("gene %s not found", gene.ID)
	}
	return nil
}

// GetApprovedGenes returns approved genes for a role, ordered by success_rate DESC, use_count DESC.
func (s *SQLiteStore) GetApprovedGenes(role string, limit int) ([]*evolution.Gene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT
		id, strategy_name, role, skills, match_condition, control_signal,
		failure_warnings, source_events, source_client_id, version,
		status, stagnation_count, use_count, success_rate,
		created_at, updated_at, approved_at
	FROM genes
	WHERE role = ? AND status = 'approved'
	ORDER BY success_rate DESC, use_count DESC LIMIT ?`, role, limit)
	if err != nil {
		return nil, fmt.Errorf("get approved genes: %w", err)
	}
	defer rows.Close()

	return scanGenes(rows)
}

// CountApprovedGenes counts approved genes for a role.
func (s *SQLiteStore) CountApprovedGenes(role string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM genes WHERE role = ? AND status = 'approved'`,
		role,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count approved genes: %w", err)
	}
	return count, nil
}

// CountByStatus counts genes with a specific status.
func (s *SQLiteStore) CountByStatus(status string) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var count int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM genes WHERE status = ?`,
		status,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count genes by status %s: %w", status, err)
	}
	return count, nil
}

// GetTopGenes returns top genes for a role ordered by success_rate DESC.
func (s *SQLiteStore) GetTopGenes(role string, limit int) ([]*evolution.Gene, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`SELECT
		id, strategy_name, role, skills, match_condition, control_signal,
		failure_warnings, source_events, source_client_id, version,
		status, stagnation_count, use_count, success_rate,
		created_at, updated_at, approved_at
	FROM genes
	WHERE role = ?
	ORDER BY success_rate DESC LIMIT ?`, role, limit)
	if err != nil {
		return nil, fmt.Errorf("get top genes: %w", err)
	}
	defer rows.Close()

	return scanGenes(rows)
}

// DeleteStagnantGenes deletes all genes with status='stagnant'.
// Returns the number of deleted rows.
func (s *SQLiteStore) DeleteStagnantGenes() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM genes WHERE status = 'stagnant'`)
	if err != nil {
		return 0, fmt.Errorf("delete stagnant genes: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// KeepTopGenesPerRole keeps only the top N genes per role by success_rate.
// Returns the number of deleted rows.
func (s *SQLiteStore) KeepTopGenesPerRole(limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`DELETE FROM genes WHERE id NOT IN (
		SELECT id FROM genes g2
		WHERE g2.role = genes.role
		ORDER BY g2.success_rate DESC LIMIT ?
	)`, limit)
	if err != nil {
		return 0, fmt.Errorf("keep top genes per role: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// ---------------------------------------------------------------------------
// Skill Drafts
// ---------------------------------------------------------------------------

// SaveSkillDraft inserts a new skill draft into the database.
func (s *SQLiteStore) SaveSkillDraft(draft *evolution.SkillDraft) error {
	if draft == nil {
		return fmt.Errorf("draft cannot be nil")
	}

	sourceGeneIDsJSON, err := json.Marshal(draft.SourceGeneIDs)
	if err != nil {
		return fmt.Errorf("marshal source_gene_ids: %w", err)
	}

	var reviewedAt interface{}
	if draft.ReviewedAt != nil {
		reviewedAt = draft.ReviewedAt.Format(time.RFC3339)
	}
	var publishedAt interface{}
	if draft.PublishedAt != nil {
		publishedAt = draft.PublishedAt.Format(time.RFC3339)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.Exec(`INSERT INTO skill_drafts (
		id, role, skill_name, content, source_gene_ids,
		status, review_comment, created_at, reviewed_at, published_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		draft.ID,
		draft.Role,
		draft.SkillName,
		draft.Content,
		string(sourceGeneIDsJSON),
		string(draft.Status),
		draft.ReviewComment,
		draft.CreatedAt.Format(time.RFC3339),
		reviewedAt,
		publishedAt,
	)
	if err != nil {
		return fmt.Errorf("insert skill draft: %w", err)
	}
	return nil
}

// GetSkillDraft retrieves a skill draft by ID.
// Returns nil, nil when no draft is found.
func (s *SQLiteStore) GetSkillDraft(draftID string) (*evolution.SkillDraft, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	row := s.db.QueryRow(`SELECT
		id, role, skill_name, content, source_gene_ids,
		status, review_comment, created_at, reviewed_at, published_at
	FROM skill_drafts WHERE id = ?`, draftID)

	draft, err := scanSkillDraft(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get skill draft: %w", err)
	}
	return draft, nil
}

// UpdateSkillDraft updates all fields of an existing skill draft by ID.
func (s *SQLiteStore) UpdateSkillDraft(draft *evolution.SkillDraft) error {
	if draft == nil {
		return fmt.Errorf("draft cannot be nil")
	}

	sourceGeneIDsJSON, err := json.Marshal(draft.SourceGeneIDs)
	if err != nil {
		return fmt.Errorf("marshal source_gene_ids: %w", err)
	}

	var reviewedAt interface{}
	if draft.ReviewedAt != nil {
		reviewedAt = draft.ReviewedAt.Format(time.RFC3339)
	}
	var publishedAt interface{}
	if draft.PublishedAt != nil {
		publishedAt = draft.PublishedAt.Format(time.RFC3339)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.Exec(`UPDATE skill_drafts SET
		role = ?, skill_name = ?, content = ?, source_gene_ids = ?,
		status = ?, review_comment = ?, created_at = ?,
		reviewed_at = ?, published_at = ?
	WHERE id = ?`,
		draft.Role,
		draft.SkillName,
		draft.Content,
		string(sourceGeneIDsJSON),
		string(draft.Status),
		draft.ReviewComment,
		draft.CreatedAt.Format(time.RFC3339),
		reviewedAt,
		publishedAt,
		draft.ID,
	)
	if err != nil {
		return fmt.Errorf("update skill draft: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("skill draft %s not found", draft.ID)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanner is an interface satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...interface{}) error
}

func scanTask(row scanner) (*reef.Task, error) {
	var t reef.Task
	var status string
	var skillsJSON, resultJSON, errorJSON sql.NullString
	var createdAt int64
	var assignedAt, startedAt, completedAt sql.NullInt64

	err := row.Scan(
		&t.ID,
		&status,
		&t.Instruction,
		&t.RequiredRole,
		&skillsJSON,
		&t.MaxRetries,
		&t.TimeoutMs,
		&t.ModelHint,
		&t.AssignedClient,
		&resultJSON,
		&errorJSON,
		&t.EscalationCount,
		&t.PauseReason,
		&createdAt,
		&assignedAt,
		&startedAt,
		&completedAt,
	)
	if err != nil {
		return nil, err
	}

	t.Status = reef.TaskStatus(status)
	t.CreatedAt = time.Unix(createdAt, 0)

	if assignedAt.Valid {
		tm := time.Unix(assignedAt.Int64, 0)
		t.AssignedAt = &tm
	}
	if startedAt.Valid {
		tm := time.Unix(startedAt.Int64, 0)
		t.StartedAt = &tm
	}
	if completedAt.Valid {
		tm := time.Unix(completedAt.Int64, 0)
		t.CompletedAt = &tm
	}

	if skillsJSON.Valid && skillsJSON.String != "" {
		if err := json.Unmarshal([]byte(skillsJSON.String), &t.RequiredSkills); err != nil {
			return nil, fmt.Errorf("unmarshal skills: %w", err)
		}
	}
	if resultJSON.Valid && resultJSON.String != "" {
		t.Result = &reef.TaskResult{}
		if err := json.Unmarshal([]byte(resultJSON.String), t.Result); err != nil {
			return nil, fmt.Errorf("unmarshal result: %w", err)
		}
	}
	if errorJSON.Valid && errorJSON.String != "" {
		t.Error = &reef.TaskError{}
		if err := json.Unmarshal([]byte(errorJSON.String), t.Error); err != nil {
			return nil, fmt.Errorf("unmarshal error: %w", err)
		}
	}

	return &t, nil
}

// scanTaskRows is a convenience wrapper for scanTask with *sql.Rows.
func scanTaskRows(rows *sql.Rows) (*reef.Task, error) {
	return scanTask(rows)
}

func marshalJSON(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return string(data)
}

func timeToUnix(t time.Time) int64 {
	return t.Unix()
}

func timePtrToUnix(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Unix()
}

// ---------------------------------------------------------------------------
// Evolution scan helpers
// ---------------------------------------------------------------------------

// scanEvolutionEvent scans a row into an EvolutionEvent.
func scanEvolutionEvent(sc scanner) (*evolution.EvolutionEvent, error) {
	var e evolution.EvolutionEvent
	var eventType, strategy, createdAt string

	err := sc.Scan(
		&e.ID, &e.TaskID, &e.ClientID, &eventType, &e.Signal, &e.RootCause,
		&e.GeneID, &strategy, &e.Importance, &createdAt,
	)
	if err != nil {
		return nil, err
	}

	e.EventType = evolution.EventType(eventType)
	e.Strategy = strategy
	e.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	return &e, nil
}

// scanGene scans a row into a Gene.
func scanGene(sc scanner) (*evolution.Gene, error) {
	var g evolution.Gene
	var status, skillsJSON, failureWarningsJSON, sourceEventsJSON string
	var createdAt, updatedAt string
	var approvedAt sql.NullString

	err := sc.Scan(
		&g.ID, &g.StrategyName, &g.Role, &skillsJSON, &g.MatchCondition,
		&g.ControlSignal, &failureWarningsJSON, &sourceEventsJSON,
		&g.SourceClientID, &g.Version, &status, &g.StagnationCount,
		&g.UseCount, &g.SuccessRate, &createdAt, &updatedAt, &approvedAt,
	)
	if err != nil {
		return nil, err
	}

	g.Status = evolution.GeneStatus(status)

	if err := json.Unmarshal([]byte(skillsJSON), &g.Skills); err != nil {
		return nil, fmt.Errorf("unmarshal skills: %w", err)
	}
	if err := json.Unmarshal([]byte(failureWarningsJSON), &g.FailureWarnings); err != nil {
		return nil, fmt.Errorf("unmarshal failure_warnings: %w", err)
	}
	if err := json.Unmarshal([]byte(sourceEventsJSON), &g.SourceEvents); err != nil {
		return nil, fmt.Errorf("unmarshal source_events: %w", err)
	}

	g.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	g.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	if approvedAt.Valid && approvedAt.String != "" {
		t, err := time.Parse(time.RFC3339, approvedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse approved_at: %w", err)
		}
		g.ApprovedAt = &t
	}

	return &g, nil
}

// scanGenes scans multiple gene rows.
func scanGenes(rows *sql.Rows) ([]*evolution.Gene, error) {
	var genes []*evolution.Gene
	for rows.Next() {
		g, err := scanGene(rows)
		if err != nil {
			return nil, fmt.Errorf("scan gene: %w", err)
		}
		genes = append(genes, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate genes: %w", err)
	}
	if genes == nil {
		genes = []*evolution.Gene{}
	}
	return genes, nil
}

// scanSkillDraft scans a row into a SkillDraft.
func scanSkillDraft(sc scanner) (*evolution.SkillDraft, error) {
	var d evolution.SkillDraft
	var status, sourceGeneIDsJSON, createdAt string
	var reviewedAt, publishedAt sql.NullString

	err := sc.Scan(
		&d.ID, &d.Role, &d.SkillName, &d.Content, &sourceGeneIDsJSON,
		&status, &d.ReviewComment, &createdAt, &reviewedAt, &publishedAt,
	)
	if err != nil {
		return nil, err
	}

	d.Status = evolution.SkillDraftStatus(status)

	if err := json.Unmarshal([]byte(sourceGeneIDsJSON), &d.SourceGeneIDs); err != nil {
		return nil, fmt.Errorf("unmarshal source_gene_ids: %w", err)
	}

	d.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	if reviewedAt.Valid && reviewedAt.String != "" {
		t, err := time.Parse(time.RFC3339, reviewedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse reviewed_at: %w", err)
		}
		d.ReviewedAt = &t
	}
	if publishedAt.Valid && publishedAt.String != "" {
		t, err := time.Parse(time.RFC3339, publishedAt.String)
		if err != nil {
			return nil, fmt.Errorf("parse published_at: %w", err)
		}
		d.PublishedAt = &t
	}

	return &d, nil
}
