# Package: `internal/agent/filechunker`

## Role

**Text file chunking** for RAG: detect text-like MIME types and extensions, split content into overlapping chunks, and **pipeline** chunk creation + embedding + datastore persistence.

## Responsibilities

- **`ChunkText`:** Size/overlap splitting with Unicode awareness (`chunker.go`; well tested).
- **`FileChunkPipeline`:** Orchestrates processing after upload (`pipeline.go`) — uses OpenAI embeddings and datastore.
- **`IsTextType` / `IsTextFileByExtension`:** Gate whether to run the text pipeline vs skip.
  The lists include YAML, JSON Lines and TSV (ADR 0x043), which the upload allowlist accepts.
- A file workspace save re-runs `ProcessAndStore` after `datastore.ClearFileChunks` (`internal/filesave`), since `CreateFileChunks` is idempotent on `chunk_status`.

## Dependencies

- **Inbound:** Attachment ingestion flows from agent or handlers.
- **Outbound:** `internal/agent/embedding`, `internal/datastore`, `internal/utils` (`DecodeTextToUTF8`), OpenAI client.

## Non-obvious decisions

- **`application/octet-stream`** with `.txt`-like extension may still be processed as text (see `pipeline_test.go` cases).
- **Metrics:** `ProcessAndStore` times its `chunk`, `embed` and `store` stages on `whatiff.file.operation.duration` (operation `upload`) and records the chunk count by outcome on `whatiff.file.operation.items`, through `telemetry.Global()`.
  Chunks are embedded in batches of `embedBatchSize` (one `embedding.CreateEmbeddings` request per batch), so the `embed` stage grows with the number of batches, not chunks.
  Each batch request is recorded as its own gen_ai embeddings operation by the embedding package; a failed batch fails the whole file and counts all its chunks as failed.

## Testing

- `chunker_test.go` — chunk boundaries, Unicode, defaults.
- `pipeline_test.go` — MIME/extension gating and unsupported types, plus stage metrics on an embedding failure and per-batch embedding requests (stub OpenAI server).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — file attachments and memory search.
