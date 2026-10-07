# Package: `internal/handlers/memory`

## Role

HTTP API for **semantic memories** — CRUD, search, plus export/import portability at `/api/memory/...`.

## Responsibilities

- **`Handler`:** Memory routes wired in `handler.go`; uses datastore memory layer and vector search.
- **Batch actions:** `batch_actions.go` — `POST /memory/batch/delete` and `POST /memory/batch/patch` for bulk delete / move / archive (`all_or_none` supported).
- **Embedding on write:** `embed.go` — after `POST /memory`, `POST /memory/batch`, and content-bearing `PATCH /memory/{id}` / `POST /memory/batch/patch` commit, the handler embeds whichever of the saved memories has no embedding (new rows, or rows whose content changed, since the datastore drops a stale embedding on edit).
  It is best-effort: a provider or store failure is logged, the save still returns success, and the agent's startup `BackfillMemoryEmbeddings` pass picks the row up on the next restart.
- **Import/export:** `export.go` streams memory ZIP downloads; `import.go` accepts a ZIP upload and triggers UUID-deduped import with bounded OpenAI embedding batches and bulk persistence.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/models`, `mux`, `zap`.

## Non-obvious decisions

- Search parameters and filters must stay aligned with `internal/datastore/memory.go` and OpenAPI.
- `NewHandler` takes an optional `*http.Client` for its OpenAI embeddings client; under a non-vendor `LLM_BACKEND` (mock/local) the server passes the deny-network client so import and create/edit embeddings cannot reach the provider (ADR 0x018).
- Create/patch handlers reach the datastore through the narrow `memoryWriteStore` interface (`embed.go`) so the write-then-embed flow is unit-testable without a database; `embedTexts` overrides the embeddings call in tests.
- **Provenance:** `GET /memory?provenance=user|external` filters, and `PATCH /memory/{id}` / `POST /memory/batch/patch` accept `provenance` (the owner confirming an external memory sets `user`); invalid values are a 400.

## Testing

- `import_test.go` covers import availability gating and multipart body-size enforcement.
- `batch_actions_test.go` covers batch id/patch parsing and unauthorized delete batch.
- `embed_test.go` covers embedding on create, batch create, patch with content changed / unchanged / absent, batch patch, and saves succeeding when embedding fails.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — memories and pgvector.
