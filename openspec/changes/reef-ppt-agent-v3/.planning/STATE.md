# v3 Planning State

## Current Stage
**Phase 0 - Planning / Gap Analysis** (BLOCKED on infrastructure)

## Timeline
- 2026-05-22 07:13: .planning/ skeleton created (PROJECT/REQUIREMENTS/ROADMAP/STATE)
- 2026-05-22 07:20: First attempt to spawn 4 researchers — appeared to succeed but `spawn_status` returned 0; `reef_status` shows 0 connected clients
- 2026-05-22 07:35: Second attempt (Stack/Features/Architecture) — same false-success pattern
- 2026-05-22 07:42: Confirmed spawn tool is broken in current runtime; archived state

## Completed Artifacts
| Path | Lines | Status |
|---|---|---|
| `proposal.md` | 76 | ✅ Complete |
| `design.md` | 186 | ✅ Complete (P0-P6 + data contracts + engine architecture) |
| `tasks.md` | 254 | ✅ Complete (T1-T7, ~30h) |
| `specs/ppt-agent/spec.md` | 137 | ✅ Complete (GIVEN/WHEN/THEN + 5 invariants + 4 error rules) |
| `.planning/PROJECT.md` | - | ✅ |
| `.planning/REQUIREMENTS.md` | - | ✅ |
| `.planning/ROADMAP.md` | - | ✅ |
| `.planning/STATE.md` | - | ✅ (this file) |
| `.planning/research/*.md` | - | ❌ NOT produced |
| `.planning/phases/*.md` | - | ❌ NOT produced |

## Blocked / Outstanding

### Infrastructure block
- `spawn` tool returns fake success (claims subagent launched, `spawn_status` shows none)
- `reef_status` shows 0 connected executors → `reef_submit_task` route also dead
- Cannot run parallel research as originally planned

### Pending research dimensions
1. **R1-Stack**: python-pptx vs alternatives, preview engine comparison, structured-output libs, AI-native PPT framework survey
2. **R2-Features**: Competitor matrix (Gamma/Beautiful.ai/通义/讯飞/WPS AI), missing features audit (charts/icons/notes/branding/multi-lang/incremental rebuild/exports)
3. **R3-Architecture**: Three-engine boundary clarity, data contract (style_ref / shape_id namespace), extensibility, failure handling, concurrency, testability, state recovery, observability, backward compat with v2.3, T1-T7 dependency
4. **R4-Pitfalls**: Style ambiguity, preview degradation breakage, refinement loop explosion, LLM hallucination on layouts, font fallback chain, mixed-language typography, image quality

## Decisions Made
1. Default mode = "reference/borrow" (not strict slot-filling)
2. Preview before final generation (P5 mandatory, max 3 refinement rounds)
3. All output PPTX elements independently editable (no whole-slide images)
4. Engine split: StyleExtractor / LayoutComposer / PPTXBuilder (3-way)

## Open Questions (need R3+R4 to close)
1. `style_ref` resolution order: reference_lib.json vs built-in DSL vs inline override?
2. `shape_id` namespace: per-slide or per-presentation?
3. State machine: how does pipeline resume after crash mid-P3?
4. Strict mode opt-in: keep `compose_with_layout_plan()` as legacy or full delete?
5. Structured LLM output: use `instructor`/`outlines`/raw JSON-mode?
6. LibreOffice headless concurrency: process pool needed?
7. Refinement diff granularity: per-shape patch or full-slide regenerate?

## Known Risks
- LLM hallucinating `style_ref` IDs that don't exist in reference_lib
- LibreOffice preview latency may exceed 5-min budget for ≥20-slide decks
- v2.3 users on `compose_with_layout_plan` need migration shim
- Spawn/reef tooling unreliable → must plan around inline serial execution

## Next Action (when unblocked)
**Plan B (recommended)**: agent runs 4 research passes serially using own tools (web_search / read_file / grep), writes 4 reports to `.planning/research/{stack,features,architecture,pitfalls}.md`, then synthesizes `gap-report.md` + `SUMMARY.md`, then patches `design.md` / `tasks.md` accordingly.

**Plan C (minimal)**: only R3 (Architecture) + R4 (Pitfalls) — skip ecosystem survey, trust current stack choice.

User to choose B or C before resuming.
