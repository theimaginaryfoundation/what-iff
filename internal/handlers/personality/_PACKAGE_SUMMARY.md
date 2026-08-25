# Package: `internal/handlers/personality`

## Role

HTTP API for **personalities** — CRUD, defaults, file attachments, and personality generation flows at `/api/personality/...`.

## Responsibilities

- **`Handler`:** Core routes in `handler.go`; file attachment helpers in `fileattachment.go`; provider binding in `provider.go`.
- Generation and default flows may invoke `internal/agent` (see `generate.go`, `create_default.go`).

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/agent`, `internal/models`, `mux`, `zap`.

## Non-obvious decisions

- Personality files participate in **file search** and **vector store** tool wiring — coordinate with `internal/agent/tools_test.go` when changing attachment behavior.

## Testing

- `generate_test.go`, `create_default_test.go` — generation and defaults.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — agent personalities.
