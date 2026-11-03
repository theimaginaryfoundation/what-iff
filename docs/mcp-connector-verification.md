# Verify an MCP connector

These tests exercise What Iff's in-process MCP client against a stateful connector.
Normal `make test` / `make test-ci` runs use local HTTP fixtures and require no live endpoint or credentials.
The same scenario runner is available behind the `mcp_live` build tag for an explicitly selected connector.

## Inventory first

Set `MCP_SMOKE_URL` to the HTTP MCP endpoint configured for the connector.
If authentication is needed, load its complete existing `Authorization` header into `MCP_SMOKE_AUTH_HEADER` through your local secret mechanism.
Neither value belongs in the plan or a committed file.

```sh
make test-mcp-inventory
```

This makes `initialize` and `tools/list` requests and prints advertised original tool names and their complete input schemas.
It makes no `tools/call` requests.
Review the manifest before sharing it: server-provided schemas and defaults may contain deployment-specific information.
The current client's discovery behavior is used, including its existing transport, session handling, and pagination limits.

## Configure a disposable-document test

Save a plan outside the checkout or in the ignored root file `mcp-smoke.local.json`.
Use the actual advertised tool names, argument schemas, and result paths from your connector.
The Foundry journal example below requires a target-world identity assertion; replace the placeholder with your selected test world's exact ID before enabling writes.
Version assertions can be added to `identity.expect` when validating a pinned deployment.

```json
{
  "identity": {
    "tool": "foundry_get_world_info",
    "arguments": {},
    "expect": {"id": "REPLACE_WITH_TEST_WORLD_ID"}
  },
  "create": {
    "tool": "foundry_create_journal",
    "arguments": {
      "name": "WhatIff smoke {{run}}",
      "pageTitle": "Connector Smoke Test",
      "pageType": "text",
      "content": "<p>{{marker}}</p>"
    }
  },
  "read": {
    "tool": "foundry_get_journal",
    "arguments": {"id": "{{id}}"},
    "expect": {
      "id": "{{id}}",
      "name": "WhatIff smoke {{run}}",
      "pages.0.content": "<p>{{marker}}</p>"
    }
  },
  "update": {
    "tool": "foundry_update_journal_page",
    "arguments": {
      "journalId": "{{id}}",
      "pageName": "Connector Smoke Test",
      "content": "<p>{{marker}}</p>"
    }
  },
  "reject": {
    "tool": "foundry_update_journal_page",
    "arguments": {
      "journalId": "{{id}}",
      "pageId": "missing-{{run}}",
      "content": "<p>Rejected test change</p>"
    }
  },
  "cleanup": {
    "tool": "foundry_delete_journal",
    "arguments": {"id": "{{id}}"}
  },
  "id_path": "id",
  "ownership_path": "name",
  "ownership_value": "WhatIff smoke {{run}}",
  "missing_error": "Journal not found"
}
```

`expect` keys and `id_path` / `ownership_path` use GJSON paths, for example `pages.0.content`.
Expected values are JSON literals compared by value, preserving distinctions such as false versus a missing field.
Assertions use `structuredContent` when supplied, or JSON in the text output for legacy connectors.
Write acknowledgements may be plain text when no assertions are configured for them; independent reads still prove the resulting state.

The runner generates a fresh `{{run}}` UUID and `{{marker}}` for each execution.
It captures `{{id}}` only from the create result and JSON-escapes all variable replacements.
The marker changes before the update, so a stale read cannot satisfy the updated-state assertion.
The ownership value must include `{{run}}`, and read, update, and cleanup arguments must reference `{{id}}`.
All configured tools must be advertised before the first tool call, and the identity assertions must pass before creation.

```sh
export MCP_SMOKE_PLAN=mcp-smoke.local.json
export MCP_SMOKE_ALLOW_WRITE=1
make test-mcp-live
```

On Windows without Make, the equivalent explicitly opted-in test is:

```powershell
$env:MCP_SMOKE_PLAN = 'mcp-smoke.local.json'
$env:MCP_SMOKE_ALLOW_WRITE = '1'
go test -tags mcp_live ./internal/agent/mcpclient -run '^TestLiveConnectorRoundTrip$' -count=1 -v
```

Set the endpoint and authorization environment variables separately through your existing local configuration.
Without the build tag, live tests are excluded even when the environment contains those variables.
With the tag but no write opt-in, the round-trip test skips; the Make target instead fails when required configuration is missing.

## Receipts and failure handling

The lifecycle is identity → create → independent read → update → independent read → rejected request → unchanged-state read → ownership read → delete → missing-document read.
The rejected request must produce an MCP tool-level `isError: true`; an HTTP or JSON-RPC error cannot masquerade as a passing negative test.
Its optional `expect` assertions can also constrain a structured error code or detail.
Deletion must be followed by `isError: true` and the configured missing-document text, so an unrelated permission failure does not count as verified deletion.

Each local receipt records a run ID, created document ID, phase, original tool name, SHA-256 hashes of submitted arguments and normalized output, and error status.
It omits endpoint, authorization headers, arguments, result bodies, and raw transport error messages.
Receipts establish ordering and correlate a run with its configured inputs; hashes alone do not diagnose an arbitrary server-side failure.
Keep the local plan and inspect redacted connector logs when more detail is needed.
Plan diagnostics identify the invalid field or JSON byte offset, and discovery/request diagnostics report safe categories such as HTTP 401, timeouts or JSON-RPC failures.
Their wrapped causes remain available to local `errors.Is` / `errors.As` inspection, while ordinary error rendering excludes raw paths, endpoints, response bodies and malformed values.

Cleanup runs on a separate 30-second context even if the main test fails or is cancelled.
It first reads the captured ID and verifies the exact unique ownership marker, then deletes that ID and verifies absence.
The content marker changes before update assertions; cleanup instead uses the immutable run UUID and captured document ID, so stale or failed updates do not weaken ownership verification.
Failed ownership checks stop deletion and leave cleanup marked failed.
If a create response provides no usable ID, completion is uncertain: the test records `unknown_created_id` and never retries or deletes by a guessed name.
Reconcile that run's unique marker before starting another write test.

## What passing establishes

The fixture suite verifies HTTP JSON, SSE replies with preceding notifications, MCP sessions, structured results, legacy JSON text, stale writes, explicit errors, rejected mutations, target checks, cleanup, and cancellation.
A live pass establishes that this checkout's MCP client can perform the configured document lifecycle through the selected endpoint.
It does not establish that the hosted service deployed this checkout, that a webhook job has the correct connector scope, or that a personality can complete the same workflow.
Validate that deployed webhook/personality path separately.

This test reads immediately after each write and never executes a macro.
For a journal-driven macro runner, separately compare the request immediately before execution and account for any intentional request restoration after execution.
Passing CRUD tests alone cannot establish that macro execution used the intended request.
