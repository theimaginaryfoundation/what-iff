# Package: `internal/agentjobs/scheduler`

## Role

**In-process scheduler** for agent jobs: loads due jobs, claims work with Postgres advisory locks (when distributed mode is on), executes agent runs, and updates job status.

## Responsibilities

- **`Manager`:** Lifecycle: start/stop, tick loop, leadership when `AgentJobsSchedulerDistributed` is enabled (`manager.go`).
- **`Job`:** Single job execution unit (`job.go`) — interacts with `internal/agent` and datastore.
- Coordination with **`internal/datastore/scheduler_lock.go`** for multi-instance safety.

## Dependencies

- **Inbound:** `internal/server` starts the manager when config enables it.
- **Outbound:** `internal/datastore`, `internal/agent`, `internal/models`, `internal/telemetry`, `zap`.

## Non-obvious decisions

- Only one instance should execute the scheduler loop in production clusters — server config documents lock key and retry tuning.
- **Metrics** (`metrics.go`): every firing counts once on `whatiff.scheduler.runs` by outcome (`ran`, `failed`, `skipped_overlap`, `skipped_inactive`, `load_failed`, `chat_resolve_failed`, `skipped_congestion`, `deferred`, `misfire`).
  Runs that reach the agent also record `whatiff.scheduler.run.duration` (outcome `ran` or `failed`).
  `whatiff.scheduler.lateness` is start time minus the job's stored `next_run_at`, for regular firings only (manual runs and deferred retries have no planned time).
  The manager isn't handed a `*telemetry.Metrics`, so it records through `telemetry.Global()`.
  Scheduled runs create no `jobs` row, so they don't appear in the job-lifecycle metrics.
- **Schedule failure on a recurring job:** if the next run can't be computed, the job is paused and `last_error` is prefixed with `ScheduleErrorMarker` (`job.go`).
  A user pause clears `last_error`, so paused + marker is how the web app (`scheduleFailureReason` in `job-status.helpers.ts`) shows "Paused: schedule error" with the reason instead of a plain pause.
  There is no separate status value or column; keep the marker and the helper in sync.
- **Jobs that follow the user (`timezone.go`):** a cron job runs in `AgentJob.ScheduleTimezone()`.
  For a job with `follow_user_timezone`, that is the owner's browser-reported `current_timezone` (the datastore derives it as `EffectiveTimezone`); otherwise it is the job's own `timezone`.
  The reconcile fingerprint uses the effective timezone, so a change of the owner's timezone reschedules only the jobs that follow them.
  The manager then stores their new `next_run_at`, except on the first schedule after startup.
  Each run records the timezone it ran in (`last_run_timezone`).
  When a job's timezone differs from the one it last ran in, `slotGuardTrigger` skips fire times at or before the last run's wall-clock time in the new zone, so a slot that already ran is not run again.
  For example, if 8am New York has run and the user moves to Los Angeles, 8am Los Angeles that same day is skipped.
  Moving east can skip a slot that falls between the two zones' times; this is accepted.
- **MVP note:** `Config.EnableAgentJobsScheduler` gates whether the scheduler runs at all.

## Testing

- `manager_test.go`, `job_test.go` — scheduling and execution behavior with fakes/mocks.
- `timezone_test.go` — following the user's timezone, the no-rerun guard (west, east, hourly), the fingerprint, and storing the next run on reconcile.
- `metrics_test.go` — run outcomes (overlap, inactive, load failure, chat resolve failure, congestion skip and defer) and lateness rules, via `telemetrytest.UseGlobal` (not parallel).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); [`internal/server`](../../server/_PACKAGE_SUMMARY.md) for scheduler-related config flags.
