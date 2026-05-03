package memory

import (
	"database/sql"
	"strings"
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

// Retrieve returns genes matching the given role and tags, ordered by weight descending.
func (sr *SemanticRetriever) Retrieve(role string, tags []string, limit int) ([]*Gene, error) {
	if limit <= 0 {
		limit = 10
	}

	query := "SELECT id, role, content, weight, tags FROM genes WHERE 1=1"
	args := []any{}

	if role != "" {
		query += " AND role = ?"
		args = append(args, role)
	}

	if len(tags) > 0 {
		for _, tag := range tags {
			query += " AND tags LIKE ?"
			args = append(args, "%"+tag+"%")
		}
	}

	query += " ORDER BY weight DESC LIMIT ?"
	args = append(args, limit)

	rows, err := sr.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var genes []*Gene
	for rows.Next() {
		g := &Gene{}
		var tagsJSON string
		if err := rows.Scan(&g.ID, &g.Role, &g.Content, &g.Weight, &tagsJSON); err != nil {
			return nil, err
		}
		if tagsJSON != "" {
			// Simple comma-split for now
			tagsJSON = trimQuotes(tagsJSON)
			g.Tags = splitTags(tagsJSON)
		}
		genes = append(genes, g)
	}
	return genes, rows.Err()
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


// SaveGene persists a gene (insert or update on conflict).
func (sr *SemanticRetriever) SaveGene(g *Gene) error {
	tags := strings.Join(g.Tags, ",")
	_, err := sr.db.Exec(`
		INSERT INTO genes (role, content, weight, tags)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			role=excluded.role, content=excluded.content,
			weight=excluded.weight, tags=excluded.tags
	`, g.Role, g.Content, g.Weight, tags)
	return err
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "[]\"")
	return s
}

func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
