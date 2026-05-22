"""
P2: PPT Compositor - Compose PPTX from template + detail_plan + style_mapping + images.

Usage:
    python3 ppt_compositor.py \\
      --template template.pptx \\
      --plan detail_plan.json \\
      --mapping style_mapping.json \\
      --images images/ \\
      --output result.pptx

Architecture:
    compose() → for each slide in plan:
        1. Look up template slide from style_mapping
        2. clone_slide(template → output)
        3. For each content_block: fill_text_block / fill_bullet_list / fill_table / fill_image / create_shape_decor
        4. apply_slide_overrides (layout swap etc.)
        5. Save .pptx
"""
import copy
import json
import os
import sys
from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.dml.color import RGBColor
from pptx.oxml.ns import qn
from pptx.enum.shapes import MSO_SHAPE
from lxml import etree

from template_parser import load_pptx, parse_template
from schema import validate_template_structure, validate_style_mapping


# ═══════════════════════════════════════════════════════════════════════════════
# P2.1: Slide clone (XML deep copy)
# ═══════════════════════════════════════════════════════════════════════════════

CLONE_SHAPE_TAGS = {
    qn('p:sp'), qn('p:pic'), qn('p:grpSp'),
    qn('p:cxnSp'), qn('p:graphicFrame'),
}


def clone_slide(src_prs: Presentation, src_slide_index: int,
                dst_prs: Presentation) -> "Slide":
    """Clone a slide from source presentation to destination, preserving all styles.

    Args:
        src_prs: Source presentation (template).
        src_slide_index: Zero-based index of slide to clone.
        dst_prs: Destination presentation (output).

    Returns:
        The newly created destination slide object.
    """
    src_slide = src_prs.slides[src_slide_index]

    # Match slide layout in destination
    src_layout_name = src_slide.slide_layout.name
    dst_layout = None
    for layout in dst_prs.slide_layouts:
        if layout.name == src_layout_name:
            dst_layout = layout
            break
    if dst_layout is None:
        idx = min(src_slide_index, len(dst_prs.slide_layouts) - 1)
        dst_layout = dst_prs.slide_layouts[idx]

    dst_slide = dst_prs.slides.add_slide(dst_layout)

    # Remove default shapes from layout
    src_sp_tree = src_slide.shapes._spTree
    dst_sp_tree = dst_slide.shapes._spTree

    to_remove = [e for e in dst_sp_tree if e.tag in CLONE_SHAPE_TAGS]
    for elem in to_remove:
        dst_sp_tree.remove(elem)

    # Deep copy shape XML from source
    for shape_elem in src_sp_tree:
        if shape_elem.tag in CLONE_SHAPE_TAGS:
            imported = copy.deepcopy(shape_elem)
            dst_sp_tree.append(imported)

    return dst_slide


# ═══════════════════════════════════════════════════════════════════════════════
# P2.2: Text block fill
# ═══════════════════════════════════════════════════════════════════════════════

def fill_text_block(shape, content_block: dict, overrides: dict = None):
    """Fill a text shape with new content, applying style overrides.

    Supported overrides: font_size (pt), font_name, color (#RRGGBB), bold, italic
    """
    overrides = overrides or {}
    tf = shape.text_frame

    # Preserve first paragraph's run style as baseline
    if tf.paragraphs and tf.paragraphs[0].runs:
        first_run = tf.paragraphs[0].runs[0]
    else:
        first_run = None

    # Clear existing text
    tf.clear()

    content = content_block.get("content", "")
    if not content:
        return

    lines = content.split("\n")
    for i, line in enumerate(lines):
        p = tf.paragraphs[0] if i == 0 else tf.add_paragraph()
        run = p.add_run()
        run.text = line

        # Apply overrides or preserve original style
        _apply_text_overrides(run, first_run, overrides)


def _apply_text_overrides(run, baseline_run, overrides: dict):
    """Apply style overrides to a run, falling back to baseline."""
    if "font_size" in overrides:
        run.font.size = Pt(overrides["font_size"])
    elif baseline_run and baseline_run.font.size:
        run.font.size = baseline_run.font.size

    if "font_name" in overrides:
        run.font.name = overrides["font_name"]
    elif baseline_run and baseline_run.font.name:
        run.font.name = baseline_run.font.name

    if "color" in overrides:
        hex_color = overrides["color"].lstrip("#")
        run.font.color.rgb = RGBColor.from_string(hex_color)
    elif baseline_run and baseline_run.font.color and baseline_run.font.color.type is not None:
        try:
            run.font.color.rgb = baseline_run.font.color.rgb
        except AttributeError:
            pass

    if "bold" in overrides:
        run.font.bold = overrides["bold"]
    elif baseline_run:
        run.font.bold = baseline_run.font.bold

    if "italic" in overrides:
        run.font.italic = overrides["italic"]
    elif baseline_run:
        run.font.italic = baseline_run.font.italic


# ═══════════════════════════════════════════════════════════════════════════════
# P2.3: Bullet list fill
# ═══════════════════════════════════════════════════════════════════════════════

def fill_bullet_list(shape, content_block: dict, overrides: dict = None):
    """Fill a text shape with bullet items.

    content_block:
        {type: "bullet_list", items: ["item1", "item2", ...]}
    """
    overrides = overrides or {}
    tf = shape.text_frame

    # Preserve first run style as baseline
    baseline = None
    if tf.paragraphs and tf.paragraphs[0].runs:
        baseline = tf.paragraphs[0].runs[0]

    tf.clear()

    items = content_block.get("items", [])
    for i, item_text in enumerate(items):
        p = tf.paragraphs[0] if i == 0 else tf.add_paragraph()
        run = p.add_run()
        run.text = item_text

        _apply_text_overrides(run, baseline, overrides)

        # Preserve or set bullet
        if "bullet" in content_block:
            p.level = content_block.get("level", 0)
        else:
            p.level = 0


# ═══════════════════════════════════════════════════════════════════════════════
# P2.4: Table fill
# ═══════════════════════════════════════════════════════════════════════════════

def fill_table_block(shape, content_block: dict, overrides: dict = None):
    """Fill a table shape with headers + rows.

    content_block:
        {type: "table", headers: ["Col1", "Col2"], rows: [["a","b"], ["c","d"]]}
    """
    overrides = overrides or {}
    if not shape.has_table:
        raise ValueError("Shape does not contain a table")

    table = shape.table
    headers = content_block.get("headers", [])
    rows = content_block.get("rows", [])
    font_size = overrides.get("font_size", None)

    # Fill headers
    for col_idx, header in enumerate(headers):
        if col_idx < len(table.columns):
            cell = table.cell(0, col_idx)
            cell.text = ""
            p = cell.text_frame.paragraphs[0]
            run = p.add_run()
            run.text = header
            run.font.bold = True
            if font_size:
                run.font.size = Pt(font_size)

    # Fill data rows
    for row_idx, row_data in enumerate(rows):
        target_row = row_idx + 1
        if target_row >= len(table.rows):
            break
        for col_idx, value in enumerate(row_data):
            if col_idx < len(table.columns):
                cell = table.cell(target_row, col_idx)
                cell.text = ""
                p = cell.text_frame.paragraphs[0]
                run = p.add_run()
                run.text = str(value)
                if font_size:
                    run.font.size = Pt(font_size)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.5: Image fill
# ═══════════════════════════════════════════════════════════════════════════════

def fill_image_block(shape, content_block: dict, image_dir: str,
                     overrides: dict = None):
    """Replace a picture placeholder with an actual image.

    content_block:
        {type: "image", image_file: "chart.png", description: "..."}
    """
    overrides = overrides or {}
    image_file = content_block.get("image_file", "")
    if not image_file:
        raise ValueError("content_block missing 'image_file'")

    image_path = os.path.join(image_dir, image_file)
    if not os.path.exists(image_path):
        raise FileNotFoundError(f"Image not found: {image_path}")

    # For PICTURE shapes, replace the image blob
    # python-pptx doesn't have a direct "replace image" API, so we update the XML
    if str(shape.shape_type) == "PICTURE (13)":
        _replace_image_blob(shape, image_path)
    else:
        # For non-picture shapes, add image to slide (position from shape)
        slide = shape._parent  # pylint: disable=protected-access
        left = shape.left
        top = shape.top
        width = shape.width
        height = shape.height

        if overrides.get("width_ratio"):
            width = int(width * overrides["width_ratio"])
        if overrides.get("height_ratio"):
            height = int(height * overrides["height_ratio"])

        pic = slide.shapes.add_picture(image_path, left, top, width, height)


def _replace_image_blob(shape, new_image_path: str):
    """Replace the image blob in a PICTURE shape with a new image."""
    nsmap = {
        'a': 'http://schemas.openxmlformats.org/drawingml/2006/main',
        'r': 'http://schemas.openxmlformats.org/officeDocument/2006/relationships',
    }
    blip_fill = shape._element.find('.//a:blipFill', nsmap)  # pylint: disable=protected-access
    if blip_fill is not None:
        blip = blip_fill.find('a:blip', nsmap)
        if blip is not None:
            # Update the embed relationship
            rId = blip.get(qn('r:embed'))
            if rId:
                part = shape.part
                # Remove old relationship
                rel = part.rels[rId]
                # Add new image relationship
                with open(new_image_path, 'rb') as f:
                    image_data = f.read()
                # Determine content type
                ext = os.path.splitext(new_image_path)[1].lower()
                content_types = {
                    '.png': 'image/png',
                    '.jpg': 'image/jpeg',
                    '.jpeg': 'image/jpeg',
                    '.gif': 'image/gif',
                    '.bmp': 'image/bmp',
                }
                content_type = content_types.get(ext, 'image/png')
                new_rId = part.relate_to(new_image_path, rel.reltype)
                blip.set(qn('r:embed'), new_rId)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.6: Shape decor (fallback when no image)
# ═══════════════════════════════════════════════════════════════════════════════

SHAPE_TYPES = {
    "rectangle": MSO_SHAPE.RECTANGLE,
    "rounded_rectangle": MSO_SHAPE.ROUNDED_RECTANGLE,
    "chevron": MSO_SHAPE.CHEVRON,
    "arrow": MSO_SHAPE.RIGHT_ARROW,
}


def create_shape_decor(slide, content_block: dict, overrides: dict = None):
    """Create a decorative shape when an image is unavailable.

    content_block:
        {type: "image", description: "Chart showing growth", shape: "rounded_rectangle"}
    """
    overrides = overrides or {}
    shape_name = content_block.get("shape", "rounded_rectangle")
    mso_shape = SHAPE_TYPES.get(shape_name, MSO_SHAPE.ROUNDED_RECTANGLE)

    # Use content_block position or default
    left = overrides.get("left", Inches(1))
    top = overrides.get("top", Inches(2))
    width = overrides.get("width", Inches(4))
    height = overrides.get("height", Inches(3))

    shape = slide.shapes.add_shape(mso_shape, left, top, width, height)

    # Set fill color from overrides or theme
    color_hex = overrides.get("color", "4472C4")
    shape.fill.solid()
    shape.fill.fore_color.rgb = RGBColor.from_string(color_hex)

    # Add description text
    tf = shape.text_frame
    tf.word_wrap = True
    p = tf.paragraphs[0]
    p.alignment = 1  # center
    run = p.add_run()
    desc = content_block.get("description", "[Image]")
    run.text = desc
    run.font.color.rgb = RGBColor(0xFF, 0xFF, 0xFF)
    run.font.size = Pt(14)
    run.font.bold = True

    return shape


# ═══════════════════════════════════════════════════════════════════════════════
# P2.7: Slide-level overrides (layout swap)
# ═══════════════════════════════════════════════════════════════════════════════

def apply_slide_overrides(slide, slide_overrides: dict):
    """Apply layout-level overrides to a slide.

    Supported: layout_swap ("left_right" / "top_bottom")
    """
    if not slide_overrides:
        return

    swap = slide_overrides.get("layout_swap")
    if not swap:
        return

    shapes = list(slide.shapes)
    if len(shapes) < 2:
        return

    if swap == "left_right":
        # Swap left coordinates of leftmost and rightmost shapes
        shapes_sorted = sorted(shapes, key=lambda s: s.left)
        a, b = shapes_sorted[0], shapes_sorted[-1]
        a.left, b.left = b.left, a.left

    elif swap == "top_bottom":
        shapes_sorted = sorted(shapes, key=lambda s: s.top)
        a, b = shapes_sorted[0], shapes_sorted[-1]
        a.top, b.top = b.top, a.top


# ═══════════════════════════════════════════════════════════════════════════════
# P2.8: Main compose function
# ═══════════════════════════════════════════════════════════════════════════════

CONTENT_FILLERS = {
    "title": fill_text_block,
    "subtitle": fill_text_block,
    "text": fill_text_block,
    "bullet_list": fill_bullet_list,
    "table": fill_table_block,
    "image": None,  # Special handling
}


def compose(template_path: str, plan_path: str, mapping_path: str,
            images_dir: str, output_path: str) -> str:
    """Main composition function.

    Args:
        template_path: Path to .pptx template.
        plan_path: Path to detail_plan.json (or detail_plan_v2.json).
        mapping_path: Path to style_mapping.json.
        images_dir: Directory containing image files.
        output_path: Path for output .pptx.

    Returns:
        Path to the generated .pptx file.
    """
    # Load inputs
    src_prs = load_pptx(template_path)
    with open(plan_path, "r", encoding="utf-8") as f:
        plan = json.load(f)
    with open(mapping_path, "r", encoding="utf-8") as f:
        mapping = json.load(f)

    # Validate
    mapping_errors = validate_style_mapping(mapping, len(src_prs.slides))
    if mapping_errors:
        raise ValueError(f"Invalid style mapping: {mapping_errors}")

    # Create mapping lookup: plan_slide → template_slide (1-indexed → 0-indexed)
    plan_to_template = {}
    for m in mapping.get("mappings", []):
        plan_to_template[m["plan_slide"]] = m["template_slide"] - 1

    # Create output presentation
    dst_prs = Presentation()
    dst_prs.slide_width = src_prs.slide_width
    dst_prs.slide_height = src_prs.slide_height

    # Compose each slide
    for plan_slide in plan.get("slides", []):
        slide_num = plan_slide.get("slide_number", 1)
        tpl_idx = plan_to_template.get(slide_num)
        if tpl_idx is None:
            raise ValueError(f"No template mapping for plan slide {slide_num}")

        # Clone template slide
        dst_slide = clone_slide(src_prs, tpl_idx, dst_prs)

        # Fill content blocks
        for block in plan_slide.get("content_blocks", []):
            block_type = block.get("type", "text")
            block_overrides = block.get("overrides", {})
            target_shape_name = block.get("target_shape")

            # Find target shape
            target_shape = None
            if target_shape_name:
                for s in dst_slide.shapes:
                    if s.name == target_shape_name:
                        target_shape = s
                        break
            if target_shape is None:
                # Auto-match: pick shape by type/position heuristic
                target_shape = _auto_match_shape(dst_slide, block_type)

            if target_shape is None:
                print(f"Warning: No shape found for block type '{block_type}' in slide {slide_num}")
                continue

            # Fill or create
            if block_type == "image":
                image_file = block.get("image_file", "")
                if image_file and os.path.exists(os.path.join(images_dir, image_file)):
                    try:
                        fill_image_block(target_shape, block, images_dir, block_overrides)
                    except Exception as e:
                        print(f"Warning: Image fill failed for '{image_file}': {e}")
                        create_shape_decor(dst_slide, block, block_overrides)
                else:
                    create_shape_decor(dst_slide, block, block_overrides)
            elif block_type in CONTENT_FILLERS:
                filler = CONTENT_FILLERS[block_type]
                if filler:
                    filler(target_shape, block, block_overrides)

        # Apply slide-level overrides
        slide_overrides = plan_slide.get("slide_overrides", {})
        apply_slide_overrides(dst_slide, slide_overrides)

    # Save output
    os.makedirs(os.path.dirname(output_path) or ".", exist_ok=True)
    dst_prs.save(output_path)
    print(f"PPT composed: {output_path} ({len(dst_prs.slides)} slides)")
    return output_path


def _auto_match_shape(slide, block_type: str):
    """Auto-match a content block to a suitable shape on the slide."""
    # Title → placeholder idx=0
    if block_type == "title":
        for shape in slide.shapes:
            if shape.is_placeholder and shape.placeholder_format.idx == 0:
                return shape

    # Subtitle → placeholder idx=1
    if block_type == "subtitle":
        for shape in slide.shapes:
            if shape.is_placeholder and shape.placeholder_format.idx == 1:
                return shape

    # Body content → placeholder idx=1
    if block_type in ("text", "bullet_list", "table"):
        for shape in slide.shapes:
            if shape.is_placeholder and shape.placeholder_format.idx >= 1:
                return shape

    # Fallback: first shape with text frame
    for shape in slide.shapes:
        if shape.has_text_frame:
            return shape

    # Last resort
    if slide.shapes:
        return slide.shapes[0]

    return None


# ═══════════════════════════════════════════════════════════════════════════════
# CLI
# ═══════════════════════════════════════════════════════════════════════════════

def main():
    import argparse
    parser = argparse.ArgumentParser(
        description="Compose a PPTX from template + content plan + style mapping"
    )
    parser.add_argument("--template", required=True, help="Path to template .pptx")
    parser.add_argument("--plan", required=True, help="Path to detail_plan.json")
    parser.add_argument("--mapping", required=True, help="Path to style_mapping.json")
    parser.add_argument("--images", default="images/", help="Directory with image files")
    parser.add_argument("--output", required=True, help="Output .pptx path")

    args = parser.parse_args()

    compose(
        template_path=args.template,
        plan_path=args.plan,
        mapping_path=args.mapping,
        images_dir=args.images,
        output_path=args.output,
    )


if __name__ == "__main__":
    main()
