# R4 — Pitfalls / 失败模式自审

> 2026-05-22. 来源: v2.3 河南案例 + python-pptx 已知坑 + LLM 结构化输出常见问题。
> 维度: 9 类共 28 个具体坑，每个标 severity + 触发条件 + 缓解.

## 1. LLM 幻觉类

### 1.1 ❌ P0 — `style_ref` 引用不存在的 ID
**触发**: LayoutComposer 让 LLM 输出 layout_plan.shapes[].style_ref, LLM 写了 `"style_ref": "primary_dark"` 但 reference_lib / built-in DSL 都没这个键。
**症状**: PPTXBuilder 静默走默认黑色, 全员同色.
**缓解**:
1. style_ref 接受**两种**形式: 字面对象 `{color: "#C00000", ...}` 或 引用键 `"primary_title"`.
2. 引用键必须在 `style_decision.allowed_refs[]` 白名单内, JSON Schema 强制 enum.
3. 走 instructor/outlines 这类 schema-locked LLM client, 不裸 JSON-mode.

### 1.2 ❌ P0 — LayoutComposer 输出 shapes[] 坐标越界 / 重叠
**触发**: LLM 自己算 EMU 坐标, 不懂 16:9 是 12192000×6858000.
**症状**: shape 出画布; 或 4 张卡片重叠成一团.
**缓解**:
1. LayoutComposer 走「版式模板 + 槽位填充」而非 LLM 自由摆放. LLM 只选版式 ID + 给每槽 content_key, 坐标由代码确定.
2. spec 加 INV: 任意 shape 必须 0 ≤ x, x+w ≤ slide_w; 任意两 shape iou < 0.05.

### 1.3 ⚠ P1 — outline LLM 给出页数与 source 内容严重失配
**触发**: 5 页 source 给 outline 出 20 页, 或 30 页 source 给 outline 出 5 页.
**症状**: 每页空洞 / 每页爆满.
**缓解**: outline_prompt 注入 source 字数 + 经验比例 (≈800 字/页), 给 LLM 输出 `target_slides ± 2`. 实际超界则警告但允许.

### 1.4 ⚠ P1 — detail LLM 篡改/补造 source 没有的数据
**触发**: source 只说 "提效 200 万", LLM 自动加 "同比+35%".
**症状**: 数据失真, 用户难发现.
**缓解**:
1. detail_prompt 强制规则: "只能引用 source.md 出现的数字, 不得补全".
2. 后处理 numeric audit: 抽 detail_plan 所有 \\d+(\\.\\d+)?%? 在 source.md 全文 grep, 找不到的标 warning 给用户复核.

### 1.5 ⚠ P1 — content_block 类型与 layout 槽位不匹配
**触发**: detail 给 `type:bullet_list`, layout 选了「数据卡片」版式 (4 槽都是 numeric).
**症状**: bullet 字串被塞进 number 槽, 字号 60pt 显示 "采用 AI 大模型..." 截断.
**缓解**: LayoutComposer 选版式时, slot type 与 block type 双向 enum 校验; 失败回退到通用 "title + body + list" 版式.

## 2. 字体 / 排版类

### 2.1 ❌ P0 — 中文字体在 LibreOffice 找不到 → PNG 预览乱码
**触发**: agent 运行环境 (容器) 无微软雅黑 / 思源黑体, LO 退化到 DejaVu Sans → 中文方块.
**症状**: 预览图全是□, 用户被吓到; PNG 与最终 PPTX 在 Office 打开效果不一致.
**缓解**:
1. Dockerfile/部署文档强制装 fonts-noto-cjk + 思源黑体.
2. PPTXBuilder 输出时显式写 `+mj-ea` `+mn-ea` 字体回退链, 让 Office 客户端按自身字体表渲染.
3. preview 前自检字体, 缺则在第一页打 warning banner.

### 2.2 ❌ P0 — 中文字数估算与英文混算
**触发**: shrink_title 算 "标题 30 字, 字号 28pt 容得下", 实际混了英文 "AI Operations 智慧运营平台" — 中文 6 字 + 英文 16 字符 ≠ 22 字宽度.
**症状**: 仍然溢出, overflow 死循环.
**缓解**: 字宽估算用 `1 中文 = 2 英文字符 = 1 em` 经验式; LO 计算后 + 5% 安全边距.

### 2.3 ⚠ P1 — 字体加粗 / 斜体 / 下划线在中文里效果差
**触发**: LLM 给 `bold: true` 但中文加粗在 PPT 里显示比英文细很多.
**缓解**: 标题/强调走「换字重」(雅黑→雅黑 Bold) 而非走 rPr `b="1"`; 内置 DSL 预设字体族.

### 2.4 ⚠ P2 — 行距/字距未规范导致密集如压缩
**触发**: python-pptx 默认行距 1.0; 中文密集.
**缓解**: pPr 写 lnSpc=1.2 + spcAft=400 (0.4 字高).

## 3. 预览引擎降级链

### 3.1 ❌ P0 — LibreOffice headless 在某些 distro 上崩溃后无 cleanup
**触发**: LO 渲染卡死, 进程僵尸 + lockfile 残留 → 下次启动失败.
**缓解**: 进程池每次启动前 `pkill -f soffice` + 删 `~/.config/libreoffice/4/user/.~lock*`; 每渲染加 timeout=30s.

### 3.2 ⚠ P1 — SVG 降级 (python-pptx 自渲染) 几何不准
**触发**: PNG 失败 → SVG, 但 SVG 是用 python-pptx + 自写转换器算的, 字宽/换行与 PPT 实际不同 → 用户预览通过, 实际打开溢出.
**缓解**: SVG 加 "几何近似" 水印; refinement 子循环禁用 SVG 模式 (必须 PNG).

### 3.3 ⚠ P2 — HTML 降级展示力差 → 用户被迫盲签
**触发**: 双重降级 SVG 也失败 → HTML (纯文本 + 色块).
**缓解**: HTML 模式弹「降级警告」, 必须用户显式 [我知道有风险, 继续] 才放行; 默认拒绝进入 P6.

## 4. 模板/参考 .pptx 解析

### 4.1 ❌ P0 — StyleExtractor 遇到 theme1.xml 无 a:clrScheme (用户模板纯硬编码 RGB)
**触发**: 河南那个模板 theme_colors=[] 就是这个情况.
**症状**: StyleExtractor 找不到主色, 默认黑色.
**缓解**: fallback: 统计 slide-level 直接 RGB 出现频次, top-2 为 primary/secondary; 写入 spec.md.

### 4.2 ❌ P0 — 用户上传的 .pptx 含密码 / 损坏 / 非标准
**触发**: 工程界混用 WPS / Keynote 导出.
**症状**: python-pptx 抛 zipfile.BadZipFile.
**缓解**: P0 加 try/except → 给用户「模板无法解析, 请用 PowerPoint 打开另存为 .pptx」提示.

### 4.3 ⚠ P1 — 参考模板含动画 / 视频 / 嵌入字体
**触发**: 老板的"骚气"模板.
**症状**: StyleExtractor 不 care, 但 layout_plan 复刻不出, 用户期望与产出落差大.
**缓解**: P4-B 解析时检测 anim/video/embedded font, 在 confirmation #3.5 显式告知 "以下效果将丢失".

### 4.4 ⚠ P2 — 母版/版式被 hide 但 shape 还在
**触发**: 用户拿了「3 个版式」的模板, 但实际只 1 个能用.
**缓解**: StyleExtractor 跳 master.element.get('show') == '0' 的版式.

## 5. Refinement 循环

### 5.1 ❌ P0 — refinement 无收敛保证
**触发**: 用户每次 reject 后 LLM 改一处, 改完又错另一处.
**缓解**:
1. spec INV: 每轮 refinement 必须只 patch 用户指出的 shape_id 集合, 不得改其他 shape (锁定其他 shape 的字段)
2. 3 轮上限到达 → 强制接受当前版 + 标 warning; 不再问

### 5.2 ⚠ P1 — refinement 用户指令模糊 → LLM 误解
**触发**: 用户说 "第 3 页太挤" — 是字号大还是字多? 是哪一块?
**缓解**: refinement_prompt 强制 LLM 先输出「我的理解」+「拟改动 shape_id 列表」, 给用户二次确认才执行 (mini-confirmation).

### 5.3 ⚠ P2 — refinement 改 detail 而非 layout
**触发**: 用户说 "把第 2 段缩短", 但 LLM 跑去改 layout 缩小字号.
**缓解**: refinement 路由层判断 — 内容相关 → 改 detail_plan + 重生 layout; 几何相关 → 只改 layout_plan.

## 6. 质量门 (Quality Gate)

### 6.1 ⚠ P1 — FONT-INHERITANCE 检查靠 XML 字段 vs 实际渲染
**触发**: XML 写了字体 = 微软雅黑, 但客户端无该字体回退到宋体 — XML 通过, 实际丑.
**缓解**: gate 区分 "spec 通过" 与 "render 通过". 后者需 LO 渲染后做 OCR/像素对比, 太重 — 默认只跑 spec 通过, render 通过仅在 `--strict` 启用.

### 6.2 ⚠ P1 — COLOR-CONSISTENCY 判定颜色族过严
**触发**: primary=#C00000, decor 用 #B00000 (相近) — 算不算 inconsistent?
**缓解**: 容差用 CIEDE2000 < 5; 文档化.

### 6.3 ⚠ P2 — CONTENT-COMPLETENESS 漏检空 shape
**触发**: layout 给 4 卡, detail 只给 3 块内容 — 第 4 卡空.
**缓解**: gate 检查 每个 shape 必须 content_key resolved (非空) 否则 violation.

## 7. 并发 / 资源

### 7.1 ⚠ P1 — LO 进程池在 Docker 内存不足时崩
**触发**: 4 并发 × 200MB = 800MB; 容器限 512MB.
**缓解**: 池 size 动态: `min(4, available_mem/300MB)`; OOM 时降到 1 + 串行.

### 7.2 ⚠ P2 — work/ 目录磁盘占用爆炸
**触发**: 每 session 产 50+MB (preview PNG + intermediate JSON + clone pptx), 100 session = 5GB.
**缓解**: 完成后 7 天自动归档 + tar.gz; 配置 retention.

## 8. 用户体验

### 8.1 ⚠ P1 — 5 个确认点中文化措辞不统一
**触发**: 不同 phase 提问语气不一, 用户答 "好的" 不知确认哪个.
**缓解**: 统一模板: `[确认点 N/5: <阶段>] 选项: [通过] / [修改] / [重做] / [换风格]`.

### 8.2 ⚠ P1 — feishu 一条消息超 30KB 被截断
**触发**: outline 大表格 + 长描述 一条贴出.
**缓解**: 长消息走附件 (.md 文件), 主文本只放摘要 + "详见附件".

### 8.3 ⚠ P2 — 用户上传错文件 (jpg 当 pptx)
**触发**: 用户给参考样式给了一张截图.
**缓解**: P4-B 收到非 .pptx → 走 OCR/视觉模型 提取色板, 但明确告知 "只能借鉴色, 不能借鉴布局".

## 9. 部署/工程

### 9.1 ⚠ P2 — python-pptx 版本兼容
**触发**: pptx 0.6 与 1.0 API 微调.
**缓解**: requirements.txt pin 版本 + CI 矩阵测.

### 9.2 ⚠ P3 — Office 365 vs WPS vs Keynote 打开差异
**触发**: 我们的 pptx 在 PowerPoint 显示完美, WPS 自动重排.
**缓解**: 文档化 "目标 viewer = Office 2019+ / Office 365"; 其余 best-effort.

## 汇总

### P0 阻塞 (开工前必须有方案)
- 1.1 style_ref 白名单 + schema-locked LLM
- 1.2 LayoutComposer 走槽位填充非自由摆放
- 2.1 CJK 字体环境硬要求
- 2.2 中英文字宽估算
- 3.1 LO cleanup 机制
- 4.1 theme 无 clrScheme 时频次回退
- 4.2 .pptx 损坏检测
- 5.1 refinement 收敛保证 + 锁字段

### P1 必须修
- 1.3/1.4/1.5 (outline 页数 / 数据保真 / type-slot 校验)
- 2.3 (中文加粗换字重)
- 3.2 (SVG 几何不准警告)
- 4.3 (动画/视频解析告知)
- 5.2 (refinement 二次确认)
- 5.3 (refinement 路由)
- 6.1/6.2 (gate spec vs render / 颜色容差)
- 7.1 (LO 池动态 size)
- 8.1/8.2 (确认话术统一 / 长消息附件)

### P2 应修
- 2.4 / 3.3 / 4.4 / 6.3 / 7.2 / 8.3 / 9.1

### P3 可选
- 9.2 多 viewer 兼容

### 对 tasks.md 增量
- T1.6 theme 频次回退 + .pptx 损坏检测 (1h)
- T2.6 LayoutComposer 槽位校验 + 坐标 INV (1h)
- T2.7 LLM client 接 instructor/outlines (2h)
- T3.7 字体回退链 + CJK 测试 (1h)
- T4.5 LO cleanup + timeout (1h)
- T4.6 SVG 降级 warning + HTML 模式拒绝 (1h)
- T5.11 numeric audit (1h)
- T5.12 refinement 路由 + 锁字段 (2h)
- T6.4 内置 DSL 提供「色板回退表」(1h)
- T7.8 CJK 字体测试 (1h)
- T7.9 fixtures 加 broken_template / mixed_lang / overflow / image_missing 场景 (2h)

**累计增时**: ~14h. 总计 ~45h (R3) + ~14h (R4) - 部分重叠 = **~50h**.

### 对 spec.md 增量
- INV-9: style_ref 必须在白名单或字面对象
- INV-10: 任意 shape 坐标 ∈ 画布 + 两两 IoU < 0.05
- INV-11: refinement 仅 patch 用户指定 shape_id
- EH-6: .pptx 损坏 → 友好错误
- EH-7: CJK 字体缺失 → 警告 + 推荐安装
- EH-8: LO 崩溃 → cleanup + 降级 SVG
- Spec 10: Numeric Audit
- Spec 11: Refinement Convergence

### 与 R3 重叠
R3-A1 (style_ref 优先级) + R4-1.1 (style_ref 幻觉) → 合并为同一 P0 任务 (T1.5 + T2.7)
R3-B1 (resume) + R4-5.1 (refinement 收敛) → 共用 session 状态机
R3-D1 (fixture 库) + R4-7.9 (broken/mixed 场景) → 合并 T7 重写

