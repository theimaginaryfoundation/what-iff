# Package: `internal/handlers/webhook`

## Role

HTTP handlers for the **webhook surface**: static-token management for the signed-in user, and the scoped routes an outside integration calls with that token.

## Responsibilities

- **Token management** (`tokens.go`): create, list and revoke webhook tokens under `/api/webhook-tokens`, authenticated as the account (session JWT).
  Creation takes the token's scopes and stores exactly those; omitting them gives `messages:write` only.
- **Send** (`messages.go`): `POST /api/webhooks/chat/{chatId}/messages` in `user`, `assistant` or `background` mode.
- **Read** (`reads.go`, `read_params.go`): `GET /api/webhooks/chat`, `/personality`, `/chat/{chatId}/messages`, `/chat/chat-message/{id}` and `/job/{id}`.
  Each mirrors a session-API read and calls the same datastore method, but returns an allow-listed shape.
- Does **not** authenticate requests or check scopes itself: `middleware.WebhookAuthMiddleware` and `middleware.RequireWebhookScope` do, wired in `RegisterWebhookRoutes`.

## Key types and entry points

| Symbol | Notes |
|--------|--------|
| `Handler`, `NewHandler` | Takes a `Provider` (datastore reads and token storage), a `MessageAgent` (runs sends), and a logger. |
| `RegisterWebhookRoutes` | Mounts the webhook routes and attaches the scope each requires: `messages:write` for the POST, `chat:read` for the five GETs. |
| `RegisterTokenRoutes` | Mounts the session-authenticated token management routes. |
| `pagingFrom`, `*FiltersFrom` | Strict query parsing shared by the list routes. |

## Dependencies

- **Inbound:** `internal/server` (router setup).
- **Outbound:** `internal/datastore` (through `Provider`), `internal/agent` (through `MessageAgent`), `internal/middleware`, `internal/models`, `internal/handlers/handlerutils`, `mux`, `zap`.

## Non-obvious decisions

- **Reads need their own scope.**
  A token with no stored scopes (every token created before scopes existed) is write-only, so adding the read routes gave no existing token read access.
  See [ADR 0x023](../../../docs/adr/0x023-webhook-read-access.md).
- **Responses are a subset, not the internal structs.**
  Messages use `models.WebhookMessage` and personas use `models.WebhookPersonality`, so tool calls, model reasoning, portrait thumbnails, storage keys and a persona's system prompt and scratchpad are never served.
  Threads and jobs are returned as the session API returns them because their JSON already omits private fields.
- **Not yours is not found.**
  The datastore folds ownership into its queries, and `readFailed` maps every "missing or someone else's" outcome (including a job lookup's unauthorized) to the same 404 wording.
- **Malformed filters are rejected, not ignored.**
  The session list routes skip a filter they cannot parse; these return 400, so an integration cannot believe it filtered when it did not.
- **`limit` is clamped to 100** (default 20); every read response is `Cache-Control: no-store`.
- **Reads are audited without content:** `auditRead` logs the operation, token id and row count.

## Testing

- `messages_test.go` — the send routes across the three modes, including a chat owned by another user.
- `reads_test.go` — every read route through a real router with the scope middleware: token and scope enforcement, the allow-listed response shapes, cursor and page paths, malformed input, not-found behaviour, persona filtering, and the audit log.
  `reads_mock_test.go` holds the mock provider's read methods.
- `tokens_test.go` — scope handling at token creation.
- The scope rules themselves are tested in `internal/models/webhook_token_test.go` and `internal/middleware/webhook_scope_test.go`; storage of scopes, including pre-scopes tokens, is in `internal/datastore/webhooktoken_test.go`.

## Related documentation

- [ADR 0x023: Webhook read access and token scopes](../../../docs/adr/0x023-webhook-read-access.md)
- [Integration guide](../../../docs/integrations.md) — capability inventory and recommended patterns.
- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — HTTP Layer.
