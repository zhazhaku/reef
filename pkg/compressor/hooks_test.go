package compressor

import (
	"context"
	"strings"
	"testing"
)

// =============================================================================
// ContextItem — 上下文单元
// =============================================================================
// A ContextItem represents a single unit in the context assembly pipeline.
// It carries the raw content and an estimated token count, which is used by
// AssembleHook for budget allocation and CompactHook for overflow detection.

// =============================================================================
// Helpers
// =============================================================================

func newTestAssembleHook() *AssembleHook {
	return NewAssembleHook(NewContentRouter())
}

func newTestCompactHook() *CompactHook {
	return NewCompactHook(NewContentRouter())
}

// makeItems creates a slice of ContextItems from plain strings, each with
// a token estimate of len(s)/4 (rough heuristic).
func makeItems(strs ...string) []ContextItem {
	items := make([]ContextItem, len(strs))
	for i, s := range strs {
		items[i] = ContextItem{
			Content: []byte(s),
			Tokens:  len(s) / 4,
		}
	}
	return items
}

// totalTokens sums up the estimated token counts.
func totalTokens(items []ContextItem) int {
	sum := 0
	for _, item := range items {
		sum += item.Tokens
	}
	return sum
}

// itemsContent returns a concatenated view of item contents for debug.
func itemsContent(items []ContextItem) string {
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteString("|")
		}
		b.Write(item.Content)
	}
	return b.String()
}

// =============================================================================
// Assemble Hook Tests
// =============================================================================

func TestAssembleHookBudgetSufficient(t *testing.T) {
	h := newTestAssembleHook()
	ctx := context.Background()
	items := makeItems("hello", "world", "this is fine")
	budget := totalTokens(items) + 100 // plenty of room

	assembled, overflow, err := h.Assemble(ctx, items, budget)
	if err != nil {
		t.Fatalf("Assemble: unexpected error: %v", err)
	}
	if len(overflow) != 0 {
		t.Errorf("overflow: expected 0 items, got %d", len(overflow))
	}
	if len(assembled) != len(items) {
		t.Errorf("assembled: expected %d items, got %d", len(items), len(assembled))
	}
}

func TestAssembleHookBudgetTight(t *testing.T) {
	h := newTestAssembleHook()
	ctx := context.Background()
	items := []ContextItem{
		{Content: []byte("aaa"), Tokens: 10},
		{Content: []byte("bbb"), Tokens: 10},
		{Content: []byte("ccc"), Tokens: 10},
		{Content: []byte("ddd"), Tokens: 10},
		{Content: []byte("eee"), Tokens: 10},
	}
	// Budget fits only 3 items
	budget := 30

	assembled, overflow, err := h.Assemble(ctx, items, budget)
	if err != nil {
		t.Fatalf("Assemble: unexpected error: %v", err)
	}
	if len(assembled)+len(overflow) != len(items) {
		t.Errorf("total: assembled(%d)+overflow(%d) != original(%d)", len(assembled), len(overflow), len(items))
	}
	if len(overflow) < 2 {
		t.Errorf("overflow: expected at least 2 items overflowed, got %d", len(overflow))
	}
	// Assembled items should all be within total budget
	if totalTokens(assembled) > budget {
		t.Errorf("budget exceeded: assembled %d tokens, budget %d", totalTokens(assembled), budget)
	}
}

func TestAssembleHookEmptyItems(t *testing.T) {
	h := newTestAssembleHook()
	ctx := context.Background()

	assembled, overflow, err := h.Assemble(ctx, nil, 100)
	if err != nil {
		t.Fatalf("Assemble empty: unexpected error: %v", err)
	}
	if len(assembled) != 0 || len(overflow) != 0 {
		t.Error("Assemble empty: expected zero assembled and zero overflow")
	}
}

func TestAssembleHookZeroBudget(t *testing.T) {
	h := newTestAssembleHook()
	ctx := context.Background()
	items := []ContextItem{
		{Content: []byte("one"), Tokens: 5},
		{Content: []byte("two"), Tokens: 5},
		{Content: []byte("three"), Tokens: 5},
	}

	assembled, overflow, err := h.Assemble(ctx, items, 0)
	if err != nil {
		t.Fatalf("Assemble zero budget: unexpected error: %v", err)
	}
	if len(assembled) != 0 {
		t.Errorf("zero budget: expected 0 assembled, got %d", len(assembled))
	}
	if len(overflow) != len(items) {
		t.Errorf("zero budget: expected all items in overflow, got %d", len(overflow))
	}
}

func TestAssembleHookSingleItemExceedsBudget(t *testing.T) {
	h := newTestAssembleHook()
	ctx := context.Background()
	// A giant item that alone exceeds budget
	bigStr := strings.Repeat("x", 1000)
	items := makeItems(bigStr)
	itemTokens := len(bigStr) / 4 // 250
	budget := itemTokens / 2      // 125 — half what's needed

	assembled, overflow, err := h.Assemble(ctx, items, budget)
	if err != nil {
		t.Fatalf("Assemble big item: unexpected error: %v", err)
	}
	// Even a single big item goes to overflow if it can't fit
	if len(assembled)+len(overflow) != 1 {
		t.Errorf("expected 1 total item, assembled(%d)+overflow(%d)", len(assembled), len(overflow))
	}
}

// =============================================================================
// Compact Hook Tests
// =============================================================================

func TestCompactHookWithinBudget(t *testing.T) {
	h := newTestCompactHook()
	ctx := context.Background()
	items := makeItems("short", "also short", "tiny")
	budget := totalTokens(items) + 100

	compacted, err := h.Compact(ctx, items, budget)
	if err != nil {
		t.Fatalf("Compact: unexpected error: %v", err)
	}
	if len(compacted) != len(items) {
		t.Errorf("Compact: expected %d items, got %d", len(items), len(compacted))
	}
	// Within budget should preserve original content
	for i := range items {
		if string(compacted[i].Content) != string(items[i].Content) {
			t.Errorf("Compact: item[%d] modified unnecessarily", i)
		}
	}
}

func TestCompactHookOverBudget(t *testing.T) {
	h := newTestCompactHook()
	ctx := context.Background()

	// Create items where total exceeds budget
	items := makeItems(
		strings.Repeat("data-", 100), // 500 bytes, ~125 tokens
		strings.Repeat("log-", 100),  // 400 bytes, ~100 tokens
		strings.Repeat("msg-", 100),  // 400 bytes, ~100 tokens
	)
	budget := totalTokens(items) / 2 // only half fits

	compacted, err := h.Compact(ctx, items, budget)
	if err != nil {
		t.Fatalf("Compact over budget: unexpected error: %v", err)
	}
	// Compacted total should be <= budget
	compactedTokens := totalTokens(compacted)
	if compactedTokens > budget {
		t.Errorf("Compact: compacted tokens %d > budget %d", compactedTokens, budget)
	}
}

func TestCompactHookEmptyItems(t *testing.T) {
	h := newTestCompactHook()
	ctx := context.Background()

	compacted, err := h.Compact(ctx, nil, 100)
	if err != nil {
		t.Fatalf("Compact empty: unexpected error: %v", err)
	}
	if len(compacted) != 0 {
		t.Errorf("Compact empty: expected 0 items, got %d", len(compacted))
	}
}

func TestCompactHookZeroBudget(t *testing.T) {
	h := newTestCompactHook()
	ctx := context.Background()
	items := makeItems("some-content", "more-stuff")

	compacted, err := h.Compact(ctx, items, 0)
	if err != nil {
		t.Fatalf("Compact zero budget: unexpected error: %v", err)
	}
	// Zero budget should aggressively compact: remove all item content
	for i, item := range compacted {
		if len(item.Content) > 0 {
			t.Errorf("Compact zero: item[%d] still has %d bytes", i, len(item.Content))
		}
	}
}

func TestCompactHookLargeSingleItem(t *testing.T) {
	h := newTestCompactHook()
	ctx := context.Background()

	big := strings.Repeat("ABCDEFGHIJ", 200) // 2000 bytes, ~500 tokens
	items := makeItems(big)
	budget := len(big)/4 / 4 // 125 tokens — a quarter of the item

	compacted, err := h.Compact(ctx, items, budget)
	if err != nil {
		t.Fatalf("Compact large: unexpected error: %v", err)
	}
	if len(compacted) != 1 {
		t.Fatalf("Compact large: expected 1 item, got %d", len(compacted))
	}
	// Large item should be aggressively truncated
	if len(compacted[0].Content) >= len(big) {
		t.Errorf("Compact large: item should be shorter than original (%d vs %d)", len(compacted[0].Content), len(big))
	}
}

// =============================================================================
// Assemble → Compact Chain Test
// =============================================================================

func TestAssembleCompactChain(t *testing.T) {
	assembleHook := newTestAssembleHook()
	compactHook := newTestCompactHook()
	ctx := context.Background()

	items := makeItems(
		"hello world",
		"this is a longer message with many words",
		"short",
		"another pretty long message that takes space",
		"tiny",
	)
	budget := totalTokens(items) / 2 // tight budget

	// Step 1: Assemble — fill budget and get overflow
	assembled, overflow, err := assembleHook.Assemble(ctx, items, budget)
	if err != nil {
		t.Fatalf("Chain Assemble: %v", err)
	}
	t.Logf("assembled=%d overflow=%d budget=%d", len(assembled), len(overflow), budget)

	// Step 2: Compact — try to squeeze overflow into remaining budget
	if len(overflow) > 0 {
		remainingBudget := budget - totalTokens(assembled)
		if remainingBudget > 0 {
			compacted, err := compactHook.Compact(ctx, overflow, remainingBudget)
			if err != nil {
				t.Fatalf("Chain Compact: %v", err)
			}
			t.Logf("compacted overflow: %d -> %d items", len(overflow), len(compacted))
			// Merge compacted into assembled
			assembled = append(assembled, compacted...)
		}
	}

	// Final assembled items should be within budget (best effort)
	finalTokens := totalTokens(assembled)
	t.Logf("final assembled: %d items, %d tokens (budget %d)", len(assembled), finalTokens, budget)
}
