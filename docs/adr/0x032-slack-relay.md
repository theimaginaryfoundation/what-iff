# ADR 0x032: Slack relay — your persona as its own Slack app

- **Status:** Proposed (expected to change once it is in use)
- **Branch:** `feat/slack-relay` (stacked on `feat/relay-queue-batch`)
- **Date:** 2026-10-28
- **Deciders:** What Iff maintainers
- **Builds on:** [ADR 0x025](0x025-discord-relay.md) (Discord relay), [ADR 0x028](0x028-buzz-relay.md) (Buzz relay), [ADR 0x031](0x031-relay-turn-batching-and-loop-guard.md) (batching and the loop guard) and the shared runtime `internal/relaykit`

## Context

Personas already live in Discord channels (ADR 0x025) and Buzz communities (ADR 0x028): a channel is bound to a sandboxed relay thread, a mention starts a turn, and the reply is posted back as the persona.
People want the same in Slack, where most teams already work.

Today that is done by hand, and it shows.
The maintainers' own Slack relay has no code of its own; it is assembled from generic parts:

- inbound, a Slack trigger calls What Iff's webhook API (`POST /api/webhooks/chat/{chatId}/messages` with a `wht_` token), one thread per channel, set up channel by channel;
- outbound, the persona has a Slack MCP connector holding a bot token, and its instructions and memories say when and where to post;
- so replies come from one shared bot, not the persona; a reply is only posted when the model remembers to call the connector; reading what happened means polling; and files cannot be posted at all, because the Slack MCP cannot upload bytes (images go out as links instead).

Slack differs from Discord and Buzz in ways that decide the design:

- A Slack **app** is the unit of identity: its bot user has the name and avatar people see, and is what they `@mention` and invite to channels.
  An app installed in a workspace gets a bot token (`xoxb-`) for that workspace.
- Events reach an app either over HTTP (the Events API: Slack calls a public HTTPS request URL, signed with the app's signing secret) or over a WebSocket the app opens (Socket Mode, with an app-level token `xapp-` holding `connections:write`).
- A message is identified by its channel and `ts`; a thread is the `thread_ts` of its first message.
  There is no reply-to-one-message inside a thread.
- Since May 2025, apps distributed to other workspaces outside the Slack Marketplace get much lower limits on reading history (`conversations.history`/`conversations.replies`: one request a minute); apps a workspace builds for itself keep the normal limits.
  Socket Mode apps cannot be listed in the Marketplace.
- A free Slack workspace may install at most 10 apps and custom integrations in all.
- `files.upload` was retired in November 2025; files go up in three steps (`files.getUploadURLExternal`, a POST of the bytes, `files.completeUploadExternal`).

## Decision

### 1. Bring your own app: the app *is* the persona

- The owner creates one Slack app per persona per workspace, from a manifest What Iff generates (Integrations → Slack → "Create the Slack app").
  The button opens `https://api.slack.com/apps?new_app=1&manifest_json=…`, so the app arrives with the persona's name as its name and its bot's display name, Socket Mode on, token rotation off, and exactly the scopes and events below.
- The owner installs the app to the workspace, creates an app-level token with `connections:write`, and pastes the bot token and the app token into What Iff, attaching them to the persona.
- On paste the server checks both:
  - `auth.test` with the bot token gives the workspace (`team_id`, name, URL), the bot user and the bot id;
  - `bots.info` gives the bot's app id;
  - opening a Socket Mode connection with the app token and reading its `hello` gives the app id the app token belongs to, which must be the same app.
- Both tokens are stored encrypted with the same AES-GCM helper as MCP, Discord and Buzz secrets (`TOKEN_ENCRYPTION_SECRET`); without that secret no app can be added.
  Neither is ever returned by the API.
- One What Iff connection per Slack app (`app_id` is unique), and one app per persona per workspace; a persona can be in several workspaces through several apps.
- The avatar cannot be set through Slack's API (bots have no `users.setPhoto`, and manifests carry no icon).
  The tab links the app's settings page, where the owner uploads the persona's portrait as the app's icon.
  The name is set by the manifest; renaming the persona later means renaming the app in Slack.

Required bot scopes, and why:

| Scope | For |
|---|---|
| `channels:history`, `groups:history` | Message events in public and private channels the app is in (mentions and thread continuations), and reading a thread's first message to quote it |
| `channels:read`, `groups:read` | Listing channels to bind, their names, and rename events |
| `channels:join` | Joining a public channel from the app ("Join" in the tab); private channels need `/invite @persona` in Slack |
| `chat:write` | Posting replies and notices |
| `files:read`, `files:write` | Fetching files posted with a mention; uploading the files a reply produced |
| `reactions:write` | The "working on it" reaction (§5) |
| `users:read` | Speakers' display names, whether an author is a bot, and the bot's app id |

Bot events: `message.channels`, `message.groups`, `channel_rename`, `group_rename`, `channel_left`, `group_left`, and the app events `app_uninstalled` and `tokens_revoked`.
`app_mention` is not subscribed: a mention also arrives as a message event, and taking both would deliver every mention twice.

### 2. Socket Mode, behind a leader lock

- The gateway holds one Socket Mode connection per active app (`relaykit.Supervisor`, shared with Discord and Buzz).
  It leads through a Postgres advisory lock on its own key, **`80920035`** (80920031 is the agent-job scheduler, 80920032 Discord, 80920033 the memory embedding backfill, 80920034 Buzz; the registry is in `.env.example`), overridable with `SLACK_GATEWAY_LOCK_KEY`.
  `SLACK_GATEWAY_SINGLE_INSTANCE=true` skips the lock for one-process local runs.
- A connection calls `apps.connections.open`, dials the WebSocket URL it returns (through the public-address dial guard, `relaykit.PublicOnlyControl`), waits for `hello`, and then acknowledges every envelope at once, before handling it, since Slack redelivers what is not acknowledged within a few seconds.
  On a `disconnect` message (Slack refreshes connections every few hours) or a read error it opens a new URL, with backoff.
- What a connection listens to does not depend on its bindings: Slack sends an app every message in every channel it is in, and the relay looks the binding up by app and channel per event.
  So binding a channel needs no reconnect; the fingerprint is the tokens alone.
- An `invalid_auth`, `account_inactive`, `token_revoked`, `app_uninstalled` or `tokens_revoked` marks the app `invalid_token` with the reason; a channel the app was removed from (`channel_left`, `group_left`) marks its bindings broken.
- Retried deliveries (Slack's `retry_attempt`) and a leader handover are harmless: inbound messages are deduplicated on the channel's message `ts` (§3).

### 3. Data

- **`slack_app`**: owner, persona (`personality_id`), workspace (`team_id`, cached name and URL), `app_id` (unique), `bot_user_id`, `bot_id`, `bot_name`, encrypted `bot_token` and `app_token`, `status` (active, invalid_token, disabled), `last_error`, `last_connected_at`.
  Unique on (workspace, persona).
- **`slack_binding`**: a channel bound to a relay thread: app, `chat_id`, `channel_id`, cached `channel_name`, `is_private`, `inbound_enabled`, `allow_user_ids`, `deny_user_ids`, `answer_agents`, `allow_unrestricted`, `status` (active, broken), `last_error`, `last_activity_at`.
  Unique on (app, channel).
- **`slack_message_link`**: one row per message that crossed the boundary: the What Iff message, the binding, `direction`, the message `ts` (inbound: unique per binding, the dedupe key), its `thread_ts`, the `ts` of each message posted (outbound), the author's user id and display name, and the agent depth (ADR 0x031).
- **`slack_pending_post`**: "post the next reply in this thread to this binding", set by the composer toggle or `post_to_slack` and consumed by the reply hook; it expires after 30 minutes.

As for Discord and Buzz, persona and thread are plain UUID columns, and the storage stays per platform (ADR 0x028, Alternatives).

### 4. Inbound

- A message in a bound channel starts a turn when it **mentions the persona's bot** (`<@U…>`), or when it is **in a thread the persona has replied in, from someone the persona answered in that thread** (`SlackThreadContinues`).
  Anyone else in the thread mentions the persona.
  Slack has no reply to a single message inside a thread, so "someone it answered there" stands in for Buzz's direct-reply rule.
- Only something someone wrote counts (no subtype, `thread_broadcast`, `file_share`, or an app's `bot_message`); edits, deletes, joins and other subtypes are ignored, and so is anything older than its binding.
- Never answered: the persona's own messages (its bot user or bot id), integration posts with no user behind them (`bot_message` without a bot user, as from incoming webhooks), and, unless the binding has `answer_agents`, other apps' bots (a message carrying a `bot_id`).
  With `answer_agents`, bots are answered behind the loop guard of ADR 0x031, with the depth persisted on the link rows.
- The message is saved as a user message in the relay thread as `alice (Slack, #general): text`.
  Slack's markup is turned into plain text: the persona's own mention is removed, other `<@U…>` mentions become `@name`, `<#C…|name>` becomes `#name`, `<!here>` becomes `@here`, links become `label (url)`, and `&amp;`, `&lt;`, `&gt;` are unescaped.
  A message in a thread started by someone else quotes that first message (`(in a thread started by bob: "…")`).
  Names come from `users.info` (display name, then real name, then handle), cached for 10 minutes, and are cleaned by `relaykit.SpeakerLabel`.
- Files posted with it are fetched from `files.slack.com` only (HTTPS, the bot token sent to that host and no other, public addresses checked after DNS on every connection), at most 4 files of 20 MB per turn, and attached through the app's upload path.
- The turn runs through the plugin turn starter, metered against the persona's owner with source `slack`, one turn at a time per relay thread; mentions that arrive meanwhile are batched per place and answered in one turn (`relaykit.TurnQueue`, ADR 0x031).
  A place is the binding plus the thread (`thread_ts`); the channel's top-level messages share one place, as in Buzz.
- Relay turns run without the owner's time zone.
  A failed turn posts a neutral notice that reveals nothing about the owner's account; a queue past its cap posts the throttled busy notice; neither is ever posted to a bot.

### 5. Output

- The agent writes one reply; the relay also delivers it to Slack.

| Turn | Goes to Slack |
|---|---|
| Triggered from Slack | Always, into the thread of the message that asked (a top-level message gets a new thread under it) |
| Typed in the app, in a relay thread | Only when the composer toggle "Post the next reply to #channel" is on |
| Any thread of a persona connected to Slack | Only when the agent calls `post_to_slack(channel)` |

- Replies go in the thread, not the channel: that is how Slack apps are expected to answer, and it keeps a busy channel readable.
  Slack notifies the people following the thread, which includes whoever asked, so the reply adds no mention of its own.
- While the turn runs, the persona's bot adds an `:eyes:` reaction to the message it is answering and removes it when the turn ends.
  Slack has no typing indicator for apps outside its assistant surface, and a reaction is the convention people already read as "working on it".
- The reply's Markdown is converted to Slack's `mrkdwn` (`**bold**` → `*bold*`, `[text](url)` → `<url|text>`, headings to bold lines, tables to code blocks), with `&`, `<` and `>` escaped everywhere outside the links it builds.
  So model output can never form `<!channel>`, `<!here>` or `<@U…>` and notify anyone; `link_names` stays off.
- Text is split at 4,000 characters on paragraph, then line boundaries, never inside a code block (`relaykit.SplitText`, the Discord splitter generalised).
- Files the reply produced (never `context_only` ones) are uploaded with the three-step flow into the same thread, at most 4 files and 50 MB per reply; anything left out is named in a note.
- Posting respects Slack's 429 answers (`Retry-After`), retries other transient errors, and is idempotent on the outbound link.
  `not_in_channel`, `channel_not_found`, `is_archived` and `restricted_action` mark the binding broken with the reason; `invalid_auth` and its kin mark the app `invalid_token`.

### 6. Who may mention, what a relay thread can reach, and what it learns

Unchanged from ADR 0x025 §6 and §7, applied to Slack bindings:

- A relay thread the server creates is sandboxed and quiet; binding a thread that is not sandboxed needs `allow_unrestricted`, re-checked on every mention (`relaykit.ThreadOpen`).
- Allow and deny lists are Slack user ids; deny always wins and an empty allow list means anyone in the channel.
- `post_to_slack` in a relay thread of any relay, sandboxed or not, or in any sandboxed chat, reaches only that thread's own channels.
- A thread bound to a Slack channel is an external relay chat: its memories are `external`, the speaker is the Slack display name (the link keeps the user id), and the extraction note names Slack (`RelayPlatformForChat`).

### 7. Configuration and surfaces

- The relay is off by default.
  `SLACK_RELAY_ENABLED=true` (or `1`, `on`) switches it on for every user of the server; otherwise `/api/slack` is not routed (the web app reads the 404 as off and hides its Slack surfaces), the gateway does not start, and `post_to_slack` is neither listed nor offered.
  As for Buzz (ADR 0x028 §9) there is no per-user gate: each persona's app is one its owner created in a workspace they choose.
- Nothing is answered or posted while the persona's app is paused or its tokens are refused, or the binding is broken.
  This is checked again just before each post, so a pause also stops a reply already armed (composer, `post_to_slack`) or a turn already running; arming a post on such a binding is refused (409).
- `SLACK_GATEWAY_LOCK_KEY`, `SLACK_GATEWAY_SINGLE_INSTANCE` as in §2.
- Routes (authenticated, owner-scoped, another account's rows are a 404): `/api/slack/manifest?personality_id=`, `/apps`, `/apps/{id}` (update tokens, refresh, pause, remove), `/apps/{id}/channels`, `/apps/{id}/channels/{channelId}/join`, `/bindings`, `/bindings/{id}`, `/chats/{chatId}`, `/chats/{chatId}/pending/{bindingId}`.
- Web app: an Integrations → Slack tab; Slack relay threads in the Thread Manager's Jobs tab; the composer toggle in a relay thread.

## Replacing the hand-built relay

To retire the manual setup once this is on for the maintainers' accounts:

1. Create the persona's app from the tab's manifest link (or add Socket Mode, an app-level token, and the scopes and events above to the existing Slack app, if it should become one persona's app), install it, and paste the two tokens.
2. Bind each channel the old setup served, to its existing thread (with the unrestricted acknowledgement if that thread is not sandboxed) or to a new sandboxed one.
3. Turn off the Slack trigger or workflow that calls the webhook API, and revoke the `wht_` webhook token it used (Integrations → Webhooks).
4. Detach the Slack MCP connector from the persona, and remove the posting rules from its instructions and memories; `post_to_slack` replaces them.
5. Image links that tools already return keep working in messages; a reply's own images are now uploaded as files.

## Trying it

There is no local Slack to run against, so trying the relay needs a real workspace (a free one works, within its 10-app limit):

1. Run What Iff with `SLACK_RELAY_ENABLED=true` and `TOKEN_ENCRYPTION_SECRET` set; locally, `SLACK_GATEWAY_SINGLE_INSTANCE=true`.
   No public URL is needed: the gateway connects out to Slack.
2. In Integrations → Slack, pick a persona and follow "Create the Slack app" (the manifest link), install the app, create the app-level token, and paste both tokens.
3. Upload the persona's portrait as the app's icon in the app's settings (the tab links them).
4. Invite the persona to a channel (`/invite @Vix`, or "Join" for a public channel), bind it, and mention the persona there.

## Alternatives considered

- **One What Iff Slack app, installed per workspace by OAuth, with personas speaking through `chat:write.customize`** (a `username` and `icon_url` on each message).
  It is one click to install and has no per-persona app limit, but a persona would not be a member: nobody can `@Vix`, the member list shows one bot, every message is badged with the shared app's name, and routing a mention of "the app" to a persona needs a convention (a name prefix, or one persona per channel).
  It also needs a public OAuth redirect and an app registered per deployment (self-hosters would each register their own), and as a distributed app outside the Marketplace it gets the reduced history limits.
  Rejected for now; a shared "quick start" app could come later for workspaces that do not want an app per persona, as for Discord.
- **The Events API (HTTP) instead of Socket Mode.**
  It scales out without a leader lock, but every self-hosted What Iff would need a public HTTPS URL, each app's request URL would have to route by `api_app_id` to that app's signing secret, and Slack's `url_verification` and 3-second answer rules would land in the API's request path.
  Socket Mode needs only outbound connections, matches the leader-locked gateways of the other two relays, and is allowed for apps a workspace builds for itself, which these are.
  Rejected; it is the path if a Marketplace-listed shared app is ever built.
- **Create the app automatically through the manifest API** (`apps.manifest.create` with an owner's configuration token).
  Configuration tokens expire after 12 hours, app-level tokens cannot be created through the API, and installing still needs the owner's click, so it saves one step of three.
  Not now; the manifest link gives the same app.
- **Keep the webhook-plus-MCP setup and document it.**
  It cannot post as the persona, cannot post files, depends on the model choosing to post, and is configured channel by channel in two places.
  Rejected; this ADR replaces it.
- **Slack's `markdown` block for replies** instead of converting to `mrkdwn`.
  It renders standard Markdown, but how it treats Slack's mention syntax is not documented, and the relay's rule is that model output can never notify anyone.
  Converting and escaping keeps that rule checkable in a test; revisit when Slack documents it.
- **Answer every message in a thread the persona replied in.** Rejected as too chatty, as for Buzz.
- **Reply in the channel, not in a thread.** Rejected: Slack apps answer in threads, and a burst of replies in a channel buries the conversation.
- **Slack's assistant surface (`assistant.threads.*`, the split-view AI app).** It is for one-to-one help in a side panel, not membership of channels; a separate follow-up if wanted.

## Consequences

- A persona can be a member of a Slack workspace, with its own name and avatar, without What Iff needing a public endpoint; one leader holds one WebSocket per persona app.
- Setup is heavier than Discord's: create the app from the link, install it (which some workspaces route to an admin for approval), make an app token, paste two tokens.
  Each persona uses one of a free workspace's 10 app slots.
- Every message in every channel the app is in reaches the server, not only mentions; the app is in the channels its owner invited it to, so this is bounded by what they chose.
- What the persona posts is a Slack message in someone else's workspace; deleting the relay thread in What Iff deletes nothing in Slack.
- `post_to_slack` carries the same prompt-injection exposure as `post_to_discord` and `post_to_buzz` (ADR 0x025, Consequences), with the same limits.

## Not done yet

- Not tried against a real Slack workspace yet: the wire format is covered by tests against fakes of Slack's Web API and Socket Mode.
- DMs to the persona (`message.im`), Slack's assistant surface, slash commands, shortcuts and interactive messages.
- Edits and deletes from Slack, and editing a posted reply when the What Iff message is regenerated.
- A shared quick-start app (Alternatives), and creating the app from the manifest API.
- Showing "via Slack · alice" on relayed messages in the app.
- Offering the persona's portrait as a download next to the app's settings link (the portrait URL needs the owner's session, so a plain link does not work).
- Keeping relay turns out of the owner's own chat allowance in the private overlay (it keys on `metering.TurnSource`; `slack` needs the same treatment as `discord` and `buzz`).
- The note an external memory carries in model context still says `said by <name> on Discord` whatever the relay (`memoryutil.ExternalProvenanceNote`); the memory stores the speaker but not the platform.
  That predates this ADR (Buzz memories read the same) and needs the platform recorded on the memory, or a platform-neutral wording.
- Slack Connect channels shared with other organisations: their members are answered like anyone else in the channel, and their files may need `files.info` before they can be fetched.
