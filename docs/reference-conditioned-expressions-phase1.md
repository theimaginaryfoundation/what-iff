# Reference-conditioned expressions: Phase 1

> The canonical portrait defines visual identity. Text describes constraints. Expression generation may alter expression and minimal pose, but must not reinterpret identity.

## Acceptance criteria

- An accepted `cover_image_id` resolves to actual image bytes or provider-valid reference data.
- A capable provider adapter receives that image input plus the expression instruction.
- The request records `generation_method` and whether reference generation/image edit is genuinely supported.
- A provider without reference capability takes the deliberate 3x3 fallback path and must not masquerade as reference-conditioned generation.
- Every generated output receives an inspectable sidecar receipt containing `personality_id`, `canonical_image_id`, `canonical_image_version`, `generation_method`, reference capability/input state, and `provider`.
- Tests prove the canonical image bytes cross the provider boundary. The prose likeness remains supplemental constraints for the quality path, not the visual anchor.

## Phase 1 vertical slice

`cover_image_id -> image bytes -> capable adapter -> reference edit -> expression -> generation receipt`

The quality path is gated by:

```text
EXPRESSION_REFERENCE_GENERATION_ENABLED=true
```

When enabled, Phase 1 regenerates only `happy` and `content` independently from the same immutable canonical portrait. The other seven default expressions continue to use the existing 3x3 grid. If the provider cannot genuinely accept a reference image, all nine remain on the explicit `grid_fallback` path.

The OpenAI adapter uses the image-edit endpoint with high input fidelity. Each expression starts from the original canonical portrait; generated expressions are never chained as references for later expressions.

## Provenance

Each generated image stores a sibling object at:

`<image-s3-key>.generation.json`

The receipt records whether image bytes were actually supplied. Validation makes these states impossible to conflate:

- `reference_edit` / `reference_generation` require a canonical image, supported capability, and `reference_input_supplied=true`.
- `grid_fallback` requires `reference_input_supplied=false`.

For Phase 1, `canonical_image_version` is derived from the immutable cover attachment ID (`attachment:<uuid>`). Replacing the canonical portrait therefore produces a new version token without requiring a schema migration before the mechanism is proven.

## Phase 2

After the anchor is validated in production:

- move all default expressions to independent canonical-reference generation,
- store first-class canonical-image/version provenance in the expression data model,
- add identity-consistency validation,
- use bounded regeneration on validation failure,
- surface terminal identity-preservation failure rather than silently accepting a mismatched character,
- regenerate or mark expressions stale when the canonical portrait changes.
