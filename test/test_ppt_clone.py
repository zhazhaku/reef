"""
P0.1: Template slide clone feasibility validation.
Tests python-pptx's ability to clone template slides while preserving all styles.
"""
import unittest
import copy
import os
from lxml import etree
from pptx import Presentation
from pptx.util import Inches, Pt, Emu
from pptx.oxml.ns import qn

TEMPLATE_PATH = os.path.join(os.path.dirname(__file__), "fixtures", "test_template.pptx")
OUTPUT_DIR = os.path.join(os.path.dirname(__file__), "fixtures", "output")


def _clone_slide_to_prs(src_prs, src_slide_idx, dst_prs):
    """Core slide clone: deep copy XML of a slide from source to destination presentation."""
    src_slide = src_prs.slides[src_slide_idx]
    # Determine which slide layout to use in the destination
    src_layout = src_slide.slide_layout
    # Find matching layout in dest (by name or index)
    dst_layout = None
    for layout in dst_prs.slide_layouts:
        if layout.name == src_layout.name:
            dst_layout = layout
            break
    if dst_layout is None:
        dst_layout = dst_prs.slide_layouts[src_slide_idx % len(dst_prs.slide_layouts)]

    dst_slide = dst_prs.slides.add_slide(dst_layout)

    # Deep copy all shape XML elements from source to destination
    src_sp_tree = src_slide.shapes._spTree
    dst_sp_tree = dst_slide.shapes._spTree

    # Remove existing placeholder shapes from destination (they come from layout)
    existing_shapes = list(dst_sp_tree)
    for elem in existing_shapes:
        if elem.tag in (qn('p:sp'), qn('p:pic'), qn('p:grpSp'), qn('p:cxnSp'), qn('p:graphicFrame')):
            dst_sp_tree.remove(elem)

    # Copy shapes from source
    for shape_elem in src_sp_tree:
        if shape_elem.tag in (qn('p:sp'), qn('p:pic'), qn('p:grpSp'), qn('p:cxnSp'), qn('p:graphicFrame')):
            imported = copy.deepcopy(shape_elem)
            dst_sp_tree.append(imported)

    return dst_slide


def _get_shape_props(shape):
    """Extract key properties for comparison."""
    return {
        "name": shape.name,
        "shape_type": str(shape.shape_type),
        "left": shape.left,
        "top": shape.top,
        "width": shape.width,
        "height": shape.height,
        "has_text": shape.has_text_frame,
        "text": shape.text if shape.has_text_frame else None,
    }


def _get_text_style(shape):
    """Extract text style properties for comparison."""
    if not shape.has_text_frame:
        return None
    result = []
    for p_idx, para in enumerate(shape.text_frame.paragraphs):
        for r_idx, run in enumerate(para.runs):
            result.append({
                "paragraph": p_idx,
                "run": r_idx,
                "text": run.text,
                "font_size": run.font.size,
                "bold": run.font.bold,
                "italic": run.font.italic,
                "color": _safe_rgb(run.font.color),
            })


def _safe_rgb(color):
    """Extract RGB string safely, handling _NoneColor (theme-inherited)."""
    try:
        if color and color.type is not None:
            return str(color.rgb)
    except (AttributeError, TypeError):
        pass
    return None
    return result


class TestSlideClonePreservation(unittest.TestCase):
    """P0.1.2: Clone preserves shape count, types, and positions."""

    @classmethod
    def setUpClass(cls):
        os.makedirs(OUTPUT_DIR, exist_ok=True)
        cls.src_prs = Presentation(TEMPLATE_PATH)

    def test_clone_shape_count(self):
        """Clone COVER slide: shape count matches."""
        dst_prs = Presentation()
        dst_prs.slide_width = self.src_prs.slide_width
        dst_prs.slide_height = self.src_prs.slide_height

        _clone_slide_to_prs(self.src_prs, 0, dst_prs)

        self.assertEqual(len(self.src_prs.slides[0].shapes), len(dst_prs.slides[0].shapes),
                         f"Shape count mismatch: src={len(self.src_prs.slides[0].shapes)}, dst={len(dst_prs.slides[0].shapes)}")

    def test_clone_shape_types(self):
        """Clone CONTENT slide: shape types match."""
        dst_prs = Presentation()
        dst_prs.slide_width = self.src_prs.slide_width
        dst_prs.slide_height = self.src_prs.slide_height

        _clone_slide_to_prs(self.src_prs, 1, dst_prs)

        src_types = [str(s.shape_type) for s in self.src_prs.slides[1].shapes]
        dst_types = [str(s.shape_type) for s in dst_prs.slides[0].shapes]
        self.assertEqual(src_types, dst_types)

    def test_clone_shape_positions(self):
        """Clone slide: shape positions (left, top) preserved within 1 EMU tolerance."""
        dst_prs = Presentation()
        dst_prs.slide_width = self.src_prs.slide_width
        dst_prs.slide_height = self.src_prs.slide_height

        _clone_slide_to_prs(self.src_prs, 0, dst_prs)

        src_shapes = list(self.src_prs.slides[0].shapes)
        dst_shapes = list(dst_prs.slides[0].shapes)
        self.assertEqual(len(src_shapes), len(dst_shapes))
        for s_s, d_s in zip(src_shapes, dst_shapes):
            self.assertEqual(s_s.left, d_s.left, f"Left mismatch for '{s_s.name}'")
            self.assertEqual(s_s.top, d_s.top, f"Top mismatch for '{s_s.name}'")
            self.assertEqual(s_s.width, d_s.width, f"Width mismatch for '{s_s.name}'")
            self.assertEqual(s_s.height, d_s.height, f"Height mismatch for '{s_s.name}'")

    def test_clone_text_style_preserved(self):
        """P0.1.3: Clone preserves font size, color, bold, italic per run."""
        dst_prs = Presentation()
        dst_prs.slide_width = self.src_prs.slide_width
        dst_prs.slide_height = self.src_prs.slide_height

        _clone_slide_to_prs(self.src_prs, 0, dst_prs)

        src_styles = _get_text_style(self.src_prs.slides[0].shapes[0])
        dst_styles = _get_text_style(dst_prs.slides[0].shapes[0])
        self.assertEqual(src_styles, dst_styles,
                         f"Text style mismatch:\n  src={src_styles}\n  dst={dst_styles}")

    def test_clone_and_replace_text(self):
        """P0.1.1: Clone COVER slide, replace title and subtitle text."""
        dst_prs = Presentation()
        dst_prs.slide_width = self.src_prs.slide_width
        dst_prs.slide_height = self.src_prs.slide_height

        _clone_slide_to_prs(self.src_prs, 0, dst_prs)

        # Replace text
        dst_slide = dst_prs.slides[0]
        dst_slide.shapes[0].text = "REPLACED TITLE"
        dst_slide.shapes[1].text = "REPLACED SUBTITLE"

        # Verify
        self.assertEqual(dst_slide.shapes[0].text, "REPLACED TITLE")
        self.assertEqual(dst_slide.shapes[1].text, "REPLACED SUBTITLE")

        # Font size should be preserved after replace
        self.assertEqual(dst_slide.shapes[0].text_frame.paragraphs[0].runs[0].font.size,
                         self.src_prs.slides[0].shapes[0].text_frame.paragraphs[0].runs[0].font.size)

        # Save for visual verification
        output = os.path.join(OUTPUT_DIR, "clone_replaced.pptx")
        dst_prs.save(output)
        self.assertTrue(os.path.exists(output))
        self.assertGreater(os.path.getsize(output), 1000)

    def test_clone_bullet_list_slide(self):
        """Clone CONTENT slide with bullet list, verify all bullets preserved."""
        dst_prs = Presentation()
        dst_prs.slide_width = self.src_prs.slide_width
        dst_prs.slide_height = self.src_prs.slide_height

        _clone_slide_to_prs(self.src_prs, 1, dst_prs)

        src_slide = self.src_prs.slides[1]
        dst_slide = dst_prs.slides[0]

        # Verify bullet text preserved
        src_body = src_slide.shapes[1]
        dst_body = dst_slide.shapes[1]
        self.assertEqual(len(src_body.text_frame.paragraphs), len(dst_body.text_frame.paragraphs))
        for i, (sp, dp) in enumerate(zip(src_body.text_frame.paragraphs, dst_body.text_frame.paragraphs)):
            self.assertEqual(sp.text, dp.text, f"Paragraph {i} text mismatch")

        # Save for visual verification
        output = os.path.join(OUTPUT_DIR, "clone_bullet_list.pptx")
        dst_prs.save(output)
        self.assertGreater(os.path.getsize(output), 1000)

    def test_clone_all_slides_integration(self):
        """P0.1.4 Integration: Clone all 5 slides to new presentation."""
        dst_prs = Presentation()
        dst_prs.slide_width = self.src_prs.slide_width
        dst_prs.slide_height = self.src_prs.slide_height

        for i in range(len(self.src_prs.slides)):
            _clone_slide_to_prs(self.src_prs, i, dst_prs)

        self.assertEqual(len(dst_prs.slides), len(self.src_prs.slides))

        # Verify each slide has same number of shapes
        for i in range(len(self.src_prs.slides)):
            self.assertEqual(
                len(self.src_prs.slides[i].shapes),
                len(dst_prs.slides[i].shapes),
                f"Slide {i} shape count mismatch"
            )

        output = os.path.join(OUTPUT_DIR, "clone_all_slides.pptx")
        dst_prs.save(output)
        self.assertTrue(os.path.exists(output))
        print(f"\nIntegration test: all slides cloned to {output}")


if __name__ == "__main__":
    unittest.main(verbosity=2)
