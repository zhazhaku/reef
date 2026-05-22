# Reef PPT Agent — 最终详细设计文档 (v2.3)

> 日期: 2026-05-21
> 设计师: Hermes Coordinator
> 状态: 融合更新
> 
> **核心诊断**（2026-05-20 确认）:
> PPT 生成质量问题的根因不是"技术 bug"，
> 而是架构层面 **AI 大模型没有参与模板理解和内容布局**。
> v1 克隆引擎（XML deep copy）反而是落地 AI 前置分析层的**最佳载体**。
> 
> **v2.3 融合更新**（2026-05-21）:
> - 🔥 P0: 修复 slide master 丢失 — shutil.copy 保留用户模板母版、layouts、主题
> - 🔥 模板解析增强: `detect_template_patterns()` 空间分组 — 容器检测、可重复模式识别
> - 🔥 布局规划升级: 新增 `clone_group` 行动 — AI 可克隆容器组并偏移，支持内容动态扩展
> - 📋 实施路线更新: 总工时 ~27.5h（~4 工作日）

---

## 目录

1. [问题根因诊断](#一问题根因诊断)
2. [改进架构：AI 分析 + 引擎执行](#二改进架构ai-分析--引擎执行)
3. [引擎选择与路由](#三引擎选择与路由)
   - 3.0 [母版保留 — 修复 slide master 丢失](#30-母版保留--修复-slide-master-丢失-) 🔥 新增（v2.3）
   - 3.1 [v1 克隆引擎 — AI 前置分析层的最佳载体](#31-v1-克隆引擎--ai-前置分析层的最佳载体)
   - 3.2 [路由规则](#32-路由规则)
   - 3.3 [三层升级在 v1 上的映射](#33-三层升级在-v1-上的映射)
4. [三层升级详细设计](#四三层升级详细设计)
   - [第一层: AI 模板深度分析](#第一层-ai-模板深度分析-phase-1-增强--critical)
     - 4.1.1 [空间分组增强](#411-空间分组增强-detect_template_patterns-) 🔥 新增（v2.3）
   - [第二层: AI 内容-布局智能匹配](#第二层-ai-内容-布局智能匹配-phase-3-增强--critical)
     - 4.2.1 [容器感知布局](#421-容器感知布局-clone_group-) 🔥 新增（v2.3）
   - [第三层: 引擎执行](#第三层-引擎执行--按场景选择)
     - 4.3.1 [clone_group 行动](#431-clone_group-行动-expand_container_group-) 🔥 新增（v2.3）
5. [用户可见流水线（5 阶段）](#五用户可见流水线5-阶段)
6. [关键数据结构](#六关键数据结构)
7. [实施路线](#七实施路线)
   - 7.0 [P0 母版修复](#70-p0-母版修复-) 🔥 新增（v2.3）
   - 7.1 [阶段 1: AI 模板分析 + v1 克隆引擎增强](#阶段-1-ai-模板分析--v1-克隆引擎增强--18h)
   - 7.2 [阶段 2: 预览管道](#阶段-2-预览管道--6h)
   - 7.3 [阶段 3: 溢出处理与质量门](#阶段-3-溢出处理与质量门--7h)
8. [当前状态与下一步](#八当前状态与下一步)
9. [附录: 相关文档索引](#附录-相关文档索引)

---

## 一、问题根因诊断

### 1.1 当前架构的缺陷

```
用户模板.pptx
    ↓
template_parser.py (机械提取: hex颜色, 字体名, 字号, 形状类型)
    ↓
ppt_compositor.py (机械操作: _auto_match_shape → fill_text_block → 输出)
    ↓
最终 PPTX: 文字进了模板，但毫无设计感 ⚠️
```

**核心缺陷：AI 大模型在整个链条中只参与了大纲和内容生成，完全没有参与"模板理解"和"内容布局适配"。**

所有设计决策都由机械规则完成：
- `_auto_match_shape()`: 按 placeholder idx=0 找标题，毫无语义理解
- `_get_default_style()`: 硬编码 EMU 值（254000=20pt），不继承模板真实字号
- 内容溢出：不处理，直接截断或超出形状边界

### 1.2 对比：ppt-master 怎么做

| 维度 | 当前 reef-ppt-agent | ppt-master |
|------|---------------------|------------|
| 模板理解 | 机械提取 hex/font/size | LLM 深度分析设计语言 |
| 内容布局 | `_auto_match_shape` 猜 placeholder | LLM 理解模板框架后自由排版 |
| 生成方式 | python-pptx 填充已有形状 | AI 手写 SVG → DrawingML 转换 |
| 设计一致性 | 无约束 | `spec_lock.md` 约束所有页面 |
| AI 价值 | 只在大纲/内容生成 | **全程参与设计决策** |

---

## 二、改进架构：AI 分析 + 引擎执行

### 2.1 核心原则

```
┌──────────────────────────────────────────────┐
│                                              │
│   AI 负责: "理解模板" + "决定什么内容放哪里"    │
│   引擎负责: "执行填入" + "完美保留样式"         │
│                                              │
│   分工清晰，各自做擅长的事                      │
└──────────────────────────────────────────────┘
```

### 2.2 全链路架构图

```
用户模板 .pptx + 内容需求
      │
      ▼
┌─────────────────────────────────┐
│  Phase 1: AI 模板深度分析        │  ← 新增核心环节
│  ─────────────────────────────  │
│  输入: template_parser.py 的     │
│        structure.json (机械数据)  │
│  LLM 分析 → design_language.md:  │
│    • 视觉 DNA (配色逻辑/字体层次)  │
│    • 布局模式 (每类页面的分区)      │
│    • 元素语义 (装饰/功能元素角色)    │
│    • 内容适配规则 (溢出策略)        │
└──────────────┬──────────────────┘
               │
               ▼
┌─────────────────────────────────┐
│  Phase 2: 大纲生成               │  🔔 确认点 1/3
│  ─────────────────────────────  │
│  LLM 基于 design_language.md +   │
│  用户内容需求 → 生成大纲          │
│  自动匹配合适页数                  │
└──────────────┬──────────────────┘
               │ (用户确认)
               ▼
┌─────────────────────────────────┐
│  Phase 3: AI 内容-布局智能匹配    │  🔔 确认点 2/3
│  ─────────────────────────────  │
│  LLM 基于 design_language.md     │
│  生成 detail_plan.json +         │
│       layout_plan.json:          │
│    • 每个 content_block → 哪个形状│
│    • 溢出处理策略                 │
│    • 形状装饰/强调建议             │
└──────────────┬──────────────────┘
               │ (用户确认)
               ▼
┌─────────────────────────────────┐
│  Phase 4: 预览 + 微调            │  🔔 确认点 3/3
│  ─────────────────────────────  │
│  SVG→PNG 图片预览                │
│  逐页对话式微调                   │
└──────────────┬──────────────────┘
               │ (用户确认)
               ▼
       ┌───────┴───────┐
       ▼               ▼
┌──────────────┐ ┌──────────────┐
│ v1 克隆引擎   │ │ v2 SVG 引擎   │
│ (自定义模板)  │ │ (内置风格)    │
│              │ │              │
│ XML deep     │ │ LLM 手写 SVG │
│ copy + 按    │ │ → svg2pptx   │
│ layout_plan  │ │ 原生导出      │
│ 精确填入      │ │              │
│              │ │              │
│ ✅ 模板样式   │ │ ✅ 灵活创作   │
│   100% 保留   │ │              │
└──────┬───────┘ └──────┬───────┘
       └───────┬────────┘
               ▼
         最终 output.pptx
```

---


---

## 三、引擎选择与路由

### 3.0 母版保留 — 修复 slide master 丢失 🔥 新增（v2.3 融合）

#### 问题表现

- 用户模板：**3 个 slide master、14 个 slide layout**
- 生成结果：**1 个 slide master、11 个 layout（python-pptx 默认模板的）**
- 用户模板的主题色、字体方案、txStyles 继承链全部丢失

#### 根因

`compose_with_layout_plan()` 和 `compose()` 创建输出 PPTX 时调用 `Presentation()`，这创建的是 python-pptx **默认空白模板**，不是用户模板：

```python
dst_prs = Presentation()  # ← python-pptx 默认空白模板
```

然后对每一页调用 `clone_slide(src_prs, tpl_idx, dst_prs)`。`clone_slide` 只深拷贝 slide 的 spTree（形状 XML），但 `dst_prs.slide_layouts` 始终来自默认模板。即使用户 slide 的 layout 名称被匹配到，该 layout 的 **slide master 引用** 仍指向默认 master，而非用户模板的 master。

**结论**: `clone_slide` 只复制了 slide 级别的形状，自定义 masters、layouts、theme 从未被转移到输出文件中。

#### 修复方案（推荐）

以用户模板为基创建输出文件，而非从空白开始：

```python
import shutil
shutil.copy(template_path, output_path)
dst_prs = Presentation(output_path)
# 删除模板原有 slides（保留 masters/layouts/theme）
while len(dst_prs.slides) > 0:
    _delete_slide(dst_prs, 0)
# 然后用 clone_slide 逐页复制用户选中的 slide
for tpl_idx in template_indices:
    clone_slide(src_prs, tpl_idx, dst_prs)
```

这样 `dst_prs` 天然拥有用户模板的 slide masters、layouts、主题，后续 `clone_slide` 添加的 slide 能正确继承母版样式。

> **分类**: 纯工程修复，不涉及 AI/LLM 改动  
> **工时**: ~0.5h  
> **优先级**: P0 — 模板保真度的基础前提，必须先修

### 3.1 v1 克隆引擎 — AI 前置分析层的最佳载体

```
v1 克隆引擎（XML deep copy）
  ├─ 优势：完美保留模板样式（主题继承、PLACEHOLDER、字号/颜色/字体）
  ├─ 缺陷：_auto_match_shape() 是机械规则，不理解模板设计意图
  └─ 修复：增加 AI 前置分析层 → LLM 输出 layout_plan.json 驱动填入

v2 SVG 引擎
  ├─ 优势：AI 可以直接编辑 SVG
  ├─ 缺陷：SVG↔PPTX 往返丢失主题继承链、PLACEHOLDER→TEXT_BOX
  └─ 适用场景：内置风格（无模板保真度要求）
```

**v1 的缺陷不是引擎本身，而是缺少 AI 前置分析层。增加后即成为最优解。**

### 3.2 路由规则

| 条件 | 引擎 | 理由 |
|------|------|------|
| 用户上传自定义 .pptx 模板 | **v1 克隆引擎** + AI 前置分析 | XML deep copy 100% 保留样式 |
| 用户选择内置风格（无模板） | v2 SVG 引擎 | 无模板保真度要求 |
| 混合场景（模板 + AI 图表） | v1 克隆引擎 + 图片嵌入 | 主体克隆，图表图片嵌入 |

### 3.3 三层升级在 v1 上的映射

| 升级层 | 在 v1 克隆引擎上的实现 | 说明 |
|---------|----------------------|------|
| 1. AI 模板深度分析 | `design_language.md` 由 LLM 生成 | 完全复用 |
| 2. 内容-布局智能匹配 | `layout_plan.json` 替代 `style_mapping.json` | 从机械规则升级为 AI 决策 |
| 3. AI SVG 生成 | **不需要**（v1 克隆引擎替代） | v1 天然规避 SVG 往返丢失问题 |

---


---

## 四、三层升级详细设计

### 第一层: AI 模板深度分析 (Phase 1 增强) 🔴 CRITICAL

**新增输出**: `design_language.md`（LLM 生成的设计理解文档）

**实现路径**:
```
Phase 1:
  现有: template_parser.py → structure.json (机械数据)
  新增: structure.json → LLM 模板分析 Prompt → design_language.md
```

**`design_language.md` 内容规范**:

```markdown
# 模板设计语言分析

## 视觉 DNA
- **风格**: [科技蓝商务风/学术严谨风/创意活泼风/...]
- **配色逻辑**: 主色 #XXXXXX 用于标题强调，辅色 #XXXXXX 用于...
- **字体层次**: 封面标题 XXpt XX字体 bold → 章节标题 XXpt → 正文 XXpt

## 布局模式
- **封面页 (Slide 0)**: [标题位置区域，副标题位置，装饰元素描述]
- **目录页 (Slide 1)**: [布局描述，最多容纳 N 项]
- **内容页 (Slide 2..N-1)**: [标题栏位置，正文区域大小，每页最多 X 行]
- **结尾页 (Slide N)**: [致谢/联系方式区域]

## 元素语义
- [元素描述] → [语义角色]，每个 [页面类型] 必出现 / 可选
- [元素描述] → 装饰元素，保持原样不修改

## 内容适配规则
- 正文超过 N 行 → 拆分为两页
- 标题超过 M 字 → 缩小到 XXpt，换行处理
- 有数据 → 优先使用含表格/图表的模板页
```

**实现**: 新增 `prompts/template_analysis_deep.md` Prompt 文件

---


#### 4.1.1 空间分组增强 — `detect_template_patterns()` 🔥 新增（v2.3 融合）

##### 问题

当前 `template_parser.py` 输出的 `structure.json` 是扁平 shape 列表：

```json
{
  "slides": [{
    "slide_type": "CONTENT",
    "shapes": [
      {"type": "AUTO_SHAPE", "name": "矩形: 圆角 8", "left": 4390232, ...},
      {"type": "TEXT_BOX", "name": "文本框 5", "left": 4450000, ...},
      ...
    ]
  }]
}
```

LLM 从这个扁平列表中**看不到**：
- ❌ 形状之间的空间包含关系（哪个 shape 在哪个 shape 里面）
- ❌ 容器模式识别（矩形框 + 内部文字 = 一个「卡片」组件）
- ❌ 可重复组检测（同尺寸同间距的多个框 = 可扩展模式）
- ❌ 形状角色语义（标签 vs 内容容器 vs 装饰元素）

结果：LLM 无法理解模板的设计意图，`layout_planning.md` prompt 收到的仍是扁平列表，只能做 semantic → spatial → font-size 三级机械匹配。

##### 新增: `detect_template_patterns()`

在 `template_parser.py` 的 `parse_template()` 流程中，机械提取 shape 属性后，新增空间分组步骤：

```python
def detect_template_patterns(slide_shapes):
    """
    检测 slides 中的设计模式，返回结构化分组。
    
    检测规则:
    1. 容器检测: 矩形/圆角矩形 + 内部的文字/图标 → card 组件
    2. 重复模式: 同尺寸同间距的多个容器 → repeatable_group
    3. 层级关系: 标题→副标题→内容区的空间划分
    4. 装饰元素: 纯色块、线条、背景图形
    """
```

**检测规则详解**：

| 规则 | 触发条件 | 输出标签 | 示例 |
|------|---------|---------|------|
| **容器检测** | 圆角矩形/矩形 + 内部文字 shape(s) + 可选图标 | `card` 组件 | 模板中的「要点卡片」 |
| **可重复模式** | 同尺寸（±5% 容差）同间距（±10% 容差）的多个 card | `repeatable_group` | 2 个并排卡片 → 可扩展到 3 个 |
| **层级关系** | 按 Y 坐标从上到下划分 title → subtitle → content 区域 | `layout_zones` | 标题区(0-15%)/副标题区(15-30%)/内容区(30-100%) |
| **装饰元素** | 纯色填充无文字的大矩形、水平/垂直线条 | `decoration` | 背景色块、分割线 |

**增强后的 `structure.json` 输出**：

```json
{
  "slides": [{
    "slide_type": "CONTENT",
    "shapes": [...],
    "components": [
      {
        "type": "card",
        "container_shape_idx": 2,
        "label_shape_idx": 3,
        "content_shape_idx": 4,
        "group_id": "card_group_1",
        "repeatable": true
      },
      {
        "type": "card",
        "container_shape_idx": 5,
        "label_shape_idx": 6,
        "content_shape_idx": 7,
        "group_id": "card_group_1",
        "repeatable": true
      }
    ],
    "layout_zones": {
      "title_zone": {"y_min": 0, "y_max": 0.15},
      "subtitle_zone": {"y_min": 0.15, "y_max": 0.30},
      "content_zone": {"y_min": 0.30, "y_max": 1.0}
    },
    "decorations": [
      {"shape_idx": 0, "type": "background_block"}
    ]
  }]
}
```

这个增强后的结构直接输入到 LLM 的 `design_language.md` 生成 prompt 中，使 AI 能理解：
- 模板有哪些**组件**（卡片、标签组、图表区）
- 哪些组件可以**重复扩展**（内容多时自动加卡片）
- 页面的**空间分区**（标题区、内容区边界）
- 哪些是**装饰元素**（不填充内容，保留原样）

### 第二层: AI 内容-布局智能匹配 (Phase 3 增强) 🔴 CRITICAL

**当前**: `_auto_match_shape` 按 placeholder idx + 字号启发式匹配

**改进**: LLM 基于 `design_language.md` 输出 `layout_plan.json`

**`layout_plan.json` 结构**:

```json
{
  "slides": [
    {
      "template_slide": 0,
      "plan_slide": 0,
      "layout_strategy": "cover",
      "blocks": [
        {
          "content_key": "title",
          "target_shape_idx": 0,
          "target_shape_name": "标题 1",
          "action": "fill_text",
          "style_overrides": {}
        },
        {
          "content_key": "subtitle",
          "target_shape_idx": 1,
          "target_shape_name": "副标题 2",
          "action": "fill_text",
          "style_overrides": {}
        }
      ]
    },
    {
      "template_slide": 2,
      "plan_slide": 1,
      "layout_strategy": "content_bullets",
      "overflow_strategy": "split_to_two_slides",
      "blocks": [
        {
          "content_key": "title",
          "target_shape_idx": 0,
          "target_shape_name": "标题 1",
          "action": "fill_text"
        },
        {
          "content_key": "bullets",
          "target_shape_idx": 1,
          "target_shape_name": "内容占位符 2",
          "action": "fill_bullet_list",
          "style_overrides": {
            "font_size": 18
          }
        }
      ]
    }
  ]
}
```

**关键改进**:
- 每个 content_block **精确指定**目标形状（按 idx + name 双重定位）
- `overflow_strategy` 明确溢出处理方案
- `action` 指定操作类型（fill_text / fill_bullet_list / fill_table / fill_image / decor）
- `style_overrides` 仅在用户明确指定时使用，否则继承模板

**实现**: 新增 `prompts/layout_planning.md` Prompt 文件，修改 `ppt_compositor.py` 支持 `layout_plan.json` 驱动

---


#### 4.2.1 容器感知布局 — `clone_group` 行动 🔥 新增（v2.3 融合）

##### 问题

在 v2.2 的 `layout_plan.json` 中，LLM 只能为**已存在的 shape** 指定 `fill_text`/`fill_bullet_list` 等行动。当用户内容需要的卡片数量超过模板提供的卡片数量时（例如模板有 2 个卡片但内容需要 3 个），LLM 无法扩展。

同时，LLM 的 matching 是在「扁平的 shape 列表」上进行的，无法利用容器关系做更智能的匹配。

##### 新增: 容器感知匹配 + clone_group 行动

有了 §4.1.1 的空间分组后，`layout_planning.md` prompt 可以指导 LLM：

1. **优先匹配到组件组**而非单个 shape — 将 `components` 作为匹配目标
2. **当内容项 > 组件数时**，使用 `clone_group` 行动克隆最后一个容器组并偏移
3. **识别装饰元素**并将其标记为 `decor`（保持原样不填充）

**`layout_plan.json` 中新增 `clone_group` 行动示例**：

```json
{
  "blocks": [
    {
      "action": "clone_group",
      "group_id": "card_group_1",
      "source_component_idx": 1,
      "offset_emu": {"x": 0, "y": 1900000},
      "fill_content": [
        {"shape_role": "label", "text": "新要点 3"},
        {"shape_role": "content", "text": "第三个要点的详细内容..."}
      ]
    }
  ]
}
```

**`layout_planning.md` prompt 更新要点**：

- 在 prompt 中描述 `components` 和 `layout_zones` 的语义
- 添加 `clone_group` 行动的使用说明：何时使用、offset 如何计算
- 强调：优先使用已有组件，仅在内容溢出且存在 `repeatable_group` 时使用克隆
- 装饰元素（`decorations`）应使用 `decor` 行动，不填充内容

### 第三层: 引擎执行 🔵 按场景选择

**自定义模板**: v1 克隆引擎（`ppt_compositor.py`）
- XML deep copy 保留 slide 的完整样式
- 按 `layout_plan.json` 精确填入内容
- 不经过 SVG 往返，零样式丢失

**内置风格**: v2 SVG 引擎
- AI 手写 SVG → `svg2pptx` 原生导出
- 适用于无模板保真度要求的场景

---


#### 4.3.1 clone_group 行动 — `expand_container_group()` 🔥 新增（v2.3 融合）

在 `ppt_compositor.py` 的 `LAYOUT_PLAN_ACTIONS` 字典中新增 `clone_group` 行动，对应实现函数 `expand_container_group()`：

```python
LAYOUT_PLAN_ACTIONS = {
    "fill_text":        fill_text_block,
    "fill_bullet_list": fill_bullet_list,
    "fill_table":       fill_table_block,
    "fill_image":       fill_image_block,
    "clone_group":      expand_container_group,  # 🔥 新增（v2.3）
    "decor":            None,
    "skip":             None,
}
```

**`expand_container_group()` 执行流程**：

```python
def expand_container_group(slide, block, template_slide, src_prs):
    """
    克隆一个容器组（card_group）到当前 slide，
    偏移位置，清除旧文本，填入新内容。
    
    Args:
        block.source_component_idx: 源组件的索引
        block.offset_emu: {x, y} 偏移量（EMU）
        block.fill_content: [{shape_role, text}, ...]
    """
    # 1. 从 template slide 深拷贝容器 shape 及其内部所有子 shape
    source_component = template_slide.components[block.source_component_idx]
    for shape_idx in source_component.all_shape_indices:
        cloned_shape = deep_copy_shape(template_slide.shapes[shape_idx], slide)
        # 2. 偏移位置
        cloned_shape.left += block.offset_emu['x']
        cloned_shape.top += block.offset_emu['y']
        # 3. 清除旧文本
        if cloned_shape.has_text_frame:
            for para in cloned_shape.text_frame.paragraphs:
                para.clear()
    # 4. 按 fill_content 填入新内容
    for fill_item in block.fill_content:
        target_shape = find_shape_by_role(slide, fill_item.shape_role)
        set_shape_text(target_shape, fill_item.text)
```

**关键设计决策**：

| 决策 | 说明 |
|------|------|
| 深拷贝整个容器组 | 保留所有样式（圆角、填充色、阴影等），不只是复制文本框 |
| 先清除再填充 | 避免模板占位文字残留 |
| 偏移由 LLM 计算 | LLM 在 `layout_plan.json` 中根据原组间距计算 `offset_emu` |
| 仅克隆最后一个组件 | 简化实现，避免复杂的网格重排 |

> **注意**: `clone_group` 行动**排在 fill_text/fill_bullet_list/fill_table/fill_image 之后**执行，确保先填充已有组件再扩展新组件。

---

## 五、用户可见流水线（5 阶段）

| 阶段 | 用户可见 | 确认点 | 内部子步骤 |
|------|---------|--------|-----------|
| **Phase 1**: 模板分析 | 自动 | — | template_parser.py → LLM 生成 design_language.md |
| **Phase 2**: 大纲生成 | ✅ 展示大纲 | 🔔 1/3 | LLM 生成 outline.json |
| **Phase 3**: 详情+布局 | ✅ 展示详情+布局 | 🔔 2/3 | LLM 生成 detail_plan.json + layout_plan.json |
| **Phase 4**: 预览+微调 | ✅ 预览图片 | 🔔 3/3 | SVG→PNG + 逐页对话式微调 |
| **Phase 5**: 合成输出 | 自动 | — | v1 克隆/v2 SVG 引擎 + 质量检查 + 后处理 |

**3 确认点**（精简自 v2.0 的 5+ 确认点）:
1. 大纲确认 → 用户确认页面结构和主题分布
2. 详情+布局确认 → 用户确认每页内容和布局规划
3. 预览确认 → 用户确认视觉效果，可选微调

**自动映射**（替代 v2.0 手动风格映射 Phase 4）:
- 封面 → template[0], 目录 → template[1], 内容 → template[2..N-1], 结尾 → template[N]
- 由 LLM 在 layout_plan.json 中自动完成

---

## 六、关键数据结构

### 6.1 `style_profile.json` (Phase 1 输出)

```json
{
  "colors": {
    "primary": "#0070C0",
    "secondary": "#C00000",
    "body": "#333333",
    "background": "#FFFFFF"
  },
  "fonts": {
    "title": {"family": "微软雅黑", "size_pt": 40, "bold": true},
    "subtitle": {"family": "微软雅黑", "size_pt": 20, "bold": false},
    "body": {"family": "微软雅黑", "size_pt": 18, "bold": false}
  },
  "slide_types": {
    "0": "cover",
    "1": "toc",
    "2": "content",
    "7": "ending"
  }
}
```

### 6.2 `layout_plan.json` (Phase 3 输出 — 驱动 v1 克隆引擎)

```json
{
  "engine": "clone",
  "template_convention": {
    "cover": 0, "toc": 1, "content_range": [2, 6], "ending": 7
  },
  "slides": [
    {
      "template_slide": 0,
      "plan_slide": 0,
      "layout_strategy": "cover",
      "blocks": [
        {
          "target_shape_idx": 0,
          "content_key": "title",
          "action": "fill_text"
        },
        {
          "target_shape_idx": 1,
          "content_key": "subtitle",
          "action": "fill_text"
        }
      ]
    }
  ],
  "overflow_rules": {
    "max_bullets_per_page": 6,
    "max_title_chars": 30,
    "on_overflow": "split_or_shrink"
  }
}
```

### 6.3 `detail_plan.json` (Phase 3 输出 — 内容数据)

```json
{
  "slides": [
    {
      "slide_number": 1,
      "slide_type": "cover",
      "content_blocks": [
        {"type": "title", "text": "AI赋能企业数字化转型"},
        {"type": "subtitle", "text": "2026年度战略规划"}
      ]
    },
    {
      "slide_number": 3,
      "slide_type": "content",
      "content_blocks": [
        {"type": "title", "text": "项目背景与挑战"},
        {
          "type": "bullet_list",
          "items": [
            "传统业务流程效率低下，人工成本高",
            "数据孤岛严重，决策缺乏全局视角"
          ]
        }
      ]
    }
  ]
}
```

---


---

## 七、实施路线

### 7.0 P0 母版修复 🔥 新增（v2.3 融合）

| 任务 | 工时 | 优先级 |
|------|------|--------|
| 修改 `compose_with_layout_plan()` 使用 shutil.copy 方案 | 0.3h | P0 |
| 修改 `compose()` 同样使用 shutil.copy 方案 | 0.1h | P0 |
| 验证 3+ slide master 的模板生成结果母版数量正确 | 0.1h | P0 |

> **小计: ~0.5h** — 先行修复，不依赖其他改动

### 阶段 1: AI 模板分析 + v1 克隆引擎增强 🔴 ~18h

| 任务 | 工时 |
|------|------|
| 🔥 新增 `detect_template_patterns()` 空间分组（容器检测、重复模式、层级关系、装饰元素） | 3h |
| 改造 `template_parser.py` 输出增强 structure.json（含 components、layout_zones、decorations） | 1h |
| 新增 `prompts/template_analysis_deep.md` Prompt（含空间分组数据的利用说明） | 2h |
| 新增 `prompts/layout_planning.md` Prompt（含 clone_group 行动说明和容器感知匹配） | 2h |
| 改造 `ppt_compositor.py` 支持 `layout_plan.json` 驱动 | 4h |
| 🔥 新增 `expand_container_group()` clone_group 行动实现 | 2h |
| 新增 `design_language.md` 解析器 | 1h |
| E2E 测试（3 种模板 × 2 种内容类型，含容器扩展场景） | 3h |

> **小计: ~18h**（v2.2 原 12h + v2.3 新增 6h）

### 阶段 2: 预览管道 🟡 ~6h

| 任务 | 工时 |
|------|------|
| SVG → PNG 渲染（cairosvg / LibreOffice headless） | 3h |
| 逐页微调对话界面 | 3h |

> **小计: ~6h**（不变）

### 阶段 3: 溢出处理与质量门 🟡 ~7h

| 任务 | 工时 |
|------|------|
| 内容溢出自动检测 + 分页/缩字/改写策略 | 3h |
| 质量门（颜色一致性/字号继承/图片分辨率） | 2h |
| 🔥 母版一致性质量门（验证 slide master 数量、主题色继承） | 1h |
| 集成测试 | 1h |

> **小计: ~7h**（v2.2 原 6h + v2.3 新增质量门 1h）

### 总计: ~31.5h → **~27.5h（~4 工作日）**

> v2.2 原估算: ~24h（~3.5 工作日）  
> v2.3 新增: ~7.5h（母版修复 0.5h + 空间分组 3h + clone_group 2h + prompt 更新 1h + 质量门 1h）  
> 但母版修复可与空间分组/clone_group 并行开发，实际关键路径约 27.5h

---

## 八、当前状态与下一步

### 8.1 已完成

| 模块 | 状态 | 测试 |
|------|------|------|
| `template_parser.py` | ✅ | 70 tests |
| `ppt_compositor.py` (v1 克隆引擎) | ✅ | 33 tests |
| `prompts/template_analysis_deep.md` Prompt | ✅ | — |
| `prompts/layout_planning.md` Prompt | ✅ | — |
| `design_language.md` 解析器 | ✅ | — |
| `FINAL-DESIGN.md` v2.3 融合设计 | ✅ | — |

### 8.2 待实施

| 任务 | 优先级 | 工时 | 状态 |
|------|--------|------|------|
| 🔥 P0 母版保留修复（shutil.copy 方案） | P0 | 0.5h | ⬜ 待实施 |
| 🔥 `detect_template_patterns()` 空间分组 | P1 | 3h | ⬜ 待实施 |
| 🔥 增强 structure.json 输出（components/layout_zones/decorations） | P1 | 1h | ⬜ 待实施 |
| 改造 `ppt_compositor.py` 支持 `layout_plan.json` 驱动 | P1 | 4h | ⬜ 待实施 |
| 🔥 新增 `expand_container_group()` clone_group 行动 | P1 | 2h | ⬜ 待实施 |
| 更新 `layout_planning.md` prompt（容器感知 + clone_group） | P1 | 1h | ⬜ 待实施 |
| SVG → PNG 预览渲染 | P2 | 3h | ⬜ 待实施 |
| 逐页微调对话界面 | P2 | 3h | ⬜ 待实施 |
| 内容溢出自动检测 + 策略 | P2 | 3h | ⬜ 待实施 |
| 质量门（颜色/字号/母版一致性） | P2 | 3h | ⬜ 待实施 |
| E2E 测试（含容器扩展场景） | P1 | 3h | ⬜ 待实施 |

> **注意**: 空间分组（`detect_template_patterns`）和 clone_group（`expand_container_group`）是 v2.3 融合新增的核心增强，目前均待实施。P0 母版修复是先行依赖，需最先完成。

### 8.3 立即下一步

1. **P0 母版修复**: 修改 `compose_with_layout_plan()` 和 `compose()`，使用 shutil.copy 方案（0.5h）
2. **空间分组实现**: 新增 `detect_template_patterns()` 到 `template_parser.py`（3h）
3. **clone_group 引擎支持**: 新增 `expand_container_group()` 到 `ppt_compositor.py`（2h）
4. **Prompt 更新**: 更新 `layout_planning.md` prompt 以利用增强结构数据（1h）
5. **集成测试**: 用含 card 组件的模板测试完整链路（3h）

---

## 附录: 相关文档索引

| 文档 | 路径 | 说明 |
|------|------|------|
| v2.2 深度改进方案 | `ppt-agent-v2.2-deep-improvement-plan.md` | 三层升级详细分析 + v1 落地 |
| v2.1 融合设计 | `ppt-agent-v2-fusion-design.md` | 5 阶段流水线详细设计 |
| v2.1 需求缺口修复 | `ppt-agent-v2.1-design-gap-fix.md` | 用户需求 vs 设计差距分析 |
| v1.0 原始设计 | `design.md` | 6 阶段 python-pptx 管线 |
| ppt-master 能力分析 | `ppt-master-analysis.md` | ppt-master 架构深度分析 |
| GSD 实施计划 v2 | `gsd-plan-v2.md` | 80 任务分解 + 测试矩阵 |
| SKILL.md | `../../skills/ppt-agent/SKILL.md` | Agent 执行工作流技能定义 |
