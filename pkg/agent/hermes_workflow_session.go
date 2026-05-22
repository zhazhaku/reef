// Reef - Distributed multi-agent swarm orchestration system

package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// WorkflowStore persists WorkflowSession to the SQLite database
// shared with seahorse (table: hermes_workflow_sessions).
type WorkflowStore struct {
	db *sql.DB
}

// NewWorkflowStore creates a WorkflowStore backed by an existing SQLite database.
func NewWorkflowStore(db *sql.DB) *WorkflowStore {
	return &WorkflowStore{db: db}
}

// GetSession retrieves the active workflow session for a conversation.
// Returns nil if no session exists.
func (s *WorkflowStore) GetSession(ctx context.Context, convID string) (*WorkflowSession, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT conversation_id, task_id, phase,
		        task_profile, current_round, max_rounds,
		        directions, converge_streak,
		        selected_clients, task_boards,
		        created_at, updated_at
		 FROM hermes_workflow_sessions
		 WHERE conversation_id = ?`, convID,
	)

	var ws WorkflowSession
	var taskProfileJSON, directionsJSON, clientsJSON, boardsJSON sql.NullString
	var phaseStr, createdAt, updatedAt string

	err := row.Scan(
		&ws.ConversationID, &ws.TaskID, &phaseStr,
		&taskProfileJSON, &ws.CurrentRound, &ws.MaxRounds,
		&directionsJSON, &ws.ConvergeStreak,
		&clientsJSON, &boardsJSON,
		&createdAt, &updatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	ws.Phase = WorkflowPhase(phaseStr)
	ws.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	ws.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	if taskProfileJSON.Valid && taskProfileJSON.String != "" {
		var tp TaskProfile
		if err := json.Unmarshal([]byte(taskProfileJSON.String), &tp); err == nil {
			ws.TaskProfile = &tp
		}
	}
	if directionsJSON.Valid && directionsJSON.String != "" {
		_ = json.Unmarshal([]byte(directionsJSON.String), &ws.Directions)
	}
	if clientsJSON.Valid && clientsJSON.String != "" {
		_ = json.Unmarshal([]byte(clientsJSON.String), &ws.SelectedClients)
	}
	if boardsJSON.Valid && boardsJSON.String != "" {
		_ = json.Unmarshal([]byte(boardsJSON.String), &ws.TaskBoards)
	}
	if ws.TaskBoards == nil {
		ws.TaskBoards = make(map[string]string)
	}

	return &ws, nil
}

// SaveSession persists (inserts or updates) a WorkflowSession.
func (s *WorkflowStore) SaveSession(ctx context.Context, ws *WorkflowSession) error {
	taskProfileJSON := jsonNullable(ws.TaskProfile)
	directionsJSON := jsonNullable(ws.Directions)
	clientsJSON := jsonNullable(ws.SelectedClients)
	boardsJSON := jsonNullable(ws.TaskBoards)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO hermes_workflow_sessions
		 (conversation_id, task_id, phase,
		  task_profile, current_round, max_rounds,
		  directions, converge_streak,
		  selected_clients, task_boards,
		  updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(conversation_id) DO UPDATE SET
		   task_id = excluded.task_id,
		   phase = excluded.phase,
		   task_profile = excluded.task_profile,
		   current_round = excluded.current_round,
		   max_rounds = excluded.max_rounds,
		   directions = excluded.directions,
		   converge_streak = excluded.converge_streak,
		   selected_clients = excluded.selected_clients,
		   task_boards = excluded.task_boards,
		   updated_at = excluded.updated_at`,
		ws.ConversationID, ws.TaskID, string(ws.Phase),
		taskProfileJSON, ws.CurrentRound, ws.MaxRounds,
		directionsJSON, ws.ConvergeStreak,
		clientsJSON, boardsJSON,
	)
	if err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// UpdatePhase atomically updates only the phase field.
func (s *WorkflowStore) UpdatePhase(ctx context.Context, convID string, phase WorkflowPhase) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE hermes_workflow_sessions SET phase = ?, updated_at = datetime('now')
		 WHERE conversation_id = ?`,
		string(phase), convID,
	)
	return err
}

// DeleteSession removes the workflow session for a conversation.
func (s *WorkflowStore) DeleteSession(ctx context.Context, convID string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM hermes_workflow_sessions WHERE conversation_id = ?", convID,
	)
	return err
}

// jsonNullable marshals any value to a JSON string for SQLite storage.
// Returns "null" for nil values.
func jsonNullable(v any) string {
	if v == nil {
		return "null"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}
