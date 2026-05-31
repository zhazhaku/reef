# PPT Agent v3 Roadmap

> v3.0 GA: 62.5h | v3.1 后续: ~17h | 总: ~80h

## v3.0 GA — P0 修复 + 7 阶段全链路 (62.5h)

### Phase A: Engine Foundation (M1, ~20h)

- T1 StyleExtractor: 4 级 style_ref 解析 + 白名单 + clrScheme fallback (7.5h)
- T2 LayoutComposer: 命名空间生成 + 几何验证 + auto_grid 降级 + LLM schema-lock (11h)
- T6 Built-in Styles: 5 内置样式 DSL + theme fallback (4.5h, 可与 T1 并行)

### Phase B: Output & Preview (M2+M3, ~18h)

- T3 PPTXBuilder: 新建表格 + 主题注入 + 完整性校验 + CJK 字体子集 (12h)
- T4 Preview Engine: SVG 主路径 + LO 池 + HTML 兜底 (6h)

### Phase C: Pipeline & UX (M4, ~12.5h)

- T5 Pipeline Integration: session 持久化 + LLM 降级 + 5 确认点 + refinement 收敛 + 超时归档 (12.5h)

### Phase D: Quality (M5, ~9h)

- T7 Tests: LLM fixture + schema 验证 + 几何边界 + 完整性 + checkpoint resume (9h)
- 河南移动 PPT 复测 + 灰度

## v3.1 后续 (~17h)

- T0 多源解析器 pdf/xlsx/url (6h)
- 内置样式扩展到 10 种 (3h)
- 并发/多用户隔离 (4h)
- 结构化日志体系完整化 (2h)
- 跨平台字体差异处理 (2h)

## 里程碑

| M | 内容 | 累计工时 |
|---|------|----------|
| M1 | Engine 可独立产出合规 layout_plan.json | 20h |
| M2 | PPTXBuilder 输出可打开 .pptx | 32h |
| M3 | 预览三级降级链路打通 | 38h |
| M4 | 5 确认点 + resume + LLM 降级全链通 | 50.5h |
| M5 | 测试通过 + 河南 PPT 复测 OK = GA | 62.5h |

## 关键决策回顾

- Q1 预览主路径 = **A** (SVG 优先, PNG 降级)
- Q2 表格策略 = **A** (新建表格, 不改模板)
- Q3 工时上限 = **C** (v3.0/v3.1 分期, v3.0=62.5h)
- Q4 多源解析器 = **B** (延后到 v3.1)
- Q5 内置样式数量 = **A** (5 种)

详见 `.planning/DECISIONS.md` D9-D15 和 `.planning/GAP-REPORT.md`。
