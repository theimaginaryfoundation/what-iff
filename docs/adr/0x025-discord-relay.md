# ADR 0x025: Discord relay — your persona as its own Discord bot

- **Status:** Accepted (expected to change once it is in use)
- **Date:** 2026-10-05
- **Deciders:** What Iff maintainers
- **Builds on:** sandboxed threads (`Chat.sandboxed`, `Chat.IsSandboxed()`), the plugin seam (`internal/plugins`) and the reply hook (`internal/replyhook`)

## Context

People want to talk to a persona from Discord: tag `@Vix` in a channel and get an answer from Vix, and ask Vix in the app to post something to a channel.

Discord has no MCP server and no outgoing webhooks: nothing calls us when a keyword or mention appears.
Hearing a channel needs a bot holding a Gateway (websocket) connection.

A Discord channel is also a public surface.
Whoever is allowed to tag the bot drives a conversation that runs on the owner's account, can draw on whatever that conversation is allowed to read, and has its replies posted where other people can read them.
So the relay must not, by default, hand strangers the owner's memories, other threads, scratchpad or tools, and what the persona learns from strangers must not be mistaken for what the owner said.

## Decision

### 1. Bring your own bot: the bot *is* the persona

- The user creates a Discord application, adds a bot, and pastes the bot token into the Integrations → Discord tab, attaching it to a persona. One bot per persona; a bot can be connected by one account only.
- On paste, the server validates the token with Discord (`GET /users/@me` for the bot user, `GET /oauth2/applications/@me` for the application id and whether the Message Content intent is on).
- The token is stored encrypted with the same AES-GCM helper as MCP tokens (`TOKEN_ENCRYPTION_SECRET`); without that secret no bot can be added. The token is never returned by the API.
- The invite link is built from the application id with View Channel, Send Messages (and in threads), Embed Links, Attach Files, Read Message History and Add Reactions.
- "Use persona's name and portrait" sets the bot's username and avatar from the persona (`PATCH /users/@me`). Discord allows about two username changes an hour, so it is an explicit action.
- Real @mentions need no privileged intent: Discord sends a bot the content of any message that mentions it. Message Content is optional and only needed for future triggers.

### 2. Gateway connections run in the API server, behind a leader lock

There is one Gateway connection per bot, and with more than one API instance exactly one must hold each bot's connection, or every instance would answer every mention.

- The supervisor (`internal/discordrelay/gateway`) leads through a Postgres advisory lock on its own key, `80920032` (the agent-job scheduler uses `80920031`), overridable with `DISCORD_GATEWAY_LOCK_KEY`. Followers retry about every 15 s; the leader re-reads the active bots about every 30 s and opens, reopens (new token or intents) or closes connections to match.
- On shutdown the leader closes its connections and releases the lock, so another instance takes over within one retry. A brief overlap is harmless: inbound messages are deduplicated on the Discord message id (§3).
- `DISCORD_GATEWAY_SINGLE_INSTANCE=true` skips the lock for one-process local runs (and databases without advisory locks).
- `Run(ctx, locker, source, sink)` does not depend on running inside the API server, so moving the gateway into its own process later is a deploy change, not a rewrite.

### 3. Data

- **`discord_bot`**: owner, persona (`personality_id`), `application_id`, `bot_user_id`, `bot_username`, encrypted `token`, `message_content`, `status` (active, invalid_token, disabled), `last_error`.
- **`discord_binding`**: a channel bound to a relay thread: bot, `chat_id`, guild and channel ids and cached names, `inbound_enabled`, `allow_user_ids`, `deny_user_ids`, `allow_unrestricted` (§6), `status` (active, broken), `last_error`, `last_activity_at`. Unique on (bot, channel). A thread can have several bindings.
- **`discord_message_link`**: one row per message that crossed the boundary: the What Iff message, the binding, `direction`, the Discord message ids, and for inbound messages the author id and display name. Unique on (binding, inbound Discord message id): the dedupe key for retries and leader handover. It is also the inbound message's provenance (who said it), and the outbound delivery record.
- **`discord_pending_post`**: "post the next reply in this thread to this binding", set by the composer toggle or the `post_to_discord` tool and consumed by the reply hook. It expires after 30 minutes.

Persona and thread are plain UUID columns, so the `Personality` and `Chat` schemas stay unaware of the relay; a deleted persona or thread shows up as a broken binding.

### 4. Output mirroring

The agent writes one reply, saved as the What Iff message as usual; the relay then also delivers it to Discord.
Nothing writes a separate "Discord version" of a reply.

| Turn | Goes to Discord |
|---|---|
| Triggered from Discord | Always, back to the channel (or Discord thread) it came from, as a reply to the triggering message |
| Typed in the app, in a relay thread | Only when the composer toggle "Post the next reply to #channel" is on (off by default, not sticky) |
| Any thread of a persona with a bot | Only when the agent calls `post_to_discord` |

- `post_to_discord(channel)` takes no text: it marks the reply being written, and the finished reply is posted as written. It is offered only when the persona's bot has an active binding, and its channels are listed in the agent's instructions, so the model cannot invent a destination. A sandboxed thread may post only to its own channel and never learns the owner's other channels.
- Posts go out as the bot through the REST API with `allowed_mentions: {"parse": [], "replied_user": true}`: model output can never ping `@everyone`, roles or users.
- Replies are split at 2000 characters on paragraph, then line boundaries, never inside a code block; Markdown tables become code blocks. Files saved with the reply are uploaded within Discord's limits, and anything left out is named in a note.
- Only files the reply produced are saved with it. A file a tool merely fetched for the model to look at (`find_context`'s fetch mode) is marked context-only and is not saved onto the reply, so the owner's existing files are never re-published to a channel.
- Delivery is asynchronous through the reply hook, never delays or fails the turn, retries transient errors, and is idempotent on the link row. A rejected token marks the bot `invalid_token`; a channel the bot can no longer post to marks the binding `broken`.

### 5. Inbound

- In a bound channel with `inbound_enabled`, the persona answers an @mention of the bot, or a Discord reply to one of its messages. Bots and webhooks are always ignored, including the bot itself.
- The message is saved as a user message in the relay thread as `alice (Discord, #general): text`. A reply to someone other than the bot quotes the replied-to message first. Files posted with it are fetched from Discord's CDN only (HTTPS, public addresses only, at most 4 files of 20 MB) and attached through the app's own upload path.
- The turn runs through the plugin turn starter (`plugins.Deps.Turns`, the same path as the app and the webhook user mode), metered against the bot's owner.
- One turn at a time per relay thread; up to 5 tags queue behind it, and an overflow is told so in the channel (at most once a minute per binding). While a turn runs the bot shows "typing…". A failed turn posts a neutral notice that reveals nothing about the owner's account.
- Relay turns run without the owner's time zone, so their timestamps do not reveal it. Display names are cleaned of the characters that make up the `name (Discord, #channel):` label, so a nickname cannot pose as another speaker.

### 6. Who may tag, and what a relay thread can reach

- Each binding has an allow list and a deny list of Discord user ids. An empty allow list means anyone in the channel; deny always wins.
- **A new relay thread is sandboxed and quiet by default.** When a binding is made without an existing thread, the server creates one with:
  - `sandboxed` = true: it reads only itself. It sees no memory but those created in it (and its own checkpoint summary), no other conversation, no scratchpad, no owner name, no account-wide files, jobs, skills or personas, and only the files uploaded to it; and everything it learns is saved Chat-scoped, so it never reaches the owner's account;
  - `disabled_tools` = `create_agent_job`, `run_subagent`, `update_scratchpad`, `web_search`, `fetch_page`, `generate_image`, plus any names another build registers through `discordrelay.RegisterRelayThreadDisabledTool` (the sandbox already refuses `create_agent_job` and `update_scratchpad` whatever this list says; the list is what the owner sees and can change);
  - no MCP connectors (the app's create route attaches the user's default connectors; the relay does not).
  The owner can change the tool list, or switch the sandbox off, in the thread's settings.
- **Binding an existing thread leaves its settings alone.** A thread that is not sandboxed is refused unless the request carries `allow_unrestricted`, because anyone allowed to tag the bot would then act on the owner's whole account; the acknowledgement is stored only while the thread is not sandboxed.
- **The relay re-checks on every tag.** It reads the bound thread's current sandbox flag each time, including when a queued tag's turn comes. An unacknowledged thread that is not sandboxed drops tags (silently in Discord, with a warning on the binding). When a thread is sandboxed again, a stored acknowledgement is withdrawn, so switching the sandbox off later pauses the binding instead of silently re-opening it. A tag queued for a binding that was repointed meanwhile is dropped.
- **Relay threads are walled off from each other by the sandbox itself.** A sandboxed chat reads only itself, so one server's channel is not readable from another server's relay thread, and a relay thread's external memories (§7) are not retrievable from any other chat's sandbox. The relay adds no separate rule for this.
- **`post_to_discord` in a relay thread** (any bound thread, sandboxed or not) or in any sandboxed chat reaches only that thread's own channel(s), and never learns the names of the owner's other channels. The channel list is given to the model only when the tool is available in the thread.

### 7. What is learned in a relay thread is external

A memory written from a relay thread (any thread bound to a channel, sandboxed or not: the sandbox decides what the thread reads, not who talks in it) records **provenance** `external` and, where known, the Discord **speaker** it came from (`Memory.provenance`, `Memory.source_speaker`).

- Every write path does it: `create_memory` (an optional `speaker` argument, defaulting to the author of the message being answered), checkpoint extraction (in such threads extraction uses a schema variant with a `speaker` per memory and is told that the speakers are Discord users labelled by name, not the account owner), merges and links, and the thread's checkpoint summary.
- Scope is chosen as in any chat; in a sandboxed relay thread it is always Chat, so what the persona learns stays in that thread. Provenance does not gate reads (the sandbox does); it records trust, and matters wherever an external memory can still be read: the memory manager, and the owner's chats when an unsandboxed thread was bound.
- Wherever an external memory is shown to the model (prefetch, `find_context`, summaries), it carries `source=external(unverified; said by <name> on Discord, not by the user)`. Display names are cleaned so they cannot break out of that note.
- A fold is external when any member is, and keeps a speaker only when every member names the same one. Undo restores the survivor's provenance unless the owner relabelled it since.
- The memory manager shows an **External** badge with the speaker and filters by source; the owner can set a memory back to `user` to confirm it. Export and import carry both fields.

### 8. Configuration and surfaces

- On by default; `DISCORD_RELAY_ENABLED=false` (or `0`, `off`) switches it off, and then `/api/discord` is not routed. The web app asks once whether the relay is available and hides the Integrations tab, the Jobs-tab relay threads and the composer toggle when it is not.
- Routes (authenticated): `/api/discord/bots`, `/bots/{id}` (update, remove), `/bots/{id}/sync-profile`, `/bots/{id}/guilds`, `/bots/{id}/guilds/{guildId}/channels`, `/bindings`, `/bindings/{id}`, `/chats/{chatId}`, `/chats/{chatId}/pending/{bindingId}`. Everything is owner-scoped; another account's bot, binding or thread is a 404.
- Web app: the Integrations → Discord tab; relay threads next to agent-job threads in the Thread Manager's Jobs tab (through `ThreadAutomationSource`); the composer toggle above the composer in a relay thread.

## Consequences

- A persona can live in Discord without the owner wiring anything beyond a bot token, and a stranger's tag reaches only the thread's own conversation.
- The relay adds a websocket per bot to one API instance. At larger scale, per-bot lock keys would spread bots across instances without changing the supervisor's interface.
- A sandboxed relay thread does not teach the persona anything outside itself: what it learns is Chat-scoped to that thread. That is the safe default, and it also means a persona gets no cross-thread memory of Discord. The owner can switch the sandbox off for a thread (and acknowledge the binding), after which its memories follow the ordinary rules: User-scoped external memories reach the owner's other chats with the provenance note and the External filter as the mitigation, not a wall.
- The relay thread cannot read its persona's documents or the owner's files; only what is uploaded to it. Giving a relay thread chosen reference material needs a way to bring data into a sandbox on purpose, which does not exist yet.
- `post_to_discord` is offered in the owner's ordinary chats of a persona with a bot. A prompt injection there (a fetched page, a tool result, an external memory) could ask the model to post a reply publicly. Posting is limited to the persona's bound channels, posts nothing but the reply and the files it produced, and never pings anyone; a confirmation step is a candidate follow-up.

## Not done yet

- A way to move data into or out of a sandbox on purpose: reference documents or chosen memories in, vetted learnings out. The sandbox is deliberately all-or-nothing today, so a persona has no scratchpad or cross-thread memory in a relay thread; how compartmentalization should work beyond that is an open design question.
- Slash commands, channel-history context, name triggers, DMs, honouring edits and deletes from Discord, an hourly trigger cap per binding, a shared "quick start" bot, and running the gateway as its own service.
- A message-footer outlet showing "via Discord · alice" and "Posted to #general" (the data is in `discord_message_link` and `GET /api/discord/chats/{chatId}`).
