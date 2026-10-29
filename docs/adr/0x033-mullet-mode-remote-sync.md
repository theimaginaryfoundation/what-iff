# ADR 0x033: Mullet mode — keep a self-hosted copy of a hosted account, automatically

- **Status:** Proposed (experimental beta)
- **Date:** 2026-10-13, revised 2026-10-28 (sync on wake instead of polling; first published as 0x026, renumbered because 0x026 went to another ADR first)
- **Deciders:** What Iff maintainers
- **PR:** [#324](https://github.com/theimaginaryfoundation/what-iff/pull/324)
- **Builds on:** webhook read access and token scopes ([ADR 0x023](0x023-webhook-read-access.md)), the account export and import (ADR 0x018), lazy thread rehydration, the plugin seam (`internal/plugins`), and the Discord relay's shape ([ADR 0x025](0x025-discord-relay.md)).
- **Related:** "What if…" branching, PR #148 (draft): the fork semantics used here.

**Mullet mode** is the product name: business up front (the hosted app, for talking on the go), party in the back (your own server, with your own models, keys and data).
The app, code, routes and settings call it *Remote sync*, which says what it does.
How to set it up and why you would: [the mullet mode guide](../guides/mullet-mode.md).

## Context

People talk to their personas on the hosted app (whatiff.chat) when they are out, and some of them also run the open-source app at home.
Today the only way to move a conversation from one to the other is an account export: request it, wait for the email, download the ZIP, import it locally.
That is a one-off snapshot. The next conversation on the hosted app is not there, and doing it again re-imports everything.

The pieces for something better already exist:

- The hosted app serves an owner's threads and messages to a static `wht_` token that holds the `chat:read` scope (ADR 0x023): thread list (newest activity first), a thread's messages (newest first, with a stable cursor), and persona names.
- The open-source app can create threads from foreign transcripts, and a thread with an import `source` and no summary is summarized lazily the first time it is used (thread rehydration), so a long imported history continues well.
- Account-export imports record `import_hash = sha256(source chat id)` on every thread they create, so a local thread can be traced back to the hosted thread it came from.
- The Discord relay showed how an optional, server-side integration ships in this tree: a plugin with its own tables, an encrypted token, and a background loop on the server's lifecycle.

What is missing is the loop: "paste a token once, and from then on everything you say on the hosted app shows up here."

The hard part is not copying. It is what happens when **both sides write to the same thread**: you continue a conversation at home, then continue the same conversation on your phone.
Interleaving the two by timestamp would produce a transcript neither persona actually saw. Overwriting either side loses something somebody said.
Branching (PR #148) already gives the answer the product wants: when histories diverge, keep both, as a thread and its branch.

## Architecturally significant requirements

| # | Requirement | Why it shapes the design |
|---|---|---|
| R1 | **Set up once, then hands-off.** One token, one URL, one toggle. No per-thread action, no manual export. | The local server syncs on its own, triggered by the local app being used, not by a one-shot import. |
| R2 | **Works against today's hosted app.** No hosted-side release is needed for v1. | Only the `chat:read` webhook reads may be used, with the shapes they already return. |
| R3 | **Nothing is lost on either side.** Sync never deletes or edits a local message, and never writes to the hosted account. | v1 is pull-only and append-only; local deletes and edits stay local. |
| R4 | **Concurrent writes never interleave.** If a mirrored thread got local messages and the hosted thread also moved on, both continuations survive as separate threads. | Divergence is detected per thread and resolved by forking, never by merging. |
| R5 | **Idempotent and crash-safe.** Re-running a sync, two instances running at once, or a crash halfway through never duplicates a message. | Per-message links with a unique key; each thread's changes commit in one transaction; a lease per account. |
| R6 | **Gentle on the hosted app.** A copy nobody is reading costs the hosted app (almost) nothing. | Syncs start when the local app is used (§8), with a slow, optional background sync; incremental discovery that stops at the first unchanged page; page limits; per-run caps; no retries on auth errors. |
| R7 | **Off unless the operator turns it on, and safe when on.** A multi-tenant server must not become a proxy into other networks. | Opt-in env flag; HTTPS to public addresses only, checked on every connection (local networks only with an explicit flag); the token is encrypted at rest and never returned. |
| R8 | **Continuing offline works well.** A mirrored 2,000-message thread must not be fed raw to the model on the first local turn. | Mirrored threads are created as imported threads, so the existing rehydration summary (and memory seeding) runs on first use. |
| R9 | **Optional and removable.** The feature is its own package and tables; the core `chats` and `chat_messages` schemas do not change. | Plugin seam, separate tables keyed by plain UUIDs, like the Discord relay. |

## Decision

### 1. Shape: a pull-only plugin on the local server

```
 hosted app (whatiff.chat)                          self-hosted app
 ┌──────────────────────────┐   HTTPS, wht_ token   ┌────────────────────────────────────┐
 │ GET /api/webhooks/chat    │ ◄──────────────────── │ remotesync.Syncer (one account)    │
 │ GET /api/webhooks/chat/   │                       │   discover changed threads         │
 │     {id}/messages         │                       │   pull new messages per thread     │
 │ (chat:read scope)         │                       │   plan: append | adopt | fork      │
 └──────────────────────────┘                       │        ▼                           │
                                                     │ datastore (one tx per thread)      │
                                                     │   chats / chat_messages            │
                                                     │   remote_sync, remote_sync_chat,   │
                                                     │   remote_sync_message              │
                                                     └────────────────────────────────────┘
```

- `internal/remotesync` holds the logic: an HTTP `Client` for the hosted webhook reads, a `Syncer` that runs one account's sync against a `Store` interface, and a `Runner` that picks due accounts on a timer.
  It is tested against a fake remote and a fake store.
- `internal/remotesyncplugin` wires it in through the plugin seam: the `/api/remote-sync` routes and the runner on the server's lifecycle context.
  It is linked in `cmd/api-server/plugins.go` like the Discord relay, and does nothing unless `REMOTE_SYNC_ENABLED=true`.
- Sync is **pull-only** in v1. Writing local turns back to the hosted account is a separate decision (see Follow-ups).

### 2. Setup (R1, R7)

1. On the hosted app: Tools → Webhooks → create a token with **Read threads** (`chat:read`). This page already exists.
2. On the local app: Tools → **Remote sync** → paste the token (the address defaults to the hosted API, `https://api.whatiff.chat`; the app's own addresses are accepted and mapped to it) → **Save**.
   Saving checks the token against the remote (`GET /webhooks/personality?limit=1`) and reports precisely what is wrong: unreachable, not a token (401), or missing `chat:read` (403).
3. Sync is on. The first run starts immediately. After that, the copy is brought up to date whenever the local app is opened or comes back after a while (§8), plus once an hour in the background. **Sync now** runs one at once.

A brand-new local account has no persona yet, and the app normally sends such an account straight to persona setup.
Tools is exempt from that redirect (like Import & Export), and the first-run welcome offers "Keep a copy of those threads here" when the server runs mullet mode, so someone whose personas live on the hosted app can bring their history over first.

Minting the token stays a deliberate step on the hosted app: a local server asking the hosted app to mint its own credential would be circular, and the token page is where scopes are chosen and revoked.

### 3. Data (R5, R9)

Three tables, owned by the user (cascade on account deletion), referring to chats and messages by plain UUID so the core schemas are unchanged:

- **`remote_sync`** (one per user): `base_url`, `token` (encrypted with the MCP token helper, `TOKEN_ENCRYPTION_SECRET`; sensitive), `token_hint` (last four characters, for display), `enabled`, `status` (`ok`, `error`, `needs_attention`), `last_error`, `last_run_at`, `last_success_at`, `last_run` (counts from the last run), and `lease_until`, the claim that keeps two instances (or a timer tick and **Sync now**) from running the same account at once.
- **`remote_sync_chat`** (one per hosted thread): `remote_chat_id`, `chat_id` (the local thread currently tracking it), `remote_name`, `remote_last_message_time` (the hosted thread's `last_message_time` as last seen, for change detection), `remote_head_sent_at` (the newest hosted message pulled), `forks` (how many times this thread forked), `state` (`active`, or `detached` when the local thread was deleted). Unique on (sync, remote chat id).
- **`remote_sync_message`** (one per hosted message copied into a local thread): `chat_id`, `chat_message_id`, `remote_message_id`. Unique on (local chat, remote message id): the dedupe key. It is also provenance: a local message with a link came from the hosted app; one without was written here.

### 4. Discovering what changed (R6)

The hosted thread list is ordered by `last_message_time`, newest first.
The syncer walks it a page (100) at a time, once for active and once for archived threads, and compares each thread's `last_message_time` with the value stored on its link.
A thread with no link, or a different value, is **changed**. The walk stops at the first page with no changed thread: everything after it is older still.
An idle account therefore costs two list requests per interval. Threads with no messages yet are skipped.

At most `REMOTE_SYNC_MAX_THREADS_PER_RUN` (default 50) threads are synced per round. The rest keep their old stored value, so they are still "changed". A round that leaves some goes straight on to another, with a full walk (the newest page is now unchanged and would stop an incremental one), until none are left, a round fails, or a round makes no progress; the rounds are recorded as one run. A first sync of a large account therefore finishes in one go instead of taking one wake or interval per 50 threads.

### 5. Pulling a thread's new messages

The syncer walks the thread's messages newest first with `cursor`, and stops once it passes `remote_head_sent_at` minus a two-minute **overlap**.
Messages it already linked into the tracking thread are dropped; what is left, oldest first, is the batch to apply.
The overlap catches a message that committed late with an earlier timestamp. The link table makes the overlap free of duplicates.

Hosted messages are never edited or deleted one by one (ADR 0x023), and replies are saved whole, never streamed into a placeholder row, so the newest message is always safe to copy.

### 6. Applying a batch: append, adopt or fork (R3, R4)

Everything below happens in one transaction per thread, together with the link updates.

The **head** of a tracking thread is its newest linked message (the newest message that came from the hosted app).
A **local tail** is any message in the tracking thread ordered after the head that has no link: something written here.

| Situation | What happens |
|---|---|
| No link and no local thread | **Create** a mirror thread and append the whole history. |
| No link, but a local thread with `import_hash = sha256(remote chat id)` (an earlier account-export import, or a mirror whose link was removed) | **Adopt** it: align its messages with the hosted history, oldest first, by origin and text, and link the matching prefix. Then continue as below. Blank hosted replies (a generation that failed or was cut off) are never copied: an export leaves them out, so copying them would break the alignment and fork every imported thread that has one. |
| Link, no local tail | **Append** the batch to the tracking thread. |
| Link, local tail | **Fork**: create a new thread holding a copy of the tracking thread up to and including the head (links copied too), append the batch to it, and point the link at it. The old thread keeps the local continuation and is no longer tracked. |
| Link, but the local thread was deleted | Mark the link `detached` and stop. Deleting a mirror locally means "I don't want this here"; it is not recreated. |

A fork is named `<thread name> (from <host>)` and is created exactly like a mirror, so it too is summarized lazily when first used.
Forking again later works the same way: whichever thread is tracking is the one that gets checked for a local tail.

```
 hosted:  A1 B1 A2 B2 ─────────────► A3 B3            (you kept talking on your phone)
 local:   A1 B1 A2 B2 ─► a3 b3                         (you kept talking at home)

 after the next sync:
 "Trip planning"                A1 B1 A2 B2 a3 b3      (untracked: your home continuation)
 "Trip planning (from …)"       A1 B1 A2 B2 A3 B3      (tracking: the hosted continuation)
```

Forking is what PR #148 does for a person; here the syncer does it when histories diverge.
That PR is a draft and far behind `main`, so this change does not depend on it: the fork copy is a small datastore function of its own, and the fork point is kept on the link.
When #148 lands, the sync fork should set #148's `forked_from_chat_id` and `forked_from_message_id` so the branch banner and "N branches" marker show up; that is a few lines.

### 7. What a mirror thread is (R8)

- `source = remote_sync`, `import_hash = sha256(remote chat id)` (so a later account-export import skips it instead of duplicating it, and so a removed link can be re-adopted), name from the hosted thread, archived if the hosted thread is archived, `last_message_time` from its newest message.
- **Persona:** the hosted thread's `personality_name` is matched by name (trimmed, case-insensitive) against the local personas, the way account import matches them, and kept on the link (`remote_personality_name`).
  Persona prompts are private working state that the `chat:read` surface does not serve (ADR 0x023), so a copy whose persona is not here yet has none.
  Every run and every wake re-matches such copies (`RematchRemoteSyncPersonas`, no remote call), and the web app sends a wake when a persona is created or imported, so creating "Vix" (or importing the account export) gives Vix's threads to Vix straight away. A persona someone chose for a copy is never replaced.
  The first-run welcome names the personas copied threads are waiting for (`unmatched_personas`): with no persona at all, the app sends an account to persona setup, and a sync-only account would otherwise see no sign that its threads are already here.
- **Model:** left unset, like any imported thread; the local default applies.
- **Messages:** text, origin, `sent_at` and the generation display fields (model, persona and mood names, expression key). Read status is `read`. Tool calls, reasoning and attachments are not served by `chat:read` and are not copied.
- **Context (summaries):** a reply reads a thread's running summary plus at most the 50 newest messages after it, so whatever sits between the two is invisible. Sync keeps that gap closed in two ways (`settleRemoteSummary`, run whenever remote messages land in a thread nobody has continued here):
  - **The hosted summary comes too.** `GET /api/webhooks/chat/{id}/summary` (`chat:read`) returns the hosted thread's summary and `window_start` (its `last_checkpoint_at`).
    The syncer asks for it with each thread it brings messages into, and the copy takes it when it reaches further than the copy's own.
    A new copy then needs no summarizing here at all, however long it is.
    The summary is derived from the thread's messages, which the scope already serves, so it widens nothing.
    A hosted server from before the route answers 404, which reads as "no summary".
  - **A local catch-up otherwise.** When more than 50 messages sit past a copy's summary (no hosted summary, one that lags, or an account-export import that sync appended to), the copy is flagged `rehydration_state = stale`.
    Its next use runs the existing rehydration in catch-up mode: the messages from the old window start up to the last few turns are folded into the existing summary in one pass (chunked when long), and memories are seeded from that stretch.
  - A copy that has never been summarized is left to its first rehydration, which summarizes everything but the last few turns, plus memory seeding, on the local server's own model keys.
  A diverged home thread is never given the hosted summary: it is no longer the hosted thread. Its fork, which is, is.
- **Renames:** a hosted rename is copied only if the local name is still the last hosted name, so a local rename wins.

### 8. Running: sync on wake, not on a clock (R5, R6)

The copy only matters when someone is about to read it, so that is when the remote is asked.

- **Wake.** The web app calls `POST /api/remote-sync/wake` when a signed-in session starts, and when the tab becomes visible after at least five minutes hidden (`RemoteSyncWakeService`).
  The server starts a run if the last one finished more than two minutes ago (`Syncer.Wake`), and otherwise does nothing.
  Sync being off, not set up, waiting on its owner or already running is not an error: a wake is a hint.
  When the wake started a run, the app follows it and refreshes the thread list if anything came over, so a thread continued on the phone is simply there.
- **Background.** A slow sync for copies nobody is reading (backups, a server whose app is left open): `REMOTE_SYNC_INTERVAL`, default one hour, or `off` for wakes only.
  The first version polled every five minutes; for a hosted app serving many home servers that is a steady stream of requests that almost always find nothing.
  Each background run is due up to a fifth of the interval late (60 to 72 minutes by default), by an amount derived from the account and its last run (`backgroundJitter`).
  Without it, home servers that start together (after a release, or a power cut) would stay in step and reach the hosted app in the same minute every hour; with it they drift apart within a few cycles.
- **Leases.** Every run, however started, first **claims a lease** with a conditional update (`lease_until < now`). Only the claimer runs it, so several API instances, several open tabs, or a wake racing the timer need no leader lock.
- Accounts run one at a time per instance, with a per-request timeout and a cap on response size.
- **401 or 403** sets `needs_attention` with a message that says which, and neither wakes nor the timer run that account until its settings are saved again: retrying a revoked token helps nobody.
- Network errors, 429 and 5xx record `error` and are retried next interval. A failure on one thread is recorded and the run moves on to the next.
- Each run logs counts only (threads checked, updated, created, forked; messages added), never content.

### 9. API (local, session-authenticated)

| Route | Purpose |
|---|---|
| `GET /api/remote-sync` | The account's sync settings and status: URL, token hint, enabled, status, last error, last run and its counts, totals (threads mirrored, messages, forks). `configured: false` when there is none. |
| `PUT /api/remote-sync` | Save URL, token (required the first time; omitted keeps the stored one) and enabled. Validates against the remote before saving; clears `needs_attention`. |
| `DELETE /api/remote-sync` | Stop syncing and forget the token and links. Local threads stay. |
| `POST /api/remote-sync/run` | Run now (202). 409 if a run is already in progress. |
| `POST /api/remote-sync/wake` | The app is being used: run if stale, and re-match personas (§7, §8). Always 200 with `{started, rematched}`. |

When the plugin is off these routes are not mounted (404), and the web app hides the tab and sends no wakes.
That relies on a cross-origin browser being able to read the 404, so the server's not-found and method-not-allowed responses carry CORS headers like every other response (before this, mux skipped middleware for them, and a switched-off plugin looked switched on).

### 10. Security (R7)

- **Opt-in per deployment:** `REMOTE_SYNC_ENABLED=true`. The hosted deployment does not set it, so the hosted app never makes outbound sync requests on a user's behalf. The open-source `docker-compose.yml` turns it on, because a self-hosted server is the thing this is for.
- **HTTPS to public addresses only.** Every connection is checked after DNS resolution (`internal/netguard`, shared with the Discord relay's CDN fetcher), so a user-chosen address cannot reach the server's own network or the cloud metadata service. `REMOTE_SYNC_ALLOW_LOCAL_NETWORK=true` permits `http://` and private or loopback addresses, for syncing from a server on your own network or for development. Redirects are not followed, so the token is never replayed to another host.
- **The token is a read credential for the whole hosted account's conversations.** It is encrypted at rest, never returned by the API (only its last four characters), and never logged. The hosted side can revoke it at any time.
- Error messages shown to the user name the class of failure (unreachable, unauthorized, missing scope, unexpected response), not raw upstream bodies.

## Alternatives considered

- **Periodic account export + import.** Full fidelity (personas, memories), but every run re-exports the whole account, delivery is by email link by design (an export link is never returned to the session that asked), and import is per-thread all-or-nothing, so new messages in an existing thread are never picked up. Rejected for the loop; still the right way to bring personas and memories over once.
- **Merge both sides into one thread by timestamp.** Produces a transcript neither persona saw: each side's replies were written without the other side's turns. Rejected in favour of forking (R4).
- **Last writer wins / hosted side wins.** Loses what someone said. Rejected (R3).
- **Two-way sync in v1.** Writing local turns to the hosted thread needs a write that records an existing exchange without running the agent, which the webhook surface does not have (`user` mode runs a turn; `assistant` mode posts only a reply). It also turns every conflict into a two-sided one. Deferred.
- **Stack on PR #148 now.** It is 346 commits behind `main` with conflicts in ten files. Making the sync wait for it, or rebasing someone's draft as a side effect, is the wrong trade for an experiment. The fork semantics are the same; only the lineage display waits.
- **A new hosted scope (`sync:read`) first.** It would let the sync also bring persona prompts, memories and attachments, and an ascending "since" read would make polling cheaper. It needs a hosted release and a decision about serving private working state to a static token. Deferred so v1 works today (R2), and listed below.
- **Outbound webhooks from the hosted app instead of polling.** Lower latency, but a home server is often not reachable from the internet. Deferred.
- **Polling every five minutes** (the first version). Simple, but every connected home server costs the hosted app two requests per interval, around the clock, nearly all of which find nothing. Replaced by sync on wake (§8).
- **Firebase (FCM) push on change, then sync on wake.** The hosted app already sends FCM pushes to the mobile app (`overlay/internal/push`). Considered on 2026-10-28 and not taken for now:
  - FCM delivers to app instances running a Firebase SDK (Android, iOS, a browser's service worker), not to a server process, so the home server itself cannot receive it. The only receiver at home would be a browser tab of the local app.
  - A push to that tab only helps while the tab is open but unused. Once someone looks at it, a wake already syncs; the push would save at most the gap between "a message was sent on the phone" and "I came back to the tab".
  - It would put the hosted Firebase project's web config, a service worker and a third-party push dependency into every self-hosted install, and the hosted side would keep device tokens for browsers on other people's servers, keyed to a webhook token.
  - What it was meant to fix, no-op load, is fixed by not polling at all.

  Worth revisiting if the local side gains a native client, or if people want the copy current while the tab sits in the background (a long-poll or server-sent-events "something changed" channel from the hosted app would also work behind NAT, without Google in the path).

## Consequences

- A self-hosted install can keep a live copy of a hosted account's conversations with one token.
- A conversation continued in both places becomes two threads. That is the intended result, but it can surprise people until the branch UI from #148 shows the relationship.
- Local copies of hosted threads take local model calls on first use (rehydration summary and memory seeding), metered against the local keys, unless the hosted summary came with them.
- The hosted app serves one new read route (`GET /webhooks/chat/{id}/summary`), so the summary half needs a hosted deploy; everything else works against the hosted app as it is.
- Three new tables and one new plugin. Removing the feature is deleting the plugin and dropping the tables; mirrored threads remain ordinary imported threads.

## Not done yet

- **#148 lineage:** set `forked_from_*` on sync forks so the branch banner and markers show them.
- **`sync:read` scope (hosted):** persona prompts and settings, memories, attachment downloads, chats `updated_since`, an ascending messages-since cursor, and deletion tombstones, behind its own scope and a clear "full copy of your account" warning on the token page.
- **Two-way sync:** an idempotent "record an exchange" write on the hosted side, then push local tails of tracked threads that have not forked.
- **Hosted-side helper:** a "Sync to a self-hosted app" preset on the Webhooks tab that creates a correctly scoped token in one click.
- **Thread list badge** for mirrored and forked threads (the link table already knows which are which).
- **Archive and favourite state** are copied when a mirror is created, not afterwards.
- **Close the apply race:** a local turn that commits between the local-tail check and the append is appended after rather than forked. The window is milliseconds; locking the tracking thread's row for the apply would close it.
- **A summary written after the last message** (the hosted checkpoint runs after the reply is saved) comes over with the thread's next message, not on its own; until then the local catch-up covers the gap.
- **The hosted web app asks `GET /api/remote-sync` once per session** to decide whether to show the tab and send wakes, and gets a 404 from the router (no database work). A build-time flag could skip it where the feature can never be on.

## Test plan

- **Unit (`internal/remotesync`):** the client's error classification and paging; discovery stopping at the first unchanged page; the pull overlap and dedupe; the planner's create / adopt / append / fork / detach decisions, including a second fork; persona matching; caps and the skip for `needs_attention`.
- **Datastore:** apply in one transaction (a failure leaves nothing), fork copy up to the head with links copied, adoption alignment, taking the hosted summary only when it reaches further, flagging a backlog stale and claiming it once, the lease claim under contention, ownership on every query, cascade on account deletion.
- **Handlers:** validation outcomes on save, token never returned, 404 when the plugin is off.
- **End to end (manual for now):** one local server syncing user B from user A on the same server over `http://localhost` with `REMOTE_SYNC_ALLOW_LOCAL_NETWORK=true`: first sync, incremental append, local reply then remote reply (fork), local delete (detach), run now.
