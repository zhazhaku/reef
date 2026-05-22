"""
P1.7 & P4.2: JSON Schema validators for TemplateStructure and StyleMapping.

Provides:
    validate_template_structure(data) -> list[str]  # returns validation errors
    validate_style_mapping(data, template_slide_count) -> list[str]
"""
from template_parser import VALID_SHAPE_TYPES

VALID_SLIDE_TYPES = {"COVER", "SECTION", "CONTENT", "ENDING", "TOC"}
REQUIRED_SLIDE_KEYS = {"slide_number", "slide_type", "shapes"}
REQUIRED_SHAPE_KEYS = {"type", "left", "top", "width", "height", "name", "shape_id"}


def validate_template_structure(data: dict) -> list:
    """Validate a TemplateStructure dict. Returns list of error strings (empty = valid)."""
    errors = []

    if "slides" not in data:
        errors.append("Missing top-level key: 'slides'")
        return errors
    if "theme" not in data:
        errors.append("Missing top-level key: 'theme'")

    slides = data.get("slides", [])
    if not isinstance(slides, list):
        errors.append("'slides' must be a list")
        return errors

    for i, slide in enumerate(slides):
        prefix = f"slides[{i}]"

        # Required slide keys
        for key in REQUIRED_SLIDE_KEYS:
            if key not in slide:
                errors.append(f"{prefix}: missing required key '{key}'")

        # Valid slide type
        stype = slide.get("slide_type")
        if stype not in VALID_SLIDE_TYPES:
            errors.append(f"{prefix}: invalid slide_type '{stype}'")

        # Validate each shape
        for j, shape in enumerate(slide.get("shapes", [])):
            sprefix = f"{prefix}.shapes[{j}]"

            for key in REQUIRED_SHAPE_KEYS:
                if key not in shape:
                    errors.append(f"{sprefix}: missing required key '{key}'")

            stype = shape.get("type")
            if stype not in VALID_SHAPE_TYPES:
                errors.append(f"{sprefix}: invalid shape type '{stype}'")

    return errors


def validate_style_mapping(data: dict, template_slide_count: int) -> list:
    """Validate a StyleMapping dict.

    Args:
        data: The style mapping JSON dict.
        template_slide_count: Total number of slides in the template (1-indexed).

    Returns:
        list of error strings (empty = valid).
    """
    errors = []

    if "mappings" not in data:
        errors.append("Missing top-level key: 'mappings'")
        return errors

    mappings = data["mappings"]
    if not isinstance(mappings, list):
        errors.append("'mappings' must be a list")
        return errors

    seen_plan_slides = set()

    for i, m in enumerate(mappings):
        prefix = f"mappings[{i}]"

        for key in ["plan_slide", "template_slide", "reason"]:
            if key not in m:
                errors.append(f"{prefix}: missing required key '{key}'")

        plan = m.get("plan_slide")
        if plan is not None:
            if plan in seen_plan_slides:
                errors.append(f"{prefix}: duplicate plan_slide {plan}")
            seen_plan_slides.add(plan)

        tpl = m.get("template_slide")
        if tpl is not None:
            if not (1 <= tpl <= template_slide_count):
                errors.append(
                    f"{prefix}: template_slide {tpl} out of range "
                    f"[1, {template_slide_count}]"
                )

    return errors
