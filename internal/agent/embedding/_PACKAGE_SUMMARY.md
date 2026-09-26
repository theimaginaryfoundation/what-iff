# Package: `internal/agent/embedding`

## Role

**OpenAI embedding** API wrapper: single-call embedding vector creation used by memory search, file chunk search, and related datastore flows.

## Responsibilities

- **`CreateEmbedding`:** Takes context, OpenAI client, and input string; returns `[]float32` vector.
- **`CreateEmbeddings`:** One Embeddings API call for a batch of inputs, returned in input order; every embedding caller goes through it.
- **Metrics:** each call records `gen_ai.client.operation.duration` (operation `embeddings`, model, `call_path` from context, `error.type` on failure) and its input tokens on `gen_ai.client.token.usage` / `whatiff.gen_ai.tokens`.
  It records through `telemetry.Global()`, since callers pass only an OpenAI client.

## Dependencies

- **Inbound:** `internal/agent/tools` (search), `internal/datastore` paths that embed text.
- **Outbound:** OpenAI SDK, `internal/telemetry`.

## Non-obvious decisions

- Errors propagate to callers for retry/logging; no caching at this layer.

## Testing

- `embedding_test.go` — typically mocked client or integration-style (see file).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **Data layer** (pgvector) and agent tools.
