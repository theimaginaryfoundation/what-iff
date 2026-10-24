# Package: `internal/relaykit`

## Role

The machinery the chat relays share: the Discord relay (ADR 0x025) and the Buzz relay (ADR 0x028).
Each relay keeps its own storage, wire format and trigger rules; what is the same for both lives here, so it is written and fixed once.

## Responsibilities

- **`TurnQueue`:** one inbound turn at a time per relay thread, with a bounded queue behind it.
  A thread's worker starts on first use and retires once idle; `Active` tells whether a relay turn is running in a thread.
- **`Replies`:** matches finished replies (the reply hook) with the inbound turns waiting for them.
  It remembers a release that came before its waiter, and keeps a reply whose inbound link is not attached yet (an orphan) until the relay can post it.
- **`Throttle`:** at most one action per key per interval (the "busy" notice).
- **`Supervisor[T]`:** one long-lived connection per target while holding a leader lock; it re-lists targets, reconnects on a changed fingerprint, and closes everything and releases the lock when it stops leading.
  `Kick` makes the leader reconcile now (after the owner bound a channel, say).
  `AdvisoryLocker` (the datastore's Postgres advisory lock, a key per relay) and `SingleInstanceLocker`.
- **`ThreadOpen`, `Gate`:** the fail-closed check that a bound thread may be driven from outside (sandboxed, or acknowledged by the owner), keeping the binding's record honest (deleted thread, stale acknowledgement, paused warning).
- **`LoadReplyFiles`:** the files a reply produced, within a platform's limits, never the context-only ones.
- **`PublicOnlyControl`, `IsPublicIP`:** the `net.Dialer` hook that refuses non-public addresses after DNS on every connection.
- **`SpeakerLabel`, `Allowed`, `SleepCtx`:** speaker names cleaned so they cannot forge the `name (Platform, #channel):` label; allow and deny lists where deny wins.

## Key types and entry points

- `TurnQueue`, `Replies`, `Throttle`, `Supervisor`, `Lock`, `Locker`, `Conn`, `AdvisoryLocker`, `SingleInstanceLocker`, `PublicOnlyControl`, `IsPublicIP`, `SpeakerLabel`, `Allowed`, `SleepCtx`.

## Dependencies

- **Inbound:** `internal/discordrelay` (and its `gateway`), `internal/buzzrelay`, `internal/buzzplugin`.
- **Outbound:** `internal/datastore` (`TryAcquireSchedulerLeaderLock`), `internal/replyhook` (`Event`), `zap`.

## Non-obvious decisions

- **Generic, not interface-based.**
  The supervisor and queue take the relay's own types (`Supervisor[gateway.Bot]`, `TurnQueue[inboundWork]`), so each relay keeps its existing structs and tests.
- **Enqueue is race-free:** an item is sent under the lock the idle worker takes before retiring, so it can never land in a queue whose worker just exited.

## Testing

- `relaykit_test.go` covers serialisation and the queue bound, early releases and orphans, the throttle, labels and lists, the address guard, and reconciling on fingerprints.
