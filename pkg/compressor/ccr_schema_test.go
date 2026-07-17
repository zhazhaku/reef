package compressor

import (
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// TestCCRSchemaCreate
// ---------------------------------------------------------------------------

func TestCCRSchemaCreate(t *testing.T) {
	rec := NewCCRRecord("abc123", "application/json", "smart_crusher", 1000, 400, 0.4)

	if rec.ID == "" {
		t.Error("CCRRecord.ID should not be empty")
	}
	if rec.OriginalHash != "abc123" {
		t.Errorf("OriginalHash: want abc123, got %s", rec.OriginalHash)
	}
	if rec.ContentType != "application/json" {
		t.Errorf("ContentType: want application/json, got %s", rec.ContentType)
	}
	if rec.CompressorUsed != "smart_crusher" {
		t.Errorf("CompressorUsed: want smart_crusher, got %s", rec.CompressorUsed)
	}
	if rec.OriginalSz != 1000 {
		t.Errorf("OriginalSz: want 1000, got %d", rec.OriginalSz)
	}
	if rec.FinalSz != 400 {
		t.Errorf("FinalSz: want 400, got %d", rec.FinalSz)
	}
	if rec.Ratio != 0.4 {
		t.Errorf("Ratio: want 0.4, got %f", rec.Ratio)
	}
	if rec.RefCount != 0 {
		t.Errorf("RefCount: want 0, got %d", rec.RefCount)
	}
	if rec.CompressedAt.IsZero() {
		t.Error("CompressedAt should not be zero")
	}
}

// ---------------------------------------------------------------------------
// TestCCRSchemaCompressionInfo
// ---------------------------------------------------------------------------

func TestCCRSchemaCompressionInfo(t *testing.T) {
	rec := NewCCRRecord("hash123", "text/code", "code_compressor", 2000, 700, 0.35)

	// Verify compression metadata
	if rec.Ratio <= 0 || rec.Ratio > 1.0 {
		t.Errorf("Ratio out of range: %f", rec.Ratio)
	}
	if rec.OriginalSz <= 0 {
		t.Errorf("OriginalSz should be positive: %d", rec.OriginalSz)
	}
	if rec.FinalSz <= 0 {
		t.Errorf("FinalSz should be positive: %d", rec.FinalSz)
	}
	// Timestamp should be recent
	if time.Since(rec.CompressedAt) > time.Second {
		t.Errorf("CompressedAt too old: %v", rec.CompressedAt)
	}
}

// ---------------------------------------------------------------------------
// TestCCRSchemaRefCount
// ---------------------------------------------------------------------------

func TestCCRSchemaRefCount(t *testing.T) {
	rec := NewCCRRecord("hash", "text/plain", "none", 100, 100, 1.0)

	if rec.RefCount != 0 {
		t.Errorf("initial RefCount should be 0, got %d", rec.RefCount)
	}

	rec.IncRef()
	if rec.RefCount != 1 {
		t.Errorf("after IncRef: want 1, got %d", rec.RefCount)
	}

	rec.IncRef()
	rec.IncRef()
	if rec.RefCount != 3 {
		t.Errorf("after 3x IncRef: want 3, got %d", rec.RefCount)
	}

	rec.DecRef()
	if rec.RefCount != 2 {
		t.Errorf("after DecRef: want 2, got %d", rec.RefCount)
	}

	// DecRef should not go below 0
	rec.DecRef()
	rec.DecRef()
	rec.DecRef() // would go to -1
	if rec.RefCount < 0 {
		t.Errorf("RefCount should not go below 0, got %d", rec.RefCount)
	}
}

// ---------------------------------------------------------------------------
// TestCCRSchemaTTL
// ---------------------------------------------------------------------------

func TestCCRSchemaTTL(t *testing.T) {
	rec := NewCCRRecord("hash", "application/json", "smart_crusher", 1000, 400, 0.4)

	// Set TTL to 1 hour from now
	ttl := time.Now().Add(time.Hour)
	rec.ExpiresAt = ttl

	if rec.ExpiresAt != ttl {
		t.Errorf("ExpiresAt not set correctly")
	}

	// Should not be expired
	if rec.IsExpired() {
		t.Error("record with future TTL should not be expired")
	}

	// Set TTL to 1 second ago
	rec.ExpiresAt = time.Now().Add(-time.Second)
	if !rec.IsExpired() {
		t.Error("record with past TTL should be expired")
	}
}

// ---------------------------------------------------------------------------
// TestCCRSchemaStorage
// ---------------------------------------------------------------------------

func TestCCRSchemaStorage(t *testing.T) {
	rec := NewCCRRecord("hash", "application/json", "smart_crusher", 1000, 400, 0.4)

	rec.Storage = "inline"
	rec.Tags = []string{"production", "critical"}

	if rec.Storage != "inline" {
		t.Errorf("Storage: want inline, got %s", rec.Storage)
	}
	if len(rec.Tags) != 2 {
		t.Errorf("Tags: want 2 items, got %d", len(rec.Tags))
	}
	if rec.Tags[0] != "production" || rec.Tags[1] != "critical" {
		t.Errorf("Tags: unexpected values: %v", rec.Tags)
	}
}

// ---------------------------------------------------------------------------
// TestCCRSchemaDefaultStorage
// ---------------------------------------------------------------------------

func TestCCRSchemaDefaultStorage(t *testing.T) {
	rec := NewCCRRecord("hash", "text/plain", "none", 100, 100, 1.0)

	// Default storage should be set
	if rec.Storage == "" {
		t.Error("Storage should have a default value")
	}
}
