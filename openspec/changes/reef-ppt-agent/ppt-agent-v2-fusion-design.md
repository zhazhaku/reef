# Reef PPT Agent v2.1 — 统一 SVG 引擎 (需求对齐版)

> 设计日期: 2026-05-17 | 修订: 2026-05-19 (对齐用户原始需求)
> 设计师: Hermes Coordinator
> 状态: 待审阅
> 基于: reef-ppt-agent (v1.0 MVP) + ppt-master (hugohe3/ppt-master, 17k stars)
> 修订记录: v2.0 (双引擎→统一SVG) → v2.1 (10+阶段→5阶段, 3确认点, 自动风格映射, 删除Strategist设计锁)

---

## 1. 融合目标

将 ppt-master 的 SVG 引擎能力融入 reef-ppt-agent，形成**用户可见 5 阶段流水线**（内部含 10+ 子步骤）：

**核心原则**（v2.1 修正）：
- 用户只需 **3 次确认**：大纲 → 详情+布局 → 预览
- 模板风格（配色/字体）**自动提取**，不作为独立确认步骤
- 内容→模板映射**自动完成**（封面→封面, 目录→目录, 内容→最优内容页）
- 预览在详情确认**之后**提供（文字摘要 + 图片预览）
- 内部仍走统一 SVG 引擎（pptx_to_svg 桥接 → AI 编辑 → svg2pptx 原生输出）

**用户可见 5 阶段**：
```
Phase 1: 模板分析 ── 提取配色+字体+页类型+槽位约束
Phase 2: 大纲生成 ── LLM 按输入源自定大纲 → 🔔确认
Phase 3: 详情+布局 ── LLM 展开每页内容+布局 → 🔔确认
Phase 4: 预览 ────── 文字摘要 → 图片预览 → 🔔确认 (可微调)
Phase 5: 合成输出 ── 自动映射→SVG桥接→注入→svg2pptx
```

### 1.1 融合前 vs 融合后

```
融合前 (v1.0 MVP)                    融合后 (v2.0 Full, 统一 SVG)
─────────────────────                ──────────────────────────────
用户文本描述                           多源素材 (PDF/DOCX/URL/Excel/文本)
    │                                      │
    ▼                                      ▼
[跳过]                                Phase 0: 素材预处理 (NEW)
                                          source_to_md → Markdown
    │                                      │
    ▼                                      ▼
用户上传模板.pptx                     Phase A: 风格确认 (NEW)
    │                                 ├── 内置风格 (ppt-master 模板库)
    ▼                                 └── 自定义模板 (用户上传 .pptx)
Phase 1: 模板解析                          │
    │                                      ▼
    ▼                                 Phase 1: 模板解析 (保留)
Phase 2: 大纲生成                          │
Phase 3: 内容编排                          ▼
Phase 4: 风格映射                     Phase 1B: 自定义模板桥接 (NEW)
Phase 5: 逐页微调                          pptx_to_svg → 8 个 shape-aware SVG
Phase 6: PPT 合成                          │
    python-pptx 克隆                       ▼
                                    Phase 2: 大纲生成 (保留)
                                    Phase 3: 内容编排 (保留)
                                    Phase 4: 风格映射 (保留)
                                    Phase 5: 逐页微调 (保留)
                                         │
                                         ▼
                                    Phase 6: 统一 SVG 编辑 → PPTX 合成 (大改)
                                         │
                                    ┌──── 所有路径汇入 SVG 引擎 ────┐
                                    │                                 │
                                    │  LLM 直接编辑 SVG 文件          │
                                    │  (文字/颜色/布局/图表/图片)      │
                                    │  spec_lock.md 保证跨页一致性     │
                                    │                                 │
                                    └────────────┬────────────────────┘
                                                 ▼
                                         svg_to_pptx (ppt-master native)
                                         └── 输出原生可编辑 .pptx
                                                 │
                                                 ▼
                                    Phase 7: 质量检查 (NEW)
                                         ├── 文本溢出检查
                                         └── 一致性检查
                                    Phase 8: 后处理 (NEW)
                                         ├── 动画注入
                                         └── 旁白/TTS (可选)
```

### 1.2 双路径架构 (v2.2 核心设计 — 2026-05-20 修订)

**原则: 两条路径，按模板来源自动路由，AI 全程参与设计决策。**

**关键发现**（2026-05-20 确认）: 在此之前 PPT 生成质量差的**根因不是技术 bug，而是架构层面 AI 大模型没有参与模板理解和内容布局**。v1 克隆引擎（XML deep copy）反而是落地 AI 前置分析层的**最佳载体**——它完美保留模板样式，只需增加 AI 模板理解和布局决策两个前置环节即可。

```
                    用户提供模板 .pptx
                          │
                          ▼
                ┌─────────────────────┐
                │  Phase 1: AI 模板深度分析 │  ← 新增核心环节
                │  ─────────────────── │
                │  LLM 读取 template_parser │
                │  输出 → design_language.md │
                │  (视觉DNA/布局模式/元素语义) │
                └──────────┬──────────┘
                           │
              ┌────────────┴────────────┐
              ▼                         ▼
    入口 A: 自定义模板              入口 B: 内置风格
    用户上传 .pptx                 用户从 5 种风格选择
              │                         │
              ▼                         ▼
    ┌────────────────────┐    ┌────────────────────┐
    │ v1 克隆引擎 (推荐)    │    │ v2 SVG 引擎         │
    │ ────────────────── │    │ ────────────────── │
    │ AI 布局决策          │    │ LLM 手写 SVG        │
    │  ↓ layout_plan.json │    │  ↓ 继承布局模板框架  │
    │ ppt_compositor.py   │    │ svg2pptx 原生导出    │
    │ XML deep copy       │    │                     │
    │ → 完美保留模板样式    │    │ → 灵活创作           │
    └────────────────────┘    └────────────────────┘
              │                         │
              └────────────┬────────────┘
                           ▼
                  最终 output.pptx
```

**路由规则**:
| 条件 | 引擎 | 理由 |
|------|------|------|
| 用户上传自定义 .pptx 模板 | **v1 克隆引擎** + AI 前置分析 | XML deep copy 100% 保留样式 |
| 用户选择内置风格（无模板） | v2 SVG 引擎 | 无模板保真度要求，AI 手写更灵活 |

**AI 参与设计决策的三个关键点**:
1. **Phase 1**: LLM 分析模板结构 → `design_language.md`（理解视觉 DNA）
2. **Phase 3**: LLM 规划内容布局 → `layout_plan.json`（精确到每个形状的语义匹配）
3. **Phase 4**: LLM 审核预览 → 逐页微调（保持设计一致性）

**python-pptx 角色**:
  ✅ Phase 1 模板解析 + AI 分析输入源
  ✅ Phase 5 v1 克隆引擎执行（自定义模板路径）
  ❌ 不再用于独立合成——必须有 AI 前置分析

> 详细分析见: `ppt-agent-v2.2-deep-improvement-plan.md` §二-B"落地引擎选择"

---

## 2. 用户可见 5 阶段流水线

> v2.1 修正: 从 10+ 内部阶段压缩为 5 个用户可见阶段，3 确认点。
> 内部细节（素材预处理/桥接/质量检查/后处理）作为子步骤嵌入。

---

### Phase 1: 模板分析 (Template Analysis)

**输入**: 用户上传的 `.pptx` 模板文件
**输出**: `style_profile` (配色+字体+页类型+槽位约束)
**工具**: `template_parser.py` + `pptx_to_svg.py` (桥接)

核心产物 `style_profile`:
```json
{
  "colors": {"primary": "#1A56DB", "secondary": "#666666", "text": "#333333", "background": "#FFFFFF"},
  "fonts": {"title": {"family": "Microsoft YaHei", "size_pt": 44, "bold": true},
            "subtitle": {"family": "Microsoft YaHei", "size_pt": 24},
            "body": {"family": "Microsoft YaHei", "size_pt": 18}},
  "slide_types": {"cover": [0], "toc": [1], "content": [2,3,4,5,6], "ending": [7]},
  "slide_slots": [{"slide": 0, "slots": [{"type": "title", "font_size_pt": 44, "max_chars": 20}]}]
}
```

**约定**:
- Slide 0 = 封面模板，Slide 1 = 目录模板
- Slides 2..N-1 = 内容模板（可重复使用）
- Slide N = 结尾模板

**内部子步骤**:
- Phase 1B: `pptx_to_svg` 桥接（将自定义模板转为 shape-aware SVG，保留 `data-shape-type`/`data-placeholder` 属性）
- 保留 python-pptx 克隆引擎作为 v1.0 fallback

---

### Phase 2: 大纲生成 (Outline Generation)

> 🔔 **确认点 1/3**

**输入**: 用户输入源 + `style_profile`
**输出**: `outline` (LLM 生成，用户确认)
**工具**: `prompts/outline_prompt.md` + LLM

LLM 根据输入源内容**自动决定页数和结构**:
```
封面 → 目录1 → 内容1 → 内容2 → 目录2 → 内容3 → 内容4 → ... → 结尾
```

- 页数**不限制**，由内容需求驱动
- 支持多目录分组（目录1, 目录2...）
- 大纲包含每页类型 + 标题 + 3-5 条要点摘要
- 用户确认后进入详情阶段

**内部子步骤**:
- Phase 0 (可选): 多格式素材预处理 (PDF/DOCX/URL/Excel→Markdown via `source_to_content.sh`)
- Phase A (可选): 内置风格选择 (5 种: academic/business/tech/creative/minimal, 无模板时使用)

---

### Phase 3: 详情+布局 (Detail & Layout)

> 🔔 **确认点 2/3**

**输入**: 确认后的大纲 + `style_profile`
**输出**: `detail_plan.json` (每页具体内容+槽位分配, 用户确认)
**工具**: `prompts/detail_prompt.md` + LLM (模板感知模式)

每页生成:
- 内容块类型 (title/subtitle/text/bullet_list/table/image)
- 具体文案 (受模板字号约束: 封面标题≤20字, 正文≤200字等)
- 分配到模板槽位 (title→标题区, subtitle→副标题区, body→正文区)
- 样式 overrides: **仅当用户明确指定时**; 未指定则 100% 继承模板

**自动风格映射** (v2.1 核心变更):
- 大纲封面 → 自动匹配模板 Slide 0 (封面页)
- 大纲目录 → 自动匹配模板 Slide 1 (目录页)
- 大纲内容页 → 自动匹配模板 Slides 2..N-1 中**内容最匹配**的页
- 大纲结尾 → 自动匹配模板 Slide N (结尾页)
- **无需用户手动指定每页映射** (删除旧 Phase 4 手动风格映射)

---

### Phase 4: 预览 (Preview)

> 🔔 **确认点 3/3**

**输入**: 确认后的 `detail_plan.json`
**输出**: 文字摘要 + 图片预览 (用户确认, 可选微调)

**两段预览**:
1. **文字摘要**: 每页标题 + 前 3 条要点 + 字号信息 (Phase 3 确认后立即提供)
2. **图片预览**: SVG → PNG 渲染 (cairosvg 或 LibreOffice headless)，用户最终确认

**可选微调** (v2.1 保留，但不强制):
- 预览后用户可指定特定页调整: "第3页正文太长，精简到100字" / "第5页标题字号调大"
- 调整 → 重新预览 → 确认 循环 (单页粒度)
- 所有调整写入 `detail_plan_v2.json` overrides

---

### Phase 5: 合成输出 (Compose)

**输入**: 最终确认的 `detail_plan.json` + 模板 SVG 文件
**输出**: 最终 `.pptx` 文件
**工具**: `edit_and_compose.py` → `svg2pptx.py`

**自动执行** (无需确认):
1. 大纲骨架 → 模板页自动映射
2. SVG 内容注入 (XPath, 保留模板字体/颜色样式)
3. svg2pptx 原生 DrawingML 合成
4. 质量检查 (文字溢出/颜色一致性/图片分辨率/结构完整性)

**内部子步骤**:
- Phase 7: 质量检查 (自动, 阻塞仅 ERROR 级别)
- Phase 8: 后处理 (动画注入 + 旁白 + TTS, 可选)

---

### 确认点对比: v2.0 vs v2.1

| | v2.0 | v2.1 |
|---|------|------|
| 确认点数量 | 5+ (大纲/详情/风格映射/8项设计锁/逐页微调) | **3** (大纲/详情+布局/预览) |
| 风格映射 | 用户手动指定每页映射 | **系统自动匹配** |
| Strategist 设计锁 | 8 项 BLOCKING 逐项确认 | **删除** (配色/字体从模板自动提取) |
| 预览 | 设计存在但未实现 | **文字摘要+图片预览** |
| 逐页微调 | 强制交互环节 | **可选** (预览后按需) |
## 3. 数据结构扩展 (v2.1)

v2.1 以 **3 个核心 JSON + 1 个预览层** 为数据骨架，其余文档为可选内部子步骤输出。

### 3.1 style_profile.json（Phase 1 输出 — 新增）

模板分析的核心产出，驱动后续所有阶段：

```json
{
  "slide_count": 8,
  "canvas": {"width": 1280, "height": 720},
  "colors": {
    "primary": "#1A56DB",
    "secondary": "#666666",
    "accent": "#C00000",
    "text": "#333333",
    "background": "#FFFFFF"
  },
  "fonts": {
    "title": {"family": "Microsoft YaHei", "size_pt": 44, "bold": true},
    "subtitle": {"family": "Microsoft YaHei", "size_pt": 24},
    "body": {"family": "Microsoft YaHei", "size_pt": 18},
    "notes": {"family": "Microsoft YaHei", "size_pt": 12}
  },
  "slide_types": {
    "cover": [0],
    "toc": [1],
    "content": [2, 3, 4, 5, 6],
    "ending": [7]
  },
  "slots": [
    {"slide": 0, "shape_type": "title", "placeholder_idx": 0, "font_size_pt": 44},
    {"slide": 0, "shape_type": "subtitle", "placeholder_idx": 1, "font_size_pt": 24},
    {"slide": 1, "shape_type": "body", "placeholder_idx": 0, "font_size_pt": 18}
  ]
}
```

**关键原则**：
- `slide_types` 约定：slide 0 = 封面，slide 1 = 目录，slide 2..N-1 = 内容页，slide N = 结尾
- `slots` 为每页每个可填充槽位提供类型+字号约束
- 配色+字体自动从模板提取，用户无需确认

### 3.2 outline.json（Phase 2 输出）

LLM 按输入源自定大纲，页数不设上限：

```json
{
  "pages": [
    {"type": "cover", "title": "河南移动流量经营四运营经验汇报"},
    {"type": "toc", "title": "目录", "items": ["项目背景", "精准营销", "智能网络", "用户感知", "价值效益"]},
    {"type": "content", "section": "项目背景", "title": "流量红利消退，经营模式转型"},
    {"type": "content", "section": "运营一", "title": "精准营销运营"},
    {"type": "content", "section": "运营四", "title": "价值效益运营"},
    {"type": "ending", "title": "谢谢"}
  ],
  "metadata": {"style_id": null, "template_path": "/path/to/template.pptx"}
}
```

### 3.3 detail_plan.json（Phase 3 输出 — 含自动风格映射）

每页详细内容+槽位布局，**风格映射自动完成**（封面→slide 0, 目录→slide 1, 内容→最优内容页, 结尾→最后页）：

```json
{
  "slides": [
    {
      "plan_slide": 0,
      "template_slide": 0,
      "slide_type": "cover",
      "content_blocks": [
        {"type": "title", "text": "河南移动流量经营四运营经验汇报", "font_size": 44},
        {"type": "subtitle", "text": "数智化部 2026年5月", "font_size": 24}
      ]
    },
    {
      "plan_slide": 2,
      "template_slide": 3,
      "slide_type": "content",
      "content_blocks": [
        {"type": "title", "text": "精准营销运营", "font_size": 36},
        {"type": "bullet_list", "items": ["千人千面推荐", "转化率提升28%", "ROI 3.2x"], "font_size": 18},
        {"type": "text", "content": "基于用户画像的实时推荐引擎...", "font_size": 14}
      ]
    }
  ]
}
```

### 3.4 内置风格 catalog（Phase 2 可选 — Phase A 子步骤）

5 种内置风格作为无模板场景 fallback，定义在 `style_catalog.yaml`：

| style_id | 名称 | 主色 | 适用场景 |
|----------|------|------|---------|
| academic | 学术答辩 | navy `#003366` | 论文答辩、课题汇报 |
| business | 商务汇报 | blue `#1A56DB` | 企业报告、项目汇报 |
| tech | 科技产品 | purple `#7C3AED` | 产品发布、技术分享 |
| creative | 创意设计 | red `#DC2626` | 营销方案、品牌策划 |
| minimal | 极简白底 | monochrome | 数据报告、内部总结 |

### 3.5 可选内部子步骤输出

以下为内部子步骤输出，用户不可见，仅供阶段间数据传递：

| 文件 | 来源 | 用途 |
|------|------|------|
| `material.md` | Phase 0 可选 | 多源素材→统一 MD，作为 Phase 2 输入 |
| `style_selection.json` | Phase A 可选 | 用户风格选择（内置/自定义） |
| `spec_lock.md` | Phase 5 内部 | 颜色/字体一致性约束（LLM 每页必读） |
| `quality_report.json` | Phase 5 内部 | 自动质量检查结果 |
| `animation_config.yaml` | Phase 5 内部 | 动画注入配置 |


---

## 4. 工作流状态机 (Go, v2.1 简化)

v2.1 简化为 **5 用户可见阶段 + DONE**，替代 v2.0 的 10+ 阶段模型。

```go
// ppt_workflow.go — v2.1 状态机 (5 阶段)

const (
    PhaseWaitTemplate PPTPhase = "WAIT_TEMPLATE" // 等待用户提供模板
    PhaseParse        PPTPhase = "PARSE"         // Phase 1: 模板解析 → style_profile.json
    PhaseOutline      PPTPhase = "OUTLINE"       // Phase 2: 大纲生成 → outline.json
    PhaseDetail       PPTPhase = "DETAIL"        // Phase 3: 详情+布局 → detail_plan.json
    PhasePreview      PPTPhase = "PREVIEW"       // Phase 4: 预览 (可用微调)
    PhaseCompose      PPTPhase = "COMPOSE"       // Phase 5: 合成输出 → output.pptx
    PhaseDone         PPTPhase = "DONE"
)

type PPTSession struct {
    SessionID     string           `json:"session_id"`
    Phase         PPTPhase         `json:"phase"`
    TemplatePath  string           `json:"template_path"`   // 用户自定义模板
    StyleProfile  string           `json:"style_profile"`   // style_profile.json 路径
    Outline       string           `json:"outline"`         // outline.json 路径
    DetailPlan    string           `json:"detail_plan"`     // detail_plan.json 路径
    OutputPPTX    string           `json:"output_pptx"`     // 最终输出
    Version       string           `json:"version"`         // "v2.1"
    Style         string           `json:"style"`           // 内置风格 ID (可选)
    ComposeEngine string           `json:"compose_engine"`  // "svg" (固定)
}
```

### 状态转移 (v2.1 线性流程)

```
WAIT_TEMPLATE → PARSE → OUTLINE → DETAIL → PREVIEW → COMPOSE → DONE
                  ↑         ↑         ↑
                  │         │         └── 可选微调循环
                  │         └── 🔔 确认点 2: 详情+布局
                  └── 🔔 确认点 1: 大纲
                                    🔔 确认点 3: 预览 (文字摘要+图片 PNG)
```

**关键变更 (v2.0 → v2.1)**：
- 移除 `WAIT_MATERIAL`, `PREPROCESS`, `STYLE_SELECT`, `REFINE`, `QUALITY_CHECK`, `POST_PROCESS` 阶段常量
- Phase 0 (素材预处理)、Phase A (风格选择) 降级为 Phase 2 可选子步骤
- Phase 7 (质量检查)、Phase 8 (后处理) 降级为 Phase 5 内部子步骤
- `ComposeEngine` 固定为 `"svg"`；v1.0 克隆引擎通过 `Version: "v1.0"` + `InitV1()` 向后兼容



## 5. 集成架构图 (v2.1)


```
┌────────────────────────────────────────────────────────────────┐
│              REEF PPT AGENT v2.1 — 统一 SVG 引擎                │
├────────────────────────────────────────────────────────────────┤
│                                                                │
│  用户输入 ──→ Phase 1 ──→ Phase 2 ──→ 🔔 ──→ Phase 3 ──→ 🔔    │
│  (模板+内容)   模板分析    大纲生成   确认   详情+布局   确认     │
│                  │                                  │          │
│           style_profile.json                 detail_plan.json   │
│           (配色+字体+槽位)                  (含自动风格映射)     │
│                                                                │
│  ──→ Phase 4 ──→ 🔔 ──→ Phase 5 ──→ output.pptx               │
│      预览        确认    合成输出                                │
│    (文摘+PNG)           (SVG桥接→编辑→svg2pptx)                 │
│       │                                                        │
│       └── 可选微调循环 (逐页调整, 不限次数)                     │
│                                                                │
│  内部子步骤 (用户不可见):                                       │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ Phase 0: 多源素材→MD  │ Phase A: 内置风格匹配             │  │
│  │ Phase 1B: pptx→SVG桥  │ Phase 7: 自动质量检查             │  │
│  │ spec_lock.md 一致性   │ Phase 8: 动画/旁白后处理          │  │
│  └──────────────────────────────────────────────────────────┘  │
│                                                                │
│  引擎: 统一 SVG 管道                                            │
│  自定义模板: .pptx → pptx_to_svg → SVG → AI编辑 → svg2pptx     │
│  内置风格: 5种预设 (academic/business/tech/creative/minimal)   │
│  确认点: 3 (大纲 → 详情+布局 → 预览)                            │
└────────────────────────────────────────────────────────────────┘
```

### 5.1 管道收敛点

v2.1 取消了 v2.0 的双入口路由。所有路径在 Phase 2 之前汇入统一的 5 阶段管道：

```
自定义模板 (.pptx)
    │
    ▼
Phase 1B: pptx_to_svg 桥接 → 8 页 shape-aware SVG
    │
    ├──────────────────────────────────────┐
    ▼                                      ▼
Phase 1: 模板分析                      Phase A: 内置风格
style_profile.json                     5种预设配色+字体
    │                                      │
    └────────────┬─────────────────────────┘
                 ▼
          Phase 2: 大纲生成
                 │
                 ▼
          Phase 3: 详情+布局 (自动风格映射)
                 │
                 ▼
          Phase 4: 预览
                 │
                 ▼
          Phase 5: SVG 编辑 → svg2pptx → output.pptx
```

### 5.2 与 v2.0 的关键差异

| 维度 | v2.0 | v2.1 |
|:---|:---|:---|
| 阶段数 | 10+ (Phase 0/A/1B/1-8) | 5 用户可见 |
| 确认点 | 5+ (含 8 项 BLOCKING 设计锁) | 3 (大纲/详情+布局/预览) |
| 风格映射 | Phase 4 手动指定 | Phase 3 自动匹配 |
| 设计锁 | Strategist 8 项 BLOCKING | spec_lock.md 自动约束 (内部) |
| 预览 | 从未实现 | 文字摘要 + 图片 PNG |
---

## 6. 用户交互流程 (v2.1: 3 确认点)

### 6.1 完整对话示例

```
用户: 帮我把这个项目报告做成 PPT
      [上传 report.pdf + template.pptx]

Agent: 📄 已收到素材 report.pdf + 模板 template.pptx
      
      **Phase 1: 模板分析...**
      ✓ 配色: 主色 #1A56DB, 辅色 #666666
      ✓ 字体: 标题 44pt Microsoft YaHei, 正文 18pt
      ✓ 结构: 封面(1页) + 目录(1页) + 内容(5页可用) + 结尾(1页)
      
      **Phase 2: 生成大纲...**
      [Auto-generated outline from PDF content]
      
      ┌─────────────────────────────────────────┐
      │ 📋 PPT 大纲 (共 10 页)                    │
      │                                          │
      │ 1. 封面 — 2026年度项目总结报告            │
      │ 2. 目录 — 五大章节                        │
      │ 3. 背景与目标                             │
      │ 4. 核心技术方案                            │
      │ 5. 实施里程碑                              │
      │ 6. 目录2 — 成果与展望                     │
      │ 7. 关键指标达成                            │
      │ 8. 团队与协作                              │
      │ 9. 未来规划                                │
      │ 10. 结尾 — 谢谢                           │
      └─────────────────────────────────────────┘
      🔔 **请确认大纲**，或提出修改 (增删页/调整顺序)

用户: 第7和第8页调换一下顺序

Agent: 已调整。确认大纲？回复 "开始" 继续。

用户: 开始

Agent: **Phase 3: 生成详情+布局...**
      
      ┌─────────────────────────────────────────┐
      │ 📝 详情方案 (每页内容+布局)                │
      │                                          │
      │ 第1页 封面                                │
      │   title: 2026年度项目总结报告 (44pt, 蓝色)│
      │   subtitle: 技术研发部 (24pt, 灰色)       │
      │                                          │
      │ 第2页 目录                                │
      │   bullet_list:                           │
      │   • 1. 背景与目标                         │
      │   • 2. 核心技术方案                       │
      │   ... (5项, 18pt 宋体)                   │
      │                                          │
      │ 第3页 背景与目标 (内容页)                  │
      │   title: 项目背景与目标 (36pt, 蓝色)      │
      │   body: 在当前数字化转型浪潮下... (18pt)  │
      │   bullet_list:                           │
      │   • 市场需求增长 300%                     │
      │   • 技术栈升级迫在眉睫                    │
      │   • 团队规模扩大至 50 人                  │
      │   ... (共8页详情)                         │
      └─────────────────────────────────────────┘
      🔔 **请确认详情方案**

用户: 第3页加上一个架构图

Agent: 好的，"架构图"已加入第3页内容块。
      
      🔔 确认后进入预览阶段。

用户: 确认

Agent: **Phase 4: 生成预览...**
      
      📷 正在渲染 10 页预览...
      [发送 10 张 PNG 图片预览]
      
      🔔 **预览如上**。如需调整某页，请说明 (如 "第5页标题改大")。
      全部满意请回复 "生成"。

用户: 第5页正文太多，精简到100字

Agent: 已调整第5页正文 (230字→98字)。
      [发送更新后预览]
      🔔 满意请回复 "生成"。

用户: 生成

Agent: **Phase 5: 合成最终 PPTX...**
      ✓ 自动映射模板 (封面→模板封面, 内容→最优内容页)
      ✓ SVG 内容注入 (保留模板字体/颜色)
      ✓ svg2pptx 原生合成
      ✓ 质量检查通过 (0 错误, 2 警告)
      
      ✅ 您的 PPT 已生成！[发送 final.pptx]
```

### 6.2 确认点流程图

```
Phase 1: 模板分析 (自动)
    │
Phase 2: 大纲生成
    │
    ▼
 [🔔 确认1: 大纲] ──否──→ 用户修改大纲
    │ 是                     │
    ▼                        │
Phase 3: 详情+布局           │
    │                        │
    ▼                        │
 [🔔 确认2: 详情] ──否──→ 用户修改详情
    │ 是                     │
    ▼                        │
Phase 4: 预览                │
    │                        │
    ▼                        │
 [🔔 确认3: 预览] ──否──→ 逐页微调 ──┘
    │ 是
    ▼
Phase 5: 合成输出 (自动)
    │
    ▼
  ✅ 完成
```

### 6.3 微调交互 (Phase 4 子循环)

```
用户查看预览 → 发现问题
    │
    ├── "第3页正文太长" → 重新注入第3页 → 发送更新预览
    ├── "第5页标题颜色改红色" → override color → SVG重编辑 → 预览
    ├── "第2页加一个图表" → 更新 detail_plan → SVG重编辑 → 预览
    └── "全部OK, 生成" → Phase 5
```
## 7. 文件结构

```
skills/ppt-agent/
├── SKILL.md                    # 技能定义 (v2.1 更新)
│
├── v1.0/                       # 现有 v1.0 代码 (保留)
│   ├── template_parser.py
│   ├── ppt_compositor.py
│   ├── schema.py
│   └── prompts/
│       ├── outline_prompt.md
│       ├── detail_prompt.md
│       ├── template_analysis.md
│       └── style_mapping_prompt.md
│
├── v2.0/                       # v2.1 新增 (保留 v2.0 目录名)
│   ├── source_to_md/           # ← ppt-master 素材转换 (MIT 许可)
│   │   ├── pdf_to_md.py
│   │   ├── doc_to_md.py
│   │   ├── web_to_md.py
│   │   ├── excel_to_md.py
│   │   ├── ppt_to_md.py
│   │   └── source_to_content.sh     # 统一入口脚本
│   │
│   ├── svg_engine/              # ← ppt-master SVG 引擎 (MIT 许可)
│   │   ├── svg_to_pptx/
│   │   │   ├── drawingml_converter.py
│   │   │   ├── drawingml_elements.py
│   │   │   ├── drawingml_styles.py
│   │   │   ├── drawingml_paths.py
│   │   │   ├── drawingml_context.py
│   │   │   ├── pptx_builder.py
│   │   │   ├── pptx_slide_xml.py
│   │   │   ├── pptx_dimensions.py
│   │   │   ├── pptx_media.py
│   │   │   ├── pptx_notes.py
│   │   │   ├── pptx_narration.py
│   │   │   ├── tspan_flattener.py
│   │   │   ├── animation_config.py
│   │   │   └── use_expander.py
│   │   │
│   │   ├── pptx_to_svg/
│   │   │   ├── ooxml_loader.py
│   │   │   ├── slide_to_svg.py
│   │   │   ├── shape_walker.py
│   │   │   ├── converter.py
│   │   │   ├── prstgeom_to_svg.py
│   │   │   ├── custgeom_to_svg.py
│   │   │   ├── txbody_to_svg.py
│   │   │   ├── tbl_to_svg.py
│   │   │   ├── pic_to_svg.py
│   │   │   ├── fill_to_svg.py
│   │   │   ├── ln_to_svg.py
│   │   │   ├── effect_to_svg.py
│   │   │   ├── color_resolver.py
│   │   │   └── emu_units.py
│   │   │
│   │   ├── svg_quality_checker.py
│   │   ├── pptx_animations.py
│   │   ├── notes_to_audio.py       # 旁白/TTS (可选)
│   │   └── image_gen.py            # 图片生成 (可选后端)
│   │
│   ├── prompts/                 # v2.1 新增 prompt
│   │   ├── strategist_prompt.md      # 策略师: 产出 design_spec + spec_lock
│   │   ├── svg_generator_prompt.md   # SVG 执行器: 逐页手写 SVG
│   │   └── image_prompt.md           # 图片生成描述
│   │
│   └── style_catalog.yaml      # 内置风格定义
│
├── templates/                   # PPT 模板资源
│   ├── layouts/
│   │   ├── layout_index.json
│   │   ├── cover/
│   │   ├── content/
│   │   └── ending/
│   ├── charts/
│   │   ├── chart_index.json
│   │   ├── bar/
│   │   ├── pie/
│   │   └── flow/
│   └── icons/
│
└── test/
    ├── test_v1.0/               # 现有测试 (全部保留)
    │   ├── test_ppt_clone.py
    │   ├── test_template_parser.py
    │   ├── test_ppt_compositor.py
    │   ├── test_prompts.py
    │   └── test_e2e_ppt_agent.py
    │
    └── test_v2.0/               # v2.1 新增测试
        ├── test_source_to_md.py
        ├── test_svg_to_pptx.py
        ├── test_pptx_to_svg.py
        ├── test_quality_checker.py
        ├── test_svg_engine_e2e.py
        └── test_animation.py
```

---

## 8. 实施状态 (2026-05-19: ✅ 全部完成)

v2.1 核心流水线已全部实施完毕，80/80 GSD 任务完成，280 测试全部通过。

| Phase | 原计划 | 状态 |
|:---|:---:|:---:|
| I: 素材预处理 + 风格选择 | 18 tasks, 11h | ✅ |
| II: SVG 引擎融合 | 32 tasks, 22h | ✅ |
| III: 质量检查 + 后处理 | 18 tasks, 10h | ✅ |
| IV: Go 状态机 + E2E + 部署 | 12 tasks, 9h | ✅ |
| **总计** | **80 tasks, ~52h** | **✅ 完成** |

### 测试覆盖

| 层级 | 数量 | 状态 |
|:---|:---:|:---:|
| Python 单元测试 (v1.0) | 101 | ✅ |
| Python 集成测试 (v2.0 Phase 1-4) | 171 | ✅ |
| Go 工作流测试 (PPTWorkflow) | 8 | ✅ |
| **总计** | **280** | ✅ |

### 部署状态

- ✅ 新二进制已编译部署 (`/root/reef_server/reef`, 47MB)
- ✅ SKILL.md 已更新至 v2.1
- ✅ gsd-plan-v2.md 80/80 任务全部勾选
- ✅ ROADMAP.md 4/4 Phase 完成
- ✅ v1.0 代码/测试零破坏性变更

```


---

---
## 9. 风险 & 缓解

| 风险 | 影响 | 概率 | 缓解措施 |
|:---|:---|:---:|------|
| ppt-master 依赖库不兼容 | Phase II 阻塞 | 中 | 在 II.1 之前先跑 `pip install` 验证所有依赖；v1.0 克隆逻辑保留在 ppt_compositor.py 作为紧急回退 |
| SVG 引擎生成质量不稳定 | Phase II 产出质量差 | 中 | spec_lock.md 锁定关键参数；Phase 7 质量检查自动修正常见问题 |
| SVG 管道中 LLM 编辑质量不达预期 | Phase II 工时膨胀 | 中 | 写死关键样式参数在 spec_lock.md；限制每页 LLM 编辑轮数上限 (3轮)；v1.0 克隆逻辑保留在 ppt_compositor.py 作为紧急回退 |
| 素材转换中文乱码 | Phase I 产出质量差 | 低 | source_to_md 已验证支持 UTF-8；增加编码检测 |
| 内置模板不够丰富 | 用户选择受限 | 高 | 初期 5 种风格 (学术/商务/科技/创意/极简)，后续扩展 |
| 动画注入后文件损坏 | Phase III 产出不可用 | 低 | 动画注入后自动打开 PPTX 验证有效性 |

---

## 10. 与现有 v1.0 的关系

```
v1.0 (当前 ── MVP 完成)
  │
  ├── template_parser.py       → 保留，作为自定义模板路径
  ├── ppt_compositor.py        → 降级为 Phase 1B 桥接工具 (pptx_to_svg)
  ├── schema.py                → 保留，扩展支持 v2.1 结构
  ├── prompts/*.md             → 保留，新增 strategist + executor
  ├── ppt_workflow.go          → 扩展至 v2.1 状态机 (向后兼容)
  │
  └── v2.0/                    → v2.1 新增模块 (保留目录名)
      ├── source_to_md/        → 素材预处理层
      ├── pptx_to_svg/         → 自定义模板桥接
      ├── svg_export/          → 统一 SVG 编辑管道
      ├── templates/           → 内置模板资源
      ├── style_catalog.yaml   → 风格目录
      └── test_phase*.py       → Phase 1-4 集成测试 (171 测试)

v1.0 所有 101 个 Python 测试 + 7 个 Go 测试 → 全部保留
v2.1 新增 ~170 个测试
```

---

## 11. 关键设计决策记录

| 决策 | 选项 A | 选项 B | 选择 | 理由 |
|:---|------|------|:---:|------|
| 引擎架构 | 双引擎 (克隆+SVG) | 统一 SVG 引擎 | **统一 SVG 引擎** | 自定义模板通过 pptx_to_svg 桥接进入 SVG 管道；内置风格直接加载 SVG 布局模板 |
| SVG 路径 | 新增桥接 | 作为新增路径 | **新增桥接** | v1.0 代码保留但不承担合成职责，pptx_to_svg 作为新桥接工具 |
| ppt-master 集成方式 | 子模块 clone | 手动复制 | **手动复制** | 避免 git submodule 复杂性；ppt-master 为 MIT 许可，复制合规 |
| 内置风格数量 | 2-3 种 | 5+ 种 | **5 种** | 覆盖主要场景，投入产出比最佳 |
| 动画注入 | 始终启用 | 用户可选 | **用户可选** | 不是所有场景都需要动画 |

### v2.1 修正 (2026-05-19)

| 决策 | v2.0 旧方案 | v2.1 新方案 | 理由 |
|:---|------|------|------|
| 确认点数量 | 5+ (大纲/详情/风格映射/8项设计锁/逐页微调) | **3** (大纲/详情+布局/预览) | 用户原始需求明确只有 3 次确认 |
| 风格映射 | 用户手动指定每页"第3页用模板第5页" | **系统自动匹配** (封面→封面模板, 内容→最优内容页) | 用户明确要求自动匹配 |
| Strategist 设计锁 | 8 项 BLOCKING 确认 (配色/字体/图表/图片/布局/页数/动画/特殊) | **删除** (配色+字体从模板 Phase 1 自动提取) | 用户从未要求此环节，且模板分析已有能力自动提取 |
| 预览 | 设计存在但从未实现 | **文字摘要+图片预览**, 在详情确认后提供 | 用户原始需求明确"再次确认后提供预览" |
| 用户可见阶段 | 10+ (Phase 0/A/1/1B/2/3/4/5/6/7/8) | **5** (模板分析/大纲/详情+布局/预览/合成) | 内部实现细节应隐藏 |

---

## 12. 审阅清单 (v2.1 更新)

请确认以下设计决策：

- [ ] **5 阶段用户流程** — 模板分析→大纲→详情+布局→预览→合成
- [ ] **3 确认点** — 大纲 / 详情+布局 / 预览
- [ ] **自动风格映射** — 系统根据大纲骨架自动匹配模板页
- [ ] **删除 Strategist 设计锁** — 配色+字体从模板 Phase 1 自动提取
- [ ] **预览两段式** — 文字摘要 + 图片预览 (详情确认后)
- [ ] **可选微调** — 预览后可逐页调整，但不强制
- [ ] **统一 SVG 引擎** — 自定义模板 pptx→SVG 桥接 → 统一编辑管道
- [ ] **Phase 0 素材预处理** — 多源输入→统一 Markdown, 作为 Phase 2 可选输入
- [ ] **内置风格 5 种** — 无模板时 fallback
- [ ] **v1.0 代码/测试全部保留** — 零破坏性变更
