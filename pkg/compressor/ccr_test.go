package compressor

import (
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestCCRStore() CCRStore {
	return NewMemoryCCRStore()
}

// ---------------------------------------------------------------------------
// TestCCRRefCount — table-driven: init / increment / decrement / no-go-below-zero
// ---------------------------------------------------------------------------

func TestCCRRefCount(t *testing.T) {
	tests := []struct {
		name string
		ops  []string // "inc" or "dec"
		want int
	}{
		{"init zero", nil, 0},
		{"inc once", []string{"inc"}, 1},
		{"inc thrice", []string{"inc", "inc", "inc"}, 3},
		{"inc+dec", []string{"inc", "inc", "dec"}, 1},
		{"inc×2 dec×2", []string{"inc", "inc", "dec", "dec"}, 0},
		{"no below zero", []string{"dec", "dec", "dec"}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := NewCCRRecord("hash1", "json", "test", 100, 50, 0.5)
			for _, op := range tt.ops {
				switch op {
				case "inc":
					rec.IncRef()
				case "dec":
					rec.DecRef()
				}
			}
			if rec.RefCount != tt.want {
				t.Errorf("RefCount = %d, want %d", rec.RefCount, tt.want)
			}
			if rec.RefCount < 0 {
				t.Error("RefCount must never go below 0")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCCRTTLExpired — table-driven TTL boundary checks
// ---------------------------------------------------------------------------

func TestCCRTTLExpired(t *testing.T) {
	tests := []struct {
		name      string
		offset    time.Duration // from Now
		isExpired bool
	}{
		{"future 1h", time.Hour, false},
		{"future 1ms", time.Millisecond, false},
		{"past 1s", -time.Second, true},
		{"past 1h", -time.Hour, true},
		{"zero (epoch)", -time.Since(time.Time{}), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := NewCCRRecord("hash1", "json", "test", 100, 50, 0.5)
			rec.ExpiresAt = time.Now().Add(tt.offset)
			if got := rec.IsExpired(); got != tt.isExpired {
				t.Errorf("IsExpired() = %v, want %v (offset=%v)", got, tt.isExpired, tt.offset)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCCRStoreCRUD — table-driven: save → get → verify
// ---------------------------------------------------------------------------

func TestCCRStoreCRUD(t *testing.T) {
	tests := []struct {
		name       string
		hash       string
		compressor string
	}{
		{"basic", "hash1", "smart_crusher"},
		{"code", "hash2", "code_compressor"},
		{"log", "hash3", "log_compressor"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestCCRStore()
			rec := NewCCRRecord(tt.hash, "application/json", tt.compressor, 1000, 400, 0.4)

			// Save
			if err := store.Save(rec.ID, rec); err != nil {
				t.Fatalf("Save: %v", err)
			}

			// Get
			got, ok := store.Get(rec.ID)
			if !ok {
				t.Fatal("Get: record not found after Save")
			}
			if got.ID != rec.ID {
				t.Errorf("ID: got %q, want %q", got.ID, rec.ID)
			}
			if got.OriginalHash != rec.OriginalHash {
				t.Errorf("Hash mismatch: got %q, want %q", got.OriginalHash, rec.OriginalHash)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestCCRStoreGetNotFound
// ---------------------------------------------------------------------------

func TestCCRStoreGetNotFound(t *testing.T) {
	store := newTestCCRStore()
	_, ok := store.Get("nonexistent-id")
	if ok {
		t.Error("Get: expected false for nonexistent record")
	}
}

// ---------------------------------------------------------------------------
// TestCCRStoreDelete
// ---------------------------------------------------------------------------

func TestCCRStoreDelete(t *testing.T) {
	store := newTestCCRStore()
	rec := NewCCRRecord("hash1", "application/json", "smart_crusher", 1000, 400, 0.4)

	if err := store.Save(rec.ID, rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if !store.Delete(rec.ID) {
		t.Error("Delete: expected true for existing record")
	}

	// Should be gone
	_, ok := store.Get(rec.ID)
	if ok {
		t.Error("Get: record should be gone after Delete")
	}

	// Delete again should return false
	if store.Delete(rec.ID) {
		t.Error("Delete: expected false for already-deleted record")
	}
}

// ---------------------------------------------------------------------------
// TestCCRStoreList
// ---------------------------------------------------------------------------

func TestCCRStoreList(t *testing.T) {
	store := newTestCCRStore()

	r1 := NewCCRRecord("h1", "application/json", "sc", 1000, 400, 0.4)
	r2 := NewCCRRecord("h2", "text/code", "cc", 500, 200, 0.4)

	store.Save(r1.ID, r1)
	store.Save(r2.ID, r2)

	list := store.List()
	if len(list) != 2 {
		t.Errorf("List: expected 2 records, got %d", len(list))
	}
}

// ---------------------------------------------------------------------------
// TestCCRStoreCleanup
// ---------------------------------------------------------------------------

func TestCCRStoreCleanup(t *testing.T) {
	store := newTestCCRStore()

	// Expired record
	r1 := NewCCRRecord("h1", "application/json", "sc", 1000, 400, 0.4)
	r1.ExpiresAt = time.Now().Add(-time.Hour)
	store.Save(r1.ID, r1)

	// Valid record
	r2 := NewCCRRecord("h2", "text/code", "cc", 500, 200, 0.4)
	r2.ExpiresAt = time.Now().Add(time.Hour)
	store.Save(r2.ID, r2)

	cleaned := store.Cleanup()
	if cleaned != 1 {
		t.Errorf("Cleanup: expected 1 cleaned, got %d", cleaned)
	}

	// Expired record should be gone
	_, ok := store.Get(r1.ID)
	if ok {
		t.Error("expired record should be gone after Cleanup")
	}

	// Valid record should remain
	_, ok = store.Get(r2.ID)
	if !ok {
		t.Error("valid record should remain after Cleanup")
	}
}

// ---------------------------------------------------------------------------
// TestCCRStoreConcurrency
// ---------------------------------------------------------------------------

func TestCCRStoreConcurrency(t *testing.T) {
	store := newTestCCRStore()
	rec := NewCCRRecord("h1", "application/json", "sc", 1000, 400, 0.4)
	store.Save(rec.ID, rec)

	var wg sync.WaitGroup
	const goroutines = 50
	const opsPerG = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerG; j++ {
				store.Get(rec.ID)
			}
		}()
	}

	wg.Wait()

	// Record should still be intact
	got, ok := store.Get(rec.ID)
	if !ok {
		t.Fatal("record lost after concurrent reads")
	}
	if got.ID != rec.ID {
		t.Error("record corrupted after concurrent reads")
	}
}
