# Package: `internal/agentjobs/schedule`

## Role

**Schedule parsing and normalization** for agent jobs: translate user-facing schedule strings into concrete run times and quartz/cron-like structures used by the scheduler.

## Responsibilities

- Parse schedule input, validate timezone and recurrence, compute `nextRunAt` (see `parse.go` and related).
- **How a recurring schedule reads in the timezone it runs in now (`timezone.go`, [ADR 0x027](../../../docs/adr/0x027-jobs-follow-user-timezone.md)):** `CronTrigger`, `NextRunAt`, `LostSlotOnMove` and `StoreNextRun`.
  They are pure and shared by the scheduler, the job handlers and the agent's job tools, so all of them compute the same next run.
  For a job that follows the user and has moved since it last ran, the trigger skips slots that already ran, and fails instead of returning one if it cannot get past them.
  `StoreNextRun` leaves an unreadable schedule to the scheduler (which pauses the job and says why); only a failed write is an error.

## Dependencies

- **Inbound:** `internal/handlers/agentjob`, `internal/agent` (job tools), `internal/agentjobs/scheduler`.
- **Outbound:** `github.com/reugn/go-quartz` or related (see imports), `internal/models` schedule types.

## Non-obvious decisions

- **Manual `ResponseNewParams`** for one-off LLM schedule interpretation is called out in the architecture doc as an acceptable pattern — if this package invokes the model, keep that doc link accurate.

## Testing

- `parse_test.go` — cron minimum-interval validation.
- `parse_no_temperature_test.go` — `Parse` sends the utility model with no temperature (recording `openAICaller`).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — background jobs; [`internal/agentjobs/scheduler`](scheduler/_PACKAGE_SUMMARY.md).
