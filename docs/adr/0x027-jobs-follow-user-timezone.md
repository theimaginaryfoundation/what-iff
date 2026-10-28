# ADR 0x027: Recurring jobs that follow the user's timezone

- **Status:** Accepted
- **Date:** 2026-10-22 (revised 2026-10-28 after review: sections 4–7)
- **Deciders:** What Iff maintainers
- **Issue:** [#371](https://github.com/theimaginaryfoundation/what-iff/issues/371) (follow-up [#372](https://github.com/theimaginaryfoundation/what-iff/issues/372))
- **PR:** [#373](https://github.com/theimaginaryfoundation/what-iff/pull/373)

## Context

A recurring agent job reads its schedule in a timezone, so "every day at 8am" means 8am where the user lives.
Each job saves its own `timezone` when it is created or edited, defaulted from the user's profile timezone, and the scheduler builds the cron trigger in that saved zone.

That breaks when the user travels.
A maintainer on a trip from New York to Los Angeles found every "morning check-in" firing at 5am local time.
Some jobs should stay where they are (a reminder before the New York market opens), but jobs about the user's day (check-ins, rituals, an evening wind-down) should move with them and move back when they come home.

The app already knew where the user was.
The web app sends the browser's IANA timezone with every chat message (`client_timezone`), but only for that request's context; it was never saved.

While working through this we also found that changing the profile timezone moves no existing job, because each job keeps the zone it saved.
That is a separate gap, filed as #372.

## Decision

### 1. A per-job flag, off by default

`agent_jobs.follow_user_timezone` (bool, default false) marks a job whose recurring schedule follows the user.
Nothing turns it on for the user.
The job editor has a "Follow me when I travel" checkbox.
`create_agent_job` and `update_agent_job` take `follow_user_timezone`, and their descriptions tell the model to set it only when the user asks for it.
We decided against guessing from the wording ("every morning" sounds local): a wrong guess moves a job the user wanted pinned, and that is more frustrating than asking.

### 2. "Where the user is" is what the browser reports, applied automatically and best effort

`users.current_timezone` holds the zone the web app's browser last reported, through `PUT /api/user/current-timezone`.
The web app (`CurrentTimezoneService`) reports on sign-in and whenever the tab becomes visible again, and only when the zone differs from the one the server has.
The server writes only on a change.

The browser's zone (`Intl.DateTimeFormat().resolvedOptions().timeZone`) comes from the device's OS clock setting, not from IP geolocation, so a VPN exit node in another country does not move anyone's jobs.
That was the main reason we had considered a manual override, and it does not apply.

Detection applies without asking.
It is best effort, and we accept its limits: Discord and webhook messages carry no timezone and never change it, and a device with the wrong clock setting gives a wrong answer.
Two devices in different zones each report theirs; section 7 covers what happens when they keep taking turns.
`"Local"` is rejected everywhere a timezone is accepted (`modeltypes.ValidTimezoneName`, used by the profile, current-timezone, job and schedule-parse paths): Go's `time.LoadLocation` accepts it, but it means the server's own zone.
The profile `timezone` is not touched; it remains the user's home zone for everything else.

### 3. The effective timezone is derived when a job is loaded

A job keeps its own `timezone` as its home and fallback.
`toAgentJobModel` derives `EffectiveTimezone`: the owner's `current_timezone` for a job that follows them and once one has been reported, else the job's `timezone`.
Every agent-job query already loads the owner edge, so there is no extra query.
The API returns it as `effective_timezone`, and `AgentJob.ScheduleTimezone()` is what the scheduler, the run context ("now" and timestamps the model sees) and logs use.

We do not rewrite `agent_jobs.timezone` when the user moves.
Deriving it means coming home needs no write and no memory of what the zone used to be, and a job that stops following the user goes straight back to its own zone.

### 4. The scheduler reschedules only the jobs that move

The reconcile fingerprint (`manager.go`) uses the effective timezone, so a change of the user's timezone reschedules exactly the jobs that follow them, within one reconcile interval (30s).
Nothing else updates a job's stored `next_run_at` until it runs, so for a following job the manager writes the new trigger's next fire time when it differs from the stored one.
It does this on the first schedule after startup or a leader handover too, in case the user's timezone changed in between; for a weekly job the stored value could otherwise stay in the old zone for days.
Turning the setting on or off (job editor or `update_agent_job`) stores the job's next run in its new effective timezone straight away, so the response and the job card are right before the scheduler catches up.

The schedule arithmetic (`CronTrigger`, `NextRunAt`, `LostSlotOnMove`, `StoreNextRun`) lives in `internal/agentjobs/schedule`, which the scheduler, the job handlers and the agent's job tools all use.

### 5. The day of the move: no slot runs twice

Each run records the zone it ran in (`agent_jobs.last_run_timezone`).
When a following job's zone differs from the one it last ran in, the slot guard skips fire times at or before the last run's wall-clock time placed in the new zone, but only those that come before the job's next regular run in the zone it left.
If it cannot get past that cutoff (a schedule that stops advancing, or more than 10,000 fire times), it fails rather than return a slot that already ran; the scheduler then reports the schedule error.

Moving west: 8am ran in New York, the user signs in from Los Angeles at 9am New York time, and 8am Los Angeles (11am New York) that same day does not run again.
Runs recorded before `last_run_timezone` existed count as having run in the job's own zone.

The guard applies only to jobs that follow the user.
A pinned job's timezone changes only when someone edits it (editing a schedule saves the browser's zone as the job's), and an edited job runs exactly as edited, the way it always has.

The second condition keeps frequent schedules on their cadence.
An hourly job that ran at 10:00 New York (7:00 Los Angeles) and moves to Los Angeles at 7:30 there runs next at 8:00 Los Angeles (11:00 New York), the time it would have run anyway, even though "8:00" already ran in New York.
Skipping every label that ran would have left it four hours without a run; the first version did that, and review caught it.
Coarser schedules can still show a longer gap on the day of a move: "every 4 hours" that ran at 8:00 New York skips 8:00 Los Angeles (11:00 New York, sooner than its next run at noon New York) and runs at noon Los Angeles, seven hours later.
We accept that: running at a time that is not on the schedule would be more surprising than one long gap.

### 6. Moving east: a lost slot runs once, right away

Moving east can lose a slot instead: it has already passed in the new zone but had not come up yet in the old one.
For example, 8am Los Angeles ran yesterday, and the user signs in from New York at 9am; 8am New York has passed, and 8am Los Angeles (11am New York) would never fire.

Whenever reconcile (re)schedules a following job, including at startup, `LostSlotOnMove` checks for such a slot and `runCatchUp` runs the job once immediately.
Several lost slots (an hourly job) still mean one run, not a burst.
The catch-up run records the new zone, so it does not repeat, and it records no lateness metric because it has no planned time.

Signing in late, which is the usual case after a flight, loses nothing.
While the user is offline the old schedule keeps running, so by the time they sign in from New York at 2pm, 8am Los Angeles has already run.
A lost slot only exists in the window between the two zones' times, so a catch-up run is never more than the size of the time difference late.

Turning the setting on counts as a move, on purpose.
A job pinned to Los Angeles runs at 8am there; the user, whose browser reports New York, turns following on at 9am New York.
The job now runs on New York time, where 8am has passed, while 8am Los Angeles (11am New York) has not come yet; the choices are to run it now or to skip the day, and running it now is closer to what the user asked for.
So saving the setting can start a run straight away.

### 7. Devices that keep disagreeing pause following

Two signed-in devices in different zones (a laptop left open at home, a phone on a trip) take turns reporting, and following jobs would move back and forth with them; a move east inside the window can also start a catch-up run as well as the regular one.
We cannot tell which device the user means, so when it looks like this we stop guessing and tell them.

The server keeps the last day of changes (`users.current_timezone_changes`).
When a report makes the day's changes four or more, or three that go back and forth between two zones (New York → Los Angeles → New York → Los Angeles), following is paused (`users.timezone_following_paused_at`, `modeltypes.TimezoneFlapping`).
Real trips stay below both: one move, a trip with a stop on the way, or a same-day return.
A first report is not counted as a change.

While paused, `current_timezone` is cleared, so following jobs go back to their own timezone (the one the user chose, and the one they ran in before the feature existed), and reports are ignored.
The read, the check for a following job and the write happen in one transaction, and the write is conditional on what was read, so two devices reporting at once cannot both apply.
The report that paused it answers with `paused_now`; the profile carries `timezone_following_paused_at`.
The web app shows a notice with **Resume following** and **Dismiss**, and the job editor says following is paused, with a Resume link.
Resuming (`POST /api/user/current-timezone/resume`) clears the pause and the history, and the browser reports its zone again straight away.
A user with no following job is never paused: there is nothing to protect, and a notice would only be noise.

## Alternatives considered

- **A manual "travel timezone" with an end date.**
  This was the first idea, to guard against a VPN making the app think the user had moved.
  The browser's zone is not IP-based, so the risk was not there, and a manual setting is one more thing to remember to set and unset.
  It is still a reasonable addition if detection turns out to be wrong too often.
- **Ask before switching** ("Looks like you're on Pacific time — move your jobs?").
  Rejected in favour of applying automatically: the flag is already the user's consent for that job, and a prompt on every trip is friction for the case the feature exists for.
- **IP geolocation.** Rejected: VPNs and mobile carriers make it unreliable, and it would send the user's location to a lookup service.
- **A timezone header on every API request, saved in the auth middleware.** It would catch more requests, but puts a write on the hot path of every call; a dedicated endpoint called on sign-in and tab focus is explicit and easy to test.
- **Rewriting each job's `timezone` when the user moves.** Needs a write per job, loses the home zone unless we store it somewhere else, and makes "stop following" ambiguous.
- **On the day of the move, keeping the old schedule for the size of the shift before switching** (for moving east).
  It delivers the lost run later than catching up does (11am instead of 9am in the example), and for hourly jobs the old and new zones' times land on the same hours, which is hard to reason about.
- **Accepting the lost eastbound run.**
  It was the first version of the PR; it was changed because a check-in that silently does not happen on the day you travel is exactly the surprise this feature is meant to remove.

## Consequences

- A job that follows the user keeps its local time on a trip and returns to it at home, with no action after the one-time opt-in.
- Jobs that do not follow the user behave exactly as before; the fingerprint and triggers only change for jobs whose effective zone differs from their own.
- Three new columns, all added by auto-migration: `users.current_timezone`, `agent_jobs.follow_user_timezone`, `agent_jobs.last_run_timezone`.
  The datastore's hand-written SQLite test fixtures carry them too.
- `RecordAgentJobRun` takes the run's timezone, and `models.AccountBackupAgentJob` carries `follow_user_timezone` (older packets import it as false).
- A user who switches between devices in different zones moves their following jobs once or twice, and then following pauses with a notice instead of continuing to move them.
  Until they resume, following jobs run on their own timezone.
- Two more columns for the pause: `users.current_timezone_changes` (JSON) and `users.timezone_following_paused_at`.

## Not done yet

- Offering to move pinned jobs when the profile timezone changes (#372).
- A manual override for when detection is wrong (see Alternatives).
- A timezone signal from Discord or webhooks.
- A job that has never run is not checked for an eastbound lost slot, since there is no last run to compare against.
- A pause does not end on its own; the user resumes it. An automatic resume after some quiet time is a candidate if pauses turn out to be common.
- Turning following off does not apply the no-rerun guard (it applies only while a job follows the user), so on the day someone switches it off a slot can run in both zones.
