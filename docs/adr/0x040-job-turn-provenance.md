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

The live "Job running" label also could not say which job was running, because the run's queue ticket does not know its agent job.

## Decision

### 1. A display-only `run_job` tool call opens a job's turn

A scheduled run's turn starts with a pseudo tool call, `run_job`, built the way "Load Memory" already is (`memoryEnrichmentToolCallName`).
It is not a real tool, and the model never sees it.
Its input names the job (`agent_job_id`, `title`) and its output is the job's prompt.

It is shown live: it is the first row of the turn's tool timeline from the moment the run starts (the ticket's progress), ahead of the memory load.
It is saved with the reply, first among its tool calls (`leadToolCallsForChatContext`).
So the prompt can be read in the thread, in the same place and form as everything else the turn did.

### 2. The prompt still stays out of the thread's history

`toolContextPolicyFor` never carries a `run_job` result into a later turn's context, as for "Load Memory".
The prompt is in the thread for the user to read, and nowhere the model reads it, so the reasons for keeping it out of history still hold.

### 3. The job reaches the agent on the run's context

The scheduler puts the job's id and title on the run's context (`models.WithJobRunSource`), as it already does for the job's turn settings (ADR 0x026).
Several interfaces sit between the scheduler and the agent, and only scheduled runs have a job, so a context value is less disruptive than a new parameter on every entry point.
The sync entry points' detached context copies it (`detachedUserContext`).
A turn with no source (a webhook background run, the onboarding welcome) gets no `run_job` call.

Because the saved call names the job, no schema change is needed.
The ticket still does not know its agent job, and does not need to.

### 4. A job chip shows the job, its prompt and a link, collapsed

A reply with a `run_job` call carries a chip at its top, styled like an attached thread's chip: a clock and the job's title.
While the run is live, it reads "Job running · <title>".
It is collapsed by default; opened, it shows the prompt the job ran and an "Open job" link to the job's page.
So a job's turn is marked, its prompt is one click away, and none of it reads as a user turn.

The `run_job` call is left out of the reply's tool-call list, live and saved, so it shows in one place only.
The data stays a tool call: only the presentation is a chip.

## Alternatives considered

- **Saving the prompt as a user message.** It would show the prompt, but put it back into the thread's history and the model's context, which is what the ephemeral prompt avoids. It would also read as if the user had said it.
- **A `source_agent_job_id` column on chat messages.** It would mark job replies directly, but the `run_job` call already carries the job, and the chip needs the prompt's call anyway. One record of where a reply came from is enough.
- **`run_job` as a row in the tool-call list, with an icon-only clock chip.** This was the first version: the prompt sat with the turn's other tool calls and the chip named the job only on hover. A job's prompt is not something the model did, though, and the list collapses it out of sight behind "N tool calls". A chip on the reply, like an attachment, puts it with the reply and names the job at a glance.
- **A `run_job` call for webhook background runs too.** A webhook has no page to link to, and its message is the external system's, so it is left out for now.

## Consequences

- Every scheduled run's reply gains one saved tool call, and its turn's live timeline gains one row, which the web client shows as the chip rather than as a tool row.
- A job run's memory load now shows live in its timeline, as on a user's turn, since the run now has a progress recorder from the start.
- Replies saved before this change have no `run_job` call, so they show no chip. Nothing is backfilled.
- Renaming a job does not rename the chip's tooltip on earlier replies: the call records the title at the time of the run. The link still goes to the job.
- A deleted job's chip still links to its page, which then reports the job missing.

## Not done yet

- Webhook background runs get no `run_job` call.
- A stopped Advanced run saved as a partial reply from its draft (ADR 0x026) does not get the `run_job` call, since that save does not go through the reply's tool-call assembly.
