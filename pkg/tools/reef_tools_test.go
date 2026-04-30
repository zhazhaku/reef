package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/zhazhaku/reef/pkg/reef"
)

// mockBridge implements reef.ReefBridge for testing.
type mockBridge struct {
	submitFn  func(instruction, role string, skills []string, opts reef.TaskOptions) (string, error)
	queryFn   func(taskID string) (*reef.TaskSnapshot, error)
	statusFn  func() (*reef.SystemStatus, error)
}

func (m *mockBridge) SubmitTask(instruction, role string, skills []string, opts reef.TaskOptions) (string, error) {
	if m.submitFn != nil {
		return m.submitFn(instruction, role, skills, opts)
	}
	return "task-mock-1", nil
}

func (m *mockBridge) QueryTask(taskID string) (*reef.TaskSnapshot, error) {
	if m.queryFn != nil {
		return m.queryFn(taskID)
	}
	return &reef.TaskSnapshot{TaskID: taskID, Status: "Running"}, nil
}

func (m *mockBridge) Status() (*reef.SystemStatus, error) {
	if m.statusFn != nil {
		return m.statusFn()
	}
	return &reef.SystemStatus{ConnectedClients: 2, RunningTasks: 1}, nil
}

func TestReefSubmitTaskTool_Basic(t *testing.T) {
	bridge := &mockBridge{}
	tool := NewReefSubmitTaskTool(bridge)

	if tool.Name() != "reef_submit_task" {
		t.Fatalf("expected reef_submit_task, got %s", tool.Name())
	}

	result := tool.Execute(context.Background(), map[string]any{
		"instruction":   "search the web for Go tutorials",
		"required_role": "executor",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.ForLLM), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed["task_id"] != "task-mock-1" {
		t.Fatalf("expected task-mock-1, got %v", parsed["task_id"])
	}
	if parsed["status"] != "Queued" {
		t.Fatalf("expected Queued, got %v", parsed["status"])
	}
}

func TestReefSubmitTaskTool_DefaultRole(t *testing.T) {
	var capturedRole string
	bridge := &mockBridge{
		submitFn: func(instruction, role string, skills []string, opts reef.TaskOptions) (string, error) {
			capturedRole = role
			return "task-2", nil
		},
	}
	tool := NewReefSubmitTaskTool(bridge)

	result := tool.Execute(context.Background(), map[string]any{
		"instruction": "do something",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if capturedRole != "executor" {
		t.Fatalf("expected default role 'executor', got %q", capturedRole)
	}
}

func TestReefSubmitTaskTool_MissingInstruction(t *testing.T) {
	bridge := &mockBridge{}
	tool := NewReefSubmitTaskTool(bridge)

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing instruction")
	}
}

func TestReefSubmitTaskTool_WithSkills(t *testing.T) {
	var capturedSkills []string
	bridge := &mockBridge{
		submitFn: func(instruction, role string, skills []string, opts reef.TaskOptions) (string, error) {
			capturedSkills = skills
			return "task-3", nil
		},
	}
	tool := NewReefSubmitTaskTool(bridge)

	result := tool.Execute(context.Background(), map[string]any{
		"instruction":     "do something",
		"required_skills": []any{"web_search", "code_execution"},
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if len(capturedSkills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(capturedSkills))
	}
}

func TestReefQueryTaskTool_Basic(t *testing.T) {
	bridge := &mockBridge{}
	tool := NewReefQueryTaskTool(bridge)

	if tool.Name() != "reef_query_task" {
		t.Fatalf("expected reef_query_task, got %s", tool.Name())
	}

	result := tool.Execute(context.Background(), map[string]any{
		"task_id": "task-123",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	var parsed reef.TaskSnapshot
	if err := json.Unmarshal([]byte(result.ForLLM), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.TaskID != "task-123" {
		t.Fatalf("expected task-123, got %s", parsed.TaskID)
	}
}

func TestReefQueryTaskTool_MissingTaskID(t *testing.T) {
	bridge := &mockBridge{}
	tool := NewReefQueryTaskTool(bridge)

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error for missing task_id")
	}
}

func TestReefStatusTool_Basic(t *testing.T) {
	bridge := &mockBridge{}
	tool := NewReefStatusTool(bridge)

	if tool.Name() != "reef_status" {
		t.Fatalf("expected reef_status, got %s", tool.Name())
	}

	result := tool.Execute(context.Background(), map[string]any{})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}

	var parsed reef.SystemStatus
	if err := json.Unmarshal([]byte(result.ForLLM), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.ConnectedClients != 2 {
		t.Fatalf("expected 2 connected clients, got %d", parsed.ConnectedClients)
	}
	if parsed.RunningTasks != 1 {
		t.Fatalf("expected 1 running task, got %d", parsed.RunningTasks)
	}
}

func TestReefStatusTool_Error(t *testing.T) {
	bridge := &mockBridge{
		statusFn: func() (*reef.SystemStatus, error) {
			return nil, assertErr("server down")
		},
	}
	tool := NewReefStatusTool(bridge)

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Fatal("expected error")
	}
}

func TestReefSubmitTaskTool_BridgeError(t *testing.T) {
	bridge := &mockBridge{
		submitFn: func(instruction, role string, skills []string, opts reef.TaskOptions) (string, error) {
			return "", assertErr("no clients available")
		},
	}
	tool := NewReefSubmitTaskTool(bridge)

	result := tool.Execute(context.Background(), map[string]any{
		"instruction": "do something",
	})
	if !result.IsError {
		t.Fatal("expected error")
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
