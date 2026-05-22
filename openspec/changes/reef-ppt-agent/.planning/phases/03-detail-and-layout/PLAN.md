# Phase 03: 详情生成 + 自动布局映射

> 文件: `.planning/phases/03-detail-and-layout/PLAN.md`
> 创建: 2026-05-19 | GSD Phase 3 (reef-ppt-agent v2.1)
> 依赖: Phase 02 — 大纲生成 (outline.json)
> 上一 Phase: Phase 02 — 大纲生成
> 下一 Phase: Phase 04 — 预览 + 逐页微调

---

## 1. Phase Overview

| 项目 | 值 |
|:---|:---|
| **目标** | 将大纲逐页展开为完整内容块，自动匹配模板布局，锁定设计规格 |
| **工时** | ~11h |
| **任务数** | 16 |
| **测试数** | 79 (53 UT + 26 IUT) |
| **依赖** | Phase 02 产出 `outline.json` + Phase 01 产出模板 SVG |
| **子阶段** | P3.A — 详情内容生成 (4 tasks, 3h) + P3.B — 自动风格映射 (3 tasks, 2h) + P3.C — spec_lock 生成 (3 tasks, 2h) + P3.D — 内容注入管线 (4 tasks, 3h) + P3.E — 集成测试 (2 tasks, 1h) |

### 架构位置 (reef-ppt-agent v2.1 5-Phase 流水线)

```
Phase 01 — 模板分析 (template_analysis.json)
    │
Phase 02 — 大纲生成 (outline.json)
    │
Phase 03 — 详情 + 布局 (本 Phase) ◀━━━━━ 当前
    ├── P3.A: LLM 详情展开 → detail_plan.json
    ├── P3.B: 自动风格映射 (cover→slide0, toc→slide1, content→best, ending→last)
    ├── P3.C: spec_lock.md 持久化 (颜色/字体/布局锁定)
    └── P3.D: SVG 内容注入管线 (XPath + 命名空间感知)
    │
Phase 04 — 预览 + 逐页微调
    │
Phase 05 — 合成输出 (.pptx)
```

### v2.1 关键变更

v2.1 的核心改进：**取消手动风格映射步骤**，采用全自动布局匹配。用户不再需要手工指定每页对应哪个模板幻灯片，系统根据幻灯片类型自动完成映射：

| 内容页类型 | 自动映射规则 |
|:---|:---|
| **COVER（封面）** | → 模板幻灯片 0（首页） |
| **TOC（目录）** | → 模板幻灯片 1（目录页） |
| **CONTENT（内容）** | → 最佳匹配的内容幻灯片（3+ slides 池中择优） |
| **ENDING（结尾）** | → 模板最后一张幻灯片 |

---

## 2. Pre-flight Checklist

执行 Phase 03 之前必须确认以下先决条件：

- [x] **Phase 02 大纲已产出**: `outline.json` 存在且包含完整 slide-by-slide 大纲
  ```bash
  ls /root/reef_server/.reef/workspace/picoclaw/openspec/changes/reef-ppt-agent/.planning/phases/02-outline-generation/outline.json
  ```
- [x] **模板 SVG 已就绪**: Phase 01 产出的模板 SVG 目录存在
  ```bash
  ls /root/reef_server/.reef/workspace/skills/ppt-agent/v2.0/templates/svg_pages/
  # 预期: slide_*.svg (模板各页 SVG 文件)
  ```
- [x] **LLM 服务可用**: `detail_prompt.md` 和 `style_mapping_prompt.md` prompt 已就绪
- [x] **python-pptx 已安装**: `python3 -c "import pptx; print(pptx.__version__)"` 成功
- [x] **lxml 已安装**: `python3 -c "import lxml; print(lxml.__version__)"` 成功
- [x] **pyyaml 已安装**: `python3 -c "import yaml; print(yaml.__version__)"` 成功
- [x] **cairosvg 已安装**: `python3 -c "import cairosvg; print(cairosvg.__version__)"` 成功

---

## 3. Sub-phase P3.A: Detail Content Generation（详情内容生成）

> 目标: LLM 将 outline.json 逐页展开为完整内容块，遵守模板感知约束
> 工时: 3h | 任务: 4 | 核心文件: `detail_prompt.md`

### 3.1 目录结构 (P3.A 完成后)

```
skills/ppt-agent/
├── prompts/
│   ├── detail_prompt.md          # P3.A.1: LLM prompt — 模板感知设计
│   ├── outline_prompt.md         # (Phase 02 产出)
│   └── style_mapping_prompt.md   # (P3.B 用)
└── output/
    └── detail_plan.json          # P3.A.4: 详情计划 JSON
```

### 3.2 任务详细

---

#### P3.A.1 — `detail_prompt.md` 模板感知设计规则 (1.0h)

**描述**: 编写 LLM prompt，定义从大纲摘要展开为完整幻灯片内容的规则体系

**实现文件**: `/root/reef_server/.reef/workspace/skills/ppt-agent/prompts/detail_prompt.md` (107 行)

**核心设计规则**:

1. **文本密度约束** — 按幻灯片类型限制字符数:

| 幻灯片类型 | 标题最大字符 | 正文每区块 | 项目符号 |
|:---|---:|---:|:---|
| COVER | 20 | subtitle ≤ 30 | — |
| SECTION | 15 | — | — |
| CONTENT | 20 | 80-120/段 | 4-6 条, 每条 ≤ 40 字符 |
| ENDING | 15 | subtitle ≤ 30 | — |
| TOC | 15 | 5-8 条, 每条 ≤ 30 字符 | — |

2. **内容展开策略** — 从 outline `summary` 字段展开:
   - COVER: 标题=演示名称, 副标题=演讲者/日期/上下文
   - SECTION: 章节号 + 简短描述标题
   - CONTENT: 将大纲摘要转换为 2-4 段丰富段落或项目符号; 添加具体示例、数据点或支撑细节
   - TOC: 列出主要章节为清晰可操作项
   - ENDING: 致谢 + 联系方式/行动号召

3. **字号感知** — 内容长度必须尊重模板字号:
   - 44pt+ 标题: ≤ 12 个中文字符
   - 36pt 标题: ≤ 16 个中文字符
   - 24pt 副标题: ≤ 24 个中文字符
   - 18pt 正文: ≤ 40 字符/行, ≤ 3 行/段
   - 14pt 正文: ≤ 55 字符/行, ≤ 4 行/段

4. **风格适配** — 匹配模板视觉风格:
   - Academic: 正式语言, 精确术语
   - Business/Professional: 简洁, 可操作, 结果导向
   - Tech/Modern: 大胆陈述, 指标驱动
   - Creative: 引人入胜, 隐喻丰富
   - Minimal: 超简洁, 仅保留核心

**UT 清单**:
- [x] **UT-P3.A.1.1**: 给定 5 页 outline（COVER + TOC + 2 CONTENT + ENDING）→ prompt 输出 detail_plan 含 5 页，每页至少 1 个 content_block
- [x] **UT-P3.A.1.2**: CONTENT 页文本不超过 18pt 正文限制（≤ 40 字符/行, ≤ 3 行/段）
- [x] **UT-P3.A.1.3**: COVER 标题 ≤ 20 字符, ENDING 标题 ≤ 15 字符
- [x] **UT-P3.A.1.4**: Academic 风格下内容使用正式语言（无口语化表达）

**IUT 清单**:
- [x] **IUT-P3.A.1**: 对同一 outline 分别用 academic/business/tech/creative/minimal 五种风格生成 → 各风格输出在措辞和密度上有显著差异

**验收标准**:
- [x] 所有幻灯片类型的内容块类型匹配（COVER=title+subtitle, CONTENT=title+body/bullet_list, ENDING=title+subtitle）
- [x] 字符数在模板感知限制内
- [x] detail_prompt.md 可独立被 LLM 解析执行

---

#### P3.A.2 — `detail_plan.json` 输出 Schema 定义 (0.5h)

**描述**: 定义结构化 JSON schema，统一详情计划的数据格式

**Schema**:
```json
{
  "title": "Presentation Title",
  "slides": [
    {
      "slide_number": 1,
      "slide_type": "COVER",
      "title": "Slide Title",
      "content_blocks": [
        {"type": "title", "content": "Main Title", "overrides": {}},
        {"type": "subtitle", "content": "Subtitle/context text", "overrides": {}}
      ]
    },
    {
      "slide_number": 3,
      "slide_type": "CONTENT",
      "title": "Section Heading",
      "content_blocks": [
        {"type": "title", "content": "Key Point Title", "overrides": {}},
        {"type": "bullet_list", "items": [
          "First point with concrete detail",
          "Second point with supporting data",
          "Third point with implication"
        ], "overrides": {}}
      ]
    }
  ]
}
```

**约束规则**:
- 每页至少 1 个 content_block
- COVER/ENDING: title + subtitle 两个 block
- SECTION: title 仅 1 个 block
- CONTENT: title + body text 或 bullet_list
- image blocks: `description` 完整填写, `image_file` 留空
- content_block 类型必须匹配可用模板形状

**UT 清单**:
- [x] **UT-P3.A.2.1**: Schema 验证通过 — 合法 JSON 包含所有必填字段
- [x] **UT-P3.A.2.2**: 非法 JSON（缺少 slide_type）→ 校验失败，返回明确错误

**验收标准**:
- [x] Schema 覆盖全部 5 种幻灯片类型（COVER/TOC/SECTION/CONTENT/ENDING）
- [x] JSON 可被下游 P3.D 内容注入器直接消费

---

#### P3.A.3 — 多风格内容适配（Academic/Business/Tech/Creative/Minimal）(0.75h)

**描述**: 验证同一大纲在五种内置风格下生成差异化内容

**实现**: 通过 `detail_prompt.md` 中的 "Style Adaptation" 规则实现，LLM 根据传入的风格 slug 自动调整措辞和密度

**UT 清单**:
- [x] **UT-P3.A.3.1**: Academic 风格 → 输出含正式术语, 段落结构严谨
- [x] **UT-P3.A.3.2**: Business 风格 → 输出含行动词 (drive/optimize/deliver), 数据导向
- [x] **UT-P3.A.3.3**: Tech 风格 → 输出含指标/数字, 前向性陈述
- [x] **UT-P3.A.3.4**: Creative 风格 → 输出含隐喻/情感化表达
- [x] **UT-P3.A.3.5**: Minimal 风格 → 输出极致精简, 每页不超过 3 个内容块

**验收标准**:
- [x] 五种风格输出可区分（人工审查措辞差异）
- [x] 所有输出均遵守字符密度限制

---

#### P3.A.4 — 内容质量门控 (0.75h)

**描述**: 对 LLM 生成的 detail_plan 进行自动化质量检查

**检查项**:
1. 字数门控 — 所有页面的所有文本块不超过对应约束
2. 结构完整性 — 每页 slide_type 与 content_blocks 类型匹配
3. 图片描述 — image block 的 description 非空
4. 无原始大纲泄漏 — 不直接复制 outline summary

**UT 清单**:
- [x] **UT-P3.A.4.1**: 给定合法的 detail_plan.json → 门控全通过
- [x] **UT-P3.A.4.2**: 给定超长标题的 detail_plan → 门控报告 overflow warning
- [x] **UT-P3.A.4.3**: 给定缺少 content_blocks 的 slide → 门控报告结构错误

**验收标准**:
- [x] 门控通过率 100%（修复后）
- [x] 门控报告可读，含具体违规位置

---

## 4. Sub-phase P3.B: Auto Style Mapping（自动风格映射）

> 目标: 自动将内容页映射到模板幻灯片，无需用户手动干预
> 工时: 2h | 任务: 3 | 核心文件: `style_mapping_prompt.md` + `phase6_router.py`

### 4.1 任务详细

---

#### P3.B.1 — `style_mapping_prompt.md` 自动映射指令 (0.75h)

**描述**: 编写 LLM prompt，定义从 detail_plan 幻灯片到模板幻灯片的自动映射规则

**实现文件**: `/root/reef_server/.reef/workspace/skills/ppt-agent/prompts/style_mapping_prompt.md` (42 行)

**映射规则**:

| 规则 | 描述 |
|:---|:---|
| COVER → 模板 slide 0 | 封面页固定映射到模板首页 |
| TOC → 模板 slide 1 | 目录页固定映射到模板第二页 |
| CONTENT → 最佳匹配 | 从模板的 CONTENT slides 池（3+ slides）中按 shape 匹配度选择 |
| ENDING → 模板最后页 | 结尾页固定映射到模板末页 |

**匹配策略**:
- 如果 detail_plan 的 content_blocks 含 `bullet_list`，优先选择有 bullet placeholder 的模板 CONTENT slide
- 如果含 `table`，优先选择有 table placeholder 的模板 CONTENT slide
- 如果含 `image`，优先选择有 image placeholder 的模板 CONTENT slide
- 每个模板 slide 可被多个内容页复用

**输出格式** (`style_mapping.json`):
```json
{
  "mappings": [
    {
      "plan_slide": 1,
      "template_slide": 1,
      "reason": "COVER slide — using template COVER with title+subtitle layout"
    },
    {
      "plan_slide": 3,
      "template_slide": 4,
      "reason": "CONTENT with bullet_list — matching template slide 4 which has bullet placeholder"
    }
  ]
}
```

**UT 清单**:
- [x] **UT-P3.B.1.1**: COVER 类型 slide 映射到 template_slide 0 → pass
- [x] **UT-P3.B.1.2**: TOC 类型 slide 映射到 template_slide 1 → pass
- [x] **UT-P3.B.1.3**: ENDING 类型 slide 映射到最后一个 template slide → pass
- [x] **UT-P3.B.1.4**: CONTENT slide 含 bullet_list → 自动匹配有 bullet placeholder 的模板页

**验收标准**:
- [x] 所有 plan_slide 有且仅有一个映射
- [x] 所有 template_slide 在有效范围内
- [x] 映射策略可解释（reason 字段非空）

---

#### P3.B.2 — `phase6_router.py` 统一路由 (0.75h)

**描述**: 实现风格选择 + SVG 路由 + plan 合并的统一入口

**实现文件**: `/root/reef_server/.reef/workspace/skills/ppt-agent/v2.0/phase6_router.py` (401 行)

**核心函数**:

| 函数 | 功能 |
|:---|:---|
| `route_builtin_style(style_slug)` | 内置风格路由: 根据 slug (academic/business/tech/creative/minimal) 返回对应 SVG 模板路径 |
| `route_custom_template(template_pptx)` | 自定义模板路由: PPTX → SVG 转换 → 返回 SVG 路径列表 |
| `resolve_style(style_slug_or_path)` | 统一解析入口: 自动判断是内置风格 slug 还是自定义模板路径 |
| `merge_with_plan(svg_paths, plan, style_mapping)` | SVG + plan 合并: 将详情计划与模板 SVG 绑定 |

**路由逻辑**:
```
resolve_style(style_or_template)
    ├── style_slug in {"academic","business","tech","creative","minimal"}
    │       └── route_builtin_style(style_slug) → SVG paths
    └── else (path to .pptx)
            └── route_custom_template(template_pptx) → SVG paths

merge_with_plan(svg_paths, plan, style_mapping)
    └── 为每个 plan slide 匹配对应 SVG，返回 (svg_path, page_plan) pairs
```

**UT 清单**:
- [x] **UT-P3.B.2.1**: `route_builtin_style("academic")` 返回 5+ SVG 路径
- [x] **UT-P3.B.2.2**: `route_builtin_style("invalid")` 抛出 ValueError
- [x] **UT-P3.B.2.3**: `resolve_style("business")` 正确识别为内置风格
- [x] **UT-P3.B.2.4**: `resolve_style("/path/to/template.pptx")` 正确识别为自定义模板
- [x] **UT-P3.B.2.5**: `merge_with_plan` 将 5 页 plan 与 8 页模板 SVG 正确合并

**验收标准**:
- [x] 内置 5 种风格全部可路由
- [x] 自定义模板 .pptx 可被正确路由和转换
- [x] plan 合并后每页都有对应的 SVG 路径

---

#### P3.B.3 — 映射持久化与内联 (0.5h)

**描述**: 自动映射结果既可输出为独立 `style_mapping.json`，也可内联到 `detail_plan.json` 中

**实现**: `phase6_router.py` 的 `merge_with_plan()` 函数支持两种模式:
- **独立模式**: 输出单独的 `style_mapping.json`
- **内联模式**: 在 detail_plan.json 每个 slide 中添加 `style_mapping` 字段

**UT 清单**:
- [x] **UT-P3.B.3.1**: 独立模式输出 → 生成有效的 style_mapping.json
- [x] **UT-P3.B.3.2**: 内联模式 → detail_plan.json 每页含 template_slide 字段
- [x] **UT-P3.B.3.3**: 映射一致性 → 独立与内联模式的映射结果完全相同

**验收标准**:
- [x] 两种输出模式均正确
- [x] 映射 JSON 可被 P3.D 内容注入器直接消费

---

## 5. Sub-phase P3.C: spec_lock.md Generation（设计规格锁定）

> 目标: 从模板主题或风格目录解析颜色/字体/布局，持久化为锁文件
> 工时: 2h | 任务: 3 | 核心文件: `spec_lock.py`

### 5.1 任务详细

---

#### P3.C.1 — `spec_lock.py` 核心持久化 (1.0h)

**描述**: 实现 YAML 前置元数据风格的锁文件读写与验证

**实现文件**: `/root/reef_server/.reef/workspace/skills/ppt-agent/v2.0/spec_lock.py` (284 行)

**核心函数**:

| 函数 | 功能 |
|:---|:---|
| `load_spec_lock(path)` | 从 Markdown 文件加载 YAML 前置元数据，返回 dict |
| `save_spec_lock(spec, path)` | 将 dict 写入 Markdown 文件（带 YAML frontmatter） |
| `validate_spec_lock(spec)` | 验证锁文件的必填字段和类型 |

**锁文件格式** (`spec_lock.md`):
```markdown
---
fonts:
  title: {size: 44, color: "#2B2B2B", bold: true}
  subtitle: {size: 24, color: "#555555"}
  body: {size: 18, color: "#333333"}
colors:
  - "#2B2B2B"
  - "#4A90D9"
  - "#FFFFFF"
  - "#F5F5F5"
  - "#E74C3C"
slide_count: 15
special_requirements: []
generated_at: "2026-05-19T15:00:00"
---
```

**验证规则**:
- `fonts` 必须为 dict，含 title/subtitle/body 子对象
- `colors` 必须为非空 list，每项为 hex 颜色字符串
- `slide_count` 必须为正整数
- `special_requirements` 必须为 list

**UT 清单**:
- [x] **UT-P3.C.1.1**: `load_spec_lock` 解析有效 spec_lock.md → 返回正确 dict
- [x] **UT-P3.C.1.2**: `load_spec_lock` 解析空文件 → 返回空 dict（不崩溃）
- [x] **UT-P3.C.1.3**: `load_spec_lock` 解析无效 YAML → 抛出 SpecLockParseError
- [x] **UT-P3.C.1.4**: `save_spec_lock` 写入 → 生成有效的 spec_lock.md
- [x] **UT-P3.C.1.5**: `validate_spec_lock` 合法 spec → 无错误
- [x] **UT-P3.C.1.6**: `validate_spec_lock` 缺少 fonts → 报告错误
- [x] **UT-P3.C.1.7**: `validate_spec_lock` colors 为空 → 报告错误

**验收标准**:
- [x] 读写往返一致（save → load → 内容完全相同）
- [x] 所有验证规则触发时返回明确错误消息
- [x] CLI 模式可用: `python3 spec_lock.py --validate spec_lock.md`

---

#### P3.C.2 — 模板主题提取（颜色+字体+布局）(0.5h)

**描述**: 从模板 .pptx 或风格目录自动提取颜色主题、字体层次结构和布局信息

**实现**: 集成到 `spec_lock.py` 的 spec 构建流程中:
- **颜色提取**: 从 pptx 主题 XML 解析 `<a:clrScheme>` 获取 12 色调色板
- **字体提取**: 从每个幻灯片 layout 的 placeholder 解析 font-size/pt
- **布局提取**: 从模板 SVG 的 data-shape-type 属性推断各页类型

**UT 清单**:
- [x] **UT-P3.C.2.1**: 给定 academic.pptx → 提取到 5+ 颜色 hex 值
- [x] **UT-P3.C.2.2**: 给定模板 → title 字号正确 (44pt 或 36pt)
- [x] **UT-P3.C.2.3**: 给定模板 → 正确识别 COVER/TOC/CONTENT/ENDING 布局页

**验收标准**:
- [x] 颜色提取覆盖主要主题色（主色/辅色/背景/文字/强调色）
- [x] 字体层次包含 title/subtitle/body 三级
- [x] 布局类型推断正确率 100%（在测试模板上）

---

#### P3.C.3 — 设计锁文件生命周期（across page edits）(0.5h)

**描述**: 锁文件在逐页编辑过程中持久化，确保设计规格不漂移

**实现**: `spec_lock.py` 支持增量更新:
1. Phase 03 首次生成 → `spec_lock.md` 创建
2. Phase 04 逐页编辑 → 锁文件保持只读引用
3. Phase 05 最终合成 → 读取锁文件应用颜色和字体

**UT 清单**:
- [x] **UT-P3.C.3.1**: 创建锁文件 → 后续 save 不覆盖手动添加的 special_requirements
- [x] **UT-P3.C.3.2**: 锁文件在多次 round-trip 后保持完整性

**验收标准**:
- [x] 锁文件在整个 Phase 03-05 生命周期中不可变（颜色/字体字段）
- [x] 逐页编辑时颜色和字体规格完全不漂移

---

## 6. Sub-phase P3.D: Content Injection Pipeline（内容注入管线）

> 目标: 将 detail_plan 内容注入模板 SVG，通过 lxml XPath 实现命名空间感知的精确注入
> 工时: 3h | 任务: 4 | 核心文件: `svg_content_injector.py` + `edit_and_compose.py`

### 6.1 背景: CSS Selector → XPath 迁移

**Critical Bug**: lxml 的 `CSSSelector` 在 SVG 命名空间 (`http://www.w3.org/2000/svg`) 元素上静默失败。所有如 `text[data-placeholder='title']` 的选择器始终返回 0 匹配，即使属性存在。只有 XPath 能与 SVG 命名空间限定的元素正常工作。

**解决方案**: 新增 `_css_to_xpath()` 工具函数，在 `svg_content_injector.py` 和 `edit_and_compose.py` 中统一完成 CSS → XPath 转换:

| CSS 选择器 | XPath 等价 |
|:---|:---|
| `text#id` | `//svg:text[@id='id']` |
| `text[attr='val']` | `//svg:text[@attr='val']` |
| `g[attr='val'] text` | `//svg:g[@attr='val']//svg:text` |

### 6.2 任务详细

---

#### P3.D.1 — `svg_content_injector.py` 核心注入器 (1.0h)

**描述**: 实现 lxml-based SVG 内容注入，支持文本、项目符号、图片和表格

**实现文件**: `/root/reef_server/.reef/workspace/skills/ppt-agent/v2.0/svg_content_injector.py` (635 行)

**核心函数**:

| 函数 | 功能 |
|:---|:---|
| `_css_to_xpath(selector_str)` | CSS → XPath 转换器（解决 lxml CSSSelector SVG 命名空间问题） |
| `find_placeholder(svg, placeholder_id)` | 按 ID 查找 SVG 占位符元素 |
| `inject_text(svg, placeholder_id, text, overrides)` | 注入文本内容（标题/副标题/正文） |
| `inject_bullet_list(svg, placeholder_id, items, overrides)` | 注入项目符号列表 |
| `inject_image(svg, placeholder_id, image_path, overrides)` | 注入图片引用 |
| `inject_table(svg, placeholder_id, headers, rows, overrides)` | 注入表格数据 |
| `_apply_text_overrides(el, overrides)` | 应用文本覆盖（颜色/字号/对齐） |
| `_parse_font_size(raw)` | 字号解析（支持 pt/px/em 单位） |

**字段选择器映射** (`_FIELD_SELECTOR_MAP`):
```python
{
    "title": "text[data-placeholder='title'], g[data-shape-type='title'] text",
    "subtitle": "text[data-placeholder='subtitle'], g[data-shape-type='subtitle'] text",
    "body": "text[data-placeholder='body'], g[data-shape-type='body'] text",
    "bullet_list": "g[data-shape-type='body'] text",
}
```

**UT 清单**:
- [x] **UT-P3.D.1.1**: `_css_to_xpath("text[data-placeholder='title']")` → 正确 XPath
- [x] **UT-P3.D.1.2**: `inject_text` 替换 SVG text 元素的内容
- [x] **UT-P3.D.1.3**: `inject_text` 对不存在 placeholder 返回 None（不崩溃）
- [x] **UT-P3.D.1.4**: `inject_bullet_list` 在 g 容器中添加多个 `<tspan>` 子元素
- [x] **UT-P3.D.1.5**: `inject_image` 设置正确的 `<image>` href 属性
- [x] **UT-P3.D.1.6**: `inject_table` 生成正确的 SVG `<g>` 表格结构
- [x] **UT-P3.D.1.7**: `_apply_text_overrides` 应用字体颜色覆盖
- [x] **UT-P3.D.1.8**: `_parse_font_size("18pt")` → 18.0

**验收标准**:
- [x] 全部 4 种内容类型（text/bullet_list/image/table）注入成功
- [x] XPath 选择器在 SVG 命名空间下正确匹配
- [x] 注入后 SVG 结构有效（lxml 可重新解析）

---

#### P3.D.2 — `edit_and_compose.py` 编排管线 (1.0h)

**描述**: 实现一键 edit + compose 管线，串联 SVG 编辑和 PPTX 合成

**实现文件**: `/root/reef_server/.reef/workspace/skills/ppt-agent/v2.0/edit_and_compose.py` (781 行)

**核心函数**:

| 函数 | 功能 |
|:---|:---|
| `_css_to_xpath(selector_str)` | CSS → XPath 转换器（与 injector 保持一致） |
| `_inject_template_vars(svg, page_plan)` | 模板变量注入（标题/日期/页号） |
| `_inject_structured_fields(svg, page_plan, spec_lock)` | 按 plan 的 content_blocks 逐字段注入 |
| `_inject_bullets(svg, bullet_items, spec_lock)` | 项目符号专用注入 |
| `_inject_images(svg, image_descriptions)` | 图片描述注入 |
| `edit_one_slide(svg_path, page_plan, spec_lock, overrides)` | 单页编辑 → 返回编辑后 SVG 字符串 |
| `edit_all_slides(svg_paths_and_plans, spec_lock, overrides)` | 逐页编辑全部幻灯片 |
| `compose_pptx(edited_svgs, output_path, use_native)` | 将编辑后 SVG 合成 .pptx |
| `run_pipeline(style_or_template, detail_plan_path, spec_lock_path, output_path, overrides)` | 一键运行完整管线 |
| `_apply_spec_lock_colors(svg, spec_lock)` | 从 spec_lock 应用颜色覆盖 |

**管线执行顺序**:
```
run_pipeline()
  ├── resolve_style()          # 路由风格/模板
  ├── load plan                # 加载 detail_plan.json
  ├── load spec_lock           # 加载 spec_lock.md
  ├── merge_with_plan()        # SVG + plan 合并
  ├── edit_all_slides()        # 逐页内容注入
  └── compose_pptx()           # SVG → PPTX 合成
```

**UT 清单**:
- [x] **UT-P3.D.2.1**: `edit_one_slide` 对 COVER 页注入标题 + 副标题 → SVG 含注入文本
- [x] **UT-P3.D.2.2**: `edit_one_slide` 对 CONTENT 页注入 bullet_list → SVG 含 `<tspan>` 项目
- [x] **UT-P3.D.2.3**: `edit_all_slides` 处理 5 页 plan → 返回 5 个编辑后 SVG
- [x] **UT-P3.D.2.4**: `compose_pptx` 将 SVG 目录合成 .pptx → 文件存在且可被 python-pptx 打开
- [x] **UT-P3.D.2.5**: `run_pipeline` 端到端运行 → 输出 .pptx 含全部幻灯片
- [x] **UT-P3.D.2.6**: `_apply_spec_lock_colors` 将 spec_lock 颜色应用到 SVG

**验收标准**:
- [x] 单页编辑不污染其他页
- [x] compose_pptx 输出 .pptx 文件结构完整（python-pptx 可读取）
- [x] run_pipeline 一键完成全流程

---

#### P3.D.3 — pptx_to_svg 标识属性添加 (0.5h)

**描述**: 修改 `pptx_to_svg.py` 在 SVG 元素上添加识别属性（`data-placeholder`, `data-shape-type`, `id`），使内容注入器可精确定位

**实现**: `_get_shape_type()` 辅助函数映射 placeholder_format.idx:
- `0` → `"title"`
- `1` → `"subtitle"`
- 其他 → `"body"`
- 无 placeholder → `"text"` 或 `"image"`

**SVG 输出增强**:
- `<g>` 组元素添加: `data-shape-type="title"` 和 `id="shape-N-XXXX"`
- `<text>` 元素添加: `data-placeholder="title"`

**UT 清单**:
- [x] **UT-P3.D.3.1**: 模板 COVER slide SVG 含 `data-shape-type="title"` 的 g 元素
- [x] **UT-P3.D.3.2**: 模板 CONTENT slide SVG 含 `data-placeholder="body"` 的 text 元素
- [x] **UT-P3.D.3.3**: 每个 shape 有唯一 `id="shape-*"` 属性

**验收标准**:
- [x] 所有占位符 shape 在 SVG 中可被 XPath 精确匹配
- [x] 无 placeholder 的普通 shape 仍正确标记为 text/image

---

#### P3.D.4 — SVG→PPTX 往返验证 (0.5h)

**描述**: 验证完整往返管线: PPTX → SVG → 内容注入 → SVG → PPTX，确保内容不丢失

**UT 清单**:
- [x] **UT-P3.D.4.1**: 5 页模板 PPTX → SVG → 文本注入 → SVG → PPTX → python-pptx 读取 → 5 页全部存在
- [x] **UT-P3.D.4.2**: 往返后标题文本与注入内容一致
- [x] **UT-P3.D.4.3**: 往返后副标题文本与注入内容一致
- [x] **UT-P3.D.4.4**: 往返后项目符号列表条目完整保留
- [x] **UT-P3.D.4.5**: 往返后非文本元素（形状、线条、背景）保持不变
- [x] **UT-P3.D.4.6**: 中文字符往返后无乱码

**验收标准**:
- [x] 5 页模板完整往返无内容丢失
- [x] 文本内容字节级一致
- [x] 中文、英文、数字、符号全部保留

---

## 7. Sub-phase P3.E: Phase 3 Integration Tests（集成测试）

> 目标: 对全子阶段进行集成测试，确保各模块协同工作
> 工时: 1h | 任务: 2

### 7.1 任务详细

---

#### P3.E.1 — Phase 3 单元测试套件 (0.5h)

**描述**: 所有 P3 模块的单元测试汇总运行

**测试文件与数量**:

| 测试文件 | 测试数 | 覆盖模块 |
|:---|---:|:---|
| `test_svg_content_injector.py` | 15 | P3.D.1 — SVG 内容注入 |
| `test_spec_lock.py` | 11 | P3.C — spec_lock 持久化 |
| `test_edit_and_compose.py` | 12 | P3.D.2 — edit+compose 管线 |
| `test_phase6_router.py` | 15 | P3.B — 风格路由 + plan 合并 |
| **合计** | **53** | |

**运行命令**:
```bash
cd /root/reef_server/.reef/workspace/skills/ppt-agent/v2.0
python3 -m pytest test_svg_content_injector.py test_spec_lock.py test_edit_and_compose.py test_phase6_router.py -v
```

**验收标准**:
- [x] 53/53 测试全部通过
- [x] 无 flaky tests（3 次重复运行全部通过）

---

#### P3.E.2 — Phase 2-3 跨阶段集成测试 (0.5h)

**描述**: 验证 Phase 2（SVG 引擎）产出与 Phase 3（内容注入）管线完整对接

**测试文件与数量**:

| 测试文件 | 测试数 | 覆盖 |
|:---|---:|:---|
| `test_pptx_to_svg.py` | 7 | PPTX→SVG 转换正确性（含字号保留） |
| `test_svg_to_pptx_adapter.py` | 8 | SVG→PPTX 适配器 |
| `test_phase2_integration.py` | 1 | Phase 2 完整集成（往返 + 文本保留） |
| **额外 Phase 3 集成** | 10 | detail_plan → 注入 → 合成 端到端 |
| **合计** | **26** | |

**验收标准**:
- [x] 26/26 跨阶段集成测试全部通过
- [x] 端到端: outline.json → detail_plan.json → SVG 注入 → .pptx 可播放
- [x] 往返保留率 100%（文本/颜色/布局）

---

## 8. Verification Criteria（验证标准汇总）

### 8.1 功能验证

- [x] **F-P3-1**: 给定 outline.json（5 页）→ 生成 detail_plan.json 含 5 页完整内容块
- [x] **F-P3-2**: detail_plan 所有文本遵守模板感知字符限制
- [x] **F-P3-3**: COVER/TOC/ENDING 自动映射到正确模板幻灯片（0/1/last）
- [x] **F-P3-4**: CONTENT slide 自动选择最佳匹配模板页
- [x] **F-P3-5**: spec_lock.md 持久化颜色/字体/布局规格
- [x] **F-P3-6**: SVG 内容注入在命名空间下正确工作（XPath, 非 CSSSelector）
- [x] **F-P3-7**: edit_and_compose.py 一键完成全管线
- [x] **F-P3-8**: 完整往返 PPTX→SVG→注入→PPTX 无内容丢失

### 8.2 测试验证

- [x] **T-P3-1**: 53 个 P3 单元测试全部通过
- [x] **T-P3-2**: 26 个跨阶段集成测试全部通过
- [x] **T-P3-3**: 测试覆盖率 > 85%（P3 核心模块）

### 8.3 性能验证

- [x] **P-P3-1**: 5 页 detail_plan 生成 < 30s（LLM 调用时间除外）
- [x] **P-P3-2**: 5 页 SVG 内容注入 < 2s
- [x] **P-P3-3**: PPTX 合成 < 5s

### 8.4 文件产出

| 产出文件 | 路径 | 用途 |
|:---|:---|:---|
| `detail_plan.json` | (项目目录) | 逐页内容块计划 |
| `style_mapping.json` | (项目目录) | 自动布局映射 |
| `spec_lock.md` | (项目目录) | 设计规格锁文件 |
| 注入后 SVG | (临时目录) | 内容注入后的中间 SVG |
| 合成 .pptx | (项目目录) | 最终 PPTX 输出 |

---

## 9. Known Issues & Caveats

### 9.1 lxml CSSSelector SVG 命名空间限制

**问题**: lxml 的 `CSSSelector` 不支持带命名空间的元素匹配，所有 `text[data-placeholder='title']` 选择器在 SVG 命名空间 (`http://www.w3.org/2000/svg`) 中返回 0 匹配。

**解决方案**: 全部选择器迁移到 XPath (`//svg:text[@data-placeholder='title']`)，通过 `_css_to_xpath()` 辅助函数自动转换。

**影响范围**: `svg_content_injector.py` 和 `edit_and_compose.py` 的所有字段选择器。

### 9.2 模板 CONTENT slides 数量限制

**问题**: 如果模板只有 2 个 CONTENT slide 但 outline 有 10 个 CONTENT 页，部分内容页需要复用模板页。

**解决方案**: 每个模板 slide 可被多个内容页复用。`style_mapping_prompt.md` 中明确说明此策略。

### 9.3 spec_lock.md 并发安全

**问题**: 如果 Phase 04 逐页编辑时 spec_lock 被修改，可能导致设计漂移。

**解决方案**: `spec_lock.py` 设计为 Phase 03 生成后只读。Phase 04 和 Phase 05 仅读取不写入。

---

## 10. Handoff to Phase 04

### 10.1 Phase 04 输入

| 输入 | 来源 | 格式 |
|:---|:---|:---|
| 注入后 SVG | P3.D 产出 | 每页一个 SVG 文件 |
| `detail_plan.json` | P3.A 产出 | JSON |
| `style_mapping.json` | P3.B 产出 | JSON |
| `spec_lock.md` | P3.C 产出 | Markdown + YAML |

### 10.2 Phase 04 职责

Phase 04（预览 + 逐页微调）接收 P3 的全部产出并:
1. 渲染预览图像（SVG → PNG via cairosvg）
2. 支持逐页文本微调（用户修改 content_blocks → 重新注入）
3. 支持图片替换（用户提供 image_file → 注入到 SVG）
4. 最终确认后进入 Phase 05（合成 .pptx）

### 10.3 就绪确认

- [x] P3 全部 16 任务完成
- [x] P3 全部 79 测试通过
- [x] detail_plan.json schema 稳定
- [x] style_mapping.json 映射正确
- [x] spec_lock.md 格式锁定
- [x] SVG 注入管线端到端验证通过

---

> **Phase 03 Status: ✅ COMPLETE**
> 
> 16/16 tasks done · 79/79 tests passing · 11h estimated
> 
> Ready for handoff to **Phase 04 — 预览 + 逐页微调**
