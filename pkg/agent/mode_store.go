// Reef - Distributed multi-agent swarm orchestration system

package agent

import (
	"context"
	"database/sql"
	"fmt"
)

// ModeStore persists per-conversation mode state in SQLite.
// It shares the seahorse database at {workspace}/sessions/seahorse.db.
type ModeStore struct {
	db *sql.DB
}

// NewModeStore creates a ModeStore backed by an existing SQLite database.
func NewModeStore(db *sql.DB) *ModeStore {
	return &ModeStore{db: db}
}

// GetMode returns the current mode for a conversation, defaulting to ModeChat.
func (s *ModeStore) GetMode(ctx context.Context, convID string) (ConversationMode, error) {
	var modeStr string
	err := s.db.QueryRowContext(ctx,
		"SELECT mode FROM conversation_mode WHERE conversation_id = ?",
		convID,
	).Scan(&modeStr)

	if err == sql.ErrNoRows {
		return ModeChat, nil
	}
	if err != nil {
		return ModeChat, fmt.Errorf("get mode for conv %s: %w", convID, err)
	}

	switch modeStr {
	case "hermes":
		return ModeHermes, nil
	default:
		return ModeChat, nil
	}
}

// SetMode sets the mode for a conversation (upsert).
func (s *ModeStore) SetMode(ctx context.Context, convID string, mode ConversationMode) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conversation_mode (conversation_id, mode, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(conversation_id) DO UPDATE SET
		   mode = excluded.mode,
		   updated_at = excluded.updated_at`,
		convID, string(mode),
	)
	if err != nil {
		return fmt.Errorf("set mode for conv %s: %w", convID, err)
	}
	return nil
}

// ResetMode resets a conversation to the default chat mode and removes
// any Hermes workflow session state (called when a workflow completes
// or is aborted).
func (s *ModeStore) ResetMode(ctx context.Context, convID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		"DELETE FROM conversation_mode WHERE conversation_id = ?",
		convID,
	)
	if err != nil {
		return fmt.Errorf("delete mode: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"DELETE FROM hermes_workflow_sessions WHERE conversation_id = ?",
		convID,
	)
	if err != nil {
		return fmt.Errorf("delete workflow session: %w", err)
	}

	return tx.Commit()
}
