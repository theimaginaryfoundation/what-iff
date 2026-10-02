# Package: `internal/stcard`

## Role

Converts between WhatIff personalities and SillyTavern character cards: `chara_card_v2` on export, `chara_card_v2` and `chara_card_v3` on import, as JSON or as a PNG with the card embedded.
Pure and dependency-free (standard library only), so the HTTP handler and the account export share one implementation.

## Responsibilities

- **`Parse`** (`import.go`) reads a card (JSON or PNG) into a name, a flattened system prompt, optional lore files, omitted fields, a warnings list and the verbatim `data` object.
- **`ExtractFromPNG` / `EmbedInPNG` / `IsPNG`** (`png.go`) read and write the base64 card in a PNG `tEXt` chunk (`chara` for v2, `ccv3` for v3, v3 preferred on read).
  Only `tEXt` is read; the picture is left byte-for-byte alone apart from the added chunk.
- **`Compose` / `Split`** (`stcard.go`) are the two halves of the prompt-segment format.
- **`Build` / `Marshal` / `Export`** (`export.go`) reconstruct a card from a personality's name and prompt plus the stored passthrough blob.

## Key decisions

- **Flatten on import, reconstruct on export, preserve everything else.**
  `system_prompt`, `description`, `personality` and `scenario` become one prompt: the bare `system_prompt` first, then each remaining non-empty segment under an exact-match header (`### [Description]`, `### [Personality]`, `### [Scenario]`).
  A card with only a `system_prompt` gets no headers at all.
- **The headers are structural.**
  `Split` recognises a header only as a whole line, exactly, so it stays deterministic.
  Absent headers, or a header that appears twice, make the whole prompt export as `system_prompt`; an export that did split adds `HeaderWarning` to `creator_notes` (once, never stacked across round trips).
- **Over the length limit: shed, then refuse.**
  When the flattened prompt exceeds `ImportOptions.MaxPromptUnits`, the ancillary `scenario` and then `personality` sections are left out (in that order, only as many as needed) and a warning says which.
  The text stays in the passthrough blob, and `OmittedFields` records what was left out so export restores it until the user writes their own section.
  If it is still too long (or nothing was left to shed) the import fails with `ErrPromptTooLong`, whose message states the length and the limit.
- **Behavior fields are never applied to the prompt.**
  `first_mes`, `alternate_greetings`, `mes_example` and `post_history_instructions` are ignored for behavior — post-history injection in particular changes model behavior in ways the card author did not intend for this runtime — and survive only in the passthrough blob.
  `scan_depth`, `token_budget` and `recursive` (runtime lore-injection semantics) are not modelled either.
- **Character book.**
  Up to `LoreFlattenMax` (5) enabled entries are flattened under `### [Lore]` as `#### name` sub-sections; more become one file per entry (`LoreEntry`), returned to the caller to attach.
  Disabled and empty entries are skipped (disabled ones with a warning), entries are ordered by `insertion_order`, and an entry is named by its comment, then name, then keys.
  A book that would push the prompt over `ImportOptions.MaxPromptUnits` spills to files instead; the book never causes fields to be shed.
- **Export keeps the book in the blob.**
  When the blob has a `character_book`, the prompt's `[Lore]` section is dropped on export (the blob is canonical); with no book to hold it, the section stays in `system_prompt` rather than vanishing.
- **Authorship.**
  `creator` is the exporting user's WhatIff username only when the original card named no author; re-exporting an imported card never rewrites its author.
- **Numbers stay exact** through the blob (`json.Number` on decode) so large integers in extensions are not rounded.

## Dependencies

- **Inbound:** `internal/handlers/personality` (import and export endpoints), `internal/exporter` (per-personality `sillytavern.json` in the account export), `internal/models` (`LoreEntry` in the import response).
- **Outbound:** none inside the repo.

## Not modelled (deliberate or TBD)

- v1 cards and `iTXt`/`zTXt` PNG text chunks.
- The picture itself: the server never stores a PNG card's image on import (the web client attaches it as the cover), and on export the caller supplies the cover bytes.
- `{{char}}` / `{{user}}` macros are left literal.
- Lore edits made after import are not reflected in the exported `character_book`.
- Required-by-v2 fields we have no source for (`first_mes`, `tags`, `character_version`) export as the blob's value or empty.

## Testing

- `stcard_test.go` — compose/split round trips, mangled-header fallback, every parse rule above, build semantics, and a full import → export → import round trip.
- `omit_test.go` — shedding order, the still-too-long error, lore never causing a shed, and export restoring omitted fields.
- `png_test.go` — embed/extract round trip, replacing an existing card, v3 preference, malformed PNGs, and `Parse` on a PNG.

## Related documentation

- [ADR 0x024](../../docs/adr/0x024-sillytavern-card-import-export.md)
