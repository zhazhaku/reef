# v3 Decisions Log (Merged)

> 截止 2026-05-22 07:42。后续每次决策追加在底部，标日期。

## D1 — 默认模式
- **决策**: 默认 = **借鉴模式 (reference/borrow)**；strict-slot-fill 模式仅作 opt-in
- **来源**: 用户原话「样式是参考，不是严格填槽」
- **影响**: P4 LayoutComposer 不再用 `target_shape_idx` 锁死模板形状

## D2 — 预览强制
- **决策**: P5 预览为**强制**确认点（confirmation #4），不可跳过；refinement 子循环上限 **3 轮**
- **降级链**: PNG（LibreOffice）→ SVG（python-pptx 自渲染）→ HTML（fallback）
- **影响**: 必须预留 LibreOffice headless 环境

## D3 — 输出可编辑性
- **决策**: 最终 PPTX **每个元素必须独立可编辑**；禁止整页图片
- **影响**: PPTXBuilder 从空白 Presentation() 起逐 shape 创建；不可走 PDF→图片→嵌入捷径

## D4 — 引擎三分
- **决策**: StyleExtractor / LayoutComposer / PPTXBuilder 三段式
- **边界**:
  - StyleExtractor: 仅产 `reference_lib.json`（颜色/字号/字体配对/布局指纹）
  - LayoutComposer: 仅产 `layout_plan.json v3`（shapes 数组 + style_ref 指针）
  - PPTXBuilder: 消费 layout_plan，产 `.pptx` + 跑质量门
- **影响**: T1-T3 可独立单测

## D5 — 7 阶段 + 5 确认点
- P0 Input → P1 Outline(🔔#1) → P2 Detail(🔔#2) → P3 Style(🔔#3) → P4 Layout(🔔#3.5 仅自定义) → P5 Preview(🔔#4 强制) → P6 PPTX(🔔#5)
- 状态: 已写入 proposal.md / design.md / spec.md

## D6 — 内置风格 5 套
- 商务蓝 / 科技红 / 极简黑 / 学术绿 / 活力橙
- 以 DSL（JSON）定义颜色 + 字号 + 8 种布局模板
- 状态: design.md §3 含 DSL 占位，**完整 DSL 内容待 T6 阶段落地**

## D7 — 质量门 4 维
- FONT-INHERITANCE / COLOR-CONSISTENCY / CONTENT-COMPLETENESS / TEXT-OVERFLOW
- 连续 3 轮未通过 → 落「降级输出」并标 warning，不阻塞返回

## D8 — overflow 3 策略
- shrink_title / split_to_two_slides / two_column
- **新增策略需走 T3 扩展点**（待 T3 设计扩展接口）

---

## 待决策（需要研究输入）

### Q1 — style_ref 解析顺序
候选: ① reference_lib.json 优先 → 内置 DSL fallback ② 反过来 ③ inline override 永远最高优先
**当前倾向**: ③ + ① + ② 三级
**阻塞**: 等 R3-Architecture 报告

### Q2 — shape_id 命名空间
候选: 页内唯一 (`shape_3`) vs 全局唯一 (`slide_5_shape_3`)
**当前倾向**: 全局唯一，方便跨页 diff 增量重建
**阻塞**: 等 R3 报告

### Q3 — 结构化 LLM 输出
候选: instructor / outlines / json-mode / function-calling / 裸 JSON + schema 校验
**当前倾向**: 未定
**阻塞**: 等 R1-Stack 报告

### Q4 — v2.3 后向兼容
候选: ① 保留 `compose_with_layout_plan()` 作为 legacy ② 提供迁移脚本一次性转换 ③ 完全删除
**当前倾向**: ① + ②，给老用户至少 1 个 release 窗口
**阻塞**: 等 R3 报告

### Q5 — 并发模型
候选: ① 单进程串行 ② LibreOffice 进程池 ③ 全异步 (asyncio + uno)
**当前倾向**: ② (LO 池 size=2-4)
**阻塞**: 等 R1 报告对 LO 并发的实测数据

### Q6 — 缺失功能补不补
候选功能: 智能配图 / 数据→图表 / 演讲备注 / 一键品牌化 / 增量重建 / 多语言字体配对
**当前倾向**: P0 必须补「演讲备注」「品牌化」；其余 P1/P2
**阻塞**: 等 R2-Features 竞品对比

### Q7 — 状态机持久化
候选: ① 每 phase 落盘 JSON ② SQLite session table ③ 不持久化（中断重来）
**当前倾向**: ① 落盘（已有 work/ 目录结构）
**阻塞**: 等 R3 报告

---

## 决策追踪规则
- 任何「待决策」项有研究输入后 → 移入「已决策」并标日期
- 已决策项要改动 → 必须开新条目 D(N+1) 并写 supersedes D(X) 注脚
- proposal.md / design.md / spec.md / tasks.md 改动必须先在此登记


---

## D9-D15 关闭原 Q1-Q7 (2026-05-22, 依据 GAP-REPORT)

### D9 — style_ref 解析顺序 (closes Q1)
**决策**: inline override > reference_lib > built_in DSL > slide_type 默认
- inline = layout_plan.shapes[].style_ref 写字面对象
- reference_lib = 自定义模板路径下的 reference_lib.json
- built_in DSL = 5 套内置风格的 JSON
- 默认 = slide_type 决定的兜底
- 引用键必须在 style_decision.allowed_refs[] 白名单内 (走 instructor schema-locked)

### D10 — shape_id 命名空间 (closes Q2)
**决策**: 全局唯一 `s{plan_slide}_{role}_{seq}` (e.g. `s3_card_1`, `s3_card_2`)
- overflow split 分配新 plan_slide 数字, 旧 id 不复用
- refinement 走 shape_id diff (增量 patch)

### D11 — 结构化 LLM 输出 (closes Q3)
**决策**: 走 `instructor` (or `outlines`) — schema-locked, 不裸 JSON-mode
- 所有 LLM 调用必须有 Pydantic 模型
- enum 字段强制白名单 (slide_type / shape_type / action / style_ref 键)

### D12 — v2.3 后向兼容 (closes Q4)
**决策**: 保留 `compose_with_layout_plan()` 一个 release, 标 deprecated
- 提供 `migrate_v2_to_v3.py` 一次性转换脚本
- 老 SKILL.md 保留 1 release, 新版默认走 v3

### D13 — 并发模型 (closes Q5)
**决策**: LibreOffice 进程池, size 动态: `min(4, available_mem/300MB)`
- OOM 降到 1 + 串行
- per-slide 渲染 (非整 deck), 增量 refinement 只渲 dirty slide
- work/<session_id>/ 隔离多用户

### D14 — 缺失功能优先级 (closes Q6)
**决策** (按 P0/P1/P2 分级):
- **P0 必补** (T0/T3 阶段): 多源 parser 实现 / 表格 shape (row/col/merge) / 图片 shape (fit/crop) / 演讲备注
- **P1** (T3.7): 中文排版默认值 / 字宽估算 / 字体回退链
- **P2** (post-v3): 一键品牌化 / 增量重建 / 数据→图表 / 多语言字体配对 / 智能配图

### D15 — 状态持久化 (closes Q7)
**决策**: 每 phase 完成 atomic write JSON 到 work/<session>/
- work/<session>/session.json: {phase_completed, last_action, slides_dirty: []}
- CLI `--resume <session_dir>` 从断点续
- refinement 走 dirty-slide 增量
- 1h 无响应 → pending; 24h → 归档

---

## 决策完整性
- D1-D8: 初始决策
- D9-D15: 由 GAP-REPORT 关闭原 Q1-Q7
- **共 15 条已决策, 0 待决** → 进入实现 ready
