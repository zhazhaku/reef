# Requirements — PPT Agent v3

## Must Haves
1. **7 阶段流水线**: P0→P1→P2→P3→P4→P5→P6，每阶段有明确输入/输出/确认点
2. **5 个确认点**: #1(outline) #2(detail) #3(style) #3.5(custom mapping) #4(preview)
3. **强制预览**: P5 必须生成并展示预览，用户确认后才能进 P6
4. **双路径风格**: 内置 5 种 DSL + 自定义 .pptx 参考
5. **风格借鉴**: 自定义路径只借配色/字体/布局骨架，不复制 shape
6. **从空白构建**: PPTXBuilder 从 Presentation() 构建，每元素独立可编辑
7. **溢出检测**: 3 策略自动修复 (shrink_title / split_to_two_slides / two_column)
8. **质量门**: FONT-INHERITANCE / COLOR-CONSISTENCY / CONTENT-COMPLETENESS / TEXT-OVERFLOW
9. **向后兼容**: v2.3 compose_with_layout_plan() 保留为 strict mode
10. **河南移动 PPT 重做**: 作为验收测试

## Should Haves
1. 8 种内置版式模板 (center_title / left_title / data_cards / three_column / two_column / flow_chart / comparison / timeline)
2. 预览渲染降级链 PNG → SVG → HTML
3. 细化子循环 (refinement) 最多 3 轮
4. StyleProfile schema 标准化
5. reference_lib.json 多页风格提取

## Nice to Haves
1. 风格预览缩略图 (800x450 PNG)
2. HTML 降级预览
3. 风格选择 UI 消息格式化
4. 严格模式 deprecation warning

## Out of Scope
- 实时协作编辑
- 动画/转场效果
- 在线 PPT 播放
- 多人协作工作流
