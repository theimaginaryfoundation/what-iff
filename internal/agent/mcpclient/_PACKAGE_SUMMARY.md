# Package: `internal/agent/mcpclient`

## Role

In-process MCP runtime client for connector tool discovery and invocation.

## Responsibilities

- Discover tools from user-owned MCP connectors over JSON-RPC/HTTP.
- Build stable internal tool names (`mcp__<connector>__<tool>`).
- Cache per-connector discovery manifests with TTL.
- Execute MCP `tools/call` requests and normalize output for agent tool results.
- Preserve `isError` and `structuredContent` alongside text and generated attachments (the client keeps what the server sent).
- Keep connector-level failures isolated so one unhealthy connector does not break others.

## Key types and entry points

- `Client`: runtime client with cache and HTTP transport.
- `DiscoverTools`: resolves tool manifests for eligible connectors.
- `CallToolByFullName`: resolves connector/tool target and performs MCP invocation.
- `CallToolResult`: detailed result with tool status, structured data, model-visible output, attachments, and `Err()` for tool-level failures.
- `ToolReportedError`: the typed error `Err()` returns for a failure the server reported (`isError`). Callers distinguish it with `errors.As` from transport and protocol errors (the agent logs it at Warn, without its text).

## Dependencies

- **Inbound:** `internal/agent` (`mcp_tools.go`, tool dispatch path).
- **Outbound:** standard `net/http`, `encoding/json`, `internal/models`.

## Non-obvious decisions

- Discovery and execution use JSON-RPC method calls (`initialize`, `tools/list`, `tools/call`) with best-effort `notifications/initialized`.
- Tool invocation resolves sanitized display names back to original MCP tool names via cached discovery records.
- Eligibility filtering is state-driven (`active` and `refresh_failed` allowed by default).
- Detailed calls return tool-level errors in `CallToolResult.IsError`; their Go error value is reserved for transport or protocol failures.
  String-returning calls and agent dispatch convert tool-level failures through `CallToolResult.Err()`.
- Model-visible output is the result's text. Labeled structured JSON is used only when there is no text: the spec has servers repeat structured data as text, so appending it doubles tokens (2x to 9x measured on real replies). It is always kept on `CallToolResult.StructuredContent`.

## Testing

- `client_test.go` covers discovery, full-name parsing, and tool invocation using an HTTP test server.
- `tool_result_test.go` covers structured output (text wins, structured fallback), tool-level errors, raw fallback, and detailed versus string-returning calls.

## Related

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md)
