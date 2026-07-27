package commands

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// autoCommand returns the Definition for the /auto command family.
// Auto commands manage the AutoLoopOrchestrator's mode, task queue,
// and execution lifecycle.
func autoCommand() Definition {
	return Definition{
		Name:        "auto",
		Description: "Control automatic/manual loop execution",
		Aliases:     []string{"autotask", "autoloop"},
		SubCommands: []SubCommand{
			{
				Name:        "mode",
				Description: "Display or switch execution mode",
				ArgsUsage:   "[auto|manual|chat|hermes]",
				Handler:     handleAutoMode,
			},
			{
				Name:        "run",
				Description: "Run a single task in auto mode",
				ArgsUsage:   "<instruction>",
				Handler:     handleAutoRun,
			},
			{
				Name:        "step",
				Description: "Execute one step in manual mode",
				Handler:     handleAutoStep,
			},
			{
				Name:        "loop",
				Description: "Loop auto N times or infinite",
				ArgsUsage:   "[count|infinite]",
				Handler:     handleAutoLoop,
			},
			{
				Name:        "stop",
				Description: "Stop the auto orchestrator",
				Handler:     handleAutoStop,
			},
			{
				Name:        "status",
				Description: "Show orchestrator status",
				Handler:     handleAutoStatus,
			},
			{
				Name:        "queue",
				Description: "Show queued messages",
				Handler:     handleAutoQueue,
			},
			{
				Name:        "history",
				Description: "Show recent task history",
				Handler:     handleAutoHistory,
			},
		},
	}
}

// autoNotWired returns true if rt is nil or no auto callbacks are wired.
func autoNotWired(rt *Runtime) bool {
	return rt == nil || (rt.GetAutoStatus == nil && rt.SetAutoMode == nil)
}

//nolint:unparam
func handleAutoMode(_ context.Context, req Request, rt *Runtime) error {
	if autoNotWired(rt) {
		return req.Reply("Auto mode is not available (AutoLoopOrchestrator not wired).")
	}

	tokens := strings.Fields(strings.TrimSpace(req.Text))
	if len(tokens) < 3 {
		// Display current mode
		mode, isActive := "", false
		if rt.GetAutoStatus != nil {
			status := rt.GetAutoStatus()
			if m, ok := status.(map[string]interface{}); ok {
				if v, ok := m["mode"]; ok {
					mode = fmt.Sprintf("%v", v)
				}
				if s, ok := m["state"]; ok {
					isActive = fmt.Sprintf("%v", s) != "idle"
				}
			}
		}
		activeStr := ""
		if isActive {
			activeStr = " (active)"
		}
		return req.Reply(fmt.Sprintf("Current mode: %s%s", mode, activeStr))
	}

	// /auto mode <value> — switch mode
	newMode := strings.ToLower(tokens[2])
	validModes := map[string]bool{
		"auto":   true,
		"manual": true,
		"chat":   true,
		"hermes": true,
	}
	if !validModes[newMode] {
		return req.Reply(fmt.Sprintf("Invalid mode: %s. Valid: auto, manual, chat, hermes", newMode))
	}

	if rt.SetAutoMode == nil {
		return req.Reply("Switching mode is not available (SetAutoMode not wired).")
	}

	old, err := rt.SetAutoMode(newMode)
	if err != nil {
		return req.Reply(fmt.Sprintf("Failed to switch mode: %s", err.Error()))
	}
	return req.Reply(fmt.Sprintf("Switched from %s to %s", old, newMode))
}

//nolint:unparam
func handleAutoRun(_ context.Context, req Request, rt *Runtime) error {
	if autoNotWired(rt) {
		return req.Reply("Auto run is not available (AutoLoopOrchestrator not wired).")
	}

	// Extract instruction after "/auto run "
	text := req.Text
	for _, prefix := range []string{"/autotask run", "/autoloop run", "/auto run"} {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	if text == "" {
		return req.Reply("Usage: /auto run <instruction>")
	}

	if rt.EnqueueAutoMessage == nil {
		return req.Reply("EnqueueAutoMessage not wired.")
	}

	rt.EnqueueAutoMessage(text)
	return req.Reply(fmt.Sprintf("Task queued: %s", truncateForDisplay(text, 60)))
}

//nolint:unparam
func handleAutoStep(_ context.Context, req Request, rt *Runtime) error {
	if autoNotWired(rt) {
		return req.Reply("Auto step is not available (AutoLoopOrchestrator not wired).")
	}
	if rt.RunAutoStep == nil {
		return req.Reply("RunAutoStep not wired.")
	}
	rt.RunAutoStep()
	return req.Reply("Step triggered. Check /auto status for results.")
}

//nolint:unparam
func handleAutoLoop(_ context.Context, req Request, rt *Runtime) error {
	if autoNotWired(rt) {
		return req.Reply("Auto loop is not available (AutoLoopOrchestrator not wired).")
	}

	tokens := strings.Fields(strings.TrimSpace(req.Text))
	if len(tokens) < 3 {
		return req.Reply("Usage: /auto loop <count|infinite>")
	}

	val := strings.ToLower(tokens[2])
	if val == "infinite" || val == "inf" || val == "0" {
		if rt.SetAutoMode == nil {
			return req.Reply("SetAutoMode not wired.")
		}
		rt.SetAutoMode("auto")
		if rt.SetAutoLoopCount != nil {
			rt.SetAutoLoopCount(0) // 0 = infinite
		}
		return req.Reply("Loop mode set to: infinite")
	}

	count, err := strconv.Atoi(val)
	if err != nil || count < 1 {
		return req.Reply(fmt.Sprintf("Invalid count: %s. Use a positive number or 'infinite'.", val))
	}

	if rt.SetAutoMode != nil {
		rt.SetAutoMode("auto")
	}
	if rt.SetAutoLoopCount != nil {
		rt.SetAutoLoopCount(count)
	}
	return req.Reply(fmt.Sprintf("Loop mode set to: %d iteration(s)", count))
}

//nolint:unparam
func handleAutoStop(_ context.Context, req Request, rt *Runtime) error {
	if autoNotWired(rt) {
		return req.Reply("Auto stop is not available (AutoLoopOrchestrator not wired).")
	}
	if rt.StopAuto == nil {
		return req.Reply("StopAuto not wired.")
	}
	rt.StopAuto()
	return req.Reply("Auto orchestrator stopped.")
}

//nolint:unparam
func handleAutoStatus(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.GetAutoStatus == nil {
		return req.Reply("Auto status is not available (GetAutoStatus not wired).")
	}
	s := rt.GetAutoStatus()
	return req.Reply(formatAutoStatus(s))
}

//nolint:unparam
func handleAutoQueue(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.GetAutoQueue == nil {
		return req.Reply("Auto queue not available (GetAutoQueue not wired).")
	}
	queue := rt.GetAutoQueue()
	if len(queue) == 0 {
		return req.Reply("Queue is empty.")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Queue (%d items):\n", len(queue)))
	for i, item := range queue {
		b.WriteString(fmt.Sprintf("  %d. %s\n", i+1, truncateForDisplay(item, 50)))
	}
	return req.Reply(b.String())
}

//nolint:unparam
func handleAutoHistory(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.GetAutoHistory == nil {
		return req.Reply("Auto history not available (GetAutoHistory not wired).")
	}
	history := rt.GetAutoHistory()
	if len(history) == 0 {
		return req.Reply("No task history.")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("History (%d entries):\n", len(history)))
	for i, h := range history {
		b.WriteString(fmt.Sprintf("  %d. [%s] %s\n", i+1, h.Status, truncateForDisplay(h.Instruction, 50)))
	}
	return req.Reply(b.String())
}

// truncateForDisplay shortens a string for CLI display.
func truncateForDisplay(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen+3 {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// formatAutoStatus formats an OrchestratorStatus (received as interface{}) for display.
func formatAutoStatus(s interface{}) string {
	switch v := s.(type) {
	case map[string]interface{}:
		var b strings.Builder
		b.WriteString("Orchestrator Status:\n")

		// Print in a specific order for readability
		order := []string{"mode", "state", "plan_id", "plan_status", "tasks_total",
			"tasks_completed", "tasks_failed", "tasks_running", "tasks_pending",
			"clients_active", "clients_max"}
		seen := make(map[string]bool)
		for _, k := range order {
			if val, ok := v[k]; ok {
				b.WriteString(fmt.Sprintf("  %s: %v\n", k, val))
				seen[k] = true
			}
		}
		// Remaining keys not in order
		for k, val := range v {
			if !seen[k] {
				b.WriteString(fmt.Sprintf("  %s: %v\n", k, val))
			}
		}
		return b.String()
	default:
		return fmt.Sprintf("%+v", s)
	}
}
