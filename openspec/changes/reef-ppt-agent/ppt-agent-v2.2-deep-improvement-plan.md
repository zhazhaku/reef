# Reef PPT Agent v2.2 — 深度改进方案：AI 参与模板理解与布局决策

> 日期: 2026-05-20 | 修订: 2026-05-20 (增加 v1 克隆引擎落地路径)
> 设计师: Hermes Coordinator
> 状态: 待审阅
> 核心理念: **AI 大模型必须参与模板理解和内容布局，而非仅做文本生成**
> 
> **关键发现**: 你此前指出的 PPT 生成质量问题，根因不是"技术 bug"，
> 而是架构层面 AI 没有参与模板理解和内容布局——这一诊断完全正确。
> v1 克隆引擎（XML deep copy）反而是落地 AI 前置分析层的**最佳载体**。

---

## 一、问题根因诊断

### 1.1 当前架构的问题

```
用户模板.pptx
    ↓
template_parser.py (机械提取: hex颜色, 字体名, 字号, 形状类型)
    ↓
ppt_compositor.py (机械操作: _auto_match_shape → fill_text_block → 输出)
    ↓
最终 PPTX: 文字进了模板，但毫无设计感
```

**核心缺陷：AI 大模型在整个链条中只参与了大纲和内容生成，完全没有参与"模板理解"和"内容布局适配"。**

### 1.2 ppt-master 怎么做的（对比）

```
用户提供素材 + 模板选择
    ↓
Strategist (LLM 角色): 深度分析内容 + 模板风格 → 生成设计规格书
    ├── 理解模板的视觉 DNA: 为什么标题在那里、正文用这个字号
    ├── 制定设计策略: 配色方案、排版节奏、图表风格、图片调性
    └── 输出 spec_lock.md (8 大确认项)
    ↓
Executor (LLM 角色): 逐页手写 SVG
    ├── 读取 spec_lock.md 锁定的设计参数
    ├── 读取模板 SVG 文件，继承背景/装饰元素/布局框架
    ├── 在模板框架内自由排版内容（标题、正文、图表、图片）
    └── 输出每页 SVG（AI 真正理解了设计意图后的产物）
    ↓
svg_to_pptx: SVG → 原生可编辑 PPTX
```

**关键差异**:
| 维度 | 我们的做法 | ppt-master |
|------|-----------|------------|
| 模板理解 | 机械提取 hex/font/size | LLM 深度分析设计语言 |
| 内容布局 | _auto_match_shape (找 placeholder idx=0) | LLM 理解模板框架后自由排版 |
| 生成方式 | python-pptx 填充已有形状 | AI 手写 SVG 再转 PPTX |
| 设计一致性 | 无 spec_lock | spec_lock.md 约束所有页面 |
| AI 价值 | 只在大纲/内容生成 | 全程参与设计决策 |

---

## 二、改进方案: 三层升级

### 第一层: 模板深度分析 (Phase 1 增强) 🔴 CRITICAL

**当前**: `template_parser.py` 机械提取 hex/font/size  → 输出 `structure.json`

**改进**: 增加 **LLM 模板分析环节**，生成 `design_language.md`:

```
现有: Phase 1 → template_parser.py → structure.json (机械数据)
新增: Phase 1 → template_parser.py → structure.json
            → LLM 模板分析 Prompt → design_language.md (设计理解)
```

**`design_language.md` 的内容** (LLM 生成):

```markdown
# 模板设计语言分析

## 视觉 DNA
- **风格**: 科技蓝商务风，扁平化设计
- **配色逻辑**: 深蓝(#0070C0)用于标题强调，浅灰(#F2F2F2)大面积背景，
  红色(#C00000)用于重点标注
- **字体层次**: 封面标题 40pt 微软雅黑 bold → 章节标题 36pt → 
  正文 18pt → 注释 12pt

## 布局模式
- **封面页**: 标题居中偏上(30%高度)，副标题在下方 20pt，底部有装饰色块
- **内容页**: 顶部蓝色标题栏(高 80px)，左侧竖线装饰，
  正文区域高度约 400px，最多容纳 6 个要点
- **过渡页**: 大号章节号 + 章节标题居中，纯色背景

## 元素语义
- 蓝色横条 → 标题背景，每个内容页必出现
- 左侧竖线 → 视觉引导线，间距标题 20px
- 右下角 logo → 装饰元素，保持原样

## 内容适配规则
- 内容页正文超过 6 行 → 拆分为两页
- 标题超过 20 字 → 缩小到 32pt，换行处理
- 有数据 → 优先使用含表格/图表的模板页
```

**实现方式**: 新增 Prompt 文件 `prompts/template_analysis_deep.md`，在 Phase 1 末尾调用 LLM。

---

### 第二层: 内容-模板智能匹配 (Phase 3 增强) 🔴 CRITICAL

**当前**: `_auto_match_shape` 按 placeholder idx + 字号启发式匹配

**改进**: LLM 根据"详细内容"和"模板设计语言"，**决定每页内容用哪个模板页、如何在模板内布局**

```
Phase 3 (增强后):
  输入: detail_plan + design_language.md + structure.json
    ↓
  LLM 内容布局规划 → layout_plan.json
    ├── 每页选择哪个模板 slide
    ├── 每个 content_block 映射到模板的哪个占位符/区域
    ├── 内容溢出时如何调整(分页/缩字号/重新措辞)
    └── 装饰元素的保留/修改策略
```

**`layout_plan.json` 示例**:

```json
{
  "slides": [
    {
      "plan_slide": 1,
      "template_slide": 0,
      "slide_type": "cover",
      "content_placement": {
        "title": {
          "shape": "标题 1",
          "reason": "封面标题占位符，40pt bold，位于幻灯片上方 30% 处"
        },
        "subtitle": {
          "shape": "副标题 2", 
          "reason": "封面副标题占位符，20pt，位于标题下方"
        }
      },
      "layout_notes": "保持封面原有布局，替换文字即可"
    },
    {
      "plan_slide": 3,
      "template_slide": 2,
      "slide_type": "content",
      "content_placement": {
        "title": {
          "shape": "标题 1",
          "reason": "蓝色标题栏，36pt bold，这是内容页的标准标题位"
        },
        "bullet_list": {
          "shape": "内容占位符 2",
          "reason": "正文区域，18pt，最多容纳 6 个要点。当前有 4 个要点，空间充足"
        }
      },
      "layout_notes": "要点清晰，空间舒适，无需调整"
    }
  ]
}
```

---

### 第三层: AI SVG 生成 (Phase 5 增强) 🟡 HIGH

这是参考 ppt-master 最核心的能力——**让 LLM 手写 SVG**。

**当前 v2.0 SVG 管道问题**: `pptx_to_svg` 转换自定义模板 → AI 编辑 SVG → `svg_to_pptx` 转回 → **格式大量丢失**（字号坍缩、形状类型丢失、主题链断裂）。

**改进方案**: 分两种场景:

#### 场景 A: 自定义模板（用户上传 .pptx）→ 走克隆引擎

```
自定义模板.pptx
    ↓
Phase 1B: pptx_to_svg.py (桥接)
    ↓
Phase 3: LLM 生成 layout_plan.json (内容-模板匹配)
    ↓
Phase 5: 增强版 ppt_compositor.py
    ├── 读取 layout_plan.json (AI 决定的布局方案)
    ├── 克隆指定模板 slide
    ├── 按 AI 规划的形状映射填充内容
    ├── 处理溢出: 分页/调整字号/改写文本
    └── 输出 PPTX
```

**增强点**: `ppt_compositor.py` 不再自己做 `_auto_match_shape`，而是完全按 LLM 生产的 `layout_plan.json` 来操作。AI决定了"这个标题放进这个形状"，代码只负责执行。

#### 场景 B: 内置风格（5 种主题）→ 走 AI SVG 生成

```
内置风格选择 (如: 科技蓝)
    ↓
Phase 3: LLM 生成 detail_plan + spec_lock
    ↓
Phase 5: LLM 手写 SVG (类似 ppt-master Executor)
    ├── 读取风格模板 SVG 文件 (templates/svg_pages/tech/01_cover.svg 等)
    ├── 理解模板的装饰元素和布局框架
    ├── 在框架内自由排版内容
    ├── 每页生成独立 SVG
    └── 质量检查 (color_consistency_checker, svg_text_overflow_detector 等)
    ↓
svg_export/svg2pptx.py: SVG → 原生 PPTX
```

---

## 二-B: 落地引擎选择 — v1 克隆引擎是 AI 前置分析层的最佳载体

### 为什么 v1 更适合落地 AI 前置分析

```
v1 克隆引擎（XML deep copy）
  ├─ 优势：完美保留模板样式（主题继承、PLACEHOLDER、字号/颜色/字体）
  └─ 缺陷：_auto_match_shape() 是机械规则，不理解模板设计意图

v2 SVG 引擎
  ├─ 优势：AI 可以直接编辑 SVG
  └─ 缺陷：SVG↔PPTX 往返丢失主题继承链、PLACEHOLDER→TEXT_BOX
```

**v1 的缺陷不是引擎本身，而是缺少一个 AI 前置分析层。**

### 改进架构：AI 分析 + v1 克隆执行

```
用户模板 .pptx
      │
      ▼
┌─────────────────────────────────┐
│  Phase 1: AI 模板深度分析        │  ← 新增（二层升级第 1 层）
│  ─────────────────────────────  │
│  LLM 读取 template_parser 输出   │
│  生成 design_language.md:        │
│    • 每个 slide 的视觉分区        │
│    • 形状的语义角色               │
│    • 内容适配规则                 │
│    • 布局弹性约束                 │
└──────────────┬──────────────────┘
               │
               ▼
┌─────────────────────────────────┐
│  Phase 3: AI 内容-布局智能匹配    │  ← 新增（二层升级第 2 层）
│  ─────────────────────────────  │
│  LLM 基于 design_language.md     │
│  生成 layout_plan.json:          │
│    • 每个 content_block → 哪个形状│
│    • 溢出处理策略                 │
│    • 形状装饰/强调建议             │
└──────────────┬──────────────────┘
               │
               ▼
┌─────────────────────────────────┐
│  Phase 5: v1 克隆引擎执行        │  ← 保持不变
│  ─────────────────────────────  │
│  ppt_compositor.compose()        │
│  XML deep copy + 按 layout_plan  │
│  精确填入内容                     │
│  → 完美保留模板样式               │
└─────────────────────────────────┘
```

**关键点**：AI 负责"理解模板"和"决定什么内容放哪里"，v1 克隆引擎负责"执行填入并保留样式"。分工清晰，各自做擅长的事。

### 与三层升级方案的关系

| 升级层 | 在 v1 上的实现 | 说明 |
|---------|---------------|------|
| 1. AI 模板深度分析 | `design_language.md` 由 LLM 生成 | 完全复用 |
| 2. 内容-布局智能匹配 | `layout_plan.json` 替代 `style_mapping.json` | 从机械规则升级为 AI 决策 |
| 3. AI 生成 | **不需要**（v1 克隆引擎替代） | v1 天然规避 SVG 往返丢失问题 |

v2.2 的第三层"AI 手写 SVG"是 v2 引擎为解决模板保真度而引入的——但 v1 克隆引擎本身就保真度 100%，**所以根本不需要这一层**。

### v1 vs v2 引擎场景分工

| 场景 | 推荐引擎 | 理由 |
|------|---------|------|
| 用户上传自定义模板 | **v1 克隆引擎** + AI 前置分析 | XML deep copy 100% 保留样式 |
| 用户选择内置风格 | v2 SVG 引擎 | 无模板保真度要求，AI 手写 SVG 更灵活 |
| 混合：自定义模板 + 复杂图表 | v1 克隆引擎 + 图片嵌入 | 模板主体克隆，图表图片嵌入 |

---

## 三、具体实施计划

### Phase 1 改造: 模板深度分析

| 任务 | 内容 | 工时 |
|------|------|------|
| 1.1 | 创建 `prompts/template_analysis_deep.md` — LLM 分析模板设计语言 | 1h |
| 1.2 | 修改 `ppt_workflow.go` Phase 1 → 增加 LLM 调用生成 `design_language.md` | 2h |
| 1.3 | 测试: 用 3 种不同风格模板验证分析结果 | 1h |

### Phase 2 改造: 内容-模板智能匹配

| 任务 | 内容 | 工时 |
|------|------|------|
| 2.1 | 创建 `prompts/layout_planning.md` — LLM 规划每页内容布局 | 1h |
| 2.2 | 修改 Phase 3 流程 → 生成 `layout_plan.json` (含 shape 映射+布局说明) | 2h |
| 2.3 | 修改 `ppt_compositor.py` → 接受 `layout_plan.json` 驱动而非自主匹配 | 3h |
| 2.4 | 测试: 短内容/长内容/数据密集型 3 种场景验证 | 2h |

### Phase 3 改造: AI SVG 生成 (内置风格)

| 任务 | 内容 | 工时 |
|------|------|------|
| 3.1 | 创建 `prompts/executor_svg.md` — LLM 手写 SVG 的详细指南 | 2h |
| 3.2 | 集成 Phase 5 → LLM 逐页生成 SVG (继承模板框架) | 3h |
| 3.3 | SVG 生成后的质量检查流水线集成 | 1h |
| 3.4 | E2E 测试: 5 种内置风格 × 3 种内容类型 = 15 个 PPT | 3h |

### Phase 4 改造: 克隆引擎增强 (自定义模板)

| 任务 | 内容 | 工时 |
|------|------|------|
| 4.1 | `ppt_compositor.py` 改为完全由 `layout_plan.json` 驱动 | 3h |
| 4.2 | 增强主题字体继承 (已完成 ✅) | — |
| 4.3 | 内容溢出处理: 自动分页 + 字号微调 + 文本改写 | 3h |
| 4.4 | E2E 测试: 8 页/20 页/数据密集型 3 种模板 | 3h |

### 总工时估算: ~30h (~4 工作日)

---

## 四、预期效果

| 改进维度 | 改进前 | 改进后 |
|---------|--------|--------|
| **模板理解** | 机械提取 hex/font/size | LLM 深度分析设计语言，输出 design_language.md |
| **内容布局** | `_auto_match_shape` 按 idx 启发式 | LLM 规划 `layout_plan.json`，AI 决定匹配策略 |
| **字体样式** | 主题继承时有丢失 | 完全保留 (已修复) + AI 知道何时调整 |
| **位置准确性** | 依赖模板原有位置 | LLM 理解位置语义后精准映射 |
| **元素保留** | 全部保留或全部清除 | AI 判断哪些装饰保留、哪些需要修改 |
| **内容适配** | 无溢出处理 | 自动分页/调字号/重措辞 |
| **内置风格** | 无 AI 生成能力 | LLM 手写 SVG → 原生 PPTX |
| **设计一致性** | 无跨页约束 | spec_lock.md + layout_plan.json |

---

## 五、与 ppt-master 的对齐

| ppt-master 能力 | 我们如何实现 | 优先级 |
|----------------|-------------|--------|
| Strategist 设计锁 | `design_language.md` + `spec_lock.md` (精简为 3 项) | P0 |
| AI 理解模板布局 | LLM 生成 `layout_plan.json` | P0 |
| AI 手写 SVG | 内置风格场景 LLM 生成 SVG | P1 |
| 模板继承+自由排版 | `layout_plan.json` 驱动克隆引擎 | P0 |
| 质量检查 | 已有: text_overflow_detector 等 4 个 checker | ✅ |
| 多格式素材预处理 | 已有: source_to_md (5 种格式) | ✅ |

---

## 六、核心原则

1. **AI 决定"做什么"** — LLM 理解模板、规划布局、做出设计决策
2. **代码只负责"执行"** — python-pptx/svg_to_pptx 按 AI 的规划精确操作
3. **模板为尊** — AI 理解模板的设计意图后，在模板框架内创作，而非覆盖
4. **3 确认点不变** — 大纲 → 详情+布局 → 预览，流程简洁
5. **双引擎保留** — 自定义模板走克隆引擎 (精确保真)，内置风格走 SVG 引擎 (AI 创作)
