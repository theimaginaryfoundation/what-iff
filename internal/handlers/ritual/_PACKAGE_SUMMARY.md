# Package: `internal/handlers/ritual`

## Role

HTTP API for **rituals** (reusable prompt/action templates) and **bindings** — list/create/update/delete at `/api/ritual/...`.

## Responsibilities

- **`Handler`:** Ritual CRUD plus hotkey/binding endpoints (`handler.go`, `binding.go`, `hotkey.go`).

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/models`, `mux`.

## Non-obvious decisions

- System rituals vs DB-backed rituals are split in `internal/agent/system_rituals.go` — HTTP layer should not duplicate UUID rules.
- **`mcp_server_ids` on update:** `models.Ritual.MCPServerIDs` is a **pointer slice** — omit the JSON field to leave MCP edges unchanged; send an array (including `[]`) to replace the set (`internal/datastore/ritual.go` `UpdateRitual`). IDs must belong to the authenticated user; invalid sets return **400** (`ErrInvalidRequestBody` from datastore).

## Testing

- `hotkey_test.go`, `binding_test.go` — binding behavior.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — rituals in product overview.
