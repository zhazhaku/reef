# Roadmap — PPT Agent v3

## Phase 1: StyleExtractor (4 tasks, 4h)
**Goal**: 能从任意 .pptx 提取 StyleProfile，定义 5 种内置风格 DSL
- T1.1 StyleProfile Schema
- T1.2 PPTX Style Extractor
- T1.3 Reference Library Builder
- T1.4 Built-in Style DSL Definitions

## Phase 2: LayoutComposer (5 tasks, 6h)
**Goal**: 根据 detail_plan + style_ref 生成 layout_plan.json v3
- T2.1 Layout Plan v3 Schema
- T2.2 Built-in Layout Templates (8 种)
- T2.3 Layout Selection Logic
- T2.4 Custom Reference Layout Borrowing
- T2.5 Layout Plan Generator

## Phase 3: PPTXBuilder (5 tasks, 6h)
**Goal**: 从空白构建 PPTX，每元素独立可编辑
- T3.1 Blank Deck Builder
- T3.2 Shape Factory
- T3.3 Slide Builder
- T3.4 Full Deck Builder + Overflow + Quality Gate
- T3.5 Backward Compatibility (strict mode)

## Phase 4: Preview Engine (3 tasks, 3h)
**Goal**: 强制预览生成，PNG→SVG→HTML 降级
- T4.1 PNG Renderer
- T4.2 HTML Fallback Renderer
- T4.3 Preview Pipeline

## Phase 5: Pipeline Integration (4 tasks, 4h)
**Goal**: 7 阶段状态机 + 确认点 + SKILL.md v3
- T5.1 Phase Router (状态机)
- T5.2 SKILL.md v3 Update
- T5.3 Prompt Updates
- T5.4 End-to-End Integration Test

## Phase 6: Built-in Styles + Polish (3 tasks, 2h)
**Goal**: 5 风格缩略图 + 选择 UI + 验证
- T6.1 Style Preview Thumbnails
- T6.2 Style Selection UI Message
- T6.3 Style Application Verification

## Phase 7: Tests (5 tasks, 5h)
**Goal**: 全覆盖测试 + 河南移动 PPT 验收
- T7.1 Unit Tests — StyleExtractor
- T7.2 Unit Tests — LayoutComposer
- T7.3 Unit Tests — PPTXBuilder
- T7.4 Unit Tests — Preview Engine
- T7.5 Integration Test — Full Pipeline (河南移动验收)
