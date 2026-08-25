# Repository Guidelines

## General Guidelines
- Don't create entire files in one forward pass. Iterate.

## Project Structure & Module Organization
- Backend (Go): `cmd/api-server/main.go`; additional binaries in `cmd/` (e.g. `generate-test-conversations`). Application code in `internal/{handlers,server,auth,agent,datastore,providers,middleware,...}`.
- Data layer: Ent ORM generated code in `ent/`; edit schemas in `ent/schema` and run `make generate` (do not edit generated files in `ent/` directly).
- Frontend (Angular): `web/app/`.
- Docs: `docs/`, `ARCHITECTURE.md`, `openapi.yaml`, `scripts/`.

## Build, Test, and Development Commands
- `make help` — list tasks.
- `make run` — run API locally (reads `.env`); `make run-mock` — same but with the in-process mock LLM (no provider keys/egress; ADR 0x018).
- Local stack helpers: `make check-env`, `make db-up`/`db-down` (Postgres+pgvector in Docker), `make web` (Angular dev server), `make dev-up`/`dev-down` (background API), `make local-superuser` (interactive local admin), `make mock-e2e` (hermetic backend E2E, local/on-demand).
- `make fmt` / `make fmt-fix` — check/fix Go formatting.
- `make vet` — static analysis; `make test` — run Go tests; `make build` — verify the build (cross-compiles a throwaway `linux/amd64` binary; the `bootstrap` output name is a legacy artifact of the retired Lambda target).
- `make tidy` — check `go.mod`/`go.sum` are tidy; `make test-short` — tests without verbose output.
- `make generate` — regenerate Ent code after schema changes.
- `make pre-commit` — run all pre-commit checks (`fmt vet tidy test build check-no-local-models`). CI runs the same formatting/vet/tidy/build checks but regenerates Ent code first and uses `make test-ci` (mock LLM, dummy keys, race detector) as its test gate; the frontend is validated by its own `frontend-pr-validation` workflow. Passing locally is a strong signal, not a guarantee.
- Frontend: `cd web/app && npm install && npm start`.
- Docker stack: `docker compose up --build`.
- **CI stays on `make` wherever a target exists.** A workflow step must not
  inline a raw command that duplicates a Makefile target (e.g. `go generate
  ./ent` instead of `make generate`) — call the target instead, so the
  Makefile stays the single source of truth for what the check actually runs
  and a developer can reproduce a CI failure locally with the same command.
  Exceptions: bare toolchain/cache steps with no repo-specific behavior
  (`go mod download`, `npm ci`), and CI-only bootstrapping that has no local
  dev equivalent (e.g. e2e workflows building and backgrounding a throwaway
  API binary with container-specific ports/env, which `make run`/`make
  dev-up` aren't shaped for). When adding a new CI step, check `make help`
  first; if the step's logic belongs in a target, add one rather than
  inlining it.

## Coding Style & Naming Conventions
- `.editorconfig` enforced.
  - Go: tabs, `gofmt` required; packages lowercase (no underscores); exported identifiers `CamelCase`.
  - JS/TS/JSON/YAML: 2-space indent; Prettier configured in the web app.
- Keep handlers thin; business logic in services/datastore. Avoid editing files under `ent/` manually.
- API changes must update `openapi.yaml` and related docs.

## Testing Guidelines
- Backend: Go `testing` with table-driven tests when appropriate. Files end with `_test.go`; test funcs `TestXxx`. Run `go test ./...` or `make test`.
- Frontend: `cd web/app && npm test`. Runs on Vitest via the `@angular/build:unit-test` builder (jsdom); Karma and Jasmine are gone.
- Frontend coverage: `make web-unit-coverage` / `make admin-unit-coverage` run the same suites with V8 coverage and rewrite the lcov `SF:` paths to repo-root-relative so Codecov's components match. `make lcov-summary LCOV=<path>` prints a total.
- Frontend E2E: Playwright suite in `web/app/e2e/` (`poms/`,
  `fixtures/`, `sdk/`, `tests/{functional,journeys,visual,a11y}`) — run via
  `npm run e2e`/`e2e:mock-llm`/`e2e:local-llm`/`e2e:mock-llm:visual`
  (see `e2e/README.md` for prerequisites). Full guidance: the
  `playwright-e2e` Claude Code skill and `web/app/e2e/README.md`.
- Prefer unit tests with mocks over hitting external services. Cover handlers, services, and critical utils.

## Commit & Pull Request Guidelines
- Conventional Commits (seen in history): `feat:`, `fix:`, `docs:`, `ci:`, etc.
  - Example: `feat(auth): implement JIT user provisioning`.
- Before pushing: `make pre-commit` (or `make install-hooks` once to auto-run checks).
- **If you touched `openapi.yaml`, regenerate the e2e SDK and commit it** — `cd web/app && npm run sdk:generate`. The spec is a frontend build input, so a backend-only PR that skips this fails `frontend-pr-validation`. Rationale and gotchas in the [architecture summary](docs/ARCHITECTURE_SUMMARY.md).
- PRs: clear description, linked issues, focused diff, tests added/updated, docs updated (README/ARCHITECTURE/OpenAPI). Include screenshots for UI changes.

## Architecture & package docs

Read these before reasoning about structure, module relationships, or any
change spanning multiple files — they are the source of truth this file
deliberately does not duplicate:

- **`docs/ARCHITECTURE_SUMMARY.md`** — start here. System architecture: purpose,
  layers, data flow, and cross-cutting rules (agent/model-context, data layer).
  It also defines the repo's documentation map and the `_PACKAGE_SUMMARY.md`
  template.
- **`internal/<package>/_PACKAGE_SUMMARY.md`** — per-package docs: role,
  responsibilities, key entry points, dependencies, non-obvious decisions, and
  testing notes. List them all with `find internal -name _PACKAGE_SUMMARY.md`.
- **`ARCHITECTURE.md`** — the long-form reference behind the summary.

**Anti-drift expectation:** any change under `internal/...` that affects
behavior, public API, dependencies, or documented behavior updates the relevant
`_PACKAGE_SUMMARY.md` — and `docs/ARCHITECTURE_SUMMARY.md` if it touches system
boundaries — **in the same PR**. The summary's "Documentation maintenance"
section is the authoritative version of this rule; follow it there rather than
relying on this summary of it. If the architecture summary itself looks
outdated, flag it rather than silently working around it.

## AI Agent Rules
- Cursor/agent guidance lives in `.cursor/rules/*.mdc` (`general`, `api`, `datastore`, `ent`, `ui`, `uitesting`). Consult the relevant file before making substantive changes in that area.
- `CLAUDE.md` and `GEMINI.md` are intentional symlinks to this file (`AGENTS.md`) so every agent reads one source of truth. Edit `AGENTS.md`; do not replace the symlinks with copies (some editors, Windows checkouts, and archive/CI contexts may not preserve them). Their targets are relative, so any future move of these files must preserve the symlinks or update their targets in the same change.
- Agent skills follow the same one-source-of-truth pattern: `.agents/skills/` is the vendor-neutral source, and `.claude/skills/` contains relative symlinks into it so Claude Code's auto-discovery (which only looks in `.claude/skills/`) still finds them. Edit skill content under `.agents/skills/`; never edit through the `.claude/skills/` symlinks or replace them with copies. `make check-skill-symlinks` (CI-gated on changes under either skills tree or to `AGENTS.md`/`CLAUDE.md`/`GEMINI.md`) verifies every `.agents/skills/<name>` has a matching symlink and that the `CLAUDE.md`/`GEMINI.md` symlinks above exist — run it after adding or renaming a skill.

  For what each skill covers, read `.agents/skills/<name>/SKILL.md` — its
  frontmatter `description` is the summary. Listing them here as well only
  created a second copy to keep in sync, and the copy is the one that goes
  stale: a skill added or renamed under `.agents/skills/` is discovered
  automatically, but nothing makes this file follow. `ls .agents/skills/` is
  the authoritative list.

  Two notes that aren't derivable from the skills themselves: the
  `playwright-cli`, `playwright-component-testing`, and `playwright-trace`
  skills are generated by `npx playwright init-skills --loop agents` and should
  be refreshed with that command rather than hand-edited, and
  `playwright-component-testing` covers React/Vue component testing, so it is
  likely inapplicable here (this repo is Angular, e2e-only).

## PR feedback triage

To address the review feedback on a pull request, produce a triage plan first —
don't edit blind. The workflow lives in the `address-pr-feedback` skill:

- Collector: `.agents/skills/address-pr-feedback/scripts/collect.sh [<N>]` —
  resolves the PR (most recent authored by default) and returns reviews + inline
  + conversation comments as one pre-filtered JSON blob (regression tables and
  bare bot approvals dropped; structured findings and human comments kept).
- Method: `.agents/skills/address-pr-feedback/SKILL.md` — isolate in a worktree,
  triage by **author-first routing** (human comments are never filed as noise),
  present a grouped `file:line` plan.

Read-only — no commits, pushes, or PR replies. Claude Code triggers the skill
automatically; other agents and humans run the collector and follow SKILL.md.

## Security & Configuration Tips
- Never commit secrets. Copy `.env.example` → `.env`. Typical vars: `OPENAI_API_KEY`, `JWT_SECRET`, `JWT_REFRESH_SECRET`; optional: `AUTO_MIGRATE=true`, `SERVER_PORT`.
- Deployment: ships as a standard container (see the root `Dockerfile`), runnable on any container platform. Infrastructure-as-code for the maintainers' hosted deployment lives outside this repository and is not required to run the app.
