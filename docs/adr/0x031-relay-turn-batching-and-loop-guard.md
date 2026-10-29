# ADR 0x031: Relay turns — batching bursts, bounding agent loops, keeping channel names fresh

- **Status:** Proposed
- **Branch:** `feat/relay-queue-batch` (stacked on `feat/buzz-relay`)
- **Date:** 2026-10-24
- **Deciders:** What Iff maintainers
- **Builds on:** [ADR 0x025](0x025-discord-relay.md) (Discord relay), [ADR 0x028](0x028-buzz-relay.md) (Buzz relay) and the shared runtime `internal/relaykit`

## Context

Both relays ran one turn at a time per relay thread, with up to 5 tags queued behind it, and each queued tag became its own turn and its own reply.
A persona tagged ten times in a minute therefore answered ten times: it spammed the channel back and spent the owner's credits on every ping.

Agents could not talk to each other in any useful way.
Discord ignored every bot.
Buzz ignored agents that carry an owner attestation unless the binding set `answer_agents`, but a persona joined by invite has no attestation, so two What Iff personas in one Buzz channel could already ping-pong without limit.
ADR 0x025 and ADR 0x028 both left "agent-to-agent conversation and its loop guards" as a follow-up.

A channel renamed in Discord kept its old name in What Iff: the binding stored the name once, at binding time, and the app and the relayed speaker labels (`alice (Discord, #old-name): …`) kept showing it.

The owner's direction for this change:

- The first ping starts a turn at once.
  The second waits; anything that arrives while it waits joins it, up to a cap of about 5 to 10, and the persona answers the batch in one reply, choosing what is worth answering.
- Agents may answer each other, bounded: about 3 consecutive agent exchanges, a rate limit, and a wrap-up hint near the cap.
  The batching damps loops already; the cap is the backstop.
  The depth must survive a restart, so it lives in data we already persist.
- Channel renames may be picked up lazily, on the next message.

## Decision

### 1. One turn per batch (`relaykit.TurnQueue`)

- The first message for an idle relay thread starts a turn at once, on its own.
- Messages that arrive while a turn runs wait in batches, one batch per place, where a place is where the reply will go:
  - Discord: the binding plus the Discord channel or thread the message is in;
  - Buzz: the binding plus the NIP-10 thread root, with the channel's top-level messages sharing one place.
  A batch never mixes places, because its one reply is posted to one place.
- When the running turn ends, the oldest waiting batch runs as one turn.
  Messages that arrive while it runs form the next batch.
- A waiting batch is held for a short gather window (2 s from its first message) before it runs.
  This only matters when the previous turn was quicker than the burst; the first message of an idle thread never waits.
- At most `RELAY_BATCH_MAX` messages (default **8**) wait per relay thread, across all its places.
  The default sits in the owner's 5–10 range: enough for a busy channel's burst, small enough that the batch prompt stays short.

### 2. Past the cap: dropped, with the existing throttled notice

A message that arrives while the cap is reached is dropped, and the existing "I'm getting a lot of messages at once…" notice is posted at most once a minute per binding.
We chose this over rolling the overflow into a following batch:

- rolling over keeps an unbounded queue, or a second cap that is the same choice one batch later;
- a spammer's later pings are the least valuable, and the notice tells real people to tag again;
- it is the behaviour the relays already had, so nothing new reaches the channel.

The notice is never sent to an agent (a bot in Discord, an agent in Buzz): an agent that answers the notice would feed the flood.
The same holds for the "couldn't reply" failure notice.

### 3. What the batched turn sees, and where its reply goes

- The turn's user message is the batch's relayed lines, oldest first, each with its speaker label as before (`bob (Discord, #general): …`).
  A batch of several opens with a relay note: the messages arrived together; write one reply; answer several at once or only those worth answering; it is posted once, as a reply to the last message, so address anyone else by name (`relaykit.BatchPrompt`).
- Every message of the batch gets its own inbound message link, all attached to the turn's one saved message.
  `FindInbound…Link` returns the newest of them, so the reply goes where the newest message is:
  - Discord: a reply to the newest message, which pings only its author (`allowed_mentions` is unchanged, ADR 0x025 §4);
  - Buzz: into the newest message's thread, p-tagging only its author.
- The other askers are named in the text, not pinged.
  Pinging every author of a burst would amplify it, and the persona may have chosen not to answer some of them.
- Files share one budget per turn (the per-message limit, 4), not one per message, so a burst of uploads cannot multiply it.
- Memory provenance names everyone the turn answered ("alice, bob"), from the batch's links, instead of only the first author.

### 4. Agents may answer each other, behind a loop guard (`relaykit.LoopGuard`)

- Discord bindings get `answer_agents` (default off), like Buzz bindings already had.
  With it on, a message from another bot (`author.bot`) can start a turn; webhooks and the persona's own bot never do.
- Buzz now also treats an author whose profile says `bot: true` (NIP-24), or who is another persona's identity on this server (ADR 0x028 §5), as an agent, not only one whose event carries an owner attestation.
  What Iff personas publish that flag, so a persona joined by invite is recognised as an agent.
- Every inbound message link records an **agent depth** (`agent_depth` on `discord_message_links` and `buzz_message_links`):
  0 when a person wrote it; for an agent, one more than the binding's newest inbound message, or 1 when that one is older than 30 minutes.
  A person tagging the persona resets the chain.
  Because the depth is on the link rows, a restart or a leader handover keeps it.
- The chain is the bound channel, not a thread.
  An agent cannot escape the count by starting a new thread or a top-level message, and anyone tagging the persona there resets it.
- A message deeper than `RELAY_AGENT_MAX_DEPTH` (default **3**) is not answered and not recorded; nothing is posted, since an agent would answer a refusal.
- Near the cap the turn carries a relay note:
  at depth `max − 1` "start wrapping up; you can reply N more time", at depth `max` "this is your last reply in this exchange; do not ask a question or invite another reply".
- A batch's depth is its shallowest message's: a person in the batch makes it a person's turn.
- Backstop: at most `RELAY_AGENT_TURNS_PER_HOUR` (default **20**) agent-started turns per binding per hour.
  This counter is in memory, so a restart resets it; the persisted depth cap is what bounds a loop.

### 5. Channel names stay fresh

- **Discord:** the gateway already keeps discordgo's state cache (the Guilds intent), which follows `CHANNEL_UPDATE`.
  - Each relayed message carries the bound channel's current name from that cache (the parent's, for a message in a Discord thread); when it differs from the binding's, the binding is renamed before the turn is built.
  - The gateway also handles `CHANNEL_UPDATE` and renames the bot's bindings of that channel right away, so the app shows it without waiting for a tag.
    The rename is one `UPDATE … WHERE channel_name <> new`, off the gateway's event goroutine.
- **Buzz:** channel names come from relay-signed kind 39000 metadata, and they had the same staleness.
  When a mention is answered, the relay reads the channel's metadata (at most once per 10 minutes per binding), trusts it only when the relay signed it, and renames the binding when it changed.
  A live subscription to 39000 was not worth it: the session's `since` bound would skip renames made while disconnected, which the lazy read covers anyway.
- The relay thread's own title ("Discord · #general") is left alone: the owner may have renamed it.

### 6. Configuration

`RELAY_BATCH_MAX`, `RELAY_AGENT_MAX_DEPTH` and `RELAY_AGENT_TURNS_PER_HOUR` apply to both relays (`relaykit.PolicyFromEnv`).
They are positive integers; anything else keeps the default and is logged.
They are deployment settings, not per binding, until a binding needs its own.

## Alternatives considered

- **Answer every queued message, one turn each (the status quo).** Rejected: it is the spam-back and the credit burn this change exists to stop.
- **Debounce every message, including the first.** Rejected: the owner wanted the first ping answered at once; the common case is a single tag.
- **One batch per relay thread, whatever the place.** Rejected: one reply cannot land in two Discord threads or two Buzz threads; the people in the other one would never see it.
- **Roll overflow into the next batch.** Rejected (§2).
- **Ping every author of a batch.** Rejected (§3): it amplifies a burst; the persona names whom it answers.
- **Count depth per Discord thread or Buzz thread, or follow reply references exactly.** Rejected: an agent could reset the count by posting top-level, and following references needs JSON lookups on every message. The channel-wide chain is simpler and errs towards stopping.
- **Keep the depth in memory only.** Rejected by the owner: a restart would reset a running loop.
- **A separate counter table, or a column on the binding.** Rejected: the inbound link is already written for every relayed message and is where provenance lives; the newest one within the chain gap is one indexed query (`created_at`, binding).
- **Post a notice to an agent past the cap.** Rejected: the agent would answer it.

## Consequences

- A burst of tags costs two turns (the first, then the batch) instead of one per tag, and the channel gets two replies.
- A batch reply reads differently: it may address several people at once and skip some messages on purpose.
- Two What Iff personas, or a persona and another bot, can hold a short exchange in a bound channel when the owner allows it, and it ends by itself after about three replies from each side unless a person joins in.
- An agent past the cap is ignored silently, which can look like the persona stopped working to the agent's owner; the binding's logs say why.
- Discord bindings carry a new column (`answer_agents`, default false), and both link tables carry `agent_depth` (default 0) with an index on (`created_at`, binding).
  Ent's auto-migration adds them; existing rows read as people.
- The Discord gateway handles one more event type; nothing about its intents changes.

## Not done yet

- Per-binding limits (batch size, depth, rate) in the app, if the deployment defaults turn out not to fit every channel.
- Showing on a relayed message in the app that it was answered as part of a batch, or that a message was dropped by the loop guard.
- Telling the owner (on the binding) when the loop guard or the rate limit is dropping messages, instead of only logging it.
- Renaming the relay thread when its channel is renamed, as an opt-in.
- Buzz renames caught live (a metadata subscription with its own `since` handling), if the lazy read proves too slow.
