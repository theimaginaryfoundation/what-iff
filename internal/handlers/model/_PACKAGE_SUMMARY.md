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

## Testing

- *(Handler-level tests if validation grows.)*

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — model entities.
