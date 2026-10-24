# Package: `internal/buzzrelay`

## Role

The Buzz relay's logic and wire client (ADR 0x028): a persona is a member of Buzz communities under its own Nostr key, mentions in bound channels become turns in their relay threads, and replies are published back, signed by the persona.
It talks to a community through `Relay` (the HTTP bridge) and the reconnecting WebSocket `Session`, and to storage through `Store`, so the service is tested without either.

## Responsibilities

- **Access (`access.go`):** the relay is an experimental feature.
  `Enabled` is the operator switch (`BUZZ_RELAY_ENABLED=true`, default off); `UserMayUse` adds the per-user policy a build may link (`featuregate.ExperimentalEnabled`).
  The routes, the gateway and `post_to_buzz` all ask `UserMayUse`.
- **Client (`client.go`):** `ParseEndpoint` (address, wss URL or invite link; ws/http only when insecure), NIP-11 `Info`, NIP-98 signed `Query` and `Publish`, `JoinPolicy`, `ClaimInvite` (with the join-terms receipt), and `MintInvite` for rigs and tests.
  Every connection goes through the public-address dial guard unless `AllowPrivate` (local development).
- **Session (`session.go`):** one WebSocket per persona per community; it answers the NIP-42 challenge (with the owner attestation when there is one), holds the per-binding subscriptions resumed from the newest event seen minus a skew, verifies every event, pings, and reconnects with backoff.
  It reports `active`, `needs_access` (the relay refused the persona) or `error`, and subscriptions the relay closes.
- **Events (`events.go`, `channels.go`):** kind 0 profiles (bot flag), kind 9 messages with NIP-10 thread tags and p-tag mentions, typing pulses, join requests, `ThreadOf`/`ReplyThread`, `SplitMessage` (64 KiB), and `ListChannels` (relay-signed NIP-29 metadata and rosters).
- **Trigger and prompt (`trigger.go`):** `Mentions` (p tag), `FromAgent` (a NIP-OA auth tag), and `PromptText` (`name (Buzz, #channel): text`, the persona's own mention removed, a replied-to message quoted).
- **Service (`service.go`):** `HandleEvent` applies the rules (bound channel, newer than the binding, not the persona's own, deny/allow lists, agents only with `answer_agents`, a mention or a thread continuation, and the fail-closed thread gate), queues one turn at a time per thread, records the inbound link (the dedupe key), and starts the turn metered as `buzz`.
  `OnReply` (the reply hook) publishes into the asker's thread with a p tag for the asker, and to bindings the composer toggle or `post_to_buzz` armed.
- **Media (`media.go`):** Blossom uploads and members-only reads (BUD-11 auth with the community host), imeta tags, inbound attachments fetched from the community's own host only, and `PrepareMedia` (images re-encoded without metadata, which Buzz requires).

## Key types and entry points

- `Enabled`, `UserMayUse`.
- `Client`, `NewClient`, `Endpoint`, `ParseEndpoint`, `Signer`, `SignerFor`, `EndpointFor`, `Session`, `SessionConfig`.
- `Service`, `Store`, `Relay`, `TurnSource`, `NoticeFailed`, `NoticeBusy`, `UnrestrictedWarning`, `ThreadDeletedError`.
- `MessageEvent`, `ProfileEvent`, `TypingEvent`, `JoinEvent`, `ThreadOf`, `ReplyThread`, `ListChannels`, `PromptText`, `MediaOf`, `PrepareMedia`.
- Subpackage `nostr`: keys, events, auth, attestations (see its summary).

## Dependencies

- **Inbound:** `internal/buzzplugin` (routes, gateway, reply hook), `cmd/buzz-rig`.
- **Outbound:** `internal/buzzrelay/nostr`, `internal/relaykit` (turn queue, reply bookkeeping, thread gate, reply files, dial guard), `internal/plugins`, `internal/replyhook`, `internal/imageutil`, `internal/featuregate`, `internal/models`, `github.com/gorilla/websocket`, `zap`.

## Non-obvious decisions

- **Every NIP-98 header carries a nonce.**
  Two identical requests in one second would otherwise sign the same event, and Buzz's replay guard refuses the second.
- **The community is the host.**
  NIP-98 URLs, the NIP-42 relay tag and the Blossom server tag all name the host the connection was opened on (port included); the relay binds the community from it.
- **Thread continuations need no mention** when someone replies directly to the persona's message, or the asker writes on in their own thread (`Store.BuzzThreadContinues`); anyone else in the thread must mention the persona.
- **Model output never notifies anyone but the asker:** only the asker's p tag is added, never mentions parsed from the reply.
- **Events older than their binding are never answered** (a reconnect resumes a minute back, and a new binding must not answer old mentions).

## Testing

- `service_test.go` drives the whole flow against fake `Store`, `Relay` and turn starter; `live_test.go` exercises the wire format against a real Buzz relay when `BUZZ_LIVE_RELAY` and `BUZZ_LIVE_OWNER_SECRET` are set (see the local rig notes in ADR 0x028).
