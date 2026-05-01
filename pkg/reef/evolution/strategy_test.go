package evolution

import (
	"math"
	"testing"
)

// =============================================================================
// Task 5: Strategy Weights
// =============================================================================

func TestStrategyWeights(t *testing.T) {
	tests := []struct {
		name     string
		strategy Strategy
		want     StrategyWeights
	}{
		{
			name:     "Balanced",
			strategy: StrategyBalanced,
			want:     StrategyWeights{Innovate: 0.50, Optimize: 0.30, Repair: 0.20},
		},
		{
			name:     "Innovate",
			strategy: StrategyInnovate,
			want:     StrategyWeights{Innovate: 0.80, Optimize: 0.15, Repair: 0.05},
		},
		{
			name:     "Harden",
			strategy: StrategyHarden,
			want:     StrategyWeights{Innovate: 0.20, Optimize: 0.40, Repair: 0.40},
		},
		{
			name:     "RepairOnly",
			strategy: StrategyRepairOnly,
			want:     StrategyWeights{Innovate: 0.00, Optimize: 0.00, Repair: 1.00},
		},
		{
			name:     "unknown falls back to Balanced",
			strategy: Strategy("nonexistent"),
			want:     StrategyWeights{Innovate: 0.50, Optimize: 0.30, Repair: 0.20},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.strategy.Weights()
			if !floatEqual(got.Innovate, tt.want.Innovate) {
				t.Errorf("Innovate: got %f, want %f", got.Innovate, tt.want.Innovate)
			}
			if !floatEqual(got.Optimize, tt.want.Optimize) {
				t.Errorf("Optimize: got %f, want %f", got.Optimize, tt.want.Optimize)
			}
			if !floatEqual(got.Repair, tt.want.Repair) {
				t.Errorf("Repair: got %f, want %f", got.Repair, tt.want.Repair)
			}
		})
	}

	t.Run("RepairOnly has exactly 0.0 and 1.0", func(t *testing.T) {
		w := StrategyRepairOnly.Weights()
		if w.Innovate != 0.0 {
			t.Errorf("expected Innovate=0.0, got %f", w.Innovate)
		}
		if w.Optimize != 0.0 {
			t.Errorf("expected Optimize=0.0, got %f", w.Optimize)
		}
		if w.Repair != 1.0 {
			t.Errorf("expected Repair=1.0, got %f", w.Repair)
		}
	})
}

func TestStrategyWeights_Validate(t *testing.T) {
	tests := []struct {
		name    string
		weights StrategyWeights
		wantErr bool
	}{
		{
			name:    "valid sum exactly 1.0",
			weights: StrategyWeights{Innovate: 0.50, Optimize: 0.30, Repair: 0.20},
			wantErr: false,
		},
		{
			name:    "valid sum close to 1.0 (within tolerance)",
			weights: StrategyWeights{Innovate: 0.3333333333, Optimize: 0.3333333333, Repair: 0.3333333334},
			wantErr: false,
		},
		{
			name:    "invalid sum = 1.5",
			weights: StrategyWeights{Innovate: 0.50, Optimize: 0.50, Repair: 0.50},
			wantErr: true,
		},
		{
			name:    "invalid sum = 0.5",
			weights: StrategyWeights{Innovate: 0.20, Optimize: 0.20, Repair: 0.10},
			wantErr: true,
		},
		{
			name:    "invalid negative weight",
			weights: StrategyWeights{Innovate: -0.10, Optimize: 0.60, Repair: 0.50},
			wantErr: true,
		},
		{
			name:    "invalid weight > 1.0",
			weights: StrategyWeights{Innovate: 1.50, Optimize: 0.00, Repair: -0.50},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.weights.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestAllDefinedStrategiesWeightsSumToOne(t *testing.T) {
	strategies := []Strategy{StrategyBalanced, StrategyInnovate, StrategyHarden, StrategyRepairOnly}
	for _, s := range strategies {
		t.Run(string(s), func(t *testing.T) {
			w := s.Weights()
			sum := w.Innovate + w.Optimize + w.Repair
			if math.Abs(sum-1.0) > 1e-9 {
				t.Errorf("strategy %s weights sum to %f, expected 1.0", s, sum)
			}
		})
	}
}

func floatEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
