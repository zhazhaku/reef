// Package compressor — Assemble Hook
//
// AssembleHook injects a token budget into the context assembly pipeline.
// It receives a list of ContextItems and a total token budget, then
// determines which items fit within the budget and which overflow.
//
// Architecture (ADR-002): Hooks are zero-intrusion — if the hook is nil,
// all items pass through without budget enforcement.

package compressor

import (
	"context"
	"fmt"
)

// =============================================================================
// ContextItem — unit of content with estimated token count
// =============================================================================

// ContextItem represents a single content unit in the context assembly
// pipeline. It carries raw bytes and an estimated token count that is used
// by AssembleHook for budget allocation and CompactHook for overflow
// detection.
type ContextItem struct {
	Content []byte
	Tokens  int
}

// =============================================================================
// AssembleHook — token-budget injector
// =============================================================================

// AssembleHook distributes a token budget across context items during the
// Assemble phase of the Seahorse pipeline. Items that fit within the budget
// are returned in the "assembled" slice; items that would cause the budget
// to be exceeded are returned in the "overflow" slice.
//
// The hook uses a greedy algorithm: it takes items in order until the
// budget is exhausted, then all remaining items go to overflow.
type AssembleHook struct {
	router *ContentRouter
}

// NewAssembleHook creates an AssembleHook backed by the given ContentRouter.
func NewAssembleHook(router *ContentRouter) *AssembleHook {
	return &AssembleHook{router: router}
}

// Assemble distributes the given token budget across items.  It returns:
//
//   assembled — items that fit within the budget (greedy, in order)
//   overflow  — items that did not fit
//
// A nil or empty items slice results in empty assembled and overflow with
// no error. A zero budget puts all items into overflow.
func (h *AssembleHook) Assemble(ctx context.Context, items []ContextItem, budget int) ([]ContextItem, []ContextItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	if budget < 0 {
		return nil, nil, fmt.Errorf("assemble_hook: negative budget %d", budget)
	}

	if len(items) == 0 {
		return nil, nil, nil
	}

	// Greedy allocation: take items until budget exhausted.
	remaining := budget
	splitIdx := 0

	for i, item := range items {
		if item.Tokens <= remaining {
			remaining -= item.Tokens
			splitIdx = i + 1
		} else {
			break
		}
	}

	assembled := items[:splitIdx]
	overflow := items[splitIdx:]

	return assembled, overflow, nil
}
