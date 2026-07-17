// Package compressor provides content compression for LLM context windows.
//
// [RED] This test file is written BEFORE the implementation.
// Running `go test` now should FAIL — types, interface, and registry do not exist yet.
package compressor

import (
	"context"
	"testing"
)

// =============================================================================
// mockCompressor is a stub implementing Compressor for registry tests.
// =============================================================================

type mockCompressor struct {
	name string
	typ  CompressType
}

func (m *mockCompressor) Name() string                                           { return m.name }
func (m *mockCompressor) Type() CompressType                                     { return m.typ }
func (m *mockCompressor) Compress(ctx context.Context, content []byte, opts CompressOptions) (*CompressResult, error) {
	return &CompressResult{
		Content:    string(content),
		OriginalSz: len(content),
		FinalSz:    len(content),
		Ratio:      1.0,
	}, nil
}
func (m *mockCompressor) Decompress(ctx context.Context, compressed []byte) ([]byte, error) {
	return compressed, nil
}

// =============================================================================
// TestCompileInterface — compile-time check that Compressor interface is valid
// =============================================================================

func TestCompileInterface(t *testing.T) {
	// This test verifies the Compressor interface compiles.
	// Declaring a variable of type Compressor forces the compiler to check.
	var _ Compressor = &mockCompressor{name: "compile-check"}

	// Verify the interface is usable
	c := Compressor(&mockCompressor{name: "test", typ: CompressTypeTruncate})
	if c == nil {
		t.Fatal("Compressor must not be nil")
	}
	if c.Name() != "test" {
		t.Errorf("Name() = %q, want %q", c.Name(), "test")
	}
	if c.Type() != CompressTypeTruncate {
		t.Errorf("Type() = %v, want %v", c.Type(), CompressTypeTruncate)
	}
}

// =============================================================================
// TestRegistry — test Register/Get/MustGet basic operations
// =============================================================================

func TestRegistry(t *testing.T) {
	mock := &mockCompressor{name: "test", typ: CompressTypeTruncate}

	// Register should succeed
	Register("test", mock)

	// Get should return the registered compressor
	got := Get("test")
	if got == nil {
		t.Fatal("Get('test') returned nil after Register")
	}
	if got.Name() != "test" {
		t.Errorf("got.Name() = %q, want %q", got.Name(), "test")
	}

	// MustGet should not panic for registered compressor
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("MustGet('test') panicked unexpectedly: %v", r)
			}
		}()
		must := MustGet("test")
		if must == nil {
			t.Error("MustGet('test') returned nil")
		}
	}()
}

// =============================================================================
// TestRegistryMustGetPanic — MustGet on nonexistent must panic
// =============================================================================

func TestRegistryMustGetPanic(t *testing.T) {
	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		MustGet("nonexistent")
	}()
	if !didPanic {
		t.Fatal("MustGet('nonexistent') should have panicked")
	}
}

// =============================================================================
// TestRegistryDuplicate — duplicate Register must panic
// =============================================================================

func TestRegistryDuplicate(t *testing.T) {
	mock1 := &mockCompressor{name: "dup"}
	mock2 := &mockCompressor{name: "dup"}

	Register("dup", mock1)

	didPanic := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
			}
		}()
		Register("dup", mock2)
	}()
	if !didPanic {
		t.Fatal("Register('dup', ...) second time should have panicked")
	}
}

// =============================================================================
// TestRegistryNames — Names() returns list of registered names
// =============================================================================

func TestRegistryNames(t *testing.T) {
	Register("alpha", &mockCompressor{name: "alpha"})
	Register("beta", &mockCompressor{name: "beta"})

	names := Names()
	if len(names) < 2 {
		t.Fatalf("Names() returned %d items, want >= 2", len(names))
	}

	// Verify both registered compressors appear
	found := make(map[string]bool)
	for _, n := range names {
		found[n] = true
	}
	if !found["alpha"] {
		t.Error("Names() missing 'alpha'")
	}
	if !found["beta"] {
		t.Error("Names() missing 'beta'")
	}
}

// =============================================================================
// TestRegistryEmpty — empty registry Names() returns empty slice
// =============================================================================

func TestRegistryEmpty(t *testing.T) {
	// Note: this test assumes a fresh registry.
	// Since the registry is package-level, earlier tests may have registered
	// compressors. This test validates that Names() returns a slice (not nil)
	// and does not panic even when the registry is used after other tests.
	//
	// We just validate that Names() is callable and returns a non-nil value.
	names := Names()
	if names == nil {
		t.Fatal("Names() returned nil, want empty slice")
	}
}

// =============================================================================
// TestCompressOptions — verify CompressOptions struct compiles and consts exist
// =============================================================================

func TestCompressOptions(t *testing.T) {
	opts := CompressOptions{
		MaxTokens:    4096,
		Priority:     "high",
		PreserveKeys: []string{"id", "name"},
	}

	// Verify CompressType enum values
	if CompressTypeUnknown != 0 {
		t.Errorf("CompressTypeUnknown = %d, want 0", CompressTypeUnknown)
	}
	if CompressTypeTruncate != 1 {
		t.Errorf("CompressTypeTruncate = %d, want 1", CompressTypeTruncate)
	}
	if CompressTypeSummarize != 2 {
		t.Errorf("CompressTypeSummarize = %d, want 2", CompressTypeSummarize)
	}
	if CompressTypeDrop != 3 {
		t.Errorf("CompressTypeDrop = %d, want 3", CompressTypeDrop)
	}

	_ = opts // suppress unused warning
}

// =============================================================================
// BenchmarkCompress — benchmark skeleton (RED: will benchmark empty stub)
// =============================================================================

func BenchmarkCompress(b *testing.B) {
	b.ReportAllocs()

	mock := &mockCompressor{name: "bench", typ: CompressTypeTruncate}
	opts := CompressOptions{MaxTokens: 4096, Priority: "normal"}
	data := []byte(`{"key":"value","nested":{"deep":true}}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = mock.Compress(context.Background(), data, opts)
	}
}
