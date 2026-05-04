package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/zhazhaku/reef/pkg/reef/evolution"
)

// ---------------------------------------------------------------------------
// Interfaces
// ---------------------------------------------------------------------------

// LLMProvider is the evolution-dedicated LLM interface, separate from the
// task execution LLM. It accepts system and user prompts and returns raw text.
type LLMProvider interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (string, error)
}

// geneEvolverStore is the minimal interface the LocalGeneEvolver needs from
// the persistence layer. Defined locally to avoid coupling to concrete store types.
type geneEvolverStore interface {
	// GetRecentEvents returns up to limit most recent events for a client.
	GetRecentEvents(clientID string, limit int) ([]*evolution.EvolutionEvent, error)
	// InsertEvolutionEvent persists a new evolution event.
	InsertEvolutionEvent(event *evolution.EvolutionEvent) error
	// SaveGene persists a Gene to the genes table.
	SaveGene(gene *evolution.Gene) error
	// GetTopGenes returns up to limit most-recent Genes for the given role.
	GetTopGenes(role string, limit int) ([]*evolution.Gene, error)
	// MarkEventsProcessed records that the given events have been processed into a gene.
	MarkEventsProcessed(eventIDs []string, geneID string) error
}

// GeneGateChecker is the interface for the local gatekeeper.
// It performs syntax + semantic checks on a Gene before submission.
type GeneGateChecker interface {
	Check(gene *evolution.Gene) bool
}

// GeneSubmittor is the interface for submitting genes to the Server.
// Implementation sends via WebSocket with an offline queue fallback.
type GeneSubmittor interface {
	Submit(gene *evolution.Gene)
}

// ---------------------------------------------------------------------------
// EvolverConfig
// ---------------------------------------------------------------------------

// EvolverConfig configures the LocalGeneEvolver.
type EvolverConfig struct {
	// MaxEventsPerCycle is the max events queried per Evolve() call.
	// Defaults to 10.
	MaxEventsPerCycle int

	// MaxGeneLines is the cap on ControlSignal line count.
	// Defaults to 200.
	MaxGeneLines int

	// StagnationThreshold is the number of consecutive no-improvement cycles
	// before marking a Gene as stagnant. Defaults to 3.
	StagnationThreshold int

	// LLMTimeout is the timeout for LLM generation calls.
	// Defaults to 30 seconds.
	LLMTimeout time.Duration

	// MaxControlSignalChars is the maximum character length of ControlSignal.
	// Defaults to 5000.
	MaxControlSignalChars int
}

// setDefaults applies default values for zero-valued config fields.
func (c *EvolverConfig) setDefaults() {
	if c.MaxEventsPerCycle <= 0 {
		c.MaxEventsPerCycle = 10
	}
	if c.MaxGeneLines <= 0 {
		c.MaxGeneLines = 200
	}
	if c.StagnationThreshold <= 0 {
		c.StagnationThreshold = 3
	}
	if c.LLMTimeout <= 0 {
		c.LLMTimeout = 30 * time.Second
	}
	if c.MaxControlSignalChars <= 0 {
		c.MaxControlSignalChars = 5000
	}
}

// ---------------------------------------------------------------------------
// LocalGeneEvolver
// ---------------------------------------------------------------------------

// LocalGeneEvolver is the client-side GEP (Generate-Evolve-Publish) core.
// It queries recent events, groups them by type, selects targets via strategy
// weights, generates or mutates Genes via LLM, detects stagnation, and saves+submits.
type LocalGeneEvolver struct {
	store     geneEvolverStore
	llm       LLMProvider
	gate      GeneGateChecker
	submitter GeneSubmittor
	strategy  evolution.Strategy
	config    EvolverConfig
	rng       *rand.Rand
	mu        sync.Mutex
	logger    *slog.Logger
}

// NewLocalGeneEvolver creates a new LocalGeneEvolver.
// Config fields that are zero are replaced with sensible defaults.
func NewLocalGeneEvolver(
	store geneEvolverStore,
	llm LLMProvider,
	gate GeneGateChecker,
	submitter GeneSubmittor,
	strategy evolution.Strategy,
	config EvolverConfig,
	logger *slog.Logger,
) *LocalGeneEvolver {
	config.setDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &LocalGeneEvolver{
		store:     store,
		llm:       llm,
		gate:      gate,
		submitter: submitter,
		strategy:  strategy,
		config:    config,
		rng:       rand.New(rand.NewSource(time.Now().UnixNano())),
		logger:    logger,
	}
}

// SetRNG replaces the random number generator (for deterministic testing).
func (e *LocalGeneEvolver) SetRNG(rng *rand.Rand) {
	e.rng = rng
}

// Config returns a copy of the current config (read-only).
func (e *LocalGeneEvolver) Config() EvolverConfig {
	return e.config
}

// ---------------------------------------------------------------------------
// Evolve — main GEP cycle
// ---------------------------------------------------------------------------

// Evolve executes one complete GEP evolution cycle.
// Returns the generated Gene (may be nil if no actionable signal) and any error.
func (e *LocalGeneEvolver) Evolve(ctx context.Context) (*evolution.Gene, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validation: store and llm are required.
	if e.store == nil {
		return nil, fmt.Errorf("evolver: store is nil")
	}
	if e.llm == nil {
		return nil, fmt.Errorf("evolver: llm provider is nil")
	}

	// Step 1: Query recent events
	events, err := e.queryRecentEvents(ctx, e.config.MaxEventsPerCycle)
	if err != nil {
		return nil, fmt.Errorf("evolver: query events: %w", err)
	}
	if len(events) == 0 {
		return nil, nil // no events to process
	}

	// Step 2: Group by type
	successEvents, failureEvents := e.splitByType(events)

	// Step 3: Select target via strategy weights
	target := e.selectTarget(successEvents, failureEvents)
	if target == nil || len(target) == 0 {
		return nil, nil // no actionable signal
	}

	// Step 4: Check for existing similar Gene
	existingGene := e.findSimilarGene(target)

	// Step 5: Generate or mutate
	var gene *evolution.Gene
	if existingGene != nil {
		gene, err = e.mutateGene(ctx, existingGene, target)
	} else {
		gene, err = e.generateGene(ctx, target)
	}
	if err != nil {
		return nil, fmt.Errorf("evolver: gene generation failed: %w", err)
	}
	if gene == nil {
		return nil, nil
	}

	// Step 6: Stagnation detection (only on mutation / update)
	if existingGene != nil {
		if !e.hasImproved(existingGene, gene, target) {
			gene.StagnationCount = existingGene.StagnationCount + 1
			if gene.StagnationCount >= e.config.StagnationThreshold {
				gene.Status = evolution.GeneStatusStagnant
				e.logger.Warn("gene marked stagnant",
					slog.String("gene_id", gene.ID),
					slog.Int("stagnation_count", gene.StagnationCount))
				e.notifyStagnation(gene)
				// Save stagnant gene for audit trail, do NOT submit
				if saveErr := e.saveGene(gene); saveErr != nil {
					e.logger.Error("failed to save stagnant gene",
						slog.String("gene_id", gene.ID),
						slog.String("error", saveErr.Error()))
				}
				e.markEventsProcessed(events, gene.ID)
				return gene, nil
			}
		} else {
			gene.StagnationCount = 0
			if gene.Status == evolution.GeneStatusStagnant {
				gene.Status = evolution.GeneStatusDraft // unstagnation
				e.logger.Info("gene unstagnated",
					slog.String("gene_id", gene.ID))
			}
		}
	}

	// Step 7: Local gate check
	if e.gate != nil && !e.gate.Check(gene) {
		gene.Status = evolution.GeneStatusRejected
		if saveErr := e.saveGene(gene); saveErr != nil {
			e.logger.Error("failed to save rejected gene",
				slog.String("gene_id", gene.ID),
				slog.String("error", saveErr.Error()))
		}
		return gene, nil
	}

	// Step 8: Save
	gene.Status = evolution.GeneStatusDraft
	if err := e.saveGene(gene); err != nil {
		return nil, fmt.Errorf("evolver: save gene: %w", err)
	}

	// Mark source events as processed
	e.markEventsProcessed(events, gene.ID)

	// Step 9: Async submit (skip if stagnant or rejected)
	if gene.Status == evolution.GeneStatusDraft && e.submitter != nil {
		go e.submitter.Submit(gene)
	}

	return gene, nil
}

// ---------------------------------------------------------------------------
// queryRecentEvents
// ---------------------------------------------------------------------------

// queryRecentEvents fetches the most recent events from the store.
// limit caps the number of events returned.
func (e *LocalGeneEvolver) queryRecentEvents(ctx context.Context, limit int) ([]*evolution.EvolutionEvent, error) {
	if limit <= 0 {
		limit = 10
	}
	// Use empty clientID to get all recent events across clients
	events, err := e.store.GetRecentEvents("", limit)
	if err != nil {
		return nil, err
	}
	return events, nil
}

// ---------------------------------------------------------------------------
// splitByType
// ---------------------------------------------------------------------------

// splitByType separates events into success and failure/blocking categories.
func (e *LocalGeneEvolver) splitByType(events []*evolution.EvolutionEvent) (successes, failures []*evolution.EvolutionEvent) {
	for _, evt := range events {
		switch evt.EventType {
		case evolution.EventSuccessPattern:
			successes = append(successes, evt)
		case evolution.EventFailurePattern, evolution.EventBlockingPattern:
			failures = append(failures, evt)
		case evolution.EventStagnation:
			// Stagnation events are informational, skip for target selection
		}
	}
	return successes, failures
}

// ---------------------------------------------------------------------------
// selectTarget — strategy weight distribution
// ---------------------------------------------------------------------------

// selectTarget picks target events based on the active strategy's probability weights.
//
// Decision tree:
//   - If failures exist AND random < Repair weight → return failures (repair mode).
//   - If successes exist AND random < Innovate + Repair → filterNovelPatterns (new success).
//   - If successes exist → filterExistingPatterns (optimize existing).
//   - Fallback → failures (if any), else nil.
func (e *LocalGeneEvolver) selectTarget(successes, failures []*evolution.EvolutionEvent) []*evolution.EvolutionEvent {
	w := e.strategy.Weights()
	r := e.rng.Float64()

	// Repair mode: pick failures
	if len(failures) > 0 && r < w.Repair {
		return failures
	}

	// Innovate mode: novel success patterns
	if len(successes) > 0 && r < w.Innovate+w.Repair {
		novel := e.filterNovelPatterns(successes)
		if len(novel) > 0 {
			return novel
		}
	}

	// Optimize mode: existing patterns
	if len(successes) > 0 {
		existing := e.filterExistingPatterns(successes)
		if len(existing) > 0 {
			return existing
		}
	}

	// Fallback to failures, then nil
	if len(failures) > 0 {
		return failures
	}
	return nil
}

// filterNovelPatterns returns events where GeneID is empty (no existing gene).
func (e *LocalGeneEvolver) filterNovelPatterns(events []*evolution.EvolutionEvent) []*evolution.EvolutionEvent {
	var result []*evolution.EvolutionEvent
	for _, evt := range events {
		if evt.GeneID == "" {
			result = append(result, evt)
		}
	}
	return result
}

// filterExistingPatterns returns events where GeneID is non-empty (pattern has been seen).
func (e *LocalGeneEvolver) filterExistingPatterns(events []*evolution.EvolutionEvent) []*evolution.EvolutionEvent {
	var result []*evolution.EvolutionEvent
	for _, evt := range events {
		if evt.GeneID != "" {
			result = append(result, evt)
		}
	}
	return result
}

// ---------------------------------------------------------------------------
// findSimilarGene
// ---------------------------------------------------------------------------

// findSimilarGene checks whether there is an existing Gene that matches
// the target events' pattern (by role or skill overlap).
func (e *LocalGeneEvolver) findSimilarGene(target []*evolution.EvolutionEvent) *evolution.Gene {
	if len(target) == 0 {
		return nil
	}
	// Use the role from the first event's context. In practice the role is
	// inferred from the event's client, but we scan the top genes for the
	// evolution-strategy role as a heuristic.
	role := e.strategyToRole(e.strategy)
	genes, err := e.store.GetTopGenes(role, 1)
	if err != nil || len(genes) == 0 {
		return nil
	}
	// Return the top gene if it's not stagnant/retired
	g := genes[0]
	if g.Status == evolution.GeneStatusStagnant || g.Status == evolution.GeneStatusRetired {
		return nil
	}
	return g
}

// strategyToRole maps strategy strings to conventional role names for gene lookup.
func (e *LocalGeneEvolver) strategyToRole(s evolution.Strategy) string {
	switch s {
	case evolution.StrategyRepairOnly:
		return "repair"
	case evolution.StrategyHarden:
		return "harden"
	case evolution.StrategyInnovate:
		return "innovate"
	default:
		return "balanced"
	}
}

// ---------------------------------------------------------------------------
// generateGene
// ---------------------------------------------------------------------------

// generateGene calls the LLM to create a brand-new Gene from target events.
func (e *LocalGeneEvolver) generateGene(ctx context.Context, events []*evolution.EvolutionEvent) (*evolution.Gene, error) {
	systemPrompt := BuildGeneGenerationPrompt(events)
	userPrompt := e.buildUserPrompt(events)

	llmCtx, cancel := context.WithTimeout(ctx, e.config.LLMTimeout)
	defer cancel()

	raw, err := e.llm.Generate(llmCtx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("llm generate: %w", err)
	}

	gene, err := e.parseGeneJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	// Validate StrategyName
	if gene.StrategyName == "" {
		e.logger.Error("llm returned gene with empty StrategyName",
			slog.String("raw_response", truncateToMaxChars(raw, 1000)))
		return nil, fmt.Errorf("evolver: llm returned gene with empty StrategyName")
	}

	// Set metadata
	gene.ID = uuid.NewString()
	gene.Version = 1
	gene.CreatedAt = time.Now().UTC()
	gene.UpdatedAt = gene.CreatedAt
	gene.SourceEvents = e.eventIDs(events)
	if len(events) > 0 {
		gene.SourceClientID = events[0].ClientID
	}
	gene.Status = evolution.GeneStatusDraft

	// Post-process: truncate ControlSignal
	e.truncateControlSignal(gene)

	return gene, nil
}

// ---------------------------------------------------------------------------
// mutateGene
// ---------------------------------------------------------------------------

// mutateGene calls the LLM to update an existing Gene based on new events.
func (e *LocalGeneEvolver) mutateGene(ctx context.Context, existing *evolution.Gene, events []*evolution.EvolutionEvent) (*evolution.Gene, error) {
	systemPrompt := BuildGeneMutationPrompt(existing, events)
	userPrompt := e.buildUserPrompt(events)

	llmCtx, cancel := context.WithTimeout(ctx, e.config.LLMTimeout)
	defer cancel()

	raw, err := e.llm.Generate(llmCtx, systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("llm mutate: %w", err)
	}

	gene, err := e.parseGeneJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("parse llm response: %w", err)
	}

	// Preserve identity, increment version
	gene.ID = existing.ID
	gene.Version = existing.Version + 1
	gene.CreatedAt = existing.CreatedAt
	gene.UpdatedAt = time.Now().UTC()
	gene.SourceClientID = existing.SourceClientID
	// Preserve existing source events + append new ones (deduplicated)
	seen := make(map[string]bool)
	gene.SourceEvents = make([]string, 0, len(existing.SourceEvents)+len(events))
	for _, id := range existing.SourceEvents {
		if !seen[id] {
			gene.SourceEvents = append(gene.SourceEvents, id)
			seen[id] = true
		}
	}
	for _, evt := range events {
		if !seen[evt.ID] {
			gene.SourceEvents = append(gene.SourceEvents, evt.ID)
			seen[evt.ID] = true
		}
	}

	// Post-process
	e.truncateControlSignal(gene)

	return gene, nil
}

// ---------------------------------------------------------------------------
// hasImproved
// ---------------------------------------------------------------------------

// hasImproved heuristically determines whether the new gene represents an
// improvement over the existing one.
//
// Rules:
//   - If target contains only success events → assume improvement (true).
//   - If target contains failure events → compare ControlSignal change.
//     >10% change in ControlSignal content is considered an improvement.
//   - Otherwise, returns true for new genes (generate), and for mutate
//     returns the ControlSignal-based delta check.
func (e *LocalGeneEvolver) hasImproved(existing, newGene *evolution.Gene, target []*evolution.EvolutionEvent) bool {
	// New gene generation: always assume improvement
	if existing == nil {
		return true
	}

	// All success events: assume improvement
	allSuccess := true
	for _, evt := range target {
		if evt.EventType != evolution.EventSuccessPattern {
			allSuccess = false
			break
		}
	}
	if allSuccess {
		return true
	}

	// Compare ControlSignal change; >10% delta = improvement
	oldLen := len(existing.ControlSignal)
	newLen := len(newGene.ControlSignal)
	if oldLen == 0 {
		return newLen > 0
	}

	delta := float64(abs(newLen-oldLen)) / float64(oldLen)
	return delta > 0.10
}

// ---------------------------------------------------------------------------
// stagnation detection
// ---------------------------------------------------------------------------

// notifyStagnation creates an EvolutionEvent recording that a gene has gone stagnant.
// This is best-effort; errors are logged but not returned.
func (e *LocalGeneEvolver) notifyStagnation(gene *evolution.Gene) {
	stagnationEvent := &evolution.EvolutionEvent{
		ID:         fmt.Sprintf("evt-stag-%s-%d", gene.ID, time.Now().UnixNano()),
		TaskID:     gene.ID, // reference the gene
		ClientID:   gene.SourceClientID,
		EventType:  evolution.EventStagnation,
		Signal:     fmt.Sprintf("Gene %s stagnant after %d cycles", gene.ID, gene.StagnationCount),
		RootCause:  "no_improvement",
		GeneID:     gene.ID,
		Strategy:   string(e.strategy),
		Importance: 0.8,
		CreatedAt:  time.Now().UTC(),
	}
	if err := e.store.InsertEvolutionEvent(stagnationEvent); err != nil {
		e.logger.Error("failed to insert stagnation event",
			slog.String("gene_id", gene.ID),
			slog.String("error", err.Error()))
	}
}

// ---------------------------------------------------------------------------
// saveGene
// ---------------------------------------------------------------------------

// saveGene persists a Gene to the store.
func (e *LocalGeneEvolver) saveGene(gene *evolution.Gene) error {
	if gene == nil {
		return fmt.Errorf("evolver: cannot save nil gene")
	}
	return e.store.SaveGene(gene)
}

// ---------------------------------------------------------------------------
// markEventsProcessed
// ---------------------------------------------------------------------------

// markEventsProcessed flags the given events as processed into a gene.
func (e *LocalGeneEvolver) markEventsProcessed(events []*evolution.EvolutionEvent, geneID string) {
	ids := make([]string, 0, len(events))
	for _, evt := range events {
		ids = append(ids, evt.ID)
	}
	if err := e.store.MarkEventsProcessed(ids, geneID); err != nil {
		e.logger.Error("failed to mark events processed",
			slog.String("gene_id", geneID),
			slog.Int("event_count", len(ids)),
			slog.String("error", err.Error()))
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// parseGeneJSON parses raw LLM JSON output into a Gene struct.
func (e *LocalGeneEvolver) parseGeneJSON(raw string) (*evolution.Gene, error) {
	// The LLM may wrap the JSON in markdown code fences. Strip them.
	cleaned := stripCodeFences(raw)

	var gene evolution.Gene
	if err := json.Unmarshal([]byte(cleaned), &gene); err != nil {
		e.logger.Error("failed to parse gene JSON",
			slog.String("raw_response", truncateToMaxChars(raw, 1000)),
			slog.String("error", err.Error()))
		return nil, fmt.Errorf("invalid gene JSON: %w", err)
	}
	return &gene, nil
}

// buildUserPrompt concatenates event signals into a user-facing prompt.
func (e *LocalGeneEvolver) buildUserPrompt(events []*evolution.EvolutionEvent) string {
	var sb strings.Builder
	sb.WriteString("Observed signals:\n")
	for i, evt := range events {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s", i+1, evt.EventType, evt.Signal))
		if evt.RootCause != "" {
			sb.WriteString(fmt.Sprintf(" (root cause: %s)", evt.RootCause))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// truncateControlSignal enforces the MaxGeneLines and MaxControlSignalChars limits.
// If the signal exceeds limits, it is truncated and a warning is logged.
func (e *LocalGeneEvolver) truncateControlSignal(gene *evolution.Gene) {
	if gene == nil {
		return
	}

	// Truncate by character count
	if len(gene.ControlSignal) > e.config.MaxControlSignalChars {
		gene.ControlSignal = gene.ControlSignal[:e.config.MaxControlSignalChars]
		e.logger.Warn("control signal truncated by char limit",
			slog.String("gene_id", gene.ID),
			slog.Int("max_chars", e.config.MaxControlSignalChars))
	}

	// Truncate by line count
	lines := strings.Split(gene.ControlSignal, "\n")
	if len(lines) > e.config.MaxGeneLines {
		gene.ControlSignal = strings.Join(lines[:e.config.MaxGeneLines], "\n")
		e.logger.Warn("control signal truncated by line limit",
			slog.String("gene_id", gene.ID),
			slog.Int("max_lines", e.config.MaxGeneLines))
	}

	// Truncate FailureWarnings
	if len(gene.FailureWarnings) > 20 {
		gene.FailureWarnings = gene.FailureWarnings[:20]
	}
}

// eventIDs extracts IDs from a slice of events.
func (e *LocalGeneEvolver) eventIDs(events []*evolution.EvolutionEvent) []string {
	ids := make([]string, 0, len(events))
	for _, evt := range events {
		ids = append(ids, evt.ID)
	}
	return ids
}

// stripCodeFences removes leading/trailing markdown code fences from raw LLM output.
func stripCodeFences(raw string) string {
	s := strings.TrimSpace(raw)
	// Remove ```json or ``` fences
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

// abs returns the absolute value of x.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
