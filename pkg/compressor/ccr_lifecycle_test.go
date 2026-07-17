package compressor

import (
	"sync"
	"testing"
	"time"
)

// =============================================================================
// AT-061 [RED]: CCR 生命周期测试
// =============================================================================
//
// 测试 CCR 完整生命周期：
//   - 创建新 CCR 记录 → ID 非空
//   - 引用计数递增/递减 → 不为负
//   - 引用计数归零 → 自动标记为待清理
//   - 主动清理已过期的 CCR 条目
//   - 并发安全的引用操作

// newTestCCRID generates a unique ID for each test record
func newTestID(suffix string) string {
	return "test-ccr-" + suffix + "-" + time.Now().Format("150405.000000")
}

// ---------------------------------------------------------------------------
// TestCCRLifecycleCreate — 创建新 CCR 记录
// ---------------------------------------------------------------------------

func TestCCRLifecycleCreate(t *testing.T) {
	store := NewMemoryCCRStore()

	record, err := NewCCR(store, "test-content-hash", "application/json", "smart_crusher", 1000, 500, 0.5)

	if err != nil {
		t.Fatalf("NewCCR failed: %v", err)
	}
	if record.ID == "" {
		t.Fatal("NewCCR: expected non-empty ID")
	}
	if record.OriginalHash != "test-content-hash" {
		t.Errorf("OriginalHash: got %q, want %q", record.OriginalHash, "test-content-hash")
	}
	if record.CompressorUsed != "smart_crusher" {
		t.Errorf("CompressorUsed: got %q, want %q", record.CompressorUsed, "smart_crusher")
	}
	if record.RefCount != 0 {
		t.Errorf("RefCount after create: got %d, want 0", record.RefCount)
	}

	// Verify it's retrievable from the store
	retrieved, ok := store.Get(record.ID)
	if !ok {
		t.Fatal("record not found in store after creation")
	}
	if retrieved.ID != record.ID {
		t.Errorf("retrieved ID mismatch: %q vs %q", retrieved.ID, record.ID)
	}
}

// ---------------------------------------------------------------------------
// TestCCRLifecycleAddRef — 引用计数递增
// ---------------------------------------------------------------------------

func TestCCRLifecycleAddRef(t *testing.T) {
	store := NewMemoryCCRStore()

	record, err := NewCCR(store, "hash-1", "application/json", "smart_crusher", 1000, 500, 0.5)
	if err != nil {
		t.Fatalf("NewCCR failed: %v", err)
	}

	// Add reference
	if err := AddRef(store, record.ID); err != nil {
		t.Fatalf("AddRef failed: %v", err)
	}

	retrieved, ok := store.Get(record.ID)
	if !ok {
		t.Fatal("record not found after AddRef")
	}
	if retrieved.RefCount != 1 {
		t.Errorf("RefCount after AddRef: got %d, want 1", retrieved.RefCount)
	}

	// Add another reference
	if err := AddRef(store, record.ID); err != nil {
		t.Fatalf("AddRef (2) failed: %v", err)
	}
	retrieved, _ = store.Get(record.ID)
	if retrieved.RefCount != 2 {
		t.Errorf("RefCount after 2nd AddRef: got %d, want 2", retrieved.RefCount)
	}
}

// ---------------------------------------------------------------------------
// TestCCRLifecycleReleaseRef — 引用计数递减且不为负
// ---------------------------------------------------------------------------

func TestCCRLifecycleReleaseRef(t *testing.T) {
	store := NewMemoryCCRStore()

	record, err := NewCCR(store, "hash-2", "text/code", "code_compressor", 500, 200, 0.4)
	if err != nil {
		t.Fatalf("NewCCR failed: %v", err)
	}

	// Add 3 references
	for i := 0; i < 3; i++ {
		if err := AddRef(store, record.ID); err != nil {
			t.Fatalf("AddRef %d failed: %v", i, err)
		}
	}

	// Release one
	if err := ReleaseRef(store, record.ID); err != nil {
		t.Fatalf("ReleaseRef failed: %v", err)
	}
	retrieved, _ := store.Get(record.ID)
	if retrieved.RefCount != 2 {
		t.Errorf("RefCount after 1 release: got %d, want 2", retrieved.RefCount)
	}

	// Release remaining
	if err := ReleaseRef(store, record.ID); err != nil {
		t.Fatalf("ReleaseRef (2) failed: %v", err)
	}
	if err := ReleaseRef(store, record.ID); err != nil {
		t.Fatalf("ReleaseRef (3) failed: %v", err)
	}
	retrieved, _ = store.Get(record.ID)
	if retrieved.RefCount != 0 {
		t.Errorf("RefCount after all releases: got %d, want 0", retrieved.RefCount)
	}

	// Release below zero — should NOT go negative
	if err := ReleaseRef(store, record.ID); err != nil {
		t.Fatalf("ReleaseRef (4) failed: %v", err)
	}
	retrieved, _ = store.Get(record.ID)
	if retrieved.RefCount < 0 {
		t.Errorf("RefCount went negative: got %d", retrieved.RefCount)
	}
	if retrieved.RefCount != 0 {
		t.Errorf("RefCount after over-release: got %d, want 0", retrieved.RefCount)
	}
}

// ---------------------------------------------------------------------------
// TestCCRLifecycleCleanupOnZeroRef — 引用归零且过期时自动清理
// ---------------------------------------------------------------------------

func TestCCRLifecycleCleanupOnZeroRef(t *testing.T) {
	store := NewMemoryCCRStore()

	// Create record with past expiry and RefCount=0
	record, err := NewCCR(store, "hash-expired", "text/plain", "log_compressor", 100, 10, 0.1)
	if err != nil {
		t.Fatalf("NewCCR failed: %v", err)
	}

	// Manually set expiry to 1 second in the past
	record.ExpiresAt = time.Now().Add(-1 * time.Second)
	store.Save(record.ID, record)

	// Cleanup should remove it (ref=0 + expired)
	removed := CleanupExpired(store)
	if removed != 1 {
		t.Errorf("CleanupExpired: got %d removed, want 1", removed)
	}

	// Record should be gone
	_, ok := store.Get(record.ID)
	if ok {
		t.Error("record should have been cleaned up but still exists")
	}
}

// ---------------------------------------------------------------------------
// TestCCRLifecycleNoCleanupWithRef — 引用非零时不过期清理
// ---------------------------------------------------------------------------

func TestCCRLifecycleNoCleanupWithRef(t *testing.T) {
	store := NewMemoryCCRStore()

	record, err := NewCCR(store, "hash-active", "application/json", "smart_crusher", 1000, 500, 0.5)
	if err != nil {
		t.Fatalf("NewCCR failed: %v", err)
	}

	// Add a reference so RefCount > 0
	if err := AddRef(store, record.ID); err != nil {
		t.Fatalf("AddRef failed: %v", err)
	}

	// Expire it
	retrieved, _ := store.Get(record.ID)
	retrieved.ExpiresAt = time.Now().Add(-1 * time.Second)
	store.Save(record.ID, retrieved)

	// Cleanup should NOT remove it (ref > 0)
	removed := CleanupExpired(store)
	if removed != 0 {
		t.Errorf("CleanupExpired with ref>0: got %d removed, want 0", removed)
	}

	_, ok := store.Get(record.ID)
	if !ok {
		t.Error("record with active ref should NOT have been cleaned up")
	}
}

// ---------------------------------------------------------------------------
// TestCCRLifecycleConcurrent — 并发安全的引用操作
// ---------------------------------------------------------------------------

func TestCCRLifecycleConcurrent(t *testing.T) {
	store := NewMemoryCCRStore()

	record, err := NewCCR(store, "hash-concurrent", "application/json", "smart_crusher", 1000, 500, 0.5)
	if err != nil {
		t.Fatalf("NewCCR failed: %v", err)
	}

	goroutines := 20
	addsPerRoutine := 50
	var wg sync.WaitGroup

	// Concurrent adds
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < addsPerRoutine; i++ {
				_ = AddRef(store, record.ID)
			}
		}()
	}
	wg.Wait()

	// Check final ref count
	retrieved, _ := store.Get(record.ID)
	wantRefs := goroutines * addsPerRoutine
	if retrieved.RefCount != wantRefs {
		t.Errorf("Concurrent AddRef: got %d, want %d", retrieved.RefCount, wantRefs)
	}

	// Concurrent releases
	var wg2 sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			for i := 0; i < addsPerRoutine; i++ {
				_ = ReleaseRef(store, record.ID)
			}
		}()
	}
	wg2.Wait()

	retrieved, _ = store.Get(record.ID)
	if retrieved.RefCount != 0 {
		t.Errorf("Concurrent ReleaseRef: got %d, want 0", retrieved.RefCount)
	}
}

// ---------------------------------------------------------------------------
// TestCCRLifecycleAddRefNotFound — 对不存在的 ID 操作
// ---------------------------------------------------------------------------

func TestCCRLifecycleAddRefNotFound(t *testing.T) {
	store := NewMemoryCCRStore()

	err := AddRef(store, "non-existent-id")
	if err == nil {
		t.Error("AddRef on non-existent ID should return error")
	}

	err = ReleaseRef(store, "non-existent-id")
	if err == nil {
		t.Error("ReleaseRef on non-existent ID should return error")
	}
}
