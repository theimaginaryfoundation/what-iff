# Package: `internal/database`

## Role

**Database bootstrap:** opens PostgreSQL (Ent dialect), optionally loads credentials from AWS Secrets Manager, runs migrations, and exposes the `*ent.Client` used by the rest of the app.

## Responsibilities

- Connection string assembly from environment (`DB_HOST`, `DB_PASSWORD`, etc.). `DB_PASSWORD` always comes from the process environment — in deployed environments ECS injects it from Secrets Manager via the task definition's `secrets` block (the old in-process `DB_SECRET_ARN` fetch path was removed).
- **`NewClient`:** Returns `*ent.Client` and `*sql.DB` for health checks (see `db.go`).
- Driver imports: PostgreSQL (and MySQL driver may be present for tooling compatibility — verify `db.go`).

## Key types and entry points

- Functions returning `*ent.Client` and `*sql.DB` (or equivalent) for `internal/server` and tests.

## Dependencies

- **Inbound:** `cmd/api-server`, tests, jobs.
- **Outbound:** `ent`, `database/sql`, AWS SDK (Secrets Manager), `zap`.

## Non-obvious decisions

- **Secrets Manager:** JSON secret shape for password retrieval is documented inline in `getDBPassword`.
- **`DB_SSL_MODE` defaults to `require`** for the `postgres` branch of `NewClient` (`db.go`) — fail closed, since an unset value must not silently mean no TLS. `docker-compose.yml` and `.env.example` both set `DB_SSL_MODE=disable` explicitly, because this repo's local Postgres has no TLS listener; that's a documented local opt-out, not the default.
- **Admin provisioning (ADR 0x018):** `CreateOrPromoteAdmin` (`seed.go`) is the single create/promote/password-reset helper with an explicit `AdminProvisionResult`, called only by `cmd/create-superuser` (interactive, TTY-required, local-DB-only). The former `SUPERADMIN_*` boot path (`ensureSuperAdmin`) was **removed** after a security review flagged it — no environment variable can mint an admin in a running deployment. CI test users use the public register endpoint.

## Testing

- *(Typically integration-tested via app startup; add focused tests if connection logic grows.)*

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Data layer**.
