# Package: `internal/providerconnections`

## Role

How accounts reach model providers (ADR 0x027): the `provider_connections` table, the presets that fill a connection in, listing a provider's models, and the routes an account manages its connections through.

## Responsibilities

- **Storage (`store.go`):** `Store` is the only code that writes `provider_connections`.
  A connection has a preset, a name, an optional flavor and address, and an optional key, encrypted with the deployment's token key (`Sealer`) and write-only: `Connection` carries only `has_key` and the last-four `hint`.
  Built-in connections (`SetBuiltinKey`, `Builtins`, `DeleteBuiltin`, `BuiltinKey`) back `internal/accountkeys`; at most one per built-in provider per account, enforced by the unique `(user_id, builtin)` index.
  `Create`, `Update`, `Delete` and `List` serve any connection; `List` puts built-in connections first.
- **Presets (`presets.go`):** `Presets` lists the eight built-in providers (at the addresses the deployment reaches them at), OpenRouter, Ollama, LM Studio and Custom.
  Ollama and LM Studio default to the deployment's `LOCAL_SERVER_HOST` (`localhost`, or `host.docker.internal` in the container image), and their address is editable, as Custom's is.
  A connection given exactly its preset's address stores none, so it follows the preset if that default changes.
  `KeysURL` says where to create a key, for the presets where that is known.
- **Listing (`listing.go`):** `Lister.List` reads a provider's models through its OpenAI-compatible `GET {base}/models` or Anthropic-compatible `GET {base}/v1/models`, sorted by id.
  Anthropic pages are walked by hand, going on only while `has_more` is true and the cursor moved (the SDK's auto-pager never checks that, so a server ignoring `after_id` would be asked until the timeout); every listing stops at 50 pages or 5,000 models.
  A failure is a `ConnectError` with a stable code: `provider_unreachable` (network or timeout), `provider_key_rejected` (401/403), `provider_unavailable` (5xx or 429) or `provider_incompatible` (anything else, such as a 404 or a page that is not JSON).
  Under the mock LLM backend it answers a fixed list and sends nothing.
- **Routes (`routes.go`):** on the authenticated router, mounted with the key routes only when accounts supply keys.
  `GET /provider-connections`, `POST /provider-connections` (lists first, stores only on success, answers with the connection and its models), `GET /provider-connections/presets`, `PATCH` and `DELETE /provider-connections/{id}`, `GET /provider-connections/{id}/models` (the live listing with which models are enabled) and `PUT /provider-connections/{id}/models` (enable and disable).
- **Connection models (`models.go`):** enabling a model creates its row on the `models` table (`connection_id` set, `provider` the connection's preset), or brings back the one disabled before; disabling soft-deletes it, so anything naming it fails clearly.
  A connection's models are deleted with it (the foreign key cascades), and neither the delete nor disabling may take away the account's default model (`default_model_in_use`).
  Capabilities come from the provider's listing where it reports them (OpenRouter: supported parameters and input modalities; Ollama: its native `/api/show`, asked per model after listing, best effort, since its OpenAI-compatible list reports only ids), then from what is known about the API (Anthropic-compatible servers take tools; Anthropic's own models take images); the rest the account confirms when enabling.
  Adding a connection, or changing its key or address, needs the owner's password (`reauth.Confirmer`); a rename or a delete does not.
- **`CustomAddressesAllowed`:** whether accounts may choose an address.
  True by default; a deployment that serves the public binds it to false in an `init()`, which drops those presets from the list and refuses them with `custom_address_not_allowed`.
  `GET /provider-access` reports it.

- **Provider quirks, found by listing each real provider with a key it does not know (2026-10-29):** OpenAI, Anthropic, Mistral, DeepSeek, Qwen and Xiaomi answer 401 from the model list itself.
  z.ai's Anthropic-compatible model list answers without checking the key, so at its default address z.ai is listed through its OpenAI-compatible endpoint, which does; pointed elsewhere by `ZAI_BASE_URL`, it is listed where it was pointed, so the key never goes to a host the deployment did not configure.
  OpenRouter's model list is public, so its key is also checked against `/api/v1/key`.
  Google answers a bad key with 400, which for Google alone counts as a rejected key.
  Each quirk is a field on its preset (`Target`), not a branch in the listing code.

## Dependencies

- **Inbound:** `internal/server` (builds the store, lister, presets and routes), `internal/accountkeys` (built-in connections), `internal/handlers/provideraccess` (`CustomAddressesAllowed`).
- **Outbound:** `ent` (`ProviderConnection`), the OpenAI and Anthropic SDKs (model services only), `internal/telemetry` (error classification), `internal/reauth`, `internal/middleware`, `internal/handlers/handlerutils`, `internal/models`.

## Non-obvious decisions

- **Listing never sends the deployment's keys.**
  The SDK clients' defaults read `OPENAI_API_KEY`, `OPENAI_ORG_ID`, `ANTHROPIC_API_KEY` and `ANTHROPIC_AUTH_TOKEN` from the environment, so the lister builds the SDKs' model services directly, without those defaults.
  A connection without a key sends no credential at all.
- **Listing does not go through the shared provider client.**
  Its credential transport replaces the key on any request to a built-in provider's host with the account's stored one, so a key being tested would never be the one sent.
  The server gives the lister its own client: the network block, the call timeouts and the instrumentation, without the credential transport.
- **Status codes are read from the SDKs' error types here,** not through `telemetry.ClassifyError`'s registry, which only knows them once `internal/agent/provider` has loaded.
- **A provider's error text is logged, never returned,** and the key is cut out of it where the `ConnectError` is made: a server can echo what it was sent, and no log line may contain a key.
- **`CustomAddressesAllowed` guards every route that reaches an account-chosen address** (adding, a new key or address, listing models), so turning it off also stops connections already stored; renaming and deleting reach nothing and stay allowed.
- **Nothing is cached:** each listing asks the provider.
- **The password check comes before any outbound request,** so an unconfirmed session cannot make the server reach an address.

## Testing

- `listing_test.go` — against local test servers: a key echoed in a provider's error is redacted (mutation-checked); the connection's key is sent and the deployment's environment keys never are (mutation-checked: building the SDK client the usual way fails it), Anthropic paging, the stable code for each failure, the mock listing, address checks, and the presets, Anthropic paging that stops when the cursor does not move (mutation-checked) or at the page cap, z.ai listed at the configured address, `keys_url`, the capabilities OpenRouter reports, Ollama's `/api/show` (a failed lookup leaves that model unknown), and what is known about each API.
- `routes_test.go` — over in-memory SQLite (tables written by hand): adding a built-in connection that the key resolver then reads, a second one refused before any call (`provider_connection_exists`), a local server without a key and an address stored only when it differs from the preset's, Custom (any number), bad input rejected before any call, the password checked before any call (mutation-checked), a failed listing storing nothing, custom addresses turned off (for adding, and for connections already stored), edits (rename without a password; a new key tested and saved; a dead address leaving the connection as it was), a fixed built-in address, listing and deleting, other accounts' connections, and that every route needs an account; enabling models (shown in the listing with their capabilities, stored with the preset and connection), disabling and enabling again keeping the row, the default model protected from disabling and from its connection's removal, models going with their connection, and bad choices and other accounts refused.

## Related documentation

- [ADR 0x027](../../docs/adr/0x027-provider-connections.md)
- [`internal/accountkeys`](../accountkeys/_PACKAGE_SUMMARY.md)
- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md)
