---
change: reef-ppt-agent-v3
schema: openspec/v1
status: draft
---

# PPT Agent v3 — Implementation Tasks

## Overview

| Phase | Tasks | Est. Hours | Depends On |
|-------|-------|------------|------------|
| T1: StyleExtractor | 4 | 4h | — |
| T2: LayoutComposer | 5 | 6h | T1 |
| T3: PPTXBuilder | 5 | 6h | T2 |
| T4: Preview Engine | 3 | 3h | T2 |
| T5: Pipeline Integration | 4 | 4h | T1-T4 |
| T6: Built-in Styles | 3 | 2h | T1 |
| T7: Tests | 5 | 5h | T1-T6 |
| **Total** | **29** | **30h** | |

---

## T1: StyleExtractor (4 tasks, 4h)

### T1.1 StyleProfile Schema
- Define StyleProfile JSON schema: colors{primary,secondary,accent,bg,text}, fonts{title,body}, decor{accent_bar,card_radius,icon_style}, layout_skeleton{regions[]}
- File: `schema.py` — add `validate_style_profile()`
- Test: valid/invalid profiles

### T1.2 PPTX Style Extractor
- Parse .pptx → extract colors (Counter), fonts (Counter), sizes (Counter), decoration patterns
- Output: StyleProfile JSON
- File: `style_extractor.py` (new)
- Test: extract from henan_ppt template, verify #C00000 primary, #0070C0 secondary

### T1.3 Reference Library Builder
- Multi-slide .pptx → per-slide StyleProfile → reference_lib.json
- File: `style_extractor.py` — `build_reference_lib()`
- Test: 9-slide henan template → 9 StyleProfiles

### T1.4 Built-in Style DSL Definitions
- Define 5 built-in styles as StyleProfile JSON:
  - 商务蓝: primary=#1F4E79, secondary=#2E75B6, accent=#BDD7EE
  - 科技红: primary=#C00000, secondary=#0070C0, accent=#E7E6E6
  - 极简黑: primary=#252B3A, secondary=#404040, accent=#F2F2F2
  - 学术绿: primary=#375623, secondary=#548235, accent=#C5E0B4
  - 活力橙: primary=#C55A11, secondary=#ED7D31, accent=#FBE5D6
- File: `builtin_styles/` directory with 5 JSON files
- Test: each validates against StyleProfile schema

---

## T2: LayoutComposer (5 tasks, 6h)

### T2.1 Layout Plan v3 Schema
- Define layout_plan.json v3 schema in `schema.py`
- Key change: shapes[] with {shape_id, type, x, y, w, h, content_key, style_ref, action}
- Remove target_shape_idx (v2.3 legacy)
- Test: validate_layout_plan_v3()

### T2.2 Built-in Layout Templates
- Define 8 layout templates as JSON:
  - center_title, left_title, data_cards(2-4), three_column, two_column, flow_chart, comparison, timeline
- Each template: shapes[] with relative positions (0.0-1.0 normalized)
- File: `layout_templates/` directory
- Test: each validates against layout_plan v3 schema

### T2.3 Layout Selection Logic
- slide_type + content_block count/type → candidate templates → best match
- Example: COVER → center_title; CONTENT with 4 bullet_list → data_cards(4)
- File: `layout_composer.py` (new) — `select_layout()`
- Test: 9 slide types → correct template selection

### T2.4 Custom Reference Layout Borrowing
- Given reference_lib.json + detail_plan → borrow layout skeleton (not shapes)
- Extract region structure (header, body columns, footer) from reference
- Map content_blocks to borrowed regions
- File: `layout_composer.py` — `compose_from_reference()`
- Test: henan template reference → 9-slide layout_plan

### T2.5 Layout Plan Generator
- Master function: detail_plan + style_decision → layout_plan.json
- Routes to built-in or custom path
- File: `layout_composer.py` — `generate_layout_plan()`
- Test: end-to-end both paths

---

## T3: PPTXBuilder (5 tasks, 6h)

### T3.1 Blank Deck Builder
- Create Presentation() from scratch (no template clone)
- Set slide dimensions from style_ref or default 16:9
- File: `pptx_builder.py` (new) — `create_deck()`
- Test: empty deck with correct dimensions

### T3.2 Shape Factory
- Create shape by type: text_box, rectangle, rounded_rectangle, image, connector, table
- Apply style_ref: font, size, color, bold, alignment, fill
- File: `pptx_builder.py` — `create_shape()`
- Test: each shape type with style injection

### T3.3 Slide Builder
- Given layout_plan.slides[i]: create slide + iterate shapes[] → create_shape()
- Handle content_key resolution from detail_plan
- File: `pptx_builder.py` — `build_slide()`
- Test: build single slide from layout_plan

### T3.4 Full Deck Builder
- Iterate all slides in layout_plan → build_slide()
- Run overflow detection (3-strategy auto-fix)
- Run quality gate
- File: `pptx_builder.py` — `build_deck()`
- Test: build full 9-slide deck

### T3.5 Backward Compatibility
- Keep compose_with_layout_plan() as "strict mode" opt-in
- Add deprecation warning when called
- File: `ppt_compositor.py` — add warning
- Test: strict mode still works

---

## T4: Preview Engine (3 tasks, 3h)

### T4.1 PNG Renderer
- Render layout_plan slide to PNG using Pillow/cairosvg
- Fallback: SVG → PNG conversion
- File: `preview_engine.py` (new) — `render_slide_png()`
- Test: render 3 slide types

### T4.2 HTML Fallback Renderer
- If PNG/SVG fail: generate HTML static page with positioned divs
- File: `preview_engine.py` — `render_slide_html()`
- Test: render comparison slide

### T4.3 Preview Pipeline
- layout_plan → iterate slides → render (PNG > SVG > HTML) → save to work/previews/
- Generate summary.txt with per-slide text summary
- File: `preview_engine.py` — `generate_previews()`
- Test: full 9-slide preview generation

---

## T5: Pipeline Integration (4 tasks, 4h)

### T5.1 Phase Router
- Implement 7-phase state machine with confirmation points
- State: P0→P1→P2→P3→P4→P5→P6
- Confirmation gates: #1(P1), #2(P2), #3(P3), #3.5(P4-B), #4(P5), #5(P6-optional)
- File: `pipeline.py` (new)
- Test: state transitions with/without confirmations

### T5.2 SKILL.md v3 Update
- Rewrite SKILL.md to reflect v3 pipeline
- Update function table, phase descriptions, confirmation points
- File: `SKILL.md`
- Test: human review

### T5.3 Prompt Updates
- Update outline_prompt.md: add slide_type guidance
- Update detail_prompt.md: add content_block type constraints
- Add layout_prompt.md: LLM prompt for custom reference mapping
- File: `prompts/`
- Test: prompt produces valid JSON outputs

### T5.4 End-to-End Integration Test
- Run full pipeline with henan_ppt source + custom reference
- Verify: 9 slides, all confirmation points hit, output editable
- File: `tests/test_e2e_v3.py`
- Test: pass

---

## T6: Built-in Styles (3 tasks, 2h)

### T6.1 Style Preview Thumbnails
- Generate 800x450 PNG thumbnail for each built-in style
- Show sample COVER + CONTENT slide
- File: `builtin_styles/{name}/preview.png`
- Test: 5 thumbnails exist

### T6.2 Style Selection UI Message
- Format style choice message with thumbnail descriptions
- File: `pipeline.py` — `format_style_choice()`
- Test: message format

### T6.3 Style Application Verification
- Apply each built-in style to a test deck → verify colors/fonts match DSL
- File: `tests/test_builtin_styles.py`
- Test: 5 styles pass

---

## T7: Tests (5 tasks, 5h)

### T7.1 Unit Tests — StyleExtractor
- Test style extraction from various .pptx templates
- Test reference_lib building
- Test built-in DSL validation
- File: `tests/test_style_extractor.py`
- Count: ~15 tests

### T7.2 Unit Tests — LayoutComposer
- Test layout selection for all slide types
- Test custom reference borrowing
- Test layout_plan v3 schema validation
- File: `tests/test_layout_composer.py`
- Count: ~20 tests

### T7.3 Unit Tests — PPTXBuilder
- Test each shape type creation
- Test overflow detection and auto-fix
- Test quality gate
- File: `tests/test_pptx_builder.py`
- Count: ~20 tests

### T7.4 Unit Tests — Preview Engine
- Test PNG/SVG/HTML rendering
- Test degradation fallback
- File: `tests/test_preview_engine.py`
- Count: ~10 tests

### T7.5 Integration Test — Full Pipeline
- Test complete P0→P6 flow with mock LLM
- Verify all 5 confirmation points
- Verify output .pptx is editable (no whole-slide images)
- File: `tests/test_pipeline_v3.py`
- Count: ~8 tests

---

## Execution Order

```
T1.1 → T1.2 → T1.3
T1.4 (parallel)
T2.1 → T2.2 → T2.3 → T2.4 → T2.5
T3.1 → T3.2 → T3.3 → T3.4 → T3.5
T4.1 → T4.2 → T4.3
T5.1-T5.4 (after T1-T4)
T6.1-T6.3 (after T1)
T7.1-T7.4 (parallel with T1-T4)
T7.5 (after T5)
```

## Migration Notes

- v2.3 `compose_with_layout_plan()` preserved as "strict mode"
- v2.3 `template_parser.py` reused for reference parsing
- v2.3 `schema.py` extended (not replaced)
- v2.3 prompts/ directory extended with new layout_prompt.md
- New files: style_extractor.py, layout_composer.py, pptx_builder.py, preview_engine.py, pipeline.py
