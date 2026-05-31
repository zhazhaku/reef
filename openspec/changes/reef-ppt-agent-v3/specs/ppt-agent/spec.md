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

---

## 附录 B — P0 修复增补 (v3.0)

> 决策来源: GAP-REPORT.md + DECISIONS.md D9-D15 | 全部采纳建议

### B.1 新增不变量 INV-6 ~ INV-11

- **INV-6 style_ref 白名单**: 所有 `layout_plan.json` 中的 `style_ref` 必须存在于 `reference_lib.json` 键集合或 `built_in_dsl` 5 种 ID 之中。schema 校验失败拒绝写入。
- **INV-7 Checkpoint 持久化**: 每个确认点 (#1~#5) 通过后，`session.json` 必须原子写入 (`os.replace`)，否则视为该 phase 未完成。
- **INV-8 几何边界**: `layout_plan` 所有 shape 坐标必须满足 `0 ≤ x, x+cx ≤ slide_width` 且 `0 ≤ y, y+cy ≤ slide_height`；越界由 `validate_geometry()` auto-clamp。
- **INV-9 CJK 宽度估算**: 文本溢出检测必须按 `est_width = sum(2*em if is_cjk(c) else 1*em) * font_size_pt * 12700` + 10% padding 计算，不得按英文字符宽度估算。
- **INV-10 .pptx 完整性**: `PPTXBuilder.write()` 后必须用 `Presentation()` 重新打开 + 所有 XML 部件 `etree.fromstring()` 验证，失败回退 checkpoint。
- **INV-11 修正轮次上限**: P5 refinement loop 单 session 最多 3 轮，第 4 轮强制选择 (进入 P6 / 降级到上一稳定版本)。

### B.2 新增行为规格 Spec 7 ~ Spec 11

#### Spec 7 — Checkpoint 恢复
- **GIVEN** 用户在 P3 之后中断会话
- **WHEN** 执行 `ppt-agent --resume <session_id>`
- **THEN** 系统从 `session.json` 读取 `current_phase=P3`，从该 phase 开始继续，已完成 phase 的产物 (source.md/outline.json/detail_plan.json) 直接复用，不重新生成

#### Spec 8 — 几何验证
- **GIVEN** LayoutComposer 产出 `layout_plan.json`
- **WHEN** PPTXBuilder 接收前
- **THEN** `validate_geometry()` 必须执行；越界自动 clamp 并 warn；重叠 IOU>0.3 警告；失败 shape ≥3 个时 fallback 到 `auto_grid_layout()`

#### Spec 9 — LLM 重试与降级
- **GIVEN** LLM 调用 (任意 phase)
- **WHEN** 3 次连续失败 (timeout / schema invalid)
- **THEN** 按模型 profile 降级 (quality → balanced → budget)；3 个 profile 全失败 → 上报用户并保存当前 checkpoint
- **重试退避**: 1s / 4s / 16s (指数)

#### Spec 10 — .pptx 损坏检测
- **GIVEN** PPTXBuilder.write() 完成
- **WHEN** 输出文件落盘
- **THEN** `verify_output(path)` 必须执行：Presentation() 重打开 + 所有 .xml 部件 well-formed 校验；失败 → 回退到上一 checkpoint 重试 1 次；仍失败 → 上报用户并保留损坏文件供诊断

#### Spec 11 — 修正循环收敛
- **GIVEN** 用户在 P5 拒绝预览
- **WHEN** 进入 refinement loop
- **THEN** 单 session 累计 ≤3 次；第 4 次系统消息「已达修正上限，请二选一: (a) 强制进入 P6 (b) 降级到上一稳定预览」；每轮 diff 必须 ≥1 字段变化，否则视为无意义重试

### B.3 新增错误处理 EH-5 ~ EH-8

#### EH-5 LibreOffice 崩溃
- **触发**: LO subprocess exit code ≠ 0 或 ≥30s 无响应
- **处理**:
  1. SIGKILL 子进程 + 清理 `tmp_lo_*` 目录
  2. 进程池标记该 worker dead，重新拉起
  3. 当前 slide 降级到 SVG 路径
  4. 累计 LO 崩溃 ≥3 次/session → 全局禁用 LO，仅 SVG/HTML

#### EH-6 style_ref 幻觉
- **触发**: LLM 返回 `style_ref` 不在白名单
- **处理**:
  1. Pydantic 验证失败 (instructor 库自动重试)
  2. 重试 prompt 注入 `Available style_refs: [...]`
  3. 3 次失败 → 用 `built_in_dsl[default]` 替换 + warn

#### EH-7 几何溢出
- **触发**: `validate_geometry()` 返回 ≥1 越界/重叠
- **处理**:
  1. ≤2 个 shape 越界: auto-clamp 到边界内 + warn 日志
  2. ≥3 个: fallback 到 `auto_grid_layout()` (依 block 数选 2x2/3x1/3x2)
  3. 用户可在 P5 预览看到 warn 标记，refinement 时手动调整

#### EH-8 .pptx 损坏
- **触发**: Spec 10 验证失败
- **处理**:
  1. 第 1 次失败: 回退到上一 checkpoint (P5 预览状态) + 重新 build
  2. 第 2 次仍失败: 保留损坏 .pptx 到 `failed/<session>_<ts>.pptx` + 上报用户 + 提示诊断步骤

### B.4 既有 EH 细化

- **EH-1 LLM timeout 细化**: 按 Spec 9 三级降级 + 指数退避，移除原「一次失败即上报」的描述
- **EH-2 overflow 链补充**: 在 `shrink_title → split_to_two_slides → two_column` 之后追加 `auto_grid_fallback` 作为第 4 级兜底

### B.5 命名空间不变量补充 (D10 落地)

- shape_id 格式: `s{plan_slide}_{role}_{seq}`，正则 `^s[\w]+_[a-z]+_\d+$`
- content_key 格式: `{plan_slide}.{block_type}.{seq}`，正则 `^[\w]+\.[a-z_]+\.\d+$`
- 写入 `layout_plan.json` 前调用 `validate_namespace(plan)` 检查全局唯一 + 格式合规

---

*附录 B 结束。规约已锁定，可进入实现阶段。*
