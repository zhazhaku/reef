package lht

import (
	"strings"
	"testing"
)

// ===================== T4B.1.1: StallTracker — 停滞计数器 =====================

func TestStallTrackerIncrementOnNoProgress(t *testing.T) {
	tracker := NewStallTracker(3)
	task := &TaskNode{TaskID: "t1", StallCount: 0}

	// 连续 3 轮无进展，stall_count 应递增
	for i := 0; i < 3; i++ {
		tracker.RecordNoProgress(task)
	}
	if task.StallCount != 3 {
		t.Fatalf("expected stall_count=3, got %d", task.StallCount)
	}
}

func TestStallTrackerResetOnProgress(t *testing.T) {
	tracker := NewStallTracker(3)
	task := &TaskNode{TaskID: "t1", StallCount: 2}

	// 有进展时重置 stall_count
	tracker.RecordProgress(task)
	if task.StallCount != 0 {
		t.Fatalf("expected stall_count=0 after progress, got %d", task.StallCount)
	}
}

func TestStallTrackerIsStalledAtK(t *testing.T) {
	tracker := NewStallTracker(3)
	task := &TaskNode{TaskID: "t1", StallCount: 3}

	if !tracker.IsStalled(task) {
		t.Fatal("expected IsStalled=true when stall_count >= K(3)")
	}

	task.StallCount = 2
	if tracker.IsStalled(task) {
		t.Fatal("expected IsStalled=false when stall_count < K(3)")
	}
}

func TestStallTrackerCustomK(t *testing.T) {
	tracker := NewStallTracker(5)
	task := &TaskNode{TaskID: "t1", StallCount: 4}

	if tracker.IsStalled(task) {
		t.Fatal("expected IsStalled=false with K=5 and stall_count=4")
	}

	task.StallCount = 5
	if !tracker.IsStalled(task) {
		t.Fatal("expected IsStalled=true with K=5 and stall_count=5")
	}
}

func TestStallTrackerDefaultK(t *testing.T) {
	tracker := NewStallTracker(0) // 0 means use default K=3
	if tracker.K != MaxStallRounds {
		t.Fatalf("expected default K=%d, got %d", MaxStallRounds, tracker.K)
	}
}

// ===================== T4B.1.2: 停滞触发 → 升级求助 =====================

func TestAntiStallCheckTriggersEscalationOnStall(t *testing.T) {
	goal := &Goal{GoalID: "g-stall", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-stall",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 3, RepairCount: 0, StrategySwitchCount: 0},
		},
	}
	budget := &Budget{
		GoalID:    "g-stall",
		MaxRounds: 10,
		UsedRounds: 2,
		MaxTokens: 50000,
		UsedTokens: 10000,
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)

	if !result.ShouldEscalate {
		t.Fatal("expected ShouldEscalate=true when stall_count >= K")
	}
	if result.Reason != ReasonStall {
		t.Fatalf("expected reason=%s, got %s", ReasonStall, result.Reason)
	}
}

func TestAntiStallCheckNoEscalationWhenProgressing(t *testing.T) {
	goal := &Goal{GoalID: "g-ok", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-ok",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 0, RepairCount: 1, StrategySwitchCount: 0},
		},
	}
	budget := &Budget{
		GoalID:    "g-ok",
		MaxRounds: 10,
		UsedRounds: 2,
		MaxTokens: 50000,
		UsedTokens: 10000,
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)
	if result.ShouldEscalate {
		t.Fatal("expected no escalation when all counters are within limits")
	}
}

// ===================== T4B.1.3: 预算硬上限 =====================

func TestAntiStallCheckBudgetExhausted(t *testing.T) {
	goal := &Goal{GoalID: "g-budget", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-budget",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 0},
		},
	}
	budget := &Budget{
		GoalID:    "g-budget",
		MaxRounds: 10,
		UsedRounds: 10, // exhausted
		MaxTokens: 50000,
		UsedTokens: 10000,
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)
	if !result.ShouldEscalate {
		t.Fatal("expected ShouldEscalate=true when budget is exhausted")
	}
	if result.Reason != ReasonBudgetExhausted {
		t.Fatalf("expected reason=%s, got %s", ReasonBudgetExhausted, result.Reason)
	}
}

func TestAntiStallCheckBudgetTokenExhausted(t *testing.T) {
	goal := &Goal{GoalID: "g-budget-tok", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-budget-tok",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 0},
		},
	}
	budget := &Budget{
		GoalID:    "g-budget-tok",
		MaxRounds: 10,
		UsedRounds: 3,
		MaxTokens: 50000,
		UsedTokens: 50000, // token exhausted
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)
	if !result.ShouldEscalate {
		t.Fatal("expected ShouldEscalate=true when token budget is exhausted")
	}
}

func TestAntiStallCheckBudgetNotExhausted(t *testing.T) {
	goal := &Goal{GoalID: "g-ok2", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-ok2",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 0},
		},
	}
	budget := &Budget{
		GoalID:    "g-ok2",
		MaxRounds: 10,
		UsedRounds: 5,
		MaxTokens: 50000,
		UsedTokens: 20000,
	}
	if budget.IsExhausted() {
		t.Fatal("budget should not be exhausted")
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)
	if result.ShouldEscalate {
		t.Fatal("expected no escalation when budget is not exhausted")
	}
}

// ===================== T4B.1.4: 求助报告生成 =====================

func TestGenerateHelpReportContainsContext(t *testing.T) {
	goal := &Goal{
		GoalID:      "g-report",
		Description: "test task",
		State:       StateEscalated,
	}
	plan := &Plan{
		GoalID: "g-report",
		Tasks: []TaskNode{
			{TaskID: "t1", Description: "subtask A", StallCount: 3, RepairCount: 3, StrategySwitchCount: 2},
			{TaskID: "t2", Description: "subtask B", StallCount: 1, RepairCount: 0, StrategySwitchCount: 0},
		},
	}
	budget := &Budget{
		GoalID:    "g-report",
		MaxRounds: 10,
		UsedRounds: 8,
		MaxTokens: 50000,
		UsedTokens: 45000,
	}
	taskErrors := map[string]string{
		"t1": "evaluator FAIL: output mismatch",
		"t2": "timeout",
	}

	report := GenerateHelpReport(goal, plan, budget, taskErrors)

	// 验证包含必要字段
	if report.GoalID != "g-report" {
		t.Fatalf("expected GoalID=g-report, got %s", report.GoalID)
	}
	if report.State != StateEscalated {
		t.Fatalf("expected State=ESCALATED, got %s", report.State)
	}
	if report.StallRounds != 3 {
		t.Fatalf("expected StallRounds=3, got %d", report.StallRounds)
	}
	if report.RepairCount != 3 {
		t.Fatalf("expected RepairCount=3, got %d", report.RepairCount)
	}
	if report.StrategySwitchCount != 2 {
		t.Fatalf("expected StrategySwitchCount=2, got %d", report.StrategySwitchCount)
	}
	if !strings.Contains(report.ErrorSummary, "evaluator FAIL") {
		t.Fatalf("expected ErrorSummary to contain 'evaluator FAIL', got: %s", report.ErrorSummary)
	}
	if !strings.Contains(report.ErrorSummary, "timeout") {
		t.Fatalf("expected ErrorSummary to contain 'timeout', got: %s", report.ErrorSummary)
	}
	if !strings.Contains(report.BudgetUsed, "8/10") {
		t.Fatalf("expected BudgetUsed to contain '8/10', got: %s", report.BudgetUsed)
	}
	if report.Suggestion == "" {
		t.Fatal("expected non-empty Suggestion")
	}
}

func TestGenerateHelpReportEmptyErrors(t *testing.T) {
	goal := &Goal{GoalID: "g-empty", State: StateEscalated}
	plan := &Plan{GoalID: "g-empty", Tasks: []TaskNode{}}
	budget := &Budget{GoalID: "g-empty"}

	report := GenerateHelpReport(goal, plan, budget, nil)
	if report.GoalID != "g-empty" {
		t.Fatalf("expected GoalID=g-empty, got %s", report.GoalID)
	}
	if report.ErrorSummary == "" {
		// 无错误时也应该有占位信息
		t.Log("ErrorSummary is empty (no errors to report)")
	}
}

// ===================== T4B.1.5: 交互循环上限 =====================

func TestAntiStallCheckRepairExhaustedTriggersStrategySwitch(t *testing.T) {
	goal := &Goal{GoalID: "g-repair", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-repair",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 0, RepairCount: 3, StrategySwitchCount: 1},
		},
	}
	budget := &Budget{
		GoalID:    "g-repair",
		MaxRounds: 10,
		UsedRounds: 5,
		MaxTokens: 50000,
		UsedTokens: 20000,
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)
	// 修复耗尽但策略切换未耗尽 → 不应升级，应切换策略
	if result.ShouldEscalate {
		t.Fatal("repair exhausted but strategy switch not exhausted yet — should not escalate")
	}
	// 应提示需要切换策略
	if result.SuggestedAction != ActionSwitchStrategy {
		t.Fatalf("expected SuggestedAction=switch_strategy, got %s", result.SuggestedAction)
	}
}

func TestAntiStallCheckStrategySwitchExhaustedEscalates(t *testing.T) {
	goal := &Goal{GoalID: "g-switch", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-switch",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 1, RepairCount: 3, StrategySwitchCount: 2},
		},
	}
	budget := &Budget{
		GoalID:    "g-switch",
		MaxRounds: 10,
		UsedRounds: 7,
		MaxTokens: 50000,
		UsedTokens: 40000,
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)
	if !result.ShouldEscalate {
		t.Fatal("strategy switch exhausted (M=2) AND repair exhausted (N=3) — should escalate")
	}
	if result.Reason != ReasonStrategyExhausted {
		t.Fatalf("expected reason=%s, got %s", ReasonStrategyExhausted, result.Reason)
	}
}

func TestAntiStallCheckAllCountersWithinLimits(t *testing.T) {
	goal := &Goal{GoalID: "g-ok3", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-ok3",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 1, RepairCount: 1, StrategySwitchCount: 0},
			{TaskID: "t2", StallCount: 0, RepairCount: 2, StrategySwitchCount: 1},
		},
	}
	budget := &Budget{
		GoalID:    "g-ok3",
		MaxRounds: 10,
		UsedRounds: 3,
		MaxTokens: 50000,
		UsedTokens: 15000,
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)
	if result.ShouldEscalate {
		t.Fatalf("expected no escalation when all counters within limits, got reason=%s", result.Reason)
	}
}

func TestAntiStallCheckMultipleTaskNodesWorstCase(t *testing.T) {
	// 取所有 TaskNode 中计数器最大值来判断
	goal := &Goal{GoalID: "g-multi", State: StateExecuting}
	plan := &Plan{
		GoalID: "g-multi",
		Tasks: []TaskNode{
			{TaskID: "t1", StallCount: 0, RepairCount: 0, StrategySwitchCount: 0},
			{TaskID: "t2", StallCount: 3, RepairCount: 3, StrategySwitchCount: 2}, // worst case
		},
	}
	budget := &Budget{
		GoalID:    "g-multi",
		MaxRounds: 10,
		UsedRounds: 5,
		MaxTokens: 50000,
		UsedTokens: 20000,
	}

	cfg := DefaultConfig()
	result := CheckAntiStall(cfg, goal, plan, budget, nil)
	if !result.ShouldEscalate {
		t.Fatal("expected escalation when at least one task node is worst-case exhausted")
	}
}

func TestAntiStallCheckTerminalStatesNoEscalation(t *testing.T) {
	for _, state := range []State{StateCompleted, StateAborted} {
		goal := &Goal{GoalID: "g-term", State: state}
		plan := &Plan{
			GoalID: "g-term",
			Tasks: []TaskNode{
				{TaskID: "t1", StallCount: 3, RepairCount: 3, StrategySwitchCount: 2},
			},
		}
		budget := &Budget{
			GoalID:    "g-term",
			MaxRounds: 10,
			UsedRounds: 10,
		}

		cfg := DefaultConfig()
		result := CheckAntiStall(cfg, goal, plan, budget, nil)
		if result.ShouldEscalate {
			t.Fatalf("expected no escalation from terminal state %s", state)
		}
	}
}

// ===================== T4B.1.6: 求助报告格式化输出 =====================

func TestHelpReportStringFormat(t *testing.T) {
	goal := &Goal{
		GoalID:      "g-fmt",
		Description: "构建 REST API",
		State:       StateEscalated,
	}
	plan := &Plan{
		GoalID: "g-fmt",
		Tasks: []TaskNode{
			{TaskID: "t1", Description: "数据库迁移", StallCount: 3, RepairCount: 3, StrategySwitchCount: 2},
		},
	}
	budget := &Budget{
		GoalID:    "g-fmt",
		MaxRounds: 10,
		UsedRounds: 9,
	}
	taskErrors := map[string]string{"t1": "migration failed: connection refused"}

	report := GenerateHelpReport(goal, plan, budget, taskErrors)
	str := report.String()

	// 验证关键信息出现在格式化输出中
	required := []string{
		"g-fmt",
		"ESCALATED",
		"stall_count=3",
		"repair_count=3",
		"strategy_switch=2",
		"migration failed",
		"9/10",
	}
	for _, r := range required {
		if !strings.Contains(str, r) {
			t.Fatalf("expected report string to contain %q, got:\n%s", r, str)
		}
	}
}

// ===================== T4B.1.7: AntiStallResult 完整性 =====================

func TestAntiStallResultFields(t *testing.T) {
	result := AntiStallResult{
		ShouldEscalate:  true,
		Reason:          ReasonStall,
		SuggestedAction: ActionEscalate,
	}

	if !result.ShouldEscalate {
		t.Fatal("ShouldEscalate should be true")
	}
	if result.Reason != ReasonStall {
		t.Fatalf("expected reason=%s", ReasonStall)
	}
}
