# ADR 0x039: Live scheduled-job turns in the open thread

- **Status:** Accepted
- **Date:** 2026-11-02
- **Deciders:** What Iff maintainers
- **Issue:** none (reported in conversation)
- **PR:** see the pull request that adds this file

## Context

A scheduled agent job runs a turn inside an existing thread (`internal/agentjobs/scheduler` → `Agent.HandleAgentJobPrompt`).
While it ran, a user with that thread open saw nothing of it.
The job's reply appeared only once the run had finished and something reloaded the thread.
If the user sent a message meanwhile, it queued behind the run in the per-chat turn gate (#254).
It showed "Waiting for the previous reply…" for a reply the user could not see, and after ten minutes it failed with "timed out waiting for an earlier turn in this chat to finish".

A user's own turn already streams.
Its `chat_message` job carries the draft text, live reasoning and tool timeline, and the web client polls the job (`AssistantTurn`).
A scheduled run has no job row of its own.
It holds its place in the gate with a queue ticket, an `agent_job_run` job keyed by the chat id (`beginEphemeralChatTurn`).
Four things kept that ticket from working like a user turn's job:

- **Nothing streamed onto it.** A Standard turn wrote its draft and tool timeline to the tracking job, which a scheduled run does not have, so it wrote them nowhere. Only an Advanced run's saved draft used the ticket.
- **It never pointed at the reply.** The ticket went from `processing` straight to `complete` with no `result_id`, so a client following it would report "Job completed but no result was provided".
- **Nothing could find it.** `GET /chat/{chatId}/active-job` looked only at `chat_message` jobs.
- **Stop did not reach it.** The thread's Stop marks every live turn job in the thread cancelled, but nothing in the run watched its ticket, so the run went on and later wrote `complete` over the cancellation.

The web client also has no push channel.
It picks a running turn up on thread entry and on tab-return syncs only, so a run that starts while the user is looking at the thread is never noticed.

## Decision

### 1. A scheduled run streams on its queue ticket

`handleEphemeralPrompt` hands the generation the ticket when there is no tracking job.
The draft text, live reasoning and tool timeline then land on the ticket exactly as they land on a user turn's job.
Once the reply is saved, the ticket goes through `inference_complete` with the reply as its `result_id` (`advanceJobInferenceComplete`, which also clears the draft) and then `complete`.
A client following the ticket therefore fetches the reply the same way it does for a user turn.
Webhook background runs already had a tracking job and are unchanged.

### 2. One lookup for "the turn to follow in this thread"

`GET /chat/{chatId}/active-job` now considers every turn job (`chat_message` and `agent_job_run`) and says which kind it found (`job_type`).
It no longer returns the newest job.
It returns the oldest turn still before its reply (`pending` or `processing`), which is the one generating now, since the gate runs a chat's turns in job order.
It finds that one over the same scan as the turn gate (`ListPendingTurnJobsForChat`: oldest first, the same limit), so the two agree on which turn is running.
When every live turn has replied, it returns the newest one still in its post-inference phases, as before, from a second scan ordered newest first.
That keeps post-inference jobs a dead worker left behind from crowding the thread's own turn out of the scan.
A run keyed by the chat answers no saved message, so `message_id` is absent for it.

This is the existing endpoint widened rather than a second one.
The client already asks it on every thread entry and sync, and needs one answer about what is running.

### 3. The client watches an open thread

While a thread is open, the tab visible, the thread loaded and no reply of this tab's landing, the client asks the endpoint every 5 seconds (`CHAT_ACTIVE_TURN_WATCH_INTERVAL_MS`).
A tick that finds a turn starts following it, as a thread entry would.
This also catches turns started from another tab, a webhook or a relay, which had the same blind spot.

### 4. Shown like a reply, marked as a job

A run followed while it is still generating gets the pending-reply bubble a user turn gets, with its streaming text, reasoning and live tool calls, plus a "Job running" label.
The run answers no user message, so the bubble cannot use the "no assistant reply after the last user message" rule.
Instead the client remembers the newest message when it picked the run up.
The bubble shows until an assistant message arrives after that one, and only a message after it counts as the run's reply, so an earlier reply never ends the run's draft or animates again.
A run picked up only after it replied is polled as a plain turn, with no bubble.

### 5. The composer explains, and Stop stops the run

While a run generates, the composer holds sends as it does during the user's own reply, and says so: "A job is replying in this thread. You can send once it finishes, or stop it."
Stop works on a scheduled run.
The run registers its ticket as a running job and watches the ticket's status (`stoppableTurnTicket`), the same two paths a user turn's job uses for a Stop handled by this instance or by another.
A Stop cancels the run's context with its own cause, `ErrTurnStopped`, which wraps `context.Canceled` so the ticket still ends cancelled.
A run stopped mid-call is reported with that error, even when the error that surfaced was the provider's (`stoppedTurnError`).
A run cancelled for any other reason, such as a worker shutting down, keeps plain `context.Canceled` and is still recorded as a failure, so the thread never claims a stop nobody made.
The scheduler records "Stopped from its thread before it finished." as the run's last error, and does not post its "I tried to run your scheduled job, but it failed" message.
The registration ends once the reply is saved, so a late Stop no longer cuts the run's memory and checkpoint work short.

## Alternatives considered

- **A push channel (SSE or WebSocket) instead of the watch poll.** It would be immediate and idle-free, but the app has no such channel, and adding one is infrastructure out of all proportion to this fix. The watch is one small GET per visible open thread.
- **Checking only around a job's `next_run_at`.** The client could time its checks to the thread's schedule. It would miss Run now, webhooks, other tabs and clock skew, and still needs the same lookup.
- **Letting the user send while a run generates, queued behind it.** The gate supports it, but the client follows one turn at a time, and sending would replace the run's live bubble with the queued turn's. Holding sends with an explanation matches what a user's own reply does.
- **Hiding Stop during a run.** It was simpler, but would leave the composer locked with no way out for the length of a long Advanced run, or indefinitely behind a run whose worker died.
- **Naming the job in the label.** The ticket does not know which agent job it serves, so this needs a schema change.

## Consequences

- An open, visible, idle thread makes one `GET /chat/{chatId}/active-job` every 5 seconds.
- With two live turns in a thread, the client now follows the one generating, not the newest one queued behind it. The queued one is picked up by the next watch tick once the first has replied.
- A scheduled run's ticket now carries draft deltas and a tool timeline while it runs, and a `result_id` afterwards.
- Stopping a Standard scheduled run saves nothing; an Advanced one keeps its partial reply, as on the chat path. A stopped one-off job ends `failed`, with the stop as its last error.

## Not done yet

- The label says "Job running", not which job.
- The bubble uses the thread's personality, not a job's personality override.
- A run's expression pick lands after its ticket completes, so the reply's expression shows on the next sync rather than live.
