package evolution

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// validGene returns a fully-populated Gene that passes Validate().
func validGene() *Gene {
	now := time.Now().UTC().Truncate(time.Second)
	approved := now.Add(-1 * time.Hour)
	return &Gene{
		ID:              "gene-001",
		StrategyName:    "retry-with-backoff",
		Role:            "executor",
		Skills:          []string{"bash", "python"},
		MatchCondition:  "task fails with timeout",
		ControlSignal:   "1. detect timeout\n2. calculate backoff\n3. retry with delay",
		FailureWarnings: []string{"retry loop may cause resource exhaustion"},
		SourceEvents:    []string{"evt-001", "evt-002"},
		SourceClientID:  "client-42",
		Version:         3,
		Status:          GeneStatusDraft,
		StagnationCount: 0,
		UseCount:        5,
		SuccessRate:     0.85,
		CreatedAt:       now,
		UpdatedAt:       now,
		ApprovedAt:      &approved,
	}
}

// =============================================================================
// Task 1: Gene Validation
// =============================================================================

func TestGeneValidation_Passes(t *testing.T) {
	g := validGene()
	errs := g.Validate()
	if errs != nil {
		t.Fatalf("expected nil errors for valid gene, got %d errors: %v", len(errs), errs)
	}
}

func TestGeneValidation_EmptyStrategyName(t *testing.T) {
	g := validGene()
	g.StrategyName = ""
	errs := g.Validate()
	assertHasError(t, errs, "strategy_name required")
}

func TestGeneValidation_EmptyControlSignal(t *testing.T) {
	g := validGene()
	g.ControlSignal = ""
	errs := g.Validate()
	assertHasError(t, errs, "control_signal required")
}

func TestGeneValidation_ControlSignalExactly5000Chars_Passes(t *testing.T) {
	g := validGene()
	g.ControlSignal = strings.Repeat("x", 5000)
	errs := g.Validate()
	// Must NOT contain a "exceeds 5000 chars" error
	for _, e := range errs {
		if strings.Contains(e.Error(), "exceeds 5000 chars") {
			t.Fatalf("expected 5000 chars to pass, got error: %v", e)
		}
	}
}

func TestGeneValidation_ControlSignal5001Chars_Fails(t *testing.T) {
	g := validGene()
	g.ControlSignal = strings.Repeat("x", 5001)
	errs := g.Validate()
	assertHasError(t, errs, "control_signal exceeds 5000 chars")
}

func TestGeneValidation_ControlSignalExactly200Lines_Passes(t *testing.T) {
	g := validGene()
	// 200 lines means 199 newlines
	g.ControlSignal = strings.Repeat("line\n", 199) + "line"
	if lineCount(g.ControlSignal) != 200 {
		t.Fatalf("expected 200 lines, got %d", lineCount(g.ControlSignal))
	}
	errs := g.Validate()
	for _, e := range errs {
		if strings.Contains(e.Error(), "exceeds 200 lines") {
			t.Fatalf("expected 200 lines to pass, got error: %v", e)
		}
	}
}

func TestGeneValidation_ControlSignal201Lines_Fails(t *testing.T) {
	g := validGene()
	g.ControlSignal = strings.Repeat("line\n", 200) + "line"
	if lineCount(g.ControlSignal) != 201 {
		t.Fatalf("expected 201 lines, got %d", lineCount(g.ControlSignal))
	}
	errs := g.Validate()
	assertHasError(t, errs, "control_signal exceeds 200 lines")
}

func TestGeneValidation_FailureWarningsExactly20_Passes(t *testing.T) {
	g := validGene()
	warnings := make([]string, 20)
	for i := range warnings {
		warnings[i] = "warning"
	}
	g.FailureWarnings = warnings
	errs := g.Validate()
	for _, e := range errs {
		if strings.Contains(e.Error(), "failure_warnings exceeds") {
			t.Fatalf("expected 20 warnings to pass, got error: %v", e)
		}
	}
}

func TestGeneValidation_FailureWarnings21_Fails(t *testing.T) {
	g := validGene()
	warnings := make([]string, 21)
	for i := range warnings {
		warnings[i] = "warning"
	}
	g.FailureWarnings = warnings
	errs := g.Validate()
	assertHasError(t, errs, "failure_warnings exceeds 20 items")
}

func TestGeneValidation_AllViolationsAccumulate(t *testing.T) {
	g := validGene()
	g.StrategyName = ""
	g.ControlSignal = ""
	errs := g.Validate()
	if len(errs) < 2 {
		t.Fatalf("expected at least 2 errors for multiple violations, got %d: %v", len(errs), errs)
	}
}

func TestGeneValidation_EmptySkills_Valid(t *testing.T) {
	g := validGene()
	g.Skills = nil
	errs := g.Validate()
	if errs != nil {
		t.Fatalf("expected nil errors for nil Skills, got: %v", errs)
	}
	g.Skills = []string{}
	errs = g.Validate()
	if errs != nil {
		t.Fatalf("expected nil errors for empty Skills, got: %v", errs)
	}
}

func TestGeneValidation_LineCountEmptyString(t *testing.T) {
	if lineCount("") != 1 {
		t.Fatalf("expected lineCount(\"\") == 1, got %d", lineCount(""))
	}
}

// =============================================================================
// Task 2: Gene JSON Round-trip
// =============================================================================

func TestGeneJSONRoundTrip(t *testing.T) {
	t.Run("fully populated with ApprovedAt", func(t *testing.T) {
		g1 := validGene()
		data, err := json.Marshal(g1)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var g2 Gene
		if err := json.Unmarshal(data, &g2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if !reflect.DeepEqual(g1, &g2) {
			t.Errorf("round-trip mismatch:\n  original: %+v\n  decoded:  %+v", g1, &g2)
		}
	})

	t.Run("nil ApprovedAt", func(t *testing.T) {
		g1 := validGene()
		g1.ApprovedAt = nil
		data, err := json.Marshal(g1)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var g2 Gene
		if err := json.Unmarshal(data, &g2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if !reflect.DeepEqual(g1, &g2) {
			t.Errorf("round-trip mismatch with nil ApprovedAt:\n  original: %+v\n  decoded:  %+v", g1, &g2)
		}

		// Verify approved_at is omitted from JSON
		if strings.Contains(string(data), "approved_at") {
			t.Error("JSON output should omit approved_at when nil")
		}
	})

	t.Run("unknown Status string does not panic", func(t *testing.T) {
		jsonStr := `{"id":"g1","strategy_name":"s","control_signal":"c","status":"nonexistent","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}`
		var g Gene
		err := json.Unmarshal([]byte(jsonStr), &g)
		if err != nil {
			t.Fatalf("unmarshal with unknown status should not error: %v", err)
		}
		if g.Status != "nonexistent" {
			t.Errorf("expected status 'nonexistent', got '%s'", g.Status)
		}
	})

	t.Run("extra unknown field in JSON is ignored", func(t *testing.T) {
		jsonStr := `{"id":"g1","strategy_name":"s","control_signal":"c","extra_field":"ignored","status":"draft","created_at":"2025-01-01T00:00:00Z","updated_at":"2025-01-01T00:00:00Z"}`
		var g Gene
		err := json.Unmarshal([]byte(jsonStr), &g)
		if err != nil {
			t.Fatalf("unmarshal with extra field should not error: %v", err)
		}
		if g.ID != "g1" || g.StrategyName != "s" {
			t.Errorf("gene fields affected by extra field: %+v", g)
		}
	})

	t.Run("very long ControlSignal JSON does not truncate", func(t *testing.T) {
		g := validGene()
		g.ControlSignal = strings.Repeat("x", 4999)
		data, err := json.Marshal(g)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if len(data) <= 4999 {
			t.Errorf("JSON output too short: expected > 4999 bytes, got %d", len(data))
		}
	})
}

// =============================================================================
// Helpers
// =============================================================================

func assertHasError(t *testing.T, errs []error, want string) {
	t.Helper()
	for _, e := range errs {
		if e.Error() == want {
			return
		}
	}
	t.Errorf("expected error %q in:\n%v", want, errs)
}
