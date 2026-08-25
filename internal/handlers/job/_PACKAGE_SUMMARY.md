# Package: `internal/handlers/job`

## Role

HTTP API for **background jobs** (distinct from **agent jobs**): list/get generic `Job` entities at `/api/job/...` used for long-running work tracking in the product.

## Responsibilities

- **`Handler`:** Routes under `/api/job/...` — list and get by id (`list.go`, `get.go`, `handler.go`).

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/models`, `mux`.

## Non-obvious decisions

- Naming: do not confuse with `internal/handlers/agentjob` — different Ent entity and purpose.

## Testing

- `list_test.go`, `handler_test.go`, `get_test.go` — pagination and retrieval.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — job entities in data layer.
