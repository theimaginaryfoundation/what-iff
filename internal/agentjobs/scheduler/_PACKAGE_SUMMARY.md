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
- **Run failure text:** `last_error` and the assistant message posted on a failed run use `providerkeys.FailureText`, so a missing provider key reads "No OpenAI API key is set." rather than the transport error.
  There is no separate status value or column; keep the marker and the helper in sync.
- **Manual runs (`run_now.go`, [ADR 0x041](../../../docs/adr/0x041-manual-job-runs-on-any-instance.md)):** `RunAgentJobNow` works on every instance while the manager is running (`running`, set by `Start` or for the whole of `Run`, leader or follower).
  It does not need the quartz scheduler, which only the leader has; the per-chat turn gate orders it against scheduled runs.
  `ErrSchedulerNotActive` means the manager is not running on this instance (not started, start failed, or stopped).
  The per-user window is checked before the run is accepted: over it, a `*RunLimitError` (`errors.Is` `ErrRunLimitReached`) carries the limit, the window and, when the datastore can list execution times, `RetryAt`.
  A manual run that still meets the limit when it executes is skipped with a chat message, never deferred.
- **Execution window:** 10 runs per user per sliding 30 minutes, counted by `CountRecentSuccessfulAgentJobExecutions` (the user's `job_run` usage events; zero in the open-source build).
  Those events are billing records, not runs, so the count can exceed the user's agent-job runs (Advanced steps, `run_subagent`, webhook runs; issue #427).
- **MVP note:** `Config.EnableAgentJobsScheduler` gates whether the scheduler runs at all.

## Testing

- `manager_test.go`, `job_test.go` — scheduling and execution behavior with fakes/mocks.
- `run_now_test.go` — manual runs: accepted on a follower, refused with `ErrSchedulerNotActive` only when not running, a rate-limited run is a `*RunLimitError` (never `ErrSchedulerNotActive`), reset-time arithmetic.
- `metrics_test.go` — run outcomes (overlap, inactive, load failure, chat resolve failure, congestion skip and defer) and lateness rules, via `telemetrytest.UseGlobal` (not parallel).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); [`internal/server`](../../server/_PACKAGE_SUMMARY.md) for scheduler-related config flags.
