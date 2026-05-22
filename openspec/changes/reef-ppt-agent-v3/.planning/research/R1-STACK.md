# R1 Stack Research

**Researcher**: R1-Stack subagent  
**Date**: 2026-05-22  
**Environment**: python-pptx 1.0.2, Pillow 12.1.1, cairosvg 2.9.0, no LibreOffice installed  

---

## Critical Gaps (must fix before P1)

### CG-1: PNG 预览方案不可行 — design.md 的降级链假设错误

**design.md 声称**: "PNG 优先, SVG 降级, HTML 最终降级"，T4.1 写 "Render layout_plan slide to PNG using Pillow/cairosvg"。

**事实**: Pillow 和 cairosvg 都**无法直接渲染 layout_plan**。它们是像素/矢量绘图库，不是 PPTX 渲染器。要从 layout_plan.json 得到视觉预览，只有两条路：

| 方案 | 可行性 | 时间 |
|------|--------|------|
| **A**: 先 PPTXBuilder 生成 .pptx → LibreOffice headless 转 PNG | ✅ 可行但慢 | 冷启动 ~8s + 每页 ~1-2s = 9页 ~20-25s |
| **B**: 自写 Pillow/cairosvg 渲染器，解析 layout_plan 手动绘制矩形/文字 | ⚠️ 可行但工作量巨大 | 开发 2-3 天，且渲染结果与 PowerPoint 不一致 |
| **C**: HTML 渲染（div+CSS 定位）→ Chromium 截图 | ✅ 可行 | 需装 Chromium/Puppeteer |

**关键问题**: 当前环境 **没有安装 LibreOffice**（`soffice` 不存在）。方案 A 需要额外安装 ~400MB 包。方案 B 的 Pillow 渲染器需要自己实现文本换行、字体度量、shape 渲染——本质上是重写半个 PPTX 渲染引擎。

**建议**: 
1. 降级链应改为 **HTML → (Chromium screenshot) → PNG**，而非 design.md 中的 PNG > SVG > HTML
2. HTML 渲染器是最现实的"第一级"：layout_plan → positioned divs + CSS → 浏览器截图 / 直接展示 HTML
3. 如果必须出 PNG，安装 LibreOffice headless，走 PPTX → PDF → PNG 路径
4. **T4.1 任务描述需重写**，当前 "Pillow/cairosvg" 方案是误导

### CG-2: python-pptx SmartArt 完全不支持，但 proposal 未标注

**实测**: `Presentation.slides.shapes` 没有 `add_smart_art` 方法。python-pptx 1.0.2 **无法创建 SmartArt**。

proposal.md 明确将 "视频/动画/SmartArt 转换" 列为 Out of scope，但 design.md 的 layout_plan schema 中 `type` 字段没有排除 `smartart`，T2.2 的 8 种内置版式中有 `flow_chart` 和 `timeline`——这两种版式在商业 PPT 中几乎必定使用 SmartArt 图形。

**影响**: 
- `flow_chart` 版式只能用 connector + rectangle 模拟，效果远不如原生 SmartArt
- `timeline` 版式同理，只能用 line + circle 手绘
- 如果用户参考 .pptx 中含 SmartArt，StyleExtractor 会遇到无法解析的 shape 类型

**建议**:
1. layout_plan schema 的 `type` 枚举中**显式排除** `smartart`，增加 `flow_chart_manual` / `timeline_manual` 类型
2. StyleExtractor 遇到 SmartArt shape 应跳过并记录 warning，不要尝试提取
3. 在 spec.md 中增加 Invariant: "SmartArt shapes are not supported; flow/timeline layouts use manual shape composition"

### CG-3: python-pptx connector 的 `begin_connect` / `end_connect` 在 PowerPoint 中不一定可编辑

**实测**: `add_connector` + `begin_connect(shape, site)` 在 python-pptx 中可以调用，但生成的 connector 在 PowerPoint 中打开后，连接点可能不"吸附"到目标 shape——因为 python-pptx 设置的是静态坐标，不是动态连接（OOXML 的 `<a:cxnSp>` 需要 `<a:stCxn>` / `<a:endCxn>` 子元素引用 shape ID + connection site index）。

python-pptx 1.0.2 的 `begin_connect` 实际上是设置坐标，**不是**设置 OOXML 连接关系。这意味着 connector 在 PowerPoint 中不会随 shape 移动而自动跟随。

**影响**: flow_chart 版式的 connector 在 PowerPoint 中手动调整 shape 位置后会断裂。

**建议**: 
1. 如果 flow_chart 是高频版式，考虑使用 `python-pptx` 的 `oxml` 层手动注入 `<a:stCxn>` / `<a:endCxn>` 元素
2. 或者放弃动态连接，在文档中注明 "connectors are static; moving shapes will not auto-reroute connectors"
3. 更好的替代：用 `add_shape(MSO_SHAPE.RIGHT_ARROW)` 代替 connector，视觉上更直观且不需要连接逻辑

---

## Important Risks (should address)

### IR-1: MSO_AUTO_SIZE 枚举值与 design.md 不一致

design.md 未提及文本自动调整策略，但 PPTXBuilder 必须处理文本溢出（overflow detection 是 T3.4 的核心功能）。

**实测**: python-pptx 1.0.2 的 `MSO_AUTO_SIZE` 枚举为：
- `NONE` — 不自动调整
- `SHAPE_TO_FIT_TEXT` — shape 随文本扩展
- `TEXT_TO_FIT_SHAPE` — 文本缩小以适应 shape
- `MIXED`

**注意**: 没有 `TEXT_TO_SHAPE_HEIGHT`（我之前测试用错了枚举值）。

**风险**: overflow detection 的 3 策略（shrink_title / split_to_two_slides / two_column）需要在代码中精确映射到 `MSO_AUTO_SIZE`。如果用错枚举值会直接报错。

### IR-2: StyleExtractor 解析 theme1.xml 的复杂度被低估

**技术细节**: 从 .pptx 提取颜色需要处理以下 XML namespace：

| Namespace | 用途 | 关键元素 |
|-----------|------|----------|
| `a` (drawingml) | 主题色定义 | `<a:clrScheme>`, `<a:srgbClr>`, `<a:schemeClr>` |
| `p` (presentation) | 幻灯片结构 | `<p:sp>`, `<p:txBody>` |
| `r` (relationships) | 资源引用 | `r:embed` (图片), `r:id` |
| `p14`/`p15` | 扩展属性 | 自定义动画/过渡 |

**颜色优先级问题**:
1. `<a:schemeClr val="accent1">` → 需查 `ppt/theme/theme1.xml` 的 `<a:clrScheme>` 解析为 RGB
2. `<a:srgbClr val="C00000">` → 直接 RGB，无需查主题
3. `<a:solidFill>` 内可能嵌套 `<a:schemeClr>` + `<a:lumMod>` / `<a:lumOff>` → 亮度调整
4. `<a:gradFill>` 渐变 → 需取 stops 的多个颜色

**T1.2 预估 4h 严重不足**。仅处理 `<a:schemeClr>` + 亮度调整 + 渐变就至少需要 1 天。河南移动模板的颜色提取验证（#C00000 primary, #0070C0 secondary）需要确认是直接 RGB 还是 schemeClr 引用。

**建议**: T1.2 拆为两个子任务：
- T1.2a: 直接 RGB 颜色提取（4h）
- T1.2b: theme1.xml schemeClr 解析 + 亮度调整（4h）

### IR-3: LLM API 调用预算 — token 量级估算

| 阶段 | 输入 token | 输出 token | 模型 | 估算成本 (GPT-4o) |
|------|-----------|-----------|------|-------------------|
| P1 大纲 | source.md ~3-8K + prompt ~1K = **4-9K** | outline.json ~1-2K | GPT-4o | $0.05-0.12 |
| P2 细化 | source.md + outline.json ~5-10K + prompt ~1K = **6-11K** | detail_plan.json ~3-8K | GPT-4o | $0.10-0.28 |
| P4-B 映射 | detail_plan + reference_lib ~5-10K + prompt ~1K = **6-11K** | mapping table ~0.5K | GPT-4o | $0.06-0.12 |

**总估算**: 3 次大调用，**15-31K input + 4.5-10.5K output**，成本 $0.21-0.52 / 次。

**延迟**: GPT-4o 3 次调用串行约 30-60s（含网络）。如果用户走 P4-B 自定义路径，P1+P2+P4-B 三次串行约 45-90s。加上 P5 预览渲染和 P6 合成，**5 分钟预算是紧但可行的**——前提是预览渲染不依赖 LibreOffice 冷启动。

**风险**: 如果 source.md 超过 20K tokens（长文档场景），P2 输入会膨胀到 25K+，成本翻倍。应设置 source.md 上限或分片策略。

### IR-4: 5 种内置 DSL 风格覆盖不足

**与市面 AI PPT 工具对比**:

| 风格 | v3 内置 | Gamma | Tome | MindShow | iSlide | 差距 |
|------|---------|-------|------|----------|--------|------|
| 商务蓝 | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| 科技红 | ✅ | ✅ | ❌ | ✅ | ✅ | — |
| 极简黑 | ✅ | ✅ | ✅ | ✅ | ✅ | — |
| 学术绿 | ✅ | ❌ | ❌ | ❌ | ❌ | v3 独有优势 |
| 活力橙 | ✅ | ❌ | ❌ | ❌ | ❌ | v3 独有优势 |
| **渐变/霓虹** | ❌ | ✅ | ✅ | ❌ | ❌ | **缺失** |
| **插画/手绘** | ❌ | ✅ | ✅ | ❌ | ❌ | **缺失** |
| **中国风** | ❌ | ❌ | ❌ | ✅ | ✅ | **缺失** |
| **毛玻璃/新拟态** | ❌ | ✅ | ❌ | ❌ | ❌ | **缺失** |

**关键缺失**:
1. **渐变/霓虹风格** — 这是 Gamma 最受欢迎的风格，年轻人/互联网场景高频使用
2. **中国风** — MindShow 和 iSlide 都有，河南移动案例就是国企场景，中国风是刚需
3. **插画/手绘风** — 教育/培训场景高频

**建议**: v3 P1 至少补充 **中国风** 和 **渐变科技** 两种内置 DSL，将内置风格从 5 扩到 7。

### IR-5: python-pptx table 样式控制有限

**实测**: `add_table(rows, cols, ...)` 返回的 `Table` 对象可以设置单元格文本和基本格式，但：
- 无法设置表格整体样式（如 "Medium Style 2 – Accent 1"）——python-pptx 没有 `table.style` 属性映射到 OOXML 的 `<a:tblStyle>`
- 单元格边框需要逐个设置 `cell.margin_left` 等，无法批量应用表格主题
- 合并单元格后，被合并的 cell 仍存在但内容为空，容易导致迭代错误

**影响**: detail_plan 中 `type: "table"` 的 content_block 在 PPTXBuilder 中生成的表格会很朴素（白底黑字无边框），与商业 PPT 的精致表格差距大。

**建议**: 
1. 在 PPTXBuilder 中为 table 类型预置 3 种表格样式（通过 oxml 层注入 `<a:tblStyle val="{guid}">`）
2. 或在 layout_plan 的 `style_ref` 中增加 `table_style` 字段

---

## Recommendations

### R1: 重写预览渲染方案

将 T4 拆为：
- **T4.1**: HTML 渲染器（layout_plan → positioned HTML+CSS）— 这是唯一不依赖外部软件的方案
- **T4.2**: Chromium/Puppeteer 截图（HTML → PNG）— 可选，需要安装 Chromium
- **T4.3**: LibreOffice 渲染（PPTX → PDF → PNG）— 可选，需要安装 LO headless

降级链改为：**HTML (always available) > Chromium PNG > LibreOffice PNG**

### R2: 增加内置风格到 7 种

P1 补充：
- **中国风**: primary=#8B0000, secondary=#D4A017, accent=#F5F5DC, fonts={title: "思源宋体", body: "思源黑体"}, decor={accent_bar: "云纹", card_radius: 0}
- **渐变科技**: primary=#6366F1, secondary=#8B5CF6, accent=#E0E7FF, decor={accent_bar: "渐变条", card_radius: 12}

### R3: StyleExtractor 分阶段交付

- **MVP (P1)**: 仅提取直接 RGB 颜色 + 字体名 + 字号 → StyleProfile
- **V2 (P2)**: 增加 theme1.xml schemeClr 解析 + 亮度调整
- **V3 (P3)**: 增加渐变填充 + 装饰元素模式识别

### R4: source.md 设置 token 上限

在 P0 阶段增加截断逻辑：如果 source.md 超过 15K tokens（约 20K 中文字符），自动摘要到 15K 以内，保留原文供 P2 按需回查。

### R5: connector 替代方案

flow_chart 和 timeline 版式中，用 `MSO_SHAPE.RIGHT_ARROW` / `MSO_SHAPE.DOWN_ARROW` 代替 connector。箭头 shape 更直观，且不存在连接点问题。

---

## Open Questions for User

1. **预览渲染优先级**: 是否可以接受 HTML 预览作为唯一方案（不做 PNG）？HTML 预览在飞书/Telegram 中无法直接展示图片，但可以发链接或截图。如果必须 PNG，需要确认环境是否允许安装 LibreOffice headless（~400MB）或 Chromium（~200MB）。

2. **SmartArt 替代方案确认**: flow_chart / timeline 版式用手工 shape 组合（rectangle + arrow）替代 SmartArt，用户是否接受？商业 PPT 中 SmartArt 是高频需求，但 python-pptx 无法创建。

3. **中国风字体依赖**: 中国风 DSL 需要 "思源宋体" / "思源黑体"，这些字体在服务器环境通常未安装。是否允许 font fallback（如 fallback 到 "SimSun" / "Microsoft YaHei"）？或者需要捆绑字体文件？

4. **LLM 模型选择**: 当前 token 预算基于 GPT-4o 定价。是否考虑用 GPT-4o-mini 做 P1（大纲生成对质量要求较低）以降低成本？P1 用 mini 可节省 ~60% 成本。

5. **表格样式最低标准**: PPTXBuilder 生成的表格是否需要接近商业级样式（带主题色、交替行色、边框）？如果是，需要额外 4-6h 开发 oxml 层表格样式注入。如果"能用就行"，当前 python-pptx 默认样式可接受。
