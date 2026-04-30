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
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// migrate creates the schema if it doesn't exist.
func (s *SQLiteStore) migrate() error {
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
	return nil
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
