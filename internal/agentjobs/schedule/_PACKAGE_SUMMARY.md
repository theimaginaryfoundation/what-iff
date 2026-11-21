# Package: `internal/agentjobs/schedule`

## Role

**Schedule parsing and normalization** for agent jobs: translate user-facing schedule strings into concrete run times and quartz/cron-like structures used by the scheduler.

## Responsibilities

- Parse schedule input, validate timezone and recurrence, compute `nextRunAt` (see `parse.go` and related).

## Dependencies

- **Inbound:** `internal/handlers/agentjob`, `internal/datastore` when updating jobs.
- **Outbound:** `github.com/reugn/go-quartz` or related (see imports), `internal/models` schedule types.

## Non-obvious decisions

- **Schedule interpretation is a utility call:** `Parse` sends a `provider.UtilityRequest` on the simple role through a `provider.UtilityCaller` (the agent), which picks the model and its API.

## Testing

- `parse_test.go` — cron minimum-interval validation.
- `parse_request_test.go` — `Parse` asks the simple role for the `AgentJobSchedule` structured output (recording `UtilityCaller`).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — background jobs; [`internal/agentjobs/scheduler`](scheduler/_PACKAGE_SUMMARY.md).
