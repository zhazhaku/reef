# Tasks: Reef PPT Agent

## Phase 0: 研究验证 (Research)

### T0.1 — python-pptx 模板克隆验证 [SMALL]
**目标**: 验证 python-pptx 能否正确克隆模板 slide（保留所有样式）并填充新内容。
**文件**: 新 `test/test_ppt_clone.py`

测试用例:
1. 从模板克隆 COVER slide → 替换标题文字 → 保存，肉眼对比样式
2. 克隆 CONTENT slide with 多个 shape → 逐 shape 替换文字
3. 克隆带图表的 slide → 替换数据

**工时**: 2h
**依赖**: 无

### T0.2 — 模板解析 JSON 格式验证 [SMALL]
**目标**: 用真实 PPT 模板测试 `template_parser.py` 输出的 JSON 是否包含 LLM 所需的所有信息。
**文件**: `test/test_template_parser.py`

**工时**: 1h
**依赖**: T0.1

---

## Phase 1: 模板解析器 (Template Parser)

### T1.1 — template_parser.py 实现 [MEDIUM]
**文件**: `pkg/skills/ppt-agent/template_parser.py`

功能:
- 解析 .pptx 所有 slide 和 shape
- 提取文本内容 + 字体样式 + 颜色 + 大小
- 识别 shape 类型 (TEXT_BOX, PLACEHOLDER, PICTURE, TABLE, CHART)
- 自动推断 slide 类型 (COVER/TOC/SECTION/CONTENT/ENDING)
- 提取主题配色和字体集
- 输出结构化 JSON

**工时**: 4h
**依赖**: T0.1

### T1.2 — TemplateStructure JSON Schema [SMALL]
**文件**: `pkg/skills/ppt-agent/schema.py`

定义 `TemplateStructure` 和 `SlideInfo` 的 JSON Schema 用于验证。

**工时**: 1h
**依赖**: T1.1

---

## Phase 2: PPT 合成器 (PPT Compositor)

### T2.1 — Slide 克隆函数 [MEDIUM]
**文件**: `pkg/skills/ppt-agent/ppt_compositor.py`

实现 `_clone_slide()`: 将模板 slide 的形状/关系/样式完整克隆到输出 PPT。

**工时**: 3h
**依赖**: T1.1

### T2.2 — 内容填充函数 [MEDIUM]
**文件**: `pkg/skills/ppt-agent/ppt_compositor.py`

实现:
- `_fill_text_block()` — 填充文本框
- `_fill_table_block()` — 创建表格
- `_fill_image_block()` — 插入图片
- `_fill_bullet_list()` — 填充要点列表
- `_create_shape_decor()` — 无图片时创建装饰图形

**工时**: 4h
**依赖**: T2.1

### T2.3 — 主合成函数 + CLI [SMALL]
**文件**: `pkg/skills/ppt-agent/ppt_compositor.py`

```bash
python3 ppt_compositor.py \
  --template template.pptx \
  --plan detail_plan_v2.json \
  --mapping style_mapping.json \
  --images images/ \
  --output result.pptx
```

注：输入改为 `detail_plan_v2.json`（含 overrides），以支持 Phase 5 微调后的最终方案。

**工时**: 2h
**依赖**: T2.1, T2.2

---

## Phase 3: Agent Prompt 设计

### T3.1 — 大纲生成 Prompt (LLM) [SMALL]
**文件**: `pkg/skills/ppt-agent/prompts/outline_prompt.md`

System prompt + 输出 JSON Schema 定义。

**工时**: 1h
**依赖**: 无

### T3.2 — 详细内容 Prompt (LLM) [SMALL]
**文件**: `pkg/skills/ppt-agent/prompts/detail_prompt.md`

System prompt + content_blocks 类型定义（含 `overrides` 字段说明）。

**工时**: 1h
**依赖**: T3.1

### T3.3 — 模板分析 Prompt (LLM) [SMALL]
**文件**: `pkg/skills/ppt-agent/prompts/template_analysis.md`

LLM 阅读 template_parser.py 的 JSON 输出后生成人类可读的 "模板使用指南"，帮助用户在 Phase 4 做风格映射。

**工时**: 1h
**依赖**: T1.1

### T3.4 — 微调解析器 (Override Parser) [MEDIUM]
**文件**: `pkg/skills/ppt-agent/override_parser.py`

实现：
- 解析用户自然语言调整指令 → 定位目标 slide/content_block → 更新 overrides 字段
- 支持单页操作：`"第3页标题字号改成32pt"` → `slides[2].content_blocks[title].overrides.font_size = 32`
- 支持全局操作：`"全局正文14pt"` → 遍历所有 text/bullet_list block 设置 font_size
- 支持内容块操作：`"第5页正文第1条改为XXX"` / `"删除第3页正文第2条"`
- 返回更新后的 `detail_plan_v2.json` + 变更摘要

**工时**: 5h
**依赖**: T3.2

### T3.5 — 微调 Prompt (LLM) [SMALL]
**文件**: `pkg/skills/ppt-agent/prompts/refinement_prompt.md`

System prompt 定义微调对话协议：确认用户调整意图，调用 override_parser，**必要时调用 preview_composer 生成预览**，反馈变更摘要，等待下一指令或确认完成。

**工时**: 1h
**依赖**: T3.4

### T3.6 — 预览合成器 (Preview Composer) [MEDIUM]
**文件**: `pkg/skills/ppt-agent/preview_composer.py`

实现：
- 输入：template.pptx + detail_plan_v2.json + 目标 slide 编号
- 从模板深拷贝对应 slide 的 XML 结构到临时单页 PPTX
- 应用该 slide 的所有 overrides（字号/颜色/字体/图片尺寸/布局交换等）
- 输出临时 `slide_preview.pptx`
- 调用 `soffice --headless --convert-to png` 渲染为 1280x720 PNG
- 返回 PNG 文件路径供 Agent 发送给用户
- 降级方案：检测 LibreOffice 可用性，不可用时输出文本预览描述

**工时**: 4h
**依赖**: T2.1, T3.4

### T3.7 — 预览缓存管理 [SMALL]
**文件**: `pkg/skills/ppt-agent/preview_cache.py`

实现：
- 按 slide 编号 + preview_version 管理预览 PNG
- 同一 slide 多次调整后，旧预览自动覆盖
- 微调阶段结束时清理所有临时文件
- 限制预览 PNG 总大小 < 50MB

**工时**: 1h
**依赖**: T3.6

---

## Phase 4: picoclaw 集成 (✅ 已完成)

### T4.1 — PPT Agent Skill 注册 [SMALL] ✅
**文件**: `skills/ppt-agent/SKILL.md` (workspace) + `pkg/skills/ppt-agent/SKILL.md` (code)

```yaml
name: ppt-agent
description: "Multi-phase AI PPT generation from user .pptx templates..."
```

**已实现**:
- Standard YAML frontmatter with `name` + `description` (compatible with SkillsLoader)
- Body: 5-phase workflow instructions, CLI commands, JSON data formats
- Path references use workspace-relative paths: `skills/ppt-agent/template_parser.py`

**工时**: 0.5h ✅
**依赖**: T1.1, T2.3

### T4.2 — Tool 注册 [SMALL] (v1.1)
**文件**: `pkg/tools/reef_tools.go`

新增:
- `ppt_parse_template` tool → 调用 `python3 skills/ppt-agent/template_parser.py`
- `ppt_compose` tool → 调用 `python3 skills/ppt-agent/ppt_compositor.py`

**工时**: 1h (v1.1)
**依赖**: T1.1, T2.3, T4.1

### T4.3 — PPT Workflow 状态机 [MEDIUM] ✅
**文件**: `pkg/agent/ppt_workflow.go`

```go
type PPTPhase int  // 6 states: WAIT_TEMPLATE → PARSE → OUTLINE → DETAIL → STYLE_MAP → COMPOSE → DONE
type PPTSession struct { Phase; Template; Artifacts map[string]string; SlideCount }
type PPTWorkflow struct { sessions map[string]*PPTSession }  // thread-safe via sync.Mutex
```

**测试**: `go test -run TestPPTWorkflow -v` → 7/7 通过 ✅

**工时**: 2.0h ✅ (实际实现 5 阶段，v1.1 增加 REFINE 子状态)
**依赖**: T4.1

### T4.4 — Workspace Skill 部署 [SMALL] ✅
复制 ppt-agent 到 workspace skills 目录，使其可被 SkillsLoader 发现：

```bash
# 已执行
cp -r pkg/skills/ppt-agent/ workspace/skills/ppt-agent/
```

**验证**:
- `SkillsLoader.ListSkills()` 返回 `ppt-agent`, `source="workspace"` ✅
- `SkillsLoader.LoadSkill("ppt-agent")` 返回非空 markdown 内容 ✅
- 路径 `skills/ppt-agent/SKILL.md` 存在且可读 ✅

**工时**: 0.5h ✅
**依赖**: T4.1

---

## Phase 5: 测试与文档 (✅ MVP 完成)

### T5.1 — E2E 测试 [MEDIUM] ✅
**文件**: `test/test_e2e_ppt_agent.py`

完整流程已验证:
1. 输入模板 (5 页) + 内容文本 ✅
2. template.json 输出格式正确 ✅
3. outline.json 5 页大纲正确 ✅
4. detail_plan.json 包含完整 content_blocks ✅
5. 最终 .pptx 5 页内容正确 (Slide 1 "Project Reef" → Slide 5 "Thank You") ✅
6. 108 tests + 2 subtests 全部通过 ✅

**工时**: 2.0h ✅ (实际)

### T5.2 — 用户指南 [SMALL] (v1.1)
**文件**: `docs/ppt-agent/USER_GUIDE.md`

飞书交互流程说明 + 示例。

**工时**: 1h (v1.1)

### T5.3 — 真实模板测试集 [SMALL] (v1.1)
收集 3-5 个不同类型的真实 PPT 模板（商务/教育/科技/营销），验证兼容性。

**工时**: 2h (v1.1)

---

## 依赖图

```
Phase 0:  T0.1 ── T0.2
              │
Phase 1:  T1.1 ── T1.2
              │
        ┌─────┴─────┐
Phase 2: T2.1 ── T2.2 ── T2.3
        │
Phase 3: T3.1 ── T3.2 ── T3.4 ── T3.5
        T3.3 ────────┘        │
                     T3.6 ── T3.7
        │
Phase 4: T4.1 ── T4.2 ── T4.3
        │
Phase 5: T5.1 ── T5.2
        T5.3
```

## 工时汇总

| Phase | 任务 | 工时 |
|-------|------|------|
| 0 | T0.1-T0.2 | 3h |
| 1 | T1.1-T1.2 | 5h |
| 2 | T2.1-T2.3 | 9h |
| 3 | T3.1-T3.7 | 14h |
| 4 | T4.1-T4.3 | 5.5h |
| 5 | T5.1-T5.3 | 7h |
| **总计** | | **43.5h (~6 工作日)** |
