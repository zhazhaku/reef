package lht

import (
	"strings"
	"testing"
)

// --- EstimateBudget ---

func TestEstimateBudgetProjectMedium(t *testing.T) {
	input := BudgetEstimationInput{
		GoalID:     "goal-001",
		Category:   CategoryProject,
		TaskCount:  6,
		MaxDepth:   3,
		Complexity: ComplexityMedium,
	}
	bp := EstimateBudget(input)

	if bp.GoalID != "goal-001" {
		t.Errorf("GoalID: got %q, want %q", bp.GoalID, "goal-001")
	}
	if bp.MaxRounds <= 0 {
		t.Errorf("MaxRounds should be positive, got %d", bp.MaxRounds)
	}
	if bp.MaxTokens <= 0 {
		t.Errorf("MaxTokens should be positive, got %d", bp.MaxTokens)
	}
	if bp.MaxDurationHours <= 0 {
		t.Errorf("MaxDurationHours should be positive, got %f", bp.MaxDurationHours)
	}
	if bp.Rationale == "" {
		t.Error("Rationale should not be empty")
	}
}

func TestEstimateBudgetCodeLowComplexity(t *testing.T) {
	input := BudgetEstimationInput{
		GoalID:     "goal-002",
		Category:   CategoryCode,
		TaskCount:  3,
		MaxDepth:   1,
		Complexity: ComplexityLow,
	}
	bp := EstimateBudget(input)

	// Low complexity should produce smaller budget than medium
	inputMedium := input
	inputMedium.Complexity = ComplexityMedium
	bpMedium := EstimateBudget(inputMedium)

	if bp.MaxRounds >= bpMedium.MaxRounds {
		t.Errorf("low complexity budget (%d) should be < medium (%d)", bp.MaxRounds, bpMedium.MaxRounds)
	}
}

func TestEstimateBudgetHighComplexity(t *testing.T) {
	input := BudgetEstimationInput{
		GoalID:     "goal-003",
		Category:   CategoryGeneral,
		TaskCount:  5,
		MaxDepth:   4,
		Complexity: ComplexityHigh,
	}
	bp := EstimateBudget(input)

	inputMedium := input
	inputMedium.Complexity = ComplexityMedium
	bpMedium := EstimateBudget(inputMedium)

	if bp.MaxRounds <= bpMedium.MaxRounds {
		t.Errorf("high complexity budget (%d) should be > medium (%d)", bp.MaxRounds, bpMedium.MaxRounds)
	}
}

func TestEstimateBudgetResearch(t *testing.T) {
	input := BudgetEstimationInput{
		GoalID:     "goal-004",
		Category:   CategoryResearch,
		TaskCount:  4,
		MaxDepth:   2,
		Complexity: ComplexityMedium,
	}
	bp := EstimateBudget(input)
	// Research has higher token baseline (80000 per task)
	if bp.MaxTokens <= 0 {
		t.Error("Research budget tokens should be positive")
	}
}

func TestEstimateBudgetUnknownCategory(t *testing.T) {
	input := BudgetEstimationInput{
		GoalID:     "goal-005",
		Category:   GoalCategory("unknown"),
		TaskCount:  2,
		MaxDepth:   0,
		Complexity: ComplexityMedium,
	}
	bp := EstimateBudget(input)
	// Should fall back to general baseline
	if bp.MaxRounds <= 0 {
		t.Error("unknown category should fall back to general baseline")
	}
}

// --- ToBudget ---

func TestBudgetProposalToBudget(t *testing.T) {
	bp := BudgetProposal{
		GoalID:           "goal-001",
		MaxRounds:        30,
		MaxTokens:        200000,
		MaxDurationHours: 15.0,
	}

	b := bp.ToBudget()
	if b.GoalID != "goal-001" {
		t.Errorf("GoalID: got %q, want %q", b.GoalID, "goal-001")
	}
	if b.MaxRounds != 30 {
		t.Errorf("MaxRounds: got %d, want 30", b.MaxRounds)
	}
	if !b.IsConfigured() {
		t.Error("Budget should be configured after ToBudget")
	}
	if b.IsExhausted() {
		t.Error("Fresh budget should not be exhausted")
	}
	if b.UsedRounds != 0 || b.UsedTokens != 0 || b.UsedHours != 0 {
		t.Error("Fresh budget should have zero usage")
	}
}

// --- IsExhausted / IsConfigured ---

func TestBudgetIsExhaustedRounds(t *testing.T) {
	b := Budget{MaxRounds: 5, UsedRounds: 5, MaxTokens: 1000, MaxDurationHours: 10}
	if !b.IsExhausted() {
		t.Error("Budget with UsedRounds >= MaxRounds should be exhausted")
	}
}

func TestBudgetIsExhaustedTokens(t *testing.T) {
	b := Budget{MaxTokens: 1000, UsedTokens: 1000}
	if !b.IsExhausted() {
		t.Error("Budget with UsedTokens >= MaxTokens should be exhausted")
	}
}

func TestBudgetIsExhaustedHours(t *testing.T) {
	b := Budget{MaxDurationHours: 5.0, UsedHours: 5.0}
	if !b.IsExhausted() {
		t.Error("Budget with UsedHours >= MaxDurationHours should be exhausted")
	}
}

func TestBudgetNotExhaustedWhenUnder(t *testing.T) {
	b := Budget{MaxRounds: 10, UsedRounds: 5, MaxTokens: 1000, UsedTokens: 500}
	if b.IsExhausted() {
		t.Error("Budget under limits should not be exhausted")
	}
}

func TestBudgetUnlimitedNotExhausted(t *testing.T) {
	b := Budget{} // all zeros = unconfigured, unlimited
	if b.IsExhausted() {
		t.Error("Unconfigured budget should not be exhausted")
	}
	if b.IsConfigured() {
		t.Error("Zero-value budget should not be configured")
	}
}

func TestBudgetConfiguredSemantics(t *testing.T) {
	if (Budget{MaxRounds: 5}).IsConfigured() != true {
		t.Error("Budget with MaxRounds should be configured")
	}
	if (Budget{MaxTokens: 1000}).IsConfigured() != true {
		t.Error("Budget with MaxTokens should be configured")
	}
	if (Budget{MaxDurationHours: 1.0}).IsConfigured() != true {
		t.Error("Budget with MaxDurationHours should be configured")
	}
	if (Budget{}).IsConfigured() != false {
		t.Error("Zero-value budget should not be configured")
	}
}

// --- TrackRound / TrackTokens / TrackHours ---

func TestTrackRound(t *testing.T) {
	b := Budget{MaxRounds: 3}
	ok1 := b.TrackRound() // 1/3
	ok2 := b.TrackRound() // 2/3
	ok3 := b.TrackRound() // 3/3 — last allowed, reaches limit
	ok4 := b.TrackRound() // rejected, already exhausted

	if !ok1 || !ok2 || !ok3 {
		t.Error("first 3 rounds should be OK")
	}
	if ok4 {
		t.Error("4th round should be rejected (already exhausted)")
	}
	// UsedRounds should be 3, not 4, because the 4th call was rejected
	if b.UsedRounds != 3 {
		t.Errorf("UsedRounds: got %d, want 3 (4th call rejected before increment)", b.UsedRounds)
	}
}

func TestTrackTokens(t *testing.T) {
	b := Budget{MaxTokens: 100}
	ok1 := b.TrackTokens(60)
	ok2 := b.TrackTokens(30)
	ok3 := b.TrackTokens(20) // exceeds

	if !ok1 || !ok2 {
		t.Error("first 90 tokens should be OK")
	}
	if ok3 {
		t.Error("110 tokens should report exhausted")
	}
	if b.UsedTokens != 110 {
		t.Errorf("UsedTokens: got %d, want 110", b.UsedTokens)
	}
}

func TestTrackHours(t *testing.T) {
	b := Budget{MaxDurationHours: 2.0}
	ok1 := b.TrackHours(1.5)
	ok2 := b.TrackHours(0.4)
	ok3 := b.TrackHours(0.2) // exceeds

	if !ok1 || !ok2 {
		t.Error("1.9 hours should be OK")
	}
	if ok3 {
		t.Error("2.1 hours should report exhausted")
	}
}

// --- Remaining ---

func TestRemaining(t *testing.T) {
	b := Budget{MaxRounds: 10, UsedRounds: 3, MaxTokens: 1000, UsedTokens: 200, MaxDurationHours: 5.0, UsedHours: 1.5}
	r, tok, h := b.Remaining()

	if r != 7 {
		t.Errorf("remaining rounds: got %d, want 7", r)
	}
	if tok != 800 {
		t.Errorf("remaining tokens: got %d, want 800", tok)
	}
	if h != 3.5 {
		t.Errorf("remaining hours: got %f, want 3.5", h)
	}
}

func TestRemainingUnlimited(t *testing.T) {
	b := Budget{} // all zeros
	r, tok, h := b.Remaining()

	if r != -1 {
		t.Errorf("unlimited rounds should be -1, got %d", r)
	}
	if tok != -1 {
		t.Errorf("unlimited tokens should be -1, got %d", tok)
	}
	if h != -1 {
		t.Errorf("unlimited hours should be -1, got %f", h)
	}
}

func TestRemainingFloorZero(t *testing.T) {
	b := Budget{MaxRounds: 5, UsedRounds: 10}
	r, _, _ := b.Remaining()
	if r != 0 {
		t.Errorf("remaining should floor at 0, got %d", r)
	}
}

// --- IsApproachingLimit ---

func TestIsApproachingLimit(t *testing.T) {
	b := Budget{MaxRounds: 10, UsedRounds: 8, MaxTokens: 1000, UsedTokens: 100, MaxDurationHours: 10, UsedHours: 1}
	if !b.IsApproachingLimit(0.8) {
		t.Error("80% rounds used should trigger approaching")
	}
	if b.IsApproachingLimit(0.9) {
		t.Error("80% rounds but not 90% should not trigger")
	}
}

func TestIsApproachingLimitTokens(t *testing.T) {
	b := Budget{MaxTokens: 1000, UsedTokens: 900}
	if !b.IsApproachingLimit(0.9) {
		t.Error("90% tokens used should trigger approaching")
	}
}

func TestIsApproachingLimitUnlimited(t *testing.T) {
	b := Budget{} // all unlimited
	if b.IsApproachingLimit(0.5) {
		t.Error("unlimited budget should never approach limit")
	}
}

// --- UsageReport ---

func TestUsageReport(t *testing.T) {
	b := Budget{
		GoalID:           "goal-001",
		MaxRounds:        10,
		UsedRounds:       3,
		MaxTokens:        5000,
		UsedTokens:       1200,
		MaxDurationHours: 8.0,
		UsedHours:        2.5,
	}

	report := b.UsageReport()
	if !strings.Contains(report, "goal-001") {
		t.Error("usage report should contain goal ID")
	}
	if !strings.Contains(report, "3/10") {
		t.Error("usage report should show round usage")
	}
	if !strings.Contains(report, "1200/5000") {
		t.Error("usage report should show token usage")
	}
}

func TestUsageReportUnlimited(t *testing.T) {
	b := Budget{GoalID: "goal-002"}
	report := b.UsageReport()
	if !strings.Contains(report, "∞") {
		t.Error("unlimited dimensions should show ∞")
	}
}

// --- ResetUsage ---

func TestResetUsage(t *testing.T) {
	b := Budget{MaxRounds: 10, UsedRounds: 5, MaxTokens: 1000, UsedTokens: 500, MaxDurationHours: 5.0, UsedHours: 2.0}
	b.ResetUsage()
	if b.UsedRounds != 0 || b.UsedTokens != 0 || b.UsedHours != 0 {
		t.Error("ResetUsage should zero all counters")
	}
	// Max values should be preserved
	if b.MaxRounds != 10 {
		t.Error("ResetUsage should preserve MaxRounds")
	}
}

// --- Depth multiplier ---

func TestDepthMultiplier(t *testing.T) {
	if depthMultiplier(0) != 1.0 {
		t.Error("depth 0 multiplier should be 1.0")
	}
	if depthMultiplier(1) != 1.15 {
		t.Error("depth 1 multiplier should be 1.15")
	}
	if depthMultiplier(3) != 1.45 {
		t.Errorf("depth 3 multiplier should be 1.45, got %f", depthMultiplier(3))
	}
	if depthMultiplier(15) != depthMultiplier(10) {
		t.Error("depth > 10 should be capped at 10")
	}
}
