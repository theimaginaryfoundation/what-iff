# ADR 0x028: Buzz relay — your persona as a member of a Buzz community

- **Status:** Proposed (proof of concept; expected to change once it is in use)
- **Branch:** `feat/buzz-relay`
- **Date:** 2026-10-22
- **Deciders:** What Iff maintainers
- **Builds on:** [ADR 0x025](0x025-discord-relay.md) (Discord relay), sandboxed threads (`Chat.context_scope`), the plugin seam (`internal/plugins`) and the reply hook (`internal/replyhook`)

## Context

[Buzz](https://github.com/block/buzz) is Block's open-source (Apache 2.0) workspace for people and agents.
Under the hood it is a Nostr relay: every message, reaction and membership change is an event signed by its author's secp256k1 key.
A Buzz **community** is the workspace a user reaches by URL; one relay deployment can host many communities behind many hostnames.
Agents in Buzz are members, not bots bolted on the side: an agent is a keypair, optionally attested by its owner's key ([NIP-OA](https://github.com/block/buzz/blob/main/docs/nips/NIP-OA.md)), that people add to channels the way they add a person.

We want multi-person, multi-agent rooms where What Iff personas take part, and where what happens in a room can come back, on purpose, to the owner's own What Iff.
Discord gives us rooms but treats a persona as a bot; Buzz treats it as a member, signs every message, and can be self-hosted per customer group.
Building rooms inside the app is the other option, and is not ruled out (see Alternatives).

Buzz has no production web client to embed (its browser host is for development and tests), so a UI-level integration would mean writing our own Nostr web client.
Its wire protocol, by contrast, is small and open: WebSocket `REQ`/`EVENT`/`AUTH` and an HTTP bridge (`/query`, `/events`, `/api/invites/claim`) that takes NIP-98 signed requests.
So this ADR integrates at the API layer: the What Iff server holds each persona's Buzz key and acts as that member, exactly as the Discord relay acts as a persona's bot.

The Discord relay already solved the hard parts: a leader-locked gateway, a sandboxed and quiet relay thread per channel, one turn at a time per thread, external provenance for what strangers say, and replies mirrored out through the reply hook.
The Buzz relay reuses that design and, where the two would otherwise duplicate code, shares it (`internal/relaykit`).

## Decision

### 1. The persona owns a Buzz identity; the server holds the key

- Connecting a persona to Buzz generates a secp256k1 keypair for it (`buzz_agent`), one per persona.
  The secret key is stored encrypted with the same AES-GCM helper as MCP and Discord tokens (`TOKEN_ENCRYPTION_SECRET`); without that secret no persona can be connected.
  The secret key is never returned by the API; the public key is shown as an `npub` so people can find and add the persona in Buzz.
- The same key is used in every community the persona joins: Buzz's model is that identity is portable and community state is not.
- The persona's profile is published as a kind 0 event (`name`, `display_name`, `about`, `picture`, `bot: true`).
  "Use persona's name and portrait" republishes it; the portrait is uploaded to the community's media store first (Blossom `PUT /media/upload`) when it has one.
- What Iff never asks for, and never holds, a person's own Buzz key.

### 2. Joining a community

A persona joins a community (`buzz_community`: the agent plus a relay URL) in one of three ways, whichever the community allows:

| Community | How the persona gets in |
|---|---|
| Open (no member list) | It authenticates; the relay registers it. |
| Invite-only, and the owner has an invite link | The owner pastes the link; the server claims it as the persona (`POST /api/invites/claim`, NIP-98 signed). When the community has join terms, the owner reads and accepts them for the persona first (`/api/invites/accept-policy`, with the owner's own confirmation of any age requirement), and the accepted version is recorded; until then the claim answers 409 `buzz_terms_required` with the terms in its details. |
| Invite-only, no invite | The app shows the persona's `npub`; an admin adds it as a member in Buzz, and the owner presses "Check again". |

- Optionally, the owner pastes a NIP-OA **owner attestation** for the persona's key (`["auth", owner_pubkey, conditions, sig]`).
  The server verifies its signature and carries it on the NIP-42 `AUTH` event and on every event it signs, so Buzz shows the persona as the owner's agent and can admit it through the owner's membership ([NIP-AA](https://github.com/block/buzz/blob/main/docs/nips/NIP-AA.md)).
  What Iff does not mint attestations: that needs the owner's key, which stays with the owner.
- The relay URL is normalised to `wss://host[:port]` (`ws://` only for local development, see §9) and read once on add (NIP-11: name, whether auth and membership are required, the relay's own pubkey).
- Connection health is recorded on the community row: `active`, `needs_access` (authenticated, but not a member), `error` (unreachable, or the relay rejected us), or `disabled` (paused by the owner).

### 3. Gateway: one WebSocket per persona per community, behind a leader lock

- The supervisor (`relaykit.Supervisor`, shared with the Discord gateway) leads through a Postgres advisory lock on its own key, `80920034` (the agent-job scheduler uses `80920031`, Discord `80920032`, the memory embedding backfill `80920033`; the registry is in `.env.example`), overridable with `BUZZ_GATEWAY_LOCK_KEY`.
  `BUZZ_GATEWAY_SINGLE_INSTANCE=true` skips the lock for one-process local runs.
- The leader keeps one connection per active community row, answers the relay's `AUTH` challenge with a kind 22242 event signed by the persona (plus its attestation, when there is one), and subscribes to each bound channel: `{"kinds":[9], "#h":[channel], "since": …}`.
  It re-reads the rows and bindings about every 30 s and resubscribes when a binding is added or removed.
- On reconnect it resubscribes from the last event it saw, minus a skew, so a short outage loses nothing; redelivered events are deduplicated on the event id (§4).
- Events are verified (id and Schnorr signature) before anything else; an event whose signature does not check out is dropped.

### 4. Data

- **`buzz_agent`**: owner, persona (`personality_id`, unique), `pubkey` (hex, unique), encrypted `secret_key`, `status` (active, disabled), `last_error`.
- **`buzz_community`**: agent, `relay_url` (unique per agent), cached `name` and `relay_pubkey`, optional `auth_tag` (verified attestation) and `owner_pubkey`, `policy_version` accepted for the persona, `status`, `last_error`, `last_connected_at`.
- **`buzz_binding`**: a channel bound to a relay thread: community, `chat_id`, `channel_id` (the NIP-29 group id, Buzz's `h` tag), cached `channel_name`, `inbound_enabled`, `allow_pubkeys`, `deny_pubkeys`, `answer_agents`, `allow_unrestricted`, `status` (active, broken), `last_error`, `last_activity_at`. Unique on (community, channel).
- **`buzz_message_link`**: one row per message that crossed the boundary: the What Iff message, the binding, `direction`, the Buzz event id (inbound: unique per binding, the dedupe key) and its thread root, the event ids posted (outbound), and for inbound messages the author's pubkey and display name.
- **`buzz_pending_post`**: "post the next reply in this thread to this binding", set by the composer toggle or the `post_to_buzz` tool and consumed by the reply hook; it expires after 30 minutes.

Persona and thread are plain UUID columns, as for Discord, so `Personality` and `Chat` stay unaware of the relay.
The Discord tables are not generalised into shared `relay_*` tables (see Alternatives).

### 5. Inbound

- A message in a bound channel starts a turn when it **p-tags the persona** (a mention) or **replies in a thread the persona posted in** (its `e` root or parent is an event the persona published, per `buzz_message_link`).
- Never answered: the persona's own events, events that fail verification, and, unless the binding has `answer_agents`, events carrying a NIP-OA `auth` tag (other agents).
  Agent-to-agent conversation, and the loop guards it needs, are deliberately left for the same follow-up that covers Discord's.
- The allow and deny lists are pubkeys (hex or `npub`); deny always wins and an empty allow list means anyone in the channel.
- The message is saved as a user message in the relay thread as `alice (Buzz, #general): text`.
  The persona's own `@name` and `nostr:npub…` mention are removed. The author's name is their kind 0 `display_name` (or `name`), read from the relay and cached; it is cleaned the same way as Discord names (`relaykit.SpeakerLabel`), so a name cannot pose as another speaker's label.
- The turn runs through the plugin turn starter, metered against the persona's owner with source `buzz`; one turn at a time per relay thread, up to 5 queued behind it (`relaykit.TurnQueue`, shared with Discord).
  While the turn runs the persona shows as typing (kind 20002, ephemeral).
- A failed turn posts a neutral notice that reveals nothing about the owner's account; relay turns run without the owner's time zone.

### 6. Who may tag, and what a relay thread can reach

Unchanged from ADR 0x025 §6, applied to Buzz bindings:

- A relay thread the server creates is sandboxed and quiet: it reads only itself, writes only Chat-scoped memories, starts with the sandbox's default-off tools, and has no MCP connectors.
- Binding an existing thread that is not sandboxed needs `allow_unrestricted`, which is stored only while the thread is not sandboxed and re-checked on every tag.
- `post_to_buzz` in a relay thread, or in any sandboxed chat, reaches only that thread's own channels.

### 7. Output

- The agent writes one reply; the relay also delivers it to Buzz.

| Turn | Goes to Buzz |
|---|---|
| Triggered from Buzz | Always: a kind 9 reply in the triggering message's thread (`e` root/reply, NIP-10), p-tagging the person who asked |
| Typed in the app, in a relay thread | Only when the composer toggle "Post the next reply to #channel" is on |
| Any thread of a persona connected to Buzz | Only when the agent calls `post_to_buzz(channel)` |

- Buzz renders Markdown and takes messages up to 64 KiB, so a reply is posted whole (split only past that); mentions in the reply text are not turned into `p` tags, so model output cannot notify anyone but the asker.
- Files the reply produced are uploaded to the community's media store and attached with `imeta` tags; files a tool merely fetched (`context_only`) are not posted, as for Discord.
- Delivery is asynchronous through the reply hook, retries transient errors, and is idempotent on the link row.
  A relay that refuses the event (`OK false`) marks the binding broken with the relay's reason when the reason is about access (`restricted:`, `blocked:`), and is retried otherwise.

### 8. What is learned in a relay thread is external

ADR 0x025 §7 applies as written, with Buzz as a second source:

- `IsExternalRelayChat` is true for a thread bound to a Discord channel **or** a Buzz channel.
- The speaker recorded on an external memory is the Buzz display name; the inbound link keeps the author's pubkey, which, unlike a Discord display name, is the author's signed identity.
- The extraction note names the platform the thread is relayed from.

### 9. Configuration and surfaces

- The relay is an **experimental feature** (`models.ExperimentalFeatureBuzzRelay`, `buzz_relay`), off by default.
  `BUZZ_RELAY_ENABLED=true` (or `1`, `on`) switches it on for the server; otherwise `/api/buzz` is not routed, the gateway does not start and `post_to_buzz` is never offered.
- On top of that switch, a build may link a per-user experimental policy (`featuregate.NewExperimental`).
  The open-source build links none, so the switch decides for everyone.
  The stock policy, `featuregate.UserFlagExperimentalGate`, turns a feature on for the users an admin listed it for (`User.experimental_features`) and for super admins; a deployment links it to release the relay to a few testers first.
  For anyone else the routes answer 404 (the web app then hides its Buzz surfaces, as when the switch is off), the gateway drops their personas' connections at its next reconcile, and the tool is not offered (`buzzrelay.UserMayUse`).
- `BUZZ_GATEWAY_LOCK_KEY`, `BUZZ_GATEWAY_SINGLE_INSTANCE` as in §3.
- `BUZZ_ALLOW_PRIVATE_RELAYS=true` lets a persona connect to relays on loopback or private addresses, and over `ws://`/`http://`.
  It is for local development: without it, the server connects only to public addresses (checked after DNS on every connection, the Discord CDN fetcher's guard, now `relaykit.PublicOnlyControl`), over TLS, because a relay URL is user input and the server would otherwise connect wherever it points.
- Routes (authenticated): `/api/buzz/agents`, `/agents/{id}` (update, remove), `/agents/{id}/sync-profile`, `/agents/{id}/communities`, `/policy?relay_url=` (preview join terms), `/communities/{id}` (update, check again, remove), `/communities/{id}/channels`, `/communities/{id}/channels/{channelId}/join`, `/bindings`, `/bindings/{id}`, `/chats/{chatId}`, `/chats/{chatId}/pending/{bindingId}`. Everything is owner-scoped; another account's rows are a 404.
- Web app: an Integrations → Buzz tab; Buzz relay threads in the Thread Manager's Jobs tab; the composer toggle in a relay thread.

## Alternatives considered

- **Embed Buzz's UI in What Iff.** There is no production web client to embed, and the desktop client's plugin panels are macOS-only and explicitly unstable. Rejected for now; revisit if Buzz ships a web client.
- **Write our own Nostr web client (rooms in the app, Buzz as the backend).** That is the "build rooms in-app" plan with a draft protocol of ~130 custom event kinds underneath. Too much surface to chase for a first integration.
- **Run the persona behind `buzz-acp`** (Buzz's harness for Claude Code, Codex, Goose and Pi) with a small ACP shim that calls What Iff's webhook API. Cheap, but the persona would live on someone's desktop and only answer while the Buzz app is open. Kept as a demo path, not the product.
- **Ask the owner for their Buzz key to mint the attestation server-side.** Rejected: What Iff would then hold a person's identity, not an agent's. The attestation is optional and pasted.
- **Generalise the Discord tables into shared `relay_bot`/`relay_binding` tables with a platform column.** Cleaner in the long run, but it migrates shipped production tables for a proof of concept. Rejected for now; the runtime pieces are shared (`relaykit`) and the storage stays per platform.
- **Answer every message in a thread the persona took part in.**
  Rejected as too chatty: a direct reply to the persona's message, or the asker writing on in their own thread, is answered without a mention (`BuzzThreadContinues`); anyone else in the thread mentions the persona.
- **Subscribe only to mentions (`#p` = persona).** Cheaper on a busy relay, but a reply in the persona's thread does not always p-tag it, and the channel stream is what later channel-history context will need. The subscription is per bound channel only, so the cost is bounded by what the owner binds.

### 10. Found against a real relay

The wire format was checked against Buzz itself (relay `ghcr.io/block/buzz:main`, October 2026), which settled details the docs leave open:

- Two identical NIP-98 requests in one second sign the same event, and Buzz's replay guard refuses the second, so every header carries a random `nonce` tag.
- The community is bound from the Host header: NIP-98 URLs, the NIP-42 `relay` tag and the Blossom `server` tag (port included) must all name it, and a relay reached under another name answers 404.
- Media reads are members-only (a BUD-11 `get` authorization, with the attestation in `x-auth-tag`), and uploads that carry image metadata are refused, so images are re-encoded first (`buzzrelay.PrepareMedia`).
- An owner attestation admits a persona that is not itself a member (NIP-AA), and an invite claim makes it a full member.

## Local development

The relay can be tried end to end without the Buzz desktop app:

- Run a Buzz relay with its Compose bundle (`deploy/compose` in block/buzz) on `localhost:3000`, closed to members, with a test owner key.
- The Buzz 1.0 desktop app accepts only `https://`/`wss://` communities, so to use it, put the relay behind TLS (Caddy with `tls internal`, the relay's `RELAY_URL` set to the TLS address) and trust Caddy's local CA in the keychain.
  A community is bound to its host, so moving the relay to a new address starts a fresh community.
  What Iff's API container then needs the CA too (`SSL_CERT_FILE` pointing at a bundle with Caddy's root appended).
- Run What Iff with `BUZZ_ALLOW_PRIVATE_RELAYS=true`. When the API runs in Docker, a sidecar sharing its network namespace forwards its `localhost:3000` to the host, so the relay sees the host name it serves.
- `go run ./cmd/buzz-rig` mints attestations and invites, creates channels, publishes profiles, posts as a test person (`say -mention`, `-reply`, `-file`) and watches a channel.
- `BUZZ_LIVE_RELAY=http://localhost:3000 BUZZ_LIVE_OWNER_SECRET=<hex> go test -run Live ./internal/buzzrelay/` runs the live wire tests.

## Questions for the Buzz team

- Can Buzz Desktop mint a NIP-OA attestation for an agent key it did not create ("adopt this agent")? Today the owner needs their key in a tool to produce one.
- Does a thread reply in the desktop app p-tag the thread's participants (or the parent's author)? We answer direct replies to the persona and the asker's own follow-ups without a mention, and would rather follow Buzz's convention.
- Is there a supported way for a hosted operator to give a third-party server's agents access across many communities (a service identity, or NIP-FI for agents)?
- What is the plan for a production web client, and for the plugin panel API (`browser.View`) on non-macOS builds?
- Which kinds and NIPs are stable enough to build on (NIP-OA, NIP-AA, NIP-29 kinds 9 and 9021, NIP-CW windows), given the 0.x → 1.0 move?
- How should an external agent signal liveness: owner-sent kind 20003, or its own presence?
- Is agent-to-agent loop prevention planned in the relay (hop counts, rate limits per agent pair), or left to each agent?

## Consequences

- A persona can be a member of any Buzz community its owner can get it into, with no Buzz app running anywhere: the What Iff server is the agent's body.
  That is the "trusted broker" Buzz's own [information-flow note](https://github.com/block/buzz/blob/main/docs/practical-information-flow-for-buzz-agents.md) proposes: the key is held by the broker, and each audience (channel) gets its own sandboxed session.
- Everything the persona posts is a signed, append-only event on a relay the owner does not necessarily control. Deleting the relay thread in What Iff does not delete anything in Buzz.
- One WebSocket per persona per community runs in one API instance; per-community lock keys would spread them later without changing the supervisor.
- The relay depends on draft Buzz NIPs (NIP-OA, NIP-AA) and on Buzz's HTTP bridge, both of which are moving quickly (Buzz 0.x → 1.0 is in progress).
- `post_to_buzz` carries the same prompt-injection exposure as `post_to_discord` (ADR 0x025, Consequences), with the same limits.

## Not done yet

- Hosting: running our own multi-community relay per customer group (host = community, NIP-FI letting What Iff mint identity assertions), as sketched in the 2026-10-22 design notes. This ADR only connects to relays that exist.
  The likely path is to run a Buzz server alongside What Iff and let users create communities on it.
  The order is: a small group of testers first (behind the per-user experimental flag, §9), then users creating their own communities, which needs a way to create a community, connect it and manage it from the app.
- Moving data into or out of a sandbox on purpose (ADR 0x025's open item), which Buzz makes more pressing: promoting a room's learnings to the owner's account, and giving a room a curated brief.
- Agent-to-agent conversation and its loop guards, shared with the Discord follow-up.
- DMs, reactions, edits and deletes from Buzz, forum channels, canvases and artifacts, and NIP-AE engrams.
- Keeping relay turns out of the owner's own chat allowance in the private overlay (it keys on `metering.TurnSource`; `buzz` needs the same treatment as `discord`).
- Moving a Buzz binding to another thread in the app (Discord has it), and sharing the composer toggle and key-chip components between the two relays.
- An author without a Buzz profile is labelled by a shortened npub, which a small model can misread in the extraction note (it once took "Buzz" for the speaker's name); a clearer fallback label is a cheap follow-up.
- Persona presence in Buzz (online/offline), and showing "via Buzz · alice" on relayed messages in the app.
