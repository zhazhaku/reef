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

---

## 附录 — P0 任务补丁 (v3.0, GAP-REPORT 全采纳)

### 任务追加 / 重构

| ID | 任务 | 估时 | 依赖 | 关联 P0 |
|----|------|------|------|---------|
| **T0**  | (推迟到 v3.1) 多源解析器 pdf/xlsx/url | — | — | G0-10 (Q4=B) |
| T1.5 | StyleExtractor 增加 4 级 style_ref 解析器 + 白名单生成 | 2h | T1 | G0-2/G0-7 |
| T1.6 | Theme fallback: clrScheme 为空时注入 business_blue + .pptx 损坏/密码预检 | 1.5h | T1 | G0-11/G0-12 |
| T2.1 | shape_id/content_key 命名空间生成器 + `validate_namespace()` | 1h | T2 | G0-3/G0-5 |
| T2.6 | LayoutComposer 增加 `validate_geometry()` (边界 clamp + IOU 重叠 + CJK 宽度) | 3h | T2.1 | G0-4/G0-13 |
| T2.7 | LLM schema-locked output (instructor + Pydantic Literal 白名单) | 2h | T1.5 | G0-7 |
| T2.8 | `auto_grid_layout()` 降级布局 (2x2/3x1/3x2) | 1h | T2.6 | EH-7 |
| T3.5 | Table 新建路径: `add_table()` + merged_cells | 1.5h | T3 | G0-9 (Q2=A) |
| T3.6 | PPTXBuilder 主题/母版注入 (clone_master from reference) | 2h | T3 | G1-15 |
| T3.7 | `verify_output()` 完整性校验 (重打开 + XML well-formed) | 1.5h | T3 | G0-11 |
| T3.8 | CJK 字体子集嵌入 (subset 微软雅黑/思源黑体到 .pptx) | 2h | T3 | G0-8 |
| **T4** | Preview Engine 重写 (SVG 主路径) | 5h | T3 | G0-1 (Q1=A) |
| T4.5 | LO 进程池 + 超时 kill + tmpdir 清理 (PNG 可选路径) | 2h | T4 | EH-5 |
| T4.6 | HTML 降级路径 (SVG 失败时启用) | 1h | T4 | G1-31 |
| T5.5 | Session 持久化 (`session.json` 原子写 + `--resume` CLI) | 3h | T5 | G0-6 (D15) |
| T5.10 | LLM 重试 + 模型降级 (quality→balanced→budget, 指数退避) | 2h | T5 | Spec 9 |
| T5.11 | 5 确认点统一措辞模板 + feishu 长消息分片 | 1h | T5 | G1-20/G1-21 |
| T5.12 | Refinement 收敛保证 (≤3 轮 + diff 校验 + 强制选择) | 1.5h | T5 | Spec 11 |
| T5.13 | 确认点超时归档 (1h 提示 / 24h archive) | 1h | T5.5 | D15 |
| T6.4 | 5 内置样式 DSL 实现 (D8) + theme fallback 配色 | 1.5h | T6 | Q5=A/G0-12 |
| T7.x | 测试体系新增: LLM fixture 录制 + schema 验证 + 几何边界用例 + .pptx 完整性 + checkpoint resume | 4h | T1-T6 | G0-14/G1-26 |

### 总工时修正

| 阶段 | 原始 (h) | P0 增量 (h) | 修正 (h) |
|------|----------|-------------|----------|
| T1 StyleExtractor | 4 | +3.5 | 7.5 |
| T2 LayoutComposer | 5 | +6 | 11 |
| T3 PPTXBuilder | 5 | +7 | 12 |
| T4 Preview Engine | 3 | +3 | 6 |
| T5 Pipeline | 4 | +8.5 | 12.5 |
| T6 Built-in Styles | 3 | +1.5 | 4.5 |
| T7 Tests | 5 | +4 | 9 |
| T0 (v3.1) | — | — | (推迟) |
| **合计** | **29** | **+33.5** | **~62.5h** |

注: 比 GAP-REPORT 预估 (~55h) 略高，原因为追加 T2.8 / T4.6 / T5.13 / T3.8 等细化任务。

### 执行 DAG (v3.0)

```
T1 → T1.5 → T1.6 ──┐
T2 → T2.1 → T2.6 → T2.7 → T2.8 ──┐
T3 → T3.5,3.6,3.7,3.8 ──────────┤
T4 → T4.5 → T4.6 ───────────────┤
                                 ├→ T5 → T5.5 → T5.10..13 → T7
T6 → T6.4 ───────────────────────┘
```

### 推迟到 v3.1 的任务

- T0 多源解析器 (pdf/xlsx/url, ~6h)
- G1-19 内置样式扩展到 10 种 (~3h)
- G1-28 并发/多用户隔离 (~4h)
- G1-29 结构化日志体系完整化 (~2h)
- G1-32 跨平台字体差异统一处理 (~2h)

### 里程碑

- **M1** (T1-T2.7 完成): StyleExtractor + LayoutComposer 可独立产出 layout_plan.json 通过 schema 验证
- **M2** (T3.x 完成): PPTXBuilder 可生成可打开的 .pptx (Spec 10 通过)
- **M3** (T4.x 完成): SVG 预览 + LO 降级 + HTML 兜底链路打通
- **M4** (T5.x 完成): 5 确认点 + session resume + LLM 降级全链路通
- **M5** (T7 完成): 测试覆盖 + 河南移动 PPT 复测通过 = v3.0 GA

---

*P0 任务补丁结束。可启动 M1 编码。*
