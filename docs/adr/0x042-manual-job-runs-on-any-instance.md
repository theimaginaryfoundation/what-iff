# ADR 0x042: Manual job runs on any instance, with the run limit checked up front

- **Status:** Accepted
- **Date:** 2026-11-03
- **Deciders:** What Iff maintainers
- **Issue:** [what-iff#427](https://github.com/theimaginaryfoundation/what-iff/issues/427)
- **PR:** [what-iff#437](https://github.com/theimaginaryfoundation/what-iff/pull/437)

## Context

"Run now" on a scheduled job sometimes failed with `AgentJob scheduler is not active` (503).
The report guessed the job run limiter was behind it.
It was not: the limiter ran only after the request had been answered 202, and a refused run showed up later as a chat message or a silent five-minute deferral.

The 503 came from `Manager.RunAgentJobNow`, which refused unless this process had a quartz scheduler (`m.sched != nil`).
In distributed mode (`AGENTJOBS_SCHEDULER_DISTRIBUTED=true`) only the instance holding the Postgres advisory lock has one.
The hosted deployment runs at least two API tasks behind a load balancer, so a manual run that landed on a follower failed, about half the time with two tasks.
The same happened on any instance for the few seconds after leadership was lost and before it was won again.

A manual run never needed quartz.
It runs on its own goroutine through the datastore and the agent, and the per-chat turn gate (issue #254) orders turns across instances through the database.
The only quartz use on that path was deferring a run that hit the limit.

## Decision

### 1. Manual runs work on any running instance

The manager has a `running` flag: set by a successful `Start`, or for the whole of `Run` (leader or follower), and cleared by `Stop`.
`RunAgentJobNow` requires only that flag.
`ErrSchedulerNotActive`, and its 503, now means the manager is genuinely not running on that instance: never started, its start failed, or it is shutting down.
A deployment with the scheduler disabled still answers 503 "AgentJob scheduler is not enabled".

### 2. The run limit is checked before a manual run is accepted

`RunAgentJobNow` applies the same per-user window as scheduled runs (10 per 30 minutes) before it starts the run.
Over the limit it returns a `*RunLimitError`, which the handler answers with 429 `job_run_limit_reached`.
`details` carries `limit`, `window_seconds` and, when known, `retry_at`; `Retry-After` is set with it.
The web app shows "Job run limit reached — try again at <local time>", and the server's message (UTC) when there is no reset time.

The reset time is when enough counted events have aged out of the window for the count to drop below the limit.
It needs the events' times, so the datastore gained an optional hook, `ListRecentAgentJobExecutionTimes`.
It is nil in the open-source build (where the limiter is inert anyway) until an implementation is linked.

### 3. Manual runs are never deferred

A manual run that loses the last slot to another run between the check and its execution is skipped with a chat message.
It is not deferred: deferring needs quartz, which a follower lacks, and would replay a run the user asked for now minutes later as a scheduled run, which skips a paused job.

## Alternatives considered

- **Keep manual runs leader-only and change only the message.** The failure would then be accurate but still happen on about half of all clicks.
- **Hand manual runs to the leader through the database** (a "run requested" column the leader's reconcile picks up). It keeps all execution on one instance, but needs a migration and adds up to the reconcile interval (30 s) of latency, for no ordering guarantee the turn gate does not already give.
- **503 for a rate-limited run with a better message.** 429 is what the repo uses for every other per-user limit (memory and account export, chat quota).

## Consequences

- A manual run on a follower is not covered by the leader's in-process overlap guard (`inFlight`), so it can run at the same time as the leader's scheduled firing of the same job.
  Both turns queue in the chat through the turn gate, and both record a run.
  Webhook-started runs already worked this way.
- The limit is read twice for a manual run (once to accept, once when it executes); the second read only matters in the race above.
- `ErrorCode` gains the specific code `job_run_limit_reached`.

## Not done yet

- **The limiter overcounts.** It counts the user's `job_run` usage events (the private overlay's `countRecentSuccessfulAgentJobExecutions`), which are billing rows, not runs.
  An Advanced-loop job run records one per step plus its final one, `run_subagent` records one from any turn (ordinary chat included), webhook-started runs record one, and failed runs that made model calls record one.
  So the limit can trip with visibly fewer than 10 job runs.
  Counting distinct `metadata.turn_id` and excluding `metadata.job_type = 'subagent'` would bring it close to runs; a dedicated run log would be exact.
- **The reset-time hook has no implementation yet.** The overlay needs a `listRecentAgentJobExecutionTimes` alongside its counter, with the same filters, ordered oldest first.
- The job detail page still replaces the job view with any Run now error (pre-existing); the jobs list now shows it above the cards.
