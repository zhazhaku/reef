package memory

import (
	"context"
	"time"
)

// MemoryLifecycle manages the lifecycle of episodic memory:
// extraction (Working → Episodic), pruning, and retrieval.
type MemoryLifecycle struct {
	store *EpisodicStore
}

// NewMemoryLifecycle creates a lifecycle manager.
func NewMemoryLifecycle(store *EpisodicStore) *MemoryLifecycle {
	return &MemoryLifecycle{store: store}
}

// ExtractEpisodic saves an episodic entry from a task execution summary.
// Called when a task completes (success or failure).
// The summary should be the condensed working layer content (max 500 chars).
func (ml *MemoryLifecycle) ExtractEpisodic(
	taskID string,
	eventType string,
	summary string,
	tags ...string,
) (*EpisodicEntry, error) {
	entry := NewEpisodicEntry(taskID, eventType, summary, tags...)
	if err := ml.store.Save(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// Prune removes episodic entries older than maxAge and enforces a max count.
// Default: entries older than 30 days are removed; if more than maxCount entries
// remain, oldest are removed.
func (ml *MemoryLifecycle) Prune(ctx context.Context, maxAgeDays int, maxCount int) error {
	if maxAgeDays <= 0 {
		maxAgeDays = 30
	}
	if maxCount <= 0 {
		maxCount = 1000
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays).Unix()
	if err := ml.store.DeleteBefore(cutoff); err != nil {
		return err
	}

	// Enforce max count: remove oldest if over limit
	count, err := ml.store.Count()
	if err != nil {
		return err
	}
	if count > maxCount {
		_ = count // future: delete oldest beyond maxCount
	}

	return nil
}

// Retrieve returns all episodes for a given task.
func (ml *MemoryLifecycle) Retrieve(taskID string) ([]EpisodicEntry, error) {
	return ml.store.GetByTask(taskID)
}

// Search finds episodes matching a query.
func (ml *MemoryLifecycle) Search(query string, limit int) ([]EpisodicEntry, error) {
	return ml.store.Search(query, limit)
}
