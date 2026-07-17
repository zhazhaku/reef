package compressor

import (
	"fmt"
	"sync"
)

// =============================================================================
// CCRStore — interface for compressed-content-record persistence
// =============================================================================
//
// CCRStore is the storage abstraction behind the Compressed Content Record
// lifecycle. Implementations may be in-memory (MemoryCCRStore for testing)
// or backed by Seahorse's SQLite instance in production.

// CCRStore defines the contract for Compressed Content Record storage.
type CCRStore interface {
	// Save persists a CCRRecord keyed by its ID. Returns an error if the
	// record cannot be stored.
	Save(id string, record *CCRRecord) error

	// Get retrieves a record by ID. The boolean indicates whether the
	// record was found.
	Get(id string) (*CCRRecord, bool)

	// Delete removes a record by ID and returns true if it existed.
	Delete(id string) bool

	// List returns a snapshot of all records currently in the store.
	List() []*CCRRecord

	// Cleanup removes all expired records (ExpiresAt in the past) and
	// returns the number of records removed. Records with RefCount > 0
	// are NOT removed even if expired.
	Cleanup() int
}

// =============================================================================
// MemoryCCRStore — thread-safe in-memory CCRStore
// =============================================================================

// MemoryCCRStore is a goroutine-safe in-memory implementation of CCRStore.
// It is suitable for testing and small deployments; production use should
// prefer a SQLite-backed store.
type MemoryCCRStore struct {
	mu        sync.RWMutex
	data      map[string]*CCRRecord
	dataStore *CompressedDataStore
}

// NewMemoryCCRStore creates an empty MemoryCCRStore.
func NewMemoryCCRStore() *MemoryCCRStore {
	return &MemoryCCRStore{
		data:      make(map[string]*CCRRecord),
		dataStore: newCompressedDataStore(),
	}
}

// Save stores a record. A nil record returns an error.
func (s *MemoryCCRStore) Save(id string, record *CCRRecord) error {
	if record == nil {
		return fmt.Errorf("ccr: cannot save nil record")
	}
	s.mu.Lock()
	s.data[id] = record
	s.mu.Unlock()
	return nil
}

// Get retrieves a record. Returns nil, false when not found.
func (s *MemoryCCRStore) Get(id string) (*CCRRecord, bool) {
	s.mu.RLock()
	rec, ok := s.data[id]
	s.mu.RUnlock()
	return rec, ok
}

// Delete removes a record. Returns true if it existed.
func (s *MemoryCCRStore) Delete(id string) bool {
	s.mu.Lock()
	_, ok := s.data[id]
	delete(s.data, id)
	s.mu.Unlock()
	return ok
}

// List returns all records in the store as a slice.
// The order is non-deterministic (map iteration).
func (s *MemoryCCRStore) List() []*CCRRecord {
	s.mu.RLock()
	out := make([]*CCRRecord, 0, len(s.data))
	for _, rec := range s.data {
		out = append(out, rec)
	}
	s.mu.RUnlock()
	return out
}

// Cleanup removes expired records that have RefCount == 0.
// Returns the number of records removed.
func (s *MemoryCCRStore) Cleanup() int {
	removed := 0

	s.mu.Lock()
	for id, rec := range s.data {
		if rec.IsExpired() && rec.RefCount == 0 {
			delete(s.data, id)
			removed++
		}
	}
	s.mu.Unlock()

	return removed
}

// =============================================================================
// CompressedStore implementation — delegates to the internal dataStore
// =============================================================================

// PutCompressedData stores compressed bytes keyed by record ID.
func (s *MemoryCCRStore) PutCompressedData(id string, data []byte) {
	s.dataStore.put(id, data)
}

// GetCompressedData retrieves compressed bytes by record ID.
func (s *MemoryCCRStore) GetCompressedData(id string) ([]byte, bool) {
	return s.dataStore.get(id)
}

// DeleteCompressedData removes compressed data by record ID.
func (s *MemoryCCRStore) DeleteCompressedData(id string) bool {
	return s.dataStore.remove(id)
}

// =============================================================================
// CCR Lifecycle — NewCCR / AddRef / ReleaseRef / CleanupExpired
// =============================================================================

// NewCCR creates a new CCRRecord, saves it to the store, and returns the
// record. The record is populated with a deterministic ID derived from the
// hash and content type, a default 7-day TTL, and RefCount=0.
func NewCCR(store CCRStore, hash, contentType, compressorUsed string, originalSz, finalSz int, ratio float64) (*CCRRecord, error) {
	record := NewCCRRecord(hash, contentType, compressorUsed, originalSz, finalSz, ratio)
	if err := store.Save(record.ID, record); err != nil {
		return nil, fmt.Errorf("NewCCR: save failed: %w", err)
	}
	return record, nil
}

// AddRef increments the reference count of a CCR record by 1. Returns an error
// if the record does not exist.
func AddRef(store CCRStore, id string) error {
	rec, ok := store.Get(id)
	if !ok {
		return fmt.Errorf("ccr.AddRef: record %q not found", id)
	}
	rec.IncRef()
	return store.Save(id, rec)
}

// ReleaseRef decrements the reference count of a CCR record by 1. The count
// is clamped to zero and never goes negative. Returns an error if the record
// does not exist.
func ReleaseRef(store CCRStore, id string) error {
	rec, ok := store.Get(id)
	if !ok {
		return fmt.Errorf("ccr.ReleaseRef: record %q not found", id)
	}
	rec.DecRef()
	return store.Save(id, rec)
}

// CleanupExpired removes all expired records from the store that have
// RefCount == 0. Returns the number of records removed.
func CleanupExpired(store CCRStore) int {
	return store.Cleanup()
}
