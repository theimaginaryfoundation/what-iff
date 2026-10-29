# ADR 0x027: Provider connections, the models they serve, and utility models

- **Status:** Proposed
- **Date:** 2026-10-29
- **Deciders:** What Iff maintainers

## Context

An account reaches a model provider through a key it stores in the app (`internal/accountkeys`, table `account_provider_keys`).
That covers the eight built-in providers, each at the address the deployment configured, with one key per provider per account.
It cannot express a provider the app does not already know: an OpenAI-compatible aggregator such as OpenRouter, a model server on the same computer such as Ollama or LM Studio, or any other compatible server.

Three things in the current design stand in the way:

- **A key is tied to one of eight fixed providers.** There is nowhere to store a name, an address or an API flavor, and an account cannot hold two keys of the same kind.
- **Provider clients are built once, at startup.** Each built-in provider gets one client at the address in the deployment's configuration. Keys are added per request by a shared HTTP transport that picks the key by the request's host, using rules also built at startup (`provider.CredentialHTTPClient`). An address only an account knows has no rule to match.
- **The provider list is fixed in the schema.** `models.provider` is an enum, so every new provider needs a database migration.

Background work is fixed too.
Memory extraction, the scratchpad, summaries, recall, chat naming, mode and expression selection, schedule parsing, personality generation, portraits and memory embeddings each call a hard-coded OpenAI model.
That is why an OpenAI key is required today, whichever model an account chats with.

## Decision

### 1. A provider connection is the unit

A connection is one way to reach one provider:

| Field | Meaning |
|---|---|
| `owner` | The account it belongs to, or empty for the deployment's own connection. |
| `preset` | A built-in provider (`openai`, `anthropic`, …), a known third party (`openrouter`, `ollama`, `lmstudio`), or `custom`. |
| `flavor` | The API it speaks: OpenAI-compatible or Anthropic-compatible. Implied by a preset, chosen for `custom`. |
| `name` | What the account sees. Defaults to the preset's name. |
| `base_url` | Empty means the preset's address. Required for `custom`. |
| `key` | Optional, encrypted at rest like stored keys are today, write-only, with a last-four hint. |

Connections live in a new table rather than widening `account_provider_keys`.
The connection, not the key, is what everything else refers to.

### 2. Who can own a connection, and what the deployment reports

A connection belongs to an account or to the deployment.
Models inherit visibility from their connection (section 5), so ownership is stated once.

Two facts are reported separately on `GET /provider-access`, rather than one "self-hosted or hosted" switch:

- **Whether accounts may add connections.** This is today's `keys_from: account`.
- **Whether Custom addresses are allowed.** On by default, since reaching a server on the same computer is the point of Custom. A deployment that serves the public turns it off.

Allowing Custom addresses on a public deployment needs a real guard against server-side request forgery. That means checking the resolved address against private and reserved ranges, following no redirects, and capping response size and time, all on every request. That guard is separate work and not part of this ADR.

### 3. Stored keys become connections

At startup, each `account_provider_keys` row becomes a preset connection for the same account, and the row is removed.
The ciphertext is copied as-is, with no decryption.
The step is idempotent, and does nothing on a deployment that supplies its own keys, whose table is empty.

Until models point at connections (section 5), an account has at most one connection per built-in preset.
The account resolver then answers "this account's OpenAI key" from its `openai` connection, and its interface does not change.
`/provider-keys` stays as a thin layer over preset connections, so existing clients keep working.

After the conversion, the Providers screen shows a one-time, dismissible notice that saved keys are now connections.
The converted connections carry the preset's plain name.

### 4. Connecting lists the provider's models

Adding a connection checks it before saving: the request lists the provider's models, and saves only if the listing works.
On failure nothing is saved, and the response carries a stable code for what went wrong: unreachable, key rejected, not a compatible server, or the account password not confirmed.
The frontend keeps the form filled in and points at the field to fix.

Listing depends on the flavor: `GET {base}/models` for OpenAI-compatible, and `GET /v1/models` for Anthropic-compatible.

A listing call uses a client built for that request, with the connection's key passed directly.
It does not go through the shared credential transport, which would replace a key being tested with the account's stored one.
It keeps the call timeouts, the telemetry, and the network block that applies when `LLM_BACKEND` is not `vendor`.
Under the mock backend, listing returns a fixed set so end-to-end tests can run without a provider.

### 5. Models hang off connections

A model row records the connection that serves it, and you can see a model if you can use its connection.
Models have no owner of their own, so visibility cannot drift from the connection.

- **Rows are created on enable.** "Found 312 models" is a live listing, not 312 rows. A re-scan adds new models and flags ones the provider no longer lists.
- **"Enabled" has one meaning:** offered to whoever can use the connection. A model that is not enabled cannot be chosen for chat or for a utility role.
- **Deleting a connection disables its models** rather than deleting them. Anything still referring to one fails with `provider_not_configured`, and the delete warns first, saying how many enabled models it disconnects.
- **Once models point at connections,** a model's requests use its own connection's key, the one-per-preset limit lifts, and a new provider no longer needs a migration.

Chat through a Custom OpenAI-compatible connection must stream.
It builds on the existing Chat Completions streaming path, not on the non-streaming local adapter.

What happens when two connections an account can use serve the same model is left for later. Nothing here needs it decided.

### 6. Utility models

Background work is grouped into **utility roles**, each assigned a model.
A role is a row in a list, so new kinds of background work add a role rather than another hard-coded provider.

| Role | Covers | Today |
|---|---|---|
| Memory and summaries | Memory extraction and merge, the scratchpad, checkpoint summaries, recall, thread rehydration | `gpt-5.6-luna` (memory and scratchpad use `claude-haiku-4-5` in chats with a Claude model) |
| Quick tasks | Chat naming, expression and mode selection, schedule parsing, memory dedupe | `gpt-5.6-luna` |
| Personality generation | Generating a personality | `gpt-5.1` |
| Images | Portraits and expression sets | `gpt-image-2.5-flare` |

Each role has a deployment default, which an account can override with a model from a connection it can use.
Whether accounts may override is a separate fact, like those in section 2.
Settings are keyed by scope and role, so a scope other than "deployment" or "account" can be added later.

Utility calls go through one provider-agnostic path that routes by the model's connection, the way subagents already route by model.
Most utility calls ask for structured output, so the model capability test suite checks it, for each model on its connection.
A role whose model cannot be reached fails with `provider_not_configured`, and a failure in background work is shown to the account, not only logged.

**Embeddings are out of scope.**
Both vector columns are fixed at 1536 dimensions, and vectors from different models cannot be compared, so changing the embedding model means re-embedding every memory and file chunk, whatever its size.
Until that work is done, memory embeddings stay on `text-embedding-3-small`, and an OpenAI connection stays required for them.

## Rollout

Each step merges before the next begins.

1. **Connections replace the key table** (sections 1 and 3). No visible change.
2. **Connection routes, model listing, the new presets and the Custom-address fact** (sections 2 and 4).
3. **The Providers screen** becomes a list of connections with one "Add provider" form, where a preset fills in the details.
4. **The models screen** (section 5).
5. **Utility models** (section 6).
6. **The model capability test suite.**

## Consequences

- An account can use OpenRouter, a local model server or any compatible server, not only the built-in providers.
- After step 5, nothing but embeddings requires an OpenAI key.
- A new provider no longer needs a migration, a configuration variable or a new client at startup.
- Deployment-owned connections make it possible to add a provider for everyone without a restart. Nothing in this ADR adds a screen for that.
- Listing a provider's models replaces typing model IDs by hand.
- The conversion in step 1 is one-way. A build from before it cannot read keys stored after it.
- Each request to a connection builds its client. That is cheap, and leaves no cache to invalidate when a connection changes.

## Alternatives considered

- **Widen `account_provider_keys`.** Add a name, an address and a flavor to the existing table. Rejected: its identity is "an account's key for a provider", and its unique index enforces exactly that. The connection is a different unit, and every later part of this design refers to it.
- **An owner on model rows.** Simple, but ownership would then be stated twice, once on the connection and once on each model, and could drift.
- **Rows for every listed model.** Rejected: an aggregator lists hundreds of models and changes its list constantly, so every database would accumulate a catalog nobody chose.
- **Per-host credential rules for account connections.** Extend the startup rules at runtime. Rejected: the rules are built once and shared, and a key being tested would be replaced by the stored one.
- **Utility models chosen only by the deployment.** Rejected: background work runs with the key of the account it is for, so a single deployment-wide choice fails for every account without a connection to that model's provider.
