# P6-05 Client Observer & Recorder — Summary

## Status: ✅ COMPLETE

All 5 tasks implemented, all tests passing, zero regressions.

...

---

# P6-06 Client Evolver (GEP) — Summary

## Status: ✅ COMPLETE

All 6 tasks implemented, all 30 evolver tests passing. Full GEP cycle implemented.

## What Was Done

### Task 1: LocalGeneEvolver struct and EvolverConfig (evolver.go)
- Defined `LLMProvider` interface for evolution-dedicated LLM calls
- Defined `geneEvolverStore` interface locally (consumer-side pattern, like recorder)
- Defined `GeneGateChecker` and `GeneSubmittor` interfaces for gatekeeper/submitter
- Created `LocalGeneEvolver` struct with store, llm, gate, submitter, strategy, config, rng, mutex, logger
- Created `EvolverConfig` with defaults: MaxEventsPerCycle=10, MaxGeneLines=200, StagnationThreshold=3, LLMTimeout=30s, MaxControlSignalChars=5000
- Constructor `NewLocalGeneEvolver` applies defaults for zero-valued config fields
- Edge cases: nil store → error, nil LLM → error

### Task 2: Evolve() — main GEP cycle
- Full 9-step GEP cycle: query → group → select → find → generate/mutate → stagnate → gate → save → submit
- Mutex-protected for concurrent safety
- `queryRecentEvents` fetches from store
- `splitByType` separates success/failure/blocking; skips stagnation events
- `findSimilarGene` looks up top gene by strategy-derived role
- `markEventsProcessed` flags source events with gene ID
- `parseGeneJSON` strips code fences from LLM output
- `truncateControlSignal` enforces line and char limits
- Edge cases: no events → nil; gate rejects → saved as rejected; gate nil → skip check

### Task 3: selectTarget with strategy weights
- Uses `Strategy.Weights()` for probability-based target selection
- Decision tree: repair (r < Repair) → failures; innovate (r < Innovate+Repair) → novel successes; optimize (else) → existing patterns; fallback → failures
- `filterNovelPatterns`: events with empty GeneID
- `filterExistingPatterns`: events with non-empty GeneID
- Deterministic testing via `SetRNG()` using seeded rand
- Verified all 4 strategy weight distributions (Balanced, Innovate, Harden, RepairOnly)

### Task 4: generateGene and mutateGene with LLM
- `generateGene`: builds prompt → LLM call → parse JSON → set metadata (ID=uuid, Version=1, timestamps, source events)
- `mutateGene`: builds mutation prompt → LLM call → parse JSON → preserve ID, increment Version, append source events
- `hasImproved`: success-only events → true; failure events → >10% ControlSignal change = improvement
- `stripCodeFences`: handles LLM ```json...``` wrapping
- Edge cases: LLM invalid JSON → error; empty StrategyName → error; timeout → error; code-fenced JSON → parsed

### Task 5: evolver_prompt.go with prompt templates
- `BuildGeneGenerationPrompt`: System prompt referencing Evolver paper (arXiv:2604.15097), instructs LLM to produce compact Gene JSON
- `BuildGeneMutationPrompt`: Refines existing Gene with new evidence, truncates ControlSignal to 3000 chars to avoid token overflow
- `BuildRootCausePrompt`: Compact root cause analysis prompt for Observer (≤500 chars target)
- All prompts are English, ≤8000 chars

### Task 6: Stagnation detection with cycle tracking
- 3 consecutive no-improvement cycles → `GeneStatusStagnant`
- Stagnant genes saved for audit trail but NOT submitted
- `notifyStagnation`: creates `EvolutionEvent` with `EventStagnation`, best-effort insert
- Unstagnation path: improvement resets StagnationCount to 0, status back to Draft
- Stagnation event notification is best-effort (errors logged, not returned)

## Test Results

```
=== All evolver tests: 30/30 PASS
=== Full evolution/client suite: 59/59 PASS (29 observer/recorder + 30 evolver)
```

## Files Changed

| File | Action |
|------|--------|
| `pkg/reef/evolution/client/evolver.go` | NEW — LocalGeneEvolver, EvolverConfig, full GEP cycle |
| `pkg/reef/evolution/client/evolver_prompt.go` | NEW — Gene generation/mutation/root-cause prompt templates |
| `pkg/reef/evolution/client/evolver_test.go` | NEW — 30 evolver tests |

## Design Decisions

1. **Consumer-side interface pattern**: `geneEvolverStore` defined locally (same pattern as recorder's `evolutionEventStore`). No coupling to concrete store types.

2. **Gate/Submitter interfaces**: Defined as `GeneGateChecker` and `GeneSubmittor` in evolver.go. Concrete implementations (gate.go, submitter.go) will satisfy these implicitly.

3. **Strategy weight distribution**: Follows the design doc exactly. Repair=0.20 means 20% of targets are failures. With empty-GeneID successes, the Optimize path falls through to fallback (failures), effectively giving Harden ~80% failure selection rate — by design for hardening.

4. **Stagnant early return**: When stagnation is detected, the gene is saved immediately and Evolve() returns. The gene is NOT submitted. This prevents stagnant genes from cluttering the server's gene pool.

5. **Deterministic RNG**: `SetRNG()` allows injection of seeded `rand.Rand` for reproducible tests of the strategy weight selection.

6. **ControlSignal truncation**: Both line count (200) and character count (5000) limits enforced. LLM mutation prompt also truncates existing ControlSignal to 3000 chars.

7. **hasImproved heuristic**: Simple >10% ControlSignal delta for failure events, always true for success-only events. Avoids over-engineering a complex LLM evaluation.
