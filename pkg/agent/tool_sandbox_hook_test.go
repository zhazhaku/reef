package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/zhazhaku/reef/pkg/tools"
)

// mockContextManager implements ContextManager for testing.
type mockContextManager struct {
	mu       sync.Mutex
	ingested []IngestRequest
}

func (m *mockContextManager) Assemble(ctx context.Context, req *AssembleRequest) (*AssembleResponse, error) {
	return &AssembleResponse{}, nil
}

func (m *mockContextManager) Compact(ctx context.Context, req *CompactRequest) error {
	return nil
}

func (m *mockContextManager) Ingest(ctx context.Context, req *IngestRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ingested = append(m.ingested, *req)
	return nil
}

func (m *mockContextManager) Clear(ctx context.Context, sessionKey string) error {
	return nil
}

func (m *mockContextManager) lastIngested() *IngestRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.ingested) == 0 {
		return nil
	}
	return &m.ingested[len(m.ingested)-1]
}

// mockToolResult creates a basic ToolResult with ForLLM content.
func mockToolResult(content string) *tools.ToolResult {
	return &tools.ToolResult{ForLLM: content}
}

// ============================================================
// L3.4 Tests: ToolSandboxHook
// ============================================================

// TestSandboxHook_BeforeToolPassThrough verifies BeforeTool is a no-op
// that returns the request unchanged with continue action.
func TestSandboxHook_BeforeToolPassThrough(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	req := &ToolCallHookRequest{
		Tool:      "exec",
		Arguments: map[string]any{"command": "ls"},
	}

	result, decision, err := hook.BeforeTool(context.Background(), req)

	if err != nil {
		t.Fatalf("BeforeTool returned error: %v", err)
	}
	if decision.Action != HookActionContinue {
		t.Errorf("expected continue, got %v", decision.Action)
	}
	if result != req {
		t.Error("expected same request pointer")
	}
}

// TestSandboxHook_AfterToolNilResult verifies nil result passes through.
func TestSandboxHook_AfterToolNilResult(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	result, decision, err := hook.AfterTool(context.Background(), nil)

	if err != nil {
		t.Fatalf("AfterTool returned error: %v", err)
	}
	if decision.Action != HookActionContinue {
		t.Errorf("expected continue, got %v", decision.Action)
	}
	if result != nil {
		t.Error("expected nil result")
	}
}

// TestSandboxHook_AfterToolNilToolResult verifies nil Result field passes through.
func TestSandboxHook_AfterToolNilToolResult(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	resp := &ToolResultHookResponse{
		Tool:   "exec",
		Result: nil,
	}

	_, decision, err := hook.AfterTool(context.Background(), resp)

	if err != nil {
		t.Fatalf("AfterTool returned error: %v", err)
	}
	if decision.Action != HookActionContinue {
		t.Errorf("expected continue, got %v", decision.Action)
	}
	// result is nil as expected, just verify no panic.
}

// TestSandboxHook_NonExecToolPassThrough verifies non-exec tools are not sandboxed.
func TestSandboxHook_NonExecToolPassThrough(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	longOutput := strings.Repeat("x", 5000)
	tools := []string{"web_fetch", "web_search", "write_file", "read_file", "short_grep", "message"}

	for _, toolName := range tools {
		resp := &ToolResultHookResponse{
			Tool:   toolName,
			Result: mockToolResult(longOutput),
			Meta: EventMeta{
				SessionKey: "test-session",
				TurnID:     "turn-1",
			},
		}

		result, decision, err := hook.AfterTool(context.Background(), resp)

		if err != nil {
			t.Errorf("AfterTool for %s returned error: %v", toolName, err)
			continue
		}
		if decision.Action != HookActionContinue {
			t.Errorf("expected continue for %s, got %v", toolName, decision.Action)
		}
		if result.Result.ForLLM != longOutput {
			t.Errorf("output should be unchanged for %s", toolName)
		}
	}
}

// TestSandboxHook_ExecSmallOutputPassThrough verifies exec with small output passes through.
func TestSandboxHook_ExecSmallOutputPassThrough(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	smallOutput := strings.Repeat("x", 1000) // below 2000 threshold

	resp := &ToolResultHookResponse{
		Tool:   "exec",
		Result: mockToolResult(smallOutput),
		Meta: EventMeta{
			SessionKey: "test-session",
			TurnID:     "turn-1",
		},
	}

	result, decision, err := hook.AfterTool(context.Background(), resp)

	if err != nil {
		t.Fatalf("AfterTool returned error: %v", err)
	}
	if decision.Action != HookActionContinue {
		t.Errorf("expected continue, got %v", decision.Action)
	}
	if result.Result.ForLLM != smallOutput {
		t.Error("small output should be unchanged")
	}
}

// TestSandboxHook_ExecLargeOutputSandboxed verifies exec with large output is sandboxed.
func TestSandboxHook_ExecLargeOutputSandboxed(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	largeOutput := "HEAD: beginning of output\n" +
		strings.Repeat("middle content line\n", 300) +
		"TAIL: end of output data here"

	resp := &ToolResultHookResponse{
		Tool:   "exec",
		Result: mockToolResult(largeOutput),
		Meta: EventMeta{
			SessionKey: "test-session",
			TurnID:     "turn-exec-1",
		},
	}

	result, decision, err := hook.AfterTool(context.Background(), resp)

	if err != nil {
		t.Fatalf("AfterTool returned error: %v", err)
	}
	if decision.Action != HookActionContinue {
		t.Errorf("expected continue, got %v", decision.Action)
	}

	// Check that output was replaced (not the original).
	if result.Result.ForLLM == largeOutput {
		t.Error("output should be replaced with summary")
	}

	summary := result.Result.ForLLM

	// Summary should contain head preview.
	if !strings.Contains(summary, "HEAD: beginning") {
		t.Error("summary should contain head preview")
	}

	// Summary should contain the sandboxed hint.
	if !strings.Contains(summary, "Sandboxed:") {
		t.Error("summary should contain sandboxed hint")
	}
	if !strings.Contains(summary, "short_grep") {
		t.Error("summary should mention short_grep")
	}
	if !strings.Contains(summary, "short_expand") {
		t.Error("summary should mention short_expand")
	}

	// Summary should be shorter than original.
	if len(summary) >= len(largeOutput) {
		t.Errorf("summary (%d chars) should be shorter than original (%d chars)", len(summary), len(largeOutput))
	}

	// Check that full output was ingested.
	ingested := cm.lastIngested()
	if ingested == nil {
		t.Fatal("expected ingest to be called")
	}
	if ingested.SessionKey != "test-session" {
		t.Errorf("ingested session key = %q, want %q", ingested.SessionKey, "test-session")
	}
	if ingested.Message.Role != "tool" {
		t.Errorf("ingested message role = %q, want %q", ingested.Message.Role, "tool")
	}
	if ingested.Message.Content != largeOutput {
		t.Error("ingested message should contain full output")
	}
	if ingested.Message.ToolCallID != "sandbox_exec_turn-exec-1" {
		t.Errorf("ingested ToolCallID = %q", ingested.Message.ToolCallID)
	}
}

// TestSandboxHook_ReefExecuteLargeOutputSandboxed verifies reef_execute is also sandboxed.
func TestSandboxHook_ReefExecuteLargeOutputSandboxed(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	largeOutput := strings.Repeat("reef output\n", 300)

	resp := &ToolResultHookResponse{
		Tool:   "reef_execute",
		Result: mockToolResult(largeOutput),
		Meta: EventMeta{
			SessionKey: "test-session",
			TurnID:     "turn-reef-1",
		},
	}

	result, _, err := hook.AfterTool(context.Background(), resp)

	if err != nil {
		t.Fatalf("AfterTool returned error: %v", err)
	}
	if result.Result.ForLLM == largeOutput {
		t.Error("reef_execute output should be sandboxed")
	}
	if strings.Contains(result.Result.ForLLM, "Sandboxed:") {
		// Good - it was sandboxed.
	}
}

// TestSandboxHook_NoSessionKeyFallback verifies pass-through when session key is missing.
func TestSandboxHook_NoSessionKeyFallback(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	largeOutput := strings.Repeat("no session\n", 300)

	resp := &ToolResultHookResponse{
		Tool:   "exec",
		Result: mockToolResult(largeOutput),
		Meta:   EventMeta{}, // no SessionKey
	}

	result, decision, err := hook.AfterTool(context.Background(), resp)

	if err != nil {
		t.Fatalf("AfterTool returned error: %v", err)
	}
	if decision.Action != HookActionContinue {
		t.Errorf("expected continue, got %v", decision.Action)
	}
	// Should pass through unchanged since no session key.
	if result.Result.ForLLM != largeOutput {
		t.Error("output should be unchanged when session key is missing")
	}
	if cm.lastIngested() != nil {
		t.Error("should not have ingested without session key")
	}
}

// TestSandboxHook_BuildSummaryHeadTail verifies the summary structure.
func TestSandboxHook_BuildSummaryHeadTail(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	fullOutput := "## Header\n\nThis is the beginning of a long output.\n" +
		strings.Repeat("middle section data here\n", 200) +
		"... and this is the tail of the output that we want to see\n"

	summary := hook.buildSummary(fullOutput, "test-id", "exec")

	// Must contain head (first 200 chars).
	if !strings.Contains(summary, "## Header") {
		t.Error("summary should contain head content")
	}

	// Must contain tail (last 500 chars).
	if !strings.Contains(summary, "tail of the output") {
		t.Error("summary should contain tail content")
	}

	// Must contain retrieval hint.
	if !strings.Contains(summary, "Sandboxed:") {
		t.Error("summary should contain sandboxed hint")
	}

	// Summary must be shorter than original.
	if len(summary) >= len(fullOutput) {
		t.Errorf("summary (%d chars) should be shorter than original (%d chars)", len(summary), len(fullOutput))
	}
}

// TestSandboxHook_BuildSummaryShortOutput verifies summary for output shorter than headLen.
func TestSandboxHook_BuildSummaryShortOutput(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	shortOutput := "just 50 chars of output, nothing more here"

	// Even though this is short, buildSummary should handle it gracefully.
	summary := hook.buildSummary(shortOutput, "test-id", "exec")

	if !strings.Contains(summary, shortOutput) {
		t.Error("summary should contain all of short output")
	}
	if !strings.Contains(summary, "Sandboxed:") {
		t.Error("summary should contain sandboxed hint")
	}
}

// TestSandboxHook_DefaultsWithZeroValues verifies defaults kick in for zero values.
func TestSandboxHook_DefaultsWithZeroValues(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(0, 0, cm)

	if hook.sandboxThreshold != 2000 {
		t.Errorf("default threshold = %d, want 2000", hook.sandboxThreshold)
	}
	if hook.keepTail != 500 {
		t.Errorf("default keepTail = %d, want 500", hook.keepTail)
	}
}

// TestSandboxHook_CustomThreshold verifies custom threshold is respected.
func TestSandboxHook_CustomThreshold(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(100, 50, cm)

	if hook.sandboxThreshold != 100 {
		t.Errorf("custom threshold = %d, want 100", hook.sandboxThreshold)
	}

	// Output at 200 chars should trigger sandbox (above 100 threshold).
	output := strings.Repeat("a", 200)
	resp := &ToolResultHookResponse{
		Tool:   "exec",
		Result: mockToolResult(output),
		Meta: EventMeta{
			SessionKey: "test-session",
			TurnID:     "turn-1",
		},
	}

	result, _, _ := hook.AfterTool(context.Background(), resp)
	if result.Result.ForLLM == output {
		t.Error("200-char output should be sandboxed with threshold=100")
	}
}

// TestSandboxHook_GetSessionKeyFromMeta verifies session key extraction from Meta.
func TestSandboxHook_GetSessionKeyFromMeta(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	resp := &ToolResultHookResponse{
		Meta: EventMeta{
			SessionKey: "agent:main:feishu:user1",
		},
	}

	key := hook.getSessionKey(resp)
	if key != "agent:main:feishu:user1" {
		t.Errorf("session key = %q, want %q", key, "agent:main:feishu:user1")
	}
}

// TestSandboxHook_GetSessionKeyFromScope verifies fallback to scope construction.
func TestSandboxHook_GetSessionKeyFromScope(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	resp := &ToolResultHookResponse{
		Meta: EventMeta{}, // No SessionKey
	}

	// Without Context, should return empty.
	if key := hook.getSessionKey(resp); key != "" {
		t.Errorf("expected empty key, got %q", key)
	}
}

// TestSandboxHook_GetToolCallID verifies tool call ID construction.
func TestSandboxHook_GetToolCallID(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	resp := &ToolResultHookResponse{
		Tool: "exec",
		Meta: EventMeta{
			TurnID: "turn-abc-123",
		},
	}

	id := hook.getToolCallID(resp)
	if id != "sandbox_exec_turn-abc-123" {
		t.Errorf("tool call ID = %q, want %q", id, "sandbox_exec_turn-abc-123")
	}
}

// TestSandboxHook_GetToolCallIDFallback verifies fallback when TurnID is empty.
func TestSandboxHook_GetToolCallIDFallback(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	resp := &ToolResultHookResponse{
		Tool: "exec",
		Meta: EventMeta{
			TurnID: "",
		},
	}

	id := hook.getToolCallID(resp)
	if !strings.HasPrefix(id, "sandbox_exec_") {
		t.Errorf("tool call ID should start with sandbox_exec_, got %q", id)
	}
}

// TestSandboxHook_IngestFailureStillSandboxes verifies sandbox still works if ingest fails.
func TestSandboxHook_IngestFailureStillSandboxes(t *testing.T) {
	// Make a mock that fails on Ingest.
	failingCM := &failingContextManager{}
	hook := NewToolSandboxHook(2000, 500, failingCM)

	largeOutput := strings.Repeat("ingest will fail\n", 300)

	resp := &ToolResultHookResponse{
		Tool:   "exec",
		Result: mockToolResult(largeOutput),
		Meta: EventMeta{
			SessionKey: "test-session",
			TurnID:     "turn-1",
		},
	}

	result, _, err := hook.AfterTool(context.Background(), resp)
	if err != nil {
		t.Fatalf("AfterTool should not return error even if ingest fails: %v", err)
	}
	// Should still have been sandboxed (summary returned).
	if result.Result.ForLLM == largeOutput {
		t.Error("output should be sandboxed even if ingest fails")
	}
}

type failingContextManager struct{}

func (f *failingContextManager) Assemble(ctx context.Context, req *AssembleRequest) (*AssembleResponse, error) {
	return &AssembleResponse{}, nil
}
func (f *failingContextManager) Compact(ctx context.Context, req *CompactRequest) error {
	return nil
}
func (f *failingContextManager) Ingest(ctx context.Context, req *IngestRequest) error {
	return context.DeadlineExceeded // any error
}
func (f *failingContextManager) Clear(ctx context.Context, sessionKey string) error {
	return nil
}

// TestSandboxHook_ExactThresholdBoundary verifies behavior at the exact threshold.
func TestSandboxHook_ExactThresholdBoundary(t *testing.T) {
	cm := &mockContextManager{}
	hook := NewToolSandboxHook(2000, 500, cm)

	// Exactly at threshold: should NOT be sandboxed (len <= threshold).
	exactOutput := strings.Repeat("x", 2000)
	resp := &ToolResultHookResponse{
		Tool:   "exec",
		Result: mockToolResult(exactOutput),
		Meta: EventMeta{
			SessionKey: "test-session",
			TurnID:     "turn-1",
		},
	}
	result, _, _ := hook.AfterTool(context.Background(), resp)
	if result.Result.ForLLM != exactOutput {
		t.Error("output at exact threshold should not be sandboxed")
	}

	// One above threshold: SHOULD be sandboxed.
	overOutput := strings.Repeat("x", 2001)
	resp2 := &ToolResultHookResponse{
		Tool:   "exec",
		Result: mockToolResult(overOutput),
		Meta: EventMeta{
			SessionKey: "test-session",
			TurnID:     "turn-2",
		},
	}
	result2, _, _ := hook.AfterTool(context.Background(), resp2)
	if result2.Result.ForLLM == overOutput {
		t.Error("output above threshold should be sandboxed")
	}
}
