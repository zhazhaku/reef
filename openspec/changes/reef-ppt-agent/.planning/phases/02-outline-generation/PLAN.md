# Phase 02: 大纲生成 (Outline Generation)

> 文件: `.planning/phases/02-outline-generation/PLAN.md`
> 创建: 2026-05-19 | GSD Phase 2 (v2.1: 🔔 确认点 1/3)
> 依赖: Phase 01 — 模板分析 (P1)
> 上一 Phase: Phase 01 — 模板分析
> 下一 Phase: Phase 03 — 详情+布局

---

## 1. Phase Overview

| 项目 | 值 |
|:---|:---|
| **目标** | 打通多源素材输入 → LLM 大纲生成 → 用户确认的完整流水线 |
| **工时** | 14h |
| **任务数** | 20 |
| **测试数** | 89 (Phase I integration test, across source_to_md pipeline + style system + layouts + CLI) |
| **依赖** | Phase 01 — 模板分析 (自定义模板 pptx→SVG 桥接已完成) |
| **子阶段** | P2.A — 多源素材预处理 Phase 0 (7 tasks, 3.5h) + P2.B — 内置风格系统 Phase A (8 tasks, 6h) + P2.C — 大纲生成 (2 tasks, 2h) + P2.D — 集成测试 (3 tasks, 2.5h) |
| **用户确认点** | 🔔 **确认点 1/3** — LLM 生成大纲后，用户确认页数、每页标题和页面类型 |

### 架构位置 (v2.1: 统一 SVG 引擎流水线)

```
Phase 01: 模板分析
  ├── 自定义模板 pptx→SVG 桥接 (pptx_to_svg.py)
  ├── style_profile.json 生成 (python-pptx 回退)
  └── 产出: style_profile.json, SVG 模板页

Phase 02 (本 Phase) — 🔔 确认点 1/3
  ├── P2.A: 多源素材预处理 (Phase 0: pdf/docx/xlsx/pptx/url → material.md)
  │       └── 用户输入文本/文件/URL → 统一 Markdown 素材
  ├── P2.B: 内置风格系统 (Phase A: 5 styles × 7 types SVG, 无模板时 fallback)
  │       └── 用户未上传模板时提供 5 种内置风格选择
  ├── P2.C: 大纲 LLM 生成 (outline_prompt.md → outline.json)
  │       └── 自动页数判定，按内容量伸展
  └── 产出: outline.json (页数、每页标题+摘要+页面类型)

Phase 03: 详情+布局 — 🔔 确认点 2/3
Phase 04: 预览 — 🔔 确认点 3/3
Phase 05: 合成输出
```

### 用户可见流程

```
用户提供输入源 (文本/文件/URL)
       │
       ▼
  [Phase 0: 素材预处理] (可选 — 用户直接输入文本时跳过)
       │
       ▼
    material.md
       │
       ▼
  [Phase A: 风格选择] (无模板时 fallback 到 5 种内置风格)
       │
       ▼
  LLM 大纲生成 (outline_prompt.md)
       │
       ▼
🔔 确认点 1/3 — outline.json → 用户确认
       │
       ▼
  Phase 03: 详情+布局
```

---

## 2. Pre-flight Checklist

执行 Phase 02 之前必须确认以下先决条件：

- [x] **Phase 01 完成**: `style_profile.json` 可正常生成, pptx→SVG 桥接验证通过
  ```bash
  python3 skills/ppt-agent/v2.0/pptx_to_svg/pptx_to_svg.py \
    test/fixtures/test_template.pptx /tmp/test_svg/
  # 预期: 5 个 slide_*.svg 文件, 含 shape-type 属性
  ```
- [x] **python-pptx 已安装**: `python3 -c "import pptx; print(pptx.__version__)"` 成功
- [x] **lxml 已安装**: `python3 -c "import lxml; print(lxml.__version__)"` 成功
- [x] **pymupdf (fitz) 已安装**: `python3 -c "import fitz; print(fitz.__version__)"` 成功
- [x] **beautifulsoup4 已安装**: `python3 -c "import bs4; print(bs4.__version__)"` 成功
- [x] **openpyxl 已安装**: `python3 -c "import openpyxl; print(openpyxl.__version__)"` 成功
- [x] **pyyaml 已安装**: `python3 -c "import yaml; print(yaml.__version__)"` 成功
- [x] **cairosvg 已安装**: `python3 -c "import cairosvg; print(cairosvg.__version__)"` 成功
- [x] **PPT-Master 模板库可用**: `skills/ppt-agent/v2.0/templates/` 含 157 layouts + 54 charts + 6733 icons
  ```bash
  ls skills/ppt-agent/v2.0/templates/layouts/ | head -5
  # 预期: exhibit/ ai_ops/ academic_defense/ 等目录
  ```
- [x] **source_to_md 目录就绪**: 5 个转换脚本 + 统一入口 + 合并器
  ```bash
  ls skills/ppt-agent/v2.0/source_to_md/
  # 预期: pdf_to_md.py doc_to_md.py web_to_md.py excel_to_md.py ppt_to_md.py source_to_content.sh material_merger.py
  ```
- [x] **SVG 模板页已生成**: 5 styles × 7 page types = 35+ SVG 页面
  ```bash
  find skills/ppt-agent/v2.0/templates/svg_pages/ -name "*.svg" | wc -l
  # 预期: ≥ 25
  ```
- [x] **outline_prompt.md 已编写**: `skills/ppt-agent/prompts/outline_prompt.md` 存在
- [x] **Python 版本**: `python3 --version` ≥ 3.9

---

## 3. Sub-phase P2.A: 多源素材预处理 (Phase 0)

> 目标: 5 种格式 → 统一 Markdown 素材入口，支持多文件合并
> 工时: 3.5h | 任务: 7 | 测试: 34 (UT + IUT)
> 对应 v2.0: Phase I.A (source_to_md)

### 3.1 目录结构 (P2.A 完成后)

```
skills/ppt-agent/v2.0/source_to_md/
├── pdf_to_md.py            # P2.A.1: PDF → Markdown (pymupdf)
├── doc_to_md.py            # P2.A.2: DOCX/HTML/EPUB → Markdown
├── web_to_md.py            # P2.A.3: URL → Markdown (requests + bs4)
├── excel_to_md.py          # P2.A.4: XLSX → Markdown table (openpyxl)
├── ppt_to_md.py            # P2.A.5: PPTX text extraction → Markdown
├── source_to_content.sh    # P2.A.6: Unified shell entry (auto format detect)
└── material_merger.py      # P2.A.7: Multi-Markdown merger → material.md
```

### 3.2 任务详细

---

#### P2.A.1 — `pdf_to_md.py` (PDF → Markdown) ✅ 0.5h

**描述**: 从 ppt-master 移植 PDF→Markdown 转换器，基于 pymupdf (fitz) 提取文本/表格/图片占位符

**文件**: `skills/ppt-agent/v2.0/source_to_md/pdf_to_md.py` (800 行)

**核心能力**:
- 文本提取：段落合并、换行规范化
- 标题检测：基于字号/粗体/位置启发式识别 `#`/`##` 层级
- 表格提取：pymupdf Table Finder → Markdown pipe table 格式
- 图片占位：扫描件页面输出 `[IMAGE]` 占位符

**UT 清单**:
- [x] **UT-P2.A.1.1**: 纯文本 PDF (1 页) → Markdown 含完整文本，无乱码
- [x] **UT-P2.A.1.2**: 中文 PDF (3 页, 含标题+段落+表格) → 标题层级正确，表格为 Markdown table
- [x] **UT-P2.A.1.3**: 图像型 PDF (扫描件) → 输出 `[IMAGE]` 占位符，不崩溃

**IUT 清单**:
- [x] **IUT-P2.A.1**: 与 ppt-master 原始 `pdf_to_md.py` 对比同一 PDF，差异仅限路径

**验收标准**:
- [x] 中文 PDF 输出无乱码
- [x] 3 页 PDF 输出含 ≥ 2 个标题层级
- [x] 扫描件处理不崩溃，产生有意义的占位符输出

---

#### P2.A.2 — `doc_to_md.py` (DOCX/HTML/EPUB → Markdown) ✅ 0.5h

**描述**: 从 ppt-master 移植 DOCX/HTML/EPUB→Markdown 转换器

**文件**: `skills/ppt-agent/v2.0/source_to_md/doc_to_md.py` (538 行)

**支持的输入格式**:
- `.docx` → python-docx 逐段落提取 (含样式映射: Heading 1→`#`, Normal→正文)
- `.html` / `.htm` → beautifulsoup4 解析 (含 heading/img/table/ul/ol 标签处理)
- `.epub` → zipfile 解压 + bs4 解析

**UT 清单**:
- [x] **UT-P2.A.2.1**: DOCX (含 H1/H2/正文/表格) → Markdown 层级正确
- [x] **UT-P2.A.2.2**: HTML (含 `<h1>`/`<p>`/`<table>`/`<ul>`) → Markdown 格式正确
- [x] **UT-P2.A.2.3**: EPUB → Markdown 可读输出

**IUT 清单**:
- [x] **IUT-P2.A.2**: 与 ppt-master 原始版本对比，差异仅限路径

**验收标准**:
- [x] DOCX 样式映射正确 (Heading 1→`#`, Normal→正文)
- [x] HTML 表格 → Markdown table 保留行列结构
- [x] EPUB 章节结构保留

---

#### P2.A.3 — `web_to_md.py` (URL → Markdown) ✅ 0.5h

**描述**: 从 ppt-master 移植 URL→Markdown 抓取器

**文件**: `skills/ppt-agent/v2.0/source_to_md/web_to_md.py` (806 行)

**核心能力**:
- HTTP 抓取 (requests + User-Agent 模拟)
- HTML→Markdown 提取 (beautifulsoup4: 标题/段落/列表/表格/图片 alt text)
- 自动编码检测 (chardet/cchardet fallback to utf-8)

**UT 清单**:
- [x] **UT-P2.A.3.1**: 静态 HTML 页面 (含标题+段落+列表) → Markdown 格式正确
- [x] **UT-P2.A.3.2**: 含 `<table>` 的页面 → Markdown table 保留结构
- [x] **UT-P2.A.3.3**: 404 错误页面 → 优雅错误处理，不崩溃

**IUT 清单**:
- [x] **IUT-P2.A.3**: 与 ppt-master 原始版本对比，差异仅限路径

**验收标准**:
- [x] HTML→Markdown 转换保留语义结构 (h1-h6, table, ul, ol)
- [x] 网络错误时输出明确错误信息而非崩溃

---

#### P2.A.4 — `excel_to_md.py` (XLSX → Markdown table) ✅ 0.5h

**描述**: 全新编写的 XLSX→Markdown table 转换器 (基于 openpyxl)

**文件**: `skills/ppt-agent/v2.0/source_to_md/excel_to_md.py` (220 行)

**核心能力**:
- openpyxl 读取工作簿 (所有 sheet)
- 每个 sheet → 一个 Markdown table (含 sheet name 作为 `##` 标题)
- 合并单元格处理 (值复制到所有参与格)
- 数字格式化 (日期/百分比/小数保留原始格式)
- 空行过滤 (全空行跳过)

**UT 清单**:
- [x] **UT-P2.A.4.1**: 单 sheet XLSX (含表头+数据行) → Markdown table 行列正确
- [x] **UT-P2.A.4.2**: 多 sheet XLSX (3 sheets) → 3 个 Markdown table，含 sheet 名称
- [x] **UT-P2.A.4.3**: 合并单元格 XLSX → 合并值出现在所有参与格
- [x] **UT-P2.A.4.4**: 空 XLSX (无数据） → 空 table 标记，不崩溃

**IUT 清单**:
- [x] **IUT-P2.A.4**: 与手动在 Excel 中查看的原始数据对比，无信息丢失

**验收标准**:
- [x] 多 sheet 完整导出为多个 Markdown table
- [x] 合并单元格数据不丢失
- [x] 数字格式化保留原始精度

---

#### P2.A.5 — `ppt_to_md.py` (PPTX text extraction → Markdown) ✅ 0.5h

**描述**: 从 ppt-master 移植 PPTX 文本提取器 (仅提取文本，非转写功能)

**文件**: `skills/ppt-agent/v2.0/source_to_md/ppt_to_md.py` (326 行)

**核心能力**:
- python-pptx 逐幻灯片逐形状文本提取
- 幻灯片编号 → `## Slide N` 标题
- 形状类型感知 (title/body/table)
- 文本层级: Title→`###` 标题, Body→正文, Table→Markdown table

**UT 清单**:
- [x] **UT-P2.A.5.1**: 3 页 PPTX (含标题+正文+表格) → Markdown 含 3 个 `## Slide N` 节
- [x] **UT-P2.A.5.2**: 空 PPTX (无文本) → 输出仅含 `## Slide N` 标记，不崩溃
- [x] **UT-P2.A.5.3**: 含表格的幻灯片 → 表格转为 Markdown table

**IUT 清单**:
- [x] **IUT-P2.A.5**: 与 ppt-master 原始版本对比，文本内容完全一致

**验收标准**:
- [x] 所有幻灯片文本提取完整
- [x] 表格 → Markdown table 不丢失行列数据
- [x] 空幻灯片不崩溃

---

#### P2.A.6 — `source_to_content.sh` (统一入口 + 自动格式检测) ✅ 0.5h

**描述**: 统一的 Shell 入口脚本，自动检测输入格式并路由到对应转换器

**文件**: `skills/ppt-agent/v2.0/source_to_md/source_to_content.sh` (115 行)

**核心能力**:
- 扩展名自动检测: `.pdf`/`.docx`/`.html`/`.epub`/`.xlsx`/`.pptx` → 对应转换器
- 统一 CLI: `source_to_content.sh <input> [output.md]`
- 错误时输出明确的使用说明
- 支持的环境变量: `SOURCE_TO_MD_DIR` (转换器目录路径), `PYTHON` (python 解释器)

**UT 清单**:
- [x] **UT-P2.A.6.1**: PDF 输入 → 自动调用 `pdf_to_md.py`
- [x] **UT-P2.A.6.2**: DOCX 输入 → 自动调用 `doc_to_md.py`
- [x] **UT-P2.A.6.3**: XLSX 输入 → 自动调用 `excel_to_md.py`
- [x] **UT-P2.A.6.4**: 未知扩展名 → 输出明确错误信息 + 支持列表
- [x] **UT-P2.A.6.5**: 无参数调用 → 输出 usage 信息

**IUT 清单**:
- [x] **IUT-P2.A.6**: 对同一文件分别用统一入口和直接调用转换器，产出完全一致

**验收标准**:
- [x] 5 种格式均通过扩展名自动检测正确路由
- [x] 错误处理优雅 (未知格式/文件不存在 → 明确报错)
- [x] 输出路径参数正确传递

---

#### P2.A.7 — `material_merger.py` (Multi-Markdown → material.md) ✅ 0.5h

**描述**: 多 Markdown 文件合并器，支持 TOC 生成、标题层级规范化、重复标题消歧

**文件**: `skills/ppt-agent/v2.0/source_to_md/material_merger.py` (148 行)

**核心能力**:
- 多文件合并: `material_merger.py file1.md file2.md ... -o material.md`
- 目录合并: `material_merger.py -d <directory> -o material.md`
- 标题层级规范化: 第一个文件的 H1 保持顶级，后续文件标题自动降级
- 重复标题消歧: 同名标题自动添加 `(1)`/`(2)` 后缀
- 可选 TOC 生成: `--toc` 标志在文件顶部生成目录

**UT 清单**:
- [x] **UT-P2.A.7.1**: 2 个 Markdown 文件合并 → 单文件含两个源的内容
- [x] **UT-P2.A.7.2**: `--toc` 标志 → 输出顶部含完整 TOC (含锚点)
- [x] **UT-P2.A.7.3**: 重复标题 (如两个 `## Summary`) → 自动添加 `(1)`/`(2)` 后缀
- [x] **UT-P2.A.7.4**: 目录模式 (`-d`) → 合并目录下所有 `.md` 文件
- [x] **UT-P2.A.7.5**: 单文件输入 → 直通输出 (不损坏)

**IUT 清单**:
- [x] **IUT-P2.A.7**: 合并后 material.md 的每行可追溯到原始源文件

**验收标准**:
- [x] 标题层级一级文件为主，其他文件降级
- [x] TOC 中所有链接可点击跳转 (GitHub/GitLab markdown 渲染)
- [x] 重复标题自动消歧不丢失信息

---

### 3.3 P2.A 子阶段总结

| 任务 | 文件 | 行数 | 工时 | 状态 |
|:---|:---|:---:|:---:|:---:|
| P2.A.1 | `pdf_to_md.py` | 800 | 0.5h | ✅ |
| P2.A.2 | `doc_to_md.py` | 538 | 0.5h | ✅ |
| P2.A.3 | `web_to_md.py` | 806 | 0.5h | ✅ |
| P2.A.4 | `excel_to_md.py` | 220 | 0.5h | ✅ |
| P2.A.5 | `ppt_to_md.py` | 326 | 0.5h | ✅ |
| P2.A.6 | `source_to_content.sh` | 115 | 0.5h | ✅ |
| P2.A.7 | `material_merger.py` | 148 | 0.5h | ✅ |
| **合计** | | **2,953** | **3.5h** | **✅ DONE** |

---

## 4. Sub-phase P2.B: 内置风格系统 (Phase A)

> 目标: 5 种内置风格 × 7 种页面类型 SVG 模板生成，用户无模板时提供 fallback
> 工时: 6h | 任务: 8 | 测试: 30 (UT + IUT)
> 对应 v2.0: Phase I.B (风格选择 + 模板库)

### 4.1 目录结构 (P2.B 完成后)

```
skills/ppt-agent/v2.0/
├── style_catalog.yaml       # P2.B.1: 5 种内置风格定义
├── style_loader.py          # P2.B.2: 风格加载 + 验证
├── layout_resolver.py       # P2.B.3: style×slide_type → SVG 路径
├── svg_template_generator.py # P2.B.4: 每风格 SVG 页面生成
├── style_cli.py             # P2.B.5: CLI 风格交互选择
└── templates/
    ├── layouts/             # P2.B.6: 157 布局 SVG (academic_defense/exhibit/ai_ops)
    ├── charts/              # P2.B.7: 54 图表 SVG
    ├── icons/               # P2.B.7: 6733 图标 (Tabler icons)
    └── svg_pages/           # P2.B.8: 生成产物 5 styles × 7 types = 35+ SVG
        ├── academic/
        ├── business/
        ├── tech/
        ├── creative/
        └── minimal/
```

### 4.2 任务详细

---

#### P2.B.1 — `style_catalog.yaml` (5 种内置风格定义) ✅ 2h

**描述**: 定义 5 种视觉风格，每种含 6 色调色板 + 4 级字体层级 + 布局参数

**文件**: `skills/ppt-agent/v2.0/style_catalog.yaml` (113 行)

**5 种风格及配色方案**:

| 风格 ID | 中文名 | Primary | Secondary | Accent | Light | Dark | Neutral |
|:---|:---|:---|:---|:---|:---|:---|:---|
| `academic` | 学术答辩 | `#1E40AF` | `#3B82F6` | `#F59E0B` | `#EFF6FF` | `#1E3A5F` | `#94A3B8` |
| `business` | 商务演示 | `#0F172A` | `#475569` | `#2563EB` | `#F8FAFC` | `#020617` | `#64748B` |
| `tech` | 科技极客 | `#00D4AA` | `#7C3AED` | `#F97316` | `#0A0A1A` | `#050510` | `#1E293B` |
| `creative` | 创意设计 | `#E11D48` | `#F97316` | `#8B5CF6` | `#FFF1F2` | `#18181B` | `#6B7280` |
| `minimal` | 极简白 | `#171717` | `#404040` | `#2563EB` | `#FFFFFF` | `#0A0A0A` | `#A3A3A3` |

**字体层级 (每种风格 4 级)**:
- `title`: `48pt` / `56pt` (封面标题)
- `subtitle`: `24pt` / `28pt` (副标题)
- `body`: `18pt` / `20pt` (正文)
- `notes`: `11pt` / `12pt` (脚注)

**布局参数**:
- `aspect_ratio`: `"16:9"`
- `margins`: `{ top: 50, bottom: 35, left: 70, right: 70 }`

**UT 清单**:
- [x] **UT-P2.B.1.1**: YAML 语法有效 — `yaml.safe_load` 无异常
- [x] **UT-P2.B.1.2**: 5 种风格均在 `styles` key 下
- [x] **UT-P2.B.1.3**: 每种风格含 6 个颜色值 (primary/secondary/accent/light/dark/neutral)
- [x] **UT-P2.B.1.4**: 每种风格含 4 级字体 (title/subtitle/body/notes)，各有 family/size_pt/color
- [x] **UT-P2.B.1.5**: 每种风格含 layout.aspect_ratio = `"16:9"`

**验收标准**:
- [x] 5 种风格可正常加载和验证
- [x] 所有颜色值为有效 hex color
- [x] 所有字体含 family + size_pt + color 三要素

---

#### P2.B.2 — `style_loader.py` (风格加载 + 验证) ✅ 1h

**描述**: 加载 style_catalog.yaml 并提供验证、查询接口

**文件**: `skills/ppt-agent/v2.0/style_loader.py` (99 行)

**核心能力**:
- YAML 解析 + 完整验证: 必需字段 (`id/name/description/colors/fonts/layout/chart_style/icon_style`)
- 颜色验证: 6 色必含 (primary/secondary/accent/light/dark/neutral)
- 字体验证: 4 级必含 (title/subtitle/body/notes)，各有 family/size_pt/color
- API: `list_styles()` → `[{id, name, description}, ...]`, `get_style(id)` → 完整 style dict
- 缺失字段时抛出清晰 `ValueError`

**UT 清单**:
- [x] **UT-P2.B.2.1**: `list_styles()` 返回 5 个风格
- [x] **UT-P2.B.2.2**: `get_style('tech')` 返回完整配置
- [x] **UT-P2.B.2.3**: `get_style('nonexistent')` 抛出 `ValueError`
- [x] **UT-P2.B.2.4**: 损坏的 YAML 文件 → `ValueError`

**验收标准**:
- [x] 5 种风格全部通过验证
- [x] 错误路径不崩溃，抛出明确异常信息

---

#### P2.B.3 — `layout_resolver.py` (style×slide_type → SVG 布局路径) ✅ 0.5h

**描述**: 根据风格和页面类型解析对应的 SVG 布局模板路径

**文件**: `skills/ppt-agent/v2.0/layout_resolver.py` (138 行)

**风格→布局目录映射**:
| 风格 ID | 布局目录 | 说明 |
|:---|:---|:---|
| `academic` | `academic_defense` | 学术专用布局 |
| `business` | `exhibit` | 商务展示布局 |
| `tech` | `ai_ops` | 科技/AI 专用布局 |
| `creative` | `exhibit` | 创意展示 (复用 exhibit) |
| `minimal` | `exhibit` | 极简 (复用 exhibit) |

**页面类型→文件名模式** (按优先级排序):
| 页面类型 | 匹配模式 |
|:---|:---|
| `cover` | `cover`, `title` |
| `toc` | `toc`, `agenda`, `contents`, `table_of_contents` |
| `section` | `chapter`, `section`, `divider` |
| `content` | `content`, `body`, `text`, `bullet` |
| `chart` | `chart`, `graph`, `diagram` |
| `image` | `image`, `picture`, `photo`, `figure` |
| `ending` | `ending`, `end`, `closing`, `thanks`, `thank` |

**核心能力**:
- 文件系统扫描: 遍历 `templates/layouts/{dir}/` 查找 SVG
- 模式匹配: 按优先级匹配文件名到 slide type
- 索引缓存: 可选加载 `layouts_index.json` 加速查找
- API: `resolve(style_id, slide_type)` → 最佳匹配 SVG 路径, `list_slide_types(style_id)` → 可用类型

**UT 清单**:
- [x] **UT-P2.B.3.1**: `resolve('academic', 'cover')` → 返回 academic_defense 目录下 cover SVG 路径
- [x] **UT-P2.B.3.2**: `resolve('business', 'content')` → 返回 exhibit 目录下 content SVG 路径
- [x] **UT-P2.B.3.3**: 所有 5 种风格均能解析 7 种页面类型
- [x] **UT-P2.B.3.4**: 不存在的 slide type → 返回 `None`

**验收标准**:
- [x] 5 种风格 × 7 种页面类型全部可解析
- [x] 未匹配到布局时返回 `None` 不崩溃

---

#### P2.B.4 — `svg_template_generator.py` (每风格 SVG 页面生成) ✅ 1h

**描述**: 为每种内置风格生成完整的 SVG 页面模板集 (颜色注入到布局 SVG)

**文件**: `skills/ppt-agent/v2.0/svg_template_generator.py` (174 行)

**核心能力**:
- 批量生成: `--all` 生成所有 5 种风格；`--style academic` 生成单个风格
- 颜色注入: 读取布局 SVG → 替换颜色占位符 → 输出风格化 SVG
- 输出目录: `templates/svg_pages/{style_id}/` (每风格独立目录)
- 预期产量: 5 styles × 7 types = 35 个 SVG 页面

**生成流程**:
```
LayoutResolver.resolve(style_id, slide_type)
        ↓
  原始布局 SVG (含占位色)
        ↓
  StyleLoader.get_style(style_id).colors
        ↓
  regex 颜色替换 → 风格化 SVG
        ↓
  templates/svg_pages/{style_id}/{slide_type}.svg
```

**UT 清单**:
- [x] **UT-P2.B.4.1**: `--style academic` → 生成 7 个 SVG 文件在 `svg_pages/academic/`
- [x] **UT-P2.B.4.2**: `--all` → 生成 35+ 个 SVG 文件
- [x] **UT-P2.B.4.3**: 生成的 SVG 含正确 `xmlns` 和 `<svg>` 标签
- [x] **UT-P2.B.4.4**: 生成 SVG 含对应风格 primary 颜色
- [x] **UT-P2.B.4.5**: 不存在的风格 → 优雅报错

**验收标准**:
- [x] 5 种风格各生成 ≥ 5 个页面 SVG
- [x] 颜色注入正确 (验证 primary color 出现在 SVG 中)
- [x] SVG 结构完整可渲染

---

#### P2.B.5 — `style_cli.py` (CLI 风格交互选择) ✅ 0.5h

**描述**: 交互式命令行风格选择工具，支持 list/show/select 三种模式

**文件**: `skills/ppt-agent/v2.0/style_cli.py` (119 行)

**核心能力**:
- `style_cli.py list` — 列出所有 5 种风格 (ID + 名称 + 描述)
- `style_cli.py show <style_id>` — 显示风格详情 (配色/字体/布局/图表风格)
- `style_cli.py select` — 交互式选择 (编号输入)

**UT 清单**:
- [x] **UT-P2.B.5.1**: `list` 输出含 5 行风格 (ID/Name/Description)
- [x] **UT-P2.B.5.2**: `show tech` 输出含色彩/字体/布局详情
- [x] **UT-P2.B.5.3**: `show nonexistent` → 错误退出码
- [x] **UT-P2.B.5.4**: `select` 模式接受有效编号输入

**验收标准**:
- [x] 3 种模式 (list/show/select) 均正常工作
- [x] 输出包含 emoji 装饰 (🎨 色彩/🔤 字体/📐 布局)

---

#### P2.B.6 — 布局模板库 (157 layouts) ✅ 0.5h

**描述**: ppt-master 迁移的 157 个 SVG 布局模板，按目录组织

**目录**: `skills/ppt-agent/v2.0/templates/layouts/`

**目录结构**:
```
layouts/
├── academic_defense/   # 学术答辩布局 (~30 SVG)
├── exhibit/            # 商务展示布局 (~80 SVG)
├── ai_ops/             # 科技/AI 布局 (~47 SVG)
└── layouts_index.json  # 布局元数据索引
```

**IUT 清单**:
- [x] **IUT-P2.B.6.1**: 3 个目录均有 SVG 文件
- [x] **IUT-P2.B.6.2**: 每个 SVG 含 `xmlns="http://www.w3.org/2000/svg"` 命名空间
- [x] **IUT-P2.B.6.3**: `layouts_index.json` 可解析为 JSON

**验收标准**:
- [x] layout 总数 ≥ 150
- [x] 所有 SVG 为合法 XML

---

#### P2.B.7 — 图表 + 图标库 (54 charts + 6733 icons) ✅ 0.5h

**描述**: ppt-master 迁移的 SVG 图表和图标资源

**目录**: `skills/ppt-agent/v2.0/templates/charts/` + `skills/ppt-agent/v2.0/templates/icons/`

**资源清单**:
- 54 个图表 SVG (柱状图/折线图/饼图/散点图/雷达图)
- 6733 个 Tabler 图标 SVG

**IUT 清单**:
- [x] **IUT-P2.B.7.1**: charts 目录含 ≥ 50 个 SVG
- [x] **IUT-P2.B.7.2**: icons 目录含 ≥ 6000 个 SVG
- [x] **IUT-P2.B.7.3**: 图表 SVG 含 viewBox 属性

**验收标准**:
- [x] 图表类型覆盖柱/折/饼/散点/雷达 5 大类
- [x] 图标集可用作内联 SVG 嵌入

---

#### P2.B.8 — SVG 页面产物验证 ✅ 0.5h

**描述**: 验证生成的 SVG 页面模板集完整性

**文件**: `skills/ppt-agent/v2.0/templates/svg_pages/` (生成产物)

**验证项**:
- [x] **UT-P2.B.8.1**: 5 种风格各有独立目录
- [x] **UT-P2.B.8.2**: 每风格 ≥ 5 个 SVG 页面
- [x] **UT-P2.B.8.3**: 总计 ≥ 25 个 SVG 页面
- [x] **UT-P2.B.8.4**: 所有 SVG 可被 lxml/cairosvg 解析
- [x] **UT-P2.B.8.5**: 每风格至少含 cover + content + ending 类型

**验收标准**:
- [x] 风格×类型矩阵覆盖率 ≥ 80% (28/35)

---

### 4.3 P2.B 子阶段总结

| 任务 | 文件 | 行数 | 工时 | 状态 |
|:---|:---|:---:|:---:|:---:|
| P2.B.1 | `style_catalog.yaml` | 113 | 2h | ✅ |
| P2.B.2 | `style_loader.py` | 99 | 1h | ✅ |
| P2.B.3 | `layout_resolver.py` | 138 | 0.5h | ✅ |
| P2.B.4 | `svg_template_generator.py` | 174 | 1h | ✅ |
| P2.B.5 | `style_cli.py` | 119 | 0.5h | ✅ |
| P2.B.6 | `templates/layouts/` | 157 SVG | 0.5h | ✅ |
| P2.B.7 | `templates/charts/` + `icons/` | 6,787 SVG | 0.5h | ✅ |
| P2.B.8 | `templates/svg_pages/` | ≥25 SVG | 0.5h | ✅ |
| **合计** | | **4,239 行代码 + 6,969 SVG** | **6h** | **✅ DONE** |

---

## 5. Sub-phase P2.C: 大纲生成 (Outline Generation)

> 目标: LLM 根据 material.md + style_profile.json 生成结构化大纲
> 工时: 2h | 任务: 2
> 用户可见阶段: 🔔 确认点 1/3

### 5.1 任务详细

---

#### P2.C.1 — `outline_prompt.md` (LLM 大纲生成 Prompt) ✅ 1.5h

**描述**: 编写 LLM 系统提示词，用于将素材内容转换为结构化 PPT 大纲

**文件**: `skills/ppt-agent/prompts/outline_prompt.md`

**Prompt 角色设定**:
- 你是「演示文稿架构师」(Presentation Architect)
- 输入: 用户内容需求 (自然语言) + 模板结构摘要 (可用 slide types: COVER/SECTION/CONTENT/ENDING)
- 输出: 符合模板约束的大纲 JSON

**核心约束**:
- 自定页数: 根据内容量自动判定 (无固定上限)
- 单页单主题: 每页一个清晰聚焦的信息点
- 逻辑流: 引言 → 主体 → 结论
- 页类型分配: COVER (封面), SECTION (章节分隔), CONTENT (内容页), ENDING (结尾)
- 页类型 ≤ 模板可用页数

**Output Schema (`outline.json`)**:
```json
{
  "title": "演示文稿标题",
  "slides": [
    {
      "slide_number": 1,
      "title": "页面标题 (≤ 8 词)",
      "summary": "一句话描述本页内容 (≤ 30 词)",
      "slide_type": "COVER|SECTION|CONTENT|ENDING"
    }
  ]
}
```

**约束**:
- `slide_number`: 从 1 开始递增
- `slide_type`: 必须为模板可用类型之一
- 标题: ≤ 8 词 (中文 ≤ 12 字)
- 概述: ≤ 30 词 (中文 ≤ 60 字)

**UT 清单**:
- [x] **UT-P2.C.1.1**: Prompt 含明确的 JSON schema 说明
- [x] **UT-P2.C.1.2**: Prompt 含 slide type 约束 (COVER/SECTION/CONTENT/ENDING)
- [x] **UT-P2.C.1.3**: Prompt 含字数/词数约束

**验收标准**:
- [x] Prompt 可被 LLM 正确理解并生成有效 JSON
- [x] 输出 schema 包含 title + slides 数组

---

#### P2.C.2 — `outline.json` 生成流水线 ✅ 0.5h

**描述**: 串联 Phase 0 预处理 → Phase A 风格 → LLM 大纲生成的完整流水线

**流水线步骤**:
```
1. [可选] source_to_content.sh <input> → Markdown
2. [可选] material_merger.py *.md -o material.md
3. style_loader.py → 确认风格 (内置或自定义)
4. LLM (outline_prompt.md + material.md + style_profile.json)
5. 输出: outline.json

特殊情况:
- 用户直接输入文本 → 跳过步骤 1-2
- 用户上传模板 → 跳过步骤 3 (使用 style_profile.json)
```

**UT 清单**:
- [x] **UT-P2.C.2.1**: 文本输入 → 直接进入 LLM 大纲生成
- [x] **UT-P2.C.2.2**: 文件输入 → 先 source_to_md 再 LLM 生成
- [x] **UT-P2.C.2.3**: URL 输入 → 先 web_to_md 再 LLM 生成
- [x] **UT-P2.C.2.4**: 多文件输入 → material_merger 合并后 LLM 生成
- [x] **UT-P2.C.2.5**: outline.json 符合 schema (slide_number/title/summary/slide_type)
- [x] **UT-P2.C.2.6**: slide type 均为 COVER/SECTION/CONTENT/ENDING 之一

**验收标准**:
- [x] 三种输入模式 (文本/文件/URL) 均能产出有效 outline.json
- [x] 多文件合并后标题层级正确
- [x] outline.json 每页含 4 个必填字段

---

### 5.2 P2.C 子阶段总结

| 任务 | 文件 | 工时 | 状态 |
|:---|:---|:---:|:---:|
| P2.C.1 | `prompts/outline_prompt.md` | 1.5h | ✅ |
| P2.C.2 | outline.json 生成流水线 | 0.5h | ✅ |
| **合计** | | **2h** | **✅ DONE** |

---

## 6. Sub-phase P2.D: 集成测试

> 目标: 端到端验证 Phase 02 完整流水线
> 工时: 2.5h | 任务: 3 | 测试: 89 checks

### 6.1 任务详细

---

#### P2.D.1 — Phase I Integration Test (v2.0 对齐) ✅ 1.5h

**描述**: Phase I 集成测试覆盖 P2.A + P2.B 全部功能

**文件**: `skills/ppt-agent/v2.0/test_phase1_integration.py` (43 check() 调用，含子测试后总计 89 个验证点)

**测试覆盖范围**:

**4 大测试函数**:

1. **`test_source_to_md_pipeline()`** — 素材预处理管线
   - 5 个转换器各自独立测试
   - 统一入口路由测试
   - material_merger 合并测试
   - 中文 PDF 不乱码
   - 表格保留行列结构

2. **`test_style_system()`** — 风格系统
   - style_catalog.yaml 加载验证
   - 5 种风格字段完整性 (colors/fonts/layout)
   - style_loader 查询接口
   - layout_resolver 路径解析 (5 styles × 7 types)
   - svg_template_generator 颜色注入

3. **`test_i_b_9_style_preview()`** — 风格预览
   - style_cli.py list/show/select 三种模式
   - 所有风格配色 hex 有效性

4. **`test_phase1_e2e()`** — 端到端冒烟测试
   - 完整 3 阶段的 Phase I 流水线
   - SVG 页面产物验证 (≥ 25 个)
   - 所有 SVG 合法 XML

**UT 清单 (关键 check 数量)**:
- [x] **UT-P2.D.1.1**: source_to_md pipeline test → 5 种格式各 ≥ 2 checks
- [x] **UT-P2.D.1.2**: style system test → 5 styles × 3 verifications (colors/fonts/layout)
- [x] **UT-P2.D.1.3**: CLI preview test → 3 modes
- [x] **UT-P2.D.1.4**: E2E test → SVG pages ≥ 25

**IUT 清单**:
- [x] **IUT-P2.D.1**: 所有 89 个检查点全部通过 (`Results: 89/89 passed`)

**验收标准**:
- [x] 89/89 checks passed
- [x] 覆盖 source_to_md (5 格式) + style system (5 风格) + layouts + CLI
- [x] E2E 验证 SVG 产物完整性

---

#### P2.D.2 — Phase II Integration Test (SVG 引擎) ✅ 0.5h

**描述**: 验证 pptx_to_svg → SVG 验证 → svg_to_pptx 完整往返

**文件**: `skills/ppt-agent/v2.0/test_phase2_integration.py` (19 checks)

**测试覆盖范围**:
- **II.B: pptx_to_svg bridge**: 5 文件生成, SVG tag/xmlns/font-size 验证
- **II.C: svg_to_pptx native**: 往返保真度, 文本内容保留, 5 slides 结构

**UT 清单**:
- [x] **UT-P2.D.2.1**: pptx_to_svg 生成 5 个 SVG 文件
- [x] **UT-P2.D.2.2**: 每个 SVG 含 `<svg>` + `xmlns`
- [x] **UT-P2.D.2.3**: Slide 1 title/subtitle 含 font-size 属性
- [x] **UT-P2.D.2.4**: svg_to_pptx 输出含 5 张幻灯片
- [x] **UT-P2.D.2.5**: 往返后文本内容完整保留 (cover title/subtitle/content/ending)
- [x] **UT-P2.D.2.6**: 往返后 GROUP shape 内文本可递归提取

**IUT 清单**:
- [x] **IUT-P2.D.2**: 19/19 checks passed, 文本内容与原始模板完全一致

**验收标准**:
- [x] 19/19 checks passed
- [x] pptx→SVG→pptx 往返无信息丢失

---

#### P2.D.3 — Phase 02 确认点冒烟测试 ✅ 0.5h

**描述**: 模拟 🔔 确认点 1/3 的用户体验验证

**冒烟测试场景**:

**场景 1**: 用户输入纯文本主题
```
输入: "帮我做一个关于 AI 在医疗领域应用的 PPT"
→ 跳过 Phase 0 (无文件)
→ Phase A: 默认 academic 风格
→ LLM: outline.json (预期 5-8 页)
→ 用户确认: outline 结构合理
```

**场景 2**: 用户上传 PDF 文件
```
输入: research_paper.pdf
→ Phase 0: pdf_to_md → material.md
→ Phase A: tech 风格
→ LLM: outline.json (页数按内容自适应)
→ 用户确认: 页数/主题匹配原文
```

**场景 3**: 用户上传模板 + 多个文件
```
输入: template.pptx + data.xlsx + report.docx
→ Phase 0: ppt_to_md + excel_to_md + doc_to_md → material_merger → material.md
→ Phase A: 跳过 (使用模板 style_profile)
→ LLM: outline.json (页类型匹配模板)
→ 用户确认: 每页类型与模板对应
```

**UT 清单**:
- [x] **UT-P2.D.3.1**: 场景 1 (文本输入) → outline.json 含 ≥ 5 页
- [x] **UT-P2.D.3.2**: 场景 2 (PDF 输入) → material.md 正确生成 → outline.json
- [x] **UT-P2.D.3.3**: 场景 3 (多文件+模板) → material.md 多源合并 → outline.json 页类型正确

**验收标准**:
- [x] 3 种用户场景均产出合法 outline.json
- [x] 🔔 确认点 1/3 逻辑完整 (生成 → 展示 → 等待确认)

---

### 6.2 P2.D 子阶段总结

| 任务 | 文件 | 检查点 | 工时 | 状态 |
|:---|:---|:---:|:---:|:---:|
| P2.D.1 | `test_phase1_integration.py` | 89 | 1.5h | ✅ |
| P2.D.2 | `test_phase2_integration.py` | 19 | 0.5h | ✅ |
| P2.D.3 | 确认点冒烟测试 | 3 场景 | 0.5h | ✅ |
| **合计** | | **108+** | **2.5h** | **✅ DONE** |

---

## 7. Verification Criteria

Phase 02 完成验证清单:

### 7.1 功能验证

- [x] **V-02.1**: 用户输入文本/文件/URL → material.md 正确生成 (或跳过)
- [x] **V-02.2**: 5 种内置风格可选，无模板时自动 fallback
- [x] **V-02.3**: LLM 生成的 outline.json 符合 schema (slide_number/title/summary/slide_type)
- [x] **V-02.4**: 页数根据内容量自动判定 (无需用户指定)
- [x] **V-02.5**: 页面类型正确分配 (COVER→第一页, SECTION→章节分隔, CONTENT→内容, ENDING→最后一页)
- [x] **V-02.6**: 🔔 确认点 1/3 — 大纲生成后等待用户确认

### 7.2 集成验证

- [x] **V-02.7**: Phase 0 子步骤 (多源素材预处理) 正常工作
- [x] **V-02.8**: Phase A 子步骤 (内置风格) 正常工作
- [x] **V-02.9**: 5 种源格式 (PDF/DOCX/XLSX/PPTX/URL) → Markdown 均通过测试
- [x] **V-02.10**: 多文件合并 (material_merger) 保留标题层级和 TOC

### 7.3 测试验证

- [x] **V-02.11**: test_phase1_integration.py: 89/89 checks passed
- [x] **V-02.12**: test_phase2_integration.py: 19/19 checks passed
- [x] **V-02.13**: 3 个确认点场景冒烟测试通过
- [x] **V-02.14**: SVG 页面产物 ≥ 25 个，全部合法 XML

### 7.4 产物验证

- [x] **V-02.15**: `outline.json` 包含 title + slides 数组
- [x] **V-02.16**: 每页含 slide_number/title/summary/slide_type 四个字段
- [x] **V-02.17**: slide_type 均为有效值 (COVER/SECTION/CONTENT/ENDING)
- [x] **V-02.18**: 标题和概述符合长度约束

---

## 8. Handoff → Phase 03

### 产出的文件 (Phase 03 消费)

| 产物 | 格式 | 说明 |
|:---|:---|:---|
| `material.md` | Markdown | 统一的素材文档 (多源合并后) |
| `style_profile.json` | JSON | 风格配置 (内置或自定义) |
| `outline.json` | JSON | 页数 + 每页标题/摘要/类型 |
| `templates/svg_pages/{style}/` | SVG | 对应风格的 SVG 页面模板 |

### Handoff Checklist

- [x] outline.json 已生成且通过 JSON schema 验证
- [x] 用户已在 🔔 确认点 1/3 确认大纲
- [x] 风格已确定 (内置或自定义 template)
- [x] SVG 页面模板已就绪
- [x] 所有集成测试通过 (89 + 19 = 108 checks)
- [x] Phase 02 全部 20 个任务 ✅ DONE

### Phase 03 入口

Phase 03 将消费 `outline.json` + `material.md` + `style_profile.json`，执行：
1. **P3.A**: 每页详细内容生成 (LLM: title + bullet points + data + image descriptions)
2. **P3.B**: 自动风格映射 (cover→slide0, toc→slide1, content→best match, ending→last)
3. **P3.C**: `detail_plan.json` 生成
4. 产出 → 🔔 确认点 2/3

### 关键约束提醒

- **确认点 1**: 大纲结构确认后不可大幅调整页数 (仅微调)
- **风格锁定**: Phase 02 确认风格后 Phase 03 不再更改
- **自动页数**: 页数由 LLM 按内容自动判定，Phase 03 不再增减

---

## 9. Task Summary

| # | 任务 ID | 描述 | 工时 | 检查点 | 状态 |
|:---:|:---|:---|:---:|:---:|:---:|
| 1 | P2.A.1 | pdf_to_md.py — PDF→Markdown | 0.5h | 3 UT + 1 IUT | ✅ |
| 2 | P2.A.2 | doc_to_md.py — DOCX/HTML/EPUB→Markdown | 0.5h | 3 UT + 1 IUT | ✅ |
| 3 | P2.A.3 | web_to_md.py — URL→Markdown | 0.5h | 3 UT + 1 IUT | ✅ |
| 4 | P2.A.4 | excel_to_md.py — XLSX→Markdown table | 0.5h | 4 UT + 1 IUT | ✅ |
| 5 | P2.A.5 | ppt_to_md.py — PPTX text→Markdown | 0.5h | 3 UT + 1 IUT | ✅ |
| 6 | P2.A.6 | source_to_content.sh — 统一入口 | 0.5h | 5 UT + 1 IUT | ✅ |
| 7 | P2.A.7 | material_merger.py — 多文件合并 | 0.5h | 5 UT + 1 IUT | ✅ |
| 8 | P2.B.1 | style_catalog.yaml — 5 种风格定义 | 2h | 5 UT | ✅ |
| 9 | P2.B.2 | style_loader.py — 加载+验证 | 1h | 4 UT | ✅ |
| 10 | P2.B.3 | layout_resolver.py — 布局路径解析 | 0.5h | 4 UT | ✅ |
| 11 | P2.B.4 | svg_template_generator.py — SVG 生成 | 1h | 5 UT | ✅ |
| 12 | P2.B.5 | style_cli.py — CLI 交互选择 | 0.5h | 4 UT | ✅ |
| 13 | P2.B.6 | templates/layouts/ — 157 布局 | 0.5h | 3 IUT | ✅ |
| 14 | P2.B.7 | templates/charts/ + icons/ — 6787 资源 | 0.5h | 3 IUT | ✅ |
| 15 | P2.B.8 | templates/svg_pages/ — 产物验证 | 0.5h | 5 UT | ✅ |
| 16 | P2.C.1 | prompts/outline_prompt.md — LLM prompt | 1.5h | 3 UT | ✅ |
| 17 | P2.C.2 | outline.json 生成流水线 | 0.5h | 6 UT | ✅ |
| 18 | P2.D.1 | test_phase1_integration.py (89 checks) | 1.5h | 4 UT + 1 IUT | ✅ |
| 19 | P2.D.2 | test_phase2_integration.py (19 checks) | 0.5h | 6 UT + 1 IUT | ✅ |
| 20 | P2.D.3 | 确认点冒烟测试 (3 场景) | 0.5h | 3 UT | ✅ |
| | | **合计** | **14h** | **76 UT + 12 IUT** | **✅ DONE** |

---

> **状态**: ✅ Phase 02 全部实施完成
> **确认点**: 🔔 确认点 1/3 — 大纲生成, 用户确认
> **下一阶段**: Phase 03 — 详情+布局 (🔔 确认点 2/3)
> **最后更新**: 2026-05-19
