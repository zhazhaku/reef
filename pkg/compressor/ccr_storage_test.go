package compressor

import (
	"testing"
	"time"
)

// =============================================================================
// AT-063 [RED]: Zstd 压缩存储测试
// =============================================================================
//
// 测试 Zstd 压缩存储：
//   - 数据能用 Zstd 压缩/解压（正反一致）
//   - 压缩后的内容小于原始内容（对可压缩数据）
//   - 解压结果与原始一致
//   - 过期的 CCR 存储数据可清理
//   - 不同会话数据隔离

// ---------------------------------------------------------------------------
// TestCCRZstdRoundtrip — Zstd 压缩解压正反一致
// ---------------------------------------------------------------------------

func TestCCRZstdRoundtrip(t *testing.T) {
	store := NewMemoryCCRStore()
	sessionID := "session-roundtrip"

	original := []byte(`{"data":[{"id":1,"name":"test","value":"abcdefghijklmnopqrstuvwxyz"},{"id":2,"name":"test2","value":"1234567890"}]}`)

	// Store with Zstd compression
	record, err := StoreZstd(store, sessionID, "application/json", original)
	if err != nil {
		t.Fatalf("StoreZstd failed: %v", err)
	}
	if record.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if record.Storage != "zstd" {
		t.Errorf("Storage: got %q, want %q", record.Storage, "zstd")
	}

	// Load and decompress
	loaded, err := LoadZstd(store, sessionID, record.ID)
	if err != nil {
		t.Fatalf("LoadZstd failed: %v", err)
	}

	if string(loaded) != string(original) {
		t.Errorf("roundtrip mismatch:\n  got  %q\n  want %q", string(loaded), string(original))
	}
}

// ---------------------------------------------------------------------------
// TestCCRZstdCompressionRatio — 压缩后内容比原始小
// ---------------------------------------------------------------------------

func TestCCRZstdCompressionRatio(t *testing.T) {
	store := NewMemoryCCRStore()
	sessionID := "session-ratio"

	// Create highly compressible content (repeated pattern)
	var original []byte
	pattern := `{"msg":"log entry with some text data that repeats","level":"info","ts":"2024-01-15T10:30:00Z"}`
	for i := 0; i < 100; i++ {
		original = append(original, []byte(pattern)...)
		if i < 99 {
			original = append(original, '\n')
		}
	}

	record, err := StoreZstd(store, sessionID, "application/json", original)
	if err != nil {
		t.Fatalf("StoreZstd failed: %v", err)
	}

	if record.FinalSz <= 0 {
		t.Error("FinalSz should be > 0")
	}
	if record.FinalSz >= record.OriginalSz {
		t.Errorf("compressed size (%d) should be smaller than original (%d)",
			record.FinalSz, record.OriginalSz)
	}
}

// ---------------------------------------------------------------------------
// TestCCRZstdDecompressionMatches — 解压结果与原始一致
// ---------------------------------------------------------------------------

func TestCCRZstdDecompressionMatches(t *testing.T) {
	store := NewMemoryCCRStore()
	sessionID := "session-match"

	original := []byte(`{"status":"ok","error":null,"result":{"count":42,"items":["a","b","c"]}}`)

	record, err := StoreZstd(store, sessionID, "application/json", original)
	if err != nil {
		t.Fatalf("StoreZstd failed: %v", err)
	}

	loaded, err := LoadZstd(store, sessionID, record.ID)
	if err != nil {
		t.Fatalf("LoadZstd failed: %v", err)
	}
	if string(loaded) != string(original) {
		t.Error("decompressed content does not match original")
	}
}

// ---------------------------------------------------------------------------
// TestCCRZstdCleanupExpired — 过期存储可清理
// ---------------------------------------------------------------------------

func TestCCRZstdCleanupExpired(t *testing.T) {
	store := NewMemoryCCRStore()
	sessionID := "session-cleanup"

	original := []byte(`{"data":"test"}`)
	record, err := StoreZstd(store, sessionID, "application/json", original)
	if err != nil {
		t.Fatalf("StoreZstd failed: %v", err)
	}

	// Expire the record
	rec, _ := store.Get(record.ID)
	rec.ExpiresAt = time.Now().Add(-1 * time.Second)
	store.Save(record.ID, rec)

	// CleanData should remove expired data
	removed := CleanData(store, sessionID)
	if removed != 1 {
		t.Errorf("CleanData: got %d removed, want 1", removed)
	}
}

// ---------------------------------------------------------------------------
// TestCCRZstdSessionIsolation — 不同会话数据隔离
// ---------------------------------------------------------------------------

func TestCCRZstdSessionIsolation(t *testing.T) {
	store := NewMemoryCCRStore()

	sessionA := "session-A"
	sessionB := "session-B"

	dataA := []byte(`{"from":"session-A"}`)
	dataB := []byte(`{"from":"session-B"}`)

	recA, err := StoreZstd(store, sessionA, "application/json", dataA)
	if err != nil {
		t.Fatalf("StoreZstd A failed: %v", err)
	}
	recB, err := StoreZstd(store, sessionB, "application/json", dataB)
	if err != nil {
		t.Fatalf("StoreZstd B failed: %v", err)
	}

	// Load from session-A should get dataA, not dataB
	loaded, err := LoadZstd(store, sessionA, recA.ID)
	if err != nil {
		t.Fatalf("LoadZstd A failed: %v", err)
	}
	if string(loaded) != string(dataA) {
		t.Error("session A loaded wrong data")
	}

	// Load from session-B should get dataB
	loaded, err = LoadZstd(store, sessionB, recB.ID)
	if err != nil {
		t.Fatalf("LoadZstd B failed: %v", err)
	}
	if string(loaded) != string(dataB) {
		t.Error("session B loaded wrong data")
	}

	// Cross-load should return error
	_, err = LoadZstd(store, sessionA, recB.ID)
	if err == nil {
		t.Error("cross-session load should return error")
	}
}

// ---------------------------------------------------------------------------
// TestCCRZstdNotFound — 不存在的记录
// ---------------------------------------------------------------------------

func TestCCRZstdNotFound(t *testing.T) {
	store := NewMemoryCCRStore()

	_, err := LoadZstd(store, "session-x", "non-existent-id")
	if err == nil {
		t.Error("LoadZstd for non-existent ID should return error")
	}

	_, err = LoadZstd(store, "session-y", "")
	if err == nil {
		t.Error("LoadZstd for empty ID should return error")
	}
}
