# Package: `internal/agent/mcpclient`

## Role

In-process MCP runtime client for connector tool discovery and invocation.

## Responsibilities

- Discover tools from user-owned MCP connectors over JSON-RPC/HTTP.
- Build stable internal tool names (`mcp__<connector>__<tool>`).
- Cache per-connector discovery manifests with TTL.
- Execute MCP `tools/call` requests and normalize output for agent tool results.
- Preserve `isError` and `structuredContent` alongside text and generated attachments.
- Keep connector-level failures isolated so one unhealthy connector does not break others.

## Key types and entry points

- `Client`: runtime client with cache and HTTP transport.
- `DiscoverTools`: resolves tool manifests for eligible connectors.
- `CallToolByFullName`: resolves connector/tool target and performs MCP invocation.
- `CallToolResult`: detailed result with tool status, structured data, model-visible output, attachments, and `Err()` for tool-level failures.

## Dependencies

- **Inbound:** `internal/agent` (`mcp_tools.go`, tool dispatch path).
- **Outbound:** standard `net/http`, `encoding/json`, `internal/models`.

## Non-obvious decisions

- Discovery and execution use JSON-RPC method calls (`initialize`, `tools/list`, `tools/call`) with best-effort `notifications/initialized`.
- Tool invocation resolves sanitized display names back to original MCP tool names via cached discovery records.
- Eligibility filtering is state-driven (`active` and `refresh_failed` allowed by default).
- Detailed calls return tool-level errors in `CallToolResult.IsError`; their Go error value is reserved for transport or protocol failures.
  String-returning calls and agent dispatch convert tool-level failures through `CallToolResult.Err()`.
  Non-empty output can accompany a non-nil error from either string-returning method, so callers must always check the error.
- Model-visible output appends labeled structured JSON after text so neither representation is discarded.
  JSON `null` is treated as absent structured content; error rendering trims whitespace without changing the complete stored output.

## Testing

- `client_test.go` covers discovery, full-name parsing, and tool invocation using an HTTP test server.
- `tool_result_test.go` covers structured output, tool-level errors, raw fallback, and detailed versus string-returning calls.
- `connector_roundtrip_test.go` exercises discovery, session headers, create/read/update/read, rejection, and verified cleanup over JSON and SSE, including legacy JSON-in-text results.
  Fault cases detect stale writes, unflagged errors, mutations after rejection, wrong targets, missing capabilities, ownership changes, and failed cleanup.
- `connector_scenario_test.go` supplies the shared scenario runner; `connector_plan_test.go` loads plans and validates each field before connecting.
  `connector_scenario_validation_test.go` verifies preflight checks, JSON-safe variables, and the whole-report privacy contract.
- `connector_diagnostics_test.go` retains underlying errors while rendering safe categories, status codes and JSON offsets.
  `connector_diagnostics_validation_test.go` verifies useful diagnostics without exposing file paths, malformed values, endpoints or response bodies.
- `connector_live_test.go` is excluded unless built with `mcp_live` and additionally requires explicit environment configuration for writes.
  See [connector verification](../../../docs/mcp-connector-verification.md) for Make targets, plan format, local receipts, and scope.

## Related

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md)
