---
change: reef-ppt-agent
schema: spec-driven
status: research
created: 2026-05-14
---

# Proposal: Reef PPT Agent — 多阶段智能 PPT 生成系统

## 1. 背景与动机

用户需要在一个已有的 PPT 模板基础上，根据内容要求生成新的 PPT。
核心需求链: 模板解析 → 大纲生成 → 内容编排 → 风格映射 → 最终合成。

现有 AI PPT 产品（Gamma, Beautiful AI, Decktopus）偏向"全 AI 创作"，而最广泛的实际场景是**用户已有固定模板，只需 AI 填充内容**。

## 2. 参考项目调研

### 2.1 最相关参考: ai-ppt-generator
| 项目 | lang | stars | 关键特性 |
|------|------|-------|---------|
| [magnetpackk/ai-ppt-generator](https://github.com/magnetpackk/ai-ppt-generator) | Python | 0 (新) | 专注已有模板场景，三步流水线（模板解析→内容整理→匹配合并），模块完全解耦 |

**三步架构**:
1. **TemplateParser**: 输入 `.pptx` → 输出 `TemplateStructure`（每页类型/占位区域/原始样式/配色字体）
2. **ContentOrganizer**: 输入资料+结构 → 输出 `ContentPlan`（大纲+数据可视化建议+图片需求）
3. **PPTCompositor**: 输入内容方案+图片+模板 → 输出最终 PPTX

**核心模块**:
```
src/
├── api/           # Web API 接口
└── core/
    ├── template_parser.py     # PPT 结构识别
    ├── content_organizer.py   # 知识库→内容方案
    ├── image_handler.py       # 图片搜索/生成
    └── ppt_compositor.py      # 模板+内容→PPTX
```

### 2.2 其他参考
| 项目 | 语言 | stars | 适用性 |
|------|------|-------|--------|
| [presenton/presenton](https://github.com/presenton/presenton) | TS | 5k | JS 全栈，有 `presentation-export` 导出模块，但偏向 server+web 架构 |
| [unidoc/unioffice](https://github.com/unidoc/unioffice) | Go | 4.9k | Go 原生 PPTX 库，适合集成到 picoclaw |

### 2.3 技术选型决策

**PPT 操作引擎**: `python-pptx`（通过 picoclaw `exec`/`spawn` 调用 Python 子进程）
- 原因: `python-pptx` 是最成熟的 PPTX 操作库，支持逐元素编辑、模板读写、图片插入、图表生成
- Go 替代: `unioffice` 可用但不如 `python-pptx` 成熟
- 实现: picoclaw Agent 生成 Python 脚本，通过 `exec` 执行

## 3. 用户需求摘要

| # | 需求 | 描述 |
|---|------|------|
| 1 | 提供模板 PPT | 用户上传任意 `.pptx` 作为风格基准 |
| 2 | 提供内容+要求 | 用户输入文本描述，定义 PPT 内容和要求 |
| 3 | 先生成大纲确认 | 系统根据内容生成 PPT 大纲，逐 slide 确认 |
| 4 | 再生成详细内容 | 大纲确认后，生成每页详细内容方案 |
| 5 | 指定风格映射 | 用户指定每页对应模板哪一页风格 |
| 6 | 逐页对话微调 | 生成前可逐页对聊调整文字/字号/图片/图形/布局 |
| 7 | 图片处理二选一 | 有图片需求→用户确认提供/或模型生成；无图→纯 PPT 图形 |
| 8 | 每元素可编辑 | 不能每页一张图，必须逐元素（文本框/图形/图片）独立可编辑 |
| 9 | 禁止 HTML 导出 | 不能用 HTML→PPT 方式，必须原生 PPTX 操作 |

## 4. 工作流设计（6 阶段）

```
Phase 1: 模板解析    用户上传 PPT → TemplateParser → TemplateStructure
                             ↓
Phase 2: 大纲生成    内容要求 + TemplateStructure → LLM → 大纲 [用户确认]
                             ↓
Phase 3: 内容编排    大纲 + 内容要求 → LLM → 详细内容方案 [用户确认]
                             ↓
Phase 4: 风格映射    详细方案 + TemplateStructure → 用户指定每页模板映射
                             ↓
Phase 5: 逐页微调    对聊模式逐页调整 → 生成预览图 → 不满意继续调 → 确认
                              ↓
Phase 6: PPT 合成    最终方案 + 模板映射 + ImageHandler → python-pptx → PPTX 输出
```

## 5. 与 picoclaw 集成

### 作为 Agent Workflow
```
用户消息 → Server (Hermes Coordinator)
              │
              ├─ Phase 1: reef_submit_task → Template Parser (Python agent)
              ├─ Phase 2: reef_submit_task → Content Planner (LLM agent)
              ├─ Phase 3: reef_submit_task → Detail Writer (LLM agent)
              ├─ Phase 4: User interactive mapping (Server)
              ├─ Phase 5: Per-slide dialog refinement (Server + detail_plan updates)
              └─ Phase 6: reef_submit_task → PPT Composer (Python agent)
```

### 两种模式
- **Standalone 模式**: 单个 picoclaw 实例直接执行完整流程
- **Swarm 模式**: Coordinator 分发到各专业 Client

## 6. 范围

### In Scope (v1)
- PPTX 模板解析（识别 slide 类型、占位区域、配色/字体/主题）
- 基于内容的 PPT 大纲生成（逐页大纲+数据可视化建议）
- 大纲→详细内容方案转换
- 逐页对话微调（文字、字号、字体、颜色、图片、图形、布局的局部调整）
- 预览迭代机制（调整后生成单页预览 PNG，不满意可继续调整）
- LibreOffice headless 渲染 + 降级文本预览方案
- 用户指定的 slide 风格映射
- 原生 PPTX 合成（python-pptx, 逐元素编辑）
- 图片处理：网络搜索或 AI 生成（通过已注册的图像生成 tool）

### Out of Scope (v1)
- 实时 Web UI 预览（当前通过飞书消息交互）
- 复杂图表自动生成（v1 仅支持简单表格/图表）
- 多模板融合
- 批量 PPT 生成

## 7. 技术栈

| 层 | 技术 | 说明 |
|----|------|------|
| Agent 框架 | picoclaw | 工作流编排 + 多轮确认 |
| PPT 引擎 | python-pptx | 原生 PPTX 操作，逐元素编辑 |
| 图片处理 | web_search + 图像生成 API | 已有 tool 复用 |
| 模板解析 | python-pptx | 解析 slide layout、placeholder、主题 |
| 渲染引擎 | LibreOffice headless | 单页 PPTX → PNG 预览渲染 |

## 8. 约束

- **不可用 HTML 导出方式** — 必须直接操作 PPTX XML
- **每 slide 每元素独立** — 禁止整页导出为图片
- **图片不强制自动生成** — 无图片需求时用 PPT 内置形状替代
- **流程必须多阶段确认** — 不能一次性生成，每阶段需用户确认（含逐页微调确认）
- **微调保留到最终方案** — 所有局部调整写入 `detail_plan_v2.json`，Phase 6 以此合成

## 9. 风险

| 风险 | 影响 | 概率 | 缓解 |
|------|------|------|------|
| python-pptx 模板兼容性 | 复杂模板解析失败 | 中 | 用 json dump slide 结构供 LLM 分析；fallback 提取所有 shape 信息 |
| 内容与模板布局不匹配 | 文字溢出/截断 | 高 | ContentOrganizer 根据占位区域大小调整内容长度 |
| 图片生成质量不可控 | 图片不符合要求 | 中 | 支持用户替换图片；优先用户提供 |
| 多轮交互 token 消耗大 | 上下文腐化 | 中 | 每阶段结束后 compact；复用 context-sandbox 机制 |

---

## 10. Skill Integration

### 10.1 Skill Discovery
ppt-agent is registered as a **workspace skill** under `skills/ppt-agent/`.
The `SkillsLoader` discovers it automatically via `SKILL.md` frontmatter
and exposes it to the agent through the `<skills>` catalog in the system prompt.

```yaml
# skills/ppt-agent/SKILL.md (frontmatter)
name: ppt-agent
description: "Multi-phase AI PPT generation from user .pptx templates..."
```

### 10.2 Skill Directory Structure
```
skills/ppt-agent/
├── SKILL.md                  ← Discovered by SkillsLoader
├── template_parser.py        ← Phase 1: .pptx → JSON
├── ppt_compositor.py         ← Phase 5: JSON → .pptx
├── schema.py                 ← JSON schema validation
└── prompts/
    ├── outline_prompt.md     ← Phase 2: content → outline
    ├── detail_prompt.md      ← Phase 3: outline → detail
    ├── template_analysis.md  ← Phase 1/4: feed structure to LLM
    └── style_mapping_prompt.md ← Phase 4: plan → style mapping
```

### 10.3 Integration Flow
```
SkillsLoader.ListSkills()
  └→ reads skills/ppt-agent/SKILL.md
      └→ SkillInfo{Name:"ppt-agent", Source:"workspace", ...}
          └→ injected into system prompt <skills> catalog

Agent receives PPT task
  └→ skill content loaded via SkillsLoader.LoadSkill("ppt-agent")
      └→ agent sees CLI commands and prompt paths
          └→ executes python3 skills/ppt-agent/template_parser.py ...
```

### 10.4 Skill vs Code-Level Module
| Aspect | Workspace Skill | Code Module |
|--------|----------------|-------------|
| Location | `workspace/skills/ppt-agent/` | `picoclaw/pkg/skills/ppt-agent/` |
| Discovered by | SkillsLoader (runtime) | Go import (compile-time) |
| Editable at runtime | ✅ Yes | ❌ Requires rebuild |
| Go workflow state machine | N/A (references skill) | `pkg/agent/ppt_workflow.go` |
| Python tools | `skills/ppt-agent/*.py` | Same files, symlinked from workspace |

The Go state machine (`ppt_workflow.go`) is a **code-level orchestrator** that
calls the Python tools via `exec`. The skill provides the **knowledge** (prompts,
workflow instructions, tool paths) while the Go code provides the **execution
framework** (state tracking, transition validation, artifact management).

## 11. 已实施状态

| Phase | 模块 | 状态 | 测试 |
|-------|------|------|------|
| P0 研究 | 克隆可行性 + Schema 验证 | ✅ 完成 | 14/14 通过 |
| P1 模板解析 | `template_parser.py` | ✅ 完成 | 37/37 通过 |
| P2 PPT 合成 | `ppt_compositor.py` | ✅ 完成 | 32/33 通过 |
| P3 Agent Prompts | 4 prompt .md 文件 | ✅ 完成 | 人工审核 |
| P4 风格映射 | `style_mapping_prompt.md` | ✅ 完成 | Schema 验证 |
| P5 集成 | `ppt_workflow.go` + SKILL.md | ✅ 完成 | 7/7 通过 |
| P5.2 E2E | `test_e2e_ppt_agent.py` | ✅ 完成 | 5 页 PPTX 验证 |

### 后置 v1.1 任务
| 任务 | 工时 |
|------|------|
| P3.4 Override Parser（自然语言→JSON） | 4.0h |
| P3.5 微调对话 Prompt | 1.0h |
| P3.6 Preview Composer（soffice 渲染） | 2.0h |
| P3.7 预览缓存管理 | 1.0h |
| P0.3 LibreOffice 预览验证 | 1.0h |
| **合计** | **9.0h** |
