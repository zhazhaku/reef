# GAP-REPORT — v3 Design Gap Analysis (R1+R2+R3+R4 Merged)

> 2026-05-22 | Sources: R1-STACK.md / R2-FEATURES.md / R3-architecture.md / R4-pitfalls.md
> Method: 4 independent research passes → deduplicate → rank by severity → assign owner task

---

## P0 — 阻塞开工 (必须补齐后才能写第一行实现代码)

| # | Issue | Source | Impact | Owner |
|---|---|---|---|---|
| G0-1 | **预览方案不可行**: design.md 写「PNG(LibreOffice)→SVG→HTML」降级链，但 python-pptx 从空白 Presentation() 构建的 deck 无 master/layout → LO headless 渲染空白页 | R1-CG1 | P5 预览阶段全黑，用户无法确认 | T4 重写 |
| G0-2 | **style_ref 解析优先级未定义**: inline / reference_lib / built-in DSL 三处可出现同一键，PPTXBuilder 取哪个？ | R3-A1 | 样式随机取值，输出不可预测 | T1.5 新增 |
| G0-3 | **shape_id 命名空间未定义**: 无生成规则 → overflow/refinement 无法做 diff | R3-A2 | 增量重建断裂 | T2.1 |
| G0-4 | **LayoutComposer 坐标越界/重叠**: LLM 自由输出 EMU 坐标，不懂 16:9 边界 | R4-1.2 + R2-CG1 | shape 出画布或叠成一团 | T2 重构 |
| G0-5 | **content_key 命名空间未定义**: detail_plan blocks 无 key 规则 → layout_plan 引用歧义 | R3-H1 | 内容映射错位 (v2.3 老问题复现) | T5.3 |
| G0-6 | **状态持久化 + resume 缺失**: 7 阶段无 session 管理，中断即从头 | R3-B1 | 用户 P5 崩溃后白干 | T5.5 |
| G0-7 | **style_ref 幻觉**: LLM 输出不存在于 reference_lib / DSL 的键 | R4-1.1 | 静默走默认色，全员同色 | T1.5 + T2.7 |
| G0-8 | **CJK 字体环境**: 容器缺微软雅黑/思源 → LO 预览方块字 | R4-2.1 | 预览不可用 | T3.7 + 部署 |
| G0-9 | **表格映射断裂**: detail_plan 有 table block，但 layout_plan v3 schema 无 table 行列/合并定义 | R2-CG2 | 表格内容无法渲染 | T3 + schema |
| G0-10 | **多源预处理 (P0) 无实现**: 5 种 parser (docx/pdf/xlsx/url/md) 全是占位 | R2-CG3 | P0 输入阶段直接失败 | T5.1 或新 T0 |
| G0-11 | **.pptx 损坏/密码检测**: 用户上传非标准文件 → python-pptx 崩 | R4-4.2 | P0 无友好错误 | T1.6 |
| G0-12 | **Theme 无 clrScheme 时频次回退**: 河南模板 theme_colors=[] 即此情况 | R4-4.1 + R1-IR2 | StyleExtractor 找不到主色 | T1.6 |

## P1 — 必须修 (可边写边补，但 P6 前必须到位)

| # | Issue | Source | Owner |
|---|---|---|---|
| G1-1 | LayoutComposer 双路径 (built-in / custom) 共用 layout_plan 但代码路径分裂 | R3-A3 | T2 重组 |
| G1-2 | PPTXBuilder theme/master 注入缺失 — blank Presentation() 继承 Calibri 默认 | R3-A4 + R2-IR3 | T3.6 |
| G1-3 | StyleProfile schema 在 design.md 与 spec.md 不一致 | R3-A5 | T1.4 |
| G1-4 | LLM 重试 + heuristic fallback 未规定 | R3-B2 | T5.6 |
| G1-5 | 确认点超时无策略 | R3-B3 | T5.7 |
| G1-6 | LO 进程池并发未规划 | R3-C1 | T4.4 |
| G1-7 | 引擎缺确定性测试夹具 | R3-D1 | T7 重写 |
| G1-8 | quality gate 自身无测试 | R3-D2 | T7.7 |
| G1-9 | v2.3 迁移脚本 | R3-E1 | T5.8 |
| G1-10 | source.md 中间格式未规范 | R3-H2 | T5.1 |
| G1-11 | 中文字数与英文混算导致 overflow 误判 | R4-2.2 | T3.7 |
| G1-12 | outline LLM 页数失配 | R4-1.3 | P1 prompt |
| G1-13 | detail LLM 篡改/补造数据 | R4-1.4 | T5.11 numeric audit |
| G1-14 | content_block type 与 layout slot 不匹配 | R4-1.5 | T2.6 |
| G1-15 | LO 崩溃后无 cleanup | R4-3.1 | T4.5 |
| G1-16 | 参考模板含动画/视频/嵌入字体 → 用户期望落差 | R4-4.3 | T1.6 告知 |
| G1-17 | refinement 无收敛保证 | R4-5.1 | T5.12 |
| G1-18 | FONT-INHERITANCE gate: XML vs 渲染 | R4-6.1 | T7 |
| G1-19 | COLOR-CONSISTENCY 容差 | R4-6.2 | T7 |
| G1-20 | 5 确认点措辞不统一 | R4-8.1 | T5 |
| G1-21 | feishu 长消息截断 | R4-8.2 | T5 |
| G1-22 | 图片处理完全忽略 | R2-IR1 | T3.5 |
| G1-23 | 中文排版默认值 (行距/字距) | R2-IR2 + R4-2.4 | T3.7 |
| G1-24 | SVG 降级几何不准 | R4-3.2 | T4.6 |
| G1-25 | SmartArt 完全不支持但未标注 | R1-CG2 | design.md 标注 |

## P2 — 应修

| # | Issue | Source |
|---|---|---|
| G2-1 | overflow 策略序列未明确 (shrink→two_column→split) | R3-B4 |
| G2-2 | 多 session 并发隔离 | R3-C2 |
| G2-3 | picoclaw 集成路径未列 | R3-E2 |
| G2-4 | 全流程无 trace/log | R3-F1 |
| G2-5 | fast-track 全自动模式 | R3-G1 |
| G2-6 | 资源管理 (images_dir) | R3-H3 |
| G2-7 | CONTENT-COMPLETENESS 漏检空 shape | R4-6.3 |
| G2-8 | LO 进程池 OOM | R4-7.1 |
| G2-9 | work/ 磁盘爆炸 | R4-7.2 |
| G2-10 | 用户上传错文件 (jpg 当 pptx) | R4-8.3 |
| G2-11 | python-pptx 版本兼容 | R4-9.1 |
| G2-12 | HTML 降级需用户显式确认 | R4-3.3 |
| G2-13 | 预览保真度问题 | R2-IR4 |
| G2-14 | 自定义参考路径「风格借鉴」语义模糊 | R2-IR5 |

## P3 — 可选

| # | Issue | Source |
|---|---|---|
| G3-1 | 预览 shape_id overlay | R3-G2 |
| G3-2 | refinement 改 detail vs layout 路由 | R4-5.3 |
| G3-3 | 多 viewer 兼容 (WPS/Keynote) | R4-9.2 |
| G3-4 | 中文竖排文本 | R2-OQ3 |
| G3-5 | 内置风格从 5 扩到 7 | R1-R2 |

---


## Patch Plan — 对 design.md / spec.md / tasks.md 的具体修改

### design.md patches

| Section | Patch | Source |
|---|---|---|
| §数据契约 | 新增「解析优先级」小节: inline override > reference_lib > built_in DSL > slide_type 默认 | G0-2 |
| §数据契约 | layout_plan.json v3 schema: shape_id 格式 = s{plan_slide}_{role}_{seq} | G0-3 |
| §数据契约 | detail_plan.json: 每个 content_block 必须有 key 字段 (type+seq, slide 内唯一) | G0-5 |
| §数据契约 | layout_plan.json v3: shapes[] 增加 table type 的 row/col/merge 定义 | G0-9 |
| §P0 输入采集 | 5 种 parser 必须有实现或明确标记「暂不支持」 | G0-10 |
| §P0 输入采集 | .pptx 损坏/密码检测 + 友好错误 | G0-11 |
| §P4 Layout | LayoutComposer 走「版式模板+槽位填充」而非 LLM 自由摆放 | G0-4 |
| §P3 Style | style_ref 接受字面对象或白名单引用键 | G0-7 |
| §P5 Preview | 重写预览方案: SVG-first + 可选 LO (需先注入 theme/master) | G0-1 |
| §P6 PPTX | PPTXBuilder 启动时注入 theme/master (非 blank Presentation) | G1-2 |
| §P6 PPTX | 中英文字宽估算: 1中文=2英文字符=1em; +5% 安全边距 | G1-11 |
| §P6 PPTX | 中文排版默认值: lnSpc=1.2, spcAft=400 | G1-23 |
| §P6 PPTX | 图片处理: layout_plan shapes[] 增加 image type + fit/crop | G1-22 |
| §P6 PPTX | overflow 策略序列: shrink_title(min28pt) > two_column > split | G2-1 |
| §Scope Out | 标注: SmartArt/动画/视频/嵌入字体不支持 | G1-25 |
| §P3 Style | StyleExtractor theme 无 clrScheme 时走 RGB 频次统计回退 | G0-12 |

### spec.md patches

| Add | Content | Source |
|---|---|---|
| INV-6 | style_ref 解析优先级: inline > reference_lib > built_in DSL > default | G0-2 |
| INV-7 | shape_id = s{plan_slide}_{role}_{seq} | G0-3 |
| INV-8 | detail_plan content_block.key = type+seq, slide 内唯一 | G0-5 |
| INV-9 | style_ref 引用键必须在 allowed_refs 白名单或为字面对象 | G0-7 |
| INV-10 | 任意 shape 坐标 in 画布; 两两 IoU < 0.05 | G0-4 |
| INV-11 | refinement 仅 patch 用户指定 shape_id, 锁定其余 | G1-17 |
| Spec 7 | StyleProfile schema + 字段类型 + 缺省值 | G1-3 |
| Spec 8 | Resume Semantics (session.json + --resume + dirty-slide 增量) | G0-6 |
| Spec 9 | Confirmation Modes (interactive / fast-track) | G2-5 |
| Spec 10 | Numeric Audit (detail_plan 数字 vs source.md 交叉校验) | G1-13 |
| Spec 11 | Refinement Convergence (3轮上限 + 锁字段 + mini-confirmation) | G1-17 |
| EH-5 | 确认点超时: 1h->pending, 24h->归档 | G1-5 |
| EH-6 | .pptx 损坏 -> 友好错误提示 | G0-11 |
| EH-7 | CJK 字体缺失 -> 警告 + 推荐安装 | G0-8 |
| EH-8 | LO 崩溃 -> cleanup + 降级 SVG | G1-15 |
| EH-1 细化 | LLM 重试: 3次 (1s/4s/16s 退避) -> heuristic fallback | G1-4 |
| EH-2 细化 | overflow 策略序列: shrink->two_column->split, 不可重复 | G2-1 |

### tasks.md patches (新增/重组)

| New Task | Hours | Depends | Source |
|---|---|---|---|
| T0: Multi-source Preprocessing | 4h | - | G0-10 |
| T1.5: style_ref resolver + 白名单 | 1h | T1 | G0-2, G0-7 |
| T1.6: theme 频次回退 + .pptx 损坏检测 + 动画告知 | 1h | T1 | G0-11, G0-12, G1-16 |
| T2 重组: T2.a/b/c | +2h | T1 | G1-1 |
| T2.6: 槽位校验 + 坐标 INV | 1h | T2 | G0-4, G1-14 |
| T2.7: LLM client 接 instructor/outlines | 2h | T2 | G0-7 |
| T3.5: image shape (fit/crop) | 1h | T3 | G1-22 |
| T3.6: theme/master 注入 | 2h | T3 | G1-2 |
| T3.7: 字体回退链 + CJK + 字宽估算 + 排版默认值 | 1h | T3 | G0-8, G1-11, G1-23 |
| T3.8: table shape (row/col/merge) | 2h | T3 | G0-9 |
| T4 重写: 预览方案 (SVG-first + 可选 LO) | 3h | T3.6 | G0-1 |
| T4.5: LO cleanup + timeout | 1h | T4 | G1-15 |
| T4.6: SVG 降级 warning + HTML 模式拒绝 | 1h | T4 | G1-24 |
| T5.5: session manager + resume | 3h | T5 | G0-6 |
| T5.6: LLM 客户端 + heuristic fallback | 2h | T5 | G1-4 |
| T5.7: confirmation timeout | 1h | T5 | G1-5 |
| T5.8: v2->v3 migration shim | 2h | T5 | G1-9 |
| T5.9: SKILL.md 重写 | 1h | T5 | G2-3 |
| T5.10: logging/trace | 1h | T5 | G2-4 |
| T5.11: numeric audit | 1h | T5 | G1-13 |
| T5.12: refinement 路由 + 锁字段 + mini-confirmation | 2h | T5 | G1-17 |
| T5.13: 确认点措辞统一 + 长消息附件 | 1h | T5 | G1-20, G1-21 |
| T6.4: 内置 DSL 色板回退表 | 1h | T6 | G0-12 |
| T7 重写: T7.1-T7.9 | 10h | T1-T6 | G1-7, G1-8 |

**原 30h -> 修订 ~55h** (含 T0 4h + 新增 ~21h)

## DECISIONS.md 待决项关闭建议

| Q | 建议 | 依据 |
|---|---|---|
| Q1 style_ref 解析顺序 | inline > reference_lib > built_in DSL > default | G0-2, G0-7 |
| Q2 shape_id 命名空间 | 全局唯一 s{plan_slide}_{role}_{seq} | G0-3 |
| Q3 结构化 LLM 输出 | instructor/outlines (schema-locked) | G0-7 |
| Q4 v2.3 后向兼容 | 保留 compose_with_layout_plan 一个 release 标 deprecated + 迁移脚本 | G1-9 |
| Q5 并发模型 | LO 进程池 size=2-4 (动态: min(4, avail_mem/300MB)) | G1-6 |
| Q6 缺失功能 | P0必补: 演讲备注/品牌化/表格/图片; P1: 中文排版; P2: 增量重建 | R2 |
| Q7 状态持久化 | 每 phase 落盘 JSON + session.json + --resume | G0-6 |
