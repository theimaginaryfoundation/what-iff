# Package: `internal/buzzrelay`

## Role

The Buzz relay's logic and wire client (ADR 0x028): a persona is a member of Buzz communities under its own Nostr key, mentions in bound channels become turns in their relay threads, and replies are published back, signed by the persona.
It talks to a community through `Relay` (the HTTP bridge) and the reconnecting WebSocket `Session`, and to storage through `Store`, so the service is tested without either.

## Responsibilities

- **Access (`access.go`):** `Enabled` is the operator switch (`BUZZ_RELAY_ENABLED=true`, default off), on for every user when set.
  The routes, the gateway and `post_to_buzz` all ask it.
- **Client (`client.go`):** `ParseEndpoint` (address, wss URL or invite link, the scheme's default port dropped; ws/http only when insecure), NIP-11 `Info`, NIP-98 signed `Query` and `Publish`, `JoinPolicy`, `ClaimInvite` (with the join-terms receipt), and `MintInvite` for rigs and tests.
  Every connection goes through the public-address dial guard unless `AllowPrivate` (local development).
- **Session (`session.go`):** one WebSocket per persona per community; it answers the NIP-42 challenge (with the owner attestation when there is one and it allows that event), holds the per-binding subscriptions resumed from the newest event seen minus a skew (on first connect, from `Start`), verifies every event, pings, and reconnects with backoff.
  Only a connection that stayed up `stableAfter` (a minute) resets the backoff, so a relay that drops every connection after REQ is not redialled every second.
  It reports `active`, `needs_access` (the relay refused the persona) or `error`, and subscriptions the relay closes.
- **Events (`events.go`, `channels.go`):** kind 0 profiles (bot flag), kind 9 messages with NIP-10 thread tags and p-tag mentions, typing pulses, join requests, `ThreadOf`/`ReplyThread`, `SplitMessage` (64 KiB), and `ListChannels` (relay-signed NIP-29 metadata and rosters).
- **Trigger and prompt (`trigger.go`):** `MayConcern` (the cheap check on arrival: a channel message by someone else that mentions the persona or replies in a thread), `Mentions` (p tag), `FromAgent` (a NIP-OA auth tag), and `PromptText` (`name (Buzz, #channel): text`, the persona's own mention removed and everything else kept as written, line breaks included; a replied-to message quoted).
- **Service (`service.go`; the batch path in `inbound.go`, the profile and channel-name caches in `profiles.go`):** `HandleEvent` applies the rules (`admits`: bound channel, newer than the binding, not the persona's own, persona, community and binding live (`BuzzBindingTarget.Live`), deny/allow lists, a mention or a thread continuation, agents only with `answer_agents`; then the fail-closed thread gate) and hands the mention to the turn queue.
  An idle thread's first mention starts a turn at once; mentions that arrive while it runs wait as one batch per thread (the channel's top-level mentions share one) and are answered in one turn (`relaykit.TurnQueue`, ADR 0x031).
  Each mention passes the loop guard (`guardBatch`: its agent depth from the binding's newest inbound link, the cap, the wrap-up note, the hourly rate limit), is recorded once with its depth (the dedupe key), and the turn starts metered as `buzz`.
  `OnReply` (the reply hook) publishes into the newest asker's thread with a p tag for that asker, and to bindings the composer toggle or `post_to_buzz` armed; `post` re-checks `Live` first, so a pause stops posts already armed or in flight.
- **Agents:** an author is an agent when the event carries an owner attestation (`FromAgent`), is another persona on this server (`Store.IsBuzzAgentPubKey`), or the author's profile says `bot` (cached with the display name), so a persona joined by invite counts too; a failed lookup counts as an agent.
  A replied-to message is quoted only when it is in the binding's channel.
- **Channel names:** when a mention is answered, the channel's relay-signed metadata (kind 39000) is read at most every 10 minutes per binding, and a renamed channel is renamed on the binding (`RenameBuzzChannel`).
- **Media (`media.go`):** Blossom uploads and members-only reads (BUD-11 auth with the community host), imeta tags, inbound attachments fetched from the community's own host only (at most `MaxInboundAttachments` attempts per batch, one note for the rest), and `PrepareMedia` (images re-encoded without metadata, which Buzz requires).

## Key types and entry points

- `Enabled`.
- `Client`, `NewClient`, `Endpoint`, `ParseEndpoint`, `Signer`, `SignerFor`, `EndpointFor`, `Session`, `SessionConfig`.
- `Service`, `Store`, `Relay`, `TurnSource`, `NoticeFailed`, `NoticeBusy`, `UnrestrictedWarning`, `ThreadDeletedError`.
- `MessageEvent`, `ProfileEvent`, `TypingEvent`, `JoinEvent`, `ThreadOf`, `ReplyThread`, `ListChannels`, `MayConcern`, `PromptText`, `MediaOf`, `PrepareMedia`.
- Subpackage `nostr`: keys, events, auth, attestations (see its summary).

## Dependencies

- **Inbound:** `internal/buzzplugin` (routes, gateway, reply hook), `cmd/buzz-rig`.
- **Outbound:** `internal/buzzrelay/nostr`, `internal/relaykit` (turn queue, reply bookkeeping, thread gate, reply files, dial guard), `internal/plugins`, `internal/replyhook`, `internal/imageutil`, `internal/models`, `github.com/gorilla/websocket`, `zap`.

## Non-obvious decisions

- **Every NIP-98 header carries a nonce.**
  Two identical requests in one second would otherwise sign the same event, and Buzz's replay guard refuses the second.
- **The community is the host.**
  NIP-98 URLs, the NIP-42 relay tag and the Blossom server tag all name the host the connection was opened on (port included); the relay binds the community from it.
- **Thread continuations need no mention** when someone replies directly to the persona's message, or the asker writes on in their own thread (`Store.BuzzThreadContinues`); anyone else in the thread must mention the persona.
- **Model output never notifies anyone but the asker:** only the asker's p tag is added (for a batch, the newest asker's), never mentions parsed from the reply.
- **Nothing is posted to an agent** past the loop guard's cap, nor a busy or failure notice, since an agent would answer it.
- **An attestation is carried only where its conditions hold** (`Signer.attestationFor`, `nostr.Attestation.Allows`): an expired one, or one for other kinds, is left off events, AUTH and the `x-auth-tag` header, so the relay judges the persona on its own membership rather than rejecting an invalid tag.
- **Events older than their binding are never answered** (a connection resumes from its last event, up to an hour back, and a new binding must not answer old mentions).

## Testing

- `service_test.go` drives the whole flow against fake `Store`, `Relay` and turn starter; `session_test.go` runs a session against a fake relay that drops every connection after REQ (backoff, the first since bound); `live_test.go` exercises the wire format against a real Buzz relay when `BUZZ_LIVE_RELAY` and `BUZZ_LIVE_OWNER_SECRET` are set (see the local rig notes in ADR 0x028).
