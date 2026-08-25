# Package: `internal/agentjobs/schedule`

## Role

**Schedule parsing and normalization** for agent jobs: translate user-facing schedule strings into concrete run times and quartz/cron-like structures used by the scheduler.

## Responsibilities

- Parse schedule input, validate timezone and recurrence, compute `nextRunAt` (see `parse.go` and related).

## Dependencies

- **Inbound:** `internal/handlers/agentjob`, `internal/datastore` when updating jobs.
- **Outbound:** `github.com/reugn/go-quartz` or related (see imports), `internal/models` schedule types.

## Non-obvious decisions

- **Manual `ResponseNewParams`** for one-off LLM schedule interpretation is called out in the architecture doc as an acceptable pattern — if this package invokes the model, keep that doc link accurate.

## Testing

- *(No `*_test.go` in this directory yet — add when schedule parsing gains unit-testable pure functions.)*

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — background jobs; [`internal/agentjobs/scheduler`](scheduler/_PACKAGE_SUMMARY.md).
