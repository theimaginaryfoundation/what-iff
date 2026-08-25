# Package: `internal/agent/filechunker`

## Role

**Text file chunking** for RAG: detect text-like MIME types and extensions, split content into overlapping chunks, and **pipeline** chunk creation + embedding + datastore persistence.

## Responsibilities

- **`ChunkText`:** Size/overlap splitting with Unicode awareness (`chunker.go`; well tested).
- **`FileChunkPipeline`:** Orchestrates processing after upload (`pipeline.go`) — uses OpenAI embeddings and datastore.
- **`IsTextType` / `IsTextFileByExtension`:** Gate whether to run the text pipeline vs skip.

## Dependencies

- **Inbound:** Attachment ingestion flows from agent or handlers.
- **Outbound:** `internal/agent/embedding`, `internal/datastore`, `internal/utils` (`DecodeTextToUTF8`), OpenAI client.

## Non-obvious decisions

- **`application/octet-stream`** with `.txt`-like extension may still be processed as text (see `pipeline_test.go` cases).

## Testing

- `chunker_test.go` — chunk boundaries, Unicode, defaults.
- `pipeline_test.go` — MIME/extension gating and unsupported types.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — file attachments and memory search.
