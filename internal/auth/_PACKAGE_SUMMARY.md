# Package: `internal/auth`

## Role

**Authentication primitives:** JWT signing/validation, password hashing, invite-code handling, and related helpers used by middleware and user lifecycle flows.

## Responsibilities

- JWT creation and verification for session-style auth (exact surface in `*.go` files in this directory).
- Password hashing (bcrypt or similar — see `password.go`).
- Cognito-related helpers may live alongside or in middleware; **this package** holds shared crypto and token utilities consumed by `internal/middleware` and handlers.

## Key types and entry points

- Inspect exports in `password.go` and any JWT helpers for `Sign`/`Verify` style APIs used at login and on protected routes.

## Dependencies

- **Inbound:** `internal/middleware`, `internal/handlers/user`, registration flows.
- **Outbound:** `golang.org/x/crypto`, `jwt` library — see imports.

## Non-obvious decisions

- **JWT validators are pinned to HS256 exactly** (`jwt.go`): both `ValidateAccessToken` and `ValidateRefreshToken` check `token.Method.Alg()` against HS256 and pass `jwt.WithValidMethods`, rather than accepting the whole HMAC family — a token signed with any other algorithm is rejected outright.
- **`ValidateSecret` (`secret.go`) gates JWT_SECRET/JWT_REFRESH_SECRET at boot**, called from `cmd/api-server/main.go`'s `init()` (mirroring `datastore.ValidateTokenEncryptionSecret`'s TOKEN_ENCRYPTION_SECRET check right next to it): a missing, under-`MinSecretLen`, or known-placeholder value (anything that has ever shipped as a `docker-compose.yml`/`.env.example` default) is a fatal startup error, not a runtime 500 the first time someone logs in. The secrets themselves are still read ad hoc via `os.Getenv` inside `generateAccessToken`/`generateRefreshToken`/`ValidateAccessToken`/`ValidateRefreshToken` — this only adds a redundant fail-fast gate at boot, it does not thread the values through `server.Config`.

## Testing

- `secret_test.go` covers `ValidateSecret`'s length and placeholder-rejection cases.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **HTTP Layer** (`internal/auth` bullet).
