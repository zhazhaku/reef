---
change: reef-ppt-agent-v3
schema: spec-driven
doc: design
---

# Design: PPT Agent v3 详细设计

## 1. 七阶段流程详解

### P0 — 输入采集
**输入**: 用户消息 (一段话 / Word / PDF / Excel / URL / .md) + 业务要求
**Agent**:
1. 检测来源类型 (MIME + 扩展名)
2. 调用对应 parser:
   - docx -> mammoth
   - pdf -> pdfplumber
   - xlsx -> openpyxl
   - url -> readability + trafilatura
   - md/txt -> 直接读取
3. 统一为 source.md 中间格式 (含标题层级 / 表格 / 图引用)
**确认点**: 无 (自动完成, 失败时报错给用户)
**产物**: work/source.md

### P1 — 大纲生成
**输入**: source.md + 用户要求
**Agent**: LLM (outline_prompt.md) 自动决定页数与章节
**产物**: work/outline.json (slides 数组: page, title, gist, content_keys)
**消息**: Markdown 大纲表格 + 取舍说明
**确认点 #1**: [通过] / [修改: 文本指令] / [重做]

### P2 — 内容细化
**输入**: outline.json + source.md
**Agent**: LLM (detail_prompt.md) 为每页生成 content_blocks
**产物**: work/detail_plan.json (slides[].content_blocks[]: type, text/items/data, importance)
**消息**: 逐页 markdown 渲染预览
**确认点 #2**: [通过] / [逐页改: page=N, 指令] / [整体重做]

### P3 — 风格选择 (新增独立阶段)
**消息**: Agent 展示两条路径:
- A 内置风格: 5 选 1 (商务蓝 / 科技红 / 极简黑 / 学术绿 / 活力橙), 各附 4 页缩略图
- B 自定义风格: 用户上传参考 .pptx (仅作风格借鉴, 不严格限制内容/页数)
**确认点 #3**: 风格路径选择
**产物**: work/style_decision.json

### P4 — 布局规划 (分双路径)

#### P4-A 内置风格路径
**输入**: detail_plan + 内置风格 DSL
**Agent**: LayoutComposer 根据 DSL 规则 + 内容量自动选择版式
**产物**: work/layout_plan.json
**消息**: 无 (直接进 P5 预览)

#### P4-B 自定义参考路径
**Step 1 — 参考解析**:
- 解析参考 .pptx -> work/reference_lib.json (每页提取 StyleProfile)
**Step 2 — AI 风格匹配**:
- LLM 为每个 detail_plan 页推荐 1 个参考页 + 理由
- 消息: 映射表 (plan_page -> ref_page, reason)
- 确认点 #3.5: 映射确认 [通过] / [调整: page=N, ref=M]
**Step 3 — 借鉴生成**:
- 提取 StyleProfile 配色/字体/装饰特征
- LayoutComposer 重新生成 layout (不复制 shape, 仅借特征)
- 产物: work/layout_plan.json


### P5 — 预览确认 (强制)

**输入**: layout_plan.json
**Agent**: 
1. 渲染引擎生成每页预览图 (PNG 优先, SVG 降级, HTML 最终降级)
2. 输出: work/previews/slide_{N}.png + work/previews/summary.txt
**确认点 #4**: 预览确认
- 展示: 每页缩略图 + 文字摘要
- 用户选项: [全部通过] / [修改第N页: ...]
- 若修改: 进入细化子循环 (refinement_prompt.md) → 更新 layout_plan → 重新预览
- 最多 3 轮细化, 超过则提醒用户"建议接受当前版本"
**产物**: 用户确认的 layout_plan.json

### P6 — PPTX 生成

**输入**: 确认后的 layout_plan.json
**Agent**: PPTXBuilder
- 从空白演示文稿构建 (非 clone template)
- 每个 shape 独立创建, 独立可编辑
- 样式从 style_ref 指针获取 (内置 DSL 或 reference_lib)
- 绝不将整页渲染为图片
**后处理**:
- 溢出检测: 3 策略自动修复 (shrink_title / split_to_two_slides / two_column)
- 质量门: FONT-INHERITANCE / COLOR-CONSISTENCY / CONTENT-COMPLETENESS / TEXT-OVERFLOW
**产物**: output/{name}.pptx
**确认点 #5**: 最终交付 (可选下载)

---

## 数据契约

| 产物 | 格式 | 生成阶段 | 说明 |
|------|------|----------|------|
| source.md | Markdown | P0 | 多源预处理统一文本 |
| outline.json | JSON | P1 | 9-15页大纲, 每页 title+summary+slide_type |
| detail_plan.json | JSON | P2 | 每页 content_blocks[], type∈{title,subtitle,body,bullet_list,image,table} |
| style_decision.json | JSON | P3 | {mode:"built_in"|"custom", style_name?, reference_path?} |
| reference_lib.json | JSON | P4-B | 每页 StyleProfile: colors/fonts/decor/layout_skeleton |
| layout_plan.json | JSON | P4 | 每页 shapes[]: {type,x,y,w,h,content_key,style_ref,action} |
| previews/ | PNG/SVG/HTML | P5 | 每页预览图 |
| output.pptx | PPTX | P6 | 最终交付 |

### layout_plan.json v3 schema (核心变更)

```json
{
  "meta": {
    "plan_slide_count": 9,
    "overflow_decisions": []
  },
  "slides": [{
    "plan_slide": 1,
    "slide_type": "COVER",
    "layout_strategy": "center_title",
    "shapes": [{
      "shape_id": "s1_title",
      "type": "text_box",
      "x": 914400, "y": 2057400,
      "w": 10363200, "h": 2743200,
      "content_key": "title",
      "style_ref": {"font": "微软雅黑", "size_pt": 40, "bold": true, "color": "#C00000"},
      "action": "fill_text"
    }]
  }]
}
```

**v2.3→v3 关键区别**:
- v2.3: `target_shape_idx` 引用模板 shape → 强绑定模板结构
- v3: `shapes[]` 从零定义新布局 + `style_ref` 借鉴风格 → 解耦内容与样式

---

## 引擎架构

```
┌─────────────┐    ┌──────────────┐    ┌───────────────┐
│ StyleExtractor│───▶│LayoutComposer │───▶│  PPTXBuilder  │
│  (NEW)       │    │ (替代 auto_   │    │ (替代 compose │
│              │    │  match_shape) │    │  _with_layout │
│ 输入: .pptx  │    │              │    │  _plan)       │
│ 输出: Style  │    │ 输入: detail  │    │               │
│ Profile      │    │ + style_ref  │    │ 输入: layout  │
│              │    │ 输出: layout  │    │ _plan         │
│ 内置: 5 DSL  │    │ _plan.json   │    │ 输出: .pptx   │
└─────────────┘    └──────────────┘    └───────────────┘
```

### StyleExtractor
- 解析 .pptx → 提取配色/字体/装饰/布局骨架 → StyleProfile
- 内置 5 种 DSL (商务蓝/科技红/极简黑/学术绿/活力橙)
- 输出格式: `{colors: {primary, secondary, accent, bg, text}, fonts: {title, body}, decor: {accent_bar, card_radius, icon_style}}`

### LayoutComposer
- 根据 detail_plan + style_ref 生成 layout_plan.json
- 内置版式库: center_title / left_title / data_cards / three_column / two_column / flow_chart / comparison / timeline
- 版式选择逻辑: slide_type → 候选版式 → content_block 数量/类型 → 最终选择
- 自定义参考: 借鉴 reference_lib 中 StyleProfile 的布局骨架, 不复制 shape

### PPTXBuilder
- 从空白 python-pptx Presentation() 构建
- 按 layout_plan.shapes[] 逐个创建 shape
- 样式从 style_ref 字段注入
- 不使用 clone_slide / 不依赖模板 shape 索引
- 保证每个元素独立可编辑

---

## v2.3 vs v3 对比

| 维度 | v2.3 (当前) | v3 (本设计) |
|------|-------------|-------------|
| 模板依赖 | 强绑定 (target_shape_idx) | 解耦 (style_ref 借鉴) |
| 内容映射 | _auto_match_shape 启发式 | LayoutComposer 语义匹配 |
| 预览 | 可选 | 强制 (P5) |
| 确认点 | 3 个 | 5 个 |
| 自定义模板 | clone + fill slot | 解析 + 借鉴风格 + 重建 |
| 内置风格 | 无 | 5 种 DSL |
| 引擎 | compose_with_layout_plan | PPTXBuilder (从零构建) |
| 失败模式 | 内容错位 (shape 匹配错) | 风格偏差 (可调, 不致命) |
