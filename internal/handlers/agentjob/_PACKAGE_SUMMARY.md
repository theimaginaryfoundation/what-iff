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
- **Run now** (`run.go`): 202 when accepted; 429 `job_run_limit_reached` when the scheduler's `*RunLimitError` says the user is over the job run window (`details`: `limit`, `window_seconds`, `retry_at` when known, plus `Retry-After`); 503 "AgentJob scheduler is not enabled" without a runner, and "AgentJob scheduler is not active" only for `ErrSchedulerNotActive` ([ADR 0x041](../../../docs/adr/0x041-manual-job-runs-on-any-instance.md)).

## Testing

- `update_test.go` — update flows, including partial `personality_id` / `model_id` patches and **400** on invalid overrides.
- `run_test.go` — run now: status gates, 503 for a missing or inactive scheduler, 429 with details for a rate-limited run.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); [`internal/agentjobs/scheduler`](../../agentjobs/scheduler/_PACKAGE_SUMMARY.md).
