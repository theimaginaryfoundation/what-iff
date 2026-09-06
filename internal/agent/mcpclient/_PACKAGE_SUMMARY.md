# Package: `internal/agent/mcpclient`

## Role

In-process MCP runtime client for connector tool discovery and invocation.

## Responsibilities

- Discover tools from user-owned MCP connectors over JSON-RPC/HTTP.
- Build stable internal tool names (`mcp__<connector>__<tool>`).
- Cache per-connector discovery manifests with TTL.
- Execute MCP `tools/call` requests and normalize output for agent tool results.
- Keep connector-level failures isolated so one unhealthy connector does not break others.

## Key types and entry points

- `Client`: runtime client with cache and HTTP transport.
- `DiscoverTools`: resolves tool manifests for eligible connectors.
- `CallToolByFullName`: resolves connector/tool target and performs MCP invocation.

## Dependencies

- **Inbound:** `internal/agent` (`mcp_tools.go`, tool dispatch path).
- **Outbound:** standard `net/http`, `encoding/json`, `internal/models`.

## Non-obvious decisions

- Discovery and execution use JSON-RPC method calls (`initialize`, `tools/list`, `tools/call`) with best-effort `notifications/initialized`.
- Tool invocation resolves sanitized display names back to original MCP tool names via cached discovery records.
- Eligibility filtering is state-driven (`active` and `refresh_failed` allowed by default).

## Testing

- `client_test.go` covers discovery, full-name parsing, and tool invocation using an HTTP test server.

## Related

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md)
