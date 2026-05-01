package evolution

import (
	"fmt"
	"log/slog"
	"math"
)

// Strategy defines the evolution strategy that governs how the Evolver
// selects targets for gene generation.
type Strategy string

const (
	StrategyBalanced   Strategy = "balanced"
	StrategyInnovate   Strategy = "innovate"
	StrategyHarden     Strategy = "harden"
	StrategyRepairOnly Strategy = "repair-only"
)

// StrategyWeights represents probability weights for target selection
// in the Evolver's selectTarget method.
type StrategyWeights struct {
	Innovate float64 `json:"innovate"` // Weight for new capability creation
	Optimize float64 `json:"optimize"` // Weight for existing pattern optimization
	Repair   float64 `json:"repair"`   // Weight for failure pattern repair
}

// Weights returns the StrategyWeights for the given strategy.
// Unknown strategies fall back to Balanced weights and log a warning.
func (s Strategy) Weights() StrategyWeights {
	switch s {
	case StrategyInnovate:
		return StrategyWeights{Innovate: 0.80, Optimize: 0.15, Repair: 0.05}
	case StrategyHarden:
		return StrategyWeights{Innovate: 0.20, Optimize: 0.40, Repair: 0.40}
	case StrategyRepairOnly:
		return StrategyWeights{Innovate: 0.00, Optimize: 0.00, Repair: 1.00}
	case StrategyBalanced:
		return StrategyWeights{Innovate: 0.50, Optimize: 0.30, Repair: 0.20}
	default:
		slog.Default().Warn("unknown strategy, falling back to balanced",
			slog.String("strategy", string(s)))
		return StrategyWeights{Innovate: 0.50, Optimize: 0.30, Repair: 0.20}
	}
}

// Validate checks that weights are within [0, 1] and sum to approximately 1.0.
// Returns nil on success.
func (sw StrategyWeights) Validate() error {
	if sw.Innovate < 0 || sw.Innovate > 1 {
		return fmt.Errorf("Innovate weight out of range: %f", sw.Innovate)
	}
	if sw.Optimize < 0 || sw.Optimize > 1 {
		return fmt.Errorf("Optimize weight out of range: %f", sw.Optimize)
	}
	if sw.Repair < 0 || sw.Repair > 1 {
		return fmt.Errorf("Repair weight out of range: %f", sw.Repair)
	}
	sum := sw.Innovate + sw.Optimize + sw.Repair
	if sum > 1.01 || math.Abs(sum-1.0) > 1e-9 {
		// Allow minor floating-point tolerance
		return fmt.Errorf("weights sum to %f, expected ~1.0", sum)
	}
	return nil
}
