# ADR 0x026: Remote sync — keep a self-hosted copy of a hosted account, automatically

- **Status:** Proposed (experimental)
- **Date:** 2026-10-13
- **Deciders:** What Iff maintainers
- **Builds on:** webhook read access and token scopes ([ADR 0x023](0x023-webhook-read-access.md)), the account export and import (ADR 0x018), lazy thread rehydration, the plugin seam (`internal/plugins`), and the Discord relay's shape ([ADR 0x025](0x025-discord-relay.md)).
- **Related:** "What if…" branching, PR #148 (draft): the fork semantics used here.

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
| R1 | **Set up once, then hands-off.** One token, one URL, one toggle. No per-thread action, no manual export. | Needs a server-side poller, not a client-side or one-shot import. |
| R2 | **Works against today's hosted app.** No hosted-side release is needed for v1. | Only the `chat:read` webhook reads may be used, with the shapes they already return. |
| R3 | **Nothing is lost on either side.** Sync never deletes or edits a local message, and never writes to the hosted account. | v1 is pull-only and append-only; local deletes and edits stay local. |
| R4 | **Concurrent writes never interleave.** If a mirrored thread got local messages and the hosted thread also moved on, both continuations survive as separate threads. | Divergence is detected per thread and resolved by forking, never by merging. |
| R5 | **Idempotent and crash-safe.** Re-running a sync, two instances running at once, or a crash halfway through never duplicates a message. | Per-message links with a unique key; each thread's changes commit in one transaction; a lease per account. |
| R6 | **Gentle on the hosted app.** An idle account costs a couple of requests per interval. | Incremental discovery that stops at the first unchanged page; page limits; per-run caps; no retries on auth errors. |
| R7 | **Off unless the operator turns it on, and safe when on.** A multi-tenant server must not become a proxy into other networks. | Opt-in env flag; HTTPS only (plain HTTP only with an explicit dev flag); the token is encrypted at rest and never returned. |
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

1. On the hosted app: Integrations → Webhooks → create a token with **Read threads** (`chat:read`). This page already exists.
2. On the local app: Integrations → **Remote sync** → paste the token (the hosted URL defaults to `https://whatiff.chat`) → **Save**.
   Saving checks the token against the remote (`GET /webhooks/personality?limit=1`) and reports precisely what is wrong: unreachable, not a token (401), or missing `chat:read` (403).
3. Sync is on. The first run starts immediately; later runs come every `REMOTE_SYNC_INTERVAL` (default 5 minutes). **Sync now** runs one at once.

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

At most `REMOTE_SYNC_MAX_THREADS_PER_RUN` (default 50) threads are synced per run. The rest keep their old stored value, so they are still "changed" next run; a first sync of a large account simply takes a few runs.

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
| No link, but a local thread with `import_hash = sha256(remote chat id)` (an earlier account-export import, or a mirror whose link was removed) | **Adopt** it: align its messages with the hosted history, oldest first, by origin and text, and link the matching prefix. Then continue as below. |
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
- **Persona:** the hosted thread's `personality_name` is matched by name (trimmed, case-insensitive) against the local personas, the way account import matches them. With no match the thread uses the local default persona.
  Persona prompts are private working state that the `chat:read` surface does not serve (ADR 0x023), so they are not synced; importing an account export once brings them over, and later mirrors then match them by name.
- **Model:** left unset, like any imported thread; the local default applies.
- **Messages:** text, origin, `sent_at` and the generation display fields (model, persona and mood names, expression key). Read status is `read`. Tool calls, reasoning and attachments are not served by `chat:read` and are not copied.
- **Context:** the thread has an import `source` and no summary, so the first local turn (or opening it) starts the existing rehydration: a summary of everything but the last few turns, plus memory seeding, on the local server's own model keys.
- **Renames:** a hosted rename is copied only if the local name is still the last hosted name, so a local rename wins.

### 8. Running (R5, R6)

- The runner ticks every minute, lists enabled accounts whose last run is older than the interval, and for each one **claims a lease** with a conditional update (`lease_until < now`). Only the claimer runs it, so several API instances need no leader lock.
- Accounts run one at a time per instance, with a per-request timeout and a cap on response size.
- **401 or 403** sets `needs_attention` with a message that says which, and the runner skips that account until its settings are saved again: retrying a revoked token every five minutes helps nobody.
- Network errors, 429 and 5xx record `error` and are retried next interval. A failure on one thread is recorded and the run moves on to the next.
- Each run logs counts only (threads checked, updated, created, forked; messages added), never content.

### 9. API (local, session-authenticated)

| Route | Purpose |
|---|---|
| `GET /api/remote-sync` | The account's sync settings and status: URL, token hint, enabled, status, last error, last run and its counts, totals (threads mirrored, messages, forks). `configured: false` when there is none. |
| `PUT /api/remote-sync` | Save URL, token (required the first time; omitted keeps the stored one) and enabled. Validates against the remote before saving; clears `needs_attention`. |
| `DELETE /api/remote-sync` | Stop syncing and forget the token and links. Local threads stay. |
| `POST /api/remote-sync/run` | Run now (202). 409 if a run is already in progress. |

When the plugin is off these routes are not mounted (404), and the UI shows the tab as unavailable with the variable to set.

### 10. Security (R7)

- **Opt-in per deployment:** `REMOTE_SYNC_ENABLED=true`. The hosted deployment does not set it, so the hosted app never makes outbound sync requests on a user's behalf. The open-source `docker-compose.yml` turns it on, because a self-hosted server is the thing this is for.
- **HTTPS only.** `REMOTE_SYNC_ALLOW_INSECURE_HTTP=true` permits `http://` for development (two local servers, or a server syncing from itself). Redirects are not followed.
- **The token is a read credential for the whole hosted account's conversations.** It is encrypted at rest, never returned by the API (only its last four characters), and never logged. The hosted side can revoke it at any time.
- Error messages shown to the user name the class of failure (unreachable, unauthorized, missing scope, unexpected response), not raw upstream bodies.

## Alternatives considered

- **Periodic account export + import.** Full fidelity (personas, memories), but every run re-exports the whole account, delivery is by email link by design (an export link is never returned to the session that asked), and import is per-thread all-or-nothing, so new messages in an existing thread are never picked up. Rejected for the loop; still the right way to bring personas and memories over once.
- **Merge both sides into one thread by timestamp.** Produces a transcript neither persona saw: each side's replies were written without the other side's turns. Rejected in favour of forking (R4).
- **Last writer wins / hosted side wins.** Loses what someone said. Rejected (R3).
- **Two-way sync in v1.** Writing local turns to the hosted thread needs a write that records an existing exchange without running the agent, which the webhook surface does not have (`user` mode runs a turn; `assistant` mode posts only a reply). It also turns every conflict into a two-sided one. Deferred.
- **Stack on PR #148 now.** It is 346 commits behind `main` with conflicts in ten files. Making the sync wait for it, or rebasing someone's draft as a side effect, is the wrong trade for an experiment. The fork semantics are the same; only the lineage display waits.
- **A new hosted scope (`sync:read`) first.** It would let the sync also bring persona prompts, memories and attachments, and an ascending "since" read would make polling cheaper. It needs a hosted release and a decision about serving private working state to a static token. Deferred so v1 works today (R2), and listed below.
- **Outbound webhooks from the hosted app instead of polling.** Lower latency, but a home server is often not reachable from the internet. Polling is the only thing that works behind NAT. Deferred.

## Consequences

- A self-hosted install can keep a live copy of a hosted account's conversations with one token. Nothing changes on the hosted side.
- A conversation continued in both places becomes two threads. That is the intended result, but it can surprise people until the branch UI from #148 shows the relationship.
- Local copies of hosted threads take local model calls on first use (rehydration summary and memory seeding), metered against the local keys.
- Three new tables and one new plugin. Removing the feature is deleting the plugin and dropping the tables; mirrored threads remain ordinary imported threads.

## Follow-ups

- **#148 lineage:** set `forked_from_*` on sync forks so the branch banner and markers show them.
- **`sync:read` scope (hosted):** persona prompts and settings, memories, attachment downloads, chats `updated_since`, an ascending messages-since cursor, and deletion tombstones, behind its own scope and a clear "full copy of your account" warning on the token page.
- **Two-way sync:** an idempotent "record an exchange" write on the hosted side, then push local tails of tracked threads that have not forked.
- **Hosted-side helper:** a "Sync to a self-hosted app" preset on the Webhooks tab that creates a correctly scoped token in one click.
- **Thread list badge** for mirrored and forked threads (the link table already knows which are which).
- **Archive and favourite state** are copied when a mirror is created, not afterwards.

## Test plan

- **Unit (`internal/remotesync`):** the client's error classification and paging; discovery stopping at the first unchanged page; the pull overlap and dedupe; the planner's create / adopt / append / fork / detach decisions, including a second fork; persona matching; caps and the skip for `needs_attention`.
- **Datastore:** apply in one transaction (a failure leaves nothing), fork copy up to the head with links copied, adoption alignment, the lease claim under contention, ownership on every query, cascade on account deletion.
- **Handlers:** validation outcomes on save, token never returned, 404 when the plugin is off.
- **End to end (manual for now):** one local server syncing user B from user A on the same server over `http://localhost` with `REMOTE_SYNC_ALLOW_INSECURE_HTTP=true`: first sync, incremental append, local reply then remote reply (fork), local delete (detach), run now.
