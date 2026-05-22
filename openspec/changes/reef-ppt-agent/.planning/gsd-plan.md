# Reef PPT Agent v2.3 — GSD 计划 (母版修复 + 空间分组 + 容器克隆)

> Version: v3.1 (对齐 FINAL-DESIGN.md v2.3)
> Design: FINAL-DESIGN.md (2026-05-21)
> Architecture: AI 分析 + 引擎执行分离 | v1 克隆引擎（自定义模板） + v2 SVG 引擎（内置风格）
> **v2.3 融合**: P0 母版修复 (shutil.copy) | 空间分组 (detect_template_patterns) | 容器克隆 (clone_group)
> Status: ✅ **S1-S3 完成** — 41 new tests passing — P0/Spatial/Clone 3 项新增待实施 (~9h)
> Updated: 2026-05-21

---

## 架构决策摘要

| 决策 | 选择 | 理由 |
|------|------|------|
| 自定义模板引擎 | **v1 克隆引擎** (XML deep copy) | 零样式丢失，完美保留模板 PLACEHOLDER + 主题继承 |
| 内置风格引擎 | **v2 SVG 引擎** (ppt-master native) | 无模板保真度要求，AI 自由创作 |
| 模板理解 | **AI LLM** → `design_language.md` | 替代机械 `_auto_match_shape`，AI 深度参与设计决策 |
| 内容布局 | **AI LLM** → `layout_plan.json` | 精确 shape→content 映射，替代 placeholder idx 猜测 |
| 样式继承 | 用户指定 > 模板原始 > 主题默认 | 三层优先级，模板原始样式 100% 保留 |

---

## 计划总览

| 阶段 | 名称 | 工时 | 任务 | 新增 UT | 新增 IUT | 状态 |
|:---:|------|:---:|:---:|:---:|:---:|:---:|
| P0 | 🔥 母版保留修复 | 0.5h | 2 | 2 | 2 | ⬜ |
| S1 | AI 模板分析 + v1 克隆引擎增强 | 12h | 9 | ~15 | ~8 | ✅ |
| S2 | 预览管道 | 6h | 5 | ~8 | ~4 | ✅ |
| S3 | 溢出处理 + 质量门 | 6h | 6 | ~10 | ~5 | ✅ |
| S4 | 🔥 空间分组 + 容器克隆 | 6h | 6 | ~8 | ~4 | ⬜ |
| **合计** | | **~30.5h** (~4 工作日) | **28** | **~43** | **~23** | |

---

## 依赖关系图

```
P0 (母版保留) ──── 阻塞性前置（必须先修）

S1 (AI分析+克隆引擎) ─────┬──→ S2 (预览管道)
                          │
                          └──→ S3 (溢出+质量门)

S4 (空间分组+容器克隆) — 依赖 S1 (需要 design_language.md / layout_plan.json)
                          └── 与 S2/S3 可并行

执行顺序: P0 → S1 → {S2, S3, S4 并行}
```

---

## 已有基础设施（无需重建）

| 模块 | 文件 | 测试 |
|------|------|:---:|
| 模板解析器 | `template_parser.py` | 70 ✅ |
| PPT 合成器（v1 克隆） | `ppt_compositor.py` | 33 ✅ |
| pptx→SVG 桥接 | `v2.0/pptx_to_svg/pptx_to_svg.py` | 7 ✅ |
| SVG→PPTX 导出 | `v2.0/svg_export/svg2pptx.py` | 8 ✅ |
| 多源素材预处理 | `v2.0/source_to_md/` (6 scripts) | 89 ✅ |
| 内置风格系统 | `style_catalog.yaml` + loader + resolver | 17 ✅ |
| SVG 内容注入器 | `v2.0/svg_content_injector.py` | 15 ✅ |
| spec_lock 设计锁 | `v2.0/spec_lock.py` | 11 ✅ |
| 质量门编排器 | `v2.0/quality_gate.py` | 40 ✅ |
| 动画注入器 | `v2.0/animation_injector.py` | — |
| Go 状态机 | `ppt_workflow.go` | 7 ✅ |
| Agent Prompts | `prompts/` (4 files) | 14 ✅ |
| **总计** | | **311** ✅ |

---

## P0: 母版保留修复 🔥 新增（v2.3）— 0.5h

> **优先级**: P0 — 模板保真度基础前提，必须先修

| 任务 | 文件 | 工时 | UT | IUT |
|------|------|:---:|:--:|:---:|
| P0.1 | `ppt_compositor.py` — `compose()` / `compose_with_layout_plan()` 改用 `shutil.copy` 方案 | 0.3h | 2 | 1 |
| P0.2 | 集成验证 — 3 master/14 layout 模板测试母版保留 | 0.2h | — | 1 |

---

## 阶段 1: AI 模板分析 + v1 克隆引擎增强 ✅ 12h (S1.A.1–S1.C.3)

### S1.A — AI 模板深度分析 Prompt (3 tasks, 3h)

> 新增 Phase 1 核心输出：`design_language.md`

- [x] **S1.A.1** 创建 `prompts/template_analysis_deep.md` Prompt 文件
  - 输入: `structure.json` (template_parser.py 机械数据)
  - 输出: `design_language.md` (视觉 DNA + 布局模式 + 元素语义 + 内容适配规则)
  - 包含：配色逻辑分析、字体层次体系、每类页面分区、装饰/功能元素角色、溢出策略
  - **UT**: Prompt 模板包含所有 4 个必需 section；schema 约束验证
  - **IUT**: 对 3 种真实模板（企业蓝/学术/创意）生成 design_language.md，人工审核合理性
  - 工时: 2h

- [x] **S1.A.2** 创建 `v2.0/design_language_parser.py`
  - 解析 `design_language.md` → 结构化 dict
  - 提取：color_palette, font_hierarchy, slide_zones, element_roles, content_rules
  - **UT**: 解析有效文档 → 正确字段；解析空/残缺文档 → 优雅降级
  - **IUT**: 对接 S1.A.1 生成的真实 design_language.md 文件
  - 工时: 1h

- [x] **S1.A.3** E2E 测试：Phase 1 完整链路
  - `template.pptx → template_parser → LLM → design_language.md → design_language_parser → 结构化 dict`
  - 3 种模板 × 1 种内容 = 3 条测试
  - 工时: — (含在 S1.A.1 的 IUT 中)

**UT/IUT**: ~5 UT + ~3 IUT

---

### S1.B — AI 内容-布局智能匹配 (3 tasks, 5h)

> 新增 Phase 3 核心：`layout_plan.json` 替代 `_auto_match_shape`

- [x] **S1.B.1** 创建 `prompts/layout_planning.md` Prompt 文件
  - 输入: `design_language.md` + `detail_plan.json` + `structure.json`
  - 输出: `layout_plan.json` (每页 slide → 每个 content_block → 目标 shape 精确映射)
  - 关键规则: shape 选择优先级（语义匹配 > 空间匹配 > 字号匹配）、溢出处理策略、内容分块规则
  - **UT**: Prompt 包含所有必需约束；schema 输出格式验证
  - **IUT**: 对 2 种内容类型（文字密集型/图文混排）× 3 种模板，验证映射合理性
  - 工时: 2h

- [x] **S1.B.2** 改造 `ppt_compositor.py` 添加 `compose_with_layout_plan()`
  - 新函数接收 `layout_plan.json` 替代 `_auto_match_shape`
  - 保留现有 `compose()` 作为 fallback（向后兼容）
  - 精确匹配：`layout_plan` 指定 shape_index → 直接定位
  - 样式继承：从模板 shape 读取 `_get_effective_text_style()` → 应用到填充文本
  - **UT**: layout_plan 有效 → 正确填充；layout_plan 缺字段 → 回退到 `_auto_match_shape`
  - **IUT**: 3 种模板 × 2 种内容类型 → 输出 PPTX 验证 shape 选择正确性
  - 工时: 3h

- [x] **S1.B.3** `layout_plan.json` Schema 验证
  - 新增 `schema.py::validate_layout_plan()` 函数
  - 验证：顶层 keys、slide 数量匹配、shape_index 有效性、content_block 引用完整性
  - **UT**: 有效 plan → 通过；无效 plan（越界 shape_index、缺失 slide、无效 content_id）→ 报错
  - **IUT**: 对接 S1.B.1 生成的 layout_plan.json
  - 工时: — (含在 S1.B.1)

**UT/IUT**: ~6 UT + ~5 IUT

---

### S1.C — S1 集成验证 (3 tasks, 4h)

- [x] **S1.C.1** 完整 E2E 测试脚本 `test/test_e2e_v22.py`
  - 3 种真实模板（企业蓝.pptx / 学术红.pptx / 创意黑.pptx）× 2 种内容类型（文字密集型 / 图文混排）= 6 条 E2E
  - 每条测试: Phase 1 (template_parser + AI 分析) → Phase 2 (大纲) → Phase 3 (detail_plan + layout_plan) → Phase 5 (compose_with_layout_plan)
  - 验证项: 所有 content_block 都已填充 / 字体正确继承 / 无溢出 / 无残留模板文本
  - 工时: 3h

- [x] **S1.C.2** 创建测试 fixture PPTX（3 个）
  - 企业蓝：16:9 蓝色主题，复杂图文混排
  - 学术红：4:3 红色主题，标题+正文
  - 创意黑：16:9 暗色主题，多装饰元素
  - 工时: 1h

- [x] **S1.C.3** 回归验证
  - 确保现有 311 tests 全部通过
  - 确保 v1.0 compose() fallback 路径正常工作
  - 工时: — (含在 S1.C.1)

**UT/IUT**: ~4 UT + ~0 IUT（均为 E2E 测试）

---

## 阶段 2: 预览管道 ✅ 6h

### S2.A — SVG → PNG 渲染 (2 tasks, 3h)

- [x] **S2.A.1** 创建 `v2.0/preview_renderer.py`
  - 支持 cairosvg（优先）和 LibreOffice headless（fallback）
  - 输入: SVG 目录 → 输出: PNG 目录 (1280×720)
  - 自动检测可用渲染器
  - **UT**: cairosvg 可用 → PNG 生成；cairosvg 不可用 → 回退到文本摘要
  - **IUT**: 真实 9 页模板 SVG → 9 张 PNG，验证尺寸和内容可辨识
  - 工时: 2h

- [x] **S2.A.2** 文本预览摘要生成
  - `preview_renderer.py::generate_text_summary()` — 逐页显示标题+正文前 200 字
  - 格式化为 Markdown/卡片式，适合飞书消息展示
  - **UT**: plan → summary 包含所有页的标题
  - **IUT**: 对接真实 detail_plan.json → 用户可读摘要
  - 工时: 1h

---

### S2.B — 逐页微调对话 (3 tasks, 3h)

- [x] **S2.B.1** 创建 `v2.0/preview_cache.py`
  - 缓存机制：`{template_hash}/{preview_version}/` 目录结构
  - 增量更新：仅重新渲染改变的页
  - **UT**: 首次生成 → 全部缓存；仅改 1 页 → 仅更新 1 页
  - **IUT**: 微调后重新预览 → 缓存命中率验证
  - 工时: 1h

- [x] **S2.B.2** 创建 `prompts/refinement_prompt.md`
  - 用户自然语言调整 → 结构化 overrides
  - 支持: 文本修改、字号调整、颜色替换、布局微调、图片替换
  - **UT**: Prompt 包含所有调整类型；输出 schema 验证
  - 工时: 1h

- [x] **S2.B.3** 更新 `edit_and_compose.py` 支持 refinement 输入
  - 新增 `--refinement overrides.json` 参数
  - overrides 合并到 detail_plan 的 content_blocks[].overrides
  - **UT**: overrides → 正确应用到 SVG/PPTX
  - **IUT**: 微调 → 重新生成 → 验证修改生效
  - 工时: 1h

**UT/IUT**: ~8 UT + ~4 IUT

---

## 阶段 3: 溢出处理 + 质量门 ✅ 6h

### S3.A — 内容溢出自动检测 (3 tasks, 3h)

- [x] **S3.A.1** 增强 `svg_text_overflow_detector.py`
  - 新增 `detect_in_layout_plan()` — 在填充前预检测（字体大小 × 字符数 vs 形状宽度）
  - 新增 `suggest_fix()` — 自动推荐修复策略（缩字号 / 分页 / 缩写 / 改写）
  - **UT**: 明显溢出 → 检测到；刚好合适 → 不误报
  - 工时: 1.5h

- [x] **S3.A.2** 创建 `v2.0/overflow_handler.py`
  - 实现 3 种溢出修复策略: 缩字号（保持可读性的最小字号）→ 分页（内容拆分到额外页）→ LLM 改写（缩短文案）
  - 策略优先级: LLM 改写 > 缩字号 > 分页
  - **UT**: 轻度溢出 → 缩字号修复；重度溢出 → LLM 改写
  - **IUT**: 对 2 种溢出场景（标题过长 / 列表项过多）验证
  - 工时: 1.5h

- [x] **S3.A.3** `overflow_handler.py` 集成到 `compose_with_layout_plan()`
  - 在填充前运行溢出检测
  - 自动应用修复策略
  - 生成 `overflow_report.json` 记录所有修复
  - 工时: — (含在 S1.B.2)

---

### S3.B — 质量门 (2 tasks, 3h)

- [x] **S3.B.1** 增强 `quality_gate.py` 集成 FINAL-DESIGN 规范
  - 新增 `check_font_inheritance()`: 验证所有形状的字号 > 0、非默认 12pt
  - 新增 `check_color_consistency()`: 验证使用的颜色都在 design_language 调色板内
  - 新增 `check_image_resolution()`: 验证嵌入图片 ≥ 800×600
  - 新增 `check_shape_coverage()`: 验证 layout_plan 覆盖了所有文本形状
  - **UT**: 各 checker 独立测试（正确/错误情况）
  - **IUT**: 集成到 compose 输出 → 验证质量报告正确
  - 工时: 2h

- [x] **S3.B.2** `quality_gate.py` 集成到 compose 管线
  - compose 完成后自动运行 quality_gate
  - 生成 `quality_report.json`
  - 非阻塞模式：仅报告，不阻止输出
  - **UT**: 质量门通过 → 正常输出；质量门失败 → 报告包含错误详情
  - 工时: 1h

### S3.C — S3 集成测试 (1 task, —)

- [x] **S3.C.1** `test/test_e2e_overflow_and_quality.py`
  - 构造溢出场景 → 验证修复策略
  - 构造质量违规 → 验证质量报告
  - 正常场景 → 零违规
  - 工时: — (含在 S3.A/S3.B 的 IUT 中)

**UT/IUT**: ~10 UT + ~5 IUT

---

## 阶段 4: 空间分组 + 容器克隆 🔥 新增（v2.3）— ~6h

> v2.3 核心增强：LLM 不再看扁平 shape 列表，而是看空间分组后的组件树，可识别容器并克隆扩展。

### S4.A — 空间分组 (`detect_template_patterns`) (3 tasks, 3h)

- [ ] **S4.A.1** `template_parser.py` — 新增 `detect_template_patterns()`
  - 4 条检测规则: 容器检测、重复模式、层级关系、装饰元素
  - 输出增强 `structure.json` 含 `components` 和 `spatial_zones`
  - 工时: 2h
- [ ] **S4.A.2** `template_parser.py` — 单元测试 (UT)
  - card 组件检测、可重复组识别、空间分区
  - 工时: 0.5h
- [ ] **S4.A.3** `template_parser.py` — 集成测试 (IUT)
  - 用含 card 组件的模板测试完整结构输出
  - 工时: 0.5h

**UT/IUT**: ~4 UT + ~2 IUT

### S4.B — 容器克隆 (`clone_group` / `expand_container_group`) (2 tasks, 2h)

- [ ] **S4.B.1** `ppt_compositor.py` — 新增 `expand_container_group()` + 注册 `clone_group` action
  - 深拷贝最后一个容器组的 XML → 偏移 `offset_emu` → 清除占位文字
  - 在 `LAYOUT_PLAN_ACTIONS` 中注册为 `"clone_group"`
  - 工时: 1.5h
- [ ] **S4.B.2** `ppt_compositor.py` — 单元/集成测试 (UT + IUT)
  - 3-card 模板扩展为 4-card 输出验证
  - 工时: 0.5h

**UT/IUT**: ~2 UT + ~2 IUT

### S4.C — Prompt 更新 + E2E 验证 (1 task, 1h)

- [ ] **S4.C.1** `prompts/layout_planning.md` — 添加容器感知匹配规则和 `clone_group` 行动说明
  - 工时: 0.5h
- [ ] **S4.C.2** E2E — 用含 card 组件的模板测试 3→4 card 扩展全链路
  - 工时: 0.5h

**UT/IUT**: ~1 UT + ~1 IUT

---

## 总计

| 指标 | 值 |
|------|-----|
| 总任务 | 28 (P0:2 + S1:9 + S2:5 + S3:6 + S4:6) |
| 总工时 | ~30.5h (~4 工作日) |
| 新增 UT | ~43 |
| 新增 IUT | ~23 |
| 已有测试 | 311 ✅ |
| 完成后测试总计 | ~377 |

---

## 执行顺序

```
Wave 1 (可并行):
  ├── S1.A.1: template_analysis_deep.md Prompt
  ├── S1.A.2: design_language_parser.py
  └── S1.C.2: 测试 fixture PPTX

Wave 2 (依赖 Wave 1):
  ├── S1.A.3: Phase 1 E2E 测试
  ├── S1.B.1: layout_planning.md Prompt
  └── S1.B.3: layout_plan.json Schema 验证

Wave 3 (依赖 Wave 2):
  ├── S1.B.2: compose_with_layout_plan() 改造
  └── S1.C.1: S1 完整 E2E 测试

Wave 4 (依赖 S1 完成, S2/S3 可并行):
  ├── S2.A.1: preview_renderer.py
  ├── S2.A.2: 文本预览摘要
  ├── S2.B.1: preview_cache.py
  ├── S3.A.1: 溢出检测增强
  └── S3.B.1: 质量门增强

Wave 5 (依赖 Wave 4):
  ├── S2.B.2: refinement_prompt.md
  ├── S2.B.3: edit_and_compose.py refinement 支持
  ├── S3.A.2: overflow_handler.py
  └── S3.B.2: 质量门集成

Wave 6 (依赖 Wave 5):
  ├── S1.C.3: 回归验证
  ├── S3.C.1: S3 集成测试
  └── 全链路 E2E 验证

Wave 7 (v2.3 新增, P0 先行, S4 可并行):
  ├── P0.1: compose_with_layout_plan() shutil.copy 方案
  ├── S4.A.1: detect_template_patterns() 实现
  ├── S4.B.1: expand_container_group() 实现
  └── S4.C.1: layout_planning.md prompt 更新

Wave 8 (依赖 Wave 7):
  ├── P0.2: master 保留集成验证
  ├── S4.A.2+: 空间分组测试
  ├── S4.B.2: clone_group 测试
  └── S4.C.2: E2E 全链路 3→4 card 扩展
```

---

## 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|:---:|:---:|------|
| LLM 生成的 layout_plan.json 不准确 | 中 | 高 | 保留 `_auto_match_shape` fallback；支持人工 Review |
| design_language.md 质量不稳定 | 中 | 中 | 模板分析 Prompt 含严格 schema 约束；支持人工修正 |
| 溢出处理策略不够智能 | 低 | 中 | 3 种策略可组合；LLM 改写作为最终兜底 |
| cairosvg/LibreOffice 不可用 | 低 | 低 | 文本摘要 fallback；依赖检测 + 优雅降级 |

---

## 相关文档

| 文档 | 路径 |
|------|------|
| 最终设计 (v2.3) | `FINAL-DESIGN.md` |
| 融合设计 (v2.1) | `ppt-agent-v2-fusion-design.md` |
| 需求缺口修复 | `ppt-agent-v2.1-design-gap-fix.md` |
| v2.2 架构决策 | `ppt-agent-v2.2-deep-improvement-plan.md` |
| v2.3 保真度修复 | `/tmp/ppt_fidelity_fix_plan.md` |
| ppt-master 分析 | `ppt-master-analysis.md` |
| v2.1 GSD 计划 | `gsd-plan-v2.md` |
| SKILL.md | `../../skills/ppt-agent/SKILL.md` |
