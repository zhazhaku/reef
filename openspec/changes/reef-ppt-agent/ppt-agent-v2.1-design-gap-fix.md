# Reef PPT Agent v2.1 — 需求对齐修正 (5 阶段简化)

> 修订日期: 2026-05-19
> 基于: 用户原始需求 vs ppt-agent-v2-fusion-design.md 差异分析

---

## 1. 差异根因

| # | 用户原始需求 | v2.0 fusion design | 修正方向 |
|---|------------|-------------------|---------|
| 1 | 提取模板配色+字体作为全局约束 | 配色/字体信息分散在 Phase 1/A/5 | **Phase 1 输出增加 style_profile** |
| 2 | 大纲由模型按输入源自定页数 | 同 | ✅ 保留 |
| 3 | 3 确认点: 大纲→详情+布局→预览 | 5+ 确认点 (含 8 项 BLOCKING) | **删除 Strategist 设计锁，改为自动** |
| 4 | 风格映射 **自动** (大纲骨架→模板) | Phase 4 手动指定每页映射 | **删除手动映射，改为自动** |
| 5 | 预览在详情确认**之后** | 预览设计存在但从未实现 | **Phase 4 新增预览段** |
| 6 | 两步+可选微调 | 逐页微调 | ✅ 保留微调能力，但不强制 |

---

## 2. 修正后流水线 (5 阶段, 3 确认点)

```
用户提供: 模板.pptx + 输入源(文字/文件)

Phase 1: 模板分析 ─── 提取 style_profile (配色+字体+页类型+槽位)
       │
Phase 2: 大纲生成 ─── LLM 按输入源自定大纲 (封面→目录→内容...→结尾)
       │                  ← 🔔 用户确认
Phase 3: 详情+布局 ── LLM 展开每页详细内容+槽位布局 (受模板字体约束)
       │                  ← 🔔 用户确认
Phase 4: 预览 ─────── 文字摘要 → 图片预览 (SVG→PNG)
       │  ↑               ← 🔔 用户确认 (不满意可逐页微调)
       │  └── 微调循环 ──┘
Phase 5: 合成输出 ─── 自动映射大纲→模板页 → SVG桥接 → 注入 → svg2pptx
```

### 确认点从 5+ 减到 3

| 确认点 | 时机 | 确认内容 |
|--------|------|---------|
| 🔔1 | 大纲后 | 整体结构/页数/章节划分 |
| 🔔2 | 详情后 | 每页具体内容和布局 |
| 🔔3 | 预览后 | 视觉效果和细节调整 |

---

## 3. 关键架构变更

### 3.1 Phase 1 输出增加 `style_profile`

```json
{
  "slides": [...],
  "theme": {...},
  "style_profile": {
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
    }
  }
}
```

### 3.2 Phase 4 风格映射 → 自动

**旧**: Phase 4 用户手动指定"第3页用模板第5页的布局"
**新**: 系统根据大纲类型自动匹配:
- 大纲封面 → 模板封面页 (slide 0)
- 大纲目录 → 模板目录页 (slide 1)
- 大纲内容 → 模板内容页 (slides 2..N-1, 按详细内容自动选最优)
- 大纲结尾 → 模板结尾页 (slide N)

### 3.3 删除 Strategist 8 项 BLOCKING 设计锁

**旧**: Phase 5 要求用户逐项确认颜色/字体/图表/图片/布局/页数/动画/特殊
**新**: 配色+字体从模板自动提取 (Phase 1), 图表/图片/布局由 LLM 在 Phase 3 自动生成, 用户只需在预览阶段看到效果

### 3.4 Phase 4 预览从"设计"升级为"实现"

**旧**: Phase 5 "逐页微调"含预览概念但从未实现
**新增**:
- 文字预览: 每页内容摘要 (Phase 3 完成后立即提供)
- 图片预览: SVG 转 PNG (确认详情后提供)
- 微调: 预览后可针对不满意的页单独调整

---

## 4. 文档更新范围

需更新以下文件:
- [ ] `ppt-agent-v2-fusion-design.md` §1 (融合目标) — 更新流程图
- [ ] `ppt-agent-v2-fusion-design.md` §2 (新增 Phase 详解) — 重写为 5 阶段
- [ ] `ppt-agent-v2-fusion-design.md` §6 (用户交互流程) — 更新为 3 确认点
- [ ] `ppt-agent-v2-fusion-design.md` §11 (设计决策记录) — 追加本次修正
- [ ] `SKILL.md` — 更新为 5 阶段流程
- [ ] `gsd-plan-v2.md` — 移除已删除的 Phase 4/5 任务

---

## 5. 不变量 (保留)

- ✅ 统一 SVG 引擎架构
- ✅ pptx_to_svg 桥接 + svg2pptx 合成
- ✅ Phase 0 多格式素材预处理 (可选)
- ✅ Phase 7 质量检查 (自动)
- ✅ Phase 8 后处理 (动画/旁白, 可选)
- ✅ Go 工作流状态机
- ✅ 内置 5 风格 + 自定义模板双入口
