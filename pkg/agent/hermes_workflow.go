// Reef - Distributed multi-agent swarm orchestration system

package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────
// Phase state machine
// ─────────────────────────────────────────────────────────────

// WorkflowPhase represents a stage in the 6-phase Hermes workflow.
type WorkflowPhase string

const (
	PhaseIdle       WorkflowPhase = "idle"
	PhaseIntake     WorkflowPhase = "intake"
	PhaseBrainstorm WorkflowPhase = "brainstorm"
	PhaseReview     WorkflowPhase = "review"
	PhaseResearch   WorkflowPhase = "research"
	PhaseDesign     WorkflowPhase = "design"
	PhaseReport     WorkflowPhase = "report"
	PhaseComplete   WorkflowPhase = "complete"
	PhaseAborted    WorkflowPhase = "aborted"
)

// validTransitions maps each phase to the set of phases it may transition to.
var validTransitions = map[WorkflowPhase][]WorkflowPhase{
	PhaseIdle:       {PhaseIntake},
	PhaseIntake:     {PhaseBrainstorm, PhaseComplete, PhaseAborted},
	PhaseBrainstorm: {PhaseReview, PhaseResearch, PhaseAborted},
	PhaseReview:     {PhaseResearch, PhaseDesign, PhaseAborted},
	PhaseResearch:   {PhaseDesign, PhaseAborted},
	PhaseDesign:     {PhaseReport, PhaseAborted},
	PhaseReport:     {PhaseComplete, PhaseAborted},
	PhaseComplete:   {},
	PhaseAborted:    {PhaseIdle},
}

// CanTransition returns true if from→to is a legal state transition.
func CanTransition(from, to WorkflowPhase) bool {
	for _, t := range validTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────
// Data types
// ─────────────────────────────────────────────────────────────

// TaskProfile captures the classification result produced during Phase 1 Intake.
type TaskProfile struct {
	TaskID           string   `json:"task_id"`
	Domain           string   `json:"domain"`
	Type             string   `json:"type"`
	Complexity       int      `json:"complexity"`
	Keywords         []string `json:"keywords"`
	EstimatedClients int      `json:"estimated_clients"`
}

// WorkflowSession is the persistent state of one Hermes workflow run.
type WorkflowSession struct {
	ConversationID  string            `json:"conversation_id"`
	TaskID          string            `json:"task_id"`
	Phase           WorkflowPhase     `json:"phase"`
	TaskProfile     *TaskProfile      `json:"task_profile,omitempty"`
	CurrentRound    int               `json:"current_round"`
	MaxRounds       int               `json:"max_rounds"`
	Directions      []Direction       `json:"directions,omitempty"`
	ConvergeStreak  int               `json:"converge_streak"`
	SelectedClients []ClientSelection `json:"selected_clients,omitempty"`
	TaskBoards      map[string]string `json:"task_boards,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// Direction represents one solution direction proposed during brainstorming.
type Direction struct {
	ID               string   `json:"id"`
	Label            string   `json:"label"`
	Description      string   `json:"description"`
	Proposer         string   `json:"proposer"`
	Round            int      `json:"round"`
	OpenspecCategory string   `json:"openspec_category,omitempty"`
	Keywords         []string `json:"keywords,omitempty"`
}

// ClientSelection records a selected client for a workflow phase.
type ClientSelection struct {
	ClientID string `json:"client_id"`
	Role     string `json:"role"`
	Fallback bool   `json:"fallback,omitempty"`
}

// ─────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────

// generateTaskID creates a short unique task identifier.
func generateTaskID() string {
	return fmt.Sprintf("h-%d", time.Now().UnixNano()%100000)
}

// formatTaskProfileReply formats the TaskProfile as a readable Markdown reply.
func formatTaskProfileReply(tp *TaskProfile, taskID string) string {
	return fmt.Sprintf(
		"## 任务分析\n\n"+
			"| 维度 | 值 |\n"+
			"|------|----|\n"+
			"| 任务ID | %s |\n"+
			"| 领域 | %s |\n"+
			"| 类型 | %s |\n"+
			"| 复杂度 | %d/5 |\n"+
			"| 预计Client数 | %d |\n"+
			"| 关键词 | %v |\n\n"+
			"是否进入 **头脑风暴** 阶段？回复「继续」开始。",
		taskID,
		tp.Domain,
		tp.Type,
		tp.Complexity,
		tp.EstimatedClients,
		tp.Keywords,
	)
}

// parseTaskProfile extracts a TaskProfile from raw LLM JSON output.
func parseTaskProfile(raw string) (*TaskProfile, error) {
	raw = stripCodeFences(raw)

	var tp TaskProfile
	if err := json.Unmarshal([]byte(raw), &tp); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}
	if tp.Complexity < 1 {
		tp.Complexity = 1
	}
	if tp.Complexity > 5 {
		tp.Complexity = 5
	}
	if tp.Type == "" {
		tp.Type = "unknown"
	}
	if tp.Domain == "" {
		tp.Domain = "general"
	}
	if tp.Keywords == nil {
		tp.Keywords = []string{}
	}
	return &tp, nil
}

// stripCodeFences removes ```json ... ``` wrappers from LLM output.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	// Strip markdown code fences
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	// Strip DeepSeek V4 thinking/tool markers that may leak into content
	s = stripDeepSeekMarkers(s)
	return strings.TrimSpace(s)
}

// stripDeepSeekMarkers removes XML-style markers that DeepSeek V4 xhigh
// sometimes injects into the content field (e.g. [thinking:...content...]).
func stripDeepSeekMarkers(s string) string {
	// Remove [thinking:] prefix and its closing [/thinking] suffix
	s = strings.TrimPrefix(s, "[thinking:]")
	s = strings.TrimSuffix(s, "[/thinking]")
	// Remove [tool_use: ...] blocks
	for {
		start := strings.Index(s, "[tool_use:")
		if start == -1 {
			break
		}
		end := strings.Index(s[start:], "]")
		if end == -1 {
			break
		}
		end += start + 1
		// Try to find matching [/tool_use]
		closeTag := strings.Index(s[end:], "[/tool_use]")
		if closeTag != -1 {
			end = end + closeTag + len("[/tool_use]")
		}
		s = s[:start] + s[end:]
	}
	return s
}
