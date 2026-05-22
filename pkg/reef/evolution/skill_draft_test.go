package evolution

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func validDraft() *SkillDraft {
	now := time.Now().UTC().Truncate(time.Second)
	return &SkillDraft{
		ID:            "draft-001",
		Role:          "executor",
		SkillName:     "retry-with-backoff",
		Content:       "# retry-with-backoff\n\n## Description\nRetry failed operations with exponential backoff.",
		SourceGeneIDs: []string{"gene-001", "gene-002"},
		Status:        SkillDraftPendingReview,
		CreatedAt:     now,
	}
}

// =============================================================================
// Task 6: SkillDraft Validation
// =============================================================================

func TestSkillDraftValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*SkillDraft)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid draft passes",
			modify:  func(d *SkillDraft) {},
			wantErr: false,
		},
		{
			name:    "empty ID fails",
			modify:  func(d *SkillDraft) { d.ID = "" },
			wantErr: true,
			errMsg:  "id required",
		},
		{
			name:    "empty Role fails",
			modify:  func(d *SkillDraft) { d.Role = "" },
			wantErr: true,
			errMsg:  "role required",
		},
		{
			name:    "empty SkillName fails",
			modify:  func(d *SkillDraft) { d.SkillName = "" },
			wantErr: true,
			errMsg:  "skill_name required",
		},
		{
			name:    "empty Content fails",
			modify:  func(d *SkillDraft) { d.Content = "" },
			wantErr: true,
			errMsg:  "content required",
		},
		{
			name:    "empty SourceGeneIDs fails",
			modify:  func(d *SkillDraft) { d.SourceGeneIDs = nil },
			wantErr: true,
			errMsg:  "at least one source_gene_id required",
		},
		{
			name:    "all violations accumulate",
			modify: func(d *SkillDraft) {
				d.ID = ""
				d.Role = ""
				d.SkillName = ""
				d.Content = ""
				d.SourceGeneIDs = nil
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := validDraft()
			tt.modify(d)
			errs := d.Validate()
			if tt.wantErr && errs == nil {
				t.Fatal("expected errors, got nil")
			}
			if !tt.wantErr && errs != nil {
				t.Fatalf("expected nil errors, got %v", errs)
			}
			if tt.wantErr && tt.errMsg != "" {
				found := false
				for _, e := range errs {
					if e.Error() == tt.errMsg {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected error %q in %v", tt.errMsg, errs)
				}
			}
			// Check all violations accumulate
			if tt.name == "all violations accumulate" && len(errs) < 5 {
				t.Errorf("expected at least 5 errors for all violations, got %d", len(errs))
			}
		})
	}
}

// =============================================================================
// Task 6: SkillDraft State Machine (CanTransitionTo)
// =============================================================================

func TestSkillDraft_CanTransitionTo(t *testing.T) {
	tests := []struct {
		name   string
		from   SkillDraftStatus
		to     SkillDraftStatus
		expect bool
	}{
		// PendingReview transitions
		{"pending_review → approved", SkillDraftPendingReview, SkillDraftApproved, true},
		{"pending_review → rejected", SkillDraftPendingReview, SkillDraftRejected, true},
		{"pending_review → published", SkillDraftPendingReview, SkillDraftPublished, false},
		{"pending_review → pending_review", SkillDraftPendingReview, SkillDraftPendingReview, false},

		// Approved transitions
		{"approved → published", SkillDraftApproved, SkillDraftPublished, true},
		{"approved → pending_review", SkillDraftApproved, SkillDraftPendingReview, false},
		{"approved → rejected", SkillDraftApproved, SkillDraftRejected, false},
		{"approved → approved", SkillDraftApproved, SkillDraftApproved, false},

		// Rejected transitions
		{"rejected → pending_review", SkillDraftRejected, SkillDraftPendingReview, true},
		{"rejected → approved", SkillDraftRejected, SkillDraftApproved, false},
		{"rejected → published", SkillDraftRejected, SkillDraftPublished, false},
		{"rejected → rejected", SkillDraftRejected, SkillDraftRejected, false},

		// Published transitions (terminal)
		{"published → any", SkillDraftPublished, SkillDraftPendingReview, false},
		{"published → any", SkillDraftPublished, SkillDraftApproved, false},
		{"published → any", SkillDraftPublished, SkillDraftRejected, false},
		{"published → any", SkillDraftPublished, SkillDraftPublished, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := validDraft()
			d.Status = tt.from
			got := d.CanTransitionTo(tt.to)
			if got != tt.expect {
				t.Errorf("CanTransitionTo(%s → %s) = %v, want %v", tt.from, tt.to, got, tt.expect)
			}
		})
	}
}

// =============================================================================
// Task 6: SkillDraft TransitionTo (with timestamp side effects)
// =============================================================================

func TestSkillDraft_TransitionTo(t *testing.T) {
	t.Run("valid transition sets ReviewedAt", func(t *testing.T) {
		d := validDraft()
		d.Status = SkillDraftPendingReview
		err := d.TransitionTo(SkillDraftApproved)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.ReviewedAt == nil {
			t.Error("ReviewedAt should be set on approved transition")
		}
		if d.Status != SkillDraftApproved {
			t.Errorf("expected status approved, got %s", d.Status)
		}
	})

	t.Run("rejected sets ReviewedAt", func(t *testing.T) {
		d := validDraft()
		d.Status = SkillDraftPendingReview
		err := d.TransitionTo(SkillDraftRejected)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.ReviewedAt == nil {
			t.Error("ReviewedAt should be set on rejected transition")
		}
	})

	t.Run("published sets PublishedAt", func(t *testing.T) {
		d := validDraft()
		d.Status = SkillDraftApproved
		err := d.TransitionTo(SkillDraftPublished)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.PublishedAt == nil {
			t.Error("PublishedAt should be set on published transition")
		}
		if d.Status != SkillDraftPublished {
			t.Errorf("expected status published, got %s", d.Status)
		}
	})

	t.Run("invalid transition returns error", func(t *testing.T) {
		d := validDraft()
		d.Status = SkillDraftPublished
		err := d.TransitionTo(SkillDraftApproved)
		if err == nil {
			t.Fatal("expected error for invalid transition")
		}
	})

	t.Run("re-submission: rejected → pending_review", func(t *testing.T) {
		d := validDraft()
		d.Status = SkillDraftRejected
		err := d.TransitionTo(SkillDraftPendingReview)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Status != SkillDraftPendingReview {
			t.Errorf("expected status pending_review, got %s", d.Status)
		}
	})
}

// =============================================================================
// Task 6: SkillDraft JSON Round-trip
// =============================================================================

func TestSkillDraftJSONRoundTrip(t *testing.T) {
	t.Run("fully populated with ReviewedAt and PublishedAt", func(t *testing.T) {
		d1 := validDraft()
		now := time.Now().UTC().Truncate(time.Second)
		d1.ReviewedAt = &now
		d1.PublishedAt = &now

		// TransitionTo sets timestamps, so just use the simple approach
		approved := time.Now().UTC().Truncate(time.Second)
		published := approved.Add(time.Hour)
		d1.Status = SkillDraftPublished
		d1.ReviewedAt = &approved
		d1.PublishedAt = &published

		data, err := json.Marshal(d1)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var d2 SkillDraft
		if err := json.Unmarshal(data, &d2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if !reflect.DeepEqual(d1, &d2) {
			t.Errorf("round-trip mismatch:\n  original: %+v\n  decoded:  %+v", d1, &d2)
		}
	})

	t.Run("nil optional timestamps", func(t *testing.T) {
		d1 := validDraft()
		d1.ReviewedAt = nil
		d1.PublishedAt = nil
		d1.ReviewComment = ""

		data, err := json.Marshal(d1)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var d2 SkillDraft
		if err := json.Unmarshal(data, &d2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if !reflect.DeepEqual(d1, &d2) {
			t.Errorf("round-trip mismatch with nil optionals:\n  original: %+v\n  decoded:  %+v", d1, &d2)
		}
	})

	t.Run("ReviewComment on rejected draft", func(t *testing.T) {
		d1 := validDraft()
		d1.Status = SkillDraftRejected
		d1.ReviewComment = "Skill content needs more examples"

		data, err := json.Marshal(d1)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		var d2 SkillDraft
		if err := json.Unmarshal(data, &d2); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if d2.ReviewComment != d1.ReviewComment {
			t.Errorf("ReviewComment mismatch: %q vs %q", d1.ReviewComment, d2.ReviewComment)
		}
	})
}
