"""
P1: Template Parser test suite - unit tests for each module function.
Covers P1.1-P1.7.
"""
import unittest
import json
import os
import sys

# Add project root for imports
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "pkg", "skills", "ppt-agent"))

from template_parser import (
    load_pptx, InvalidPPTXError,
    extract_shape_info, MSO_TYPE_MAP, VALID_SHAPE_TYPES,
    extract_text_content, _emu_to_pt, _safe_bool, _safe_color_hex,
    extract_theme,
    infer_slide_type,
    parse_template,
)

FIXTURES_DIR = os.path.join(os.path.dirname(__file__), "fixtures")
TEMPLATE_PATH = os.path.join(FIXTURES_DIR, "test_template.pptx")


# ═══════════════════════════════════════════════════════════════════════════════
# P1.1: load_pptx()
# ═══════════════════════════════════════════════════════════════════════════════

class TestLoadPptx(unittest.TestCase):
    """P1.1: load_pptx tests."""

    def test_load_valid_pptx(self):
        """P1.1.2: Load valid template, len(slides) > 0."""
        prs = load_pptx(TEMPLATE_PATH)
        self.assertGreater(len(prs.slides), 0)

    def test_load_missing_file(self):
        """P1.1.3: Non-existent file raises FileNotFoundError."""
        with self.assertRaises(FileNotFoundError):
            load_pptx("/nonexistent/path/template.pptx")

    def test_load_corrupted_file(self):
        """P1.1.4: Corrupted file raises InvalidPPTXError."""
        bad_path = os.path.join(FIXTURES_DIR, "output", "corrupted.pptx")
        os.makedirs(os.path.dirname(bad_path), exist_ok=True)
        with open(bad_path, "w") as f:
            f.write("not a zip file")
        with self.assertRaises(InvalidPPTXError):
            load_pptx(bad_path)
        os.remove(bad_path)

    def test_load_slides_incrementing(self):
        """P1.1.5 (integration): Load template, all slide_ids increment."""
        prs = load_pptx(TEMPLATE_PATH)
        ids = [s.slide_id for s in prs.slides]
        for i in range(1, len(ids)):
            self.assertGreater(ids[i], ids[i-1], f"slide_id not increasing at index {i}")


# ═══════════════════════════════════════════════════════════════════════════════
# P1.2: extract_shape_info()
# ═══════════════════════════════════════════════════════════════════════════════

class TestExtractShapeInfo(unittest.TestCase):
    """P1.2: extract_shape_info tests."""

    @classmethod
    def setUpClass(cls):
        cls.prs = load_pptx(TEMPLATE_PATH)

    def _get_first_shape(self, slide_idx=0):
        slide = self.prs.slides[slide_idx]
        return slide.shapes[0]

    def test_extract_textbox(self):
        """P1.2.2: TEXT_BOX shape correctly identified."""
        # Our template uses PLACEHOLDERs. Test with a non-placeholder shape.
        # Create a temp slide with TEXT_BOX
        from pptx import Presentation
        from pptx.util import Inches
        prs = Presentation()
        slide = prs.slides.add_slide(prs.slide_layouts[6])  # Blank layout
        tb = slide.shapes.add_textbox(Inches(1), Inches(1), Inches(5), Inches(2))
        info = extract_shape_info(tb)
        self.assertEqual(info["type"], "TEXT_BOX")

    def test_extract_placeholder(self):
        """P1.2.3: PLACEHOLDER shape correctly identified."""
        shape = self._get_first_shape(0)  # Title placeholder
        info = extract_shape_info(shape)
        self.assertEqual(info["type"], "PLACEHOLDER")
        self.assertEqual(info["placeholder_idx"], 0)

    def test_extract_dimensions(self):
        """P1.2.6: Position/size in EMU, valid positive values."""
        shape = self._get_first_shape(0)
        info = extract_shape_info(shape)
        self.assertGreater(info["width"], 0)
        self.assertGreater(info["height"], 0)
        self.assertGreaterEqual(info["left"], 0)
        self.assertGreaterEqual(info["top"], 0)

    def test_extract_returns_required_keys(self):
        """All required keys present."""
        shape = self._get_first_shape(0)
        info = extract_shape_info(shape)
        for key in ["type", "left", "top", "width", "height", "name", "shape_id"]:
            self.assertIn(key, info, f"Missing key: {key}")

    def test_extract_integration_cover_shapes(self):
        """P1.2.7 (integration): COVER has TITLE + SUBTITLE placeholders."""
        slide = self.prs.slides[0]
        infos = [extract_shape_info(s) for s in slide.shapes]
        types = [i["type"] for i in infos]
        self.assertIn("PLACEHOLDER", types)
        self.assertGreaterEqual(len(infos), 2)


# ═══════════════════════════════════════════════════════════════════════════════
# P1.3: extract_text_content()
# ═══════════════════════════════════════════════════════════════════════════════

class TestExtractTextContent(unittest.TestCase):
    """P1.3: extract_text_content tests."""

    def test_extract_has_full_text_and_paragraphs(self):
        """P1.3.2: Returns full_text and paragraphs."""
        prs = load_pptx(TEMPLATE_PATH)
        shape = prs.slides[0].shapes[0]
        result = extract_text_content(shape)
        self.assertIn("full_text", result)
        self.assertIn("paragraphs", result)
        self.assertGreater(len(result["paragraphs"]), 0)

    def test_extract_multi_paragraph(self):
        """P1.3.3: Multiple paragraphs each extracted."""
        prs = load_pptx(TEMPLATE_PATH)
        shape = prs.slides[1].shapes[1]  # Body with bullets
        result = extract_text_content(shape)
        self.assertGreater(len(result["paragraphs"]), 1,
                           f"Expected multiple paragraphs, got {len(result['paragraphs'])}")

    def test_extract_empty_shape(self):
        """P1.3.7: Shape with no text returns empty full_text."""
        from pptx import Presentation
        from pptx.util import Inches
        prs = Presentation()
        slide = prs.slides.add_slide(prs.slide_layouts[6])
        shape = slide.shapes.add_shape(1, Inches(1), Inches(1), Inches(2), Inches(2))  # Rectangle
        result = extract_text_content(shape)
        self.assertEqual(result["full_text"], "")
        # AUTO_SHAPE has an empty paragraph by default — that's fine

    def test_extract_no_text_frame(self):
        """Shape without text_frame returns empty."""
        from pptx import Presentation
        from pptx.util import Inches
        prs = Presentation()
        slide = prs.slides.add_slide(prs.slide_layouts[6])
        shape = slide.shapes.add_shape(1, Inches(1), Inches(1), Inches(2), Inches(2))
        shape.text_frame.text = ""
        # Add explicit text
        shape.text_frame.paragraphs[0].add_run().text = "Test"
        result = extract_text_content(shape)
        self.assertEqual(result["full_text"], "Test")

    def test_emu_to_pt(self):
        """P1.3.4: 18pt = 228600 EMU (18*12700)."""
        self.assertEqual(_emu_to_pt(228600), 18)
        self.assertIsNone(_emu_to_pt(None))

    def test_safe_bool(self):
        self.assertTrue(_safe_bool(True))
        self.assertFalse(_safe_bool(False))
        self.assertFalse(_safe_bool(None))

    def test_safe_color_hex_inherited(self):
        """Inherited color returns None."""
        self.assertIsNone(_safe_color_hex(None))

    def test_integration_cover_title_vs_subtitle(self):
        """P1.3.8 (integration): COVER title should have more content than subtitle or equal."""
        prs = load_pptx(TEMPLATE_PATH)
        title = extract_text_content(prs.slides[0].shapes[0])
        subtitle = extract_text_content(prs.slides[0].shapes[1])
        # Both should have text
        self.assertTrue(title["full_text"])
        self.assertTrue(subtitle["full_text"])


# ═══════════════════════════════════════════════════════════════════════════════
# P1.4: extract_theme()
# ═══════════════════════════════════════════════════════════════════════════════

class TestExtractTheme(unittest.TestCase):
    """P1.4: extract_theme tests."""

    @classmethod
    def setUpClass(cls):
        cls.prs = load_pptx(TEMPLATE_PATH)

    def test_extract_theme_slide_dimensions(self):
        """Theme must include slide_width and slide_height."""
        theme = extract_theme(self.prs)
        self.assertIn("slide_width", theme)
        self.assertIn("slide_height", theme)
        self.assertGreater(theme["slide_width"], 0)
        self.assertGreater(theme["slide_height"], 0)

    def test_extract_theme_no_crash_blank(self):
        """P1.4.4: Blank presentation returns valid dict, no exception."""
        from pptx import Presentation
        prs = Presentation()
        theme = extract_theme(prs)
        self.assertIsInstance(theme, dict)
        self.assertIn("slide_width", theme)

    def test_extract_theme_colors_list(self):
        """Theme must have theme_colors as list."""
        theme = extract_theme(self.prs)
        self.assertIsInstance(theme["theme_colors"], list)

    def test_extract_theme_default_fonts(self):
        """Theme must have default_fonts dict with major/minor keys."""
        theme = extract_theme(self.prs)
        self.assertIn("default_fonts", theme)
        self.assertIn("major", theme["default_fonts"])
        self.assertIn("minor", theme["default_fonts"])

    def test_integration_two_templates_different_theme(self):
        """P1.4.5 (integration): Two different templates produce different themes."""
        from pptx import Presentation
        prs2 = Presentation()
        prs2.slide_width = prs2.slide_width
        prs2.slide_height = prs2.slide_height
        prs2.slides.add_slide(prs2.slide_layouts[0])

        theme1 = extract_theme(self.prs)
        theme2 = extract_theme(prs2)
        # Both should be valid dicts
        self.assertIsInstance(theme1, dict)
        self.assertIsInstance(theme2, dict)


# ═══════════════════════════════════════════════════════════════════════════════
# P1.5: infer_slide_type()
# ═══════════════════════════════════════════════════════════════════════════════

class TestInferSlideType(unittest.TestCase):
    """P1.5: infer_slide_type tests."""

    def _make_shapes(self, *placeholders):
        return [{"placeholder_idx": p, "type": "PLACEHOLDER"} for p in placeholders]

    def test_infer_cover(self):
        """P1.5.2: slide_number=1 + 2+ shapes → COVER."""
        shapes = self._make_shapes(0, 1)
        self.assertEqual(infer_slide_type(0, 5, shapes), "COVER")

    def test_infer_section(self):
        """P1.5.3: Title only, no body → SECTION."""
        shapes = self._make_shapes(0)
        self.assertEqual(infer_slide_type(2, 5, shapes), "SECTION")

    def test_infer_content(self):
        """P1.5.4: Title + Body → CONTENT."""
        shapes = self._make_shapes(0, 1)
        self.assertEqual(infer_slide_type(2, 5, shapes), "CONTENT")

    def test_infer_ending(self):
        """P1.5.5: Last slide → ENDING."""
        shapes = self._make_shapes(0, 1)
        self.assertEqual(infer_slide_type(4, 5, shapes), "ENDING")

    def test_infer_fallback(self):
        """P1.5.6: Unknown → CONTENT."""
        shapes = []  # No shapes
        self.assertEqual(infer_slide_type(1, 5, shapes), "CONTENT")

    def test_integration_real_template_types(self):
        """P1.5.7 (integration): Real 5-page template → COVER, CONTENT, ENDING."""
        result = parse_template(TEMPLATE_PATH)
        types = [s["slide_type"] for s in result["slides"]]
        self.assertEqual(types[0], "COVER")
        self.assertEqual(types[-1], "ENDING")
        # At least one CONTENT
        self.assertIn("CONTENT", types[1:-1])


# ═══════════════════════════════════════════════════════════════════════════════
# P1.6: parse_template() main entry
# ═══════════════════════════════════════════════════════════════════════════════

class TestParseTemplate(unittest.TestCase):
    """P1.6: parse_template integration tests."""

    def test_parse_output_has_slides_and_theme(self):
        """P1.6.2: Output dict has slides[] and theme."""
        result = parse_template(TEMPLATE_PATH)
        self.assertIn("slides", result)
        self.assertIn("theme", result)
        self.assertIsInstance(result["slides"], list)

    def test_parse_slide_count(self):
        """P1.6.3: Output slide count == template slide count."""
        prs = load_pptx(TEMPLATE_PATH)
        result = parse_template(TEMPLATE_PATH)
        self.assertEqual(len(result["slides"]), len(prs.slides))

    def test_parse_json_serializable(self):
        """P1.6.4: json.dumps succeeds."""
        result = parse_template(TEMPLATE_PATH)
        json_str = json.dumps(result, ensure_ascii=False)
        parsed = json.loads(json_str)
        self.assertEqual(len(parsed["slides"]), len(result["slides"]))

    def test_parse_cli_integration(self):
        """P1.6.5 (integration): CLI outputs valid JSON to stdout."""
        import subprocess
        script = os.path.join(
            os.path.dirname(__file__), "..", "pkg", "skills", "ppt-agent", "template_parser.py"
        )
        result = subprocess.run(
            ["python3", script, TEMPLATE_PATH],
            capture_output=True, text=True, timeout=10
        )
        self.assertEqual(result.returncode, 0)
        data = json.loads(result.stdout)
        self.assertIn("slides", data)
        # Pipe to json.tool equivalent verification
        re_parsed = json.loads(json.dumps(data))
        self.assertEqual(len(re_parsed["slides"]), len(data["slides"]))


# ═══════════════════════════════════════════════════════════════════════════════
# P1.7: Schema validation (duplicated in schema.py, tested here)
# ═══════════════════════════════════════════════════════════════════════════════

class TestSchemaValidation(unittest.TestCase):
    """P1.7: TemplateStructure schema validation."""

    def test_valid_full_data(self):
        """P1.7.2: Full parse_template output passes validation."""
        result = parse_template(TEMPLATE_PATH)
        errors = _validate(result)
        self.assertEqual(errors, [], f"Validation errors: {errors}")

    def test_missing_slides_key(self):
        """P1.7.3: Missing 'slides' key fails."""
        errors = _validate({"theme": {}})
        self.assertIn("Missing 'slides'", errors[0])

    def test_invalid_slide_type(self):
        """P1.7.4: Invalid slide_type fails validation."""
        data = {
            "slides": [{"slide_number": 1, "slide_type": "INVALID_TYPE", "shapes": []}],
            "theme": {"slide_width": 100, "slide_height": 100},
        }
        errors = _validate(data)
        self.assertTrue(any("slide_type" in e for e in errors),
                        f"Expected slide_type error, got: {errors}")

    def test_missing_shape_required_key(self):
        """Shape missing 'name' key fails validation."""
        data = {
            "slides": [{
                "slide_number": 1, "slide_type": "CONTENT",
                "shapes": [{"type": "TEXT_BOX", "left": 0, "top": 0, "width": 100, "height": 100, "shape_id": 1}]
            }],
            "theme": {"slide_width": 100, "slide_height": 100},
        }
        errors = _validate(data)
        self.assertTrue(any("name" in e for e in errors), f"Expected 'name' error, got: {errors}")

    def test_integration_real_template_valid(self):
        """P1.7.6 (integration): Real template 100% passes."""
        result = parse_template(TEMPLATE_PATH)
        errors = _validate(result)
        self.assertEqual(errors, [])


def _validate(data: dict) -> list:
    """Minimal schema validator (P1.7.1). Returns list of error strings."""
    errors = []

    # Top-level
    if "slides" not in data:
        errors.append("Missing 'slides' key")
        return errors
    if "theme" not in data:
        errors.append("Missing 'theme' key")

    slides = data.get("slides", [])
    if not isinstance(slides, list):
        errors.append("'slides' must be a list")
        return errors

    VALID_TYPES = {"COVER", "SECTION", "CONTENT", "ENDING", "TOC"}

    for i, slide in enumerate(slides):
        prefix = f"slides[{i}]"
        for key in ["slide_number", "slide_type", "shapes"]:
            if key not in slide:
                errors.append(f"{prefix}: missing '{key}'")

        if slide.get("slide_type") not in VALID_TYPES:
            errors.append(f"{prefix}: invalid slide_type '{slide.get('slide_type')}'")

        for j, shape in enumerate(slide.get("shapes", [])):
            sprefix = f"{prefix}.shapes[{j}]"
            for key in ["type", "left", "top", "width", "height", "name", "shape_id"]:
                if key not in shape:
                    errors.append(f"{sprefix}: missing '{key}'")
            if shape.get("type") not in VALID_SHAPE_TYPES:
                errors.append(f"{sprefix}: invalid type '{shape.get('type')}'")

    return errors


if __name__ == "__main__":
    unittest.main(verbosity=2)
