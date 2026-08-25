# Personal Assistant Web App

Angular frontend for the Personal Assistant product.

## Purpose

This app provides authenticated user workflows for:

- chat and message history
- memory management
- personality and ritual management
- agent jobs
- billing/subscription UX
- integrations management (Connectors + Webhooks)

## Integrations UI

The Integrations route (`/integrations`) is a tabbed page with:

- **Connectors** (default): premium remote MCP connector CRUD and defaults.
- **Webhooks**: webhook API token lifecycle management.

Webhook token management calls:

- `GET /api/webhook-tokens`
- `POST /api/webhook-tokens`
- `DELETE /api/webhook-tokens/{id}`

The UI displays the newly created token value one time and supports copy-to-clipboard to aid secure handoff to external systems.

## Local Development

From this directory:

```bash
npm install
npm start
```

The app runs at `http://localhost:4200`.

## Build

```bash
npm run build
```

## Tests

```bash
npm test
```

Runs once via Vitest (no watch mode, no browser needed).

## Build provenance (`/version.json`)

`scripts/write-version.mjs` writes `public/version.json` — `version`, `commit`,
`built_at`, and (for a downstream overlay build) `overlay_commit` — the
frontend counterpart of the API's `GET /api/version`. It's derived and
gitignored: never hand-edit it.

It runs as a `pre*` npm hook (`prestart`, `preserve:coverage`, `prebuild`,
`prebuild:prod`, `prebuild:docker`, `prewatch`) rather than being called
directly, so **any new serve/build entry point added to `package.json` must
get its own `pre<name>: node scripts/write-version.mjs` hook** — nothing
enforces this automatically, and a missed one means that path silently
serves a 404 or stale `/version.json` (see the `preserve:coverage` history:
this is exactly the gap that originally broke CI).

## Notes for Contributors

- Keep API contract updates aligned with backend `openapi.yaml`.
- For Integrations feature work, update:
  - `src/app/features/integrations/`
  - `src/app/core/services/*`
  - `src/app/core/models/*`
- When adding new API calls, include service tests under `src/app/core/services/*.spec.ts`.
