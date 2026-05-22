---
change: reef-ppt-agent-v3
schema: openspec/v1
status: draft
---

# PPT Agent v3 — Behavior Specification

## Core Workflow

### GIVEN a user provides source materials (Word/PDF/text) and PPT requirements
### WHEN the agent starts PPT generation
### THEN it MUST follow the 7-phase pipeline: P0→P1→P2→P3→P4→P5→P6

---

## Phase Behaviors

### Spec 1: Outline Generation (P1)

**GIVEN** source.md exists in work directory
**WHEN** agent enters Phase 1
**THEN** it MUST:
1. Call LLM with outline_prompt.md + source.md + template summary
2. Generate outline.json with slides[].{title, summary, slide_type}
3. Present outline to user with confirmation point #1
4. NOT proceed to P2 until user explicitly approves

**GIVEN** user rejects outline
**WHEN** user provides feedback
**THEN** agent MUST revise outline and re-present (max 3 rounds)

---

### Spec 2: Detail Planning (P2)

**GIVEN** approved outline.json
**WHEN** agent enters Phase 2
**THEN** it MUST:
1. Call LLM with detail_prompt.md + outline.json + source.md
2. Generate detail_plan.json with content_blocks per slide
3. Present detail summary to user with confirmation point #2
4. NOT proceed to P3 until user explicitly approves

---

### Spec 3: Style Decision (P3)

**GIVEN** approved detail_plan.json
**WHEN** agent enters Phase 3
**THEN** it MUST:
1. Ask user: "选择风格: A) 内置风格 (列出5种) B) 自定义参考 .pptx"
2. Wait for user choice
3. If A: record style_decision.json {mode: "built_in", style_name: "..."}
4. If B: receive uploaded .pptx, record style_decision.json {mode: "custom", reference_path: "..."}
5. Present confirmation point #3

---

### Spec 4: Layout Planning — Custom Path (P4-B)

**GIVEN** style_decision.json with mode="custom"
**WHEN** agent enters Phase 4 custom path
**THEN** it MUST:
1. Parse reference .pptx → reference_lib.json (StyleProfile per slide)
2. Call LLM to map each plan_slide to a reference slide
3. Present mapping table with confirmation point #3.5
4. Generate layout_plan.json with style_ref pointers (NOT target_shape_idx)
5. NOT clone template shapes — only borrow style characteristics

**GIVEN** user adjusts mapping
**WHEN** user says "page=N, ref=M"
**THEN** agent MUST update mapping and re-present

---

### Spec 5: Preview (P5) — MANDATORY

**GIVEN** layout_plan.json exists
**WHEN** agent enters Phase 5
**THEN** it MUST:
1. Render preview for every slide (PNG > SVG > HTML degradation)
2. Present previews with text summary
3. Wait for confirmation point #4
4. If rejected: enter refinement sub-loop (max 3 rounds)
5. NOT proceed to P6 until user approves previews

---

### Spec 6: PPTX Generation (P6)

**GIVEN** confirmed layout_plan.json
**WHEN** agent enters Phase 6
**THEN** it MUST:
1. Build PPTX from blank Presentation() — NOT clone template
2. Create each shape independently per layout_plan.shapes[]
3. Apply style from style_ref (NOT from template shape index)
4. Run overflow detection with 3-strategy auto-fix
5. Run quality gate (FONT-INHERITANCE, COLOR-CONSISTENCY, CONTENT-COMPLETENESS, TEXT-OVERFLOW)
6. Output: output/{name}.pptx
7. Every element MUST be independently editable in PowerPoint
8. MUST NOT render any slide as a single image

---

## Invariants

### INV-1: No Fabricated Content
All slide content MUST trace back to source.md. Agent MUST NOT invent metrics, names, or data.

### INV-2: Editable Elements
Final .pptx MUST have each text/shape/image as independent editable element. Whole-slide images are FORBIDDEN.

### INV-3: Confirmation Before Proceed
Agent MUST NOT skip any confirmation point. Skipping = critical bug.

### INV-4: Style is Reference, Not Constraint
When using custom reference, agent borrows style characteristics (colors, fonts, layout skeleton). It does NOT force content into template slots.

### INV-5: Preview is Mandatory
Agent MUST generate and present previews before final PPTX. No exceptions.

---

## Error Handling

### EH-1: LLM Timeout
If LLM call fails after retries: report error to user, offer retry or manual adjustment.

### EH-2: Overflow
If content exceeds shape capacity: auto-fix with shrink_title → split_to_two_slides → two_column. Log decision in overflow_decisions[].

### EH-3: Quality Gate Failure
If quality gate fails: auto-fix if possible, otherwise report to user with specific failure items.

### EH-4: Reference Parse Failure
If custom .pptx cannot be parsed: fall back to built-in style, inform user.
