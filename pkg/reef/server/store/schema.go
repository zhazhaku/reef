package store

// CurrentSchemaVersion tracks the latest schema version.
// Version 1 = pre-existing task/client/DAG tables (created via migrate()).
// Version 2 = evolution_events, genes, skill_drafts tables.
const CurrentSchemaVersion = 2

// SchemaMigrations maps target versions to the SQL DDL that must be
// applied to move from the previous version to the target version.
// Migrations are applied in ascending version order within a transaction.
var SchemaMigrations = map[int]string{
	2: evolutionMigrationSQL,
}

// evolutionMigrationSQL adds the three evolution tables to the schema.
// All tables use IF NOT EXISTS to be idempotent.
// JSON columns (skills, failure_warnings, source_events, source_gene_ids)
// are stored as TEXT; the store layer handles marshal/unmarshal.
const evolutionMigrationSQL = `
CREATE TABLE IF NOT EXISTS evolution_events (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL,
    client_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    signal TEXT NOT NULL DEFAULT '',
    root_cause TEXT NOT NULL DEFAULT '',
    gene_id TEXT NOT NULL DEFAULT '',
    strategy TEXT NOT NULL DEFAULT '',
    importance REAL NOT NULL DEFAULT 0.0,
    created_at TEXT NOT NULL,
    processed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_ee_task_id ON evolution_events(task_id);
CREATE INDEX IF NOT EXISTS idx_ee_client_id ON evolution_events(client_id);
CREATE INDEX IF NOT EXISTS idx_ee_event_type ON evolution_events(event_type);
CREATE INDEX IF NOT EXISTS idx_ee_created_at ON evolution_events(created_at);

CREATE TABLE IF NOT EXISTS genes (
    id TEXT PRIMARY KEY,
    strategy_name TEXT NOT NULL,
    role TEXT NOT NULL,
    skills TEXT NOT NULL DEFAULT '[]',
    match_condition TEXT NOT NULL DEFAULT '',
    control_signal TEXT NOT NULL,
    failure_warnings TEXT NOT NULL DEFAULT '[]',
    source_events TEXT NOT NULL DEFAULT '[]',
    source_client_id TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    status TEXT NOT NULL DEFAULT 'draft',
    stagnation_count INTEGER NOT NULL DEFAULT 0,
    use_count INTEGER NOT NULL DEFAULT 0,
    success_rate REAL NOT NULL DEFAULT 0.0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    approved_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_gene_role ON genes(role);
CREATE INDEX IF NOT EXISTS idx_gene_status ON genes(status);

CREATE TABLE IF NOT EXISTS skill_drafts (
    id TEXT PRIMARY KEY,
    role TEXT NOT NULL,
    skill_name TEXT NOT NULL,
    content TEXT NOT NULL,
    source_gene_ids TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'pending_review',
    review_comment TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    reviewed_at TEXT,
    published_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_sd_role ON skill_drafts(role);
CREATE INDEX IF NOT EXISTS idx_sd_status ON skill_drafts(status);
`
