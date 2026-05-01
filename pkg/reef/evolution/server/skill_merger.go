// Package server implements the server-side evolution engine components.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// MergerStore — persistence interface required by SkillMergerImpl
// ---------------------------------------------------------------------------

// MergerStore combines GeneStore with SkillDraft persistence methods.
// Implemented by store.SQLiteStore and store.MemoryStore (extended).
type MergerStore interface {
	GeneStore
	SaveSkillDraft(draft *evolution.SkillDraft) error
	GetSkillDraft(draftID string) (*evolution.SkillDraft, error)
	UpdateSkillDraft(draft *evolution.SkillDraft) error
}

// ---------------------------------------------------------------------------
// Notifier — notification interface for admin alerts
// ---------------------------------------------------------------------------

// Notifier is the notification interface for skill merger events.
// Implementations include notify.Manager adapter and mock notifiers.
type Notifier interface {
	NotifyAdmin(notification Notification) error
}

// Notification represents a notification event from the SkillMergerImpl.
type Notification struct {
	Type    string // "skill_draft_ready", "skill_approved", "skill_rejected"
	Title   string
	Body    string
	DraftID string
	Role    string
}

// ---------------------------------------------------------------------------
// MergerConfig
// ---------------------------------------------------------------------------

// MergerConfig configures the SkillMergerImpl.
type MergerConfig struct {
	// MergeThreshold is the minimum number of approved genes needed to trigger a merge.
	// Default: 5.
	MergeThreshold int

	// SkillsBaseDir is the output directory for SKILL.md files.
	// Must end with "/". Default: "skills/roles/".
	SkillsBaseDir string

	// AutoApprove is a configuration field that exists for API compatibility
	// but is ALWAYS overridden to false per Q4:B decision. Setting it to true
	// will log a warning.
	AutoApprove bool

	// MaxGenesPerMerge is the maximum number of approved genes fed to the LLM per merge.
	// Default: 10.
	MaxGenesPerMerge int

	// LLMTimeout is the maximum duration for the LLM generation call.
	// Default: 60s.
	LLMTimeout time.Duration
}

// DefaultMergerConfig returns a MergerConfig with sensible defaults.
func DefaultMergerConfig() MergerConfig {
	return MergerConfig{
		MergeThreshold:   5,
		SkillsBaseDir:    "skills/roles/",
		AutoApprove:      false, // ALWAYS false per Q4:B
		MaxGenesPerMerge: 10,
		LLMTimeout:       60 * time.Second,
	}
}

// setDefaults applies default values for zero-valued fields.
func (c *MergerConfig) setDefaults() {
	if c.MergeThreshold <= 0 {
		c.MergeThreshold = 5
	}
	if c.SkillsBaseDir == "" {
		c.SkillsBaseDir = "skills/roles/"
	}
	if c.MaxGenesPerMerge <= 0 {
		c.MaxGenesPerMerge = 10
	}
	if c.LLMTimeout <= 0 {
		c.LLMTimeout = 60 * time.Second
	}
	// SkillsBaseDir MUST end with "/"
	if !strings.HasSuffix(c.SkillsBaseDir, "/") {
		c.SkillsBaseDir += "/"
	}
	// Q4:B: AutoApprove ALWAYS false
	if c.AutoApprove {
		c.AutoApprove = false
	}
}

// ---------------------------------------------------------------------------
// SkillMergerImpl
// ---------------------------------------------------------------------------

// SkillMergerImpl is the concrete implementation of the SkillMerger interface.
// It accumulates approved genes per role, triggers a merge when the
// threshold N≥5 is reached, uses LLM to generate a SKILL.md draft, saves it
// as pending_review, notifies admin, and provides approve/reject/rollback ops.
// AutoApprove is ALWAYS false per Q4:B decision.
type SkillMergerImpl struct {
	store      MergerStore
	llm        LLMProvider
	notifier   Notifier
	config     MergerConfig
	mu         sync.Mutex
	mergeLocks map[string]*sync.Mutex // per-role merge locks
	logger     *slog.Logger
}

// NewSkillMerger creates a new SkillMergerImpl.
// Logs a warning if config.AutoApprove is true (overridden to false).
func NewSkillMerger(
	store MergerStore,
	llm LLMProvider,
	notifier Notifier,
	config MergerConfig,
	logger *slog.Logger,
) *SkillMergerImpl {
	config.setDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	if config.AutoApprove {
		logger.Warn("AutoApprove is set to true in config — ALWAYS overridden to false per Q4:B")
		config.AutoApprove = false
	}
	return &SkillMergerImpl{
		store:      store,
		llm:        llm,
		notifier:   notifier,
		config:     config,
		mergeLocks: make(map[string]*sync.Mutex),
		logger:     logger,
	}
}

// getRoleLock returns the per-role merge lock, creating one if needed.
func (m *SkillMergerImpl) getRoleLock(role string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if lock, ok := m.mergeLocks[role]; ok {
		return lock
	}
	lock := &sync.Mutex{}
	m.mergeLocks[role] = lock
	return lock
}

// ---------------------------------------------------------------------------
// Task 2: CheckAndMerge — trigger condition and merge flow
// ---------------------------------------------------------------------------

// CheckAndMerge checks if the approved gene count for the given role meets the
// merge threshold and triggers a merge in a background goroutine if so.
// It implements the SkillMerger interface used by EvolutionHub.
func (m *SkillMergerImpl) CheckAndMerge(ctx context.Context, role string) {
	m.getRoleLock(role).Lock()
	defer m.getRoleLock(role).Unlock()

	if role == "" {
		m.logger.Warn("CheckAndMerge called with empty role, skipping")
		return
	}

	// Count approved genes for this role.
	count, err := m.store.CountApprovedGenes(role)
	if err != nil {
		m.logger.Error("failed to count approved genes",
			slog.String("role", role),
			slog.String("error", err.Error()))
		return
	}

	if count < m.config.MergeThreshold {
		m.logger.Debug("merge threshold not reached",
			slog.String("role", role),
			slog.Int("approved_count", count),
			slog.Int("threshold", m.config.MergeThreshold))
		return
	}

	m.logger.Info("triggering skill merge",
		slog.String("role", role),
		slog.Int("approved_count", count))

	// Launch merge in background goroutine.
	go m.merge(ctx, role)
}

// merge performs the actual gene→SKILL.md merge in a background goroutine.
func (m *SkillMergerImpl) merge(ctx context.Context, role string) {
	m.logger.Debug("merge started", slog.String("role", role))

	// Check context cancellation before expensive LLM call.
	select {
	case <-ctx.Done():
		m.logger.Warn("merge context cancelled before LLM call",
			slog.String("role", role),
			slog.String("error", ctx.Err().Error()))
		return
	default:
	}

	// Query approved genes (limited to MaxGenesPerMerge).
	genes, err := m.store.GetApprovedGenes(role, m.config.MaxGenesPerMerge)
	if err != nil {
		m.logger.Error("failed to get approved genes for merge",
			slog.String("role", role),
			slog.String("error", err.Error()))
		return
	}

	if len(genes) == 0 {
		// Race condition: count was ≥ threshold but genes were consumed/stale.
		m.logger.Warn("no approved genes found for merge (race condition)",
			slog.String("role", role))
		return
	}

	// Generate SKILL.md content via LLM.
	content, err := m.generateSKILL(ctx, genes)
	if err != nil {
		m.logger.Error("failed to generate SKILL.md",
			slog.String("role", role),
			slog.String("error", err.Error()))
		return
	}

	// Derive skill name from the genes.
	skillName := m.deriveSkillName(genes)

	// Create draft.
	draft := &evolution.SkillDraft{
		ID:            uuid.NewString(),
		Role:          role,
		SkillName:     skillName,
		Content:       content,
		SourceGeneIDs: extractGeneIDs(genes),
		Status:        evolution.SkillDraftPendingReview,
		CreatedAt:     time.Now().UTC(),
	}

	// Save draft.
	if err := m.store.SaveSkillDraft(draft); err != nil {
		m.logger.Error("failed to save skill draft",
			slog.String("draft_id", draft.ID),
			slog.String("role", role),
			slog.String("skill_name", skillName),
			slog.String("error", err.Error()))
		return
	}

	m.logger.Info("skill draft created",
		slog.String("draft_id", draft.ID),
		slog.String("role", role),
		slog.String("skill_name", skillName),
		slog.Int("gene_count", len(genes)))

	// Notify admin.
	if m.notifier != nil {
		notification := Notification{
			Type:    "skill_draft_ready",
			Title:   "New SKILL.md draft",
			Body:    fmt.Sprintf("Skill '%s' for role '%s' generated from %d genes", skillName, role, len(genes)),
			DraftID: draft.ID,
			Role:    role,
		}
		if err := m.notifier.NotifyAdmin(notification); err != nil {
			m.logger.Error("failed to notify admin about draft",
				slog.String("draft_id", draft.ID),
				slog.String("error", err.Error()))
			// Draft is saved — admin can discover it via admin endpoint.
		}
	}
}

// ---------------------------------------------------------------------------
// Task 3: generateSKILL — LLM prompt for gene→SKILL.md
// ---------------------------------------------------------------------------

// generateSKILL builds an LLM prompt from the given genes and generates a
// SKILL.md markdown document.
func (m *SkillMergerImpl) generateSKILL(ctx context.Context, genes []*evolution.Gene) (string, error) {
	if m.llm == nil {
		return "", fmt.Errorf("no LLM provider configured")
	}
	if len(genes) == 0 {
		return "", fmt.Errorf("no genes provided for SKILL generation")
	}

	// Derive skill name for use in the prompt and validation.
	skillName := m.deriveSkillName(genes)

	// Build system prompt.
	systemPrompt := buildSkillMergePrompt(genes, skillName)

	// Build user prompt with gene data.
	userPrompt := buildSkillMergeUserPrompt(genes)

	// Combine into a single prompt for the LLM.
	fullPrompt := systemPrompt + "\n\n" + userPrompt

	// Call LLM with timeout.
	ctx, cancel := context.WithTimeout(ctx, m.config.LLMTimeout)
	defer cancel()

	response, err := m.llm.Chat(ctx, fullPrompt)
	if err != nil {
		return "", fmt.Errorf("LLM call failed: %w", err)
	}

	// Edge case: empty response.
	if response == "" {
		return "", fmt.Errorf("empty response from LLM")
	}

	// Edge case: truncate excessively long responses.
	const maxChars = 100_000
	if len(response) > maxChars {
		m.logger.Warn("LLM response exceeds max chars, truncating",
			slog.Int("response_length", len(response)),
			slog.Int("max_chars", maxChars))
		response = response[:maxChars]
	}

	// Edge case: response doesn't start with "#" — prepend a title.
	if !strings.HasPrefix(strings.TrimSpace(response), "#") {
		response = fmt.Sprintf("# %s\n\n%s", skillName, response)
	}

	return response, nil
}

// buildSkillMergePrompt builds the system prompt for gene→SKILL.md conversion.
func buildSkillMergePrompt(genes []*evolution.Gene, skillName string) string {
	role := ""
	if len(genes) > 0 {
		role = genes[0].Role
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("You are converting a collection of approved Genes into a SKILL.md file for the '%s' role. ", role))
	sb.WriteString("Each Gene represents an evolved execution strategy. Combine them into a coherent skill document.\n\n")
	sb.WriteString("SKILL.md format:\n")
	sb.WriteString(fmt.Sprintf("# %s\n\n", skillName))
	sb.WriteString("## Description\n{brief description}\n\n")
	sb.WriteString("## When to Use\n{match conditions combined}\n\n")
	sb.WriteString("## Strategies\n\n")
	sb.WriteString("### {Strategy Name 1}\n{control_signal}\n\n")
	sb.WriteString("**Known Failure Patterns:**\n")
	sb.WriteString("- {failure_warnings as bullet list}\n\n")
	sb.WriteString("### {Strategy Name 2}\n...\n\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("1) Combine overlapping strategies.\n")
	sb.WriteString("2) Keep the document concise and actionable.\n")
	sb.WriteString("3) Preserve ALL failure warnings.\n")
	sb.WriteString("4) Use consistent formatting.\n")
	sb.WriteString("5) Output ONLY the SKILL.md content, no preamble or explanation.")

	return sb.String()
}

// buildSkillMergeUserPrompt builds the user prompt containing the gene data.
func buildSkillMergeUserPrompt(genes []*evolution.Gene) string {
	var sb strings.Builder
	sb.WriteString("Below are the approved Genes to merge:\n\n")

	for i, gene := range genes {
		sb.WriteString(fmt.Sprintf("## Gene %d: %s\n\n", i+1, gene.StrategyName))

		// MatchCondition
		matchCond := gene.MatchCondition
		if len(matchCond) > 3000 {
			matchCond = matchCond[:3000] + "..."
		}
		sb.WriteString(fmt.Sprintf("**MatchCondition:** %s\n\n", matchCond))

		// ControlSignal (truncated to 3000 chars if needed)
		controlSignal := gene.ControlSignal
		if len(controlSignal) > 3000 {
			controlSignal = controlSignal[:3000] + "..."
		}
		sb.WriteString(fmt.Sprintf("**ControlSignal:** %s\n\n", controlSignal))

		// FailureWarnings
		if len(gene.FailureWarnings) > 0 {
			sb.WriteString("**FailureWarnings:**\n")
			for _, fw := range gene.FailureWarnings {
				sb.WriteString(fmt.Sprintf("- %s\n", fw))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("Generate the SKILL.md document based on these genes.")
	return sb.String()
}

// ---------------------------------------------------------------------------
// deriveSkillName — derive a skill name from gene strategy names
// ---------------------------------------------------------------------------

// deriveSkillName creates a skill name from the most common strategy name
// prefix across the provided genes, sanitized to lowercase-dash format.
func (m *SkillMergerImpl) deriveSkillName(genes []*evolution.Gene) string {
	if len(genes) == 0 {
		return "merged-skill"
	}

	// Count strategy names (case-insensitive).
	counts := make(map[string]int)
	for _, g := range genes {
		lower := strings.ToLower(g.StrategyName)
		counts[lower]++
	}

	// Find the most common.
	var mostCommon string
	maxCount := 0
	for name, cnt := range counts {
		if cnt > maxCount {
			mostCommon = name
			maxCount = cnt
		}
	}

	if mostCommon == "" || maxCount == 0 {
		return "merged-skill"
	}

	// Sanitize: replace non-alphanumeric characters with dash.
	return sanitizeSkillName(mostCommon)
}

// sanitizeSkillName converts a raw name to lowercase-dash format.
// Example: "Error Recovery Patterns" → "error-recovery-patterns".
func sanitizeSkillName(name string) string {
	var sb strings.Builder
	prevDash := false

	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(unicode.ToLower(r))
			prevDash = false
		} else if r == ' ' || r == '_' || r == '-' {
			if !prevDash {
				sb.WriteRune('-')
				prevDash = true
			}
		}
	}

	result := strings.Trim(sb.String(), "-")
	if result == "" {
		return "merged-skill"
	}
	return result
}

// extractGeneIDs extracts the ID from each gene in a slice.
func extractGeneIDs(genes []*evolution.Gene) []string {
	ids := make([]string, len(genes))
	for i, g := range genes {
		ids[i] = g.ID
	}
	return ids
}

// ---------------------------------------------------------------------------
// Task 4: Approve / Reject / Rollback operations
// ---------------------------------------------------------------------------

// Approve approves a pending skill draft and writes it to disk.
// Returns an error if the draft is not found or not in pending_review status.
func (m *SkillMergerImpl) Approve(draftID string) error {
	// Fetch draft.
	draft, err := m.store.GetSkillDraft(draftID)
	if err != nil {
		return fmt.Errorf("get draft %s: %w", draftID, err)
	}
	if draft == nil {
		return fmt.Errorf("draft %s not found", draftID)
	}

	// Edge case: draft not in pending_review state.
	if draft.Status != evolution.SkillDraftPendingReview {
		return fmt.Errorf("draft %s not in pending_review state (current: %s)", draftID, draft.Status)
	}

	// Build file path: skills/roles/{role}/{skill_name}.md
	skillPath := filepath.Join(m.config.SkillsBaseDir, draft.Role, fmt.Sprintf("%s.md", draft.SkillName))

	// Create directory if not exists.
	dir := filepath.Dir(skillPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}

	// Write SKILL.md file.
	if err := os.WriteFile(skillPath, []byte(draft.Content), 0644); err != nil {
		return fmt.Errorf("write file %s: %w", skillPath, err)
	}

	// Update draft status.
	if err := draft.TransitionTo(evolution.SkillDraftApproved); err != nil {
		return fmt.Errorf("transition draft: %w", err)
	}
	if err := draft.TransitionTo(evolution.SkillDraftPublished); err != nil {
		return fmt.Errorf("transition to published: %w", err)
	}

	if err := m.store.UpdateSkillDraft(draft); err != nil {
		return fmt.Errorf("update draft status: %w", err)
	}

	m.logger.Info("skill approved and published",
		slog.String("draft_id", draftID),
		slog.String("skill_path", skillPath),
		slog.String("role", draft.Role),
		slog.String("skill_name", draft.SkillName))

	// Notify admin.
	if m.notifier != nil {
		notification := Notification{
			Type:    "skill_approved",
			Title:   "SKILL.md Approved",
			Body:    fmt.Sprintf("Skill '%s' for role '%s' has been approved and published to %s", draft.SkillName, draft.Role, skillPath),
			DraftID: draft.ID,
			Role:    draft.Role,
		}
		if err := m.notifier.NotifyAdmin(notification); err != nil {
			m.logger.Warn("failed to notify admin about approval",
				slog.String("draft_id", draftID),
				slog.String("error", err.Error()))
		}
	}

	return nil
}

// Reject rejects a pending skill draft with an optional reason.
// Returns an error if the draft is not found or not in pending_review state.
func (m *SkillMergerImpl) Reject(draftID string, reason string) error {
	// Fetch draft.
	draft, err := m.store.GetSkillDraft(draftID)
	if err != nil {
		return fmt.Errorf("get draft %s: %w", draftID, err)
	}
	if draft == nil {
		return fmt.Errorf("draft %s not found", draftID)
	}

	// Edge case: draft not in pending_review state.
	if draft.Status != evolution.SkillDraftPendingReview {
		return fmt.Errorf("draft %s not in pending_review state (current: %s)", draftID, draft.Status)
	}

	// Update draft.
	if err := draft.TransitionTo(evolution.SkillDraftRejected); err != nil {
		return fmt.Errorf("transition draft: %w", err)
	}
	draft.ReviewComment = reason

	if err := m.store.UpdateSkillDraft(draft); err != nil {
		return fmt.Errorf("update draft status: %w", err)
	}

	m.logger.Info("skill draft rejected",
		slog.String("draft_id", draftID),
		slog.String("role", draft.Role),
		slog.String("reason", reason))

	// Notify admin.
	if m.notifier != nil {
		notification := Notification{
			Type:    "skill_rejected",
			Title:   "SKILL.md Rejected",
			Body:    fmt.Sprintf("Skill '%s' for role '%s' has been rejected. Reason: %s", draft.SkillName, draft.Role, reason),
			DraftID: draft.ID,
			Role:    draft.Role,
		}
		if err := m.notifier.NotifyAdmin(notification); err != nil {
			m.logger.Warn("failed to notify admin about rejection",
				slog.String("draft_id", draftID),
				slog.String("error", err.Error()))
		}
	}

	return nil
}

// Rollback reverts a published SKILL.md file to its previous git version.
// Returns an error if git is not available or the file is not tracked.
func (m *SkillMergerImpl) Rollback(role, skillName string) error {
	// Build path: skills/roles/{role}/{skillName}.md
	skillPath := filepath.Join(m.config.SkillsBaseDir, role, fmt.Sprintf("%s.md", skillName))

	// Check if the file exists.
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		return fmt.Errorf("skill file not found: %s", skillPath)
	}

	// Determine the git repository root from the SkillsBaseDir.
	repoRoot := filepath.Dir(m.config.SkillsBaseDir)
	// If SkillsBaseDir is "skills/roles/", the repo root should be the project root.
	// Use git to find the repo root.
	absRepoRoot, err := findGitRoot(repoRoot)
	if err != nil {
		m.logger.Warn("git not available for rollback",
			slog.String("skill_path", skillPath),
			slog.String("error", err.Error()))
		return fmt.Errorf("not a git repository, rollback unavailable: %w", err)
	}

	// Build relative path within the repo.
	relPath, err := filepath.Rel(absRepoRoot, filepath.Join(repoRoot, skillPath))
	if err != nil {
		return fmt.Errorf("compute relative path: %w", err)
	}

	// Check if the file is tracked in git.
	cmd := exec.Command("git", "-C", absRepoRoot, "ls-files", "--error-unmatch", relPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("file not tracked by git: %s", relPath)
	}

	// Attempt git checkout of the previous version.
	checkoutCmd := exec.Command("git", "-C", absRepoRoot, "checkout", "HEAD~1", "--", relPath)
	output, err := checkoutCmd.CombinedOutput()
	if err != nil {
		m.logger.Error("git checkout failed",
			slog.String("skill_path", skillPath),
			slog.String("output", string(output)),
			slog.String("error", err.Error()))
		return fmt.Errorf("git checkout failed: %s — %w", strings.TrimSpace(string(output)), err)
	}

	m.logger.Info("skill rolled back via git",
		slog.String("skill_path", skillPath),
		slog.String("relative_path", relPath))

	return nil
}

// findGitRoot walks up from the given directory to find the git root.
func findGitRoot(startDir string) (string, error) {
	cmd := exec.Command("git", "-C", startDir, "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %s — %w", strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)), nil
}
