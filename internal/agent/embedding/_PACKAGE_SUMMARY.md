# Package: `internal/agent/embedding`

## Role

**OpenAI embedding** API wrapper: single-call embedding vector creation used by memory search, file chunk search, and related datastore flows.

## Responsibilities

- **`CreateEmbedding`:** Takes context, OpenAI client, and input string; returns `[]float32` vector.

## Dependencies

- **Inbound:** `internal/agent/tools` (search), `internal/datastore` paths that embed text.
- **Outbound:** OpenAI SDK.

## Non-obvious decisions

- Errors propagate to callers for retry/logging; no caching at this layer.

## Testing

- `embedding_test.go` — typically mocked client or integration-style (see file).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **Data layer** (pgvector) and agent tools.
