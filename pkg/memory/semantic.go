package memory

import (
	"database/sql"
)

// Gene represents an evolution gene (semantic memory).
type Gene struct {
	ID       int64    `json:"id"`
	Role     string   `json:"role"`
	Content  string   `json:"content"`
	Weight   float64  `json:"weight"`
	Tags     []string `json:"tags"`
}

// SemanticRetriever retrieves semantic memories (genes) from the database.
type SemanticRetriever struct {
	db *sql.DB
}

// NewSemanticRetriever creates a retriever backed by the given database.
func NewSemanticRetriever(db *sql.DB) *SemanticRetriever {
	return &SemanticRetriever{db: db}
}

// Retrieve returns genes matching the given role and tags.
// Currently returns a stubbed empty list until the genes table is integrated.
func (sr *SemanticRetriever) Retrieve(role string, tags []string, limit int) ([]*Gene, error) {
	// FUTURE: query seahorse genes table
	_ = role
	_ = tags
	_ = limit
	return []*Gene{}, nil
}

// CreateGenesTable creates the genes table if it doesn't exist.
func (sr *SemanticRetriever) CreateGenesTable() error {
	_, err := sr.db.Exec(`
		CREATE TABLE IF NOT EXISTS genes (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			role    TEXT NOT NULL,
			content TEXT NOT NULL,
			weight  REAL NOT NULL DEFAULT 1.0,
			tags    TEXT
		)
	`)
	return err
}
