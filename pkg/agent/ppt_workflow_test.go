package agent

import (
	"testing"
)

func TestPPTPhaseString(t *testing.T) {
	cases := []struct {
		phase PPTPhase
		want  string
	}{
		// v2.0 phases
		{PPTWaitMaterial, "WAIT_MATERIAL"},
		{PPTPreprocess, "PREPROCESS"},
		{PPTStyleSelect, "STYLE_SELECT"},
		// shared phases
		{PPTParse, "PARSE"},
		{PPTOutline, "OUTLINE"},
		{PPTDetail, "DETAIL"},
		{PPTStyleMap, "STYLE_MAP"},
		// v2.0-only phases
		{PPTRefine, "REFINE"},
		{PPTCompose, "COMPOSE"},
		{PPTQualityCheck, "QUALITY_CHECK"},
		{PPTPostProcess, "POST_PROCESS"},
		{PPTDone, "DONE"},
		// unknown phase
		{PPTPhase(999), "UNKNOWN(999)"},
	}

	for _, tc := range cases {
		if got := tc.phase.String(); got != tc.want {
			t.Errorf("PPTPhase(%d).String() = %q, want %q", tc.phase, got, tc.want)
		}
	}
}

func TestPPTWorkflowGetOrCreate(t *testing.T) {
	wf := NewPPTWorkflow()

	// Create new session — defaults to v2.0
	s1 := wf.GetOrCreate("session-1")
	if s1.Phase != PPTWaitMaterial {
		t.Errorf("new v2.0 session phase = %s, want WAIT_MATERIAL", s1.Phase)
	}
	if s1.Version != 2 {
		t.Errorf("new v2.0 session version = %d, want 2", s1.Version)
	}
	if s1.ComposeEngine != "svg" {
		t.Errorf("v2.0 compose engine = %q, want %q", s1.ComposeEngine, "svg")
	}

	// Get existing session
	s2 := wf.GetOrCreate("session-1")
	if s1 != s2 {
		t.Error("GetOrCreate should return the same pointer for same session ID")
	}
}

func TestPPTWorkflowInitV1(t *testing.T) {
	wf := NewPPTWorkflow()
	s := wf.InitV1("legacy-session")

	if s.Phase != PPTParse {
		t.Errorf("v1.0 session phase = %s, want PARSE", s.Phase)
	}
	if s.Version != 1 {
		t.Errorf("v1.0 session version = %d, want 1", s.Version)
	}
	if s.ComposeEngine != "clone" {
		t.Errorf("v1.0 compose engine = %q, want %q", s.ComposeEngine, "clone")
	}
}

func TestPPTWorkflowNormalFlow(t *testing.T) {
	// P5.2.3: Full flow WAIT_MATERIAL → ... → DONE (v2.0)
	t.Run("v2.0", func(t *testing.T) {
		s := &PPTSession{Version: 2, Phase: PPTWaitMaterial, Artifacts: make(map[string]string)}

		expectedSequence := []PPTPhase{
			PPTPreprocess, PPTStyleSelect, PPTParse, PPTOutline, PPTDetail,
			PPTStyleMap, PPTRefine, PPTCompose, PPTQualityCheck, PPTPostProcess, PPTDone,
		}

		for _, target := range expectedSequence {
			if err := s.Transition(target); err != nil {
				t.Fatalf("Transition %s → %s failed: %v", s.Phase.String(), target.String(), err)
			}
		}
	})

	// P5.2.3: v1.0 backward compatible flow PARSE → ... → DONE
	t.Run("v1.0", func(t *testing.T) {
		s := &PPTSession{Version: 1, Phase: PPTParse, Artifacts: make(map[string]string)}

		expectedSequence := []PPTPhase{
			PPTOutline, PPTDetail, PPTStyleMap, PPTCompose, PPTDone,
		}

		for _, target := range expectedSequence {
			if err := s.Transition(target); err != nil {
				t.Fatalf("Transition %s → %s failed: %v", s.Phase.String(), target.String(), err)
			}
		}
	})
}

func TestPPTWorkflowInvalidTransition(t *testing.T) {
	// P5.2.5: Illegal transition returns error
	t.Run("v2.0", func(t *testing.T) {
		s := &PPTSession{Version: 2, Phase: PPTWaitMaterial, Artifacts: make(map[string]string)}

		// Cannot jump from WAIT_MATERIAL to DETAIL
		err := s.Transition(PPTDetail)
		if err == nil {
			t.Error("Expected error for WAIT_MATERIAL → DETAIL transition")
		}

		// Cannot transition from DONE
		s.Phase = PPTDone
		err = s.Transition(PPTParse)
		if err == nil {
			t.Error("Expected error for DONE → PARSE transition")
		}

		// Cannot jump from PARSE to COMPOSE (must go through intermediate phases)
		s.Phase = PPTParse
		err = s.Transition(PPTCompose)
		if err == nil {
			t.Error("Expected error for PARSE → COMPOSE transition")
		}
	})

	t.Run("v1.0", func(t *testing.T) {
		s := &PPTSession{Version: 1, Phase: PPTParse, Artifacts: make(map[string]string)}

		// Cannot jump from PARSE to DETAIL
		err := s.Transition(PPTDetail)
		if err == nil {
			t.Error("Expected error for PARSE → DETAIL transition (v1.0)")
		}

		// Cannot transition from DONE
		s.Phase = PPTDone
		err = s.Transition(PPTParse)
		if err == nil {
			t.Error("Expected error for DONE → PARSE transition (v1.0)")
		}
	})
}

func TestPPTWorkflowArtifacts(t *testing.T) {
	// P5.2.6: Artifact paths correctly stored and retrieved
	s := &PPTSession{Phase: PPTParse, Artifacts: make(map[string]string)}

	s.SetArtifact(ArtifactTemplateStructure, "/tmp/ws/session-1/template_structure.json")
	s.SetArtifact(ArtifactOutline, "/tmp/ws/session-1/outline.json")

	if got := s.ArtifactPath(ArtifactTemplateStructure); got == "" {
		t.Error("Expected artifact path for template_structure.json")
	}
	if got := s.ArtifactPath("nonexistent"); got != "" {
		t.Error("Expected empty string for nonexistent artifact")
	}
}

func TestPPTWorkflowRemove(t *testing.T) {
	wf := NewPPTWorkflow()
	wf.GetOrCreate("session-1")
	wf.GetOrCreate("session-2")

	if len(wf.List()) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(wf.List()))
	}

	wf.Remove("session-1")
	if len(wf.List()) != 1 {
		t.Errorf("Expected 1 session after remove, got %d", len(wf.List()))
	}
}

func TestPPTWorkflowIntegration(t *testing.T) {
	// P5.2.7: Simulate v1.0 workflow with artifacts (backward compatible)
	t.Run("v1.0", func(t *testing.T) {
		wf := NewPPTWorkflow()
		s := wf.InitV1("integration-test-v1")

		// Phase 1: Template uploaded → Parse (InitV1 already set Phase=PPTParse)
		s.SetArtifact("template.pptx", "/tmp/template.pptx")
		s.SetArtifact(ArtifactTemplateStructure, "/tmp/template_structure.json")

		// Phase 2: Outline
		if err := s.Transition(PPTOutline); err != nil {
			t.Fatal("Phase 2 transition:", err)
		}
		s.SetArtifact(ArtifactOutline, "/tmp/outline.json")

		// Phase 3: Detail
		if err := s.Transition(PPTDetail); err != nil {
			t.Fatal("Phase 3 transition:", err)
		}
		s.SetArtifact(ArtifactDetailPlan, "/tmp/detail_plan.json")

		// Phase 4: Style mapping
		if err := s.Transition(PPTStyleMap); err != nil {
			t.Fatal("Phase 4 transition:", err)
		}
		s.SetArtifact(ArtifactStyleMapping, "/tmp/style_mapping.json")

		// Phase 5: Compose
		if err := s.Transition(PPTCompose); err != nil {
			t.Fatal("Phase 5 transition:", err)
		}
		s.SetArtifact(ArtifactOutput, "/tmp/result.pptx")

		// Phase 6: Done
		if err := s.Transition(PPTDone); err != nil {
			t.Fatal("Phase 6 transition:", err)
		}

		if s.Phase != PPTDone {
			t.Errorf("Final phase = %s, want DONE", s.Phase)
		}

		wf.Remove("integration-test-v1")
	})

	// P5.2.7: Simulate v2.0 full flow with artifacts
	t.Run("v2.0", func(t *testing.T) {
		wf := NewPPTWorkflow()
		s := wf.GetOrCreate("integration-test-v2")

		// Phase 0: Material provided
		s.MaterialFiles = []string{"/tmp/report.pdf", "https://example.com/data"}
		s.SetArtifact(ArtifactMaterialMD, "/tmp/material.md")

		// Phase 0.1: Preprocess
		if err := s.Transition(PPTPreprocess); err != nil {
			t.Fatal("Preprocess transition:", err)
		}

		// Phase A: Style selection
		s.Style = "business"
		if err := s.Transition(PPTStyleSelect); err != nil {
			t.Fatal("Style select transition:", err)
		}

		// Phase 1: Template parse (built-in style uses layout SVG)
		if err := s.Transition(PPTParse); err != nil {
			t.Fatal("Parse transition:", err)
		}
		s.SetArtifact(ArtifactTemplateStructure, "/tmp/v2_template_structure.json")

		// Phase 2: Outline
		if err := s.Transition(PPTOutline); err != nil {
			t.Fatal("Outline transition:", err)
		}
		s.SetArtifact(ArtifactOutline, "/tmp/v2_outline.json")
		s.SlideCount = 8

		// Phase 3: Detail
		if err := s.Transition(PPTDetail); err != nil {
			t.Fatal("Detail transition:", err)
		}
		s.SetArtifact(ArtifactDetailPlan, "/tmp/v2_detail_plan.json")

		// Phase 4: Style mapping
		if err := s.Transition(PPTStyleMap); err != nil {
			t.Fatal("Style map transition:", err)
		}
		s.SetArtifact(ArtifactStyleMapping, "/tmp/v2_style_mapping.json")

		// Phase 5: Refine (per-slide adjustments)
		if err := s.Transition(PPTRefine); err != nil {
			t.Fatal("Refine transition:", err)
		}
		s.SetArtifact(ArtifactSpecLock, "/tmp/spec_lock.md")

		// Phase 6: Compose (SVG engine)
		if err := s.Transition(PPTCompose); err != nil {
			t.Fatal("Compose transition:", err)
		}
		s.SetArtifact(ArtifactOutput, "/tmp/v2_result.pptx")

		// Phase 7: Quality check
		if err := s.Transition(PPTQualityCheck); err != nil {
			t.Fatal("Quality check transition:", err)
		}
		s.SetArtifact(ArtifactQualityReport, "/tmp/quality_report.json")

		// Phase 8: Post process
		if err := s.Transition(PPTPostProcess); err != nil {
			t.Fatal("Post process transition:", err)
		}
		s.SetArtifact(ArtifactAnimationConfig, "/tmp/animation_config.yaml")
		s.SetArtifact(ArtifactSpeakerNotes, "/tmp/speaker_notes.pptx")

		// Phase 9: Done
		if err := s.Transition(PPTDone); err != nil {
			t.Fatal("Done transition:", err)
		}

		if s.Phase != PPTDone {
			t.Errorf("Final phase = %s, want DONE", s.Phase)
		}
		if s.Version != 2 {
			t.Errorf("Version = %d, want 2", s.Version)
		}
		if s.ComposeEngine != "svg" {
			t.Errorf("ComposeEngine = %q, want %q", s.ComposeEngine, "svg")
		}

		wf.Remove("integration-test-v2")
	})
}

func TestPPTWorkflowSkipToParse(t *testing.T) {
	// v2.0 session at WAIT_MATERIAL → SkipToParse → PPTParse
	t.Run("v2.0 from WAIT_MATERIAL", func(t *testing.T) {
		s := &PPTSession{Version: 2, Phase: PPTWaitMaterial}
		if err := s.SkipToParse(); err != nil {
			t.Fatalf("SkipToParse failed: %v", err)
		}
		if s.Phase != PPTParse {
			t.Errorf("Phase = %s, want PARSE", s.Phase)
		}
	})

	// v2.0 session at PPTStyleSelect → SkipToParse → PPTParse
	t.Run("v2.0 from STYLE_SELECT", func(t *testing.T) {
		s := &PPTSession{Version: 2, Phase: PPTStyleSelect}
		if err := s.SkipToParse(); err != nil {
			t.Fatalf("SkipToParse failed: %v", err)
		}
		if s.Phase != PPTParse {
			t.Errorf("Phase = %s, want PARSE", s.Phase)
		}
	})

	// Already at PARSE → no-op
	t.Run("already at PARSE", func(t *testing.T) {
		s := &PPTSession{Version: 2, Phase: PPTParse}
		if err := s.SkipToParse(); err != nil {
			t.Fatalf("SkipToParse from PARSE should be no-op: %v", err)
		}
	})

	// Cannot skip from DONE
	t.Run("from DONE fails", func(t *testing.T) {
		s := &PPTSession{Version: 2, Phase: PPTDone}
		if err := s.SkipToParse(); err == nil {
			t.Error("Expected error for SkipToParse from DONE")
		}
	})

	// v1.0 session at PPTParse (already there)
	t.Run("v1.0 at PARSE", func(t *testing.T) {
		s := &PPTSession{Version: 1, Phase: PPTParse}
		if err := s.SkipToParse(); err != nil {
			t.Fatalf("v1.0 SkipToParse from PARSE should be no-op: %v", err)
		}
	})
}

func TestPPTWorkflowSetStyle(t *testing.T) {
	s := &PPTSession{Phase: PPTWaitMaterial}
	s.SetStyle("business")

	if s.Style != "business" {
		t.Errorf("Style = %q, want 'business'", s.Style)
	}
	if s.Version != 2 {
		t.Errorf("Version = %d, want 2", s.Version)
	}
	if s.ComposeEngine != "svg" {
		t.Errorf("ComposeEngine = %q, want 'svg'", s.ComposeEngine)
	}
}

func TestPPTWorkflowSetTemplate(t *testing.T) {
	s := &PPTSession{Phase: PPTWaitMaterial}
	s.SetTemplate("/tmp/template.pptx")

	if s.Template != "/tmp/template.pptx" {
		t.Errorf("Template = %q, want '/tmp/template.pptx'", s.Template)
	}
	if s.Version != 1 {
		t.Errorf("Version = %d, want 1", s.Version)
	}
	if s.ComposeEngine != "clone" {
		t.Errorf("ComposeEngine = %q, want 'clone'", s.ComposeEngine)
	}
}

func TestPPTWorkflowV1CompatFlow(t *testing.T) {
	// Simulate: plain text → skip to PARSE → normal v1.0 flow
	wf := NewPPTWorkflow()
	s := wf.GetOrCreate("compat-test")

	// User sends plain text (no file, no style) — skip material phases
	if err := s.SkipToParse(); err != nil {
		t.Fatal("SkipToParse:", err)
	}
	s.SetTemplate("/tmp/user_template.pptx")

	// Continue v1.0 flow
	expectedSequence := []PPTPhase{PPTOutline, PPTDetail, PPTStyleMap, PPTCompose, PPTDone}
	for _, target := range expectedSequence {
		if err := s.Transition(target); err != nil {
			t.Fatalf("Transition %s → %s failed: %v", s.Phase.String(), target.String(), err)
		}
	}

	if s.Phase != PPTDone {
		t.Errorf("Final phase = %s, want DONE", s.Phase)
	}
	wf.Remove("compat-test")
}
