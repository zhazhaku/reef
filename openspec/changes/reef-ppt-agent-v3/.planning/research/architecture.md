# R3 — Architecture Self-Audit

> 2026-05-22 self-review against proposal.md / design.md / tasks.md / spec.md (all v3).
> Methodology: enumerate each architectural seam, ask "what breaks if X happens", grade severity (P0 block / P1 must-fix / P2 should-fix / P3 nice).

## A. 引擎边界 (StyleExtractor / LayoutComposer / PPTXBuilder)

### A1. ❌ P0 — `style_ref` 解析顺序未定义
**问题**: design.md 写 "style_ref 字段注入"，但同一个 ref 可能在 inline override / reference_lib / 内置 DSL 三处出现。当前文档未规定优先级。
**反例**: 用户上传自定义模板 → `reference_lib.json` 给出 `primary: #C00000`，同时 `style_decision.json` 写 `mode: built_in, style_name: 商务蓝` (其 primary 是 #0070C0)，layout_plan inline 又写 `color: #000000`。PPTXBuilder 取哪个？
**修复**: 在 design.md §数据契约下新增「解析优先级」小节: inline override > reference_lib (custom 模式) > built_in DSL > slide_type 默认。在 spec.md 增 INV-6。
**Owner Task**: T1.5 新增

### A2. ❌ P0 — `shape_id` 命名空间未定义
**问题**: design.md schema 用 `"shape_id": "s1_title"`，看起来全局唯一，但没规定生成规则；T2 LayoutComposer 单元测试无法约束。
**反例**: 同一份 plan 重命名/插页/拆页 (overflow) 后 shape_id 是否要重排？refinement 子循环需要按 shape_id diff，重排即破坏 diff 语义。
**修复**: 规定 shape_id = `s{plan_slide}_{role}_{seq}` (e.g. `s3_card_1`, `s3_card_2`)，overflow split 时分配新 plan_slide 数字，旧 id 不复用。写入 spec.md INV-7。
**Owner Task**: T2.1 增加 id 生成器

### A3. ⚠ P1 — LayoutComposer 「内置版式库」与「自定义参考」共用 layout_plan 但实现路径分裂
**问题**: T2 写了 5 tasks 但没说两条路径如何复用代码。容易写成两套 if-else。
**反例**: 内置走 `BuiltinLayoutPicker.select(slide_type, blocks)`，自定义走 `ReferenceBorrowingPlanner.borrow(profile, blocks)` — 两者各自维护「avoid 超过 4 个 card」「title 居左还是居中」启发式。改一处必然漏另一处。
**修复**: 抽出 `LayoutSpec` 中间表示 (= 一组 region + role 约束)，两条路径都先产 LayoutSpec 再走同一个 `LayoutRenderer.spec_to_shapes(spec, blocks, style_ref)`。
**Owner Task**: T2 重组 — 拆为 T2.a built-in picker / T2.b reference borrower / T2.c spec renderer (共用)

### A4. ⚠ P1 — PPTXBuilder 与 python-pptx 的样式继承反向
**问题**: 我们「从空白 Presentation()」起 → 默认 master/layout 都是 python-pptx 自带的英文模板 (Calibri 18pt)。而我们注入的 style_ref 只走 run-level rPr。
**反例**: 字体生效，但段落级 (pPr) 行距/缩进、theme 级配色未覆盖 → 用户在 PPT 里改主题色，我们的字会跟着乱变；切到深色模板时白底黑字变黑底黑字。
**修复**:
1. PPTXBuilder 启动时根据 style_decision 选/造 master + theme (theme1.xml 覆盖主色)
2. 每个 shape 不仅写 rPr，还写 pPr + spPr (shape properties)
3. 文档化「为什么不能直接 blank Presentation」
**Owner Task**: T3 加 T3.6 — theme/master 注入

### A5. ⚠ P1 — StyleExtractor 输出 schema 在 design.md 与 spec.md 不一致
**design.md**: `{colors: {primary, secondary, accent, bg, text}, fonts: {title, body}, decor: {accent_bar, card_radius, icon_style}}`
**spec.md Spec 5 (style mapping)**: 未列字段。
**问题**: 实现时以哪个为准？decor.accent_bar 是什么类型 (布尔/EMU/RGB)？
**修复**: spec.md 增 Spec 7 — StyleProfile schema + 字段类型 + 缺省值。
**Owner Task**: T1.4 (schema validation)

## B. 状态机 / 失败恢复

### B1. ❌ P0 — 7 阶段无状态持久化策略
**问题**: design.md / tasks.md 没说每个 phase 产物如何持久化、如何在中断后恢复。如果 P5 预览生成到第 7 页时崩了，重启从 P0 还是 P5？
**反例**: 用户在 P5 拒绝第 3 页，要求只重做第 3 页 → 我们需要保留 P0-P4 的全部产物 + 只重跑 LayoutComposer(slide=3) + PPTXBuilder(slide=3) + Preview(slide=3)。当前无此机制。
**修复**:
1. work/ 目录定义为 session-scoped, 每 phase 完成 atomic write JSON
2. 增 work/session.json 记录 {phase_completed, last_action, slides_dirty: [3]}
3. CLI 加 `--resume <session_dir>`
4. refinement 子循环走 dirty-slide 增量
**Owner Task**: T5 加 T5.5 — session manager；spec.md 增 Spec 8 — Resume Semantics

### B2. ⚠ P1 — LLM 调用失败的重试/降级未规定
**问题**: spec.md EH-1 只说「LLM timeout → 重试 N 次后报错」，N 是几？指数退避？失败时是否 fallback 到本地启发式 (e.g. outline 用 source.md 标题层级直出)？
**反例**: P1 outline 调用 LLM 超时 3 次 → 用户等了 90s 看到 "失败"，但 source.md 明明有完整 H1/H2，本地完全能出 outline。
**修复**: spec.md EH-1 细化: 3 次重试 (1s/4s/16s 退避) → 落 fallback heuristic (P1: 按 H1 出 outline；P2: 不重写直接拷贝段落；P3: 默认商务蓝；P4-A: 默认 layout 库；P4-B: 拒绝，要求用户选 built-in)。
**Owner Task**: T5.6 LLM 客户端封装 + heuristic fallback

### B3. ⚠ P1 — 确认点超时无策略
**问题**: 5 个确认点都是阻塞等用户。如果用户睡了 8 小时不回，session 怎么办？feishu 上下文还有意义吗？
**修复**: 1h 无响应 → 自动落「pending」状态，保留 session，下次用户消息触发 resume；24h 无响应 → 归档。
**Owner Task**: T5 加 T5.7 — confirmation timeout policy

### B4. ⚠ P2 — overflow 自动修复链与质量门的死循环风险
**问题**: P6 写 "溢出检测: 3 策略自动修复"，但 spec.md EH-2 "失败 3 轮落降级"，未说每轮失败后用什么策略：是同一策略 shrink_title 跑 3 次，还是 shrink → split → two_column 轮换？
**反例**: shrink_title 把 40pt 压到 20pt 还是溢出，再压到 10pt 不可读 — 三次都「检测通过」实际惨不忍睹。
**修复**: 明确策略序列: round 1 = shrink_title(min 28pt) → round 2 = two_column → round 3 = split_to_two_slides；每轮失败必须换策略，不可重复。
**Owner Task**: T3.4 overflow handler 流程图

## C. 并发 / 性能

### C1. ⚠ P1 — LibreOffice headless 并发未规划
**问题**: P5 预览 PNG 走 LibreOffice。15 页一次性渲染串行 ≈ 15×3s = 45s。design.md 写「5 分钟预算」勉强，但 refinement 子循环 3 轮就破 5 分钟。
**修复**: LO 进程池 size=4 + per-slide PNG (而非整 deck) — 增量 refinement 只渲被改的页。
**Owner Task**: T4.4 进程池

### C2. ⚠ P2 — 多 session 并发
**问题**: 同一 agent 同时给两个用户做 PPT，work/ 目录怎么隔离？LO 池怎么排队？
**修复**: work/<session_id>/ 隔离；LO 池全局共享，FIFO 排队。
**Owner Task**: T5.5 (与 B1 合并)

## D. 可测试性

### D1. ❌ P0 — 三个引擎缺确定性测试夹具
**问题**: tasks.md T7 写 "5 tests"，但 LayoutComposer / PPTXBuilder 的输入是 LLM 输出，不可重复。
**修复**: 提供 fixture 目录 `tests/fixtures/{outline,detail_plan,style_decision,layout_plan}/<scenario>.json`，绕过 LLM 直跑引擎；每个引擎至少 5 fixture (cover/toc/data_card/three_column/two_column)。
**Owner Task**: T7 重写为: T7.1 fixture 库 / T7.2 StyleExtractor 单测 / T7.3 LayoutComposer 单测 / T7.4 PPTXBuilder 单测 / T7.5 端到端 (含 LLM mock) / T7.6 河南 PPT 回归

### D2. ⚠ P1 — quality gate 自身无测试
**问题**: FONT-INHERITANCE 怎么算「通过」？没有 ground truth。
**修复**: 每个 gate 函数 = 纯函数 `gate(pptx_path) -> List[Violation]`，给已知好/坏样本各 3 份做 pytest。
**Owner Task**: T7.7

## E. 后向兼容 / 迁移

### E1. ⚠ P1 — v2.3 用户的 layout_plan.json 不能直接用
**问题**: v2.3 的 `target_shape_idx` 在 v3 不存在。老用户的 session 重启即报错。
**修复**: 提供 `migrate_v2_to_v3.py` 一次性脚本: 读 v2.3 plan + 模板 → 抽 shape 坐标/样式 → 转 v3 shapes[]。保留 v2.3 `compose_with_layout_plan` 一个 release 标 deprecated。
**Owner Task**: T5.8 migration shim

### E2. ⚠ P2 — picoclaw 集成路径未列
**问题**: proposal.md 提了 picoclaw，design/tasks 没说 v3 怎么注册成 skill / 怎么调用。
**修复**: tasks.md 加 T5.9 — SKILL.md 重写为 v3 入口；保留 v2.3 SKILL.md 一个 release。
**Owner Task**: T5.9

## F. 观察性

### F1. ⚠ P2 — 全流程无日志/trace 规范
**问题**: 7 阶段 + 3 引擎 + LLM 调用，出问题不知道在哪一环。
**修复**: 每 phase 写 work/log/<phase>.jsonl (start/end/duration/token_usage/model)；CLI 加 `--trace` 打开 verbose。
**Owner Task**: T5.10 logging

## G. 用户体验 (确认点设计)

### G1. ⚠ P2 — 5 个确认点疲劳
**问题**: #1 outline / #2 detail / #3 style / #3.5 reference mapping / #4 preview / #5 download — 用户每次都要等并回 6 次。原本「自动化」的承诺打折。
**修复**:
- 快速通道: 用户在 P1 加 `[全自动]` 标签 → 默认通过 #2/#3/#3.5/#4，仅在质量门失败或溢出无解时回 #4。
- 默认行为不变 (照旧问)，只是给老手开口。
**Owner Task**: spec.md 加 Spec 9 — Confirmation Modes (interactive | fast-track)

### G2. ⚠ P3 — 预览交互粒度
**问题**: #4 preview 用户说「第 3 页第二段太长」，agent 怎么映射回 detail_plan.json？文本匹配？shape_id？
**修复**: 预览图叠加 shape_id 标签 (调试模式)；refinement_prompt 输入用户原话 + shape_id 列表，强制 LLM 输出 patch 形如 `{shape_id: "s3_card_2_body", new_text: "..."}`。
**Owner Task**: T4.3 preview 增 shape_id overlay；T5.6 refinement 走 patch 模式

## H. 数据契约缺口

### H1. ❌ P0 — `content_key` 命名空间未定义
**问题**: design.md schema 里 layout_plan.shapes[].content_key 引用 detail_plan.content_blocks，但没说键名规则。LLM 自由发挥就会冲突。
**反例**: detail_plan slide 3 有 content_blocks: [{type:title, ...}, {type:body, ...}, {type:body, ...}] — content_key 是 `body`?`body_1`?`body_2`? layout_plan 引用 `body` 应取第几条？
**修复**: 规定 detail_plan 必须为每个 block 写 `key: title | subtitle | body_1 | body_2 | bullet_list_1 | image_1 | table_1` (类型 + 序号，全 slide 内唯一)。spec.md INV-8。
**Owner Task**: T5.3 + spec.md

### H2. ⚠ P1 — `source.md` 中间格式未规范
**问题**: P0 输出 source.md，但 docx/pdf/xlsx/url 各自有结构信息 (表格/图片引用/层级)，统一到 markdown 后丢什么留什么？
**修复**: 规定 source.md 必须保留 ATX heading (#)、表格 (pipe table)、图片占位 `![key](path)`、列表 (- / 1.)。其余 (footnotes/inline html) 转纯文本。
**Owner Task**: T5.1 — multi-source parser 加测试

### H3. ⚠ P2 — `images_dir` 资源管理
**问题**: P0 抽出的图片 / 用户上传的图片 / built-in style 的装饰图，放哪里、命名规则、复用策略？
**修复**: work/images/{auto/<src>_<hash>.png, user/<name>, builtin/<style>/<id>.svg}；layout_plan 引用走 image_key (相对 work/images/)。
**Owner Task**: T5.1 + T3.5

## 汇总

### P0 阻塞 (必须开工前补)
- A1 style_ref 解析优先级
- A2 shape_id 命名规则
- B1 状态持久化 + resume
- D1 引擎测试夹具策略
- H1 content_key 命名空间

### P1 必须修
- A3 LayoutComposer 双路径共用 LayoutSpec
- A4 PPTXBuilder theme/master 注入
- A5 StyleProfile schema 落 spec
- B2 LLM 重试 + heuristic fallback
- B3 确认点超时
- C1 LO 进程池
- D2 quality gate 测试
- E1 v2.3 迁移脚本
- H2 source.md 规范

### P2 应修
- B4 overflow 策略序列
- C2 多 session
- E2 picoclaw 集成
- F1 trace/log
- G1 fast-track 模式
- H3 资源管理

### P3 可选
- G2 预览 shape_id overlay

### 对 tasks.md 的增量
**新增任务** (估时):
- T1.5 style_ref resolver (1h)
- T2 重组为 T2.a/b/c (+2h)
- T3.6 theme/master 注入 (2h)
- T5.5 session manager + resume (3h)
- T5.6 LLM 客户端 + fallback (2h)
- T5.7 confirmation timeout (1h)
- T5.8 v2→v3 migration (2h)
- T5.9 SKILL.md 重写 (1h)
- T5.10 logging/trace (1h)
- T7.1-T7.7 重写 (原 5h → 10h)

**总增时**: 原 30h → 修订 ~45h
**新依赖**: 无外部 lib 增加；fixture 库需手工构造

### 对 spec.md 的增量
- INV-6: style_ref 解析优先级
- INV-7: shape_id 命名规则
- INV-8: content_key 命名空间
- Spec 7: StyleProfile schema
- Spec 8: Resume Semantics
- Spec 9: Confirmation Modes
- EH-1 细化 LLM 重试链
- EH-2 细化 overflow 策略序列
- EH-5 新增: 确认点超时

### 自审结论
**架构骨架 OK**，但 5 个 P0 缺口必须在 T1 开工前补齐，否则一边写代码一边定 spec 会重蹈 v2.3 覆辙。建议下一步: 把 P0 项写入 design.md + spec.md，更新 tasks.md，再进入实现。

### 未覆盖维度 (留给其他研究)
- 外部 lib 选型 (instructor / outlines / pdfplumber 替代) → R1-Stack
- 竞品 feature gap (备注/品牌化/图表/多语言) → R2-Features
- LLM 输出陷阱 (style_ref 幻觉 / 中文字号溢出) → R4-Pitfalls
