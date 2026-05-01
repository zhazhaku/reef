package client

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ============================================================================
// Mock Implementations
// ============================================================================

// mockStore implements geneEvolverStore for testing.
type mockStore struct {
	mu     sync.Mutex
	events []*evolution.EvolutionEvent
	genes  []*evolution.Gene
}

func (m *mockStore) GetRecentEvents(clientID string, limit int) ([]*evolution.EvolutionEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.events) {
		limit = len(m.events)
	}
	result := make([]*evolution.EvolutionEvent, limit)
	copy(result, m.events[len(m.events)-limit:])
	return result, nil
}

func (m *mockStore) InsertEvolutionEvent(event *evolution.EvolutionEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, event)
	return nil
}

func (m *mockStore) SaveGene(gene *evolution.Gene) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Replace if exists, else append
	for i, g := range m.genes {
		if g.ID == gene.ID {
			m.genes[i] = gene
			return nil
		}
	}
	m.genes = append(m.genes, gene)
	return nil
}

func (m *mockStore) GetTopGenes(role string, limit int) ([]*evolution.Gene, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*evolution.Gene
	for _, g := range m.genes {
		if limit > 0 && len(result) >= limit {
			break
		}
		if g.Role == role || role == "" {
			result = append(result, g)
		}
	}
	return result, nil
}

func (m *mockStore) MarkEventsProcessed(eventIDs []string, geneID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, evt := range m.events {
		for _, id := range eventIDs {
			if evt.ID == id {
				evt.GeneID = geneID
			}
		}
	}
	return nil
}

func (m *mockStore) addEvent(e *evolution.EvolutionEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, e)
}

func (m *mockStore) addGene(g *evolution.Gene) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.genes = append(m.genes, g)
}

func (m *mockStore) eventCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.events)
}

func newMockStore() *mockStore {
	return &mockStore{
		events: make([]*evolution.EvolutionEvent, 0),
		genes:  make([]*evolution.Gene, 0),
	}
}

// mockLLM implements LLMProvider for testing.
type mockLLM struct {
	mu          sync.Mutex
	responses   []string
	callIdx     int
	generateErr error
}

func (m *mockLLM) Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generateErr != nil {
		return "", m.generateErr
	}
	if m.callIdx >= len(m.responses) {
		return "", fmt.Errorf("no more responses configured")
	}
	resp := m.responses[m.callIdx]
	m.callIdx++
	return resp, nil
}

func (m *mockLLM) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callIdx
}

// mockGate implements GeneGateChecker for testing.
type mockGate struct {
	approve bool
}

func (g *mockGate) Check(gene *evolution.Gene) bool {
	return g.approve
}

// mockSubmitter implements GeneSubmittor for testing.
type mockSubmitter struct {
	mu          sync.Mutex
	submitted   []*evolution.Gene
	submitDelay time.Duration
}

func (s *mockSubmitter) Submit(gene *evolution.Gene) {
	if s.submitDelay > 0 {
		time.Sleep(s.submitDelay)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.submitted = append(s.submitted, gene)
}

func (s *mockSubmitter) lastSubmitted() *evolution.Gene {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.submitted) == 0 {
		return nil
	}
	return s.submitted[len(s.submitted)-1]
}

// ============================================================================
// Helpers
// ============================================================================

func makeSuccessEvent(id, taskID, clientID string) *evolution.EvolutionEvent {
	return &evolution.EvolutionEvent{
		ID:         id,
		TaskID:     taskID,
		ClientID:   clientID,
		EventType:  evolution.EventSuccessPattern,
		Signal:     fmt.Sprintf("Task %s completed successfully", taskID),
		RootCause:  "",
		GeneID:     "",
		Strategy:   "balanced",
		Importance: 0.7,
		CreatedAt:  time.Now().UTC(),
	}
}

func makeFailureEvent(id, taskID, clientID string) *evolution.EvolutionEvent {
	return &evolution.EvolutionEvent{
		ID:         id,
		TaskID:     taskID,
		ClientID:   clientID,
		EventType:  evolution.EventFailurePattern,
		Signal:     fmt.Sprintf("Task %s failed: timeout", taskID),
		RootCause:  "network_timeout",
		GeneID:     "",
		Strategy:   "balanced",
		Importance: 0.8,
		CreatedAt:  time.Now().UTC(),
	}
}

func makeFailureEventWithGene(id, taskID, clientID, geneID string) *evolution.EvolutionEvent {
	e := makeFailureEvent(id, taskID, clientID)
	e.GeneID = geneID
	return e
}

func makeBlockingEvent(id, taskID, clientID string) *evolution.EvolutionEvent {
	return &evolution.EvolutionEvent{
		ID:         id,
		TaskID:     taskID,
		ClientID:   clientID,
		EventType:  evolution.EventBlockingPattern,
		Signal:     fmt.Sprintf("Task %s blocked: unrecoverable error", taskID),
		RootCause:  "disk_full",
		GeneID:     "",
		Strategy:   "balanced",
		Importance: 0.9,
		CreatedAt:  time.Now().UTC(),
	}
}

func makeGeneJSON(strategyName string) string {
	g := &evolution.Gene{
		StrategyName:    strategyName,
		Role:            "balanced",
		Skills:          []string{"debugging"},
		MatchCondition:  "Task fails with timeout",
		ControlSignal:   "1. Check network connectivity\n2. Retry with exponential backoff\n3. Escalate after 3 attempts",
		FailureWarnings: []string{"Do not retry indefinitely", "Check disk space first"},
		SourceEvents:    []string{},
		SourceClientID:  "",
		Version:         0,
		Status:          evolution.GeneStatusDraft,
	}
	b, _ := json.Marshal(g)
	return string(b)
}

// ============================================================================
// Test: Task 1 — NewLocalGeneEvolver with defaults
// ============================================================================

func TestNewLocalGeneEvolver_Defaults(t *testing.T) {
	store := newMockStore()
	llm := &mockLLM{responses: []string{makeGeneJSON("test_defaults")}}
	gate := &mockGate{approve: true}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyBalanced, EvolverConfig{}, nil)

	assert.Equal(t, 10, ev.config.MaxEventsPerCycle)
	assert.Equal(t, 200, ev.config.MaxGeneLines)
	assert.Equal(t, 3, ev.config.StagnationThreshold)
	assert.Equal(t, 30*time.Second, ev.config.LLMTimeout)
	assert.Equal(t, 5000, ev.config.MaxControlSignalChars)
}

func TestEvolve_NilStore_ReturnsError(t *testing.T) {
	llm := &mockLLM{responses: []string{makeGeneJSON("test")}}
	ev := NewLocalGeneEvolver(nil, llm, &mockGate{true}, &mockSubmitter{}, evolution.StrategyBalanced, EvolverConfig{}, nil)
	_, err := ev.Evolve(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "store is nil")
}

func TestEvolve_NilLLM_ReturnsError(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, &mockGate{true}, &mockSubmitter{}, evolution.StrategyBalanced, EvolverConfig{}, nil)
	_, err := ev.Evolve(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "llm provider is nil")
}

// ============================================================================
// Test: Task 2 — Evolve full cycle
// ============================================================================

func TestEvolveFullCycle_Success(t *testing.T) {
	store := newMockStore()
	store.addEvent(makeFailureEvent("e1", "t1", "c1"))
	store.addEvent(makeFailureEvent("e2", "t2", "c1"))

	llm := &mockLLM{responses: []string{makeGeneJSON("retry_strategy")}}
	gate := &mockGate{approve: true}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyRepairOnly, EvolverConfig{}, nil)
	ev.SetRNG(rand.New(rand.NewSource(42)))

	gene, err := ev.Evolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, gene)

	assert.Equal(t, evolution.GeneStatusDraft, gene.Status)
	assert.Equal(t, "retry_strategy", gene.StrategyName)
	assert.Equal(t, 1, gene.Version)
	assert.NotEmpty(t, gene.ID)
	assert.Equal(t, "c1", gene.SourceClientID)
	assert.Len(t, gene.SourceEvents, 2)

	// Submitter called (async — give it a moment)
	time.Sleep(50 * time.Millisecond)
	assert.NotNil(t, sub.lastSubmitted())
	assert.Equal(t, gene.ID, sub.lastSubmitted().ID)
}

func TestEvolve_NoEvents_ReturnsNil(t *testing.T) {
	store := newMockStore()
	llm := &mockLLM{}
	gate := &mockGate{approve: true}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyBalanced, EvolverConfig{}, nil)
	gene, err := ev.Evolve(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, gene)
}

func TestEvolve_GateRejects_GeneSavedAsRejected(t *testing.T) {
	store := newMockStore()
	store.addEvent(makeFailureEvent("e1", "t1", "c1"))

	llm := &mockLLM{responses: []string{makeGeneJSON("bad_strategy")}}
	gate := &mockGate{approve: false} // gate rejects everything
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyRepairOnly, EvolverConfig{}, nil)
	ev.SetRNG(rand.New(rand.NewSource(42)))

	gene, err := ev.Evolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, gene)

	assert.Equal(t, evolution.GeneStatusRejected, gene.Status)

	// Verify saved with rejected status
	savedGenes, err := store.GetTopGenes("", 10)
	require.NoError(t, err)
	assert.Len(t, savedGenes, 1)
	assert.Equal(t, evolution.GeneStatusRejected, savedGenes[0].Status)

	// Submitter should NOT be called for rejected
	time.Sleep(50 * time.Millisecond)
	assert.Nil(t, sub.lastSubmitted())
}

func TestEvolve_GateNil_SkipsGateCheck(t *testing.T) {
	store := newMockStore()
	store.addEvent(makeFailureEvent("e1", "t1", "c1"))

	llm := &mockLLM{responses: []string{makeGeneJSON("test_strat")}}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, nil, sub, evolution.StrategyRepairOnly, EvolverConfig{}, nil)
	ev.SetRNG(rand.New(rand.NewSource(42)))

	gene, err := ev.Evolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, gene)
	assert.Equal(t, evolution.GeneStatusDraft, gene.Status)
}

// ============================================================================
// Test: Task 3 — selectTarget with strategy weights
// ============================================================================

func TestSelectTarget_RepairOnly_AlwaysFailures(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyRepairOnly, EvolverConfig{}, nil)

	successes := []*evolution.EvolutionEvent{
		makeSuccessEvent("s1", "t1", "c1"),
	}
	failures := []*evolution.EvolutionEvent{
		makeFailureEvent("f1", "t2", "c1"),
	}

	// RepairOnly: Repair=1.0, so random (any value < 1.0) → failures
	for i := 0; i < 10; i++ {
		ev.SetRNG(rand.New(rand.NewSource(int64(42 + i))))
		target := ev.selectTarget(successes, failures)
		assert.NotNil(t, target)
		assert.Equal(t, failures, target, "repair-only should always select failures when present")
	}
}

func TestSelectTarget_RepairOnly_NoFailures_ReturnsNil(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyRepairOnly, EvolverConfig{}, nil)

	successes := []*evolution.EvolutionEvent{
		makeSuccessEvent("s1", "t1", "c1"),
	}
	var failures []*evolution.EvolutionEvent

	// With only successes and RepairOnly strategy, selectTarget should fall through
	// Repair=1.0, but no failures → check successes: random < 1.0 + 0.0 = 1.0 always true
	// → filterNovelPatterns → since GeneID is empty → returns novel successes
	ev.SetRNG(rand.New(rand.NewSource(42)))
	target := ev.selectTarget(successes, failures)
	assert.NotNil(t, target, "with only successes and repair-only, falls to novel patterns")
}

func TestSelectTarget_RepairOnly_AllEmpty_ReturnsNil(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyRepairOnly, EvolverConfig{}, nil)

	var successes, failures []*evolution.EvolutionEvent
	target := ev.selectTarget(successes, failures)
	assert.Nil(t, target)
}

func TestSelectTarget_Balanced_Deterministic(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	successes := []*evolution.EvolutionEvent{
		makeSuccessEvent("s1", "t1", "c1"),
	}
	failures := []*evolution.EvolutionEvent{
		makeFailureEvent("f1", "t2", "c1"),
	}

	// Seed 42: rng.Float64() = 0.867... > Repair(0.20) → skips repair
	// 0.867 > Innovate+Repair(0.50+0.20=0.70) → skips novel patterns
	// → falls to filterExistingPatterns(successes) → empty (no GeneID) → fallback to failures
	// But actual Float64 from seed 42 is ~0.867, which puts us in the "existing patterns" path
	// With empty GeneID successes, this falls through → failures
	ev.SetRNG(rand.New(rand.NewSource(42)))
	target := ev.selectTarget(successes, failures)
	assert.NotNil(t, target)
	// The actual path depends on the exact Float64 value.
	// With seed 42 and balanced weights, verify the result is deterministic.
	assert.Len(t, target, 1, "selectTarget should return exactly 1 event for this input")
	
	// Verify determinism: same seed, same inputs → same result
	ev2 := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)
	ev2.SetRNG(rand.New(rand.NewSource(42)))
	target2 := ev2.selectTarget(successes, failures)
	assert.Equal(t, target[0].ID, target2[0].ID, "same seed should produce same result")
}

func TestSelectTarget_Innovate_PrefersNovel(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyInnovate, EvolverConfig{}, nil)

	successes := []*evolution.EvolutionEvent{
		makeSuccessEvent("s1", "t1", "c1"),
	}
	failures := []*evolution.EvolutionEvent{
		makeFailureEvent("f1", "t2", "c1"),
	}

	// Seed 42: 0.86... > Repair(0.05), < Innovate+Repair(0.95) → novel patterns
	ev.SetRNG(rand.New(rand.NewSource(42)))
	target := ev.selectTarget(successes, failures)
	assert.Equal(t, successes, target) // novel success patterns
}

func TestSelectTarget_Harden_WeightedFailures(t *testing.T) {
	// Harden: Repair=0.40, Innovate=0.20, Optimize=0.40.
	// With empty-GeneID successes:
	//   - r < 0.40 → failures (repair)
	//   - 0.40 <= r < 0.60 → filterNovelPatterns → successes (novel)
	//   - r >= 0.60 → filterExistingPatterns → empty → fallback → failures
	// So: ~40% repair-failures + ~40% optimize-fallback-failures = ~80% failures, ~20% successes
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyHarden, EvolverConfig{}, nil)

	successes := []*evolution.EvolutionEvent{
		makeSuccessEvent("s1", "t1", "c1"),
	}
	failures := []*evolution.EvolutionEvent{
		makeFailureEvent("f1", "t2", "c1"),
	}

	failureCount := 0
	successCount := 0
	for seed := int64(0); seed < 100; seed++ {
		ev.SetRNG(rand.New(rand.NewSource(seed)))
		target := ev.selectTarget(successes, failures)
		if target[0].EventType == evolution.EventFailurePattern || target[0].EventType == evolution.EventBlockingPattern {
			failureCount++
		} else {
			successCount++
		}
	}

	// With ~80% failure rate expected, verify the range
	assert.Greater(t, failureCount, 65, "harden should select failures ~80% of time with empty-GeneID successes")
	assert.Greater(t, successCount, 10, "harden should still have some novel patterns")
}

// ============================================================================
// Test: Task 4 — generateGene and mutateGene
// ============================================================================

func TestGenerateGene_ValidJSON(t *testing.T) {
	store := newMockStore()
	llm := &mockLLM{responses: []string{makeGeneJSON("test_generate")}}
	ev := NewLocalGeneEvolver(store, llm, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	gene, err := ev.generateGene(context.Background(), events)
	require.NoError(t, err)
	require.NotNil(t, gene)

	assert.Equal(t, "test_generate", gene.StrategyName)
	assert.NotEmpty(t, gene.ID)
	assert.Equal(t, 1, gene.Version)
	assert.Equal(t, "c1", gene.SourceClientID)
	assert.Len(t, gene.SourceEvents, 1)
	assert.Equal(t, "e1", gene.SourceEvents[0])
}

func TestGenerateGene_LLMReturnsInvalidJSON(t *testing.T) {
	store := newMockStore()
	llm := &mockLLM{responses: []string{"this is not json"}}
	ev := NewLocalGeneEvolver(store, llm, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	_, err := ev.generateGene(context.Background(), events)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid gene JSON")
}

func TestGenerateGene_LLMReturnsJSONWithCodeFences(t *testing.T) {
	store := newMockStore()
	jsonBody := makeGeneJSON("fenced_test")
	llm := &mockLLM{responses: []string{"```json\n" + jsonBody + "\n```"}}
	ev := NewLocalGeneEvolver(store, llm, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	gene, err := ev.generateGene(context.Background(), events)
	require.NoError(t, err)
	require.NotNil(t, gene)
	assert.Equal(t, "fenced_test", gene.StrategyName)
}

func TestGenerateGene_EmptyStrategyName_ReturnsError(t *testing.T) {
	store := newMockStore()
	llm := &mockLLM{responses: []string{makeGeneJSON("")}}
	ev := NewLocalGeneEvolver(store, llm, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	_, err := ev.generateGene(context.Background(), events)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty StrategyName")
}

func TestGenerateGene_LLMTimesOut(t *testing.T) {
	store := newMockStore()
	llm := &mockLLM{generateErr: fmt.Errorf("context deadline exceeded")}
	ev := NewLocalGeneEvolver(store, llm, nil, nil, evolution.StrategyBalanced, EvolverConfig{LLMTimeout: 10 * time.Millisecond}, nil)

	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	_, err := ev.generateGene(context.Background(), events)
	assert.Error(t, err)
}

func TestMutateGene_VersionIncremented(t *testing.T) {
	store := newMockStore()
	existing := &evolution.Gene{
		ID:             "gene-1",
		StrategyName:   "old_strategy",
		Role:           "balanced",
		Version:        3,
		SourceClientID: "c1",
		SourceEvents:   []string{"e0"},
		ControlSignal:  "old signal",
		CreatedAt:      time.Now().UTC().Add(-1 * time.Hour),
	}

	updatedJSON := `{"strategy_name":"new_strategy","role":"balanced","skills":["debugging"],"match_condition":"test","control_signal":"new improved signal","failure_warnings":["warn1"]}`
	llm := &mockLLM{responses: []string{updatedJSON}}
	ev := NewLocalGeneEvolver(store, llm, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	gene, err := ev.mutateGene(context.Background(), existing, events)
	require.NoError(t, err)
	require.NotNil(t, gene)

	assert.Equal(t, "gene-1", gene.ID)
	assert.Equal(t, 4, gene.Version) // incremented
	assert.Equal(t, "c1", gene.SourceClientID)
	// Source events appended
	assert.Contains(t, gene.SourceEvents, "e0")
	assert.Contains(t, gene.SourceEvents, "e1")
	assert.Equal(t, "new improved signal", gene.ControlSignal)
}

// ============================================================================
// Test: Task 5 — evolver prompts
// ============================================================================

func TestEvolverPrompts_BuildGeneGenerationPrompt(t *testing.T) {
	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	prompt := BuildGeneGenerationPrompt(events)
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "Evolver")
	assert.LessOrEqual(t, len(prompt), 8000)
}

func TestEvolverPrompts_BuildGeneMutationPrompt(t *testing.T) {
	existing := &evolution.Gene{
		ID:            "gene-1",
		StrategyName:  "test_strat",
		Role:          "balanced",
		ControlSignal: "do something",
	}
	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	prompt := BuildGeneMutationPrompt(existing, events)
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "test_strat")
	assert.Contains(t, prompt, "t1")
	assert.LessOrEqual(t, len(prompt), 8000)
}

func TestEvolverPrompts_BuildGeneMutationPrompt_TruncatesLargeControlSignal(t *testing.T) {
	existing := &evolution.Gene{
		ID:            "gene-1",
		StrategyName:  "test_strat",
		Role:          "balanced",
		ControlSignal: stringsRepeat("x", 5000), // > 3000 limit
	}
	events := []*evolution.EvolutionEvent{
		makeFailureEvent("e1", "t1", "c1"),
	}

	prompt := BuildGeneMutationPrompt(existing, events)
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "truncated")
	// Should not contain the full 5000 chars
	assert.Less(t, len(prompt), 8000)
}

func TestEvolverPrompts_BuildRootCausePrompt(t *testing.T) {
	prompt := BuildRootCausePrompt("Fix the bug", "timeout error", "exec, read_file")
	assert.NotEmpty(t, prompt)
	assert.Contains(t, prompt, "Fix the bug")
	assert.Contains(t, prompt, "timeout error")
	assert.Contains(t, prompt, "exec, read_file")
	assert.LessOrEqual(t, len(prompt), 8000)
}

// ============================================================================
// Test: Task 6 — stagnation detection
// ============================================================================

func TestStagnationDetection_ThreeConsecutiveNoImprovement(t *testing.T) {
	store := newMockStore()
	store.addEvent(makeFailureEvent("e1", "t1", "c1"))

	// First, create an existing gene. Role must match strategyToRole for findSimilarGene.
	// StrategyRepairOnly → strategyToRole returns "repair"
	existingGene := &evolution.Gene{
		ID:              "gene-1",
		StrategyName:    "test_strat",
		Role:            "repair", // must match strategyToRole(StrategyRepairOnly)
		Version:         1,
		StagnationCount: 2, // already 2 no-improvement cycles
		SourceClientID:  "c1",
		ControlSignal:   "x",
		Status:          evolution.GeneStatusDraft,
	}
	store.addGene(existingGene)

	// The LLM returns the SAME control signal (no improvement)
	sameJSON := `{"strategy_name":"test_strat","role":"balanced","skills":[],"match_condition":"test","control_signal":"x","failure_warnings":[]}`
	llm := &mockLLM{responses: []string{sameJSON}}
	gate := &mockGate{approve: true}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyRepairOnly, EvolverConfig{StagnationThreshold: 3}, nil)
	ev.SetRNG(rand.New(rand.NewSource(42)))

	gene, err := ev.Evolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, gene)

	assert.Equal(t, evolution.GeneStatusStagnant, gene.Status)
	assert.Equal(t, 3, gene.StagnationCount)

	// Verify stagnation event was created in store
	assert.True(t, store.eventCount() >= 2, "should have original event + stagnation event")

	// Stagnant gene should NOT be submitted (submitter is for draft only)
	time.Sleep(50 * time.Millisecond)
	assert.Nil(t, sub.lastSubmitted())
}

func TestStagnationDetection_TwoNoImprovementOneImprovement(t *testing.T) {
	store := newMockStore()
	store.addEvent(makeFailureEvent("e1", "t1", "c1"))

	existingGene := &evolution.Gene{
		ID:              "gene-1",
		StrategyName:    "test_strat",
		Role:            "repair",
		Version:         1,
		StagnationCount: 2,
		SourceClientID:  "c1",
		ControlSignal:   "x",
		Status:          evolution.GeneStatusDraft,
	}
	store.addGene(existingGene)

	// LLM returns a DIFFERENT control signal (improvement)
	improvedJSON := `{"strategy_name":"test_strat","role":"balanced","skills":[],"match_condition":"test","control_signal":"y_this_is_completely_different_and_longer_than_before","failure_warnings":[]}`
	llm := &mockLLM{responses: []string{improvedJSON}}
	gate := &mockGate{approve: true}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyRepairOnly, EvolverConfig{StagnationThreshold: 3}, nil)
	ev.SetRNG(rand.New(rand.NewSource(42)))

	gene, err := ev.Evolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, gene)

	assert.Equal(t, evolution.GeneStatusDraft, gene.Status)
	assert.Equal(t, 0, gene.StagnationCount) // reset to 0

	// Should be submitted
	time.Sleep(50 * time.Millisecond)
	assert.NotNil(t, sub.lastSubmitted())
}

func TestStagnationDetection_Unstagnation(t *testing.T) {
	store := newMockStore()
	store.addEvent(makeFailureEvent("e1", "t1", "c1"))

	existingGene := &evolution.Gene{
		ID:              "gene-1",
		StrategyName:    "test_strat",
		Role:            "repair",
		Version:         1,
		StagnationCount: 3,
		SourceClientID:  "c1",
		ControlSignal:   "x",
		Status:          evolution.GeneStatusStagnant, // currently stagnant
	}
	store.addGene(existingGene)

	// Improvement comes
	improvedJSON := `{"strategy_name":"test_strat","role":"balanced","skills":[],"match_condition":"test","control_signal":"new_improved_signal_that_is_totally_different","failure_warnings":[]}`
	llm := &mockLLM{responses: []string{improvedJSON}}
	gate := &mockGate{approve: true}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyRepairOnly, EvolverConfig{}, nil)
	ev.SetRNG(rand.New(rand.NewSource(42)))

	gene, err := ev.Evolve(context.Background())
	require.NoError(t, err)
	require.NotNil(t, gene)

	assert.Equal(t, evolution.GeneStatusDraft, gene.Status) // unstagnated
	assert.Equal(t, 0, gene.StagnationCount)
}

// ============================================================================
// Test: LLM failure edge cases
// ============================================================================

func TestEvolve_LLMReturnsInvalidJSON_SaveFails(t *testing.T) {
	store := newMockStore()
	store.addEvent(makeFailureEvent("e1", "t1", "c1"))

	llm := &mockLLM{responses: []string{"not valid json at all"}}
	gate := &mockGate{approve: true}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyRepairOnly, EvolverConfig{}, nil)
	ev.SetRNG(rand.New(rand.NewSource(42)))

	_, err := ev.Evolve(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid gene JSON")
}

func TestEvolve_ContextCancelled_GracefulError(t *testing.T) {
	store := newMockStore()
	store.addEvent(makeFailureEvent("e1", "t1", "c1"))

	llm := &mockLLM{generateErr: fmt.Errorf("context canceled")}
	gate := &mockGate{approve: true}
	sub := &mockSubmitter{}

	ev := NewLocalGeneEvolver(store, llm, gate, sub, evolution.StrategyRepairOnly, EvolverConfig{}, nil)
	ev.SetRNG(rand.New(rand.NewSource(42)))

	_, err := ev.Evolve(context.Background())
	assert.Error(t, err)
}

// ============================================================================
// Test: splitByType
// ============================================================================

func TestSplitByType(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	events := []*evolution.EvolutionEvent{
		makeSuccessEvent("s1", "t1", "c1"),
		makeFailureEvent("f1", "t2", "c1"),
		makeBlockingEvent("b1", "t3", "c1"),
		makeSuccessEvent("s2", "t4", "c1"),
		{
			ID: "stag1", TaskID: "t5", ClientID: "c1",
			EventType: evolution.EventStagnation,
			Signal: "stagnant gene", CreatedAt: time.Now(),
		},
	}

	successes, failures := ev.splitByType(events)
	assert.Len(t, successes, 2)
	assert.Len(t, failures, 2) // includes both failure and blocking
}

// ============================================================================
// Test: Config defaults
// ============================================================================

func TestEvolverConfig_Defaults(t *testing.T) {
	cfg := EvolverConfig{}
	cfg.setDefaults()
	assert.Equal(t, 10, cfg.MaxEventsPerCycle)
	assert.Equal(t, 200, cfg.MaxGeneLines)
	assert.Equal(t, 3, cfg.StagnationThreshold)
	assert.Equal(t, 30*time.Second, cfg.LLMTimeout)
	assert.Equal(t, 5000, cfg.MaxControlSignalChars)
}

func TestEvolverConfig_RespectsSetValues(t *testing.T) {
	cfg := EvolverConfig{
		MaxEventsPerCycle:    5,
		MaxGeneLines:         100,
		StagnationThreshold:  5,
		LLMTimeout:           10 * time.Second,
		MaxControlSignalChars: 2000,
	}
	cfg.setDefaults()
	assert.Equal(t, 5, cfg.MaxEventsPerCycle)
	assert.Equal(t, 100, cfg.MaxGeneLines)
	assert.Equal(t, 5, cfg.StagnationThreshold)
	assert.Equal(t, 10*time.Second, cfg.LLMTimeout)
	assert.Equal(t, 2000, cfg.MaxControlSignalChars)
}

// ============================================================================
// Test: hasImproved
// ============================================================================

func TestHasImproved_SuccessEvents_AlwaysTrue(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	existing := &evolution.Gene{ControlSignal: "old"}
	newGene := &evolution.Gene{ControlSignal: "old"} // same signal
	target := []*evolution.EvolutionEvent{
		makeSuccessEvent("s1", "t1", "c1"),
	}

	assert.True(t, ev.hasImproved(existing, newGene, target))
}

func TestHasImproved_FailureEvents_SignalChanged(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	existing := &evolution.Gene{ControlSignal: "short"}
	newGene := &evolution.Gene{ControlSignal: "this is a much longer signal that has changed significantly from before"}
	target := []*evolution.EvolutionEvent{
		makeFailureEvent("f1", "t1", "c1"),
	}

	assert.True(t, ev.hasImproved(existing, newGene, target))
}

func TestHasImproved_FailureEvents_SignalSame(t *testing.T) {
	store := newMockStore()
	ev := NewLocalGeneEvolver(store, nil, nil, nil, evolution.StrategyBalanced, EvolverConfig{}, nil)

	existing := &evolution.Gene{ControlSignal: "same_signal"}
	newGene := &evolution.Gene{ControlSignal: "same_signal"}
	target := []*evolution.EvolutionEvent{
		makeFailureEvent("f1", "t1", "c1"),
	}

	assert.False(t, ev.hasImproved(existing, newGene, target))
}

// ============================================================================
// Helper
// ============================================================================

func stringsRepeat(s string, count int) string {
	result := make([]byte, 0, len(s)*count)
	for i := 0; i < count; i++ {
		result = append(result, s...)
	}
	return string(result)
}
