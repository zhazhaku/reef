---
change: reef-ppt-agent-v3
schema: spec-driven
status: draft
created: 2026-05-22
supersedes: reef-ppt-agent (v2.3)
---

# Proposal: PPT Agent v3 — Content-Driven + Style-Reference + Strong-Preview

## 1. 背景与动机

v2.3 (河南移动案例) 实战暴露 4 类根本性流程问题:

| 现象 | 根因 |
|---|---|
| 用户不知道有 5 个内置风格可选 | "风格决策"隐藏在 engine routing, 无显式确认点 |
| 大纲+内容+布局合并为一个确认点 | 出错时无法分别打回 |
| 复杂自定义模板内容全部错位 | "严格按模板槽填充"在 shape 数 != 内容段数时崩溃 |
| 预览可选, 跳过即出问题 | 缺少视觉门 (visual gate) |

v3 重构目标:

1. **输入与风格解耦** — 用户先定"讲什么", 再选"怎么呈现"
2. **模板语义降级** — 自定义 .pptx 是"风格样本库"而非"槽位容器"
3. **流程显式分裂** — 7 阶段 / 5 确认点, 每点可独立回滚
4. **预览强制化** — 渲染三级降级 (PNG/SVG/HTML), 不允许跳过
5. **引擎从零构建** — LayoutComposer 替代启发式, 元素级 add_shape 保证可编辑性

## 2. 范围

### In scope
- 多源输入预处理 (txt/md/docx/pdf/xlsx/url)
- 5 个内置风格 DSL (商务蓝 / 科技红 / 极简黑 / 学术绿 / 活力橙)
- 自定义参考 .pptx 风格特征提取 (StyleProfile)
- AI 驱动 outline -> detail_plan -> layout_plan 三段式
- 预览渲染管线 (LibreOffice / Chromium / 静态 HTML)
- 元素级 .pptx 合成 (每元素独立可编辑)
- 与 v2.3 的迁移路径与回退开关

### Out of scope
- 视频/动画/SmartArt 转换 (保留 v2.3 行为)
- 协作编辑/多人评审
- 在线 PPT 编辑器 UI
- 翻译/多语种同步生成

## 3. 流程总览

P0 输入采集 -> P1 大纲 -> P2 细化 -> P3 风格选择 -> P4 布局规划 -> P5 预览 -> P6 合成

确认点位置: #1 (P1后), #2 (P2后), #3 (P3后), #3.5 (P4-B 中, 仅自定义路径), #4 (P5后), #5 (P6后)

详细 7 阶段拆解、数据契约、引擎架构见 design.md
逐 Requirement / Scenario 规格见 specs/ppt-agent/spec.md
实施路线见 tasks.md

## 4. 与 v2.3 关系

| 维度 | v2.3 | v3 |
|---|---|---|
| Phase 数 | 5 | 7 |
| Confirmation 数 | 3 | 5 |
| 风格选择 | 隐式 | 显式 P3 |
| 自定义模板语义 | 严格槽填充 | 风格借鉴 (默认) + 严格模式 (可选) |
| 引擎 | compose_with_layout_plan 复用模板 shape | LayoutComposer 从零 add_shape |
| 预览 | 可选 | 强制 + 三级降级 |
| 输入格式 | .pptx + 文字要求 | 多源 + 要求 |

v3 与 v2.3 共存: ppt_agent_version 配置开关切换. v2.3 严格模式作为 v3 内 "strict" 子路径保留.

## 5. 成功标准

- 河南移动 PPT 重做 — 9 页全部内容正确分布, 无空白页, 无堆叠
- 任意用户上传素材 + 内置风格选择 -> 5 分钟内出预览
- 任意 5 个示例参考 .pptx -> 风格借鉴版式生成成功率 >= 90%
- 最终 .pptx 用 PowerPoint 打开 -> 每个文本/形状/图片均可独立点选编辑
