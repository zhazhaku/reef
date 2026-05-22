# ppt-master → reef ppt-agent 集成分析报告

> 分析日期: 2026-05-16 | 分析师: Hermes Coordinator
> 目标: 评估 `hugohe3/ppt-master` (17k stars, 1.6k forks) 的核心能力，给出与 reef ppt-agent 的结合方案

---

## 1. ppt-master 项目概览

| 属性 | 值 |
|------|-----|
| 作者 | Hugo He (hugohe3) |
| Stars | **17,205** |
| Forks | 1,654 |
| 语言 | Python |
| 许可 | MIT |
| 最新版本 | v2.7.0 (2026-05) |
| 描述 | AI 生成原生可编辑 PPTX，支持任意文档输入 — 原生 PowerPoint shapes，非图片 |

---

## 2. ppt-master 核心能力矩阵

### 2.1 7 步流水线

```
Source Document → Project Init → Template → Strategist → Image Gen → Executor → Export PPTX
     (Step 1)      (Step 2)     (Step 3)     (Step 4)    (Step 5)   (Step 6)    (Step 7)
```

| Step | 名称 | 核心能力 | 是否阻塞 |
|:---:|------|---------|:---:|
| 1 | Source Processing | PDF/DOCX/URL/Excel/Markdown → 统一 Markdown | BLOCKING |
| 2 | Project Init | 创建结构化项目目录，`project_manager.py init` | — |
| 3 | Template | 导入已有 PPTX 作为模板 (`pptx_template_import.py`) | 可选 |
| 4 | Strategist | AI 内容分析+设计规划 → `spec_lock.md` (颜色/字体/布局) | ⛔ BLOCKING |
| 5 | Image Gen | 图片生成或搜索（多后端），可选 | 条件 |
| 6 | Executor | **逐页手写 SVG 生成**（核心环节） | 非阻塞 |
| 7 | Export | `svg_to_pptx.py` → 原生 PPTX（含动画/备注） | 非阻塞 |

### 2.2 关键技术亮点

#### A. SVG → PPTX 转换引擎（最核心差异化能力）

```
skills/ppt-master/scripts/svg_to_pptx/
├── drawingml_converter.py   # SVG 元素 → DrawingML (OOXML shapes)
├── drawingml_elements.py     # rect/circle/path/text 元素映射
├── drawingml_styles.py       # CSS 样式 → PPTX 样式 (渐变/阴影/滤镜)
├── drawingml_paths.py        # SVG path → PPTX custGeom
├── drawingml_context.py      # 容器/上下文管理
├── pptx_builder.py           # 主构建器，组装 PPTX Package
├── pptx_slide_xml.py         # slide XML 生成
├── pptx_dimensions.py        # 16:9 / 4:3 / A4 等画布支持
├── pptx_media.py             # 图片/音频嵌入
├── pptx_notes.py             # 演讲者备注
├── pptx_narration.py         # 旁白/音频时序
├── tspan_flattener.py        # SVG tspan → 单一 text run
├── animation_config.py       # 动画配置桥接
└── use_expander.py           # 宏展开工具
```

**关键事实**: ppt-master 实现了一个完整的 SVG→DrawingML 编译器，将 SVG 的每种元素（rect, circle, path, text, image, linearGradient...）映射为 PPTX 的等价 OOXML 表示。这不是 HTML→PPTX，而是 **矢量→矢量** 的保真转换。

#### B. PPTX → SVG 逆向工程

```
skills/ppt-master/scripts/pptx_to_svg/
├── ooxml_loader.py          # OOXML XML 解析
├── slide_to_svg.py          # PPTX slide → SVG（含主题继承）
├── shape_walker.py          # 遍历 spTree 子元素
├── converter.py              # 主转换器
├── prstgeom_to_svg.py       # 预设几何 → SVG path
├── custgeom_to_svg.py       # 自定义几何 → SVG path
├── txbody_to_svg.py         # 文本框 → SVG text
├── tbl_to_svg.py             # 表格 → SVG
├── pic_to_svg.py             # 图片 → SVG <image>
├── fill_to_svg.py            # 填充样式
├── ln_to_svg.py              # 线条样式
├── effect_to_svg.py          # 特效
├── color_resolver.py        # 颜色解析（含主题色）
└── emu_units.py              # EMU ↔ 像素转换
```

**意义**: 支持将用户上传的 PPTX 模板**完整逆向为 SVG 中间表示**，然后 AI 可以在 SVG 层编辑、再通过 svg_to_pptx 写回。这是"已有模板→AI编辑→输出"闭环的基础设施。

#### C. 原生动画支持

`pptx_animations.py` — 纯 XML 生成，无需依赖 PowerPoint：
- **7 种幻灯片过渡**: fade, push, wipe, split, strips, cover, random
- **22 种元素入场动画**: appear, fade, fly, zoom, wipe, swivel, stretch...
- 动画模式: single / mixed / random

#### D. 多源文档输入

| 输入格式 | 转换工具 |
|----------|---------|
| PDF | `pdf_to_md.py` |
| DOCX/HTML/EPUB/IPYNB | `doc_to_md.py` (原生 Python) |
| Excel (.xlsx/.xlsm) | `excel_to_md.py` |
| URL | web fetch → Markdown |
| .doc/.odt/.rtf/.tex 等 | pandoc fallback |

#### E. 多角色 AI 协作

```
Strategist (设计) → 输出 spec_lock.md（颜色/字体/布局/页面节奏）
Executor (执行)  → 逐页手写 SVG
Image Generator  → 生成或搜索配图（多后端）
Template Designer → 提供模板设计建议
```

---

## 3. 与 reef ppt-agent 的对比

### 3.1 架构对比

| 维度 | ppt-master | reef ppt-agent |
|------|-----------|---------------|
| **核心范式** | 文档→SVG→PPTX（矢量→矢量） | 模板克隆+填充（python-pptx API） |
| **PPTX 写回** | DrawingML 原生生成 | python-pptx 高级 API 封装 |
| **模板处理** | PPTX→SVG 逆向 + AI 编辑 + SVG→PPTX | 直接 XML 深拷贝 slide 再替换内容 |
| **AI 驱动方式** | LLM 手写 SVG 代码（逐元素） | LLM 输出 JSON（内容方案+映射）→ 脚本合成 |
| **动画** | ✅ 7 过渡 + 22 入场 | ❌ 无 |
| **多源输入** | ✅ PDF/DOCX/Excel/URL | ❌ 仅文本描述 |
| **图片处理** | ✅ AI 生成 + 搜索 + 去水印 | 🟡 嵌入图片（需用户提供） |
| **预览** | ✅ Live Preview（SVG 即时渲染） | 🟡 待实现 (soffice → PNG) |
| **质量检查** | ✅ 3 层 Type A/B/C | ❌ 无 |
| **演讲备注/配音** | ✅ TTS 配音 | ❌ 无 |
| **项目结构** | ✅ 完整项目目录 | 🔶 Go 内存状态机 |

### 3.2 互补关系

```
ppt-master 强项 ←→ reef ppt-agent 已有
─────────────────────────────────────────
✅ SVG 引擎        ←  → ✅ 模板克隆保真 (XML deep copy)
✅ 动画            ←  → ✅ 已有模板风格继承
✅ 多源文档输入    ←  → ✅ python-pptx 稳定 API
✅ 质量检查        ←  → ✅ Go 状态机工作流
✅ Live Preview    ←  → ✅ Feishu 消息通道
```

**两者并非竞争关系，而是互补关系**：
- ppt-master 擅长 **从零创作**（文档 → 全新设计 → 精美 PPTX）
- ppt-agent 擅长 **模板驱动**（用户模板 → 内容填充 → 风格一致 PPTX）

---

## 4. 集成方案建议

### 4.1 高价值可集成能力（建议优先）

#### 🥇 方案 A: 借用 SVG→PPTX 引擎作为备选合成路径

**当前 ppt-agent**: `ppt_compositor.py` 用 python-pptx clone+fill，对简单文本填充效果好，但处理图表/图形/富文本受限。

**吸收 ppt-master**: 在内容合成阶段，将 ppt-agent 生成的 JSON 内容方案转写为 SVG 描述，然后调用 `svg_to_pptx` 引擎写入 PPTX。

```
当前路径: 模板 PPTX → clone slide → fill text/table/image → 输出 PPTX
新增路径: 模板 PPTX → JSON 方案 → SVG 表示 → svg_to_pptx → 输出 PPTX
```

**收益**:
- 支持渐变/阴影/圆角等 python-pptx 难以表达的富样式
- 支持图表可视化（ppt-master 有 `verify-charts` 校验流程）
- 统一动画能力

**代价**: 
- 需移植 18 个 SVG→PPTX 脚本（约 5k+ 行 Python）
- 需适配模板克隆 (PPTX→SVG→修改→SVG→PPTX 比直接克隆更重)

**推荐度**: ⭐⭐⭐⭐ (高，但需评估移植成本)

---

#### 🥇 方案 B: 吸收多源文档输入能力

**当前 ppt-agent**: 用户只能通过文本描述 PPT 内容。

**吸收 ppt-master Step 1**: 让 ppt-agent 支持用户上传 PDF/DOCX/URL 作为内容源。

```
新增流程:
  用户上传 PDF 文档 → pdf_to_md.py → Markdown
  用户提供 URL      → web fetch    → Markdown
  → 模板解析 JSON → ContentOrganizer (LLM) → 内容方案
```

**收益**: 大幅降低用户输入门槛（不用手动写内容，直接投喂文档）

**代价**: 移植 `source_to_md/` 脚本（3 个文件 ~500 行）+ 依赖安装

**推荐度**: ⭐⭐⭐⭐⭐ (极高，实施成本低，用户价值大)

---

#### 🥈 方案 C: 吸收质量检查体系

**当前 ppt-agent**: 无质量检查。

**吸收 ppt-master Step 6.5**: 3 层检查 —
- **Type A (结构)**: slide 数量、page_rhythm 分布、图片引用完整性
- **Type B (显式)**: 拼写错误、颜色一致性、字体一致性
- **Type C (视觉)**: SVG viewBox 溢出检测、文本截断检测

```
在 compose() 之前插入:
  validate_step() → 报错列表 → 自动修复(A) / 人工确认(B)
```

**收益**: 减少输出 PPTX 的"翻车"概率

**代价**: 轻量实现（主要是 regex 校验 + viewBox 检查）

**推荐度**: ⭐⭐⭐⭐ (高，实施成本低)

---

#### 🥉 方案 D: 吸收动画能力（差异化杀手功能）

**当前 ppt-agent**: 无动画。

**吸收 ppt-master**: 移植 `pptx_animations.py`，在 `fill_text_block()` 和 `clone_slide()` 阶段附加动画 XML。

```
fill_text_block() 后:
  if slide.animation:
    inject_animation_xml(slide, animation_config)
```

**收益**: 让 ppt-agent 成为少数支持"原生动画"的 AI PPT 工具

**代价**: 中等（`pptx_animations.py` ~200 行，但需深入 PPTX XML 文件结构）

**推荐度**: ⭐⭐⭐ (中等，差异化大但 OOXML 操作复杂)

---

### 4.2 需评估的移植方案

| 能力 | ppt-master 文件 | 行数估计 | 移植难度 | 依赖 |
|------|---------------|:---:|:---:|------|
| SVG→PPTX 引擎 | `svg_to_pptx/` (18 files) | ~5,000 | 🔴 高 | lxml, Pillow |
| PPTX→SVG 逆向 | `pptx_to_svg/` (15 files) | ~4,000 | 🔴 高 | lxml |
| 多源文档输入 | `source_to_md/` (3 files) | ~500 | 🟢 低 | pdfplumber, python-docx, pandoc |
| 动画 | `pptx_animations.py` (1 file) | ~200 | 🟡 中 | 无 |
| 质量检查 | `check_annotations.py` + `batch_validate.py` | ~300 | 🟢 低 | 无 |
| 图片生成 | `image_gen.py` + `image_backends/` | ~800 | 🟡 中 | API keys |
| 图片搜索 | `image_search.py` | ~200 | 🟢 低 | API keys |
| 项目结构 | `project_manager.py` | ~200 | 🟢 低 | 无 |
| TTS 配音 | `notes_to_audio.py` | ~200 | 🟡 中 | edge-tts / OpenAI TTS |

### 4.3 不建议移植的能力

| 能力 | 原因 |
|------|------|
| LLM 手写 SVG (Executor Phase) | 与我们的 JSON 驱动方案冲突；手写 SVG 需要极强的 LLM 推理，token 消耗巨大 |
| Strategist 8 项确认流程 | 我们的大纲→内容→映射流程已经覆盖，不需要两套设计系统 |
| 多角色 Agent 协作 | Reef 已有 Hermes 多 agent 协作架构，不需要 ppt-master 的内置角色 |

---

## 5. 推荐实施路线

### Phase 1: 低果实（1-2 天）

```
✅ 移植 source_to_md/ → ppt-agent 支持 PDF/DOCX/URL 输入
✅ 移植 check_annotations.py 质量检查 → compose() 前校验
```

### Phase 2: 差异化能力（3-5 天）

```
🔶 移植 pptx_animations.py → 动画支持
🔶 移植 image_gen.py + image_search.py → 图片生成/搜索集成
```

### Phase 3: 架构升级（1-2 周，需评估）

```
🔴 评估 SVG→PPTX 引擎移植 vs 继续使用 python-pptx
🔴 实现"已有模板→SVG 中间表示→AI 编辑→SVG→PPTX"双路径
```

### 总路线图

```
                        ppt-agent 当前
                             │
              ┌──────────────┼──────────────┐
              ▼              ▼              ▼
        Phase 1          Phase 2        Phase 3
     多源输入+质检    动画+图片生成    SVG引擎移植
        (1-2天)         (3-5天)        (1-2周)
              │              │              │
              └──────────────┴──────────────┘
                             │
                             ▼
                    ppt-agent v2.0
              模板驱动 + 多源输入 + 动画
                + 图片生成 + SVG富样式
```

---

## 6. 风险评估

| 风险 | 概率 | 影响 | 缓解措施 |
|------|:---:|:---:|---------|
| lxml 依赖冲突 | 中 | 中 | 用 pip 检查现有依赖树，必要时在独立 venv 运行 SVG 脚本 |
| python-pptx 与 DrawingML 直接写入冲突 | 高 | 高 | Phase 3 前不做双写，选一条路径；或创建独立 PPTX 后合并 slide XML |
| SVG→PPTX 移植工程量大 | 高 | 中 | Phase 3 先做 MVP（只移植 rect/circle/text），逐步扩展 |
| 与 Hermes 工作流集成复杂度 | 中 | 中 | ppt-agent 的 Go 状态机添加新 step 枚举值，PPTWorkflow 透明扩展 |
| MIT 许可兼容 | 无 | 无 | ppt-master 是 MIT，完全兼容 |

---

## 7. 总结

**ppt-master 是当前开源 AI PPT 领域的标杆项目**（17k stars），其 SVG→DrawingML 编译器和 PPTX→SVG 逆向工程是独一无二的技术资产。与 reef ppt-agent 的 **模板驱动 + JSON 方案** 范式高度互补。

**建议策略**: Phase 1（多源输入 + 质量检查）立即启动，Phase 2（动画 + 图片）作为 v1.2 功能，Phase 3（SVG 引擎）作为 v2.0 的架构决策点进行更深入的技术评估。

**关键原则**: 吸收 ppt-master 的**工具层能力**（转换器、校验器、动画器），但保留 ppt-agent 的**编排层设计**（JSON 方案 + Go 状态机 + Hermes 多 agent 协作）。
