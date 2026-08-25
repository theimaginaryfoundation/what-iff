# Package: `internal/handlers/agentjob`

## Role

HTTP API for **agent jobs** — async/scheduled assistant work: CRUD, prompt/title/schedule updates, status, and linking jobs to chats.

## Responsibilities

- **`Handler`:** Routes under `/api/agent-job/...` (see `handler.go`): list, get, create, update (including schedule parsing via `parse_schedule.go`), delete, status updates.
- Uses **`internal/providers.AgentJobProvider`** pattern where interfaces isolate datastore.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore` / provider interfaces, `internal/models`, `internal/agentjobs/schedule`, `mux`, `zap`.

## Non-obvious decisions

- Schedule strings and timezones interact with `internal/agentjobs/schedule` — keep error messages aligned with UI.

## Testing

- `update_test.go` — update flows, including partial `personality_id` / `model_id` patches and **400** on invalid overrides.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); [`internal/agentjobs/scheduler`](../../agentjobs/scheduler/_PACKAGE_SUMMARY.md).
