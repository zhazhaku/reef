package memory

import (
	"database/sql"
	"encoding/json"
	"time"
)

// EpisodicEntry is a single episodic memory record.
type EpisodicEntry struct {
	ID        int64    `json:"id"`
	TaskID    string   `json:"task_id"`
	Timestamp int64    `json:"timestamp"`
	EventType string   `json:"event_type"` // "success", "failure", "checkpoint", "block"
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// EpisodicStore provides SQLite-backed CRUD for episodic memory entries.
type EpisodicStore struct {
	db *sql.DB
}

// NewEpisodicStore creates a new store backed by the given database.
func NewEpisodicStore(db *sql.DB) *EpisodicStore {
	return &EpisodicStore{db: db}
}

// Save persists an episodic entry. ID is auto-generated.
func (s *EpisodicStore) Save(entry *EpisodicEntry) error {
	tagsJSON, _ := json.Marshal(entry.Tags)
	result, err := s.db.Exec(
		`INSERT INTO task_episodes (task_id, timestamp, event_type, summary, tags)
		 VALUES (?, ?, ?, ?, ?)`,
		entry.TaskID, entry.Timestamp, entry.EventType, entry.Summary, string(tagsJSON),
	)
	if err != nil {
		return err
	}
	id, _ := result.LastInsertId()
	entry.ID = id
	return nil
}

// GetByTask returns all episodes for a given task, ordered by timestamp.
func (s *EpisodicStore) GetByTask(taskID string) ([]EpisodicEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, timestamp, event_type, summary, tags, created_at
		 FROM task_episodes WHERE task_id = ? ORDER BY timestamp ASC`,
		taskID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

// Search finds episodes matching a query string (LIKE on summary),
// ordered by timestamp descending, limited to limit.
func (s *EpisodicStore) Search(query string, limit int) ([]EpisodicEntry, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, timestamp, event_type, summary, tags, created_at
		 FROM task_episodes WHERE summary LIKE ? ORDER BY timestamp DESC LIMIT ?`,
		"%"+query+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEpisodes(rows)
}

// DeleteBefore removes episodes older than the given Unix timestamp.
func (s *EpisodicStore) DeleteBefore(timestamp int64) error {
	_, err := s.db.Exec(`DELETE FROM task_episodes WHERE timestamp < ?`, timestamp)
	return err
}

// Count returns the total number of episodes.
func (s *EpisodicStore) Count() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM task_episodes`).Scan(&n)
	return n, err
}

// DeleteByTask removes all episodes for a given task.
func (s *EpisodicStore) DeleteByTask(taskID string) error {
	_, err := s.db.Exec(`DELETE FROM task_episodes WHERE task_id = ?`, taskID)
	return err
}

func scanEpisodes(rows *sql.Rows) ([]EpisodicEntry, error) {
	var out []EpisodicEntry
	for rows.Next() {
		var e EpisodicEntry
		var tagsJSON string
		var createdAt string
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Timestamp, &e.EventType, &e.Summary, &tagsJSON, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt = createdAt
		if tagsJSON != "" {
			json.Unmarshal([]byte(tagsJSON), &e.Tags)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// NewEpisodicEntry is a convenience constructor.
func NewEpisodicEntry(taskID, eventType, summary string, tags ...string) *EpisodicEntry {
	return &EpisodicEntry{
		TaskID:    taskID,
		Timestamp: time.Now().Unix(),
		EventType: eventType,
		Summary:   summary,
		Tags:      tags,
	}
}
