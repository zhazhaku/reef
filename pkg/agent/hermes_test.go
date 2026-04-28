// PicoClaw - Ultra-lightweight personal AI agent

package agent

import (
	"testing"
)

func TestParseHermesMode(t *testing.T) {
	tests := []struct {
		input string
		want  HermesMode
	}{
		{"", HermesFull},
		{"full", HermesFull},
		{"coordinator", HermesCoordinator},
		{"executor", HermesExecutor},
		{"unknown", HermesFull},
		{"Coordinator", HermesFull}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ParseHermesMode(tt.input)
			if got != tt.want {
				t.Errorf("ParseHermesMode(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHermesModeString(t *testing.T) {
	tests := []struct {
		mode HermesMode
		want string
	}{
		{HermesFull, "full"},
		{HermesCoordinator, "coordinator"},
		{HermesExecutor, "executor"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.mode.String(); got != tt.want {
				t.Errorf("HermesMode(%q).String() = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestHermesModeIsConstrained(t *testing.T) {
	if HermesFull.IsConstrained() {
		t.Error("HermesFull should not be constrained")
	}
	if !HermesCoordinator.IsConstrained() {
		t.Error("HermesCoordinator should be constrained")
	}
	if HermesExecutor.IsConstrained() {
		t.Error("HermesExecutor should not be constrained")
	}
}

func TestCoordinatorAllowedTools(t *testing.T) {
	allowed := CoordinatorAllowedTools()

	expected := []string{"reef_submit_task", "reef_query_task", "reef_status", "message", "reaction", "cron"}
	for _, name := range expected {
		if _, ok := allowed[name]; !ok {
			t.Errorf("expected %q in CoordinatorAllowedTools", name)
		}
	}

	forbidden := []string{"web_search", "exec", "read_file", "write_file", "spawn", "find_skills"}
	for _, name := range forbidden {
		if _, ok := allowed[name]; ok {
			t.Errorf("expected %q NOT in CoordinatorAllowedTools", name)
		}
	}
}
