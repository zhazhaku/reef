package lht

import (
	"encoding/json"
	"testing"
	"time"
)

// C1-02: Data model tests — JSON marshal/unmarshal, defaults, budget exhaustion, plan_version

// --- Goal ---

func TestGoalJSONRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := Goal{
		GoalID:      "goal-001",
		Description: "实现用户登录模块",
		State:       StateGrounding,
		CreatedAt:   now,
		UpdatedAt:   now,
		BudgetRef:   "budget-001",
		PlanRef:     "plan-v1-001",
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal Goal: %v", err)
	}

	var g2 Goal
	if err := json.Unmarshal(data, &g2); err != nil {
		t.Fatalf("unmarshal Goal: %v", err)
	}

	if g2.GoalID != g.GoalID {
		t.Errorf("GoalID: got %q, want %q", g2.GoalID, g.GoalID)
	}
	if g2.Description != g.Description {
		t.Errorf("Description: got %q, want %q", g2.Description, g.Description)
	}
	if g2.State != g.State {
		t.Errorf("State: got %q, want %q", g2.State, g.State)
	}
	if !g2.CreatedAt.Equal(g.CreatedAt) {
		t.Errorf("CreatedAt: got %v, want %v", g2.CreatedAt, g.CreatedAt)
	}
	if !g2.UpdatedAt.Equal(g.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", g2.UpdatedAt, g.UpdatedAt)
	}
	if g2.BudgetRef != g.BudgetRef {
		t.Errorf("BudgetRef: got %q, want %q", g2.BudgetRef, g.BudgetRef)
	}
	if g2.PlanRef != g.PlanRef {
		t.Errorf("PlanRef: got %q, want %q", g2.PlanRef, g.PlanRef)
	}
}

func TestGoalJSONTags(t *testing.T) {
	data := []byte(`{
		"goal_id": "g1",
		"description": "test desc",
		"state": "GROUNDING",
		"created_at": "2026-08-01T10:00:00Z",
		"updated_at": "2026-08-01T10:00:00Z",
		"budget_ref": "b1",
		"plan_ref": "p1"
	}`)

	var g Goal
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("unmarshal Goal from JSON: %v", err)
	}
	if g.GoalID != "g1" {
		t.Errorf("GoalID: got %q, want %q", g.GoalID, "g1")
	}
	if g.State != StateGrounding {
		t.Errorf("State: got %q, want %q", g.State, StateGrounding)
	}
	if g.BudgetRef != "b1" {
		t.Errorf("BudgetRef: got %q, want %q", g.BudgetRef, "b1")
	}
	if g.PlanRef != "p1" {
		t.Errorf("PlanRef: got %q, want %q", g.PlanRef, "p1")
	}
}

// --- TaskNode ---

func TestTaskNodeCounterDefaults(t *testing.T) {
	tn := TaskNode{
		TaskID:      "task-001",
		Description: "编写登录 handler",
		State:       StateGrounding,
	}
	// Counters should default to zero
	if tn.RepairCount != 0 {
		t.Errorf("RepairCount default should be 0, got %d", tn.RepairCount)
	}
	if tn.StrategySwitchCount != 0 {
		t.Errorf("StrategySwitchCount default should be 0, got %d", tn.StrategySwitchCount)
	}
	if tn.StallCount != 0 {
		t.Errorf("StallCount default should be 0, got %d", tn.StallCount)
	}
}

func TestTaskNodeJSONRoundtrip(t *testing.T) {
	tn := TaskNode{
		TaskID:             "task-001",
		Description:        "编写登录 handler",
		State:              StateExecuting,
		Dependencies:       []string{"task-000"},
		RepairCount:        1,
		StrategySwitchCount: 0,
		StallCount:          0,
		MustHaves:          []string{"支持用户名密码登录", "返回 JWT token"},
		Rubric:             "代码通过单元测试，覆盖率 > 80%",
	}

	data, err := json.Marshal(tn)
	if err != nil {
		t.Fatalf("marshal TaskNode: %v", err)
	}

	var tn2 TaskNode
	if err := json.Unmarshal(data, &tn2); err != nil {
		t.Fatalf("unmarshal TaskNode: %v", err)
	}

	if tn2.TaskID != tn.TaskID {
		t.Errorf("TaskID mismatch")
	}
	if tn2.RepairCount != tn.RepairCount {
		t.Errorf("RepairCount: got %d, want %d", tn2.RepairCount, tn.RepairCount)
	}
	if len(tn2.MustHaves) != len(tn.MustHaves) {
		t.Errorf("MustHaves length: got %d, want %d", len(tn2.MustHaves), len(tn.MustHaves))
	}
	if tn2.Rubric != tn.Rubric {
		t.Errorf("Rubric: got %q, want %q", tn2.Rubric, tn.Rubric)
	}
}

func TestTaskNodeJSONTags(t *testing.T) {
	data := []byte(`{
		"task_id": "t1",
		"description": "子任务1",
		"state": "PLANNING",
		"dependencies": ["t0"],
		"repair_count": 2,
		"strategy_switch_count": 1,
		"stall_count":2,
		"must_haves": ["验收1"],
		"rubric": "评分标准"
	}`)

	var tn TaskNode
	if err := json.Unmarshal(data, &tn); err != nil {
		t.Fatalf("unmarshal TaskNode: %v", err)
	}
	if tn.TaskID != "t1" {
		t.Errorf("TaskID: got %q", tn.TaskID)
	}
	if tn.RepairCount != 2 {
		t.Errorf("RepairCount: got %d", tn.RepairCount)
	}
	if tn.StallCount != 2 {
		t.Errorf("StallCount: got %d", tn.StallCount)
	}
	if len(tn.MustHaves) != 1 || tn.MustHaves[0] != "验收1" {
		t.Errorf("MustHaves: got %v", tn.MustHaves)
	}
}

// --- Plan ---

func TestPlanJSONRoundtrip(t *testing.T) {
	p := Plan{
		GoalID:      "goal-001",
		PlanVersion: "v1",
		Tasks: []TaskNode{
			{TaskID: "t1", Description: "子任务1", State: StateGrounding},
			{TaskID: "t2", Description: "子任务2", State: StateGrounding, Dependencies: []string{"t1"}},
		},
		Capabilities: []string{"go", "bash", "github"},
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal Plan: %v", err)
	}

	var p2 Plan
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatalf("unmarshal Plan: %v", err)
	}

	if p2.GoalID != p.GoalID {
		t.Errorf("GoalID mismatch")
	}
	if p2.PlanVersion != "v1" {
		t.Errorf("PlanVersion: got %q, want v1", p2.PlanVersion)
	}
	if len(p2.Tasks) != 2 {
		t.Errorf("Tasks length: got %d, want 2", len(p2.Tasks))
	}
	if len(p2.Capabilities) != 3 {
		t.Errorf("Capabilities length: got %d, want 3", len(p2.Capabilities))
	}
}

func TestPlanVersionFieldExists(t *testing.T) {
	p := Plan{GoalID: "g1", PlanVersion: "v2"}
	if p.PlanVersion != "v2" {
		t.Errorf("PlanVersion should be 'v2'")
	}

	data, _ := json.Marshal(p)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	if _, ok := raw["plan_version"]; !ok {
		t.Error("plan_version JSON field must exist")
	}

	v, ok := raw["plan_version"].(string)
	if !ok || v != "v2" {
		t.Errorf("plan_version value: got %v, want 'v2'", raw["plan_version"])
	}
}

// --- Budget ---

func TestBudgetJSONRoundtrip(t *testing.T) {
	b := Budget{
		GoalID:       "goal-001",
		MaxRounds:    30,
		MaxTokens:    5000000,
		MaxDurationHours: 48,
		UsedRounds:   5,
		UsedTokens:   120000,
		UsedHours:    2.5,
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal Budget: %v", err)
	}

	var b2 Budget
	if err := json.Unmarshal(data, &b2); err != nil {
		t.Fatalf("unmarshal Budget: %v", err)
	}

	if b2.MaxTokens != b.MaxTokens {
		t.Errorf("MaxTokens: got %d, want %d", b2.MaxTokens, b.MaxTokens)
	}
	if b2.UsedTokens != b.UsedTokens {
		t.Errorf("UsedTokens: got %d, want %d", b2.UsedTokens, b.UsedTokens)
	}
}

func TestBudgetExhaustedRounds(t *testing.T) {
	b := Budget{MaxRounds: 10, UsedRounds: 10}
	if !b.IsExhausted() {
		t.Error("Budget should be exhausted when used rounds >= max rounds")
	}
}

func TestBudgetExhaustedTokens(t *testing.T) {
	b := Budget{MaxTokens: 100000, UsedTokens: 100000}
	if !b.IsExhausted() {
		t.Error("Budget should be exhausted when used tokens >= max tokens")
	}
}

func TestBudgetExhaustedHours(t *testing.T) {
	b := Budget{MaxDurationHours: 24, UsedHours: 24}
	if !b.IsExhausted() {
		t.Error("Budget should be exhausted when used hours >= max hours")
	}
}

func TestBudgetNotExhausted(t *testing.T) {
	b := Budget{MaxRounds: 10, UsedRounds: 3, MaxTokens: 100000, UsedTokens: 50000, MaxDurationHours: 24, UsedHours: 10}
	if b.IsExhausted() {
		t.Error("Budget should NOT be exhausted")
	}
}

func TestBudgetZeroMaxNotExhausted(t *testing.T) {
	// If max is 0, it means unlimited / not set, so never exhausted
	b := Budget{MaxRounds: 0, UsedRounds: 999}
	if b.IsExhausted() {
		t.Error("Budget with MaxRounds=0 (unlimited) should not be exhausted")
	}
}

// --- ReviewRecord ---

func TestReviewRecordJSONRoundtrip(t *testing.T) {
	rr := ReviewRecord{
		ReviewRound: 1,
		Reviewer:    "lht-eval",
		Verdict:     "PASS",
		Issues:      []string{"minor: 变量命名可优化"},
		Score:       8.5,
	}

	data, err := json.Marshal(rr)
	if err != nil {
		t.Fatalf("marshal ReviewRecord: %v", err)
	}

	var rr2 ReviewRecord
	if err := json.Unmarshal(data, &rr2); err != nil {
		t.Fatalf("unmarshal ReviewRecord: %v", err)
	}

	if rr2.Reviewer != "lht-eval" {
		t.Errorf("Reviewer: got %q", rr2.Reviewer)
	}
	if rr2.Verdict != "PASS" {
		t.Errorf("Verdict: got %q", rr2.Verdict)
	}
	if rr2.Score != 8.5 {
		t.Errorf("Score: got %f", rr2.Score)
	}
}

func TestReviewRecordVerdicts(t *testing.T) {
	// Ensure PASS and FAIL are valid verdicts
	validVerdicts := map[string]bool{"PASS": true, "FAIL": true}
	if !validVerdicts["PASS"] || !validVerdicts["FAIL"] {
		t.Error("PASS and FAIL should be valid verdicts")
	}
}

// --- UserReply ---

func TestUserReplyJSONRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ur := UserReply{
		ReplyType: "approve",
		Content:   "批准执行，注意安全",
		Timestamp: now,
	}

	data, err := json.Marshal(ur)
	if err != nil {
		t.Fatalf("marshal UserReply: %v", err)
	}

	var ur2 UserReply
	if err := json.Unmarshal(data, &ur2); err != nil {
		t.Fatalf("unmarshal UserReply: %v", err)
	}

	if ur2.ReplyType != "approve" {
		t.Errorf("ReplyType: got %q", ur2.ReplyType)
	}
	if ur2.Content != ur.Content {
		t.Errorf("Content: got %q, want %q", ur2.Content, ur.Content)
	}
	if !ur2.Timestamp.Equal(ur.Timestamp) {
		t.Errorf("Timestamp mismatch")
	}
}

func TestUserReplyTypes(t *testing.T) {
	// 对齐答案/批准/打回意见/终审/终止指令
	types := []string{"answer", "approve", "reject", "final_approve", "abort"}
	for _, rt := range types {
		ur := UserReply{ReplyType: rt, Content: "test"}
		data, _ := json.Marshal(ur)
		var ur2 UserReply
		if err := json.Unmarshal(data, &ur2); err != nil {
			t.Errorf("unmarshal UserReply with type %q: %v", rt, err)
		}
		if ur2.ReplyType != rt {
			t.Errorf("ReplyType roundtrip: got %q, want %q", ur2.ReplyType, rt)
		}
	}
}

// --- Goal new fields (P0-01 / P0-02 / P0-03) ---

func TestGoalNewFieldsRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := Goal{
		GoalID:        "goal-002",
		Description:   "实现支付模块",
		State:         StatePaused,
		CreatedAt:     now,
		UpdatedAt:     now,
		BudgetRef:     "budget-002",
		PlanRef:       "plan-v2-001",
		PreviousState: StateExecuting,
		EscalatedFrom: "",
		PlanVersion:   "v2",
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal Goal with new fields: %v", err)
	}

	var g2 Goal
	if err := json.Unmarshal(data, &g2); err != nil {
		t.Fatalf("unmarshal Goal with new fields: %v", err)
	}

	if g2.PreviousState != StateExecuting {
		t.Errorf("PreviousState: got %q, want %q", g2.PreviousState, StateExecuting)
	}
	if g2.EscalatedFrom != "" {
		t.Errorf("EscalatedFrom should be empty, got %q", g2.EscalatedFrom)
	}
	if g2.PlanVersion != "v2" {
		t.Errorf("PlanVersion: got %q, want %q", g2.PlanVersion, "v2")
	}
}

func TestGoalNewFieldsOmitempty(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	g := Goal{
		GoalID:      "goal-003",
		Description: "minimal goal",
		State:       StateGrounding,
		CreatedAt:   now,
		UpdatedAt:   now,
		BudgetRef:   "b3",
		PlanRef:     "p3",
		// PreviousState, EscalatedFrom, PlanVersion all zero → should be omitted
	}

	data, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Empty/zero fields with omitempty should not appear in JSON
	raw := string(data)
	for _, forbidden := range []string{`"previous_state"`, `"escalated_from"`, `"plan_version"`} {
		if contains(raw, forbidden) {
			t.Errorf("zero-value field %s should be omitted from JSON, got: %s", forbidden, raw)
		}
	}

	// Unmarshal back: zero values should stay zero
	var g2 Goal
	if err := json.Unmarshal(data, &g2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if g2.PreviousState != "" {
		t.Errorf("PreviousState should be zero, got %q", g2.PreviousState)
	}
	if g2.EscalatedFrom != "" {
		t.Errorf("EscalatedFrom should be zero, got %q", g2.EscalatedFrom)
	}
	if g2.PlanVersion != "" {
		t.Errorf("PlanVersion should be zero, got %q", g2.PlanVersion)
	}
}

// --- Budget.IsConfigured (P1-01) ---

func TestBudgetIsConfigured(t *testing.T) {
	// All-zero → unconfigured
	b := Budget{}
	if b.IsConfigured() {
		t.Error("zero-valued Budget should NOT be configured")
	}

	// Any single Max > 0 → configured
	if !(Budget{MaxRounds: 1}.IsConfigured()) {
		t.Error("Budget with MaxRounds=1 should be configured")
	}
	if !(Budget{MaxTokens: 1}.IsConfigured()) {
		t.Error("Budget with MaxTokens=1 should be configured")
	}
	if !(Budget{MaxDurationHours: 1}.IsConfigured()) {
		t.Error("Budget with MaxDurationHours=1 should be configured")
	}
}

func TestBudgetConfiguredNotConfusedWithExhausted(t *testing.T) {
	// Configured but not exhausted
	b := Budget{MaxRounds: 10, UsedRounds: 3}
	if !b.IsConfigured() {
		t.Error("should be configured")
	}
	if b.IsExhausted() {
		t.Error("should not be exhausted at 3/10 rounds")
	}

	// Zero-max with high usage: not configured, never exhausted
	b2 := Budget{MaxRounds: 0, UsedRounds: 9999}
	if b2.IsConfigured() {
		t.Error("zero-max budget should not be configured")
	}
	if b2.IsExhausted() {
		t.Error("zero-max budget should never be exhausted")
	}
}

// --- TaskNode constraint methods (P1-02) ---

func TestTaskNodeRepairExhausted(t *testing.T) {
	tn := TaskNode{}
	if tn.IsRepairExhausted() {
		t.Error("RepairCount=0 should not be exhausted")
	}

	tn.RepairCount = 2
	if tn.IsRepairExhausted() {
		t.Error("RepairCount=2 should not be exhausted (N=3)")
	}

	tn.RepairCount = 3
	if !tn.IsRepairExhausted() {
		t.Error("RepairCount=3 should be exhausted (N=3)")
	}

	tn.RepairCount = 5
	if !tn.IsRepairExhausted() {
		t.Error("RepairCount=5 should be exhausted (>N)")
	}
}

func TestTaskNodeStrategySwitchExhausted(t *testing.T) {
	tn := TaskNode{}
	if tn.IsStrategySwitchExhausted() {
		t.Error("StrategySwitchCount=0 should not be exhausted")
	}

	tn.StrategySwitchCount = 1
	if tn.IsStrategySwitchExhausted() {
		t.Error("StrategySwitchCount=1 should not be exhausted (M=2)")
	}

	tn.StrategySwitchCount = 2
	if !tn.IsStrategySwitchExhausted() {
		t.Error("StrategySwitchCount=2 should be exhausted (M=2)")
	}
}

func TestTaskNodeStallExhausted(t *testing.T) {
	tn := TaskNode{}
	if tn.IsStallExhausted() {
		t.Error("StallCount=0 should not be exhausted")
	}

	tn.StallCount = 2
	if tn.IsStallExhausted() {
		t.Error("StallCount=2 should not be exhausted (K=3)")
	}

	tn.StallCount = 3
	if !tn.IsStallExhausted() {
		t.Error("StallCount=3 should be exhausted (K=3)")
	}
}

// --- ReplyType constants (P1-03) ---

func TestReplyTypeConstants(t *testing.T) {
	tests := []struct {
		constant string
		value    string
	}{
		{ReplyTypeAnswer, "answer"},
		{ReplyTypeApprove, "approve"},
		{ReplyTypeReject, "reject"},
		{ReplyTypeFinalApprove, "final_approve"},
		{ReplyTypeAbort, "abort"},
	}
	for _, tt := range tests {
		if tt.constant != tt.value {
			t.Errorf("constant value mismatch: got %q, want %q", tt.constant, tt.value)
		}
	}
}

// --- Verdict constants (P2-02) ---

func TestVerdictConstants(t *testing.T) {
	tests := []struct {
		constant string
		value    string
	}{
		{VerdictPass, "PASS"},
		{VerdictFail, "FAIL"},
		{VerdictUpgrade, "UPGRADE"},
	}
	for _, tt := range tests {
		if tt.constant != tt.value {
			t.Errorf("constant value mismatch: got %q, want %q", tt.constant, tt.value)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
