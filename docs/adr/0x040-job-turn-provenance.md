# ADR 0x040: Show which job a turn came from

- **Status:** Accepted
- **Date:** 2026-11-03
- **Deciders:** What Iff maintainers
- **Issue:** none (reported in conversation)
- **PR:** see the pull request that adds this file (stacked on the one that adds ADR 0x039)

## Context

ADR 0x039 made a scheduled job's run visible while it runs: a pending reply marked "Job running", with its text and tool calls streaming.
Once the reply was saved, nothing set it apart from a reply to the user.
Scrolling back through a thread, there was no way to tell which replies a job wrote, or which job.

The job's prompt was invisible too.
A scheduled run's prompt is an ephemeral user message: it is never saved, so it never becomes part of the thread's history.
That is deliberate, for several reasons: the prompt repeats on every run, it would read as something the user said, and it would crowd the context of later turns.
But it left a reply in the thread answering a question nobody could see.

Nothing recorded the link either.
A reply does not reference the job that produced it, and the run's queue ticket (an `agent_job_run` job keyed by the chat) does not know its agent job, so the live "Job running" label could not say which job was running.

## Decision

### 1. A typed `job_run` record on the reply

An assistant message gets a `job_run` column: a `modeltypes.JobRun` (`agent_job_id`, `title`, `prompt`) stored as JSON, as `agent_run` already is for Advanced runs (ADR 0x026).
A scheduled run's reply records the job it ran for, and the prompt and title as they were when it ran.
It is NULL on every other message.

It is display-only, like `model_reasoning`: the context builder never reads it, so the prompt still never reaches the model on later turns, and the reasons for keeping it out of the thread's history still hold.

It is a snapshot rather than a foreign key.
The prompt has to be stored anyway, since the job's prompt may change after the run, and a reply should show what it actually answered.
With the title and the job's id beside it, the reply is self-contained: it needs no join to render, and it still names its job after the job is deleted.

### 2. The same record on the run's queue ticket

A scheduled run's queue ticket gets the same `job_run` column, set when the ticket is created (`beginEphemeralChatTurn`).
The client following a run polls the ticket (`GET /job/{id}`), so it can name the job and show its prompt from the moment the run starts.
The tool timeline and other progress stay in `progress`, which several writers replace whole, so the record does not ride there.

A stopped Advanced run's partial reply, saved from the ticket's draft, takes the record from the ticket.

`GET /chat/{chatId}/active-job` returns the ticket's `job_run` too.
That is what tells a scheduled run apart from the other chat-keyed `agent_job_run` turns, a webhook background run and the onboarding welcome, which the client follows the same way (ADR 0x039) but which are no job's run.
The client shows the "Job running" label, the job chip and the composer's "A job is replying" notice only for a run with a `job_run`.
It follows the others as a plain pending reply, placed by position.

### 3. The job reaches the agent on the run's context

The scheduler puts the `JobRun` on the run's context (`models.WithJobRun`), as it already does for the job's turn settings (ADR 0x026).
Several interfaces sit between the scheduler and the agent, and only scheduled runs have a job, so a context value is less disruptive than a new parameter on every entry point.
The sync entry points' detached context copies it (`detachedUserContext`).
The ticket and the reply both read it from the context (`models.JobRunFromContext`), so a turn with none (a webhook background run, the onboarding welcome) records none.

### 4. A job chip shows the job, its prompt and a link, collapsed

A reply with a `job_run` carries a chip at its top, styled like an attached thread's chip: a clock and the job's title.
While the run is live, it reads "Job running · <title>".
It is collapsed by default; opened, it shows the prompt the job ran and an "Open job" link to the job's page.
So a job's turn is marked, its prompt is one click away, and none of it reads as a user turn.

## Alternatives considered

- **Saving the prompt as a user message.** It would show the prompt, but put it back into the thread's history and the model's context, which is what the ephemeral prompt avoids. It would also read as if the user had said it.
- **A display-only `run_job` tool call.** The first version stored the job and prompt as a pseudo tool call on the reply, like "Load Memory", and needed no schema change. But it is not a tool call: every reader of tool calls (the tool list, the context policy, metrics, exports) had to learn to skip it or treat it specially, and the live row had to be threaded into the progress payload by hand. A typed field says what it is.
- **A foreign key to the agent job.** It would keep the link exact and show a renamed job's new title, but it loses everything once the job is deleted, still needs the prompt stored beside it, and needs a join to render each reply. The snapshot covers the display need with one column.
- **Versioning agent jobs, with the reply pointing at the version it ran.** It would avoid repeating the prompt on every reply, but it needs a version table and a write on every job edit. A prompt is a few hundred bytes, and storage is not the constraint here, so the copy is simpler.
- **The job's prompt as a row in the tool-call list, with an icon-only clock chip.** A job's prompt is not something the model did, and the list collapses it out of sight behind "N tool calls". A chip on the reply, like an attachment, puts it with the reply and names the job at a glance.
- **Recording webhook background runs too.** A webhook has no page to link to, and its message is the external system's, so it is left out for now.

## Consequences

- `chat_messages` and `jobs` each gain a nullable `job_run` jsonb column (auto-migrated).
- The API returns `job_run` on chat messages, on jobs and on the thread's active job (`JobRun` in `openapi.yaml`), and account backups import it.
- A job run's memory load now shows live on its ticket, as on a user's turn.
- Replies saved before this change have no `job_run`, so they show no chip. Nothing is backfilled.
- Renaming a job does not rename the chip on earlier replies: the record keeps the title at the time of the run. The link still goes to the job.
- A deleted job's chip still names it and shows its prompt; its link then reports the job missing.

## Not done yet

- Webhook background runs record no `job_run`.
- Nothing queries replies by job (for example "every reply this job wrote"); the JSON column would need an index for that.
