// PicoClaw - Ultra-lightweight personal AI agent

package tools

import (
	"context"
	"fmt"
	"testing"
)

// mockTool implements Tool for testing Remove.
type mockTool struct {
	name string
}

func (m *mockTool) Name() string               { return m.name }
func (m *mockTool) Description() string        { return "mock" }
func (m *mockTool) Parameters() map[string]any { return nil }
func (m *mockTool) Execute(_ context.Context, _ map[string]any) *ToolResult {
	return &ToolResult{Output: "ok"}
}

func TestToolRegistry_Remove(t *testing.T) {
	r := NewToolRegistry()

	// Register a tool
	tool := &mockTool{name: "test_tool"}
	r.Register(tool)

	// Verify it's registered
	if _, ok := r.Get("test_tool"); !ok {
		t.Fatal("tool should be registered")
	}
	if r.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", r.Count())
	}

	// Remove it
	if !r.Remove("test_tool") {
		t.Error("Remove should return true for existing tool")
	}

	// Verify it's gone
	if _, ok := r.Get("test_tool"); ok {
		t.Error("tool should be removed")
	}
	if r.Count() != 0 {
		t.Fatalf("Count() = %d, want 0", r.Count())
	}

	// Remove non-existent tool
	if r.Remove("nonexistent") {
		t.Error("Remove should return false for non-existent tool")
	}
}

func TestToolRegistry_Remove_VersionIncrement(t *testing.T) {
	r := NewToolRegistry()
	v1 := r.Version()

	tool := &mockTool{name: "test_tool"}
	r.Register(tool)
	v2 := r.Version()

	if v2 <= v1 {
		t.Error("Register should increment version")
	}

	r.Remove("test_tool")
	v3 := r.Version()

	if v3 <= v2 {
		t.Error("Remove should increment version")
	}
}

func TestToolRegistry_Remove_ConcurrentSafety(t *testing.T) {
	r := NewToolRegistry()

	// Register multiple tools
	for i := 0; i < 10; i++ {
		tool := &mockTool{name: fmt.Sprintf("tool_%d", i)}
		r.Register(tool)
	}

	// Concurrently remove and register
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			r.Remove(fmt.Sprintf("tool_%d", idx))
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic
	_ = r.Count()
	_ = r.List()
}
