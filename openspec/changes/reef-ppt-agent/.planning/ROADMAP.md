# Reef PPT Agent v2.3 — Roadmap

> 创建: 2026-05-18 | 修订: 2026-05-21 (v2.3: 母版修复 + 空间分组 + 容器克隆)
> 基于: FINAL-DESIGN.md (v2.3 最终设计)
> GSD 计划: gsd-plan-v3.md (28 tasks, ~30.5h, ~4 工作日)
> 当前状态: 🟡 S1-S3 ✅ (41 tests), P0 + S4 ⬜ (8 tasks / ~6.5h)

## v2.3 架构增强 (2026-05-21)

```
AI 负责: "理解模板" + "决定什么内容放哪里" + "识别容器扩展需求"
引擎负责: "保留母版" + "空间分组" + "容器克隆" + "完美保留样式"

v1 克隆引擎增强:
  P0: shutil.copy — 保留 slide masters/layouts/theme（v2.2 丢失）
  L1: detect_template_patterns — 空间分组，AI 看组件树而非扁平列表
  L2: layout_planning.md — 容器感知匹配 + clone_group 行动说明
  L3: expand_container_group — 深拷贝+偏移容器组，支持动态内容扩展
```

## 里程碑

| 里程碑 | 版本 | 核心成果 | 状态 |
|--------|------|------|:---:|
| M1: v1.0 MVP | 2026-05-15 | 模板解析 + python-pptx 克隆引擎, 108 tests | ✅ |
| M2: v2.0 Full | 2026-05-18 | SVG 引擎 + 素材预处理 + 风格选择 + 质量检查 | ✅ |
| M3: v2.1 需求对齐 | 2026-05-19 | 5 阶段 + 3 确认点 + 自动映射, 280 tests | ✅ |
| M4: v2.2 AI 分析层 | 2026-05-20 | AI 模板深度分析 + layout_plan 驱动引擎, 361 tests | ✅ |
| M5: v2.3 保真度修复 | **2026-05-21** 🔥 | 母版保留 + 空间分组 + 容器克隆, ~377 tests | ✅ |

## GSD v3.1 计划概览

| Phase | Name | Tasks | Hours | Tests | Status |
|:-----:|------|:-----:|:-----:|:-----:|:------:|
| P0 | 🔥 母版保留修复 | 2 | 0.5h | 4 | ⬜ |
| S1 | AI 分析 + 克隆引擎 | 9 | 12h | 23 | ✅ |
| S2 | 预览管道 | 5 | 6h | 12 | ✅ |
| S3 | 溢出 + 质量门 | 6 | 6h | 6 | ✅ |
| S4 | 🔥 空间分组 + 容器克隆 | 6 | 6h | 12 | ⬜ |
| **Total** | | **28** | **~30.5h** | **~57** | |

## 执行顺序

```
Wave 7 (v2.3 P0 — 先行):
  P0.1: compose_with_layout_plan() → shutil.copy 方案
  P0.2: 母版保留集成验证

Wave 8 (v2.3 S4 — 依赖 P0 和 S1):
  S4.A.1: detect_template_patterns() 空间分组实现
  S4.B.1: expand_container_group() 容器克隆实现
  S4.C.1: layout_planning.md prompt 升级

Wave 9 (v2.3 测试):
  S4.A.2+: 空间分组测试
  S4.B.2: clone_group 测试
  S4.C.2: E2E 全链路 3→4 card 扩展
```
