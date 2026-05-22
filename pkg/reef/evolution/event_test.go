package evolution

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validEvent() *EvolutionEvent {
	return &EvolutionEvent{
		ID:         "evt-001",
		TaskID:     "task-042",
		ClientID:   "client-42",
		EventType:  EventFailurePattern,
		Signal:     "timeout on step 3",
		RootCause:  "network partition between agent and API",
		GeneID:     "gene-001",
		Strategy:   "balanced",
		Importance: 0.75,
		CreatedAt:  time.Date(2025, 6, 15, 12, 0, 0, 123456789, time.UTC),
	}
}

// =============================================================================
// Task 4: Event JSON Round-trip
// =============================================================================

func TestEventJSONRoundTrip(t *testing.T) {
	t.Run("fully populated event", func(t *testing.T) {
		e1 := validEvent()
		data, err := json.Marshal(e1)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var e2 EvolutionEvent
		if err := json.Unmarshal(data, &e2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if !reflect.DeepEqual(e1, &e2) {
			t.Errorf("round-trip mismatch:\n  original: %+v\n  decoded:  %+v", e1, &e2)
		}
	})

	t.Run("minimal event with optional fields omitted", func(t *testing.T) {
		e1 := &EvolutionEvent{
			ID:         "evt-min",
			TaskID:     "task-min",
			ClientID:   "client-min",
			EventType:  EventSuccessPattern,
			Signal:     "completed successfully",
			RootCause:  "", // Empty for success events
			GeneID:     "",
			Strategy:   "",
			Importance: 0.5,
			CreatedAt:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		data, err := json.Marshal(e1)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var e2 EvolutionEvent
		if err := json.Unmarshal(data, &e2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if e1.RootCause != e2.RootCause {
			t.Errorf("RootCause mismatch: %q vs %q", e1.RootCause, e2.RootCause)
		}
	})

	t.Run("CreatedAt preserves millisecond precision", func(t *testing.T) {
		orig := time.Date(2025, 6, 15, 12, 0, 0, 123000000, time.UTC)
		e1 := validEvent()
		e1.CreatedAt = orig
		data, err := json.Marshal(e1)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var e2 EvolutionEvent
		if err := json.Unmarshal(data, &e2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		// time.Time JSON uses RFC3339Nano which preserves nanoseconds
		if !e1.CreatedAt.Equal(e2.CreatedAt) {
			t.Errorf("CreatedAt mismatch: %v vs %v", e1.CreatedAt, e2.CreatedAt)
		}
	})

	t.Run("created_at field is present in JSON", func(t *testing.T) {
		e := validEvent()
		data, err := json.Marshal(e)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(data), "created_at") {
			t.Error("JSON should contain created_at field")
		}
	})

	t.Run("extra unknown field is ignored", func(t *testing.T) {
		jsonStr := `{"id":"e1","task_id":"t1","client_id":"c1","event_type":"failure_pattern","signal":"s","importance":0.5,"created_at":"2025-01-01T00:00:00Z","unknown_extra":"should be ignored"}`
		var e EvolutionEvent
		if err := json.Unmarshal([]byte(jsonStr), &e); err != nil {
			t.Fatalf("unmarshal with extra field: %v", err)
		}
		if e.ID != "e1" {
			t.Errorf("ID should be 'e1', got %q", e.ID)
		}
	})
}

// =============================================================================
// Task 4: Event Validation (IsValid)
// =============================================================================

func TestEventValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*EvolutionEvent)
		isValid bool
	}{
		{
			name:    "valid event passes",
			modify:  func(e *EvolutionEvent) {},
			isValid: true,
		},
		{
			name:    "empty ID fails",
			modify:  func(e *EvolutionEvent) { e.ID = "" },
			isValid: false,
		},
		{
			name:    "empty TaskID fails",
			modify:  func(e *EvolutionEvent) { e.TaskID = "" },
			isValid: false,
		},
		{
			name:    "empty ClientID fails",
			modify:  func(e *EvolutionEvent) { e.ClientID = "" },
			isValid: false,
		},
		{
			name:    "zero CreatedAt fails",
			modify:  func(e *EvolutionEvent) { e.CreatedAt = time.Time{} },
			isValid: false,
		},
		{
			name:    "Importance 0.0 is valid",
			modify:  func(e *EvolutionEvent) { e.Importance = 0.0 },
			isValid: true,
		},
		{
			name:    "Importance 1.0 is valid",
			modify:  func(e *EvolutionEvent) { e.Importance = 1.0 },
			isValid: true,
		},
		{
			name:    "Importance 1.1 is invalid",
			modify:  func(e *EvolutionEvent) { e.Importance = 1.1 },
			isValid: false,
		},
		{
			name:    "Importance -0.1 is invalid",
			modify:  func(e *EvolutionEvent) { e.Importance = -0.1 },
			isValid: false,
		},
		{
			name:    "RootCause empty for success event is valid",
			modify: func(e *EvolutionEvent) {
				e.RootCause = ""
				e.EventType = EventSuccessPattern
			},
			isValid: true,
		},
		{
			name:    "RootCause non-empty for failure event is valid",
			modify: func(e *EvolutionEvent) {
				e.RootCause = "network error"
				e.EventType = EventFailurePattern
			},
			isValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := validEvent()
			tt.modify(e)
			if got := e.IsValid(); got != tt.isValid {
				t.Errorf("IsValid() = %v, want %v", got, tt.isValid)
			}
		})
	}
}
