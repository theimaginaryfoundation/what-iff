# Package: `internal/handlers/memory`

## Role

HTTP API for **semantic memories** — CRUD, search, plus export/import portability at `/api/memory/...`.

## Responsibilities

- **`Handler`:** Memory routes wired in `handler.go`; uses datastore memory layer and vector search.
- **Import/export:** `export.go` streams memory ZIP downloads; `import.go` accepts a ZIP upload and triggers UUID-deduped import with embedding regeneration.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/models`, `mux`, `zap`.

## Non-obvious decisions

- Search parameters and filters must stay aligned with `internal/datastore/memory.go` and OpenAPI.
- `NewHandler` takes an optional `*http.Client` for its OpenAI embeddings client; under a non-vendor `LLM_BACKEND` (mock/local) the server passes the deny-network client so import embeddings cannot reach the provider (ADR 0x018).

## Testing

- `import_test.go` covers import availability gating and multipart body-size enforcement.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — memories and pgvector.
