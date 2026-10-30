# Package: `internal/database`

## Role

**Database bootstrap:** opens PostgreSQL (Ent dialect), optionally loads credentials from AWS Secrets Manager, runs migrations, and exposes the `*ent.Client` used by the rest of the app.

## Responsibilities

- Connection string assembly from environment (`DB_HOST`, `DB_PASSWORD`, etc.).
  `DB_PASSWORD` always comes from the process environment — in deployed environments ECS injects it from Secrets Manager via the task definition's `secrets` block (the old in-process `DB_SECRET_ARN` fetch path was removed).
- **`NewClient`:** Returns `*ent.Client` and `*sql.DB` for health checks (see `db.go`).
- **`RegisterPoolMetrics`** (`poolmetrics.go`): samples `sql.DBStats` at each metrics export as `db.client.connection.count` (`db.client.connection.state` = `idle`/`used`) plus cumulative pool waits and wait time.
  Called once from `internal/server`; a rising wait count means `MaxOpenConns` (100) is too low.
- Driver imports: PostgreSQL (and MySQL driver may be present for tooling compatibility — verify `db.go`).

## Key types and entry points

- Functions returning `*ent.Client` and `*sql.DB` (or equivalent) for `internal/server` and tests.

## Dependencies

- **Inbound:** `cmd/api-server`, tests, jobs.
- **Outbound:** `ent`, `database/sql`, AWS SDK (Secrets Manager), `zap`.

## Non-obvious decisions

- **Secrets Manager:** JSON secret shape for password retrieval is documented inline in `getDBPassword`.
- **`DB_SSL_MODE` defaults to `require`** for the `postgres` branch of `NewClient` (`db.go`) — fail closed, since an unset value must not silently mean no TLS.
  `docker-compose.yml` and `.env.example` both set `DB_SSL_MODE=disable` explicitly, because this repo's local Postgres has no TLS listener; that's a documented local opt-out, not the default.
- **Admin provisioning (ADR 0x018):** `CreateOrPromoteAdmin` (`seed.go`) is the single create/promote/password-reset helper with an explicit `AdminProvisionResult`, called only by `cmd/create-superuser` (interactive, TTY-required, local-DB-only).
  The former `SUPERADMIN_*` boot path (`ensureSuperAdmin`) was **removed** after a security review flagged it — no environment variable can mint an admin in a running deployment.
  CI test users use the public register endpoint.
- **No seeded catalog (ADR 0x027):** `SeedModelCatalog()` reports whether the install's catalog is the seeded one (true by default, as on a self-hosted install) rather than one its operator curates.
  A build whose operator curates the catalog replaces the function in an `init()`; boot then leaves that catalog alone, and `EnsureSeedData` fails fast unless `DEFAULT_MODEL_NAME` names an active catalog model.
  It is a function because it is asked after configuration has loaded; a package `init()` runs before that.
  On a seeded install, boot no longer seeds: each account enables its models on its connections.
  `ConvertSeededModels` (`convert_seeded.go`, run by `cmd/api-server` after the stored-key conversion, under the `vendor` backend) moves an existing install's seeded models onto the accounts that use them, as a default, in a chat, in an agent job or as a favourite: each is enabled on the account's built-in connection for its provider and those uses point at it.
  An account with no connection for the provider keeps nothing (its default and chat models are cleared, the favourite dropped, and a job naming the model fails because it no longer exists), and the seeded rows are removed; models the deployment added itself stay.
  It runs in one transaction and is safe to run again or on two instances at once.
  The mock and local test backends seed instead (`SeedTestCatalog`): their end-to-end suites and visual baselines start from the seeded models.
  Role seeding runs either way.
- **No default model needed:** `CreateOrPromoteAdmin` gives the admin the catalog's default model when there is one, and none otherwise, like any new account.

## Testing

- *(Typically integration-tested via app startup; add focused tests if connection logic grows.)*
- `poolmetrics_test.go` — pool gauges against an in-memory sqlite `*sql.DB`.
- `seed_test.go` — `EnsureSeedData` over sqlmock: a curated catalog is never written and must hold its default model; a seeded install reads no model rows at all.
- `convert_seeded_test.go` — over in-memory SQLite (tables from the ent schema, without foreign keys): an account with the provider's connection gets the model there (an existing one reused and enabled again) with its uses repointed, favourites included; one without has its default and chat cleared and its job left on the removed model (mutation-checked); seeded rows removed, the deployment's own kept; a second run does nothing.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Data layer**.
