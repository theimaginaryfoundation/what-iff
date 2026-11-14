# Package: `internal/handlers/agentjob`

## Role

HTTP API for **agent jobs** — async/scheduled assistant work: CRUD, prompt/title/schedule updates, status, and linking jobs to chats.

## Responsibilities

- **`Handler`:** Routes under `/api/agent-job/...` (see `handler.go`): list, get, create, update (including schedule parsing via `parse_schedule.go`), delete, status updates.
  Schedule parsing that can't run for want of a key or of a model for the simple role answers 409 with the cause's code (`provider_not_configured`, `utility_model_not_set`, `utility_role_off`, `utility_model_unavailable`), not 400.
- Uses **`internal/providers.AgentJobProvider`** pattern where interfaces isolate datastore.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore` / provider interfaces, `internal/models`, `internal/agentjobs/schedule`, `mux`, `zap`.

## Non-obvious decisions

- Schedule strings and timezones interact with `internal/agentjobs/schedule` — keep error messages aligned with UI.
- **Job parameters (ADR 0x044):** create and update carry `parameters` and `parameter_values`.
  Update saves the prompt, definitions and values in one `UpdateAgentJobParameters` call, since each is only valid against the others; the datastore checks them and sets the status.
  `params.go` maps the errors: a `*params.ValidationError` answers 400 `job_parameters_invalid` with it as details, and a missing value answers 409 `job_needs_values` with `missing_parameters`.
  Resuming (`PUT /status` active) and `POST /run` are refused while a parameter has no value, whatever the status.

## Testing

- `create_test.go` — parameters reach the datastore; an undefined placeholder is a 400 with details.
- `run_test.go` — run now refuses a job missing parameter values (409).
- `update_test.go` — update flows, including partial `personality_id` / `model_id` patches and **400** on invalid overrides.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); [`internal/agentjobs/scheduler`](../../agentjobs/scheduler/_PACKAGE_SUMMARY.md).
