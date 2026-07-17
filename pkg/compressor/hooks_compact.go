// Package compressor — Compact Hook
//
// CompactHook performs secondary (more aggressive) compression on context
// items that remain over budget after the Assemble phase.  Where the
// AssembleHook performs budget allocation, the CompactHook performs actual
// content truncation — it is the "last resort" before content is sent to
// the LLM.
//
// Architecture (ADR-002): Hooks are zero-intrusion — if the hook is nil or
// fails, the original items pass through unchanged.

package compressor

import (
	"context"
	"fmt"
)

// =============================================================================
// CompactHook — secondary compression for overflow items
// =============================================================================

// CompactHook applies aggressive content compression to items that would
// otherwise exceed the token budget. It tries to squeeze as many items as
// possible into the budget by truncating content proportionally.
type CompactHook struct {
	router *ContentRouter
}

// NewCompactHook creates a CompactHook backed by the given ContentRouter.
func NewCompactHook(router *ContentRouter) *CompactHook {
	return &CompactHook{router: router}
}

// Compact applies secondary compression to items, attempting to fit them
// within the given token budget. The strategy is:
//
//  1. If all items already fit, return them unchanged.
//  2. Otherwise, proportionally truncate each item's content so that the
//     total token count stays within budget.
//  3. Zero-budget effectively clears all content.
//
// Items with zero tokens are passed through untouched.
func (h *CompactHook) Compact(ctx context.Context, items []ContextItem, budget int) ([]ContextItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	if budget < 0 {
		return nil, fmt.Errorf("compact_hook: negative budget %d", budget)
	}

	total := 0
	for _, item := range items {
		total += item.Tokens
	}

	// All items fit — pass through unchanged
	if total <= budget {
		out := make([]ContextItem, len(items))
		copy(out, items)
		return out, nil
	}

	// Zero budget — empty all content
	if budget <= 0 {
		out := make([]ContextItem, len(items))
		copy(out, items)
		for i := range out {
			out[i].Content = nil
			out[i].Tokens = 0
		}
		return out, nil
	}

	// Proportional truncation:
	//   scaleFactor = budget / total  (e.g. 0.5 means keep half of every item)
	scaleFactor := float64(budget) / float64(total)

	out := make([]ContextItem, len(items))
	for i, item := range items {
		if item.Tokens == 0 || len(item.Content) == 0 {
			out[i] = item
			continue
		}
		// Truncate content proportionally
		newLen := int(float64(len(item.Content)) * scaleFactor)
		if newLen < 1 && len(item.Content) > 0 {
			newLen = 1 // keep at least 1 byte as placeholder
		}
		newTokens := int(float64(item.Tokens) * scaleFactor)
		if newTokens < 1 && item.Tokens > 0 {
			newTokens = 1
		}

		out[i] = ContextItem{
			Content: item.Content[:newLen],
			Tokens:  newTokens,
		}
	}

	return out, nil
}
