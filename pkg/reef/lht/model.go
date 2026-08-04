package lht

import "time"

// Goal represents a long-horizon task goal.
type Goal struct {
	GoalID      string    `json:"goal_id"`
	Description string    `json:"description"`
	State       State     `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	BudgetRef   string    `json:"budget_ref"`
	PlanRef     string    `json:"plan_ref"`

	// Recovery anchors — see P0-01/P0-02 in contract review.
	PreviousState State `json:"previous_state,omitempty"` // state before entering PAUSED
	EscalatedFrom State `json:"escalated_from,omitempty"` // last execution state before ESCALATED

	// PlanVersion mirrors Plan.PlanVersion in state.json for consistency
	// validation on resume (persistence spec L26).
	PlanVersion string `json:"plan_version,omitempty"`
}

// TaskNode represents a single subtask in the execution DAG.
// Counters are used for anti-stall / repair-limit detection (see D7).
type TaskNode struct {
	TaskID              string   `json:"task_id"`
	Description         string   `json:"description"`
	State               State    `json:"state"`
	Dependencies        []string `json:"dependencies,omitempty"`
	RepairCount         int      `json:"repair_count"`
	StrategySwitchCount int      `json:"strategy_switch_count"`
	StallCount          int      `json:"stall_count"`
	MustHaves           []string `json:"must_haves,omitempty"`
	Rubric              string   `json:"rubric,omitempty"`
}

// Anti-stall hard limits — see anti-stall spec (N=3, M=2, K=3) and design D7.
const (
	MaxRepairRounds        = 3 // N: same-strategy repair limit before forcing strategy switch
	MaxStrategySwitchCount = 2 // M: strategy switch limit before stalling
	MaxStallRounds         = 3 // K: consecutive stall rounds before ESCALATED
)

// IsRepairExhausted returns true when the subtask has exhausted its repair budget
// (RepairCount >= N) and must switch strategy instead of retrying identically.
func (t *TaskNode) IsRepairExhausted() bool { return t.RepairCount >= MaxRepairRounds }

// IsStrategySwitchExhausted returns true when the subtask has cycled through M
// strategy switches without success and must trigger stalling.
func (t *TaskNode) IsStrategySwitchExhausted() bool { return t.StrategySwitchCount >= MaxStrategySwitchCount }

// IsStallExhausted returns true when K consecutive rounds without progress
// have been detected, forcing ESCALATED.
func (t *TaskNode) IsStallExhausted() bool { return t.StallCount >= MaxStallRounds }

// Plan is the execution plan produced during PLANNING phase.
type Plan struct {
	GoalID       string     `json:"goal_id"`
	PlanVersion  string     `json:"plan_version"`
	Tasks        []TaskNode `json:"tasks"`
	Capabilities []string   `json:"capabilities"`
}

// Budget tracks hard limits and consumed quota for a goal.
type Budget struct {
	GoalID           string  `json:"goal_id"`
	MaxRounds        int     `json:"max_rounds"`
	MaxTokens        int64   `json:"max_tokens"`
	MaxDurationHours float64 `json:"max_duration_hours"`
	UsedRounds       int     `json:"used_rounds"`
	UsedTokens       int64   `json:"used_tokens"`
	UsedHours        float64 `json:"used_hours"`
}

// IsExhausted returns true if any hard limit has been reached.
// A zero max value means unlimited (never exhausted on that dimension).
func (b Budget) IsExhausted() bool {
	if b.MaxRounds > 0 && b.UsedRounds >= b.MaxRounds {
		return true
	}
	if b.MaxTokens > 0 && b.UsedTokens >= b.MaxTokens {
		return true
	}
	if b.MaxDurationHours > 0 && b.UsedHours >= b.MaxDurationHours {
		return true
	}
	return false
}

// IsConfigured reports whether any hard limit has been explicitly set.
// A zero-value Budget (all Max* == 0) is unconfigured; engine must not
// enter EXECUTING until a budget proposal is confirmed by the user.
func (b Budget) IsConfigured() bool {
	return b.MaxRounds > 0 || b.MaxTokens > 0 || b.MaxDurationHours > 0
}

// ReviewRecord captures a single review round by one of the three LHT roles.
type ReviewRecord struct {
	ReviewRound int      `json:"review_round"`
	Reviewer    string   `json:"reviewer"`
	Verdict     string   `json:"verdict"`
	Issues      []string `json:"issues,omitempty"`
	Score       float64  `json:"score"`
}

// Review verdicts — evaluator outputs PASS/FAIL, reviewer may UPGRADE.
const (
	VerdictPass    = "PASS"
	VerdictFail    = "FAIL"
	VerdictUpgrade = "UPGRADE"
)

// UserReply represents a user's response at one of the three gates
// (GROUNDING questions, WAIT_APPROVAL plan review, FINAL_APPROVAL final sign-off).
// Also carries channel/chat context for command routing (W3B contract).
type UserReply struct {
	ReplyType string    `json:"reply_type"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Channel   string    `json:"channel,omitempty"` // e.g. "telegram", "feishu", "cli"
	ChatID    string    `json:"chat_id,omitempty"` // conversation / group identifier
	UserID    string    `json:"user_id,omitempty"` // sender identifier
}

// ReplyType constants — the five valid user reply types across the three gates.
const (
	ReplyTypeAnswer       = "answer"
	ReplyTypeApprove      = "approve"
	ReplyTypeReject       = "reject"
	ReplyTypeFinalApprove = "final_approve"
	ReplyTypeAbort        = "abort"
)
