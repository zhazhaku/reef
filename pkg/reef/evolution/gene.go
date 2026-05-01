// Package evolution defines the core data types for the Reef self-evolution engine.
// These types form the foundation of the GEP (Gene-Evolve-Publish) pipeline:
// Gene, EvolutionEvent, Strategy, and SkillDraft.
package evolution

import (
	"fmt"
	"strings"
	"time"
)

// GeneStatus defines the lifecycle states of a Gene.
type GeneStatus string

const (
	GeneStatusDraft     GeneStatus = "draft"
	GeneStatusSubmitted GeneStatus = "submitted"
	GeneStatusApproved  GeneStatus = "approved"
	GeneStatusRejected  GeneStatus = "rejected"
	GeneStatusStagnant  GeneStatus = "stagnant"
	GeneStatusRetired   GeneStatus = "retired"
)

// Gene is the smallest reusable unit of self-evolution.
// It follows the Evolver paper's core conclusion: compact storage (≤ 200 lines
// of control_signal) outperforms verbose documentation.
type Gene struct {
	ID              string     `json:"id"`
	StrategyName    string     `json:"strategy_name"`
	Role            string     `json:"role"`
	Skills          []string   `json:"skills"`
	MatchCondition  string     `json:"match_condition"`
	ControlSignal   string     `json:"control_signal"`
	FailureWarnings []string   `json:"failure_warnings"`
	SourceEvents    []string   `json:"source_events"`
	SourceClientID  string     `json:"source_client_id"`
	Version         int        `json:"version"`
	Status          GeneStatus `json:"status"`
	StagnationCount int        `json:"stagnation_count"`
	UseCount        int        `json:"use_count"`
	SuccessRate     float64    `json:"success_rate"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	ApprovedAt      *time.Time `json:"approved_at,omitempty"`
}

// Validate performs local legality checks on the gene.
// Returns nil when all checks pass, otherwise returns a slice of distinct errors.
func (g *Gene) Validate() []error {
	var errs []error

	if g.StrategyName == "" {
		errs = append(errs, fmt.Errorf("strategy_name required"))
	}
	if g.ControlSignal == "" {
		errs = append(errs, fmt.Errorf("control_signal required"))
	}
	if len(g.ControlSignal) > 5000 {
		errs = append(errs, fmt.Errorf("control_signal exceeds 5000 chars"))
	}
	if lineCount(g.ControlSignal) > 200 {
		errs = append(errs, fmt.Errorf("control_signal exceeds 200 lines"))
	}
	if len(g.FailureWarnings) > 20 {
		errs = append(errs, fmt.Errorf("failure_warnings exceeds 20 items"))
	}

	if len(errs) == 0 {
		return nil
	}
	return errs
}

// lineCount returns the number of lines in s.
// An empty string is treated as 1 line.
func lineCount(s string) int {
	return strings.Count(s, "\n") + 1
}
