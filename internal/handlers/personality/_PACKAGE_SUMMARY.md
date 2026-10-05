# Package: `internal/handlers/personality`

## Role

HTTP API for **personalities** — CRUD, defaults, file attachments, and personality generation flows at `/api/personality/...`.

## Responsibilities

- **`Handler`:** Core routes in `handler.go`; file attachment helpers in `fileattachment.go`; provider binding in `provider.go`.
- Generation and default flows may invoke `internal/agent` (see `generate.go`, `create_default.go`).
- **Expressions:** slot CRUD in `expressions.go`; `POST .../expressions/generate-default-grid` (`expression_grid.go`) enqueues the default 3×3 grid; `POST .../expressions/generate-candidates` (`expression_candidates.go`) validates nine unique URL-safe keys + optional `reference_image_id` and enqueues an unassigned-candidates run (the UI's Generate modal assigns keepers via `PUT .../expressions/{key}`).
- **Character cards:** `card.go` serves `POST /personality/import/sillytavern` (a SillyTavern `chara_card_v2`/`v3` card as a JSON or PNG body, 8 MB cap) and `GET /personality/{id}/export/sillytavern` (`?format=png` embeds the card in the personality's cover image).
  The conversion rules live in `internal/stcard`; the handler only validates, uniquifies the name (`Name (2)`), creates the personality with the passthrough blob, and makes it the default when it is the user's first.
  An over-long prompt sheds `scenario`/`personality` with a warning rather than failing; one that is still too long is a 400 with `system_prompt_too_long`.
  A PNG card's picture is not stored by the server: the client attaches it as the cover image.
  A character book too big to flatten comes back in the response as `lore_files` for the client to upload through the normal file-attachment route.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/agent`, `internal/models`, `internal/stcard`, `mux`, `zap`.

## Non-obvious decisions

- Personality files participate in **file search** and **vector store** tool wiring — coordinate with `internal/agent/tools_test.go` when changing attachment behavior.
- **Usage stats on single-personality responses:** `ListPersonalities` fills `stats` (thread count / last used) in the datastore, but `GetPersonality`/`UpdatePersonality` return zeroed `stats` (they sit on hot agent paths).
  `GET`/`PUT /personality/{id}` therefore call `attachUsageStats` (`Store.GetPersonalityUsageStats`) so the stand-alone page matches the list card (#129).
  A stats error is logged and the personality is still returned.
- **Deleting a personality releases its attachment objects.**
  `DeletePersonality` reads the personality's attachment rows before the cascade and calls `storage.ReleaseAttachmentObjects` after it (reference counted, best effort).

## Testing

- `card_test.go` — card import (blob kept out of the prompt and responses, name collisions, lore files, rejection mapping, first-personality default) and export (native, reconstructed from a blob, 404/400).
- `card_png_test.go` — PNG import, shed-with-warning and still-too-long cases, and PNG export (embedded card, non-PNG cover converted, no cover, omitted fields restored).
- `generate_test.go`, `create_default_test.go` — generation and defaults.
- `expression_candidates_test.go` — candidate request validation and enqueue error mapping (fake `PersonalityAgent`).
- `usage_stats_test.go` — `stats` present on GET/PUT single-personality responses.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — agent personalities.
