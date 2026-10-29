# Package: `internal/relaykit`

## Role

The machinery the chat relays share: the Discord relay (ADR 0x025), the Buzz relay (ADR 0x028) and the Slack relay (ADR 0x032).
How they pace turns (batching, the agent loop guard) is ADR 0x031.
Each relay keeps its own storage, wire format and trigger rules; what is the same for both lives here, so it is written and fixed once.

## Responsibilities

- **`TurnQueue`:** one inbound turn at a time per relay thread, batching what arrives meanwhile.
  An idle thread's first message runs at once, alone.
  Messages that arrive while a turn runs wait in one batch per place (`Place`: where the reply goes), held for a short gather window, and each batch runs as one turn.
  At most `Max` messages wait per thread (`DefaultBatchMax`, 8); `Enqueue` refuses the rest.
  A thread's worker starts on first use and retires once idle; `Active` tells whether a relay turn is running in a thread.
- **`LoopGuard`, `Policy`, `BatchPrompt`:** the agent-to-agent loop guard and the batch prompt.
  `Depth` gives a message its agent depth from the binding's previous one (0 for a person), `Allows` caps it (`DefaultAgentMaxDepth`, 3), `Hint` is the wrap-up note near the cap, and `TakeAgentTurn` rate-limits agent-started turns per binding (`DefaultAgentTurnsPerHour`, 20).
  `Batch` runs all of that over a batch, oldest first, so both relays apply it the same way.
  `PolicyFromEnv` reads `RELAY_BATCH_MAX`, `RELAY_AGENT_MAX_DEPTH` and `RELAY_AGENT_TURNS_PER_HOUR`.
  `BatchPrompt` joins a batch's relayed lines under a note that they get one reply.
- **`Clock`:** the time source of the queue and the guard (`RealClock`), so tests use a fake one.
- **`Replies`:** matches finished replies (the reply hook) with the inbound turns waiting for them.
  It remembers a release that came before its waiter, and keeps a reply whose inbound link is not attached yet (an orphan) until the relay can post it.
- **`Throttle`:** at most one action per key per interval (the "busy" notice).
- **`Supervisor[T]`:** one long-lived connection per target while holding a leader lock; it re-lists targets, reconnects on a changed fingerprint, and closes everything and releases the lock when it stops leading.
  `Kick` makes the leader reconcile now (after the owner bound a channel, say).
  `AdvisoryLocker` (the datastore's Postgres advisory lock, a key per relay) and `SingleInstanceLocker`.
- **`ThreadOpen`, `Gate`:** the fail-closed check that a bound thread may be driven from outside (sandboxed, or acknowledged by the owner), keeping the binding's record honest (deleted thread, stale acknowledgement, paused warning).
- **`LoadReplyFiles`, `SkippedNote`:** the files a reply produced, within a platform's limits, never the context-only ones, and the note naming those left out.
- **`SplitText`, `TablesToCodeBlocks`, `IsFenceLine`:** a reply split into messages of a platform's length (Discord 2000, Slack 4000) on paragraphs, then lines, with a split code block closed and reopened, and Markdown tables turned into code blocks.
- **`PublicOnlyControl`, `IsPublicIP`:** the `net.Dialer` hook that refuses non-public addresses after DNS on every connection.
- **`SpeakerLabel`, `Allowed`, `SleepCtx`:** speaker names cleaned so they cannot forge the `name (Platform, #channel):` label; allow and deny lists where deny wins.

## Key types and entry points

- `TurnQueue`, `LoopGuard`, `LastInbound`, `Policy`, `PolicyFromEnv`, `BatchPrompt`, `Clock`, `Replies`, `Throttle`, `Supervisor`, `Lock`, `Locker`, `Conn`, `AdvisoryLocker`, `SingleInstanceLocker`, `PublicOnlyControl`, `IsPublicIP`, `SpeakerLabel`, `Allowed`, `SleepCtx`, `LoadReplyFiles`, `SkippedNote`, `SplitText`, `TablesToCodeBlocks`, `IsFenceLine`.

## Dependencies

- **Inbound:** `internal/discordrelay` (and its `gateway`), `internal/buzzrelay`, `internal/buzzplugin`, `internal/slackrelay`, `internal/slackplugin`.
- **Outbound:** `internal/datastore` (`TryAcquireSchedulerLeaderLock`), `internal/replyhook` (`Event`), `zap`.

## Non-obvious decisions

- **Generic, not interface-based.**
  The supervisor and queue take the relay's own types (`Supervisor[gateway.Bot]`, `Supervisor[models.SlackAppCredentials]`, `TurnQueue[inboundWork]`), so each relay keeps its existing structs and tests.
- **Enqueue is race-free:** an item is added under the lock the idle worker takes before retiring, so it can never land in a thread whose worker just exited.
- **Overflow is dropped, not rolled over:** past `Max` the relay posts its throttled busy notice (never to an agent), which keeps memory bounded and the channel quiet.
- **The loop guard keeps no depth of its own:** the relays persist each message's depth on its inbound link and hand the newest back (`LastInbound`), so a restart does not reset a loop.
  In memory are only the hourly rate limit and when each binding last dropped an agent (`effective`), which keeps the chain closed while a dropped agent keeps posting, since dropped messages are not recorded.
  `Closed` lets a relay drop an agent past the cap before queueing it; `Saturated` stands in for a previous message that could not be read, so a failed read drops agents rather than restarting a chain.
- **An agents-only batch counts as its deepest message** (`Batch`'s `turnDepth`), so the wrap-up hint counts from where the chain will stand after the turn.

## Testing

- `relaykit_test.go` covers serialisation and the queue bound, batching by place and the gather window (fake clock), the loop guard's depths, hints, batches and rate limit, the batch prompt, the env policy, early releases and orphans, the throttle, labels and lists, the address guard, reconciling on fingerprints, and the splitter's limit (Discord's `format_test.go` covers its rules).
