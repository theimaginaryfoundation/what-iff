# Package: `internal/utils`

## Role

Small **shared utilities** with no business domain — currently text encoding normalization for file ingestion pipelines.

## Responsibilities

- **`DecodeTextToUTF8`:** Normalizes bytes to UTF-8, supporting UTF-8 (with optional BOM) and UTF-16 LE/BE when BOM is present (`textdecode.go`).
- **`GetFileType`:** the upload allowlist (`fileutils.go`), extension to MIME type and whether the file is chunked for search.
  ADR 0x043 added `.ipynb` (`application/x-ipynb+json`, not chunked), `.jsonl`, `.tsv`, `.yaml` and `.yml` (chunked).
  `.svg` stays out: it is `image/*` to every image path (normalization, thumbnails, vision input) but is markup, so it needs its own handling first.

## Dependencies

- **Inbound:** `internal/agent/filechunker` and any upload path that must accept legacy encodings; `handlerutils.ProcessUpload` and `datastore.UpdateFileAttachmentName` (the type a renamed extension maps to).
- **Outbound:** Standard library only.

## Non-obvious decisions

- Invalid UTF-8 without UTF-16 BOM returns an error — callers must handle or pre-detect binary files.

## Testing

- `textdecode_test.go` — BOM and encoding cases.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — file attachment pipeline.
