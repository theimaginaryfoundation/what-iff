# Package: `internal/remotesync`

## Role

Remote sync's logic: keeps a copy of a user's threads from another What Iff server (usually the hosted app) in this one, pulled through the remote's webhook read routes with a `chat:read` token (ADR 0x032).
It talks to the remote through a small `Remote` interface and to storage through `Store`, so it is tested without either.

## Responsibilities

- **Client (`client.go`):** the remote's `GET /api/webhooks/chat`, `/chat/{id}/messages` and `/personality` (the token check), with the stored token.
  `NormalizeBaseURL` accepts https only (and maps the hosted app's web addresses to `DefaultBaseURL`); `NewHTTPClient` connects to public addresses only unless local networks are allowed; redirects are never followed.
  Failures are classified (`ErrorKind`), and `UserMessage` turns a kind into what the settings page says.
- **Syncer (`syncer.go`):** one account's run.
  `discover` walks the remote thread list (newest activity first, active then archived) and stops at the first page with nothing changed, unless the account needs a full walk.
  `pull` reads a thread's messages newest first back to its head less an overlap (the whole history for a new link) and returns them oldest first.
  Each thread is applied through `Store.ApplyRemoteSyncThread`, which creates, adopts, appends or forks.
- **Runs:** `Wake` runs the account if its last run is older than `WakeMinAge` (two minutes): the web app sends one when a session starts or comes back, which is how copies stay current without polling.
  Every wake and every run also calls `Store.RematchRemoteSyncPersonas`, so copies whose persona arrived later (created here, or imported) take it without a remote call.
  `Start` claims the lease and runs regardless of age ("sync now" and saves); `RunDue` runs every account due on the background interval; `Runner` calls it on a timer.
  A run records its outcome with `FinishRemoteSyncRun`, which also releases the lease.

## Key types and entry points

- `Syncer`, `NewSyncer`, `Config`, `Runner`, `Store`, `Remote`.
- `Client`, `NewClient`, `NewHTTPClient`, `NormalizeBaseURL`, `DefaultBaseURL`, `PageSize`.
- `Error`, `ErrorKind`, `KindOf`, `NeedsOwner`, `UserMessage`, `ErrBusy`, `ErrDisabled`.

## Dependencies

- **Inbound:** `internal/remotesyncplugin` builds the `Syncer` and `Runner` and serves the routes.
- **Outbound:** `internal/models`, `internal/netguard`, `zap`.

## Non-obvious decisions

- **Pull-only and append-only.**
  Nothing is ever written to the remote, and nothing local is edited or deleted; divergence becomes a fork (in the datastore).
- **Change detection uses the remote's `last_message_time` as an opaque value.**
  A thread is changed when the value differs from the one stored on its link, not when it is later: the agent can set it slightly before the reply's own `sent_at`.
- **A full walk after an incomplete run.**
  The early stop assumes everything past an unchanged page is unchanged, which is false for threads never synced; so the walk goes to the end until a run succeeds without deferring threads (`RemoteSyncAccount.FullWalk`).
- **The overlap is free.**
  A pull re-reads two minutes before its head to catch a message that committed late with an earlier timestamp; the datastore drops the ones already linked.
- **401 and 403 stop the account** (`needs_attention`) until its settings are saved again; network errors, 429 and 5xx are retried next run, and one failing thread does not stop the others.
  A thread deleted remotely between the list and the read is skipped, not failed.
- **Detached threads are seen, not read:** a link whose local thread was deleted records the remote's `last_message_time` without fetching messages, so it stops counting as changed.
- **Leases, not leader locks.** Any instance may run any account it claims; the lease is renewed after each thread and a cancelled run lets it expire.

## Testing

- `client_test.go` — address normalization, request shape, error classification (including a refused redirect) and the public-address default.
- `syncer_test.go` — against a fake remote and a recording store: first and later runs, the overlap, early stop, the per-run cap and the full walk after it, detached threads, a refused token, per-thread failures, `Start`.
