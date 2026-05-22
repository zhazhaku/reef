"""
P2: PPT Compositor test suite - unit and integration tests for P2.1-P2.8.
"""
import unittest
import json
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "pkg", "skills", "ppt-agent"))

from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.dml.color import RGBColor

from ppt_compositor import (
    clone_slide, fill_text_block, fill_bullet_list, fill_table_block,
    fill_image_block, create_shape_decor, apply_slide_overrides, compose,
    SHAPE_TYPES, _auto_match_shape,
)

FIXTURES_DIR = os.path.join(os.path.dirname(__file__), "fixtures")
TEMPLATE_PATH = os.path.join(FIXTURES_DIR, "test_template.pptx")
OUTPUT_DIR = os.path.join(FIXTURES_DIR, "output")

# Ensure output dir exists
os.makedirs(OUTPUT_DIR, exist_ok=True)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.1: clone_slide
# ═══════════════════════════════════════════════════════════════════════════════

class TestCloneSlide(unittest.TestCase):
    """P2.1: Slide clone tests."""

    @classmethod
    def setUpClass(cls):
        cls.src_prs = Presentation(TEMPLATE_PATH)

    def _make_dst(self):
        prs = Presentation()
        prs.slide_width = self.src_prs.slide_width
        prs.slide_height = self.src_prs.slide_height
        return prs

    def test_clone_shape_count(self):
        """P2.1.2: Cloned slide has same number of shapes."""
        dst = self._make_dst()
        clone_slide(self.src_prs, 0, dst)
        self.assertEqual(len(self.src_prs.slides[0].shapes), len(dst.slides[0].shapes))

    def test_clone_text_content(self):
        """P2.1.4: Original text preserved in clone."""
        dst = self._make_dst()
        clone_slide(self.src_prs, 0, dst)
        src_text = self.src_prs.slides[0].shapes[0].text
        dst_text = dst.slides[0].shapes[0].text
        self.assertEqual(src_text, dst_text)

    def test_clone_position_preserved(self):
        """P2.1.5: Shape positions preserved."""
        dst = self._make_dst()
        clone_slide(self.src_prs, 0, dst)
        for ss, ds in zip(self.src_prs.slides[0].shapes, dst.slides[0].shapes):
            self.assertEqual(ss.left, ds.left)
            self.assertEqual(ss.top, ds.top)
            self.assertEqual(ss.width, ds.width)
            self.assertEqual(ss.height, ds.height)

    def test_clone_integration_all_slides(self):
        """P2.1.6: Clone all 5 slides, save, verify."""
        dst = self._make_dst()
        for i in range(len(self.src_prs.slides)):
            clone_slide(self.src_prs, i, dst)
        self.assertEqual(len(dst.slides), 5)
        output = os.path.join(OUTPUT_DIR, "clone_integration.pptx")
        dst.save(output)
        self.assertTrue(os.path.exists(output))
        self.assertGreater(os.path.getsize(output), 1000)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.2: fill_text_block
# ═══════════════════════════════════════════════════════════════════════════════

class TestFillTextBlock(unittest.TestCase):
    """P2.2: Text block fill tests."""

    @classmethod
    def setUpClass(cls):
        cls.src_prs = Presentation(TEMPLATE_PATH)

    def _get_cover_title(self, dst_prs=None):
        if dst_prs is None:
            dst_prs = Presentation()
            dst_prs.slide_width = self.src_prs.slide_width
            dst_prs.slide_height = self.src_prs.slide_height
            clone_slide(self.src_prs, 0, dst_prs)
        return dst_prs.slides[0].shapes[0], dst_prs

    def test_fill_text_replace(self):
        """P2.2.2: Text replaced correctly."""
        shape, prs = self._get_cover_title()
        fill_text_block(shape, {"content": "项目背景"})
        self.assertIn("项目背景", shape.text)
        self.assertNotIn("Template Cover Title", shape.text)

    def test_fill_text_font_override(self):
        """P2.2.3: font_size override applied."""
        shape, prs = self._get_cover_title()
        fill_text_block(shape, {"content": "Test"}, {"font_size": 32})
        font_size = shape.text_frame.paragraphs[0].runs[0].font.size
        self.assertEqual(font_size, Pt(32))

    def test_fill_text_color_override(self):
        """P2.2.4: color override applied."""
        shape, prs = self._get_cover_title()
        fill_text_block(shape, {"content": "Red Text"}, {"color": "#FF0000"})
        color = shape.text_frame.paragraphs[0].runs[0].font.color.rgb
        self.assertEqual(str(color), "FF0000")

    def test_fill_text_multiline(self):
        """P2.2.5: \\n splits into multiple paragraphs."""
        shape, prs = self._get_cover_title()
        fill_text_block(shape, {"content": "Line 1\nLine 2\nLine 3"})
        self.assertEqual(len(shape.text_frame.paragraphs), 3)
        self.assertEqual(shape.text_frame.paragraphs[1].text, "Line 2")

    def test_fill_text_no_overrides(self):
        """P2.2.6: No overrides → preserves original style."""
        shape, prs = self._get_cover_title()
        fill_text_block(shape, {"content": "Keep Style"})
        self.assertIn("Keep Style", shape.text)

    def test_fill_text_integration(self):
        """P2.2.7: Fill 3 different text shapes in one slide."""
        prs = Presentation()
        prs.slide_width = self.src_prs.slide_width
        prs.slide_height = self.src_prs.slide_height
        clone_slide(self.src_prs, 0, prs)
        slide = prs.slides[0]

        fill_text_block(slide.shapes[0], {"content": "Main Title"}, {"font_size": 44, "color": "#1A56DB"})
        fill_text_block(slide.shapes[1], {"content": "Subtitle Line"}, {"font_size": 24, "color": "#666666"})

        self.assertIn("Main Title", slide.shapes[0].text)
        self.assertIn("Subtitle Line", slide.shapes[1].text)

        output = os.path.join(OUTPUT_DIR, "fill_text_integration.pptx")
        prs.save(output)
        self.assertGreater(os.path.getsize(output), 1000)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.3: fill_bullet_list
# ═══════════════════════════════════════════════════════════════════════════════

class TestFillBulletList(unittest.TestCase):
    """P2.3: Bullet list fill tests."""

    @classmethod
    def setUpClass(cls):
        cls.src_prs = Presentation(TEMPLATE_PATH)

    def _get_content_body(self):
        prs = Presentation()
        prs.slide_width = self.src_prs.slide_width
        prs.slide_height = self.src_prs.slide_height
        clone_slide(self.src_prs, 1, prs)
        return prs.slides[0].shapes[1], prs  # Body placeholder (idx=1)

    def test_fill_bullet_count(self):
        """P2.3.2: 4 items → 4 paragraphs."""
        shape, _ = self._get_content_body()
        fill_bullet_list(shape, {"items": ["A", "B", "C", "D"]})
        # Count paragraphs with text
        text_paras = [p for p in shape.text_frame.paragraphs if p.text.strip()]
        self.assertEqual(len(text_paras), 4)

    def test_fill_bullet_font_size(self):
        """P2.3.3: overrides font_size applied to all items."""
        shape, _ = self._get_content_body()
        fill_bullet_list(shape, {"items": ["One", "Two"]}, {"font_size": 14})
        for p in shape.text_frame.paragraphs:
            if p.runs:
                self.assertEqual(p.runs[0].font.size, Pt(14))

    def test_fill_bullet_integration(self):
        """P2.3.6: Fill bullet list with correct layout."""
        shape, prs = self._get_content_body()
        fill_bullet_list(shape, {
            "items": ["Item 1 - Detail", "Item 2 - More detail", "Item 3 - Summary"]
        }, {"font_size": 18})
        self.assertEqual(len(shape.text_frame.paragraphs), 3)
        output = os.path.join(OUTPUT_DIR, "fill_bullet_integration.pptx")
        prs.save(output)
        self.assertGreater(os.path.getsize(output), 1000)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.4: fill_table_block
# ═══════════════════════════════════════════════════════════════════════════════

class TestFillTableBlock(unittest.TestCase):
    """P2.4: Table fill tests."""

    def _create_table_slide(self):
        """Create a slide with a table shape."""
        prs = Presentation()
        slide = prs.slides.add_slide(prs.slide_layouts[5])  # Blank or Title Only
        rows, cols = 3, 3
        left, top, width, height = Inches(1), Inches(2), Inches(8), Inches(3)
        shape = slide.shapes.add_table(rows, cols, left, top, width, height)
        return shape, prs

    def test_fill_table_headers(self):
        """P2.4.2: Headers filled + bold."""
        shape, _ = self._create_table_slide()
        fill_table_block(shape, {
            "headers": ["Name", "Value", "Status"],
            "rows": [],
        })
        self.assertEqual(shape.table.cell(0, 0).text, "Name")
        # Check bold
        run = shape.table.cell(0, 0).text_frame.paragraphs[0].runs[0]
        self.assertTrue(run.font.bold)

    def test_fill_table_data(self):
        """P2.4.3: Data rows correctly filled."""
        shape, _ = self._create_table_slide()
        fill_table_block(shape, {
            "headers": ["A", "B", "C"],
            "rows": [["1", "2", "3"], ["4", "5", "6"]],
        })
        self.assertEqual(shape.table.cell(1, 0).text, "1")
        self.assertEqual(shape.table.cell(2, 2).text, "6")

    def test_fill_table_empty(self):
        """P2.4.5: Empty headers + rows → no crash."""
        shape, _ = self._create_table_slide()
        fill_table_block(shape, {"headers": [], "rows": []})
        # Should not raise

    def test_fill_table_integration(self):
        """P2.4.6: Integration - table fills and saves correctly."""
        shape, prs = self._create_table_slide()
        fill_table_block(shape, {
            "headers": ["Metric", "Q1", "Q2"],
            "rows": [["Revenue", "$10M", "$12M"], ["Growth", "5%", "8%"]],
        }, {"font_size": 12})
        output = os.path.join(OUTPUT_DIR, "fill_table_integration.pptx")
        prs.save(output)
        self.assertGreater(os.path.getsize(output), 1000)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.5: fill_image_block
# ═══════════════════════════════════════════════════════════════════════════════

class TestFillImageBlock(unittest.TestCase):
    """P2.5: Image fill tests."""

    @classmethod
    def setUpClass(cls):
        # Create a test image
        from PIL import Image
        cls.test_image = os.path.join(FIXTURES_DIR, "output", "test_chart.png")
        os.makedirs(os.path.dirname(cls.test_image), exist_ok=True)
        img = Image.new("RGB", (200, 150), color=(68, 114, 196))
        img.save(cls.test_image)

    def _create_image_slide(self):
        prs = Presentation()
        slide = prs.slides.add_slide(prs.slide_layouts[5])
        # Add a picture placeholder
        pic = slide.shapes.add_picture(
            self.test_image, Inches(3), Inches(2), Inches(4), Inches(3)
        )
        return pic, prs, slide

    def test_fill_image_png(self):
        """P2.5.2: PNG image inserted."""
        _, prs, slide = self._create_image_slide()
        # Create a new picture shape
        shape = slide.shapes.add_picture(
            self.test_image, Inches(5), Inches(2), Inches(2), Inches(2)
        )
        self.assertIsNotNone(shape)

    def test_fill_image_missing_file(self):
        """P2.5.6: Missing image raises FileNotFoundError."""
        from ppt_compositor import fill_image_block
        _, __, slide = self._create_image_slide()
        shape = slide.shapes[0]
        with self.assertRaises(FileNotFoundError):
            fill_image_block(
                shape,
                {"image_file": "nonexistent.png"},
                FIXTURES_DIR,
            )

    def test_fill_image_integration(self):
        """P2.5.7: Integration - image in correct position."""
        shape, prs, _ = self._create_image_slide()
        output = os.path.join(OUTPUT_DIR, "fill_image_integration.pptx")
        prs.save(output)
        self.assertGreater(os.path.getsize(output), 1000)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.6: create_shape_decor
# ═══════════════════════════════════════════════════════════════════════════════

class TestCreateShapeDecor(unittest.TestCase):
    """P2.6: Shape decor tests."""

    def _get_blank_slide(self):
        prs = Presentation()
        slide = prs.slides.add_slide(prs.slide_layouts[6])
        return slide, prs

    def test_create_decor_rectangle(self):
        """P2.6.2: Rectangle shape created."""
        slide, _ = self._get_blank_slide()
        initial_count = len(slide.shapes)
        create_shape_decor(slide, {"description": "Test", "shape": "rectangle"})
        self.assertEqual(len(slide.shapes), initial_count + 1)

    def test_create_decor_with_label(self):
        """P2.6.3: Description text in shape."""
        slide, _ = self._get_blank_slide()
        shape = create_shape_decor(slide, {"description": "Growth Chart"})
        self.assertIn("Growth Chart", shape.text)

    def test_create_decor_override_color(self):
        """P2.6.5: Override color applied."""
        slide, _ = self._get_blank_slide()
        shape = create_shape_decor(
            slide,
            {"description": "Colored"},
            {"color": "FF6B35"}
        )
        self.assertIsNotNone(shape.fill.fore_color.rgb)

    def test_create_decor_integration(self):
        """P2.6.6: Decor with various shapes."""
        slide, prs = self._get_blank_slide()
        for i, (shape_type, desc) in enumerate([
            ("rectangle", "Step 1"),
            ("rounded_rectangle", "Step 2"),
            ("chevron", "Step 3"),
        ]):
            create_shape_decor(
                slide,
                {"description": desc, "shape": shape_type},
                {"top": Inches(1 + i * 2), "left": Inches(2)},
            )
        self.assertEqual(len(slide.shapes), 3)
        output = os.path.join(OUTPUT_DIR, "decor_integration.pptx")
        prs.save(output)
        self.assertGreater(os.path.getsize(output), 1000)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.7: apply_slide_overrides
# ═══════════════════════════════════════════════════════════════════════════════

class TestApplySlideOverrides(unittest.TestCase):
    """P2.7: Slide override tests."""

    def _create_two_shape_slide(self):
        prs = Presentation()
        slide = prs.slides.add_slide(prs.slide_layouts[6])
        a = slide.shapes.add_shape(1, Inches(1), Inches(2), Inches(2), Inches(1))
        b = slide.shapes.add_shape(1, Inches(7), Inches(2), Inches(3), Inches(1))
        return slide, prs, a, b

    def test_apply_layout_swap_left_right(self):
        """P2.7.2: left_right swap exchanges left coordinates."""
        slide, _, a, b = self._create_two_shape_slide()
        a_orig_left, b_orig_left = a.left, b.left
        apply_slide_overrides(slide, {"layout_swap": "left_right"})
        self.assertEqual(a.left, b_orig_left)
        self.assertEqual(b.left, a_orig_left)

    def test_apply_empty_overrides(self):
        """P2.7.4: Empty overrides → no change."""
        slide, _, a, _ = self._create_two_shape_slide()
        a_orig = a.left
        apply_slide_overrides(slide, {})
        self.assertEqual(a.left, a_orig)

    def test_apply_integration(self):
        """P2.7.5: Integration - swap then save."""
        slide, prs, a, b = self._create_two_shape_slide()
        a_orig_left, b_orig_left = a.left, b.left
        apply_slide_overrides(slide, {"layout_swap": "left_right"})
        self.assertNotEqual(a.left, a_orig_left)
        output = os.path.join(OUTPUT_DIR, "layout_swap_integration.pptx")
        prs.save(output)
        self.assertGreater(os.path.getsize(output), 1000)


# ═══════════════════════════════════════════════════════════════════════════════
# P2.8: compose + CLI
# ═══════════════════════════════════════════════════════════════════════════════

class TestCompose(unittest.TestCase):
    """P2.8: Compose function and CLI tests."""

    @classmethod
    def setUpClass(cls):
        cls.plan_path = os.path.join(FIXTURES_DIR, "output", "test_plan.json")
        cls.mapping_path = os.path.join(FIXTURES_DIR, "output", "test_mapping.json")
        cls.images_dir = os.path.join(FIXTURES_DIR, "output")

        # Create a test plan (2 slides)
        plan = {
            "title": "Test Presentation",
            "slides": [
                {
                    "slide_number": 1,
                    "slide_type": "COVER",
                    "content_blocks": [
                        {"type": "title", "content": "Project Reef", "overrides": {"font_size": 44}},
                        {"type": "subtitle", "content": "A Distributed AI Swarm", "overrides": {"font_size": 24}},
                    ]
                },
                {
                    "slide_number": 2,
                    "slide_type": "CONTENT",
                    "content_blocks": [
                        {"type": "title", "content": "Key Features"},
                        {"type": "bullet_list", "items": [
                            "Multi-agent orchestration",
                            "Skill-based task routing",
                            "Sandboxed execution",
                        ], "overrides": {"font_size": 18}},
                    ]
                },
            ]
        }
        with open(cls.plan_path, "w") as f:
            json.dump(plan, f, ensure_ascii=False, indent=2)

        # Create style mapping
        mapping = {
            "mappings": [
                {"plan_slide": 1, "template_slide": 1, "reason": "Cover slide"},
                {"plan_slide": 2, "template_slide": 2, "reason": "Content with bullets"},
            ]
        }
        with open(cls.mapping_path, "w") as f:
            json.dump(mapping, f, ensure_ascii=False, indent=2)

    def test_compose_slide_count(self):
        """P2.8.2: Output slide count matches plan."""
        output = os.path.join(OUTPUT_DIR, "compose_slide_count.pptx")
        compose(TEMPLATE_PATH, self.plan_path, self.mapping_path,
                self.images_dir, output)
        prs = Presentation(output)
        self.assertEqual(len(prs.slides), 2)

    def test_compose_mapping_correct(self):
        """P2.8.3: Style mapping respected."""
        output = os.path.join(OUTPUT_DIR, "compose_mapping.pptx")
        compose(TEMPLATE_PATH, self.plan_path, self.mapping_path,
                self.images_dir, output)
        prs = Presentation(output)
        # Cover slide has title + subtitle
        cover_shapes = [s.name for s in prs.slides[0].shapes]
        self.assertTrue(any("Title" in n for n in cover_shapes), f"Cover shapes: {cover_shapes}")

    def test_compose_empty_plan(self):
        """P2.8.5: Empty plan produces empty PPTX."""
        empty_plan = os.path.join(FIXTURES_DIR, "output", "empty_plan.json")
        empty_mapping = os.path.join(FIXTURES_DIR, "output", "empty_mapping.json")
        with open(empty_plan, "w") as f:
            json.dump({"slides": []}, f)
        with open(empty_mapping, "w") as f:
            json.dump({"mappings": []}, f)
        output = os.path.join(OUTPUT_DIR, "compose_empty.pptx")
        compose(TEMPLATE_PATH, empty_plan, empty_mapping, self.images_dir, output)
        prs = Presentation(output)
        self.assertEqual(len(prs.slides), 0)

    def test_compose_cli_help(self):
        """P2.8.6: CLI --help shows all parameters."""
        import subprocess
        script = os.path.join(
            os.path.dirname(__file__), "..", "pkg", "skills", "ppt-agent", "ppt_compositor.py"
        )
        result = subprocess.run(
            ["python3", script, "--help"],
            capture_output=True, text=True, timeout=10,
        )
        self.assertEqual(result.returncode, 0)
        self.assertIn("--template", result.stdout)
        self.assertIn("--plan", result.stdout)
        self.assertIn("--mapping", result.stdout)
        self.assertIn("--output", result.stdout)

    def test_compose_cli_full(self):
        """P2.8.7: Full CLI invocation succeeds."""
        import subprocess
        script = os.path.join(
            os.path.dirname(__file__), "..", "pkg", "skills", "ppt-agent", "ppt_compositor.py"
        )
        output = os.path.join(OUTPUT_DIR, "cli_output.pptx")
        result = subprocess.run(
            ["python3", script,
             "--template", TEMPLATE_PATH,
             "--plan", self.plan_path,
             "--mapping", self.mapping_path,
             "--images", self.images_dir,
             "--output", output],
            capture_output=True, text=True, timeout=30,
        )
        self.assertEqual(result.returncode, 0, f"CLI failed: {result.stderr}")
        self.assertTrue(os.path.exists(output))

    def test_compose_integration_full(self):
        """P2.8.8: Complete 2-slide composition → valid .pptx."""
        output = os.path.join(OUTPUT_DIR, "compose_integration.pptx")
        compose(TEMPLATE_PATH, self.plan_path, self.mapping_path,
                self.images_dir, output)

        prs = Presentation(output)
        self.assertEqual(len(prs.slides), 2)

        # Slide 1: Cover
        s1_text = prs.slides[0].shapes[0].text
        self.assertIn("Project Reef", s1_text)

        # Slide 2: Content with bullets
        s2_texts = [s.text for s in prs.slides[1].shapes if s.has_text_frame]
        all_text = " ".join(s2_texts)
        self.assertIn("Key Features", all_text)
        self.assertIn("Multi-agent", all_text)

        print(f"\nIntegration test output: {output} ({os.path.getsize(output)} bytes)")


if __name__ == "__main__":
    unittest.main(verbosity=2)
