package compressor

import (
	"fmt"
	"testing"
	"time"
)

func TestDebugCleanData(t *testing.T) {
	store := NewMemoryCCRStore()
	sessionID := "session-debug"

	original := []byte(`{"data":"test"}`)
	record, err := StoreZstd(store, sessionID, "application/json", original)
	if err != nil {
		t.Fatalf("StoreZstd failed: %v", err)
	}

	rec, _ := store.Get(record.ID)
	fmt.Printf("Before: ExpiresAt=%v, IsExpired=%v, RefCount=%d, Tags=%v\n",
		rec.ExpiresAt, rec.IsExpired(), rec.RefCount, rec.Tags)

	rec.ExpiresAt = time.Now().Add(-1 * time.Second)
	store.Save(record.ID, rec)

	rec2, _ := store.Get(record.ID)
	fmt.Printf("After:  ExpiresAt=%v, IsExpired=%v, RefCount=%d, Tags=%v\n",
		rec2.ExpiresAt, rec2.IsExpired(), rec2.RefCount, rec2.Tags)

	records := store.List()
	fmt.Printf("List returns %d records\n", len(records))
	for i, r := range records {
		fmt.Printf("  [%d] ID=%s Expired=%v RefCount=%d Tags=%v\n",
			i, r.ID, r.IsExpired(), r.RefCount, r.Tags)
	}

	removed := CleanData(store, sessionID)
	fmt.Printf("CleanData returned %d\n", removed)
}
