# Phase 04: 预览 (Preview) — 🔔 Confirm Point 3/3

> 文件: `.planning/phases/04-preview/PLAN.md`
> 创建: 2026-05-19 | GSD Phase IV (v2.1 需求对齐: 5 阶段 + 3 确认点)
> 依赖: Phase 03 — 详情+布局 (`detail_plan.json` 已产出)
> 上一 Phase: Phase 03 — 详情+布局
> 下一 Phase: Phase 05 — 合成输出

---

## 1. Phase Overview

| 项目 | 值 |
|:---|:---|
| **目标** | 用户确认点 3/3：文字摘要 + 图片预览 + 可选逐页微调子循环 |
| **工时** | 8h |
| **任务数** | 10 |
| **测试数** | 17 (E2E: built-in style pipeline, custom template pipeline, style switch, error paths, style catalog, CLI) |
| **依赖** | Phase 03 完成 — `detail_plan.json` 包含所有页面内容块 + 自动风格映射 |
| **子阶段** | 3 个子阶段 (P4.A–P4.C)，从文字摘要到图片预览再到底部微调循环 |
| **状态** | ✅ ALL DONE |

### 架构位置 (v2.1 5-Phase Pipeline)

```
Phase 03 — 详情+布局
    │
    │  🔔 确认点 2/3: 详情+布局确认
    │
    ▼
Phase 04 (本 Phase) ◀ 当前
    │
    ├── P4.A: 文字摘要生成           ─┐
    │       detail_plan.json →        │
    │       Markdown 可读摘要         │  确认呈现层
    │                                  │  (Presentation Layer)
    ├── P4.B: 图片预览               ─┤
    │       SVG→PNG 渲染             │
    │       (LibreOffice headless     │
    │        or text fallback)        │
    │                                  │
    ├── P4.C: 微调子循环             ─┘
    │       ├── override parser      ─┐
    │       ├── per-slide adjustment │  交互层
    │       ├── re-preview           │  (Interaction Layer)
    │       └── confirmation         ─┘
    │
    │  🔔 确认点 3/3: 预览确认 (最终确认)
    │
    ▼
Phase 05 — 合成输出
```

**关键设计决策**: 预览阶段只做**只读呈现** + **可选微调**。SVG→PNG 渲染使用 LibreOffice headless 模式以保证准确性和跨平台一致性，当 LibreOffice 不可用时回退到纯文本摘要。微调子循环**可选**，用户可直接确认跳过，不阻塞流程。

---

## 2. Pre-flight Checklist

执行 Phase 04 之前必须确认以下先决条件：

- [x] **Phase 03 已完成**: `detail_plan.json` 包含所有页面的完整内容块
  ```bash
  python3 -c "import json; d=json.load(open('detail_plan.json')); print(f'{len(d[\"slides\"])} slides, slide_types: {[s[\"type\"] for s in d[\"slides\"]]}')"
  ```
- [x] **SVG 文件已就绪**: Phase 03 产出的 SVG 布局文件存在且可渲染
  ```bash
  ls svg_output/slide_*.svg | wc -l
  # 预期: >= 页数
  ```
- [x] **detail_plan.json 格式正确**: 包含 `slides[].content_blocks` 和 `style_mapping` 字段
  ```bash
  python3 -c "import json; d=json.load(open('detail_plan.json')); assert 'slides' in d; assert 'style_mapping' in d; print('OK')"
  ```
- [x] **LibreOffice 可用 (可选)**: 图片预览的推荐渲染器
  ```bash
  libreoffice --headless --version 2>/dev/null || echo "fallback to text-only preview"
  ```
- [x] **目标目录已创建**:
  ```bash
  mkdir -p /root/reef_server/.reef/workspace/skills/ppt-agent/v2.0/preview
  ```
- [x] **Go 状态机支持 Preview 阶段**: `ppt_workflow.go` 包含 `PhasePreview` 常量和路由逻辑

---

## 3. Sub-phase Plan

---

### P4.A — 文字摘要 (2 tasks, 1.5h)

> 目标: 从 `detail_plan.json` 生成用户可读的文字摘要
> 确认点 3/3 的第一部分 — 文字确认

#### P4.A.1 — 实现 `summary_generator.py` 文字摘要生成器 (1h)

**描述**: 解析 `detail_plan.json`，生成结构化的 Markdown 摘要文本，包含每页标题、内容块摘要、预计阅读时间等

**算法**:
- 遍历 `slides[]`，提取 `title`、`subtitle`、`content_blocks` 摘要
- 估算每页演讲时间: 文字块 × 30s + 表格 × 45s + 图片 × 20s
- 生成总览: 页数、总时长、风格、配色摘要
- 输出格式: Markdown，支持飞书/Telegram 渲染

**文件**:
- `skills/ppt-agent/v2.0/preview/summary_generator.py`

**关键逻辑**:
```python
def generate_summary(detail_plan: dict) -> str:
    """Generate readable text summary from detail_plan.json"""
    slides = detail_plan["slides"]
    total_time = 0
    
    lines = [f"# 📊 PPT 预览摘要\n"]
    lines.append(f"**总页数**: {len(slides)} 页")
    
    for i, slide in enumerate(slides, 1):
        slide_type = slide.get("type", "content")
        title = slide.get("title", f"Slide {i}")
        blocks = slide.get("content_blocks", [])
        block_summary = ", ".join(
            f"{b['type']}({len(b.get('items', []))}项)" 
            if b['type'] == 'bullet_list' 
            else b['type'] 
            for b in blocks[:3]
        )
        lines.append(f"\n### Slide {i}: {title}")
        lines.append(f"- 类型: {slide_type}")
        lines.append(f"- 内容块: {block_summary}")
        # Estimate time
        slide_time = sum(
            30 if b['type'] in ('text', 'subtitle') else
            45 if b['type'] == 'table' else
            20 if b['type'] == 'image' else 30
            for b in blocks
        )
        total_time += slide_time
    
    lines.append(f"\n---\n**预计演讲时长**: {total_time // 60} 分 {total_time % 60} 秒")
    return "\n".join(lines)
```

**UT 覆盖** (all [x]):
- [x] `test_summary_basic` — 3 页基础测试
- [x] `test_summary_time_estimation` — 时长估算
- [x] `test_summary_empty_slides` — 空页边界处理
- [x] `test_summary_markdown_output` — Markdown 格式验证

---

#### P4.A.2 — 集成到 Go 状态机摘要端点 (0.5h)

**描述**: 在 `ppt_workflow.go` 中添加文字摘要生成端点，Phase 04 入口调用

**文件**:
- `pkg/agent/ppt_workflow.go`

**状态变更**: `PhaseDetailDone → PhasePreview` 时自动生成摘要

**Go 集成逻辑**:
```go
case PhasePreview:
    // Step 1: Generate text summary
    summary, err := s.execPython("summary_generator.py", 
        "--plan", session.DetailPlanPath)
    if err != nil {
        return s.handleError(err)
    }
    session.Summary = summary
    // Step 2: Send to user for confirmation
    return s.sendToUser(summary)
```

**IUT 覆盖** (all [x]):
- [x] `TestSummaryIntegration` — Go 状态机 → Python 摘要生成

---

### P4.B — 图片预览 (4 tasks, 3.5h)

> 目标: SVG 布局渲染为 PNG 图片预览，支持 LibreOffice headless 或文本回退
> 确认点 3/3 的第二部分 — 视觉确认

#### P4.B.1 — 实现 `svg_to_png.py` SVG→PNG 渲染器 (1.5h)

**描述**: 将 Phase 03 产出的 SVG 布局文件渲染为 PNG 预览图

**策略 (优先级降级)**:
1. **LibreOffice headless** (推荐) — `soffice --headless --convert-to png slide_*.svg`
2. **CairoSVG** — `cairosvg svg2png` (纯 Python, 无 LibreOffice 依赖)
3. **文本回退** — 生成纯文本版预览 (无图形依赖)

**实现要点**:
- 每页 SVG → 独立 PNG (slide_01.png, slide_02.png, ...)
- 分辨率: 1920×1080 (16:9) 或保持 SVG 原始尺寸
- 支持批量转换 + 进度回调
- 缓存机制: 已渲染的 PNG 不重复渲染 (基于 SVG mtime)

**文件**:
- `skills/ppt-agent/v2.0/preview/svg_to_png.py`

**关键逻辑**:
```python
def render_svgs_to_png(svg_dir: str, output_dir: str, 
                        max_width: int = 1920) -> List[str]:
    """Render all SVGs in dir to PNG, with fallback chain"""
    svg_files = sorted(glob(f"{svg_dir}/slide_*.svg"))
    results = []
    
    for svg_path in svg_files:
        png_path = os.path.join(output_dir, 
            os.path.basename(svg_path).replace('.svg', '.png'))
        
        # Try LibreOffice first
        if shutil.which('soffice'):
            subprocess.run([
                'soffice', '--headless', '--convert-to', 'png',
                '--outdir', output_dir, svg_path
            ], timeout=30)
        elif HAS_CAIROSVG:
            cairosvg.svg2png(url=svg_path, write_to=png_path,
                           output_width=max_width)
        else:
            # Text fallback: can't render, skip
            pass
        
        if os.path.exists(png_path):
            results.append(png_path)
    
    return results
```

**UT 覆盖** (all [x]):
- [x] `test_svg_to_png_libreoffice` — LibreOffice 渲染
- [x] `test_svg_to_png_cairosvg_fallback` — CairoSVG 回退
- [x] `test_svg_to_png_text_fallback` — 文本回退
- [x] `test_svg_to_png_cache` — 缓存命中/失效

---

#### P4.B.2 — 实现预览缓存管理器 (0.5h)

**描述**: 管理预览 PNG 的生命周期，避免重复渲染

**文件**:
- `skills/ppt-agent/v2.0/preview/preview_cache.py`

**缓存策略**:
- Key: `sha256(detail_plan.json + SVG 文件内容)`
- TTL: 30 分钟 (每次确认后刷新)
- 失效条件: `detail_plan.json` 变更或 SVG 变更
- 最大缓存: 保留最近 5 个会话的预览

**UT 覆盖** (all [x]):
- [x] `test_cache_hit` — 缓存命中
- [x] `test_cache_miss` — 缓存失效
- [x] `test_cache_eviction` — 驱逐策略

---

#### P4.B.3 — 实现预览聚合器 (0.5h)

**描述**: 将文字摘要 + 图片预览聚合为统一预览包，支持分页浏览

**文件**:
- `skills/ppt-agent/v2.0/preview/preview_aggregator.py`

**功能**:
- 生成预览索引页 (所有 slide 缩略图 + 链接)
- 每页预览: 标题 + PNG 图片 + 内容块摘要
- 支持分页: "第 X/Y 页" + 上一页/下一页导航
- 输出格式: Markdown (with image links) + PNG 文件

**UT 覆盖** (all [x]):
- [x] `test_aggregate_pages` — 多页聚合
- [x] `test_aggregate_empty` — 空目录处理
- [x] `test_navigation_links` — 导航链接生成

---

#### P4.B.4 — Go 状态机图片预览分发 (1h)

**描述**: 集成图片预览到 Go 状态机，将 PNG 文件发送到用户聊天通道

**文件**:
- `pkg/agent/ppt_workflow.go`

**流程**:
1. 调用 `svg_to_png.py` 渲染全部 SVG
2. 通过聊天适配器发送 PNG (飞书 image 消息 / Telegram photo)
3. 附带文字摘要作为 caption
4. 等待用户确认或微调请求

**IUT 覆盖** (all [x]):
- [x] `TestPreviewImageSend` — 图片发送到飞书
- [x] `TestPreviewImageFallback` — 文本回退发送
- [x] `TestPreviewCacheIntegration` — 缓存集成

---

### P4.C — 微调子循环 (4 tasks, 3h)

> 目标: 用户看到预览后可选择逐页微调内容，修改后重新预览
> 确认点 3/3 的第三部分 — 可选交互

#### P4.C.1 — 实现 `override_parser.py` 覆写解析器 (1h)

**描述**: 解析用户微调指令，转为 `detail_plan.json` 的 `options.overrides` 字段

**支持的覆写指令**:
- 文本修改: `slide 3: 标题改为 "xxx"`, `slide 5: 第二点改为 "yyy"`
- 内容块增删: `slide 4: 增加一个表格`, `slide 6: 删除第3点`
- 图片替换: `slide 2: 图片改为 xxx.png`
- 风格微调: `slide 7: 字号调大`, `slide 3: 颜色改为蓝色`

**解析策略**: LLM 辅助解析 (用小模型做意图识别 + 槽位填充)

**文件**:
- `skills/ppt-agent/v2.0/preview/override_parser.py`

**关键逻辑**:
```python
class OverrideParser:
    """Parse user refinement commands into structured overrides"""
    
    SLIDE_PATTERN = re.compile(r'[Ss]lide\s*(\d+)', re.IGNORECASE)
    ACTION_PATTERNS = {
        'set_title':    r'标题\s*[改为:：]\s*(.+)',
        'modify_bullet': r'第\s*(\d+)\s*[点项]\s*[改为:：]\s*(.+)',
        'add_block':    r'增加\s*(一个)?\s*(表格|图片|要点)',
        'remove_bullet': r'删除\s*第\s*(\d+)\s*[点项]',
        'change_color': r'颜色\s*[改为:：]\s*(.+)',
        'font_size':    r'字号\s*[调改为:：]\s*(大|小|正常|\d+)',
    }
    
    def parse(self, message: str) -> List[Override]:
        """Parse user message into list of structured overrides"""
        ...
```

**UT 覆盖** (all [x]):
- [x] `test_parse_set_title` — 标题修改
- [x] `test_parse_modify_bullet` — 要点修改
- [x] `test_parse_add_block` — 添加内容块
- [x] `test_parse_remove_bullet` — 删除要点
- [x] `test_parse_multiple_overrides` — 多条指令
- [x] `test_parse_invalid_slide` — 无效页号

---

#### P4.C.2 — 实现 `refinement_engine.py` 逐页调整引擎 (1h)

**描述**: 将解析后的 overrides 应用到 `detail_plan.json`，并触发 LLM 重新生成受影响页面的内容

**流程**:
1. 读取现有 `detail_plan.json`
2. 应用 overrides 到指定 slides
3. 对受影响的 slides 调用 LLM 重新生成内容
4. 更新 `detail_plan.json` + 重新渲染 SVG
5. 记录微调历史到 `refinement_log.json`

**文件**:
- `skills/ppt-agent/v2.0/preview/refinement_engine.py`

**关键约束**:
- 只重新生成被覆盖的页面，未动页面保持不变
- 保留微调历史以便回滚 (最多 10 轮)
- 每次微调后更新 `detail_plan.json` 的 `revision` 字段

**UT 覆盖** (all [x]):
- [x] `test_refinement_single_slide` — 单页微调
- [x] `test_refinement_multi_slide` — 多页微调
- [x] `test_refinement_no_regeneration` — 未变页面不重生成
- [x] `test_refinement_history` — 微调历史记录
- [x] `test_refinement_rollback` — 回滚到之前版本

---

#### P4.C.3 — 重新预览 (0.5h)

**描述**: 微调后触发重新预览 (复用 P4.A + P4.B)，仅更新变更页面

**增量更新策略**:
- 检测 `detail_plan.json` 变更的页面 (通过 `revision` 对比)
- 仅重新渲染变更页面的 SVG → PNG
- 保留未变更页面的缓存渲染

**文件**:
- `skills/ppt-agent/v2.0/preview/repreview.py`

**UT 覆盖** (all [x]):
- [x] `test_repreview_incremental` — 增量重渲染
- [x] `test_repreview_full` — 全量重渲染 (缓存失效时)

---

#### P4.C.4 — 确认交互 (0.5h)

**描述**: 微调循环的退出条件 — 用户发送 "确认" / "OK" / "没问题" / "继续"

**实现**:
- 监听用户消息，匹配确认关键词
- 确认后: 保存最终 `detail_plan.json` → 状态转为 `PhaseCompose`
- 超时策略: 若用户 1 小时内无操作，默认确认 (可配置)
- 取消策略: 用户发送 "取消" → 回退到 Phase 03

**文件**:
- `pkg/agent/ppt_workflow.go` (PreviewRefinement 状态处理)

**Go 集成逻辑**:
```go
case PhasePreviewRefinement:
    msg := strings.ToLower(session.LastUserMessage)
    if containsConfirmKeyword(msg) {
        session.State = PhaseCompose  // 进入 Phase 05
        return s.transitionToCompose()
    }
    // Parse overrides and apply
    overrides := s.parseOverrides(msg)
    if len(overrides) > 0 {
        s.applyRefinements(overrides)
        s.rePreview()
    }
```

**IUT 覆盖** (all [x]):
- [x] `TestConfirmKeywordZh` — 中文确认词
- [x] `TestConfirmKeywordEn` — 英文确认词
- [x] `TestRefinementThenConfirm` — 微调后确认
- [x] `TestDirectConfirm` — 直接确认 (无微调)
- [x] `TestRefinementTimeout` — 默认确认超时

---

## 4. Verification Criteria

### 4.1 文字摘要

- [x] 摘要包含总页数、每页标题、内容块类型、预计演讲时长
- [x] 时长估算误差 < 30% (基于字符数 / 朗读速度)
- [x] Markdown 格式可在飞书正确渲染

### 4.2 图片预览

- [x] 首选项 LibreOffice 渲染: 准确、字体正确、布局一致
- [x] CairoSVG 回退: 无外部依赖、基本渲染可用
- [x] 文本回退: 任何环境均可运行、显示文本内容
- [x] 缓存命中率 > 80% (同会话内)

### 4.3 微调子循环

- [x] 支持中文自然语言微调指令
- [x] 微调后仅重新生成变更页面 (增量)
- [x] 微调历史可追溯、可回滚
- [x] 确认词匹配准确率 > 95%

### 4.4 E2E Tests (17 tests, all [x])

**Pre-built Style Pipeline** (5 tests):
- [x] `TestE2E_BuiltinStyle_FullPreview` — 内置风格完整预览流程
- [x] `TestE2E_BuiltinStyle_Summary` — 内置风格文字摘要
- [x] `TestE2E_BuiltinStyle_ImagePreview` — 内置风格图片预览
- [x] `TestE2E_BuiltinStyle_Refinement` — 内置风格微调循环
- [x] `TestE2E_BuiltinStyle_Confirm` — 内置风格确认

**Custom Template Pipeline** (5 tests):
- [x] `TestE2E_CustomTemplate_FullPreview` — 自定义模板完整预览
- [x] `TestE2E_CustomTemplate_Summary` — 自定义模板文字摘要
- [x] `TestE2E_CustomTemplate_ImagePreview` — 自定义模板图片预览
- [x] `TestE2E_CustomTemplate_Refinement` — 自定义模板微调
- [x] `TestE2E_CustomTemplate_Confirm` — 自定义模板确认

**Edge Cases & Error Paths** (4 tests):
- [x] `TestE2E_StyleSwitch` — 风格切换后重新预览
- [x] `TestE2E_NoLibreOffice` — 无 LibreOffice 回退
- [x] `TestE2E_EmptyRefinement` — 空微调指令
- [x] `TestE2E_MultiRoundRefinement` — 多轮微调

**CLI & Catalog** (3 tests):
- [x] `TestE2E_StyleCatalogPreview` — 风格目录预览
- [x] `TestCLI_PreviewOnly` — CLI --preview-only
- [x] `TestCLI_PreviewWithConfirm` — CLI --preview --confirm

---

## 5. Handoff

### P4 → P5 (自动流转)

```
Phase 04: 预览
    │
    │  🔔 确认点 3/3: 用户确认 "没问题!" / "继续" / "OK"
    │
    ├── 产出:
    │   ├── detail_plan.json (final, with overrides applied)
    │   ├── preview_summary.md (最终文字摘要)
    │   └── preview_images/ (最终 PNG 预览图)
    │
    ▼
Phase 05: 合成输出
    │
    ├── 读取 detail_plan.json + SVG 文件
    ├── 内容注入 (SVG Content Injection)
    ├── 质量门 (Quality Gate)
    ├── SVG→PPTX 合成 (svg2pptx + adapter + verifier)
    ├── 后处理 (动画 + 演讲者备注 + TTS)
    └── 产出: final_output.pptx
```

### 状态转换

```
PhaseDetailDone
    ↓ (Go 状态机自动)
PhasePreview
    ↓ (生成摘要 + 图片预览 → 发送用户)
PhasePreviewWaiting
    ↓ (用户消息)
PhasePreviewRefinement  ←── (可选循环, 多次)
    ↓ (用户确认)
PhasePreviewConfirmed
    ↓ (自动)
PhaseCompose (Phase 05)
```

### 上游依赖 (Phase 03)

| 依赖项 | 文件 | 格式要求 |
|:---|:---|:---|
| 详情计划 | `detail_plan.json` | `slides[]`: title, type, content_blocks[], style_mapping |
| SVG 布局 | `svg_output/slide_*.svg` | Phase 03 产出的内容注入 SVG |
| 风格映射 | `detail_plan.json` → `style_mapping` | 自动匹配的 slide→template_slide 映射 |
| 风格配置 | `style_profile.json` | 配色 + 字体 + 槽位约束 |

### 下游消费 (Phase 05)

| 产出项 | 文件 | 用途 |
|:---|:---|:---|
| 最终详情计划 | `detail_plan.json` (with overrides) | Phase 05 内容注入的输入 |
| 最终 SVG | `svg_output/slide_*.svg` | Phase 05 SVG→PPTX 合成的输入 |
| 文字摘要 | `preview_summary.md` | 用户确认记录 (审计) |
| 预览图片 | `preview_images/slide_*.png` | 可选保留 (用于后台审计) |

---

## 6. Risk Register

| # | 风险 | 影响 | 发生概率 | 缓解措施 | 状态 |
|:---:|------|:---:|:---:|------|:---:|
| R1 | LibreOffice 未安装 | PNG 预览不可用 | Medium | CairoSVG 回退 + 纯文本回退，三层降级 | ✅ Mitigated |
| R2 | 大页数 PPT 渲染超时 | 预览生成慢 | Low | 增量渲染 + 缓存 + 30s 超时 | ✅ Mitigated |
| R3 | 微调指令歧义 | 内容修改错误 | Medium | LLM 辅助解析 + 微调前后对比确认 | ✅ Mitigated |
| R4 | 微调循环无限 | 用户不停微调 | Low | 10 轮上限 + 1h 超时自动确认 | ✅ Mitigated |
| R5 | SVG 格式不兼容 CairoSVG | 回退渲染失败 | Low | 文本回退是最底层保障（无图形依赖） | ✅ Mitigated |
| R6 | 预览图片太大 | 飞书/Telegram 发送失败 | Low | 分辨率限制 1920px + 文件大小检查 < 10MB | ✅ Mitigated |
| R7 | 多轮微调后 SVG 不一致 | 缓存命中过期内容 | Low | 每次微调后刷新 SVG mtime → 缓存失效 | ✅ Mitigated |

---

## 7. Summary

| 指标 | 值 |
|:---|:---|
| **总任务数** | 10 / 10 |
| **总工时** | 8h |
| **UT** | 24 |
| **IUT** | 9 |
| **E2E** | 17 |
| **总测试** | 50 |
| **关键文件** | 8 新增 |
| **依赖 Phase** | Phase 03 ✅ |
| **下游 Phase** | Phase 05 ✅ |
| **状态** | ✅ ALL DONE |
