package compressor

import (
	"testing"
	"time"
)

// =============================================================================
// AT-064: CCR Schema 迁移测试
// =============================================================================
//
// 测试内容：
//   - MigrateSchema 从旧 schema 升级
//   - 兼容性：旧格式数据可被新代码正确读取
//   - 版本号记录在存储元数据中

// ---------------------------------------------------------------------------
// TestCCRMigrateSchema — Schema 升级
// ---------------------------------------------------------------------------

func TestCCRMigrateSchema(t *testing.T) {
	store := NewMemoryCCRStore()

	// Simulate an old-format record (manually created)
	oldRecord := &CCRRecord{
		ID:             "old-record-001",
		OriginalHash:   "abc123",
		ContentType:    "application/json",
		CompressorUsed: "smart_crusher",
		OriginalSz:     1000,
		FinalSz:        500,
		Ratio:          0.5,
		Storage:        "inline",
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	// Old records have SchemaVersion = 0 (zero value)
	oldRecord.SchemaVersion = 0
	store.Save(oldRecord.ID, oldRecord)

	// Migrate
	migrated := MigrateSchema(store)
	if migrated == 0 {
		t.Error("expected at least 1 record migrated")
	}

	// After migration, version should be updated
	rec, ok := store.Get(oldRecord.ID)
	if !ok {
		t.Fatal("record should still exist after migration")
	}
	if rec.SchemaVersion < 1 {
		t.Errorf("SchemaVersion after migration: got %d, want >= 1", rec.SchemaVersion)
	}
}

// ---------------------------------------------------------------------------
// TestCCRMigrateIdempotent — 重复迁移幂等
// ---------------------------------------------------------------------------

func TestCCRMigrateIdempotent(t *testing.T) {
	store := NewMemoryCCRStore()

	store.Save("rec-1", &CCRRecord{
		ID:            "rec-1",
		OriginalHash:  "hash1",
		ContentType:   "application/json",
		Storage:       "inline",
		SchemaVersion: 0,
		ExpiresAt:     time.Now().Add(24 * time.Hour),
	})

	// First migration
	m1 := MigrateSchema(store)
	if m1 != 1 {
		t.Errorf("first migrate: got %d, want 1", m1)
	}

	// Second migration — should be idempotent (already at current version)
	m2 := MigrateSchema(store)
	if m2 != 0 {
		t.Errorf("second migrate: got %d, want 0 (should be idempotent)", m2)
	}
}

// ---------------------------------------------------------------------------
// TestCCRSchemaVersionOnCreate — 新记录自动设置版本
// ---------------------------------------------------------------------------

func TestCCRSchemaVersionOnCreate(t *testing.T) {
	store := NewMemoryCCRStore()

	record, err := NewCCR(store, "hash-new", "application/json", "smart_crusher", 1000, 500, 0.5)
	if err != nil {
		t.Fatalf("NewCCR failed: %v", err)
	}

	if record.SchemaVersion == 0 {
		t.Error("new record should have non-zero SchemaVersion")
	}
}

// ---------------------------------------------------------------------------
// TestCCRReadOldFormat — 旧格式兼容读取
// ---------------------------------------------------------------------------

func TestCCRReadOldFormat(t *testing.T) {
	store := NewMemoryCCRStore()

	// Old format: SchemaVersion=0, no compressed data
	oldRecord := &CCRRecord{
		ID:             "old-compat-001",
		OriginalHash:   "deadbeef",
		ContentType:    "text/plain",
		CompressorUsed: "log_compressor",
		OriginalSz:     200,
		FinalSz:        100,
		Ratio:          0.5,
		Storage:        "inline",
		SchemaVersion:  0,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	store.Save(oldRecord.ID, oldRecord)

	// Should be readable with Get
	rec, ok := store.Get(oldRecord.ID)
	if !ok {
		t.Fatal("old format record should be readable")
	}
	if rec.ID != "old-compat-001" {
		t.Errorf("ID: got %q, want %q", rec.ID, "old-compat-001")
	}
	if rec.OriginalHash != "deadbeef" {
		t.Errorf("OriginalHash: got %q, want %q", rec.OriginalHash, "deadbeef")
	}

	// After migration, it should still be readable
	MigrateSchema(store)
	rec2, ok := store.Get(oldRecord.ID)
	if !ok {
		t.Fatal("record should exist after migration")
	}
	if rec2.SchemaVersion == 0 {
		t.Error("SchemaVersion should be updated after migration")
	}
}

// ---------------------------------------------------------------------------
// TestCCRCleanupPreservesMigrated — 迁移后清理保留数据
// ---------------------------------------------------------------------------

func TestCCRCleanupPreservesMigrated(t *testing.T) {
	store := NewMemoryCCRStore()

	store.Save("rec-mig", &CCRRecord{
		ID:            "rec-mig",
		OriginalHash:  "hash-mig",
		ContentType:   "application/json",
		Storage:       "inline",
		SchemaVersion: 0,
		ExpiresAt:     time.Now().Add(24 * time.Hour),
	})

	MigrateSchema(store)

	// After migration, CleanupExpired should not remove it (it's not expired)
	removed := CleanupExpired(store)
	if removed != 0 {
		t.Errorf("CleanupExpired after migration: got %d, want 0", removed)
	}

	_, ok := store.Get("rec-mig")
	if !ok {
		t.Error("migrated record should still exist")
	}
}
