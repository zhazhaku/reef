# R2 Features Research

> Researcher: R2-Features | Date: 2026-05-22
> Scope: Missing features, incomplete coverage, and risks in PPT Agent v3 design vs. v2.3 failure case and real-world AI PPT tools

---

## Critical Gaps (must fix before P1)

### CG-1: LayoutComposer LLM 输出的几何验证 — 新型失败模式未防护

v2.3 的核心失败是 `_auto_match_shape` 把内容塞错 shape。v3 用 LayoutComposer 从零定义布局，彻底绕开了 auto_match，但引入了**全新的失败模式**：LLM 生成的 `layout_plan.json` 中 `shapes[]` 的坐标/尺寸可能不合理。

**具体风险场景：**

| 问题 | 示例 | 后果 |
|------|------|------|
| **形状重叠** | 两个 text_box 的 (x,y,w,h) 区域交叉 | PowerPoint 中文字互相遮挡，用户看到乱码层叠 |
| **溢出画布** | shape 的 x+w > 12192000 (16:9 宽度) | PowerPoint 自动裁剪或布局错乱 |
| **比例失调** | 标题 shape 高度 8000000 (占画面 80%)，正文 shape 高度 500000 | 标题区巨大空白，正文文字挤成一条线 |
| **负坐标** | LLM 输出 x=-914400 | shape 不可见或被裁剪 |

**当前设计缺陷：** design.md §P4 和 tasks.md T2 均未提及几何验证。layout_plan.json v3 schema (design.md 数据契约) 只有 `{type, x, y, w, h, content_key, style_ref, action}` 的类型定义，没有约束条件。

**建议：** 在 T2.1 (Layout Plan v3 Schema) 中增加几何校验规则：
1. `0 ≤ x, x+w ≤ slide_width` 且 `0 ≤ y, y+h ≤ slide_height`
2. 任意两个 shapes 的 bounding box 交集面积 < 各自面积的 10%
3. 标题区高度 ≤ 画面 35%，正文区 ≥ 画面 40%
4. 在 `validate_layout_plan_v3()` 中强制检查，不通过则自动修正或拒绝

---

### CG-2: 表格 content_block 到 layout_plan shapes 的映射断裂

detail_plan.json 支持 `type: "table"` 的 content_block (design.md 数据契约明确列出)，但 layout_plan.json v3 schema 的 `shapes[]` 只有 `{type: "text_box" | "rectangle" | ...}` — **没有 `type: "table"` 的特殊字段**。

**缺失字段：**
- `rows`, `cols` (行列数)
- `col_widths[]` (列宽比例)
- `merge_cells[]` (合并单元格定义)
- `header_style`, `cell_style` (表头/单元格样式)

**影响：** 河南移动 PPT 的 slide 4 (64 shapes, 含表格) 和 slide 7 (34 shapes, 含流程+表格) 正是表格密集页。v2.3 的 auto_match 把表格行内容塞进装饰矩形 (ANALYSIS_REPORT.md §2.3)。v3 如果不能正确生成 table shape，同样无法处理这类页面。

**当前任务覆盖：** tasks.md T3.2 (Shape Factory) 提到 `table` 是支持的 shape 类型之一，但没有任何任务定义 table 的 schema、行列映射、或合并单元格逻辑。

**建议：** 
1. 在 T2.1 schema 中增加 `type: "table"` 的完整字段定义
2. 在 T2.2 增加含表格的 layout template (如 `data_table`, `comparison_table`)
3. 在 T3.2 Shape Factory 中增加 `create_table_shape()` 子任务，含行列数/合并/样式

---

### CG-3: 多源预处理 (P0) 无任务覆盖 — 5 种解析器是空中楼阁

design.md §P0 明确列出 5 种解析器：
- docx → mammoth
- pdf → pdfplumber  
- xlsx → openpyxl
- url → readability + trafilatura
- md/txt → 直接读取

**但 tasks.md 29 个任务中，没有一个任务涉及 P0 的解析器实现。** T5.1 (Phase Router) 只管状态机流转，不管数据输入。

**具体风险：**

| 解析器 | 关键问题 |
|--------|----------|
| **PDF (pdfplumber)** | pdfplumber 对表格解析质量差（合并单元格丢失、跨页表格断裂），对公式无能为力。河南移动素材如果是 PDF 格式的运营数据报表，表格数据会严重丢失 |
| **URL (readability + trafilatura)** | 两个库功能重叠但各有盲区。readability 对 SPA 页面无效；trafilatura 对中文网页提取质量不稳定。需要 fallback 链 + 人工校验 |
| **XLSX (openpyxl)** | 只提取文本，丢失图表。用户 Excel 中的图表是 PPT 的核心素材，openpyxl 无法提取 |
| **DOCX (mammoth)** | mammoth 只转 HTML，丢失 SmartArt、嵌入图表、自定义样式 |

**建议：**
1. 新增 T0 任务组 (4h) 覆盖 5 种解析器 + 统一 source.md 输出格式
2. PDF 解析增加 fallback：pdfplumber → PyMuPDF (fitz) → OCR (tesseract)
3. URL 解析增加 headless browser fallback (playwright)
4. XLSX 增加图表导出为图片的能力 (matplotlib 渲染)
5. 在 source.md 格式中增加 `![image](ref)` 标记，保留图片引用

---

## Important Risks (should address)

### IR-1: 图片处理 — v3 设计完全忽略

v3 spec INV-2 说"每元素独立可编辑"，但**整个 v3 设计文档没有一处提到图片如何处理**。

**缺失维度：**

1. **用户素材图片提取**：source.md 中的图片引用如何保留？PDF/DOCX 中的嵌入图片如何提取？
2. **图片占位符**：layout_plan.json shapes[] 没有 `type: "image"` 的特殊字段（aspect ratio、裁剪方式、fit 模式）
3. **自动裁剪/缩放**：用户素材图片尺寸各异，填入 layout 的 image shape 时如何处理？Cover? Contain? Stretch?
4. **AI 配图**：用户文字内容中没有图片时，是否自动从图库搜索配图？（Gamma/Tome 的核心功能）
5. **图片溢出**：大图填小框 → 裁剪；小图填大框 → 模糊/留白

**当前状态：** design.md 数据契约的 content_block 类型列表包含 `image`，但 layout_plan.json v3 schema 没有 image shape 的定义。tasks.md T3.2 提到 image 是支持的 shape 类型，但无具体任务。

**建议：**
1. layout_plan.json v3 schema 增加 `type: "image"` 的字段：`{fit: "cover"|"contain"|"stretch", aspect_ratio: "16:9"|"4:3"|"1:1", crop_hint: "center"}`
2. P0 解析器增加图片提取：PDF → PyMuPDF 提图，DOCX → python-docx 提图
3. source.md 增加 `![alt](path)` 图片引用格式
4. P6 PPTXBuilder 增加 `insert_image()` 方法，含 fit/crop 逻辑

---

### IR-2: 中文排版 — 零覆盖

河南移动 PPT 是纯中文内容，v3 设计**没有一处**提及中文排版需求：

| 需求 | 说明 | 当前状态 |
|------|------|----------|
| **中文字体回退链** | 微软雅黑 → 思源黑体 → Noto Sans CJK → 系统默认 | style_ref 只指定单一字体名，无 fallback |
| **中文行距/段距** | 中文默认行距 1.3-1.5 倍（比英文 1.0-1.2 大），段距通常 6-12pt | style_ref 无行距字段，PPTXBuilder 无行距设置 |
| **中文标点避头尾** | 逗号/句号不出现在行首，引号/括号不成对出现在行尾 | python-pptx 无原生支持，需手动调整 |
| **竖排文本** | 中文特有的竖排排版（封面标题、侧边标注） | 完全不支持 |
| **中文数字/单位** | "1.5亿" vs "150,000,000"，"万" vs "10⁴" | 无规范 |

**影响：** 如果 v3 用英文排版默认值生成中文 PPT，行距会过紧、标点位置会错误、字体可能在非 Windows 系统上回退到无中文字形的默认字体。

**建议：**
1. StyleProfile schema 增加 `line_spacing_ratio` 和 `paragraph_spacing_pt` 字段
2. 内置 5 种风格的 DSL 定义中明确中文默认值（行距 1.3、段距 8pt）
3. style_ref 增加 `font_fallback_chain: ["微软雅黑", "思源黑体", "Noto Sans CJK"]`
4. PPTXBuilder 增加中文标点避头尾的后处理步骤
5. 竖排文本标记为 P1+（可后做，但需在 schema 中预留 `writing_mode: "horizontal"|"vertical"` 字段）

---

### IR-3: 幻灯片母版/版式 — v3 从空白构建丢失了什么

v3 的核心架构决策是"从空白 Presentation() 构建"（design.md 引擎架构），这解决了 v2.3 的 auto_match 问题，但引入了新问题：

**丢失的能力：**
1. **母版统一修改**：PowerPoint 用户期望修改母版背景/页脚/页码时，所有幻灯片同步更新。v3 从空白构建意味着每页独立，没有母版关联。
2. **版式切换**：用户在 PowerPoint 中可以右键 → 版式 → 切换不同版式。v3 产物不支持此操作。
3. **主题色**：PowerPoint 的主题色系统允许一键换色。v3 直接写死 RGB 值，不使用主题色引用。
4. **页码/页脚占位符**：母版版式通常包含页码、日期、页脚占位符。v3 如果不创建这些占位符，用户无法通过母版统一管理。

**建议：**
1. PPTXBuilder 在创建空白 Presentation() 后，先构建一个 Slide Master + N 个 Slide Layouts
2. 每个 layout_plan 的 slide_type 映射到一个 Slide Layout
3. 使用 python-pptx 的主题色系统 (`theme_color` 而非 `rgb_color`)，允许用户在 PowerPoint 中一键换色
4. 在 Slide Master 中添加页码/日期/页脚占位符

---

### IR-4: 预览引擎的保真度问题

design.md §P5 的预览渲染管线是 PNG → SVG → HTML 三级降级。但**预览与最终 .pptx 的视觉一致性如何保证？**

**风险：**
1. **PNG 路径 (Pillow/cairosvg)**：从 layout_plan.json 渲染，不是从 .pptx 渲染。字体/间距/换行可能与 python-pptx 的实际输出不同。
2. **SVG 路径**：v2.3 有 `pptx_to_svg.py`（从 .pptx 转换），但 v3 的预览在 P6 之前生成，此时 .pptx 还不存在。
3. **HTML 路径**：用 positioned div 模拟，保真度最低。

**核心矛盾：** 用户在 P5 确认的预览，可能和 P6 生成的 .pptx 视觉不一致。如果用户基于预览确认，但最终产物不同，等于确认点失效。

**建议：**
1. 优先路径改为：先快速生成 .pptx → 用 LibreOffice headless 转 PNG → 展示预览
2. 如果 LibreOffice 不可用，才降级到 layout_plan → SVG → PNG
3. HTML 降级路径增加"此为低保真预览，最终效果以 .pptx 为准"的免责提示

---

### IR-5: 自定义参考路径的"风格借鉴"语义模糊

design.md §P4-B 说"借鉴 StyleProfile 的布局骨架，不复制 shape"。但"借鉴"的边界在哪？

**模糊场景：**
1. 参考模板有独特的装饰元素（如河南移动的红色弧线、蓝色渐变条），v3 是复制这些装饰的坐标和样式，还是重新生成类似风格的装饰？
2. 参考模板的卡片布局有圆角、阴影、边框，v3 是精确复现还是近似？
3. 参考模板有品牌 Logo，v3 是否保留？

**当前设计：** StyleProfile 的 `decor` 字段只有 `{accent_bar, card_radius, icon_style}` 三个维度，远不够描述复杂装饰。

**建议：**
1. 明确"借鉴"的定义：复制配色+字体+装饰风格参数，不复制具体 shape 坐标
2. StyleProfile.decor 增加字段：`border_style`, `shadow`, `gradient_patterns[]`, `logo_position`
3. 增加"装饰元素白名单"：哪些装饰可以自动生成（色条、圆角矩形），哪些需要用户确认（Logo、品牌标识）

---

## Recommendations

### R-1: 增加 T0 任务组 — 多源预处理 (4h)

当前 tasks.md 从 T1 开始，P0 的 5 种解析器无任务覆盖。建议新增：

| 任务 | 内容 | 依赖 |
|------|------|------|
| T0.1 | 统一 source.md schema (含图片引用、表格、公式标记) | — |
| T0.2 | DOCX 解析器 (mammoth + python-docx 提图) | T0.1 |
| T0.3 | PDF 解析器 (pdfplumber + PyMuPDF fallback + OCR fallback) | T0.1 |
| T0.4 | XLSX 解析器 (openpyxl + matplotlib 图表导出) | T0.1 |
| T0.5 | URL 解析器 (trafilatura + readability + playwright fallback) | T0.1 |
| T0.6 | 解析器集成测试 (5 种输入 → source.md 验证) | T0.2-5 |

### R-2: layout_plan.json v3 schema 增强字段

```json
{
  "shapes": [{
    "shape_id": "s1_title",
    "type": "text_box | rectangle | rounded_rectangle | image | table | connector",
    "x": 914400, "y": 2057400, "w": 10363200, "h": 2743200,
    "content_key": "title",
    "style_ref": {
      "font": "微软雅黑",
      "font_fallback_chain": ["思源黑体", "Noto Sans CJK"],
      "size_pt": 40,
      "bold": true,
      "color": "#C00000",
      "use_theme_color": "accent1",
      "line_spacing_ratio": 1.3,
      "paragraph_spacing_pt": 8,
      "writing_mode": "horizontal"
    },
    "action": "fill_text",
    // image-specific
    "fit": "cover | contain | stretch",
    "aspect_ratio": "16:9",
    "crop_hint": "center",
    // table-specific
    "rows": 3, "cols": 4,
    "col_widths": [0.3, 0.2, 0.25, 0.25],
    "merge_cells": [{"range": "A1:D1", "scope": "row"}],
    "header_style_ref": {...},
    "cell_style_ref": {...}
  }]
}
```

### R-3: 几何验证层

在 T2.1 (Layout Plan v3 Schema) 中增加 `validate_geometry()` 函数：
1. Bounding box 画布内检查
2. 重叠检测（允许 5% 误差容忍）
3. 比例合理性检查（标题/正文/装饰区域占比）
4. 自动修正：重叠 → 推移；溢出 → 缩放；比例失调 → 按模板比例调整

### R-4: 中文排版默认值

内置 5 种风格的 DSL 定义中增加：
```json
{
  "defaults": {
    "line_spacing_ratio": 1.3,
    "paragraph_spacing_pt": 8,
    "font_fallback_chain": ["微软雅黑", "思源黑体", "Noto Sans CJK"],
    "punctuation_avoidance": true,
    "writing_mode": "horizontal"
  }
}
```

### R-5: 母版/版式支持

在 T3.1 (Blank Deck Builder) 中增加：
1. 创建 Slide Master + 8 个 Slide Layouts (对应 8 种 layout template)
2. 使用 `theme_color` 引用替代硬编码 RGB
3. 添加页码/日期/页脚占位符到 Slide Master

### R-6: 预览保真度保证

在 T4.1 (PNG Renderer) 中修改优先路径：
1. **首选**：快速生成临时 .pptx → LibreOffice headless → PNG（最高保真）
2. **降级 1**：layout_plan → SVG → PNG（中等保真）
3. **降级 2**：layout_plan → HTML（低保真 + 免责提示）

---

## Open Questions for User

### OQ-1: 表格复杂度上限
v3 需要支持多复杂的表格？仅简单二维表（无合并单元格），还是需要支持任意合并单元格？这直接影响 T2.1 schema 和 T3.2 Shape Factory 的工作量。

### OQ-2: 图片来源策略
用户素材中没有图片时，是否需要 AI 自动配图？如果是：
- 图库来源：Unsplash API? Pexels API? 自建图库?
- 匹配逻辑：基于 slide title/gist 语义搜索?
- 版权风险：如何标注图片来源?

### OQ-3: 中文竖排文本优先级
中文竖排文本是 P0 必须、P1 应该、还是可以更晚？河南移动案例中没有竖排，但政府/金融行业 PPT 常用。

### OQ-4: 母版版式的期望
用户是否期望在 PowerPoint 中能通过"右键→版式"切换版式？如果是，PPTXBuilder 必须创建 Slide Layouts，工作量增加约 2h。如果不需要，每页独立构建即可。

### OQ-5: 自定义参考路径的 Logo 处理
参考 .pptx 中包含品牌 Logo 时，v3 应该：
- A) 自动提取并保留 Logo（位置+图片）
- B) 提取但需要用户确认
- C) 忽略 Logo（用户手动添加）
- D) 仅保留 Logo 位置占位符

### OQ-6: 与竞品的功能对齐优先级
以下竞品功能，哪些是 v3 P0 必须有的？
- AI 配图 (Gamma/Tome 核心功能)
- 一键换肤 (Tome 的 Apply Style)
- 演讲者备注自动生成 (Gamma)
- 动画/转场建议 (Tome)
- 实时协作编辑 (Google Slides)
- 导出为 PDF/图片 (通用需求)
- 多语言/翻译 (国际化)

### OQ-7: 预览确认的 UX
P5 预览确认时，用户修改指令的粒度是什么？
- A) 整页替换（重新生成整页 layout）
- B) 元素级修改（"把标题移到左边"、"把第 3 个卡片的内容换成…"）
- C) 全局修改（"所有页面的标题字号加大 4pt"）

B 和 C 需要更细粒度的 layout_plan 修改能力，当前 refinement_prompt.md 只支持 A。

---

## Appendix: v2.3 失败模式 → v3 新失败模式对照

| v2.3 失败 | v3 是否避开 | v3 新风险 |
|-----------|-------------|-----------|
| auto_match_shape 把内容塞错 shape | ✅ 避开（从零构建，无 auto_match） | ❌ LLM 生成的坐标/尺寸可能不合理（重叠、溢出、比例失调） |
| 跳过确认点导致错误放大 | ✅ 避开（5 确认点，INV-3 强制） | ⚠️ 确认点过多可能导致用户疲劳，跳过确认 |
| 复杂模板 shape 数 != 内容段数 | ✅ 避开（不依赖模板 shape） | ❌ 从零构建意味着丢失母版/版式/主题色能力 |
| 预览可选导致问题未发现 | ✅ 避开（P5 强制预览） | ❌ 预览与最终 .pptx 可能视觉不一致 |
| 表格内容塞进装饰矩形 | ❌ 未解决（layout_plan 无 table schema） | ❌ 新增风险：table shape 无行列/合并定义 |
| 中文行距过紧 | ❌ 未解决（无中文排版考虑） | ❌ 同 v2.3 |
| 图片处理缺失 | ❌ 未解决 | ❌ 同 v2.3，且新增 image shape 无 fit/crop 定义 |
