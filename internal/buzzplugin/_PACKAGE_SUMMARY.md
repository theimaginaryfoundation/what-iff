# Package: `internal/buzzplugin`

## Role

Wires the Buzz relay into the API server through the plugin seam: the `/api/buzz` routes, the reply hook, and the gateway that holds each persona's connection to each of its communities (ADR 0x028).
Linked by a blank import in `cmd/api-server/plugins.go`.

## Responsibilities

- **`init`:** registers the reply hook (which does nothing until the plugin is applied) and the plugin.
- **`apply`:** reads `BUZZ_RELAY_ENABLED` (off for `false`, `0` or `off`) and `BUZZ_ALLOW_PRIVATE_RELAYS`, mounts the routes, builds the `buzzrelay.Service`, and starts a `relaykit.Supervisor` over `ListActiveBuzzConnections` (lock key `DefaultLockKey` = 80920034, `BUZZ_GATEWAY_LOCK_KEY`, `BUZZ_GATEWAY_SINGLE_INSTANCE`).
- **Gateway (`plugin.go`):** one `buzzrelay.Session` per community row, a subscription per bound channel (`b:<binding id>`); connection states are recorded on the community, and a subscription the relay closes for access marks its binding broken.
- **`Handler` (`handlers.go`):** identities (generate the persona's key, pause, remove, publish the profile), communities (join by open relay, invite with join terms, or an admin adding the npub; optional owner attestation; check again; pause), channels (list, join a public one), bindings (create with a sandboxed relay thread or an acknowledged existing one), a thread's Buzz state and the composer toggle.
  After a change it kicks the supervisor so the gateway reconciles at once.

## Key types and entry points

- `Handler`, `Store`, `Relay`, `AgentResponse`, `ChatState`, `DefaultLockKey`.

## Dependencies

- **Inbound:** `internal/plugins.Apply` during server setup.
- **Outbound:** `internal/buzzrelay` (and `nostr`), `internal/relaykit`, `internal/datastore`, `internal/plugins`, `internal/replyhook`, `internal/handlers/handlerutils`, `internal/middleware`, `internal/storage`, `internal/models`.

## Non-obvious decisions

- **The server holds the persona's key, never the person's.**
  The secret is generated here, stored encrypted, and never returned; an owner attestation is pasted (verified for the persona's key) and stored encrypted too.
- **Join terms are the owner's to accept**, in the version shown and with their own age confirmation; until then a claim answers 409 `buzz_terms_required` with the terms in the envelope's details.
- **A relay URL is user input**, so routes accept only https/wss and the client dials only public addresses unless `BUZZ_ALLOW_PRIVATE_RELAYS` (local development).
- **Ownership is the datastore's job**: every store call is owner-scoped, and another account's rows are a 404.

## Testing

- `handlers_test.go` uses a fake store and relay: terms and age flow, attestation checks, access states, sandboxed relay threads and the unrestricted acknowledgement, key normalisation, ownership, and the gateway kick.
