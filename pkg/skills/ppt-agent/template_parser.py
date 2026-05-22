"""
P1: Template Parser - Parse .pptx templates into structured JSON for LLM consumption.

Usage:
    python3 template_parser.py template.pptx [--output structure.json]

Output: TemplateStructure JSON with:
    - slides[]: each slide's type, shapes, text content, font styles
    - theme: color scheme and default fonts
"""
import json
import sys
import os
from pptx import Presentation
from pptx.oxml.ns import qn


# ─── Exceptions ───────────────────────────────────────────────────────────────

class InvalidPPTXError(Exception):
    """Raised when the .pptx file is corrupted or unreadable."""
    pass


# ─── P1.1: Load PPTX ──────────────────────────────────────────────────────────

def load_pptx(path: str) -> Presentation:
    """Load a .pptx file and return a Presentation object.

    Raises:
        FileNotFoundError: if path doesn't exist
        InvalidPPTXError: if file is corrupted or not a valid .pptx
    """
    if not os.path.exists(path):
        raise FileNotFoundError(f"Template not found: {path}")
    try:
        return Presentation(path)
    except Exception as e:
        raise InvalidPPTXError(f"Failed to open {path}: {e}") from e


# ─── P1.2: Extract Shape Info ─────────────────────────────────────────────────

# MSO shape type mapping
MSO_TYPE_MAP = {
    "PLACEHOLDER (14)": "PLACEHOLDER",
    "TEXT_BOX (17)": "TEXT_BOX",
    "PICTURE (13)": "PICTURE",
    "TABLE (19)": "TABLE",
    "AUTO_SHAPE (1)": "AUTO_SHAPE",
    "GROUP (6)": "GROUP",
    "CHART (3)": "CHART",
}

VALID_SHAPE_TYPES = set(MSO_TYPE_MAP.values())


def extract_shape_info(shape) -> dict:
    """Extract basic shape metadata: type, position, size, identity.

    Returns:
        {type, left, top, width, height, name, shape_id}
        All position/size values are in EMU (English Metric Units).
    """
    raw_type = str(shape.shape_type)
    shape_type = MSO_TYPE_MAP.get(raw_type, raw_type)

    info = {
        "type": shape_type,
        "left": shape.left,
        "top": shape.top,
        "width": shape.width,
        "height": shape.height,
        "name": shape.name,
        "shape_id": shape.shape_id,
    }

    # Additional placeholder info
    if shape.is_placeholder:
        info["placeholder_idx"] = shape.placeholder_format.idx
        info["placeholder_type"] = str(shape.placeholder_format.type)

    return info


# ─── P1.3: Extract Text Content ───────────────────────────────────────────────

def extract_text_content(shape) -> dict:
    """Extract text and font styling from a shape.

    Returns:
        {full_text, paragraphs: [{text, alignment, runs: [{text, font_name,
          font_size_pt, bold, italic, color_hex}]}]}
        Empty dict if shape has no text_frame.
    """
    if not shape.has_text_frame:
        return {"full_text": "", "paragraphs": []}

    tf = shape.text_frame
    paragraphs = []
    for para in tf.paragraphs:
        runs_info = []
        for run in para.runs:
            run_data = {
                "text": run.text,
                "font_name": run.font.name,
                "font_size_pt": _emu_to_pt(run.font.size),
                "bold": _safe_bool(run.font.bold),
                "italic": _safe_bool(run.font.italic),
                "color_hex": _safe_color_hex(run.font.color),
            }
            runs_info.append(run_data)

        paragraphs.append({
            "text": para.text,
            "alignment": str(para.alignment) if para.alignment else None,
            "level": para.level,
            "runs": runs_info,
        })

    return {
        "full_text": shape.text,
        "paragraphs": paragraphs,
    }


def _emu_to_pt(emu) -> int | None:
    """Convert EMU to points. 1 pt = 12700 EMU."""
    if emu is None:
        return None
    return emu // 12700


def _safe_bool(val) -> bool:
    """Handle TriState / None values for bold/italic."""
    if val is None:
        return False
    if val is True:
        return True
    return False


def _safe_color_hex(color) -> str | None:
    """Extract #RRGGBB hex from a color object, or None if theme-inherited."""
    try:
        if color is not None and color.type is not None:
            return str(color.rgb)
    except (AttributeError, TypeError):
        pass
    return None


# ─── P1.4: Extract Theme ──────────────────────────────────────────────────────

def extract_theme(prs: Presentation) -> dict:
    """Extract theme colors and default fonts from the presentation.

    Returns:
        {slide_width, slide_height, theme_colors: [], default_fonts: {major, minor}}
    """
    theme = {
        "slide_width": prs.slide_width,
        "slide_height": prs.slide_height,
        "theme_colors": [],
        "default_fonts": {"major": None, "minor": None},
    }

    try:
        # Access slide master for theme
        if prs.slide_masters:
            master = prs.slide_masters[0]
            theme_elem = master.element

            # Extract color scheme from XML
            clr_scheme = theme_elem.findall('.//' + qn('a:clrScheme'))
            if clr_scheme:
                scheme = clr_scheme[0]
                color_names = ['dk1', 'lt1', 'dk2', 'lt2', 'accent1', 'accent2',
                              'accent3', 'accent4', 'accent5', 'accent6',
                              'hlink', 'folHlink']
                for cn in color_names:
                    elem = scheme.find(qn(f'a:{cn}'))
                    if elem is not None:
                        srgb = elem.find(qn('a:srgbClr'))
                        if srgb is not None:
                            theme["theme_colors"].append({
                                "name": cn,
                                "hex": f"#{srgb.get('val')}",
                            })

            # Extract font scheme
            font_scheme = theme_elem.findall('.//' + qn('a:fontScheme'))
            if font_scheme:
                scheme = font_scheme[0]
                for font_type, tag in [("major", "a:majorFont"), ("minor", "a:minorFont")]:
                    major = scheme.find(qn(tag))
                    if major is not None:
                        latin = major.find(qn('a:latin'))
                        ea = major.find(qn('a:ea'))
                        theme["default_fonts"][font_type] = {
                            "latin": latin.get('typeface') if latin is not None else None,
                            "east_asian": ea.get('typeface') if ea is not None else None,
                        }
    except Exception:
        # Graceful degradation: theme extraction is best-effort
        pass

    return theme


# ─── P1.5: Infer Slide Type ───────────────────────────────────────────────────

def infer_slide_type(slide_index: int, total_slides: int, shapes_info: list) -> str:
    """Infer the semantic type of a slide based on position and content.

    Rules (in priority order):
        1. First slide with placeholder → COVER
        2. Last slide → ENDING
        3. Only one placeholder (title only, no body) → SECTION
        4. Any other → CONTENT
    """
    placeholder_indices = [
        s.get("placeholder_idx")
        for s in shapes_info
        if s.get("placeholder_idx") is not None
    ]

    # Rule 1: First slide is COVER
    if slide_index == 0 and len(shapes_info) >= 2:
        return "COVER"

    # Rule 2: Last slide is ENDING
    if slide_index == total_slides - 1:
        return "ENDING"

    # Rule 3: Title-only (placeholder 0) with no body placeholder (1) → SECTION
    has_title = 0 in placeholder_indices
    has_body = 1 in placeholder_indices
    if has_title and not has_body and len(shapes_info) <= 2:
        return "SECTION"

    # Rule 4: Default to CONTENT
    return "CONTENT"


# ─── P1.6: Main Parser ────────────────────────────────────────────────────────

def parse_template(pptx_path: str) -> dict:
    """Parse a .pptx template into a TemplateStructure JSON dict.

    This is the main entry point for P1. Combines all P1.1-P1.5 functions.

    Args:
        pptx_path: Path to the .pptx template file.

    Returns:
        dict with keys: slides, theme

    Raises:
        FileNotFoundError, InvalidPPTXError
    """
    prs = load_pptx(pptx_path)
    total = len(prs.slides)

    slides = []
    for idx, slide in enumerate(prs.slides):
        shapes = []
        for shape in slide.shapes:
            shape_info = extract_shape_info(shape)
            text_content = extract_text_content(shape)
            if text_content["full_text"] or text_content["paragraphs"]:
                shape_info["text_content"] = text_content
            shapes.append(shape_info)

        slide_type = infer_slide_type(idx, total, shapes)

        slides.append({
            "slide_number": idx + 1,
            "slide_type": slide_type,
            "shapes": shapes,
        })

    theme = extract_theme(prs)

    return {
        "slides": slides,
        "theme": theme,
    }


# ─── CLI ──────────────────────────────────────────────────────────────────────

def main():
    import argparse
    parser = argparse.ArgumentParser(description="Parse a PPTX template into structured JSON")
    parser.add_argument("template", help="Path to .pptx template file")
    parser.add_argument("--output", "-o", help="Output JSON file path (default: stdout)")

    args = parser.parse_args()

    result = parse_template(args.template)
    json_str = json.dumps(result, ensure_ascii=False, indent=2)

    if args.output:
        os.makedirs(os.path.dirname(args.output) or ".", exist_ok=True)
        with open(args.output, "w", encoding="utf-8") as f:
            f.write(json_str)
        print(f"Template structure written to {args.output}")
    else:
        print(json_str)


if __name__ == "__main__":
    main()
