# P6-04 SQLite Schema Evolution — Summary

## Status: ✅ COMPLETE

All 5 tasks implemented, all 25 tests passing, zero regressions.

## What Was Done

### Task 1: schema.go with migration infrastructure
- Created `pkg/reef/server/store/schema.go` with `CurrentSchemaVersion = 2`
- Defined `SchemaMigrations` map with version 2 DDL for three new tables
- Copied evolution types (gene.go, event.go, skill_draft.go, strategy.go) to picoclaw module
- Fixed import paths from `github.com/sipeed/reef` to `github.com/zhazhaku/reef`
- DDL includes `strategy` column on evolution_events (aligned with EvolutionEvent struct)

### Task 2: Migrate() and EnsureMigrated()
- Renamed existing `migrate()` to `createTables()` for version 1 tables
- Added `Migrate()`: version-based migration with transaction-per-version, idempotent
- Added `EnsureMigrated()`: thin wrapper, called from `NewSQLiteStore`
- `createTables()` seeds `schema_version = 1` to ensure migration starts from version 2
- Transaction rollback on failure; retries on next startup

### Task 3: Evolution Events CRUD
- `InsertEvolutionEvent` — inserts with `processed=0`, RFC3339 dates, nil guard
- `GetRecentEvents` — unprocessed events per client, DESC order, limit clamping (0→empty, >1000→1000)
- `MarkEventsProcessed` — parameterized IN clause, no SQL injection
- `CountEventsByType` — count unprocessed per client/type
- `DeleteEventsBefore` — time-based cleanup, returns count
- `KeepTopNEventsPerTask` — subquery-based per-task retention

### Task 4: Genes and Skill Drafts CRUD
- Genes: `InsertGene`, `GetGene` (nil,nil on not found), `UpdateGene`, `GetApprovedGenes`, `CountApprovedGenes`, `GetTopGenes`, `DeleteStagnantGenes`, `KeepTopGenesPerRole`
- SkillDrafts: `SaveSkillDraft`, `GetSkillDraft` (nil,nil on not found), `UpdateSkillDraft`
- All JSON columns (skills, failure_warnings, source_events, source_gene_ids) use `json.Marshal/Unmarshal`
- Nullable timestamps (`approved_at`, `reviewed_at`, `published_at`) use `*time.Time` ↔ `sql.NullString`

### Task 5: Integration Test
- `TestMigration`: fresh→migrate→version=2, re-open→no-op, all 3 tables + indexes verified
- `TestFullSchemaCreation`: schema_version=2, all tables exist, end-to-end CRUD covered by other tests
- All 11 existing SQLite tests still pass

## Tables Created

| Table | Key Columns | Indexes |
|-------|------------|---------|
| `evolution_events` | id, task_id, client_id, event_type, signal, root_cause, gene_id, strategy, importance, created_at, processed | (task_id), (client_id), (event_type), (created_at) |
| `genes` | id, strategy_name, role, skills, match_condition, control_signal, failure_warnings, source_events, source_client_id, version, status, stagnation_count, use_count, success_rate, created_at, updated_at, approved_at | (role), (status) |
| `skill_drafts` | id, role, skill_name, content, source_gene_ids, status, review_comment, created_at, reviewed_at, published_at | (role), (status) |

## Files Changed

| File | Action |
|------|--------|
| `pkg/reef/evolution/event.go` | NEW — copied from design module, fixed imports |
| `pkg/reef/evolution/gene.go` | NEW — copied from design module |
| `pkg/reef/evolution/skill_draft.go` | NEW — copied from design module |
| `pkg/reef/evolution/strategy.go` | NEW — copied from design module |
| `pkg/reef/server/store/schema.go` | NEW — migration DDL + constants |
| `pkg/reef/server/store/schema_test.go` | NEW — migration and integration tests |
| `pkg/reef/server/store/sqlite.go` | MODIFIED — Migrate/EnsureMigrated + CRUD methods + scan helpers |
| `pkg/reef/server/store/sqlite_test.go` | MODIFIED — evolution events, genes, skill drafts CRUD tests |

## Commits

```
1ab1da14 feat(06-04): add evolution_events CRUD and migration tests
2abee680 feat(06-04): implement Migrate() and EnsureMigrated() with idempotent version-based migration
97c54418 feat(06-04): create schema.go with evolution DDL migration and copy evolution types
```

## Test Results

```
=== Test Count: 25 (11 existing + 14 new)
=== Result: ALL PASS
=== Duration: ~1s
=== Coverage: evolution_events, genes, skill_drafts CRUD + migration lifecycle
```

## Design Decisions

1. **Version 1 seeding**: `createTables()` inserts `schema_version=1` so `Migrate()` naturally starts at version 2. This avoids a missing migration for version 1.
2. **strategy column**: Added to evolution_events DDL to match the `EvolutionEvent.Strategy` Go field (the plan's summary mentions it, detailed DDL omitted it — resolved in favor of struct completeness).
3. **All columns use TEXT for timestamps**: RFC3339 format, consistent with the plan. No UNIX epoch conversion for evolution tables.
4. **`*sql.Rows` as scanner**: Reused the existing `scanner` interface pattern for clean scan helpers.
