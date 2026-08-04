package lht

import (
	"fmt"
	"strings"
)

// ===================== StallTracker =====================

// StallTracker tracks consecutive rounds without substantive progress for a TaskNode.
// When RecordNoProgress is called K times without an intervening RecordProgress, the
// task node is considered stalled.
type StallTracker struct {
	K int // rounds before stall trigger (default MaxStallRounds=3, configurable)
}

// NewStallTracker creates a StallTracker with the given K value.
// If k <= 0, the default MaxStallRounds (3) is used.
func NewStallTracker(k int) *StallTracker {
	if k <= 0 {
		k = MaxStallRounds
	}
	return &StallTracker{K: k}
}

// RecordNoProgress increments the stall counter on the task node.
func (s *StallTracker) RecordNoProgress(t *TaskNode) {
	t.StallCount++
}

// RecordProgress resets the stall counter — substantive progress was detected.
func (s *StallTracker) RecordProgress(t *TaskNode) {
	t.StallCount = 0
}

// IsStalled returns true when the task has exceeded the stall threshold K.
func (s *StallTracker) IsStalled(t *TaskNode) bool {
	return t.StallCount >= s.K
}

// ===================== Escalation Reasons =====================

// EscalationReason describes why a task should be escalated.
type EscalationReason string

const (
	ReasonNone            EscalationReason = ""
	ReasonStall           EscalationReason = "stall"
	ReasonBudgetExhausted EscalationReason = "budget_exhausted"
	ReasonStrategyExhausted EscalationReason = "strategy_exhausted"
	ReasonRepairExhausted   EscalationReason = "repair_exhausted"
)

// SuggestedAction describes the recommended action from an anti-stall check.
type SuggestedAction string

const (
	ActionNone           SuggestedAction = ""
	ActionContinue       SuggestedAction = "continue"
	ActionSwitchStrategy SuggestedAction = "switch_strategy"
	ActionEscalate       SuggestedAction = "escalate"
	ActionPause          SuggestedAction = "pause"
)

// ===================== AntiStallResult =====================

// AntiStallResult encapsulates the outcome of an anti-stall check against
// all task nodes and budget constraints.
type AntiStallResult struct {
	ShouldEscalate  bool             // true when escalation is required
	Reason          EscalationReason // the primary escalation reason
	SuggestedAction SuggestedAction  // recommended next action
	WorstTask       *TaskNode        // the task node that triggered the result (if any)
	Details         string           // human-readable explanation
}

// ===================== HelpReport =====================

// HelpReport is the structured escalation report generated when a task enters
// ESCALATED state. It contains all context needed for human intervention.
type HelpReport struct {
	GoalID              string `json:"goal_id"`
	Description         string `json:"description"`
	State               State  `json:"state"`
	StallRounds         int    `json:"stall_rounds"`
	RepairCount         int    `json:"repair_count"`
	StrategySwitchCount int    `json:"strategy_switch_count"`
	ErrorSummary        string `json:"error_summary"`
	BudgetUsed          string `json:"budget_used"`
	CompletedTasks      string `json:"completed_tasks"`
	BlockedTasks        string `json:"blocked_tasks"`
	Suggestion          string `json:"suggestion"`
}

// String formats the HelpReport as a human-readable string suitable for
// sending as a notification message.
func (r *HelpReport) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("【求助报告】任务 %s\n", r.GoalID))
	b.WriteString(fmt.Sprintf("描述: %s\n", r.Description))
	b.WriteString(fmt.Sprintf("状态: %s\n", r.State))
	b.WriteString(fmt.Sprintf("停滞轮次: stall_count=%d\n", r.StallRounds))
	b.WriteString(fmt.Sprintf("修复次数: repair_count=%d\n", r.RepairCount))
	b.WriteString(fmt.Sprintf("策略切换: strategy_switch=%d\n", r.StrategySwitchCount))
	if r.ErrorSummary != "" {
		b.WriteString(fmt.Sprintf("错误摘要: %s\n", r.ErrorSummary))
	}
	if r.BudgetUsed != "" {
		b.WriteString(fmt.Sprintf("预算消耗: %s\n", r.BudgetUsed))
	}
	if r.CompletedTasks != "" {
		b.WriteString(fmt.Sprintf("已完成: %s\n", r.CompletedTasks))
	}
	if r.BlockedTasks != "" {
		b.WriteString(fmt.Sprintf("卡点: %s\n", r.BlockedTasks))
	}
	b.WriteString(fmt.Sprintf("建议: %s", r.Suggestion))
	return b.String()
}

// ===================== CheckAntiStall =====================

// CheckAntiStall performs a comprehensive anti-stall analysis across all
// task nodes and budget. It returns the most severe escalation condition found.
//
// Priority order (most severe first):
//  1. Budget exhausted → escalate
//  2. Stall count ≥ K → escalate
//  3. Strategy switch exhausted (M=2) AND repair exhausted (N=3) → escalate
//  4. Repair exhausted but strategy not exhausted → suggest strategy switch
//  5. All within limits → continue
//
// Terminal states (COMPLETED, ABORTED) are never escalated.
func CheckAntiStall(cfg Config, goal *Goal, plan *Plan, budget *Budget, taskErrors map[string]string) AntiStallResult {
	// Terminal states never need escalation.
	if IsTerminal(goal.State) {
		return AntiStallResult{
			ShouldEscalate:  false,
			Reason:          ReasonNone,
			SuggestedAction: ActionContinue,
			Details:         fmt.Sprintf("task is in terminal state %s", goal.State),
		}
	}

	// 1. Budget exhaustion (highest priority).
	if budget != nil && budget.IsExhausted() {
		return AntiStallResult{
			ShouldEscalate:  true,
			Reason:          ReasonBudgetExhausted,
			SuggestedAction: ActionEscalate,
			Details:         fmt.Sprintf("budget exhausted: rounds=%d/%d tokens=%d/%d hours=%.1f/%.1f", budget.UsedRounds, budget.MaxRounds, budget.UsedTokens, budget.MaxTokens, budget.UsedHours, budget.MaxDurationHours),
		}
	}

	// Iterate all task nodes to find the worst-case counters.
	var worstStall, worstRepair, worstStrategy int
	var worstStallTask, worstRepairTask, worstStrategyTask *TaskNode

	if plan != nil {
		for i := range plan.Tasks {
			t := &plan.Tasks[i]
			if t.StallCount > worstStall {
				worstStall = t.StallCount
				worstStallTask = t
			}
			if t.RepairCount > worstRepair {
				worstRepair = t.RepairCount
				worstRepairTask = t
			}
			if t.StrategySwitchCount > worstStrategy {
				worstStrategy = t.StrategySwitchCount
				worstStrategyTask = t
			}
		}
	}

	stallK := cfg.MaxStallRounds
	if stallK <= 0 {
		stallK = MaxStallRounds
	}

	// 2. Stall detection: stall_count >= K
	if worstStall >= stallK {
		details := fmt.Sprintf("stall_count=%d >= K=%d on task %s", worstStall, stallK, worstStallTask.TaskID)
		return AntiStallResult{
			ShouldEscalate:  true,
			Reason:          ReasonStall,
			SuggestedAction: ActionEscalate,
			WorstTask:       worstStallTask,
			Details:         details,
		}
	}

	repairN := cfg.MaxRepairRounds
	if repairN <= 0 {
		repairN = MaxRepairRounds
	}
	strategyM := cfg.MaxStrategySwitchCount
	if strategyM <= 0 {
		strategyM = MaxStrategySwitchCount
	}

	// 3. Strategy switch exhausted (M) AND repair exhausted (N) → escalate
	if worstStrategy >= strategyM && worstRepair >= repairN {
		details := fmt.Sprintf("strategy_switch=%d >= M=%d AND repair_count=%d >= N=%d on task %s",
			worstStrategy, strategyM, worstRepair, repairN, worstStrategyTask.TaskID)
		return AntiStallResult{
			ShouldEscalate:  true,
			Reason:          ReasonStrategyExhausted,
			SuggestedAction: ActionEscalate,
			WorstTask:       worstStrategyTask,
			Details:         details,
		}
	}

	// 4. Repair exhausted but strategy not exhausted → suggest strategy switch
	if worstRepair >= repairN && worstStrategy < strategyM {
		details := fmt.Sprintf("repair_count=%d >= N=%d on task %s — suggest strategy switch (used %d/%d)",
			worstRepair, repairN, worstRepairTask.TaskID, worstStrategy, strategyM)
		return AntiStallResult{
			ShouldEscalate:  false,
			Reason:          ReasonRepairExhausted,
			SuggestedAction: ActionSwitchStrategy,
			WorstTask:       worstRepairTask,
			Details:         details,
		}
	}

	// 5. All within limits.
	return AntiStallResult{
		ShouldEscalate:  false,
		Reason:          ReasonNone,
		SuggestedAction: ActionContinue,
		Details:         "all counters within limits",
	}
}

// ===================== GenerateHelpReport =====================

// GenerateHelpReport creates a HelpReport from the current goal, plan, budget,
// and per-task error messages.
//
// The report includes:
//   - Goal ID, description, current state
//   - Worst-case stall/repair/strategy counts across all task nodes
//   - Error summary (concatenated from taskErrors map)
//   - Budget usage summary
//   - Suggested next steps for human intervention
func GenerateHelpReport(goal *Goal, plan *Plan, budget *Budget, taskErrors map[string]string) *HelpReport {
	report := &HelpReport{
		GoalID:      goal.GoalID,
		Description: goal.Description,
		State:       goal.State,
	}

	// Aggregate worst-case counters and task statuses.
	var completedNames, blockedNames []string
	if plan != nil {
		for i := range plan.Tasks {
			t := &plan.Tasks[i]
			if t.StallCount > report.StallRounds {
				report.StallRounds = t.StallCount
			}
			if t.RepairCount > report.RepairCount {
				report.RepairCount = t.RepairCount
			}
			if t.StrategySwitchCount > report.StrategySwitchCount {
				report.StrategySwitchCount = t.StrategySwitchCount
			}

			label := t.TaskID
			if t.Description != "" {
				label = t.Description
			}
			switch t.State {
			case StateCompleted:
				completedNames = append(completedNames, label)
			default:
				if t.StallCount > 0 || t.RepairCount > 0 {
					blockedNames = append(blockedNames, label)
				}
			}
		}
	}

	// Build error summary.
	if taskErrors != nil && len(taskErrors) > 0 {
		var errParts []string
		for tid, msg := range taskErrors {
			errParts = append(errParts, fmt.Sprintf("%s: %s", tid, msg))
		}
		report.ErrorSummary = strings.Join(errParts, "; ")
	} else {
		report.ErrorSummary = "(no specific errors recorded)"
	}

	// Budget usage.
	if budget != nil {
		report.BudgetUsed = fmt.Sprintf("rounds=%d/%d tokens=%d/%d hours=%.1f/%.1f",
			budget.UsedRounds, budget.MaxRounds,
			budget.UsedTokens, budget.MaxTokens,
			budget.UsedHours, budget.MaxDurationHours,
		)
	} else {
		report.BudgetUsed = "(no budget configured)"
	}

	// Completed / blocked tasks.
	if len(completedNames) > 0 {
		report.CompletedTasks = strings.Join(completedNames, ", ")
	} else {
		report.CompletedTasks = "(none)"
	}
	if len(blockedNames) > 0 {
		report.BlockedTasks = strings.Join(blockedNames, ", ")
	} else {
		report.BlockedTasks = "(none)"
	}

	// Generate suggestion.
	report.Suggestion = generateSuggestion(report)

	return report
}

// generateSuggestion creates a human-readable suggestion based on the report data.
func generateSuggestion(r *HelpReport) string {
	var parts []string

	if r.StallRounds >= MaxStallRounds {
		parts = append(parts, "连续停滞超限，建议人工介入分析卡点原因")
	}
	if r.RepairCount >= MaxRepairRounds {
		parts = append(parts, "修复已达上限，需人工提供新方向或放宽要求")
	}
	if r.StrategySwitchCount >= MaxStrategySwitchCount {
		parts = append(parts, "策略切换已耗尽，建议人工指定新策略或降低复杂度")
	}
	if strings.Contains(r.BudgetUsed, "/") && !strings.Contains(r.BudgetUsed, "(no budget") {
		parts = append(parts, "预算接近或已达上限，考虑追加或调整范围")
	}

	if len(parts) == 0 {
		parts = append(parts, "请人工检查任务状态并决定下一步（恢复/修改范围/终止）")
	}

	return strings.Join(parts, "；")
}
