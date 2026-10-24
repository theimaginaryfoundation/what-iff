# Package: `internal/handlers/model`

## Role

HTTP API for **LLM model configuration** — which models users/chats can select, defaults, and provider metadata at `/api/model/...`.

## Responsibilities

- **`Handler`:** List/get/update model records per product rules (`handler.go`).

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/models`, `mux`.

## Non-obvious decisions

- Model defaults and allowed IDs interact with `internal/models/model_defaults.go` — update both when adding models.
- `ListModels` omits `subscription_tier` unless a meter is linked (`metering.Linked`): without one, tiers have no effect, and the app shows no Tier tab or badges.

## Testing

- `handler_test.go` — the tier is omitted without a linked meter and sent with one (SQLite, models table only).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — model entities.
