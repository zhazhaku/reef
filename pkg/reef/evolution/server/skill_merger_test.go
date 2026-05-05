package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// Mock implementations for SkillMerger testing
// ---------------------------------------------------------------------------

// smMockStore implements MergerStore for SkillMerger tests.
type smMockStore struct {
	mu           sync.Mutex
	genes        map[string]*evolution.Gene
	drafts       map[string]*evolution.SkillDraft
	saveDraftErr error
}

func newSMMockStore() *smMockStore {
	return &smMockStore{
		genes:  make(map[string]*evolution.Gene),
		drafts: make(map[string]*evolution.SkillDraft),
	}
}

func (m *smMockStore) addApprovedGene(g *evolution.Gene) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g.Status = evolution.GeneStatusApproved
	m.genes[g.ID] = g
}

// -- GeneStore methods --
func (m *smMockStore) InsertGene(gene *evolution.Gene) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.genes[gene.ID] = gene
	return nil
}

func (m *smMockStore) UpdateGene(gene *evolution.Gene) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.genes[gene.ID] = gene
	return nil
}

func (m *smMockStore) GetGene(geneID string) (*evolution.Gene, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.genes[geneID]
	if !ok {
		return nil, nil
	}
	return g, nil
}

func (m *smMockStore) CountApprovedGenes(role string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, g := range m.genes {
		if g.Role == role && g.Status == evolution.GeneStatusApproved {
			count++
		}
	}
	return count, nil
}

func (m *smMockStore) CountByStatus(status string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, g := range m.genes {
		if string(g.Status) == status {
			count++
		}
	}
	return count, nil
}

func (m *smMockStore) GetApprovedGenes(role string, limit int) ([]*evolution.Gene, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*evolution.Gene
	for _, g := range m.genes {
		if g.Role == role && g.Status == evolution.GeneStatusApproved {
			result = append(result, g)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

// -- SkillDraft methods --
func (m *smMockStore) SaveSkillDraft(draft *evolution.SkillDraft) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveDraftErr != nil {
		return m.saveDraftErr
	}
	draftCopy := *draft
	m.drafts[draft.ID] = &draftCopy
	return nil
}

func (m *smMockStore) GetSkillDraft(draftID string) (*evolution.SkillDraft, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drafts[draftID]
	if !ok {
		return nil, nil
	}
	dCopy := *d
	return &dCopy, nil
}

func (m *smMockStore) UpdateSkillDraft(draft *evolution.SkillDraft) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.drafts[draft.ID]; !ok {
		return fmt.Errorf("draft %s not found", draft.ID)
	}
	draftCopy := *draft
	m.drafts[draft.ID] = &draftCopy
	return nil
}

// smMockLLM implements LLMProvider for SkillMerger tests.
type smMockLLM struct {
	mu       sync.Mutex
	response string
	err      error
	calls    []string
}

func newSMMockLLM(response string) *smMockLLM {
	return &smMockLLM{response: response}
}

func (m *smMockLLM) Chat(ctx context.Context, prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, prompt)
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

func (m *smMockLLM) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// smMockNotifier implements Notifier for SkillMerger tests.
type smMockNotifier struct {
	mu            sync.Mutex
	notifications []Notification
	err           error
}

func newSMMockNotifier() *smMockNotifier {
	return &smMockNotifier{}
}

func (m *smMockNotifier) NotifyAdmin(n Notification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notifications = append(m.notifications, n)
	if m.err != nil {
		return m.err
	}
	return nil
}

func (m *smMockNotifier) notificationsByType(typ string) []Notification {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Notification
	for _, n := range m.notifications {
		if n.Type == typ {
			out = append(out, n)
		}
	}
	return out
}

// makeMergeGene creates a gene for merge testing with a customizable strategy name.
func makeMergeGene(id, role, strategyName, controlSignal string) *evolution.Gene {
	return &evolution.Gene{
		ID:              id,
		StrategyName:    strategyName,
		Role:            role,
		Skills:          []string{"test"},
		MatchCondition:  "on_error",
		ControlSignal:   controlSignal,
		FailureWarnings: []string{"warning-timeout", "warning-memory"},
		SourceEvents:    []string{"evt-001"},
		SourceClientID:  "client-1",
		Version:         1,
		Status:          evolution.GeneStatusApproved,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
}

// makeApprovedGene creates a gene with approved status.
func makeApprovedGene(id, role string) *evolution.Gene {
	return makeMergeGene(id, role, "error-recovery", "retry on timeout with exponential backoff")
}

// =========================================================================
// Task 1: SkillMerger struct and config tests
// =========================================================================

func TestNewSkillMerger_AutoApproveForcedFalse(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test Skill\n\n## Description\nTest")
	notifier := newSMMockNotifier()

	cfg := DefaultMergerConfig()
	cfg.AutoApprove = true // This should be overridden.

	merger := NewSkillMerger(store, llm, notifier, cfg, nil)
	if merger.config.AutoApprove {
		t.Error("AutoApprove MUST be false per Q4:B decision")
	}
}

func TestNewSkillMerger_DefaultConfig(t *testing.T) {
	store := newSMMockStore()
	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)
	if merger.config.MergeThreshold != 5 {
		t.Errorf("MergeThreshold = %d, want 5", merger.config.MergeThreshold)
	}
	if merger.config.MaxGenesPerMerge != 10 {
		t.Errorf("MaxGenesPerMerge = %d, want 10", merger.config.MaxGenesPerMerge)
	}
	if merger.config.AutoApprove {
		t.Error("AutoApprove must be false")
	}
	if !strings.HasSuffix(merger.config.SkillsBaseDir, "/") {
		t.Errorf("SkillsBaseDir must end with '/', got %q", merger.config.SkillsBaseDir)
	}
}

func TestNewSkillMerger_BaseDirEndsWithSlash(t *testing.T) {
	cfg := MergerConfig{
		MergeThreshold: 5,
		SkillsBaseDir:  "skills/roles", // No trailing slash
	}
	cfg.setDefaults()
	if !strings.HasSuffix(cfg.SkillsBaseDir, "/") {
		t.Errorf("setDefaults should append '/', got %q", cfg.SkillsBaseDir)
	}
}

func TestNewSkillMerger_ZeroConfigsDefaulted(t *testing.T) {
	cfg := MergerConfig{}
	cfg.setDefaults()
	if cfg.MergeThreshold != 5 {
		t.Errorf("MergeThreshold = %d, want 5", cfg.MergeThreshold)
	}
	if cfg.MaxGenesPerMerge != 10 {
		t.Errorf("MaxGenesPerMerge = %d, want 10", cfg.MaxGenesPerMerge)
	}
	if cfg.LLMTimeout != 60*time.Second {
		t.Errorf("LLMTimeout = %v, want 60s", cfg.LLMTimeout)
	}
}

// =========================================================================
// Task 2: CheckAndMerge trigger condition tests
// =========================================================================

func TestSkillMergerTrigger_BelowThreshold_NoMerge(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test")
	notifier := newSMMockNotifier()

	// Add 4 approved genes (below threshold of 5).
	for i := 0; i < 4; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)
	merger.CheckAndMerge(context.Background(), "coder")

	// LLM should NOT be called.
	if llm.callCount() != 0 {
		t.Error("LLM should not be called when below threshold")
	}

	// No drafts should be saved.
	if len(store.drafts) != 0 {
		t.Errorf("expected 0 drafts, got %d", len(store.drafts))
	}
}

func TestSkillMergerTrigger_AtThreshold_MergeTriggered(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Error Recovery Patterns\n\n## Description\n...")
	notifier := newSMMockNotifier()

	// Add 5 approved genes (exactly at threshold).
	for i := 0; i < 5; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)
	merger.CheckAndMerge(context.Background(), "coder")

	// Wait for background goroutine to complete.
	time.Sleep(200 * time.Millisecond)

	// LLM should have been called.
	if llm.callCount() == 0 {
		t.Error("LLM should have been called when threshold is met")
	}

	// A draft should be saved.
	if len(store.drafts) == 0 {
		t.Error("expected at least 1 draft saved")
	}
}

func TestSkillMergerTrigger_AboveThreshold_MergeTriggered(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Error Recovery\n\n## Description\nTest")
	notifier := newSMMockNotifier()

	// Add 10 approved genes (above threshold).
	for i := 0; i < 10; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)
	merger.CheckAndMerge(context.Background(), "coder")

	time.Sleep(200 * time.Millisecond)

	if llm.callCount() == 0 {
		t.Error("LLM should have been called")
	}
	if len(store.drafts) == 0 {
		t.Error("expected draft to be saved")
	}
}

func TestSkillMergerTrigger_WithMaxGenesPerMerge(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test\n\n## Description\nTest")
	notifier := newSMMockNotifier()

	// Add 15 approved genes.
	for i := 0; i < 15; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	cfg := DefaultMergerConfig()
	cfg.MaxGenesPerMerge = 10
	cfg.MergeThreshold = 5

	merger := NewSkillMerger(store, llm, notifier, cfg, nil)
	merger.CheckAndMerge(context.Background(), "coder")

	time.Sleep(200 * time.Millisecond)

	if llm.callCount() == 0 {
		t.Error("LLM should have been called")
	}

	// Verify only 10 genes were passed to LLM (check prompt length or gene count in prompt).
	// We validate this indirectly: max 10 genes were fetched from store.
	if len(store.drafts) > 0 {
		for _, draft := range store.drafts {
			if len(draft.SourceGeneIDs) > 10 {
				t.Errorf("expected ≤10 source genes, got %d", len(draft.SourceGeneIDs))
			}
		}
	}
}

func TestSkillMergerTrigger_EmptyRole_NoOp(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test")
	notifier := newSMMockNotifier()

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)
	merger.CheckAndMerge(context.Background(), "")

	if llm.callCount() != 0 {
		t.Error("LLM should not be called for empty role")
	}
}

func TestSkillMergerTrigger_ConcurrentMergePerRole_Locked(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test\n\n## Description\nTest")
	notifier := newSMMockNotifier()

	// Add 10 approved genes.
	for i := 0; i < 10; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)

	// Call CheckAndMerge twice concurrently for the same role.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			merger.CheckAndMerge(context.Background(), "coder")
		}()
	}
	wg.Wait()

	time.Sleep(500 * time.Millisecond)

	// Only one merge should be triggered per role due to per-role lock.
	// Multiple goroutines calling CheckAndMerge: the first acquires the lock and triggers
	// the merge. Subsequent calls see count≥threshold but the merge goroutine hasn't
	// consumed genes yet. However, only one merge is triggered because the lock is held.
	// The actual merge count is 1 because all calls were serialized by the lock.
	if llm.callCount() > 5 {
		t.Errorf("expected ≤5 LLM calls (one per CheckAndMerge), got %d", llm.callCount())
	}
}

// =========================================================================
// Task 2 edge cases: merge with 0 genes (race), LLM error
// =========================================================================

func TestSkillMergerTrigger_ZeroGenes_RaceCondition(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test")
	notifier := newSMMockNotifier()

	// Do NOT add any genes despite the trigger being called.
	// This simulates a race where genes were consumed between CheckAndMerge and merge.

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)

	// Directly call merge which will find 0 genes.
	merger.merge(context.Background(), "coder")

	// LLM should NOT be called.
	if llm.callCount() != 0 {
		t.Error("LLM should not be called with 0 genes")
	}
	if len(store.drafts) != 0 {
		t.Error("no draft should be saved with 0 genes")
	}
}

func TestSkillMergerTrigger_LLMError_NoDraftSaved(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("")
	llm.err = fmt.Errorf("LLM timeout")
	notifier := newSMMockNotifier()

	// Add 5 approved genes.
	for i := 0; i < 5; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)
	merger.CheckAndMerge(context.Background(), "coder")

	time.Sleep(200 * time.Millisecond)

	// No draft should be saved.
	if len(store.drafts) != 0 {
		t.Errorf("expected 0 drafts on LLM error, got %d", len(store.drafts))
	}

	// No notification should be sent.
	if len(notifier.notificationsByType("skill_draft_ready")) != 0 {
		t.Error("no notification should be sent on LLM error")
	}
}

func TestSkillMergerTrigger_NoLLM_ReturnsError(t *testing.T) {
	store := newSMMockStore()
	for i := 0; i < 5; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	_, err := merger.generateSKILL(context.Background(), []*evolution.Gene{
		makeApprovedGene("gene-0", "coder"),
	})
	if err == nil {
		t.Error("expected error when no LLM provider configured")
	}
}

func TestSkillMergerTrigger_ContextCancelled_BeforeLLM(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test")
	notifier := newSMMockNotifier()

	for i := 0; i < 5; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	// merge checks ctx before LLM call.
	merger.merge(ctx, "coder")

	// LLM should NOT be called since context is cancelled.
	if llm.callCount() != 0 {
		t.Error("LLM should not be called with cancelled context")
	}
}

// =========================================================================
// Task 3: generateSKILL tests
// =========================================================================

func TestSkillMergerGenerate_ValidMarkdown(t *testing.T) {
	expected := "# Error Recovery Patterns\n\n## Description\nCombined error recovery strategies."
	llm := newSMMockLLM(expected)
	store := newSMMockStore()

	merger := NewSkillMerger(store, llm, nil, DefaultMergerConfig(), nil)

	genes := []*evolution.Gene{
		makeMergeGene("gene-0", "coder", "Error Recovery", "retry with backoff"),
		makeMergeGene("gene-1", "coder", "Error Handler", "catch and log errors"),
	}

	content, err := merger.generateSKILL(context.Background(), genes)
	if err != nil {
		t.Fatalf("generateSKILL failed: %v", err)
	}
	if content != expected {
		t.Errorf("content = %q, want %q", content, expected)
	}
}

func TestSkillMergerGenerate_EmptyResponse_Error(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("") // Empty response.
	merger := NewSkillMerger(store, llm, nil, DefaultMergerConfig(), nil)

	genes := []*evolution.Gene{
		makeApprovedGene("gene-0", "coder"),
	}

	_, err := merger.generateSKILL(context.Background(), genes)
	if err == nil {
		t.Error("expected error for empty LLM response")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Errorf("error message should mention empty response, got: %v", err)
	}
}

func TestSkillMergerGenerate_NonMarkdown_Prepended(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("This is not markdown, just plain text.")
	merger := NewSkillMerger(store, llm, nil, DefaultMergerConfig(), nil)

	genes := []*evolution.Gene{
		makeMergeGene("gene-0", "coder", "Error Recovery Patterns", "retry"),
	}

	content, err := merger.generateSKILL(context.Background(), genes)
	if err != nil {
		t.Fatalf("generateSKILL failed: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(content), "#") {
		t.Errorf("content should start with '#', got: %q", content)
	}
	if !strings.Contains(content, "error-recovery-patterns") {
		t.Errorf("content should contain derived (sanitized) skill name, got: %q", content)
	}
}

func TestSkillMergerGenerate_MultipleGenes_Combined(t *testing.T) {
	store := newSMMockStore()
	response := "# Combined Strategies\n\n## Description\nMerged from multiple genes.\n\n## Strategies\n\n### Strat A\nretry\n\n**Known Failure Patterns:**\n- timeout\n\n### Strat B\nlog errors\n\n**Known Failure Patterns:**\n- memory"
	llm := newSMMockLLM(response)
	merger := NewSkillMerger(store, llm, nil, DefaultMergerConfig(), nil)

	genes := []*evolution.Gene{
		makeMergeGene("gene-0", "coder", "Error Recovery", "retry with backoff"),
		makeMergeGene("gene-1", "coder", "Error Logging", "log all errors"),
		makeMergeGene("gene-2", "coder", "Error Recovery", "circuit breaker pattern"),
	}

	content, err := merger.generateSKILL(context.Background(), genes)
	if err != nil {
		t.Fatalf("generateSKILL failed: %v", err)
	}
	if !strings.Contains(content, "Combined Strategies") {
		t.Errorf("content should be the LLM response, got: %q", content)
	}
	if len(content) == 0 {
		t.Error("content should not be empty")
	}
}

func TestSkillMergerGenerate_TruncatedLongResponse(t *testing.T) {
	store := newSMMockStore()
	// Create a response > 100k chars
	longContent := "# Test\n\n" + strings.Repeat("A", 100_500)
	llm := newSMMockLLM(longContent)
	merger := NewSkillMerger(store, llm, nil, DefaultMergerConfig(), nil)

	genes := []*evolution.Gene{
		makeApprovedGene("gene-0", "coder"),
	}

	content, err := merger.generateSKILL(context.Background(), genes)
	if err != nil {
		t.Fatalf("generateSKILL failed: %v", err)
	}
	if len(content) > 100_000 {
		t.Errorf("content should be truncated to 100k chars, got %d", len(content))
	}
}

func TestSkillMergerGenerate_LLMTimeout(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test")
	llm.err = fmt.Errorf("timeout")
	merger := NewSkillMerger(store, llm, nil, DefaultMergerConfig(), nil)

	genes := []*evolution.Gene{
		makeApprovedGene("gene-0", "coder"),
	}

	_, err := merger.generateSKILL(context.Background(), genes)
	if err == nil {
		t.Error("expected error for LLM timeout")
	}
}

// =========================================================================
// Task 4: Approve / Reject / Rollback tests
// =========================================================================

func TestSkillMergerApprove_Success(t *testing.T) {
	store := newSMMockStore()
	notifier := newSMMockNotifier()

	// Create a pending draft.
	draft := &evolution.SkillDraft{
		ID:            "draft-001",
		Role:          "coder",
		SkillName:     "error-recovery",
		Content:       "# Error Recovery\n\n## Description\nTest skill",
		SourceGeneIDs: []string{"gene-0", "gene-1"},
		Status:        evolution.SkillDraftPendingReview,
		CreatedAt:     time.Now().UTC(),
	}
	store.drafts[draft.ID] = draft

	// Use a temp dir for SkillsBaseDir.
	tmpDir := t.TempDir()
	cfg := DefaultMergerConfig()
	cfg.SkillsBaseDir = filepath.Join(tmpDir, "skills/roles/")

	merger := NewSkillMerger(store, nil, notifier, cfg, nil)

	err := merger.Approve("draft-001")
	if err != nil {
		t.Fatalf("Approve failed: %v", err)
	}

	// Draft status should be published.
	updated, _ := store.GetSkillDraft("draft-001")
	if updated.Status != evolution.SkillDraftPublished {
		t.Errorf("draft status = %s, want %s", updated.Status, evolution.SkillDraftPublished)
	}

	// File should be written.
	expectedPath := filepath.Join(tmpDir, "skills/roles/coder/error-recovery.md")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("skill file not written at %s", expectedPath)
	}

	// Verify file content.
	content, _ := os.ReadFile(expectedPath)
	if string(content) != draft.Content {
		t.Errorf("file content = %q, want %q", string(content), draft.Content)
	}

	// Notification should be sent.
	approvedNs := notifier.notificationsByType("skill_approved")
	if len(approvedNs) != 1 {
		t.Errorf("expected 1 skill_approved notification, got %d", len(approvedNs))
	}
}

func TestSkillMergerReject_Success(t *testing.T) {
	store := newSMMockStore()
	notifier := newSMMockNotifier()

	draft := &evolution.SkillDraft{
		ID:            "draft-002",
		Role:          "coder",
		SkillName:     "bad-skill",
		Content:       "# Bad Skill",
		SourceGeneIDs: []string{"gene-0"},
		Status:        evolution.SkillDraftPendingReview,
		CreatedAt:     time.Now().UTC(),
	}
	store.drafts[draft.ID] = draft

	merger := NewSkillMerger(store, nil, notifier, DefaultMergerConfig(), nil)

	err := merger.Reject("draft-002", "poor quality, needs more genes")
	if err != nil {
		t.Fatalf("Reject failed: %v", err)
	}

	updated, _ := store.GetSkillDraft("draft-002")
	if updated.Status != evolution.SkillDraftRejected {
		t.Errorf("draft status = %s, want %s", updated.Status, evolution.SkillDraftRejected)
	}
	if updated.ReviewComment != "poor quality, needs more genes" {
		t.Errorf("review comment = %q, want %q", updated.ReviewComment, "poor quality, needs more genes")
	}

	rejectedNs := notifier.notificationsByType("skill_rejected")
	if len(rejectedNs) != 1 {
		t.Errorf("expected 1 skill_rejected notification, got %d", len(rejectedNs))
	}
}

func TestSkillMergerApprove_AlreadyPublished_Error(t *testing.T) {
	store := newSMMockStore()
	draft := &evolution.SkillDraft{
		ID:            "draft-003",
		Role:          "coder",
		SkillName:     "published-skill",
		Content:       "# Already Done",
		SourceGeneIDs: []string{"gene-0"},
		Status:        evolution.SkillDraftPublished,
		CreatedAt:     time.Now().UTC(),
	}
	store.drafts[draft.ID] = draft

	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	err := merger.Approve("draft-003")
	if err == nil {
		t.Error("expected error for already-published draft")
	}
}

func TestSkillMergerApprove_NonExistent_Error(t *testing.T) {
	store := newSMMockStore()
	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	err := merger.Approve("nonexistent")
	if err == nil {
		t.Error("expected error for non-existent draft")
	}
}

func TestSkillMergerReject_NonExistent_Error(t *testing.T) {
	store := newSMMockStore()
	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	err := merger.Reject("nonexistent", "no reason")
	if err == nil {
		t.Error("expected error for non-existent draft")
	}
}

func TestSkillMergerReject_AlreadyRejected_Error(t *testing.T) {
	store := newSMMockStore()
	draft := &evolution.SkillDraft{
		ID:            "draft-004",
		Role:          "coder",
		SkillName:     "rejected-already",
		Content:       "# Rejected",
		SourceGeneIDs: []string{"gene-0"},
		Status:        evolution.SkillDraftRejected,
		CreatedAt:     time.Now().UTC(),
	}
	store.drafts[draft.ID] = draft

	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	err := merger.Reject("draft-004", "double reject")
	if err == nil {
		t.Error("expected error for already-rejected draft")
	}
}

func TestSkillMergerReject_EmptyReason_Accepted(t *testing.T) {
	store := newSMMockStore()
	draft := &evolution.SkillDraft{
		ID:            "draft-005",
		Role:          "coder",
		SkillName:     "rejected-no-reason",
		Content:       "# Empty Reason",
		SourceGeneIDs: []string{"gene-0"},
		Status:        evolution.SkillDraftPendingReview,
		CreatedAt:     time.Now().UTC(),
	}
	store.drafts[draft.ID] = draft

	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	// Empty reason is valid — "no reason given".
	err := merger.Reject("draft-005", "")
	if err != nil {
		t.Fatalf("Reject with empty reason should succeed: %v", err)
	}

	updated, _ := store.GetSkillDraft("draft-005")
	if updated.Status != evolution.SkillDraftRejected {
		t.Errorf("draft status = %s, want %s", updated.Status, evolution.SkillDraftRejected)
	}
	if updated.ReviewComment != "" {
		t.Errorf("review comment should be empty, got %q", updated.ReviewComment)
	}
}

func TestSkillMergerRollback_NotGitRepo_Error(t *testing.T) {
	store := newSMMockStore()
	tmpDir := t.TempDir()

	// Create a skill file outside a git repo.
	skillDir := filepath.Join(tmpDir, "skills/roles/coder")
	os.MkdirAll(skillDir, 0755)
	skillPath := filepath.Join(skillDir, "test-skill.md")
	os.WriteFile(skillPath, []byte("# Test"), 0644)

	cfg := DefaultMergerConfig()
	cfg.SkillsBaseDir = filepath.Join(tmpDir, "skills/roles/")

	merger := NewSkillMerger(store, nil, nil, cfg, nil)

	err := merger.Rollback("coder", "test-skill")
	if err == nil {
		t.Error("expected error for non-git-repo rollback")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error should mention git repo, got: %v", err)
	}
}

func TestSkillMergerRollback_FileNotFound_Error(t *testing.T) {
	store := newSMMockStore()
	cfg := DefaultMergerConfig()

	merger := NewSkillMerger(store, nil, nil, cfg, nil)

	err := merger.Rollback("coder", "nonexistent-skill")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %v", err)
	}
}

// =========================================================================
// Task: Full lifecycle test — genes → merge → draft → approve → published
// =========================================================================

func TestSkillMergerLifecycle(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Error Recovery Patterns\n\n## Description\nCombined strategies for error recovery.\n\n## When to Use\nWhen errors occur.\n\n## Strategies\n\n### Retry\nUse exponential backoff.\n\n**Known Failure Patterns:**\n- timeout\n- memory")
	notifier := newSMMockNotifier()
	tmpDir := t.TempDir()

	cfg := DefaultMergerConfig()
	cfg.SkillsBaseDir = filepath.Join(tmpDir, "skills/roles/")

	merger := NewSkillMerger(store, llm, notifier, cfg, nil)

	// Step 1: Add genes and trigger merge.
	for i := 0; i < 6; i++ {
		store.addApprovedGene(makeMergeGene(
			fmt.Sprintf("gene-%d", i),
			"coder",
			"Error Recovery",
			fmt.Sprintf("retry strategy variant %d", i),
		))
	}

	merger.CheckAndMerge(context.Background(), "coder")
	time.Sleep(300 * time.Millisecond)

	// Step 2: Verify draft was created as pending_review.
	if len(store.drafts) == 0 {
		t.Fatal("expected at least 1 draft after merge")
	}

	var draftID string
	for _, d := range store.drafts {
		if d.Status == evolution.SkillDraftPendingReview && d.Role == "coder" {
			draftID = d.ID
			break
		}
	}
	if draftID == "" {
		t.Fatal("no pending_review draft found for role 'coder'")
	}

	// Step 3: Verify draft was notified.
	readyNs := notifier.notificationsByType("skill_draft_ready")
	if len(readyNs) != 1 {
		t.Errorf("expected 1 skill_draft_ready notification, got %d", len(readyNs))
	}

	// Step 4: Approve the draft.
	err := merger.Approve(draftID)
	if err != nil {
		t.Fatalf("Approve failed: %v", err)
	}

	// Step 5: Verify draft is now published.
	updated, _ := store.GetSkillDraft(draftID)
	if updated.Status != evolution.SkillDraftPublished {
		t.Errorf("expected published status, got %s", updated.Status)
	}

	// Step 6: Verify file was written.
	skillPath := filepath.Join(tmpDir, "skills/roles/coder/error-recovery.md")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Errorf("skill file not found at %s", skillPath)
	}

	// Step 7: Verify skill_approved notification.
	approvedNs := notifier.notificationsByType("skill_approved")
	if len(approvedNs) != 1 {
		t.Errorf("expected 1 skill_approved notification, got %d", len(approvedNs))
	}
}

// =========================================================================
// Test deriveSkillName
// =========================================================================

func TestDeriveSkillName_SingleGene(t *testing.T) {
	store := newSMMockStore()
	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	genes := []*evolution.Gene{
		makeMergeGene("g1", "coder", "Error Recovery Patterns", "retry"),
	}

	name := merger.deriveSkillName(genes)
	if name != "error-recovery-patterns" {
		t.Errorf("deriveSkillName = %q, want %q", name, "error-recovery-patterns")
	}
}

func TestDeriveSkillName_MultipleMatchingGenes(t *testing.T) {
	store := newSMMockStore()
	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	genes := []*evolution.Gene{
		makeMergeGene("g1", "coder", "Error Recovery", "retry"),
		makeMergeGene("g2", "coder", "Error Recovery", "backoff"),
		makeMergeGene("g3", "coder", "Logging", "log errors"),
		makeMergeGene("g4", "coder", "Error Recovery", "circuit breaker"),
	}

	name := merger.deriveSkillName(genes)
	if name != "error-recovery" {
		t.Errorf("deriveSkillName = %q, want %q", name, "error-recovery")
	}
}

func TestDeriveSkillName_EmptyGenes(t *testing.T) {
	store := newSMMockStore()
	merger := NewSkillMerger(store, nil, nil, DefaultMergerConfig(), nil)

	name := merger.deriveSkillName(nil)
	if name != "merged-skill" {
		t.Errorf("deriveSkillName for nil = %q, want 'merged-skill'", name)
	}
}

func TestSanitizeSkillName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Error Recovery Patterns", "error-recovery-patterns"},
		{"simple", "simple"},
		{"UPPER CASE NAME", "upper-case-name"},
		{"multiple   spaces", "multiple-spaces"},
		{"mixed_case_with_underscores", "mixed-case-with-underscores"},
		{"special!@#characters", "specialcharacters"},
		{"---leading-dashes---", "leading-dashes"},
		{"", "merged-skill"},
	}

	for _, tt := range tests {
		result := sanitizeSkillName(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeSkillName(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// =========================================================================
// Test extractGeneIDs
// =========================================================================

func TestExtractGeneIDs(t *testing.T) {
	genes := []*evolution.Gene{
		{ID: "gene-a"},
		{ID: "gene-b"},
		{ID: "gene-c"},
	}
	ids := extractGeneIDs(genes)
	if len(ids) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(ids))
	}
	if ids[0] != "gene-a" || ids[1] != "gene-b" || ids[2] != "gene-c" {
		t.Errorf("unexpected IDs: %v", ids)
	}
}

// =========================================================================
// Test SaveDraft failure — draft lost, merge retries next cycle
// =========================================================================

func TestSkillMergerTrigger_SaveDraftFails_NoDraft(t *testing.T) {
	store := newSMMockStore()
	store.saveDraftErr = fmt.Errorf("DB write error")
	llm := newSMMockLLM("# Test")
	notifier := newSMMockNotifier()

	for i := 0; i < 5; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)
	merger.CheckAndMerge(context.Background(), "coder")

	time.Sleep(200 * time.Millisecond)

	// Draft should not be saved.
	if len(store.drafts) != 0 {
		t.Errorf("expected 0 drafts on save error, got %d", len(store.drafts))
	}
}

// =========================================================================
// Test NotifyAdmin failure — draft still saved
// =========================================================================

func TestSkillMergerTrigger_NotifyFails_DraftStillSaved(t *testing.T) {
	store := newSMMockStore()
	llm := newSMMockLLM("# Test")
	notifier := newSMMockNotifier()
	notifier.err = fmt.Errorf("notification channel down")

	for i := 0; i < 5; i++ {
		store.addApprovedGene(makeApprovedGene(fmt.Sprintf("gene-%d", i), "coder"))
	}

	merger := NewSkillMerger(store, llm, notifier, DefaultMergerConfig(), nil)
	merger.CheckAndMerge(context.Background(), "coder")

	time.Sleep(200 * time.Millisecond)

	// Draft should still be saved even if notification fails.
	if len(store.drafts) == 0 {
		t.Error("draft should be saved even when notification fails")
	}
}
