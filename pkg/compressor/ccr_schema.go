package compressor

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// CCRRecord represents a single Compressed Content Record. It tracks the
// relationship between original and compressed content, along with metadata
// needed for lifecycle management (TTL, ref-counting).
//
// The CCR store is shared with Seahorse's SQLite instance (ADR-004).
type CCRRecord struct {
	// ID is a unique identifier for this record (UUID v4).
	ID string

	// OriginalHash stores the SHA256 hex digest of the original content.
	OriginalHash string

	// ContentType is the detected content type (application/json, text/code,
	// text/plain, etc.).
	ContentType string

	// CompressedAt records when compression was performed.
	CompressedAt time.Time

	// ExpiresAt is the TTL deadline. Records past this time are eligible for
	// garbage collection unless RefCount > 0.
	ExpiresAt time.Time

	// RefCount tracks how many active sessions reference this record.
	// RefCount must reach zero before GC can remove the record.
	RefCount int

	// Storage indicates where the compressed content is stored:
	//   "inline" — content embedded in the record itself
	//   "store"  — content stored externally (e.g. in a separate table)
	Storage string

	// CompressorUsed records the name of the compressor that produced this
	// record (e.g. "smart_crusher", "code_compressor").
	CompressorUsed string

	// OriginalSz is the byte length of the original (uncompressed) content.
	OriginalSz int

	// FinalSz is the byte length after compression.
	FinalSz int

	// Ratio = FinalSz / OriginalSz.  1.0 means no compression, 0.0 means
	// the content was entirely dropped.
	Ratio float64

	// SchemaVersion tracks the schema format version of this record.  Used
	// by MigrateSchema to upgrade records from older versions.
	SchemaVersion int

	// Tags are optional labels for categorising records (e.g. "production",
	// "critical").
	Tags []string
}

const defaultTTL = 7 * 24 * time.Hour

// currentSchemaVersion is the highest schema version understood by this code.
// New records are created at this version. MigrateSchema upgrades older records.
const currentSchemaVersion = 1

// NewCCRRecord creates a fully populated CCRRecord with the current time as
// CompressedAt, a default 7-day TTL, "inline" storage, and RefCount=0.
func NewCCRRecord(hash, contentType, compressorUsed string, originalSz, finalSz int, ratio float64) *CCRRecord {
	return &CCRRecord{
		ID:              newCCRID(hash, contentType),
		OriginalHash:    hash,
		ContentType:     contentType,
		CompressedAt:    time.Now(),
		ExpiresAt:       time.Now().Add(defaultTTL),
		RefCount:        0,
		Storage:         "inline",
		CompressorUsed:  compressorUsed,
		OriginalSz:      originalSz,
		FinalSz:         finalSz,
		Ratio:           ratio,
		SchemaVersion:   currentSchemaVersion,
	}
}

// IncRef atomically increments the reference count.
func (r *CCRRecord) IncRef() {
	r.RefCount++
}

// DecRef atomically decrements the reference count, clamping to zero.
func (r *CCRRecord) DecRef() {
	if r.RefCount > 0 {
		r.RefCount--
	}
}

// IsExpired returns true when ExpiresAt is in the past.
func (r *CCRRecord) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

// newCCRID generates a deterministic pseudo-UUID from the hash and content
// type.  Production code should use a proper UUID generator.
func newCCRID(hash, contentType string) string {
	h := sha256.Sum256([]byte(hash + ":" + contentType))
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

// =============================================================================
// Schema Migration
// =============================================================================

// MigrateSchema upgrades all records in the store that have SchemaVersion <
// currentSchemaVersion to the latest version. It is idempotent — records
// already at the current version are skipped. Returns the number of records
// migrated.
//
// Migration v0 → v1: Sets SchemaVersion = 1. Future migrations will add
// field transformations here.
func MigrateSchema(store CCRStore) int {
	migrated := 0
	for _, rec := range store.List() {
		if rec.SchemaVersion >= currentSchemaVersion {
			continue
		}
		// v0 → v1: simply update the version marker
		rec.SchemaVersion = currentSchemaVersion
		store.Save(rec.ID, rec)
		migrated++
	}
	return migrated
}
