// Package agent — PPT Workflow State Machine (P5.2)
//
// Tracks the PPT generation lifecycle per session.
//
// v1.0 (6 phases):
//
//	WAIT_TEMPLATE → PARSE → OUTLINE → DETAIL → STYLE_MAP → COMPOSE → DONE
//
// v2.0 (11 phases, unified SVG engine):
//
//	WAIT_MATERIAL → PREPROCESS → STYLE_SELECT → PARSE → OUTLINE →
//	DETAIL → STYLE_MAP → REFINE → COMPOSE → QUALITY_CHECK → POST_PROCESS → DONE
//
// Backward compatibility: v1.0 sessions skip the first 3 phases and start at PARSE.
package agent

import (
	"fmt"
	"sync"
)

// PPTPhase enumerates the states of the PPT generation pipeline.
type PPTPhase int

const (
	// v2.0 new phases (Phase 0, Phase A)
	PPTWaitMaterial PPTPhase = iota // waiting for user to provide material (.pdf, URL, etc.)
	PPTPreprocess                   // running source_to_md converters
	PPTStyleSelect                  // user selects built-in style or uploads template

	// v1.0 phases (preserved order)
	PPTParse     // template parsing (v1.0: starts here)
	PPTOutline   // LLM generates slide outline
	PPTDetail    // LLM generates detailed content
	PPTStyleMap  // LLM maps content to template style
	PPTRefine    // per-slide refinement (v2.0)
	PPTCompose   // SVG editing + svg_to_pptx (v2.0 unified SVG engine)
	PPTQualityCheck // quality gate checks (text overflow, color, image, structure)
	PPTPostProcess  // animation injection, speaker notes
	PPTDone      // terminal state
)

// String returns the phase name for logging/debugging.
func (p PPTPhase) String() string {
	switch p {
	case PPTWaitMaterial:
		return "WAIT_MATERIAL"
	case PPTPreprocess:
		return "PREPROCESS"
	case PPTStyleSelect:
		return "STYLE_SELECT"
	case PPTParse:
		return "PARSE"
	case PPTOutline:
		return "OUTLINE"
	case PPTDetail:
		return "DETAIL"
	case PPTStyleMap:
		return "STYLE_MAP"
	case PPTRefine:
		return "REFINE"
	case PPTCompose:
		return "COMPOSE"
	case PPTQualityCheck:
		return "QUALITY_CHECK"
	case PPTPostProcess:
		return "POST_PROCESS"
	case PPTDone:
		return "DONE"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", p)
	}
}

// PPTSession tracks the state and artifacts of one PPT generation session.
type PPTSession struct {
	Phase      PPTPhase
	Version    int    // 1 = v1.0 (clone engine), 2 = v2.0 (SVG engine)
	Template   string // Path to uploaded .pptx template
	Style      string // Built-in style ID (academic, business, tech, creative, minimal)
	MaterialFiles []string // Input files for preprocessing (PDF, DOCX, URL, etc.)
	ComposeEngine string  // "svg" (v2.0) or "clone" (v1.0 fallback)
	Artifacts  map[string]string
	SlideCount int // Number of slides in the outline
}

// Artifact keys used in PPTSession.Artifacts.
const (
	// v1.0 artifacts
	ArtifactTemplateStructure = "template_structure.json"
	ArtifactOutline           = "outline.json"
	ArtifactDetailPlan        = "detail_plan.json"
	ArtifactStyleMapping      = "style_mapping.json"
	ArtifactOutput            = "output.pptx"

	// v2.0 artifacts
	ArtifactMaterialMD      = "material.md"       // merged preprocessed content
	ArtifactSpecLock        = "spec_lock.md"       // strategist design lock
	ArtifactDesignSpec      = "design_spec.md"     // strategist design spec
	ArtifactQualityReport   = "quality_report.json" // quality gate report
	ArtifactAnimationConfig = "animation_config.yaml"
	ArtifactSpeakerNotes    = "speaker_notes.pptx"
)

// PPTWorkflow manages PPT generation sessions.
// Thread-safe via mutex.
type PPTWorkflow struct {
	mu       sync.Mutex
	sessions map[string]*PPTSession // keyed by session ID
}

// NewPPTWorkflow creates a new workflow manager.
func NewPPTWorkflow() *PPTWorkflow {
	return &PPTWorkflow{
		sessions: make(map[string]*PPTSession),
	}
}

// GetOrCreate returns the session for the given ID, creating one if absent.
// New sessions default to v2.0 (Version=2, Phase=PPTWaitMaterial).
func (w *PPTWorkflow) GetOrCreate(sessionID string) *PPTSession {
	w.mu.Lock()
	defer w.mu.Unlock()

	if s, ok := w.sessions[sessionID]; ok {
		return s
	}

	s := &PPTSession{
		Version:       2,
		Phase:         PPTWaitMaterial,
		Artifacts:     make(map[string]string),
		ComposeEngine: "svg", // v2.0 unified SVG engine
	}
	w.sessions[sessionID] = s
	return s
}

// InitV1 initializes or resets a session to v1.0 mode for backward compatibility.
// Used when the user provides a template directly (skipping material/style phases).
func (w *PPTWorkflow) InitV1(sessionID string) *PPTSession {
	w.mu.Lock()
	defer w.mu.Unlock()

	s := &PPTSession{
		Version:       1,
		Phase:         PPTParse,
		Artifacts:     make(map[string]string),
		ComposeEngine: "clone",
	}
	w.sessions[sessionID] = s
	return s
}

// CanTransition checks if moving from current to target phase is valid.
// Supports both v1.0 (6-phase linear) and v2.0 (11-phase linear) flows.
func (s *PPTSession) CanTransition(target PPTPhase) bool {
	// v1.0 flow: PARSE → OUTLINE → DETAIL → STYLE_MAP → COMPOSE → DONE
	v1Transitions := map[PPTPhase][]PPTPhase{
		PPTParse:    {PPTOutline},
		PPTOutline:  {PPTDetail},
		PPTDetail:   {PPTStyleMap},
		PPTStyleMap: {PPTCompose},
		PPTCompose:  {PPTDone},
	}

	// v2.0 flow: WAIT_MATERIAL → PREPROCESS → STYLE_SELECT → PARSE → ...
	//           ... → OUTLINE → DETAIL → STYLE_MAP → REFINE → COMPOSE → ...
	//           ... → QUALITY_CHECK → POST_PROCESS → DONE
	v2Transitions := map[PPTPhase][]PPTPhase{
		PPTWaitMaterial: {PPTPreprocess},
		PPTPreprocess:   {PPTStyleSelect},
		PPTStyleSelect:  {PPTParse},
		PPTParse:        {PPTOutline},
		PPTOutline:      {PPTDetail},
		PPTDetail:       {PPTStyleMap},
		PPTStyleMap:     {PPTRefine},
		PPTRefine:       {PPTCompose},
		PPTCompose:      {PPTQualityCheck},
		PPTQualityCheck: {PPTPostProcess},
		PPTPostProcess:  {PPTDone},
	}

	if s.Version == 1 {
		allowed, ok := v1Transitions[s.Phase]
		if !ok {
			return false
		}
		for _, t := range allowed {
			if t == target {
				return true
			}
		}
		return false
	}

	// v2.0 (default)
	allowed, ok := v2Transitions[s.Phase]
	if !ok {
		return false
	}
	for _, t := range allowed {
		if t == target {
			return true
		}
	}
	return false
}

// Transition moves to the target phase if valid, returns error otherwise.
func (s *PPTSession) Transition(target PPTPhase) error {
	if !s.CanTransition(target) {
		return fmt.Errorf(
			"invalid transition: %s → %s",
			s.Phase.String(), target.String(),
		)
	}
	s.Phase = target
	return nil
}

// SkipToParse advances from WAIT_MATERIAL/PPTPreprocess/PPTStyleSelect
// directly to PPTParse. Used when no material preprocessing is needed
// (e.g., v1.0 compatibility or plain-text requests).
func (s *PPTSession) SkipToParse() error {
	if s.Phase == PPTParse {
		return nil // already there
	}
	if s.Version == 2 && (s.Phase == PPTWaitMaterial || s.Phase == PPTPreprocess || s.Phase == PPTStyleSelect) {
		s.Phase = PPTParse
		return nil
	}
	return fmt.Errorf("cannot skip to PARSE from %s", s.Phase.String())
}

// SetStyle records the selected built-in style for v2.0 sessions.
func (s *PPTSession) SetStyle(styleID string) {
	s.Style = styleID
	s.Version = 2
	s.ComposeEngine = "svg"
}

// SetTemplate records the uploaded template path and switches to v1.0 mode.
func (s *PPTSession) SetTemplate(path string) {
	s.Template = path
	s.Version = 1
	s.ComposeEngine = "clone"
}

// SetArtifact records a file path for a named artifact.
func (s *PPTSession) SetArtifact(name, path string) {
	if s.Artifacts == nil {
		s.Artifacts = make(map[string]string)
	}
	s.Artifacts[name] = path
}

// ArtifactPath returns the recorded path or empty string.
func (s *PPTSession) ArtifactPath(name string) string {
	return s.Artifacts[name]
}

// Remove removes a session (cleanup after completion).
func (w *PPTWorkflow) Remove(sessionID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.sessions, sessionID)
}

// List returns all active session IDs.
func (w *PPTWorkflow) List() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	ids := make([]string, 0, len(w.sessions))
	for id := range w.sessions {
		ids = append(ids, id)
	}
	return ids
}
