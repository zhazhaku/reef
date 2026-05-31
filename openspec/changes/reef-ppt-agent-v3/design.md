---
change: reef-ppt-agent-v3
schema: spec-driven
doc: design
---

# Design: PPT Agent v3 详细设计

## 1. 七阶段流程详解

### P0 — 输入采集
**输入**: 用户消息 (一段话 / Word / PDF / Excel / URL / .md) + 业务要求
**Agent**:
1. 检测来源类型 (MIME + 扩展名)
2. 调用对应 parser:
   - docx -> mammoth
   - pdf -> pdfplumber
   - xlsx -> openpyxl
   - url -> readability + trafilatura
   - md/txt -> 直接读取
3. 统一为 source.md 中间格式 (含标题层级 / 表格 / 图引用)
**确认点**: 无 (自动完成, 失败时报错给用户)
**产物**: work/source.md

### P1 — 大纲生成
**输入**: source.md + 用户要求
**Agent**: LLM (outline_prompt.md) 自动决定页数与章节
**产物**: work/outline.json (slides 数组: page, title, gist, content_keys)
**消息**: Markdown 大纲表格 + 取舍说明
**确认点 #1**: [通过] / [修改: 文本指令] / [重做]

### P2 — 内容细化
**输入**: outline.json + source.md
**Agent**: LLM (detail_prompt.md) 为每页生成 content_blocks
**产物**: work/detail_plan.json (slides[].content_blocks[]: type, text/items/data, importance)
**消息**: 逐页 markdown 渲染预览
**确认点 #2**: [通过] / [逐页改: page=N, 指令] / [整体重做]

### P3 — 风格选择 (新增独立阶段)
**消息**: Agent 展示两条路径:
- A 内置风格: 5 选 1 (商务蓝 / 科技红 / 极简黑 / 学术绿 / 活力橙), 各附 4 页缩略图
- B 自定义风格: 用户上传参考 .pptx (仅作风格借鉴, 不严格限制内容/页数)
**确认点 #3**: 风格路径选择
**产物**: work/style_decision.json

### P4 — 布局规划 (分双路径)

#### P4-A 内置风格路径
**输入**: detail_plan + 内置风格 DSL
**Agent**: LayoutComposer 根据 DSL 规则 + 内容量自动选择版式
**产物**: work/layout_plan.json
**消息**: 无 (直接进 P5 预览)

#### P4-B 自定义参考路径
**Step 1 — 参考解析**:
- 解析参考 .pptx -> work/reference_lib.json (每页提取 StyleProfile)
**Step 2 — AI 风格匹配**:
- LLM 为每个 detail_plan 页推荐 1 个参考页 + 理由
- 消息: 映射表 (plan_page -> ref_page, reason)
- 确认点 #3.5: 映射确认 [通过] / [调整: page=N, ref=M]
**Step 3 — 借鉴生成**:
- 提取 StyleProfile 配色/字体/装饰特征
- LayoutComposer 重新生成 layout (不复制 shape, 仅借特征)
- 产物: work/layout_plan.json


### P5 — 预览确认 (强制)

**输入**: layout_plan.json
**Agent**: 
1. 渲染引擎生成每页预览图 (PNG 优先, SVG 降级, HTML 最终降级)
2. 输出: work/previews/slide_{N}.png + work/previews/summary.txt
**确认点 #4**: 预览确认
- 展示: 每页缩略图 + 文字摘要
- 用户选项: [全部通过] / [修改第N页: ...]
- 若修改: 进入细化子循环 (refinement_prompt.md) → 更新 layout_plan → 重新预览
- 最多 3 轮细化, 超过则提醒用户"建议接受当前版本"
**产物**: 用户确认的 layout_plan.json

### P6 — PPTX 生成

**输入**: 确认后的 layout_plan.json
**Agent**: PPTXBuilder
- 从空白演示文稿构建 (非 clone template)
- 每个 shape 独立创建, 独立可编辑
- 样式从 style_ref 指针获取 (内置 DSL 或 reference_lib)
- 绝不将整页渲染为图片
**后处理**:
- 溢出检测: 3 策略自动修复 (shrink_title / split_to_two_slides / two_column)
- 质量门: FONT-INHERITANCE / COLOR-CONSISTENCY / CONTENT-COMPLETENESS / TEXT-OVERFLOW
**产物**: output/{name}.pptx
**确认点 #5**: 最终交付 (可选下载)

---

## 数据契约

| 产物 | 格式 | 生成阶段 | 说明 |
|------|------|----------|------|
| source.md | Markdown | P0 | 多源预处理统一文本 |
| outline.json | JSON | P1 | 9-15页大纲, 每页 title+summary+slide_type |
| detail_plan.json | JSON | P2 | 每页 content_blocks[], type∈{title,subtitle,body,bullet_list,image,table} |
| style_decision.json | JSON | P3 | {mode:"built_in"|"custom", style_name?, reference_path?} |
| reference_lib.json | JSON | P4-B | 每页 StyleProfile: colors/fonts/decor/layout_skeleton |
| layout_plan.json | JSON | P4 | 每页 shapes[]: {type,x,y,w,h,content_key,style_ref,action} |
| previews/ | PNG/SVG/HTML | P5 | 每页预览图 |
| output.pptx | PPTX | P6 | 最终交付 |

### layout_plan.json v3 schema (核心变更)

```json
{
  "meta": {
    "plan_slide_count": 9,
    "overflow_decisions": []
  },
  "slides": [{
    "plan_slide": 1,
    "slide_type": "COVER",
    "layout_strategy": "center_title",
    "shapes": [{
      "shape_id": "s1_title",
      "type": "text_box",
      "x": 914400, "y": 2057400,
      "w": 10363200, "h": 2743200,
      "content_key": "title",
      "style_ref": {"font": "微软雅黑", "size_pt": 40, "bold": true, "color": "#C00000"},
      "action": "fill_text"
    }]
  }]
}
```

**v2.3→v3 关键区别**:
- v2.3: `target_shape_idx` 引用模板 shape → 强绑定模板结构
- v3: `shapes[]` 从零定义新布局 + `style_ref` 借鉴风格 → 解耦内容与样式

---

## 引擎架构

```
┌─────────────┐    ┌──────────────┐    ┌───────────────┐
│ StyleExtractor│───▶│LayoutComposer │───▶│  PPTXBuilder  │
│  (NEW)       │    │ (替代 auto_   │    │ (替代 compose │
│              │    │  match_shape) │    │  _with_layout │
│ 输入: .pptx  │    │              │    │  _plan)       │
│ 输出: Style  │    │ 输入: detail  │    │               │
│ Profile      │    │ + style_ref  │    │ 输入: layout  │
│              │    │ 输出: layout  │    │ _plan         │
│ 内置: 5 DSL  │    │ _plan.json   │    │ 输出: .pptx   │
└─────────────┘    └──────────────┘    └───────────────┘
```

### StyleExtractor
- 解析 .pptx → 提取配色/字体/装饰/布局骨架 → StyleProfile
- 内置 5 种 DSL (商务蓝/科技红/极简黑/学术绿/活力橙)
- 输出格式: `{colors: {primary, secondary, accent, bg, text}, fonts: {title, body}, decor: {accent_bar, card_radius, icon_style}}`

### LayoutComposer
- 根据 detail_plan + style_ref 生成 layout_plan.json
- 内置版式库: center_title / left_title / data_cards / three_column / two_column / flow_chart / comparison / timeline
- 版式选择逻辑: slide_type → 候选版式 → content_block 数量/类型 → 最终选择
- 自定义参考: 借鉴 reference_lib 中 StyleProfile 的布局骨架, 不复制 shape

### PPTXBuilder
- 从空白 python-pptx Presentation() 构建
- 按 layout_plan.shapes[] 逐个创建 shape
- 样式从 style_ref 字段注入
- 不使用 clone_slide / 不依赖模板 shape 索引
- 保证每个元素独立可编辑

---

## v2.3 vs v3 对比

| 维度 | v2.3 (当前) | v3 (本设计) |
|------|-------------|-------------|
| 模板依赖 | 强绑定 (target_shape_idx) | 解耦 (style_ref 借鉴) |
| 内容映射 | _auto_match_shape 启发式 | LayoutComposer 语义匹配 |
| 预览 | 可选 | 强制 (P5) |
| 确认点 | 3 个 | 5 个 |
| 自定义模板 | clone + fill slot | 解析 + 借鉴风格 + 重建 |
| 内置风格 | 无 | 5 种 DSL |
| 引擎 | compose_with_layout_plan | PPTXBuilder (从零构建) |
| 失败模式 | 内容错位 (shape 匹配错) | 风格偏差 (可调, 不致命) |

---

## 附录 A — P0 阻塞项修复 (v3.0 决策落地)

> 决策日期: 2026-05-22 | 决策来源: GAP-REPORT.md Q1-Q5 全部采纳建议
> 本附录为 P0 阻塞项的最终规约，覆盖正文相应章节，优先级高于先前描述。

### A.1 预览方案修正 (G0-1, Q1=A)

**决策**: PNG 路径降级为可选，**SVG 为主路径**。

- P5 主流程: `PPTXBuilder → SVG export (per slide via lxml+CairoSVG)` → 用户预览
- LO/PNG 仅作为用户显式请求时的补充路径 (`--preview=png`)，失败不阻塞主流程
- HTML 降级保留，触发条件: SVG 渲染异常 ≥1 slide
- 字体: SVG 内嵌字体子集 (subset 微软雅黑/思源黑体 CJK 范围)，避免 LO/PNG 的 CJK 缺失问题

### A.2 style_ref 解析顺序 (G0-2/G0-7, D9)

四级优先级 (从高到低):

1. `block.style_overrides` (inline)
2. `slide.style_ref → reference_lib[<key>]` (per-slide block-level)
3. `slide_master.theme` (template clone 来源)
4. `built_in_dsl[<style_id>]` (5 种内置 fallback)

**白名单校验**: 所有 LLM 输出的 `style_ref` 必须存在于 `reference_lib.json` 的键集合中；不存在直接拒绝并要求 LLM 修正。Pydantic schema 用 `Literal[...]` 锁定可选值 (instructor 库)。

### A.3 命名空间规则 (G0-3 + G0-5, D10)

- **shape_id**: `s{plan_slide}_{role}_{seq}` (例 `s3_title_0`, `s3_card_2`)。`role ∈ {title, subtitle, body, bullet, card, table, image, decor}`。
- **content_key**: `{plan_slide}.{block_type}.{seq}` (例 `3.bullet_list.0`, `5.card.2`)。
- 溢出新增页用新 `plan_slide` (例 `3a`, `3b`)，**禁止复用 id**。
- 每个 `layout_plan.json` 写入前由 `validate_namespace()` 检查全局唯一性。

### A.4 状态持久化与恢复 (G0-6, D15)

每个确认点产物原子写入 `work/<session_id>/session.json`:

```json
{
  "session_id": "20260522-091500-abc",
  "current_phase": "P3",
  "completed": ["P0", "P1", "P2"],
  "artifacts": {
    "source_md": "work/.../source.md",
    "outline_json": "work/.../outline.json",
    "detail_plan_json": "work/.../detail_plan.json"
  },
  "user_decisions": {"q_style_choice": "builtin:business_blue"},
  "updated_at": "2026-05-22T09:15:00Z"
}
```

- 写入策略: tmpfile + `os.replace` 原子化
- CLI: `ppt-agent --resume <session_id>` 从最近 phase 恢复
- 待回复确认: 1h 内提示用户、24h 归档到 `archive/`

### A.5 表格策略 (G0-9, Q2=A)

PPTXBuilder **新建表格** (`slide.shapes.add_table(rows, cols, x, y, cx, cy)`)，**不修改模板表格**。

- `layout_plan.json` 中 table block schema:
  ```json
  {
    "shape_id": "s4_table_0",
    "type": "table",
    "rows": 5, "cols": 4,
    "position": {"x": 914400, "y": 1828800, "cx": 9144000, "cy": 3657600},
    "content_key": "4.table.0",
    "style_ref": "ref_table_default",
    "merged_cells": [[0,0,0,3]]
  }
  ```
- 复杂合并单元格通过 `merged_cells: [[r1,c1,r2,c2], ...]` 描述
- v3.0 不支持: 表内嵌图片/表内嵌表

### A.6 多源解析器延后 (G0-10, Q4=B)

v3.0 仅支持 `.md` / `.txt` / `.docx` (mammoth) 三种输入。

- **延后到 v3.1**: `.pdf` (pdfplumber+OCR), `.xlsx` (openpyxl), `url` (trafilatura)
- P0 阶段对不支持格式返回明确错误 + 文档链接，不静默失败
- 待办任务从 T5.1 拆分为 T0 任务组 (v3.1 启动)

### A.7 内置样式数量 (G0-12 关联, Q5=A)

v3.0 提供 **5 种**内置样式 (D8 已定):

| ID | 名称 | 主色 | 辅色 | 字体 |
|----|------|------|------|------|
| business_blue | 商务蓝 | #0070C0 | #2E5E8A | 微软雅黑 |
| tech_red | 科技红 | #C00000 | #8B0000 | 微软雅黑 |
| minimal_black | 极简黑 | #000000 | #404040 | Helvetica Neue |
| academic_green | 学术绿 | #1F7A4D | #145239 | 思源宋体 |
| vibrant_orange | 活力橙 | #ED7D31 | #B85A1E | 微软雅黑 |

**Theme fallback** (G0-12): 模板 `clrScheme` 为空时，自动注入 `business_blue` 的色板作为 master theme。

### A.8 几何验证层 (G0-4)

新增 `LayoutComposer.validate_geometry(layout_plan, slide_width, slide_height)`:

1. **边界检查**: `0 ≤ x, x+cx ≤ slide_width`；同理 y/cy。越界 → auto-clamp 并 warn 到日志
2. **重叠检测**: shape 两两 IOU > 0.3 触发 warn (允许少量装饰重叠)
3. **CJK 宽度估算** (G0-13): `est_width(text) = sum(2*em if is_cjk(c) else 1*em) * font_size_pt * 12700`，加 10% padding
4. **降级**: 验证失败 ≥3 个 shape → fallback 到 `auto_grid_layout()` (2x2/3x1/3x2 网格)

### A.9 .pptx 损坏检测 (G0-11)

`PPTXBuilder.write()` 完成后执行:

```python
def verify_output(path):
    from pptx import Presentation
    p = Presentation(path)  # 重新打开
    assert len(p.slides) > 0
    # XML well-formed check
    import zipfile
    with zipfile.ZipFile(path) as z:
        for name in z.namelist():
            if name.endswith('.xml'):
                from lxml import etree
                etree.fromstring(z.read(name))
```

失败 → 回退到上一 checkpoint + 重试一次 + 仍失败上报用户。

### A.10 修正循环收敛保证 (G1-17, Spec 11)

- P5 refinement loop 最多 **3 轮**
- 第 4 轮: 系统消息提示「已达修正上限，请二选一: (a) 强制确认进入 P6 (b) 降级到上一稳定版本」
- 每轮 diff 必须 ≥1 字段变化，否则视为「无意义重试」直接进入强制选择

### A.11 输出工件目录约定

```
work/<session_id>/
  session.json              # A.4 状态
  source.md                 # P0 产物
  outline.json              # P1 产物
  detail_plan.json          # P2 产物
  style_decision.json       # P3 产物
  reference_lib.json        # P4-B 产物 (仅自定义路径)
  layout_plan.json          # P4 产物
  previews/
    slide_1.svg
    slide_2.svg
    ...
  output.pptx               # P6 产物
  logs/
    pipeline.jsonl          # 结构化日志 (G1-29 P1 延后)
```

---

*附录 A 结束。所有 P0 阻塞项已锁定。*
