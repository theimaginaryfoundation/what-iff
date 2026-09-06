# Package: `internal/handlers/mcpserver`

## Role

HTTP API for the MCP connector catalog — list/register user-owned connectors used by the first-party in-process MCP runtime.

## Responsibilities

- **`Handler`:** Registers `/mcp-servers` and backward-compatible **`/mcp-server`** (same routes on both). Handlers delegate to the `Provider` (datastore).

## Dependencies

- **Inbound:** `internal/server` (mount path may vary — check `server.go`).
- **Outbound:** `internal/datastore`, `internal/models`, `mux`.

## Non-obvious decisions

- Chat-level MCP association is under **`handlers/chat`**; this package is the global MCP server resource API.
- **`PUT/PATCH /mcp-servers/{id}`** accepts optional **`ritual_ids`**: omitted leaves ritual↔MCP edges unchanged; a JSON array (including `[]`) replaces the full set. Invalid or foreign ritual IDs return **400**.
- Runtime health fields (`status`, `status_reason`, `last_checked_at`, `last_healthy_at`, `tool_count`) are read-only from this handler and updated by runtime discovery/execution paths in `internal/agent`.

## Testing

- *(Add tests as handlers grow.)*

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); agent `mcp_tools.go` for tool assembly.
