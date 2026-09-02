# Reference-conditioned expressions: Phase 1 acceptance criteria

This file intentionally lands before the implementation so the vertical slice is testable and reviewable.

The canonical portrait defines visual identity. Text describes constraints. Expression generation may alter expression and minimal pose, but must not reinterpret identity.

Phase 1 acceptance criteria:

- An accepted `cover_image_id` resolves to actual image bytes or provider-valid reference data.
- A capable provider adapter receives that image input plus the expression instruction.
- The request records `generation_method` and whether reference generation/image edit is genuinely supported.
- A provider without reference capability takes the deliberate 3x3 fallback path and must not masquerade as reference-conditioned generation.
- Every output records `personality_id`, `canonical_image_id`, `canonical_image_version`, `generation_method`, and `provider`.
- Tests prove the prose-likeness path is supplemental constraints only, not the visual anchor.

Phase 1 vertical slice:

`cover_image_id -> image bytes/reference -> capable adapter -> one expression -> metadata -> inspectable output`

Phase 2 adds identity validation, bounded regeneration, and automatic regeneration when the canonical portrait changes.
