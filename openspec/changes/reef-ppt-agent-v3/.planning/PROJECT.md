# PPT Agent v3

## Overview
PPT Agent v3 是对现有 v2.3 架构的全面重构，核心变更：从"模板强绑定"转向"风格借鉴"模式，解决 v2.3 在复杂模板（>20 shapes/slide）下内容错位的根本问题。

## Problem
v2.3 的 `compose()` + `_auto_match_shape()` 启发式匹配在河南移动 PPT 案例中彻底失败：
- 29-64 shapes/slide 的复杂模板，启发式把所有内容塞进同一个 shape
- clone + fill slot 模式要求内容严格适配模板结构，无法灵活调整
- 3 个确认点不够，跳过预览直接生成导致问题到最终交付才发现

## Goal
1. 7 阶段流水线 + 5 个确认点，强制预览
2. 双路径：内置 5 风格 DSL / 自定义 .pptx 参考（借鉴风格，不强绑结构）
3. 从空白构建 PPTX，每元素独立可编辑
4. 河南移动 PPT 重做验证

## Tech Stack
- Python 3.10+ / python-pptx
- Pillow / cairosvg (预览渲染)
- LLM API (outline/detail/layout 生成)

## Key Decisions
- layout_plan.json v3: shapes[] 从零定义 + style_ref 借鉴（替代 target_shape_idx）
- StyleExtractor → LayoutComposer → PPTXBuilder 三引擎架构
- v2.3 compose_with_layout_plan() 保留为 strict mode 向后兼容
- 预览渲染降级链: PNG → SVG → HTML
