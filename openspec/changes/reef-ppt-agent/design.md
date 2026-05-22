# Design: Reef PPT Agent — 系统设计

## 1. 总体架构

```
┌─────────────────────────────────────────────────────────────┐
│                    REEF PPT AGENT SYSTEM                     │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌─────────┐ │
│  │ Phase 1  │──▶│ Phase 2  │──▶│ Phase 3  │──▶│ Phase 4 │ │
│  │ Parse    │   │ Outline  │   │ Detail   │   │ Map     │ │
│  │ Template │   │ Generate │   │ Content  │   │ Style   │ │
│  └──────────┘   └──────────┘   └──────────┘   └────┬────┘ │
│                                                     │      │
│  ┌──────────────────────────────────────────────────┘      │
│  │                                                         │
│  │  ┌───────────────────────────────────────────────┐      │
│  │  │         Phase 5: Per-Slide Refinement          │      │
│  │  │                                                │      │
│  │  │  ┌─────────────────────────────────────────┐  │      │
│  │  │  │  调整 ←──→ 预览 ←──→ 再调整  (迭代循环)   │  │      │
│  │  │  │    │        📷          │                │  │      │
│  │  │  │    └────────┴──────────┘                │  │      │
│  │  │  │         detail_plan_v2.json              │  │      │
│  │  │  └─────────────────────────────────────────┘  │      │
│  │  └───────────────────────┬───────────────────────┘      │
│  │                          │                              │
│  │                          ▼ detail_plan_v2.json          │
│  │  ┌───────────────────────────────────────────────┐      │
│  │  │            Phase 6: PPT Compositor             │      │
│  │  │                                                │      │
│  │  │  ┌─────────────┐  ┌─────────┐  ┌──────────┐  │      │
│  │  │  │ Slide Copier │  │ Content │  │ Image    │  │      │
│  │  │  │ (clone style)│  │ Filler  │  │ Handler  │  │      │
│  │  │  └──────┬──────┘  └────┬────┘  └────┬─────┘  │      │
│  │  │         └──────────────┼────────────┘        │      │
│  │  │                        ▼                      │      │
│  │  │              ┌──────────────────┐             │      │
│  │  │              │  PPTX Output      │             │      │
│  │  │              │  (.pptx file)     │             │      │
│  │  │              └──────────────────┘             │      │
│  │  └───────────────────────────────────────────────┘      │
│  └─────────────────────────────────────────────────────────│
└─────────────────────────────────────────────────────────────┘
```

## 2. Phase 1: 模板解析 (Template Parser)

### 2.1 目标
输入 `.pptx` 文件 → 输出结构化的模板描述，供 LLM 理解模板布局。

### 2.2 实现方案

Python 脚本 `template_parser.py`:

```python
from pptx import Presentation
from pptx.util import Inches, Pt, Emu
import json

def parse_template(pptx_path):
    prs = Presentation(pptx_path)
    slides_data = []
    
    for i, slide in enumerate(prs.slides):
        slide_info = {
            "index": i,
            "layout_name": slide.slide_layout.name,
            "shapes": []
        }
        
        for shape in slide.shapes:
            shape_info = {
                "shape_id": shape.shape_id,
                "name": shape.name,
                "shape_type": str(shape.shape_type),  # e.g., TEXT_BOX, PICTURE, PLACEHOLDER
                "left": shape.left,     # EMU
                "top": shape.top,
                "width": shape.width,
                "height": shape.height,
            }
            
            # Text content analysis
            if shape.has_text_frame:
                text = shape.text_frame.text
                shape_info["text"] = text
                shape_info["paragraphs"] = []
                for para in shape.text_frame.paragraphs:
                    for run in para.runs:
                        shape_info["paragraphs"].append({
                            "text": run.text,
                            "font_name": run.font.name,
                            "font_size": str(run.font.size) if run.font.size else None,
                            "bold": run.font.bold,
                            "color": str(run.font.color.rgb) if run.font.color and run.font.color.rgb else None
                        })
            
            # Placeholder analysis
            if shape.is_placeholder:
                ph = shape.placeholder_format
                shape_info["placeholder_idx"] = ph.idx
                shape_info["placeholder_type"] = str(ph.type)  # TITLE, BODY, etc.
            
            slide_info["shapes"].append(shape_info)
        
        # Slide type inference
        slide_info["inferred_type"] = infer_slide_type(slide_info)
        slides_data.append(slide_info)
    
    # Theme/color extraction
    theme_info = extract_theme_info(prs)
    
    return {
        "slide_count": len(prs.slides),
        "slide_width": prs.slide_width,
        "slide_height": prs.slide_height,
        "theme": theme_info,
        "slides": slides_data
    }

def infer_slide_type(slide_info):
    """根据 shape 文本推断 slide 类型"""
    all_text = " ".join(s["text"] for s in slide_info["shapes"] if "text" in s).lower()
    
    if any(kw in all_text for kw in ["封面", "cover", "title slide"]):
        return "COVER"
    if any(kw in all_text for kw in ["目录", "contents", "agenda", "目录"]):
        return "TOC"
    if any(kw in all_text for kw in ["章节", "section", "part"]):
        return "SECTION"
    if any(kw in all_text for kw in ["结束", "谢谢", "thank", "end", "q&a"]):
        return "ENDING"
    return "CONTENT"

def extract_theme_info(prs):
    """提取配色和字体主题"""
    theme = {"colors": set(), "fonts": set(), "font_sizes": set()}
    for slide in prs.slides:
        for shape in slide.shapes:
            if shape.has_text_frame:
                for para in shape.text_frame.paragraphs:
                    for run in para.runs:
                        if run.font.name: theme["fonts"].add(run.font.name)
                        if run.font.size: theme["font_sizes"].add(run.font.size)
                        if run.font.color and run.font.color.rgb:
                            theme["colors"].add(str(run.font.color.rgb))
    
    theme["colors"] = list(theme["colors"])
    theme["fonts"] = list(theme["fonts"])
    theme["font_sizes"] = sorted([str(s) for s in theme["font_sizes"]])
    return theme
```

### 2.3 输出格式

```json
{
  "slide_count": 12,
  "slide_width": 12192000,
  "slide_height": 6858000,
  "theme": {
    "colors": ["1A5276", "2E86C1", "FFFFFF", "333333"],
    "fonts": ["微软雅黑", "Arial"],
    "font_sizes": ["127000", "180000", "240000", "320000"]
  },
  "slides": [
    {
      "index": 0,
      "inferred_type": "COVER",
      "layout_name": "标题幻灯片",
      "shapes": [
        {
          "shape_id": 1,
          "name": "Title 1",
          "shape_type": "PLACEHOLDER (14)",
          "left": 914400, "top": 1828800, "width": 10425600, "height": 1371600,
          "text": "请在此输入标题",
          "placeholder_idx": 0,
          "placeholder_type": "TITLE (1)"
        }
      ]
    }
  ]
}
```

### 2.4 picoclaw 集成

```
Tool: exec
Command: python3 template_parser.py --input /tmp/input.pptx --output /tmp/template.json
Result: parsed JSON → enters seahorse for LLM to analyze
```

---

## 3. Phase 2: 大纲生成 (Outline Generator)

### 3.1 目标
根据内容要求 + 模板结构 → 生成逐页 PPT 大纲（仅文字大纲，不含详细内容）。

### 3.2 System Prompt

```
You are a PPT Outline Generator. Given:
1. A template structure (from template_parser.py JSON output)
2. User's content requirements

Generate a slide-by-slide outline in the following format:

```json
{
  "presentation_title": "...",
  "slides": [
    {
      "slide_number": 1,
      "template_slide_index": 0,
      "slide_type": "COVER",
      "title": "PPT 标题",
      "subtitle": "副标题",
      "bullet_points": ["要点1", "要点2"],
      "notes": "本页说明...",
      "data_visualization": null,
      "image_requests": []
    }
  ]
}
```

Rules:
- Match each content slide to the best template slide type
- COVER slide: only title + subtitle, no bullet points
- TOC slide: list section titles
- CONTENT slide: title + 3-5 bullet points
- If there's tabular data or statistics, mark data_visualization with suggested chart type (bar/line/pie/table)
- Only request images when a visual illustration would significantly improve the slide
```

### 3.3 交互流程

```
Agent → 用户:  大纲如下，请确认：
              [显示大纲列表]
              - Slide 1: 封面 — "XXX项目汇报"
              - Slide 2: 目录 — 4个章节
              - Slide 3: 背景介绍 — 3个要点
              ...

用户 → Agent:  确认 / 修改第X页YYY
```

---

## 4. Phase 3: 详细内容编排 (Detail Content)

### 4.1 目标
大纲确认后 → 生成每页的详细文字内容、图片需求描述、布局建议。

### 4.2 输出格式

```json
{
  "slides": [
    {
      "slide_number": 3,
      "title": "项目背景与挑战",
      "content_blocks": [
        {
          "type": "text",
          "content": "当前系统面临三大核心挑战：\n1. 系统耦合度高，单点故障影响全局\n2. 数据孤岛严重，跨部门协作效率低\n3. 运维成本逐年上升，人工干预占比超过60%",
          "position_hint": "main_body"
        },
        {
          "type": "data_table",
          "title": "近三年运维成本趋势",
          "headers": ["年份", "人工成本(万)", "自动化率", "事故次数"],
          "rows": [
            ["2024", "120", "15%", "23"],
            ["2025", "180", "25%", "31"],
            ["2026", "250", "40%", "45"]
          ],
          "chart_type": "bar",
          "position_hint": "right_side"
        }
      ],
      "image_requests": [
        {
          "description": "系统架构现状图 — 单体架构示意，模块间强依赖",
          "preferred_source": "ai_generate",
          "style": "technical diagram, clean lines, blue theme",
          "position_hint": "left_top",
          "size_hint": "50% width"
        }
      ],
      "layout_notes": "左侧架构图(50%宽) + 右侧数据表格(50%宽)，要点文本框在表格下方"
    }
  ]
}
```

### 4.3 内容块类型

| type | 描述 | 处理方式 |
|------|------|---------|
| `text` | 文字内容 | 填入模板占位区域或新建 TextBox |
| `data_table` | 结构化表格 | python-pptx 创建 Table shape |
| `data_chart` | 图表 | python-pptx 创建 Chart shape |
| `bullet_list` | 要点列表 | 使用模板 body placeholder |
| `image` | 图片 | 用户提供或 AI 生成后插入 |
| `diagram` | 示意图/流程图 | PPT 内置 SmartArt 或 shapes 组合 |

---

## 5. Phase 4: 风格映射 (Style Mapping)

### 5.1 交互

```
Agent → 用户:  请指定每页内容对应的模板 slide 风格：

              当前模板有 12 页:
              - Slide 0: 封面 (COVER)
              - Slide 1: 目录 (TOC)
              - Slide 2-7: 内容页 (CONTENT) × 6
              - Slide 8-9: 图表页 (with chart placeholder)
              - Slide 10: 过渡页 (SECTION)
              - Slide 11: 结束页 (ENDING)

              您的新 PPT 有 10 页:
              - Page 1 (封面):  →  使用模板 Slide 0
              - Page 2 (目录):  →  使用模板 Slide 1
              - Page 3 (背景):  →  使用模板 Slide 2
              - Page 4 (数据):  →  使用模板 Slide 8 (图表页)
              ...

用户 → Agent:  Page 1→0, Page 2→1, Page 3→2, Page 4→8, Page 5→3, ...
```

### 5.2 数据结构

```json
{
  "style_mapping": [
    {"new_slide": 1, "template_slide": 0, "comment": "封面"},
    {"new_slide": 2, "template_slide": 1, "comment": "目录"},
    {"new_slide": 3, "template_slide": 2, "comment": "背景介绍"},
    {"new_slide": 4, "template_slide": 8, "comment": "数据图表页"}
  ]
}
```

---

## 6. Phase 5: 逐页微调 (Per-Slide Refinement)

### 6.1 目标
确认 Phase 3 的详细内容方案后，以对话模式逐页局部调整：文字、字号、字体、颜色、图片、图形、布局。

每次调整后可**生成预览图**供用户检查，不满意则继续调整，**多轮迭代直到满足要求**。

调整全部保存到 `detail_plan_v2.json`，Phase 6 以此为最终合成输入。

### 6.2 带预览的交互模型

```
Agent → 用户:  Phase 3 方案已确认，共 10 页。从哪页开始调整？
              也可以说 "整体调整" 或 "直接生成" 跳过此阶段。

用户 → Agent:  第 3 页标题字号改成 32pt

Agent →        [更新 detail_plan → 生成 Slide 3 预览图]
       → 用户:  Slide 3 标题已改为 32pt。预览如下：
               [📷 preview_slide_3.png]
               继续调整这页还是看下一页？

用户 → Agent:  标题还是太大，改成 28pt，正文第1条改为
              "模块间强依赖导致故障传播"

Agent →        [更新 overrides → 重新生成预览]
       → 用户:  已更新。标题 32pt→28pt，正文第1条已修改。
               [📷 preview_slide_3_v2.png]
               还需要调整 Slide 3 吗？

用户 → Agent:  可以了，第 5 页的架构图换成 3 层简化版

Agent →        [更新 Slide 5 image_requests → 生成预览]
       → 用户:  Slide 5 已更新为 3 层简化架构。
               [📷 preview_slide_5.png]
               继续？

用户 → Agent:  整体正文 14pt，然后预览下第 1 页

Agent →        [批量更新所有 slide 正文 → 预览 Slide 1]
       → 用户:  已全局设置正文 14pt。Slide 1 预览：
               [📷 preview_slide_1.png]

用户 → Agent:  可以了，生成 PPT

Agent →        [保存 detail_plan_v2.json → 进入 Phase 6]
```

### 6.3 预览渲染机制

```
用户调整指令
     │
     ▼
┌──────────────────────────────────────┐
│  1. override_parser.py               │
│     更新目标 slide 的 overrides       │
│     → 输出 detail_plan_v2.json        │
└──────────────┬───────────────────────┘
               │
               ▼
┌──────────────────────────────────────┐
│  2. preview_composer.py              │
│     取 template.pptx 对应 slide      │
│     + detail_plan_v2 中该 slide      │
│     + overrides 样式                 │
│     → 生成临时单页 slide_preview.pptx │
└──────────────┬───────────────────────┘
               │
               ▼
┌──────────────────────────────────────┐
│  3. LibreOffice headless             │
│     soffice --headless               │
│       --convert-to png               │
│       slide_preview.pptx             │
│     → preview_slide_N.png (1280x720) │
└──────────────┬───────────────────────┘
               │
               ▼
         Agent 发送图片给用户
```

**降级方案**: 如果 LibreOffice 不可用，改用文本描述预览：
```
📄 Slide 3 预览摘要：
┌─────────────────────────┐
│ 标题 (28pt): 项目背景    │
│ 正文 (14pt):             │
│  1. 模块间强依赖...      │
│  2. 数据孤岛严重...      │
│ 图片: [3层架构图 45%宽]  │
│ 布局: 左图右文           │
└─────────────────────────┘
```

### 6.4 预览迭代状态机

```
  ┌──────────┐   用户说 "调整第X页…"    ┌────────────┐
  │ WAITING   │─────────────────────────▶│ REFINING    │
  │ (等待指令)│                          │ (处理调整)  │
  └────┬─────┘                          └─────┬──────┘
       │                                       │
       │ 用户说 "预览第X页"                     │ 调整完成
       │◄──────────────────────────────────────┘
       ▼                                       
  ┌──────────┐   用户说 "继续调整…"    ┌────────────┐
  │ PREVIEW   │◄──────────────────────│ REFINING    │
  │ (展示预览)│──────────────────────▶│ (再次调整)  │
  └────┬─────┘   用户说 "可以了"       └────────────┘
       │
       │ 用户说 "可以了/下一页/生成"
       ▼
  ┌──────────┐
  │ CONFIRMED │  Agent 保存 detail_plan_v2.json
  │ (进入     │  → 回到 WAITING (下一轮) 或进入 Phase 6
  │  Phase 6) │
  └──────────┘
```

### 6.5 支持的调整指令类型

| 类别 | 示例指令 | 作用域 | 触发预览 |
|------|---------|--------|---------|
| 文字内容 | "第X页正文第Y条改为XXX" / "删除第X页第Y条" / "增加一条XXX" | 单页 | 可 |
| 文字内容 | "第X页标题改为 XXX" / "副标题去掉" | 单页 | 可 |
| 字号 | "第X页标题改成28pt" / "全局正文14pt" | 单页/全局 | 可 |
| 字体/颜色 | "第X页重点文字加粗红色" / "正文统一用思源黑体" | 单页/全局 | 可 |
| 图片 | "第X页图换成XXX" / "第X页图片缩小到30%宽度" | 单页 | 可 |
| 布局 | "第X页图和文字左右互换" / "去掉第X页的表格" | 单页 | 可 |
| 图形 | "第X页加一个箭头从A指向B" / "流程图第3步去掉" | 单页 | 可 |
| 预览 | "预览第X页" / "看看效果" / "重新生成预览" | 单页 | ✓ |

### 6.6 数据模型: `overrides` 字段

`detail_plan.json` 中每个 `content_block` 支持 `overrides` 字段，存储逐元素样式调整：

```json
{
  "slide_number": 3,
  "title": "项目背景与挑战",
  "content_blocks": [
    {
      "type": "text",
      "content": "模块间强依赖导致故障传播\n2. 数据孤岛严重...",
      "position_hint": "main_body",
      "overrides": {
        "font_size": 14,
        "font_name": "微软雅黑",
        "color": "333333",
        "bold": false
      }
    },
    {
      "type": "image",
      "description": "3层简化架构：接入层→业务层→数据层",
      "position_hint": "left_top",
      "size_hint": "45% width",
      "overrides": {
        "width_ratio": 0.45,
        "top_offset": 0
      }
    }
  ],
  "slide_overrides": {
    "layout_swap": "left_right"
  },
  "preview_version": 2
}
```

### 6.7 批量操作

```go
// 伪代码: 批量应用全局样式
func applyGlobalOverride(plan *DetailPlan, overrideType string, value interface{}) {
    for _, slide := range plan.Slides {
        for _, block := range slide.ContentBlocks {
            if block.Type == "text" || block.Type == "bullet_list" {
                block.Overrides[overrideType] = value
            }
        }
    }
}
```

支持的批量指令：
- "全局正文 Npt" → 所有 text/bullet_list block 字号统一
- "正文统一用 XX 字体" → 所有 text/bullet_list block 字体统一
- "全部标题加粗" → 所有 title 字段 bold=true

---

## 7. Phase 6: PPT 合成 (PPT Compositor)

### 7.1 核心算法

```python
# ppt_compositor.py — 核心逻辑

from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.enum.text import PP_ALIGN
from pptx.dml.color import RGBColor
import copy

def compose_pptx(template_path, detail_plan, style_mapping, image_paths, output_path):
    """主合成函数"""
    template = Presentation(template_path)
    output = Presentation(template_path)  # 从模板复制主题
    
    # 清除 output 中所有 slide（保留主题）
    while len(output.slides) > 0:
        delete_slide(output, 0)
    
    # 逐页合成
    for page in detail_plan["slides"]:
        new_slide_num = page["slide_number"]
        template_slide_idx = style_mapping[new_slide_num]["template_slide"]
        
        # Step 1: 克隆模板 slide 作为基底
        _clone_slide(template, output, template_slide_idx)
        new_slide = output.slides[-1]
        
        # Step 2: 填充内容
        _fill_content(new_slide, page["content_blocks"], image_paths)
        
        # Step 3: 清理模板原始文本（已替换的内容）
        _clean_unused_placeholders(new_slide, page["content_blocks"])
    
    output.save(output_path)

def _clone_slide(template_prs, output_prs, slide_index):
    """克隆 template 的指定 slide（保留所有形状和样式）"""
    from lxml import etree
    from pptx.opc.constants import RELATIONSHIP_TYPE as RT
    
    src_slide = template_prs.slides[slide_index]
    
    # 添加空白 slide
    slide_layout = output_prs.slide_layouts[6]  # blank
    new_slide = output_prs.slides.add_slide(slide_layout)
    
    # 复制所有 shape 的 XML
    for shape in src_slide.shapes:
        el = copy.deepcopy(shape._element)
        new_slide.shapes._spTree.append(el)
    
    # 复制 slide 关系（图片等）
    for rel in src_slide.part.rels.values():
        if rel.is_external:
            new_slide.part.rels.get_or_add_ext_rel(rel.reltype, rel.target_ref)
    
    return new_slide

def _fill_content(slide, content_blocks, image_paths):
    """根据 content_blocks 填充 slide 内容"""
    for block in content_blocks:
        if block["type"] == "text":
            _fill_text_block(slide, block)
        elif block["type"] == "data_table":
            _fill_table_block(slide, block)
        elif block["type"] == "image":
            _fill_image_block(slide, block, image_paths)
        elif block["type"] == "bullet_list":
            _fill_bullet_list(slide, block)

def _fill_text_block(slide, block):
    """在有文本的 shape 中填充内容"""
    # 找到最匹配的 shape（按 position_hint 匹配）
    target_shape = _find_shape_by_hint(slide, block.get("position_hint", "main_body"))
    
    if target_shape and target_shape.has_text_frame:
        tf = target_shape.text_frame
        tf.clear()
        p = tf.paragraphs[0]
        p.text = block["content"]
        # 继承原 shape 的字体样式

def _fill_table_block(slide, block):
    """创建表格"""
    rows = len(block["rows"]) + 1
    cols = len(block["headers"])
    
    left = Inches(1)
    top = Inches(2)
    width = Inches(8)
    height = Inches(rows * 0.5)
    
    table_shape = slide.shapes.add_table(rows, cols, left, top, width, height)
    table = table_shape.table
    
    # 填充表头
    for j, header in enumerate(block["headers"]):
        table.cell(0, j).text = header
    
    # 填充数据行
    for i, row in enumerate(block["rows"]):
        for j, cell in enumerate(row):
            table.cell(i + 1, j).text = str(cell)

def _fill_image_block(slide, block, image_paths):
    """插入图片"""
    image_key = block.get("image_key")
    if image_key and image_key in image_paths:
        img_path = image_paths[image_key]
        # 按 position_hint 定位
        left = Inches(0.5)
        top = Inches(2)
        width = Inches(4)
        slide.shapes.add_picture(img_path, left, top, width)
```

### 7.2 关键限制：禁止整页图片

```python
# ❌ 禁止：整页导出为图片
# slide.shapes.add_picture("page1_render.png", 0, 0, prs.slide_width, prs.slide_height)

# ✅ 允许：逐元素操作
# slide.shapes[0].text = "新标题"
# slide.shapes.add_table(...)
# slide.shapes.add_picture("icon.png", left, top, Inches(1), Inches(1))
# slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, left, top, width, height)
```

### 7.3 无图片时的图形方案

当 `image_requests` 为空时，用 PPT 内置 shape 替代：

```python
from pptx.enum.shapes import MSO_SHAPE

def create_icon_shape(slide, shape_type, left, top, size):
    """创建 PPT 内置图形作为图标"""
    shape = slide.shapes.add_shape(
        shape_type,  # e.g., MSO_SHAPE.CHEVRON, MSO_SHAPE.PENTAGON
        left, top, size, size
    )
    # 应用主题色
    shape.fill.solid()
    shape.fill.fore_color.rgb = RGBColor(0x2E, 0x86, 0xC1)
    shape.line.fill.background()  # 无边框
    return shape
```

---

## 8. 图片处理策略

### 8.1 优先级

```
1. 用户明确提供图片 → 直接使用
2. 用户知识库中有匹配图片 → 提取使用
3. 需要示意/概念图 → AI 生成（web_search + 图像 API）
4. 无图片需求 → PPT shapes 替代
```

### 8.2 AI 图像生成集成

```
Phase 3 用户确认图片需求 → Agent 生成图片 → 用户确认/替换 → Phase 5 合成
```

picoclaw 现有 `web_fetch` 可以下载图片，如果集成了图像生成 API（如 DALL-E/Midjourney），可直接在 tool 链中生成。

---

## 9. 数据流总览

```
User provides: template.pptx + "做一个关于XX的汇报PPT"
        │
        ▼
┌────────────────────────────────────────────────────┐
│ Phase 1:  exec("python3 template_parser.py ...")   │
│           → template.json (slide 结构)              │
│           写入 workspace 供后续使用                  │
└────────────────────┬───────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────┐
│ Phase 2:  LLM → outline.json                       │
│           展示给用户确认                             │
└────────────────────┬───────────────────────────────┘
                     │ 用户确认
                     ▼
┌────────────────────────────────────────────────────┐
│ Phase 3:  LLM → detail_plan.json                   │
│           每页详细内容 + 图片需求 + 图表建议          │
│           展示给用户确认                             │
└────────────────────┬───────────────────────────────┘
                     │ 用户确认
                     ▼
┌────────────────────────────────────────────────────┐
│ Phase 4:  用户输入 style_mapping.json               │
│           "Page1→template slide 0, Page2→slide 1"  │
└────────────────────┬───────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────┐
│ Phase 5:  对聊逐页微调 + 预览迭代                     │
│   "第3页标题32pt" → 更新 overrides                      │
│   "预览一下" → preview_composer → soffice → PNG        │
│   "标题还是大，改28pt" → 再调整 → 再预览               │
│   "可以了" → detail_plan_v2.json                       │
└────────────────────┬───────────────────────────────┘
                     │
                     ▼
┌────────────────────────────────────────────────────┐
│ Phase 6:  exec("python3 ppt_compositor.py          │
│           --template template.pptx                 │
│           --plan detail_plan_v2.json                │
│           --mapping style_mapping.json             │
│           --images images/                         │
│           --output result.pptx")                   │
│                                                    │
│           → result.pptx 下载给用户                  │
└────────────────────────────────────────────────────┘
```

## 10. 文件清单

| 文件 | 作用 |
|------|------|
| `pkg/skills/ppt-agent/template_parser.py` | Phase 1: 模板解析 |
| `pkg/skills/ppt-agent/override_parser.py` | Phase 5: 自然语言调整指令解析 |
| `pkg/skills/ppt-agent/preview_composer.py` | Phase 5: 单页预览合成 + soffice 渲染 |
| `pkg/skills/ppt-agent/preview_cache.py` | Phase 5: 预览 PNG 缓存管理 |
| `pkg/skills/ppt-agent/ppt_compositor.py` | Phase 6: PPT 合成 |
| `pkg/skills/ppt-agent/SKILL.md` | Skill 注册和 system prompt |
| `pkg/agent/ppt_workflow.go` | 工作流状态机(可选 standalone) |
| `pkg/tools/reef_tools.go` | 注册 ppt_parse / ppt_compose tools |

---

## 11. Skill Integration 架构

### 11.1 技能加载链路

```
SkillsLoader (loader.go:150)
  └→ LoadSkill("ppt-agent")
      ├→ workspace:  skills/ppt-agent/SKILL.md ✅ 优先
      ├→ global:     ~/.reef/skills/ppt-agent/  (备用)
      └→ builtin:    $REEF_BUILTIN_SKILLS/       (最低)

System Prompt 注入 (context.go → BuildSystemPromptParts)
  └→ skillsLoader.BuildSkillsSummary()
      └→ <skill>
           <name>ppt-agent</name>
           <description>Multi-phase AI PPT generation...</description>
           <location>skills/ppt-agent/SKILL.md</location>
           <source>workspace</source>
         </skill>
```

### 11.2 Skill + Code 双层架构

```
┌─────────────────────────────────────────────────────────┐
│              Reef PPT Agent Architecture                  │
├─────────────────────────────────────────────────────────┤
│  Skills Layer (workspace/skills/ppt-agent/)              │
│  ┌──────────────────────────────────────────────────┐   │
│  │ SKILL.md           → Agent instruction & workflow │   │
│  │ prompts/*.md       → LLM prompt templates         │   │
│  │ template_parser.py → Phase 1 CLI tool             │   │
│  │ ppt_compositor.py  → Phase 5 CLI tool             │   │
│  │ schema.py          → JSON validation              │   │
│  └──────────────────────────────────────────────────┘   │
│                          │                               │
│                          │ exec("python3 skills/...")    │
│                          ▼                               │
│  Code Layer (picoclaw/pkg/)                              │
│  ┌──────────────────────────────────────────────────┐   │
│  │ agent/ppt_workflow.go  → State machine           │   │
│  │   - PPTPhase enum (6 states)                     │   │
│  │   - PPTSession (artifacts tracking)              │   │
│  │   - CanTransition / Transition (validation)      │   │
│  │   - SetArtifact / ArtifactPath (file paths)      │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

### 11.3 Skill 路径约定

所有 Python 工具通过 workspace 相对路径调用，因为 `ExecTool` 的默认 `cwd`
= agent workspace = `/root/reef_server/.reef/workspace`：

```bash
# Phase 1: Parse template
python3 skills/ppt-agent/template_parser.py input.pptx -o /tmp/structure.json

# Phase 5: Compose PPT
python3 skills/ppt-agent/ppt_compositor.py \
  --template input.pptx \
  --plan /tmp/detail_plan.json \
  --mapping /tmp/style_mapping.json \
  --images /tmp/images/ \
  --output /tmp/result.pptx
```

### 11.4 与其他 Skills 的关系

| Skill | 关系 | 说明 |
|-------|------|------|
| `agent-browser` | 独立 | PPT 生成不依赖浏览器 |
| `github` | 可选 | 模板可存储于 GitHub 仓库 |
| `summarize` | 互补 | 可用作内容摘要输入到 Phase 2 |
| `weather` | 无关 | — |
| `tmux` | 无关 | — |

### 11.5 可安装性

ppt-agent 支持通过 `install_skill` 工具从 GitHub registry 安装：

```bash
# 通过 Reef agent install_skill 工具
install_skill(slug="ppt-agent", registry="github")
# → 下载到 workspace/skills/ppt-agent/
# → SkillsLoader 自动发现
```

当前 ppt-agent 已预置在 workspace skills 中（手动部署），
未来可发布到 GitHub skills registry 实现一键安装。
