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
  The invalid index a failed build leaves is dropped first, because ent's inspection ignores `indisvalid` and would skip it; if that drop fails for a unique index, it returns an error instead of booting without the constraint.
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
- **No seeded catalog (ADR 0x027):** three separate steps, each where it applies, so seeding and converting can't be confused.
  `SeedModelCatalog()` reports whether boot seeds the catalog (`SeedCatalog`, from `models.AvailableModels`) on a real backend; it is false, and a build that wants a seeded catalog there, such as a development database, replaces it in an `init()`.
  It is a function because it is asked after configuration has loaded; a package `init()` runs before that.
  `RequireCatalogDefault` fails fast unless `DEFAULT_MODEL_NAME` names an active catalog model; `cmd/api-server` asks it only where the deployment supplies the keys, and so curates the catalog.
  `ConvertSeededModels` (`convert_seeded.go`, run by `cmd/api-server` after the stored-key conversion, under the `vendor` backend, and only where accounts bring their own keys, `providerkeys.AccountsSupplyKeys()`) moves an existing install's seeded models onto the accounts that use them, as a default, in a chat, in an agent job or as a favourite: each is enabled on the account's built-in connection for its provider and those uses point at it.
  An account with no connection for the provider keeps nothing (its default and chat models are cleared, the favourite dropped, and a job naming the model fails because it no longer exists), and the seeded rows are removed; models the deployment added itself stay.
  An account left with enabled models but no default gets one of its converted models as its default.
  It runs once per install, in one transaction, and records that it ran in the `markers` table (`seeded_catalog_converted`), so a catalog model an operator adds later under a seeded name is never taken for a seeded one; on Postgres an advisory lock makes two instances starting at once convert one after the other.
  The mock and local test backends always seed (`SeedCatalog`): their end-to-end suites and visual baselines start from the seeded models.
  Role seeding runs either way.
- **No default model needed:** `CreateOrPromoteAdmin` gives the admin the catalog's default model when there is one, and none otherwise, like any new account.

## Testing

- *(Typically integration-tested via app startup; add focused tests if connection logic grows.)*
- `concurrent_indexes_test.go` — the DDL and what is left to the migration, that the memory list indexes lead with the owner column, and the non-PostgreSQL no-op.
- `foreign_keys_test.go` — every foreign key into `mcp_servers` cascades on delete, so a server with OAuth sessions can be deleted.
- `poolmetrics_test.go` — pool gauges against an in-memory sqlite `*sql.DB`.
- `seed_test.go` — over sqlmock: `EnsureSeedData` reads no model rows at all, `RequireCatalogDefault` fails without the default, and catalog seeding is off by default.
- `convert_seeded_test.go` — over in-memory SQLite (tables from the ent schema, without foreign keys): an account with the provider's connection gets the model there (an existing one reused and enabled again) with its uses repointed, favourites included; one without has its default and chat cleared and its job left on the removed model (mutation-checked); an account whose default went is given one of its converted models; seeded rows removed, the deployment's own kept; a second run does nothing, even for a catalog model added later under a seeded name, and an install with nothing seeded is marked too.
- `ritual_links_test.go` — the link backfill against in-memory sqlite with foreign keys on, shaped like a non-destructive Postgres migration: moves once and clears the columns, skips links to deleted modes and jobs, keeps a removed link removed across restarts, and is a no-op without the legacy columns.
- `file_origins_test.go` — the origin backfill: flagged rows marked once, nothing without the legacy column.
- `ritual_slugs_test.go` — the slug backfill over the generated schema on sqlite: derived slugs, suffixes within an owner, editor-set slugs untouched.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Data layer**.
