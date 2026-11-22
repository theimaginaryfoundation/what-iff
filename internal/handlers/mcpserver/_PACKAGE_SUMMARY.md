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
- Runtime health fields (`status`, `status_reason`, `last_checked_at`, `last_healthy_at`, `tool_count`) are read-only from this handler and updated by runtime discovery/execution paths in `internal/agent`.
- **OAuth set up from the URL (issue #517, `oauth_discovery.go`):** `POST /mcp-servers/oauth/discover` previews what an MCP server says about signing in; create/update with **`oauth_auto: true`** discovers it again and gets a client (`mcpoauth.AutoConfigure`), ignoring the endpoint, scope and PKCE fields. A discovered connector whose URL changes is discovered again; one whose endpoints or client ID are edited by hand becomes manual (`clearDiscovered`). Failures answer **422** with `mcp_oauth_not_required`, `mcp_oauth_client_id_required` or `mcp_oauth_discovery_failed`.
- The **callback** passes `iss` on to `mcpoauth` and, on success, lists the connector's tools (`listToolsAfterSignIn`), so its tool count and status are current without a manual test. `test-connection` tests a signed-in OAuth connector with its token.
- **`GET /mcp-servers/oauth/client-metadata.json`** is public (beside the callback): the Client ID Metadata Document authorization servers fetch.

## Testing

- `handler_test.go`; `oauth_discovery_test.go` for discovery, auto setup, the callback's tool listing and the metadata document.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); agent `mcp_tools.go` for tool assembly.
