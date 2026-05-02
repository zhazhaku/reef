package memory

import (
	"context"
	"database/sql"
)

// MemorySystem provides a unified entry point for episodic and semantic memory.
type MemorySystem struct {
	Episodic  *MemoryLifecycle
	Semantic  *SemanticRetriever
	store     *EpisodicStore
}

// NewMemorySystem creates a memory system backed by the given database.
func NewMemorySystem(db *sql.DB) *MemorySystem {
	store := NewEpisodicStore(db)
	return &MemorySystem{
		Episodic: NewMemoryLifecycle(store),
		Semantic: NewSemanticRetriever(db),
		store:    store,
	}
}

// PruneAll prunes both episodic and semantic memories.
func (ms *MemorySystem) PruneAll(ctx context.Context) error {
	return ms.Episodic.Prune(ctx, 30, 1000)
}
