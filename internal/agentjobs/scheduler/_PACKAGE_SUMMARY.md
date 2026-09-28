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
- **MVP note:** `Config.EnableAgentJobsScheduler` gates whether the scheduler runs at all.

## Testing

- `manager_test.go`, `job_test.go` — scheduling and execution behavior with fakes/mocks.
- `metrics_test.go` — run outcomes (overlap, inactive, load failure, chat resolve failure, congestion skip and defer) and lateness rules, via `telemetrytest.UseGlobal` (not parallel).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); [`internal/server`](../../server/_PACKAGE_SUMMARY.md) for scheduler-related config flags.
