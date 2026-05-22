"""
P3 & P4: Prompt validation tests.
Verifies that each prompt .md file exists and contains required sections/keywords.
"""
import unittest
import os

PROMPTS_DIR = os.path.join(
    os.path.dirname(__file__), "..", "pkg", "skills", "ppt-agent", "prompts"
)


def _load_prompt(filename: str) -> str:
    path = os.path.join(PROMPTS_DIR, filename)
    with open(path, "r", encoding="utf-8") as f:
        return f.read()


# ═══════════════════════════════════════════════════════════════════════════════
# P3.1: Outline Prompt
# ═══════════════════════════════════════════════════════════════════════════════

class TestOutlinePrompt(unittest.TestCase):
    """P3.1: Outline generation prompt tests."""

    @classmethod
    def setUpClass(cls):
        cls.content = _load_prompt("outline_prompt.md")

    def test_prompt_exists(self):
        self.assertGreater(len(self.content), 100)

    def test_mentions_output_json_format(self):
        """P3.1.2: Prompt mentions JSON output."""
        self.assertIn("JSON", self.content)

    def test_defines_slide_fields(self):
        """P3.1.3: Output schema includes title, slides, slide_number, summary."""
        self.assertIn("title", self.content)
        self.assertIn("slides", self.content)
        self.assertIn("slide_number", self.content)
        self.assertIn("summary", self.content)

    def test_constrains_slide_count(self):
        """P3.1.4: Prompt mentions slide count constraint."""
        self.assertTrue(
            any(phrase in self.content.lower() for phrase in
                ["slide count", "total slides", "≤", "≤ template"])
        )


# ═══════════════════════════════════════════════════════════════════════════════
# P3.2: Detail Prompt
# ═══════════════════════════════════════════════════════════════════════════════

class TestDetailPrompt(unittest.TestCase):
    """P3.2: Detail content prompt tests."""

    @classmethod
    def setUpClass(cls):
        cls.content = _load_prompt("detail_prompt.md")

    def test_block_types_documented(self):
        """P3.2.2: All block types mentioned."""
        for block_type in ["title", "text", "bullet_list", "table", "image"]:
            self.assertIn(block_type, self.content,
                          f"Missing block type: {block_type}")

    def test_overrides_field_explained(self):
        """P3.2.3: Prompt explains overrides field."""
        self.assertIn("overrides", self.content)

    def test_image_description_guidelines(self):
        """P3.2.4: Image description requirements mentioned."""
        self.assertIn("description", self.content)


# ═══════════════════════════════════════════════════════════════════════════════
# P3.3: Template Analysis Prompt
# ═══════════════════════════════════════════════════════════════════════════════

class TestTemplateAnalysisPrompt(unittest.TestCase):
    """P3.3: Template analysis prompt tests."""

    @classmethod
    def setUpClass(cls):
        cls.content = _load_prompt("template_analysis.md")

    def test_output_markdown(self):
        """P3.3.2: Prompt requests Markdown output."""
        self.assertIn("Markdown", self.content)

    def test_per_slide_analysis(self):
        """P3.3.3: Prompt requests per-slide type analysis."""
        self.assertTrue(
            "slide" in self.content.lower() and
            ("type" in self.content.lower() or "COVER" in self.content)
        )


# ═══════════════════════════════════════════════════════════════════════════════
# P4.1: Style Mapping Prompt
# ═══════════════════════════════════════════════════════════════════════════════

class TestStyleMappingPrompt(unittest.TestCase):
    """P4.1: Style mapping prompt tests."""

    @classmethod
    def setUpClass(cls):
        cls.content = _load_prompt("style_mapping_prompt.md")

    def test_output_schema_fields(self):
        """P4.1.2: Required output fields documented."""
        self.assertIn("plan_slide", self.content)
        self.assertIn("template_slide", self.content)

    def test_reason_field_required(self):
        """P4.1.3: Reason field required."""
        self.assertIn("reason", self.content)


# ═══════════════════════════════════════════════════════════════════════════════
# P4.2: Style Mapping Schema (schema.py)
# ═══════════════════════════════════════════════════════════════════════════════

class TestStyleMappingSchema(unittest.TestCase):
    """P4.2: Style mapping validation tests."""

    @classmethod
    def setUpClass(cls):
        import sys
        sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "pkg", "skills", "ppt-agent"))
        from schema import validate_style_mapping as vsm
        cls.validate_fn = vsm

    def test_valid_mapping(self):
        """P4.2.2: Valid mapping passes."""
        data = {
            "mappings": [
                {"plan_slide": 1, "template_slide": 1, "reason": "Cover"},
                {"plan_slide": 2, "template_slide": 3, "reason": "Content"},
            ]
        }
        errors = TestStyleMappingSchema.validate_fn(data, 5)
        self.assertEqual(errors, [])

    def test_duplicate_plan_slide(self):
        """P4.2.3: Duplicate plan_slide fails."""
        data = {
            "mappings": [
                {"plan_slide": 1, "template_slide": 1, "reason": "A"},
                {"plan_slide": 1, "template_slide": 2, "reason": "B"},
            ]
        }
        errors = TestStyleMappingSchema.validate_fn(data, 5)
        self.assertTrue(len(errors) > 0)
        self.assertTrue(any("duplicate" in e for e in errors))

    def test_template_out_of_range(self):
        """P4.2.4: template_slide out of range fails."""
        data = {
            "mappings": [
                {"plan_slide": 1, "template_slide": 99, "reason": "Invalid"},
            ]
        }
        errors = TestStyleMappingSchema.validate_fn(data, 5)
        self.assertTrue(len(errors) > 0)
        self.assertTrue(any("out of range" in e for e in errors))


if __name__ == "__main__":
    unittest.main(verbosity=2)
