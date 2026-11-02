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
- **`BackfillRitualLinks`** (`ritual_links.go`): after the schema migration, moves mode and job skill links from the one-to-many columns ent used to keep on `rituals` (`mood_rituals`, `agent_job_rituals`) into the join tables of the same name (ADR 0x035 §11).
  Per column, in one transaction, it copies the links whose mode or job still exists and then clears the column.
  A non-destructive migration keeps the columns but drops their foreign keys, so a value left behind would point at a deleted mode or bring back a removed link on the next boot.
  `cmd/api-server` runs it between a non-destructive `Schema.Create` and the destructive pass, so `DESTRUCTIVE_MIGRATION=true` cannot drop the columns before they are copied.
  Idempotent: a no-op once the columns are cleared or dropped; `cmd/api-server` calls it on every start that migrates.
- **`PrebuildIndexes`** (`concurrent_indexes.go`): just before the ent migration, builds with `CREATE INDEX CONCURRENTLY` every plain index the generated schema (`ent/migrate.Tables`) declares on an existing table but the database lacks, under ent's own name and columns, so the migration finds it present instead of building it with a write-blocking `CREATE INDEX`.
  New tables, columns the migration adds, annotated (partial, typed, op-class) indexes and over-long names are left to the migration; a failed build is logged and left to it too.
  An invalid index left by an interrupted build is dropped and rebuilt; PostgreSQL only.
- **`BackfillFileOrigins`** (`file_origins.go`): marks rows the legacy `file_attachments.agent_authored` flag set with `origin = 'agent'` (the enum that replaced the flag, ADR 0x035); idempotent and a no-op once the column is dropped.
- **`BackfillRitualSlugs`** (`ritual_slugs.go`): gives rituals created before the `slug` column a slug derived from their name, unique per owner (`skills.Slugify` + `skills.UniqueSlug`), so the partial unique index can hold; also called on every migrating start and finds work once.

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
- **Model catalog seeding is switchable:** `EnsureSeedData` seeds the catalog from `models.AvailableModels` only when `SeedModelCatalog()` returns true, which it does by default.
  A self-hosted install needs the seeded catalog until models are chosen per account.
  A build whose operator curates the catalog replaces the function in an `init()`, so boot never adds a model the operator did not choose, with whatever pricing and tier the schema defaults give it.
  It is a function because it is asked when `EnsureSeedData` runs, after configuration has loaded; a package `init()` runs before that.
  Role seeding and the check that the default model exists run either way.

## Testing

- *(Typically integration-tested via app startup; add focused tests if connection logic grows.)*
- `concurrent_indexes_test.go` — the DDL and what is left to the migration, that the memory list indexes lead with the owner column, and the non-PostgreSQL no-op.
- `foreign_keys_test.go` — every foreign key into `mcp_servers` cascades on delete, so a server with OAuth sessions can be deleted.
- `poolmetrics_test.go` — pool gauges against an in-memory sqlite `*sql.DB`.
- `seed_test.go` — `EnsureSeedData` over sqlmock: with catalog seeding off it issues no catalog query, and with it on the catalog step runs.
- `ritual_links_test.go` — the link backfill against in-memory sqlite with foreign keys on, shaped like a non-destructive Postgres migration: moves once and clears the columns, skips links to deleted modes and jobs, keeps a removed link removed across restarts, and is a no-op without the legacy columns.
- `file_origins_test.go` — the origin backfill: flagged rows marked once, nothing without the legacy column.
- `ritual_slugs_test.go` — the slug backfill over the generated schema on sqlite: derived slugs, suffixes within an owner, editor-set slugs untouched.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Data layer**.
