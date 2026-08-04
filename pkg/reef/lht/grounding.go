package lht

import (
	"fmt"
	"strings"
)

// MaxGroundingRounds is the hard limit on alignment Q&A rounds.
// Exceeding this triggers ESCALATED (tasks 3.3 + 7.7).
const MaxGroundingRounds = 10

// GroundingQuestionCategory classifies alignment questions by domain.
type GroundingQuestionCategory string

const (
	QuestionScope             GroundingQuestionCategory = "scope"
	QuestionPriority          GroundingQuestionCategory = "priority"
	QuestionConstraint        GroundingQuestionCategory = "constraint"
	QuestionAcceptanceCriteria GroundingQuestionCategory = "acceptance_criteria"
	QuestionFollowUp          GroundingQuestionCategory = "follow_up"
)

// GroundingQuestion is a single alignment question presented to the user.
type GroundingQuestion struct {
	Category GroundingQuestionCategory `json:"category"`
	Question string                    `json:"question"`
}

// GroundingSession tracks the state of a goal-alignment Q&A session.
type GroundingSession struct {
	GoalID          string                                     `json:"goal_id"`
	Description     string                                     `json:"description"`
	Questions       []GroundingQuestion                        `json:"questions"`
	Answers         []UserReply                                `json:"answers"`
	Round           int                                        `json:"round"`
	IsComplete      bool                                       `json:"is_complete"`
	Ambiguities     map[GroundingQuestionCategory][]string     `json:"ambiguities,omitempty"`
	PendingIndex    int                                        `json:"pending_index"`
}

// GoalDefinition is the structured goal definition generated after successful alignment.
// TODO (W3A): Goal in model.go lacks granularity/scope/constraints/acceptance_criteria fields;
// this type bridges the gap until model.go is extended.
type GoalDefinition struct {
	GoalID              string   `json:"goal_id"`
	Description         string   `json:"description"`
	Granularity         string   `json:"granularity"`
	Scope               string   `json:"scope"`
	Constraints         []string `json:"constraints"`
	AcceptanceCriteria  []string `json:"acceptance_criteria"`
	Priorities          []string `json:"priorities"`
}

// NewGroundingSession creates a fresh alignment session with the standard
// four-category question set derived from the goal description.
func NewGroundingSession(goalID, description string) *GroundingSession {
	qs := GenerateQuestions(description)
	return &GroundingSession{
		GoalID:      goalID,
		Description: description,
		Questions:   qs,
		Answers:     make([]UserReply, 0),
		Round:       0,
		IsComplete:  false,
		Ambiguities: make(map[GroundingQuestionCategory][]string),
		PendingIndex: 0,
	}
}

// GenerateQuestions produces the standard alignment question set (one per category).
// In production this would be enhanced by LLM-generated questions; the template-based
// approach here serves as the v1 baseline.
func GenerateQuestions(description string) []GroundingQuestion {
	qs := []GroundingQuestion{
		{
			Category: QuestionScope,
			Question: fmt.Sprintf(
				"请明确目标的范围：\"%s\" 具体边界在哪里？哪些内容属于本次范围，哪些明确排除？",
				description,
			),
		},
		{
			Category: QuestionPriority,
			Question: "请列出本次目标的优先级排序：哪些子目标是必须达成的（P0），哪些是可选的（P1/P2）？",
		},
		{
			Category: QuestionConstraint,
			Question: "有哪些硬性约束？例如：技术栈限制、时间限制、资源限制、合规要求、兼容性要求等。",
		},
		{
			Category: QuestionAcceptanceCriteria,
			Question: "如何判定目标已达成？请给出具体的验收标准（可量化、可验证）。",
		},
	}
	return qs
}

// NextQuestion returns the next unanswered question, or nil if all answered.
func (gs *GroundingSession) NextQuestion() *GroundingQuestion {
	if gs.IsComplete {
		return nil
	}
	if gs.PendingIndex >= len(gs.Questions) {
		return nil
	}
	return &gs.Questions[gs.PendingIndex]
}

// ProcessAnswer handles a user's reply to the current grounding question.
// It evaluates whether the answer resolves the ambiguity and advances the session.
// Returns nil if processing succeeded; returns an error if the answer is insufficient.
func (gs *GroundingSession) ProcessAnswer(reply UserReply) error {
	if gs.IsComplete {
		return fmt.Errorf("grounding session is already complete")
	}
	if gs.ShouldEscalate() {
		return fmt.Errorf("grounding session has exceeded max rounds (%d), must escalate", MaxGroundingRounds)
	}

	gs.Answers = append(gs.Answers, reply)
	gs.Round++

	q := gs.NextQuestion()
	if q == nil {
		// All questions exhausted; check ambiguity
		gs.IsComplete = true
		return nil
	}

	// Evaluate whether the answer sufficiently addresses the current question.
	insufficient := isAnswerInsufficient(reply.Content)
	if insufficient {
		// Record ambiguity and generate a follow-up question.
		gs.Ambiguities[q.Category] = append(gs.Ambiguities[q.Category], reply.Content)
		followUp := GroundingQuestion{
			Category: QuestionFollowUp,
			Question: fmt.Sprintf(
				"回答不够具体。关于 [%s]，请进一步说明：您希望的具体结果是什么？",
				q.Category,
			),
		}
		// Insert follow-up at current position
		gs.Questions = append(gs.Questions[:gs.PendingIndex],
			append([]GroundingQuestion{followUp}, gs.Questions[gs.PendingIndex:]...)...)
		return fmt.Errorf("answer insufficient, follow-up question generated")
	}

	// Answer accepted; advance to next question.
	gs.PendingIndex++
	if gs.PendingIndex >= len(gs.Questions) {
		gs.IsComplete = true
	}
	return nil
}

// ShouldEscalate returns true when the grounding session has exceeded
// the hard round limit and must be escalated to the user.
func (gs *GroundingSession) ShouldEscalate() bool {
	return gs.Round >= MaxGroundingRounds
}

// HasUnresolvedQuestions returns true if there are still unanswered or
// insufficiently-resolved questions in the session.
func (gs *GroundingSession) HasUnresolvedQuestions() bool {
	return !gs.IsComplete || gs.PendingIndex < len(gs.Questions)
}

// BuildGoalDefinition constructs a GoalDefinition from the completed
// grounding session. Callers must verify IsComplete first.
func (gs *GroundingSession) BuildGoalDefinition() GoalDefinition {
	def := GoalDefinition{
		GoalID:             gs.GoalID,
		Description:        gs.Description,
		AcceptanceCriteria: make([]string, 0),
		Constraints:        make([]string, 0),
		Priorities:         make([]string, 0),
	}

	for i, q := range gs.Questions {
		// Find the corresponding answer
		var answerText string
		answerIdx := -1
		for j, a := range gs.Answers {
			if a.ReplyType == ReplyTypeAnswer && answerIdx < i {
				answerIdx = j
				answerText = a.Content
			}
		}

		switch q.Category {
		case QuestionScope:
			def.Scope = answerText
		case QuestionAcceptanceCriteria:
			def.AcceptanceCriteria = append(def.AcceptanceCriteria, answerText)
		case QuestionConstraint:
			def.Constraints = append(def.Constraints, answerText)
		case QuestionPriority:
			def.Priorities = append(def.Priorities, answerText)
		}
	}

	// Derive granularity from scope
	if def.Scope != "" {
		def.Granularity = deriveGranularity(def.Scope)
	}

	return def
}

// deriveGranularity heuristically determines granularity level from scope text.
func deriveGranularity(scope string) string {
	scopeLower := strings.ToLower(scope)
	switch {
	case strings.Contains(scopeLower, "系统") || strings.Contains(scopeLower, "平台") || strings.Contains(scopeLower, "架构"):
		return "system"
	case strings.Contains(scopeLower, "模块") || strings.Contains(scopeLower, "服务") || strings.Contains(scopeLower, "子系统"):
		return "module"
	case strings.Contains(scopeLower, "功能") || strings.Contains(scopeLower, "特性") || strings.Contains(scopeLower, "feature"):
		return "feature"
	case strings.Contains(scopeLower, "bug") || strings.Contains(scopeLower, "修复") || strings.Contains(scopeLower, "fix"):
		return "bugfix"
	default:
		return "task"
	}
}

// isAnswerInsufficient checks if a user answer is too vague to resolve ambiguity.
// v1 heuristic: very short or generic answers are insufficient.
func isAnswerInsufficient(answer string) bool {
	trimmed := strings.TrimSpace(answer)
	if len(trimmed) == 0 {
		return true
	}
	// Very short answers (<10 chars) are likely insufficient.
	if len(trimmed) < 10 {
		return true
	}
	// Generic non-answers.
	generics := []string{"不知道", "不确定", "随便", "都可以", "你决定", "无所谓", "idk", "don't know", "not sure"}
	lower := strings.ToLower(trimmed)
	for _, g := range generics {
		if strings.Contains(lower, g) {
			return true
		}
	}
	return false
}
