// PicoClaw - Ultra-lightweight personal AI agent
//
// Reef coordination tools for the Hermes Coordinator.
// These tools allow the Coordinator AgentLoop to delegate tasks
// to connected clients via the Reef Server.

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/reef"
)

// ---------------------------------------------------------------------------
// ReefSubmitTaskTool — submit a task for delegation
// ---------------------------------------------------------------------------

// ReefSubmitTaskTool submits a task to the Reef Server for execution
// by a connected client. This is the primary delegation tool for the
// Hermes Coordinator.
type ReefSubmitTaskTool struct {
	bridge reef.ReefBridge
}

// NewReefSubmitTaskTool creates a new submit task tool.
func NewReefSubmitTaskTool(bridge reef.ReefBridge) *ReefSubmitTaskTool {
	return &ReefSubmitTaskTool{bridge: bridge}
}

func (t *ReefSubmitTaskTool) Name() string { return "reef_submit_task" }

func (t *ReefSubmitTaskTool) Description() string {
	return "Submit a task to a team member for execution. Use this to delegate complex tasks that require specialized capabilities (web search, code execution, file operations, etc.). The task will be assigned to the best available team member based on their skills and capacity."
}

func (t *ReefSubmitTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"instruction": map[string]any{
				"type":        "string",
				"description": "The task instruction. Be specific and include all necessary context. The executor will follow this instruction directly.",
			},
			"required_role": map[string]any{
				"type":        "string",
				"description": "The role required for this task (e.g., 'executor', 'researcher', 'coder'). Must match a connected client's role.",
				"default":     "executor",
			},
			"required_skills": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Skills required for this task (e.g., ['web_search', 'code_execution']). Used to match against client capabilities.",
			},
			"model_hint": map[string]any{
				"type":        "string",
				"description": "Optional model hint for the executor (e.g., 'gpt-4o', 'claude-3.5-sonnet').",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Maximum execution time in milliseconds. Default: 300000 (5 minutes).",
			},
		},
		"required": []string{"instruction"},
	}
}

func (t *ReefSubmitTaskTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	instruction, _ := args["instruction"].(string)
	if strings.TrimSpace(instruction) == "" {
		return ErrorResult("instruction is required")
	}

	requiredRole, _ := args["required_role"].(string)
	if requiredRole == "" {
		requiredRole = "executor"
	}

	var requiredSkills []string
	if skillsRaw, ok := args["required_skills"]; ok {
		switch v := skillsRaw.(type) {
		case []any:
			for _, s := range v {
				if str, ok := s.(string); ok {
					requiredSkills = append(requiredSkills, str)
				}
			}
		case []string:
			requiredSkills = v
		}
	}

	modelHint, _ := args["model_hint"].(string)

	var timeoutMs int64
	if v, ok := args["timeout_ms"]; ok {
		switch n := v.(type) {
		case float64:
			timeoutMs = int64(n)
		case int64:
			timeoutMs = n
		case int:
			timeoutMs = int64(n)
		}
	}
	if timeoutMs <= 0 {
		timeoutMs = 300_000 // 5 minutes default
	}

	opts := reef.TaskOptions{
		MaxRetries: 2,
		TimeoutMs:  timeoutMs,
		ModelHint:  modelHint,
	}

	taskID, err := t.bridge.SubmitTask(instruction, requiredRole, requiredSkills, opts)
	if err != nil {
		logger.WarnCF("reef", "Failed to submit task",
			map[string]any{"error": err.Error()})
		return ErrorResult(fmt.Sprintf("failed to submit task: %v", err))
	}

	logger.InfoCF("reef", "Task submitted",
		map[string]any{
			"task_id":         taskID,
			"required_role":   requiredRole,
			"required_skills": requiredSkills,
		})

	result := map[string]any{
		"task_id": taskID,
		"status":  "Queued",
		"message": fmt.Sprintf("Task %s submitted successfully. Use reef_query_task to check status.", taskID),
	}
	resultJSON, _ := json.Marshal(result)
	return NewToolResult(string(resultJSON))
}

// ---------------------------------------------------------------------------
// ReefQueryTaskTool — query task status and result
// ---------------------------------------------------------------------------

// ReefQueryTaskTool queries the status and result of a previously
// submitted task.
type ReefQueryTaskTool struct {
	bridge reef.ReefBridge
}

// NewReefQueryTaskTool creates a new query task tool.
func NewReefQueryTaskTool(bridge reef.ReefBridge) *ReefQueryTaskTool {
	return &ReefQueryTaskTool{bridge: bridge}
}

func (t *ReefQueryTaskTool) Name() string { return "reef_query_task" }

func (t *ReefQueryTaskTool) Description() string {
	return "Query the status and result of a previously submitted task. Use this to check if a delegated task has completed and retrieve its result."
}

func (t *ReefQueryTaskTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{
				"type":        "string",
				"description": "The task ID returned by reef_submit_task.",
			},
		},
		"required": []string{"task_id"},
	}
}

func (t *ReefQueryTaskTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	taskID, _ := args["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return ErrorResult("task_id is required")
	}

	snapshot, err := t.bridge.QueryTask(taskID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("query failed: %v", err))
	}

	resultJSON, _ := json.Marshal(snapshot)
	return NewToolResult(string(resultJSON))
}

// ---------------------------------------------------------------------------
// ReefStatusTool — query overall system status
// ---------------------------------------------------------------------------

// ReefStatusTool queries the overall Reef system status including
// connected clients and task counts.
type ReefStatusTool struct {
	bridge reef.ReefBridge
}

// NewReefStatusTool creates a new status tool.
func NewReefStatusTool(bridge reef.ReefBridge) *ReefStatusTool {
	return &ReefStatusTool{bridge: bridge}
}

func (t *ReefStatusTool) Name() string { return "reef_status" }

func (t *ReefStatusTool) Description() string {
	return "Get the overall status of the multi-agent team. Shows connected team members, their skills, capacity, and task statistics. Use this to understand what resources are available before delegating tasks."
}

func (t *ReefStatusTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *ReefStatusTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	status, err := t.bridge.Status()
	if err != nil {
		return ErrorResult(fmt.Sprintf("status query failed: %v", err))
	}

	resultJSON, _ := json.Marshal(status)
	return NewToolResult(string(resultJSON))
}
