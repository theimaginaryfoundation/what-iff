# Package: `internal/agentjobs/scheduler`

## Role

**In-process scheduler** for agent jobs: loads due jobs, claims work with Postgres advisory locks (when distributed mode is on), executes agent runs, and updates job status.

## Responsibilities

- **`Manager`:** Lifecycle: start/stop, tick loop, leadership when `AgentJobsSchedulerDistributed` is enabled (`manager.go`).
- **`Job`:** Single job execution unit (`job.go`) — interacts with `internal/agent` and datastore.
- Coordination with **`internal/datastore/scheduler_lock.go`** for multi-instance safety.

## Dependencies

- **Inbound:** `internal/server` starts the manager when config enables it.
- **Outbound:** `internal/datastore`, `internal/agent`, `internal/models`, `zap`.

## Non-obvious decisions

- Only one instance should execute the scheduler loop in production clusters — server config documents lock key and retry tuning.
- **MVP note:** `Config.EnableAgentJobsScheduler` gates whether the scheduler runs at all.

## Testing

- `manager_test.go`, `job_test.go` — scheduling and execution behavior with fakes/mocks.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md); [`internal/server`](../../server/_PACKAGE_SUMMARY.md) for scheduler-related config flags.
