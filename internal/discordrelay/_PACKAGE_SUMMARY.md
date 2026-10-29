# Package: `internal/discordrelay`

## Role

The Discord relay's logic: turns tags in a bound Discord channel into turns in its relay thread, and posts finished replies back out (ADR 0x025).
It talks to Discord through a small `Discord` interface (REST via discordgo), and to storage through `Store`, so it is tested without either.

## Responsibilities

- **Inbound (`Service.HandleInbound`):** decides whether a message tags the bot (`Triggered`: an @mention, or a reply to one of its messages; webhooks and the bot itself never), answers other bots only when the binding has `answer_agents`, applies the binding's allow and deny lists (`Allowed`), and re-checks the bound thread before every tag (`threadOpen`).
  The first tag of an idle relay thread starts a turn at once; tags that arrive while it runs wait as one batch per Discord channel or thread (`relaykit.TurnQueue`, `Policy.BatchMax`) and are answered in one turn (ADR 0x031).
  Each tag is recorded once (`RecordInboundDiscordMessage`, the dedupe key) with its agent depth, and the turn starts through `plugins.TurnStarter` with the text `name (Discord, #channel): text` (`PromptText`), a batch under a relay note (`relaykit.BatchPrompt`).
- **Batched turns (`inbound.go`):** `runInbound` re-checks the binding, records each tag, builds the prompt and waits for the reply.
- **Loop guard (`guardBatch`):** gives each tag its agent depth from the binding's newest inbound link (`LastInboundDiscordLink`), drops bot tags past the cap, adds the wrap-up note near it, and rate-limits bot-started turns (`relaykit.LoopGuard`).
  A bot tag past the cap is dropped before it is queued (`LoopGuard.Closed`), and a failed read of the previous link counts as the chain at its cap (`lastInbound`).
- **Channel renames:** a tag carries the bound channel's current name from the gateway's cache (`InboundMessage.BindingChannelName`), and `ChannelRenamed` (called by the gateway on `CHANNEL_UPDATE`, too) records it on the binding (`RenameDiscordChannel`).
- **Attachments in (`attachments_in.go`):** fetches files posted with a tag (a batched turn shares one per-message budget) from Discord's CDN only (`CDNFetcher`: HTTPS, an allow-listed host, public addresses checked after DNS on every connection) and saves them through `plugins.AttachmentIngester`.
- **Outbound (`Service.OnReply`, the reply hook):** posts a reply back to where the tag came from, and to any binding a composer toggle or `post_to_discord` armed (`ConsumeDiscordPendingPosts`); at most once per message and binding, with retries for transient errors.
- **Formatting (`SplitForDiscord`):** 2000-character parts on paragraph and line boundaries, never splitting a code block without closing and reopening it; Markdown tables become code blocks.
  The splitter is `relaykit.SplitText`, shared with the Slack relay.
- **New relay thread defaults:** a server-created relay thread is a sandbox (`models.ContextScopeSandbox`), and the sandbox brings its own defaults (`models.SandboxDefaultDisabledTools`, applied by `datastore.CreateChat`); the relay keeps no list of its own.
  `RegisterRelayThreadDisabledTool` forwards to `models.RegisterSandboxDefaultDisabledTool` for builds that already call it from an `init()`.
- **Metering:** the relay's turns carry `Source: "discord"` (`TurnSource`, `plugins.UserTurn.Source`), so a metering implementation can keep them out of the owner's own chat allowance.

## Key types and entry points

- `Service`, `InboundMessage`, `Triggered`, `Allowed`, `PromptText`, `SplitForDiscord`.
- `Discord` and `REST` (`NewREST`), `InviteURL`, `ErrInvalidToken`, `ErrNoAccess`.
- `RegisterRelayThreadDisabledTool` (forwarding), `TurnSource`, `ThreadDeletedError`.
- Subpackage `gateway`: the Gateway connections (see its summary).

## Dependencies

- **Inbound:** `internal/discordplugin` builds the `Service` and wires it to the gateway and the reply hook.
- **Outbound:** `internal/models`, `internal/plugins` (turn starter, attachment ingester), `internal/replyhook` (`Event`), `internal/relaykit` (turn queue, reply bookkeeping, busy throttle, speaker labels, allow lists, public-address guard), `internal/agent/tools` (tool names for the defaults), `github.com/bwmarrin/discordgo`, `zap`.

## Non-obvious decisions

- **Fail closed on the thread.**
  The bound thread's current sandbox flag is read on every tag and again when a queued tag is run (then against the binding as it is now: a tag for a binding repointed meanwhile is dropped).
  A thread that is not sandboxed and has no owner acknowledgement (`RelayThreadOpenWithoutAcknowledgement`) drops tags and records `UnrestrictedWarning` on the binding (nothing is posted to Discord); a sandboxed thread withdraws a stale acknowledgement.
- **No owner details in relay turns:** turns start without the owner's time zone, and display names are cleaned (`speakerLabel`) so a nickname cannot forge the `name (Discord, #channel):` label.
- **A reply can beat its inbound link.**
  The turn starter returns after the turn starts, so the link is attached late; a reply that finds no link while a relay turn runs in its thread is kept (`keepOrphan`) and posted once the link is attached.
  A waiting tag also stops waiting when its job completes, so the queue never stalls on a lost hook.
- **One reply per batch.**
  Every tag of a batch gets its own inbound link, all attached to the turn's one saved message (newest first, so a reply finishing mid-attach finds the newest); the reply goes to the newest tag (`FindInboundDiscordLink` returns it) and pings only its author.
- **Nothing is posted to a bot:** no busy or failure notice, and nothing past the loop guard's cap, since a bot would answer it.
- **Queues are race-free:** a tag is enqueued under the lock the idle worker takes before retiring (`relaykit.TurnQueue`), and tuning defaults are set once.
  The queue, the reply bookkeeping (`relaykit.Replies`) and the busy throttle are shared with the Buzz relay; the thin unexported wrappers (`enqueue`, `setWaiter`, `release`, `keepOrphan`, `takeOrphan`) keep this package's tests and call sites unchanged.
- **No pings.** Every post sets `allowed_mentions` to parse nothing except the replied-to user.
- **Neutral notices.** A failed turn or a full queue posts `NoticeFailed` or `NoticeBusy`, which reveal nothing about the owner's account.
- **A reply can finish before its waiter registers** (the turn starter returns after the job starts), so `release` remembers early replies for the turn timeout.

## Testing

- `service_test.go` drives the whole flow against fake `Store`, `Discord` and turn starter; `format_test.go`, `trigger_test.go`, `attachments_in_test.go` (including the address checks) and `thread_defaults_test.go` cover the pure parts.
