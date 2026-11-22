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
- **`PUT/PATCH /mcp-servers/{id}`** accepts optional **`ritual_ids`**: omitted leaves ritual↔MCP edges unchanged; a JSON array (including `[]`) replaces the full set.
  Invalid or foreign ritual IDs return **400**.
- **`server_url`, `oauth_auth_url` and `oauth_token_url` must be public https addresses** (`validateMCPURL`), checked on create, on update when they change, and (`server_url`) on test-connection.
  With `MCP_ALLOW_LOCALHOST_CONNECTIONS`, loopback is also allowed, over http too; private ranges stay refused.
  The IP rule is `netguard.IsPublicIP`, the same predicate the client enforces at connect time, so a URL that saves is one the client will dial.
  This is the early, friendly error; the enforcement is the connect-time guard, which also covers DNS rebinding and connectors saved before the check existed (discovery marks those `invalid`).
- Runtime health fields (`status`, `status_reason`, `last_checked_at`, `last_healthy_at`, `tool_count`) are read-only from this handler and updated by runtime discovery/execution paths in `internal/agent`.

## Testing

- `handler_test.go` — connection testing, OAuth start and callback, and URL validation on create and update (`TestSavingMCPServerValidatesURLs`: private, CGNAT, NAT64, resolved hostnames, loopback with and without the flag, OAuth endpoints, and renaming a legacy connector).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); agent `mcp_tools.go` for tool assembly.
