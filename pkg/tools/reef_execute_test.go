package tools

import (
	"context"
	"strings"
	"testing"
)

// mockExecTool is a stub for the exec tool used in ReefExecuteTool tests.
type mockExecTool struct {
	output string
	err    bool
}

func (m *mockExecTool) Name() string        { return "exec" }
func (m *mockExecTool) Description() string { return "mock exec" }
func (m *mockExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"run", "list", "poll", "read", "write", "kill", "send-keys"},
			},
			"command": map[string]any{
				"type": "string",
			},
			"timeout": map[string]any{
				"type": "number",
			},
		},
		"required":             []string{"action"},
		"additionalProperties": true,
	}
}
func (m *mockExecTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if m.err {
		return ErrorResult("mock exec error")
	}
	return NewToolResult(m.output)
}

// ============================================================
// L3.1 Tests: ReefExecuteTool
// ============================================================

// TestReefExecuteTool_Name verifies the tool name.
func TestReefExecuteTool_Name(t *testing.T) {
	reg := NewToolRegistry()
	tool := NewReefExecuteTool(reg)
	if tool.Name() != "reef_execute" {
		t.Errorf("expected reef_execute, got %s", tool.Name())
	}
}

// TestReefExecuteTool_Description verifies the description mentions sandboxing.
func TestReefExecuteTool_Description(t *testing.T) {
	reg := NewToolRegistry()
	tool := NewReefExecuteTool(reg)
	desc := tool.Description()
	if !strings.Contains(desc, "sandboxed") {
		t.Error("description should mention sandboxed")
	}
	if !strings.Contains(desc, "short_grep") {
		t.Error("description should mention short_grep")
	}
}

// TestReefExecuteTool_Parameters verifies required parameters.
func TestReefExecuteTool_Parameters(t *testing.T) {
	reg := NewToolRegistry()
	tool := NewReefExecuteTool(reg)
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Errorf("expected type=object, got %v", params["type"])
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties")
	}
	if _, ok := props["command"]; !ok {
		t.Error("missing command parameter")
	}
	if _, ok := props["intent"]; !ok {
		t.Error("missing intent parameter")
	}
	if _, ok := props["timeout_ms"]; !ok {
		t.Error("missing timeout_ms parameter")
	}
	if _, ok := props["keep_tail"]; !ok {
		t.Error("missing keep_tail parameter")
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("missing required array")
	}
	found := false
	for _, r := range required {
		if r == "command" {
			found = true
			break
		}
	}
	if !found {
		t.Error("command should be required")
	}
}

// TestReefExecuteTool_MissingCommand verifies error on missing command.
func TestReefExecuteTool_MissingCommand(t *testing.T) {
	reg := NewToolRegistry()
	tool := NewReefExecuteTool(reg)

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing command")
	}
	if !strings.Contains(result.ForLLM, "missing or empty") {
		t.Errorf("expected missing/empty error, got: %s", result.ForLLM)
	}
}

// TestReefExecuteTool_EmptyCommand verifies error on empty command.
func TestReefExecuteTool_EmptyCommand(t *testing.T) {
	reg := NewToolRegistry()
	tool := NewReefExecuteTool(reg)

	result := tool.Execute(context.Background(), map[string]any{
		"command": "",
	})
	if !result.IsError {
		t.Fatal("expected error for empty command")
	}
}

// TestReefExecuteTool_DelegatesToExec verifies delegation to exec tool.
func TestReefExecuteTool_DelegatesToExec(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockExecTool{output: "file1.txt\nfile2.txt\nfile3.txt"})
	tool := NewReefExecuteTool(reg)

	result := tool.Execute(context.Background(), map[string]any{
		"command": "ls",
	})

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if result.ForLLM != "file1.txt\nfile2.txt\nfile3.txt" {
		t.Errorf("unexpected output: %s", result.ForLLM)
	}
}

// TestReefExecuteTool_WithIntent verifies intent annotation.
func TestReefExecuteTool_WithIntent(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockExecTool{output: "some output"})
	tool := NewReefExecuteTool(reg)

	result := tool.Execute(context.Background(), map[string]any{
		"command": "grep TODO *.go",
		"intent":  "Find all TODOs in the codebase",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "[intent: Find all TODOs in the codebase]") {
		t.Errorf("expected intent annotation, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "some output") {
		t.Error("expected original output")
	}
}

// TestReefExecuteTool_ExecErrorPropagates verifies exec errors propagate.
func TestReefExecuteTool_ExecErrorPropagates(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockExecTool{err: true})
	tool := NewReefExecuteTool(reg)

	result := tool.Execute(context.Background(), map[string]any{
		"command": "bad command",
	})

	if !result.IsError {
		t.Fatal("expected error from exec")
	}
	if result.ForLLM != "mock exec error" {
		t.Errorf("expected mock exec error, got: %s", result.ForLLM)
	}
}

// TestReefExecuteTool_TimeoutDefault verifies timeout defaults.
func TestReefExecuteTool_TimeoutDefault(t *testing.T) {
	reg := NewToolRegistry()
	tool := NewReefExecuteTool(reg)

	if tool.timeout.Seconds() != 30 {
		t.Errorf("expected 30s default timeout, got %v", tool.timeout)
	}
}

// TestReefExecuteTool_GetTimeoutFromArgs verifies timeout extraction from args.
func TestReefExecuteTool_GetTimeoutFromArgs(t *testing.T) {
	reg := NewToolRegistry()
	tool := NewReefExecuteTool(reg)

	// Custom timeout in args.
	secs := tool.getTimeout(map[string]any{"timeout_ms": float64(15000)})
	if secs != 15.0 {
		t.Errorf("expected 15.0s, got %v", secs)
	}

	// Default when no timeout_ms.
	secs = tool.getTimeout(map[string]any{})
	if secs != 30.0 {
		t.Errorf("expected 30.0s default, got %v", secs)
	}

	// Zero timeout_ms → use default.
	secs = tool.getTimeout(map[string]any{"timeout_ms": float64(0)})
	if secs != 30.0 {
		t.Errorf("expected 30.0s for zero, got %v", secs)
	}
}
