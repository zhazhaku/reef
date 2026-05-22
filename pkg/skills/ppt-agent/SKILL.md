---
name: ppt-agent
skills:
  - python-pptx
  - ppt_compose
  - template_parse
  - ppt_synthesis
description: >
  Multi-phase AI PPT generation agent. Parses user-uploaded .pptx templates,
  generates outlines and detailed content via LLM, maps content to template
  styles, and composes the final editable .pptx file using native python-pptx
  operations (not HTML-to-PPT conversion).
workflow:
  - phase: 1_parse
    description: Parse user-uploaded template .pptx into TemplateStructure JSON
    tool: template_parser.py
    input: template.pptx
    output: template_structure.json
  - phase: 2_outline
    description: Generate slide-by-slide outline from content requirements
    tool: outline_prompt.md (LLM)
    input: content_requirements + template_structure.json
    output: outline.json
  - phase: 3_detail
    description: Generate detailed content for each slide
    tool: detail_prompt.md (LLM)
    input: outline.json + template_structure.json
    output: detail_plan.json
  - phase: 4_style_map
    description: Map content slides to template slides for style matching
    tool: style_mapping_prompt.md (LLM)
    input: detail_plan.json + template_structure.json
    output: style_mapping.json
  - phase: 5_compose
    description: Compose the final .pptx using template + content + mapping
    tool: ppt_compositor.py
    input: template.pptx + detail_plan.json + style_mapping.json + images/
    output: result.pptx
constraints:
  - All elements individually editable (native python-pptx shapes)
  - No HTML-to-PPT conversion
  - Template styles preserved via XML deep clone
  - Images sourced from user-provided files or AI-generated with explicit confirmation
version: 0.1.0-mvp
