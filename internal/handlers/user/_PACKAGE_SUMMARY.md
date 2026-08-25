# Package: `internal/handlers/user`

## Role

HTTP API for **users** — profile, preferences, registration/login-adjacent flows, and invite handling per `user.go` and related files.

## Responsibilities

- **`Handler`:** `/api/...` user routes (exact paths in `user.go`); uses `internal/auth` and datastore.
- Registration validates signup policy before user creation: production rejects `+` email aliases, and a configured email allowlist (`ALLOWED_EMAILS`) applies in every environment — empty allowlist permits all emails.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/auth`, `internal/models`, `mux`, `zap`.

## Non-obvious decisions

- Cognito vs legacy JWT flows — follow comments in middleware and this handler when changing auth.
- Production's plus-alias restriction is driven by `server.Config.Environment` (`ENV`, then `ENVIRONMENT`); the email allowlist is configured via `ALLOWED_EMAILS`. Both checks run before datastore/Stripe side effects.

## Testing

- `user_test.go` — selected HTTP behaviors.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **HTTP Layer** and auth.
