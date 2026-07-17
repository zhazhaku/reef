package compressor

import (
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// =============================================================================
// Zstd Compressed Content Storage
// =============================================================================

// =============================================================================
// CompressedStore interface — allows any CCRStore implementation to support
// Zstd compressed data storage without direct type assertion to MemoryCCRStore.
// =============================================================================

// CompressedStore is an optional interface that a CCRStore can implement to
// support Zstd compressed payload storage alongside CCR metadata.
type CompressedStore interface {
	CCRStore
	PutCompressedData(id string, data []byte)
	GetCompressedData(id string) ([]byte, bool)
	DeleteCompressedData(id string) bool
}

// maxDecompressedSize limits the maximum decompressed payload to prevent
// zip-bomb style attacks. 256 MiB is generous for any realistic payload.
const maxDecompressedSize = 256 << 20

// CompressedDataStore stores raw compressed bytes keyed by record ID.
// It is embedded in MemoryCCRStore for Zstd storage support.
type CompressedDataStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// newCompressedDataStore creates an empty compressed data store.
func newCompressedDataStore() *CompressedDataStore {
	return &CompressedDataStore{
		data: make(map[string][]byte),
	}
}

// put stores compressed data for a record ID.
func (s *CompressedDataStore) put(id string, data []byte) {
	s.mu.Lock()
	s.data[id] = data
	s.mu.Unlock()
}

// get retrieves compressed data for a record ID.
func (s *CompressedDataStore) get(id string) ([]byte, bool) {
	s.mu.RLock()
	data, ok := s.data[id]
	s.mu.RUnlock()
	return data, ok
}

// remove deletes compressed data and returns true if existed.
func (s *CompressedDataStore) remove(id string) bool {
	s.mu.Lock()
	_, ok := s.data[id]
	delete(s.data, id)
	s.mu.Unlock()
	return ok
}

// =============================================================================
// StoreZstd / LoadZstd / CleanData
// =============================================================================

// StoreZstd compresses data with Zstd, stores both the CCR metadata and the
// compressed payload, and returns the CCRRecord. The sessionID is used to
// namespace records so that different sessions' data is isolated.
// The store must implement the optional CompressedStore interface for
// compressed data storage. If it does not, an error is returned.
func StoreZstd(store CCRStore, sessionID, contentType string, data []byte) (*CCRRecord, error) {
	// Use the CompressedStore interface instead of direct type assertion
	cStore, ok := store.(CompressedStore)
	if !ok {
		return nil, fmt.Errorf("ccr.StoreZstd: store does not implement CompressedStore")
	}

	// Compress with Zstd
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("ccr.StoreZstd: encoder create failed: %w", err)
	}
	compressed := encoder.EncodeAll(data, make([]byte, 0, len(data)/2))
	encoder.Close()

	// Create CCR record with a proper SHA-256 hash
	h := sha256.Sum256(data)
	hash := fmt.Sprintf("%x", h[:16]) // 16 bytes = 32 hex chars, low collision
	record := NewCCRRecord(hash, contentType, "zstd", len(data), len(compressed),
		float64(len(compressed))/float64(len(data)))
	record.Storage = "zstd"

	// Add session tag
	record.Tags = append(record.Tags, "session:"+sessionID)

	// Store the metadata
	if err := store.Save(record.ID, record); err != nil {
		return nil, fmt.Errorf("ccr.StoreZstd: save metadata failed: %w", err)
	}

	// Store the compressed data via the interface
	cStore.PutCompressedData(record.ID, compressed)

	return record, nil
}

// LoadZstd retrieves a CCR record by ID, decompresses the Zstd payload, and
// returns the original data. sessionID must match the session that stored it.
// Decompression is limited to maxDecompressedSize to prevent zip-bomb attacks.
func LoadZstd(store CCRStore, sessionID, id string) ([]byte, error) {
	if id == "" {
		return nil, fmt.Errorf("ccr.LoadZstd: id is empty")
	}

	// Use the CompressedStore interface instead of direct type assertion
	cStore, ok := store.(CompressedStore)
	if !ok {
		return nil, fmt.Errorf("ccr.LoadZstd: store does not implement CompressedStore")
	}

	// Retrieve metadata
	rec, found := store.Get(id)
	if !found {
		return nil, fmt.Errorf("ccr.LoadZstd: record %q not found", id)
	}

	// Verify storage type
	if rec.Storage != "zstd" {
		return nil, fmt.Errorf("ccr.LoadZstd: record %q has storage type %q, not zstd", id, rec.Storage)
	}

	// Verify session ownership
	if !hasSessionTag(rec, sessionID) {
		return nil, fmt.Errorf("ccr.LoadZstd: record %q does not belong to session %q", id, sessionID)
	}

	// Get compressed data via the interface
	compressed, found := cStore.GetCompressedData(id)
	if !found {
		return nil, fmt.Errorf("ccr.LoadZstd: compressed data for %q not found", id)
	}

	// Decompress with size limit to prevent zip bomb
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("ccr.LoadZstd: decoder create failed: %w", err)
	}
	defer decoder.Close()

	decompressed, err := decoder.DecodeAll(compressed, make([]byte, 0, min(len(compressed)*4, maxDecompressedSize)))
	if err != nil {
		return nil, fmt.Errorf("ccr.LoadZstd: decompress failed: %w", err)
	}
	if len(decompressed) > maxDecompressedSize {
		return nil, fmt.Errorf("ccr.LoadZstd: decompressed size %d exceeds limit %d", len(decompressed), maxDecompressedSize)
	}

	return decompressed, nil
}

// CleanData removes expired records (both metadata and compressed data) for
// a specific session. Returns the number of records removed.
func CleanData(store CCRStore, sessionID string) int {
	cStore, ok := store.(CompressedStore)
	if !ok {
		return 0
	}

	removed := 0
	records := store.List()
	for _, rec := range records {
		if !hasSessionTag(rec, sessionID) {
			continue
		}
		if rec.IsExpired() && rec.RefCount == 0 {
			store.Delete(rec.ID)
			cStore.DeleteCompressedData(rec.ID)
			removed++
		}
	}
	return removed
}

// hasSessionTag checks whether a record has a tag matching "session:<id>".
func hasSessionTag(rec *CCRRecord, sessionID string) bool {
	target := "session:" + sessionID
	for _, tag := range rec.Tags {
		if tag == target {
			return true
		}
	}
	return false
}
