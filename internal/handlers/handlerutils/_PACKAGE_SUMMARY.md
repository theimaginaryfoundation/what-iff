# Package: `internal/handlers/handlerutils`

## Role

**Shared HTTP helpers** reused across resource handlers — e.g. multipart file attachment parsing, validation, and response helpers.

## Responsibilities

- **`fileattachment.go`:** Utilities for reading uploads and binding to models (see tests for expected behavior).
  It also owns the upload metrics for every upload path (chat, personality, image gallery), through `telemetry.Global()`.
  `UploadFileAttachment` is the multipart wrapper over `ProcessUpload`, which holds the steps that need no HTTP: type check, buffering to a temp file (at most 30 MB), image normalization, and the provider upload.
  `ProcessUpload` returns an `*UploadError` (the status and message a client sees; it wraps `ErrUnsupportedFileType` or `ErrFileTooLarge` where they apply), and `RespondWithUploadError` writes one.
  `StoreChatAttachment` is the storage half for a chat upload: the record, the object-store key (images under the gallery path with a thumbnail, other files under the chat), and chunking.
  The chat upload handler and the plugin attachment seam (`internal/server/plugin_attachments.go`) both call it.
  `ProcessUpload` records the spooled size (`whatiff.file.size`, operation `upload`) and the image `normalize` stage time.
  `whatiff.file.uploads` counts each upload once: failures in `ProcessUpload`, the multipart parse in `UploadFileAttachment`, or `UploadToS3`, success in `TriggerAsyncFileChunking`, which every path calls exactly once after the attachment is stored.
  Callers must not count uploads themselves.
  A caller whose attachment record fails to save calls `AbandonFileAttachmentUpload`, which counts the failure, removes the temp file, and best-effort deletes the provider-side file.
  Rolling back a saved record (e.g. after an S3 failure) calls `DeleteProviderFile`, so the provider copy is not orphaned either.
  Both detach from the request context's cancellation and only log a failed delete.
- **`httpresponse.go`:** The response helpers every handler writes through — `RespondWithJSON`, `RespondWithHTML`, `RespondWithNoContent`, and `RespondWithError`.
  The last takes a code as its fourth argument: either a specific code from the models taxonomy, or `CodeNotSet` where none has been assigned yet, in which case the helper fills in the generic code for the status.
  A blank or whitespace-only code is treated as `CodeNotSet`.

## Dependencies

- **Inbound:** `internal/handlers/chat`, `internal/handlers/personality`, and other upload paths.
- **Outbound:** Standard library HTTP, `internal/models`, `internal/storage`, `internal/utils`, `internal/agent/filechunker`.
  Deliberately **not** `internal/agent` — see below.

## Non-obvious decisions

- Keep helpers **free of business rules** that belong in datastore or agent — pass dependencies explicitly.
- **This package must stay leaf-level.**
  It used to import `internal/agent` for one upload call, which formed `handlerutils → agent → middleware → handlerutils` and made the response helpers unreachable from `internal/middleware`.
  `UploadFileAttachment` now accepts a narrow `FileAttachmentUploader` interface instead; callers pass `agent.OpenAIProvider`.
  Re-introducing an `internal/agent` import here would close that ring again.
  The structural match is pinned by `var _ handlerutils.FileAttachmentUploader = (*OpenAIProvider)(nil)` in `internal/agent/provider/fileattachment.go` — it must live on that side, since asserting it here would need the very import the interface exists to avoid.
  `FileAttachmentDeleter` (the provider delete used by that cleanup) follows the same pattern and is pinned the same way.
- **`RespondWithJSON` sets `X-Content-Type-Options: nosniff`.**
  `http.Error` sets it automatically; hand-written JSON responses do not, so call sites converted away from `http.Error` would otherwise silently lose it.
- **`RespondWithJSON` handles its own marshal failure inline.**
  It cannot call `RespondWithError`, which calls it back, so it marshals the envelope directly.
  That is why nothing in the codebase needs `http.Error` any more — a failed marshal reflects one unmarshalable caller value, not a broken encoder, so the envelope can still be sent.
  `TestHTTPErrorIsNeverUsed` enforces this with an empty allow list.
- **The `err` argument to the error helpers is logged, never serialized.**
  It routinely carries driver and constraint text; the response body says only what the handler chose to say via `message`.
  Anything a client should see goes in `message` or `details` deliberately (ADR 0x020).
- **Pass `CodeNotSet`, never the generic constant that matches the status.**
  Writing `models.ErrCodeNotFound` next to `http.StatusNotFound` duplicates information already in the call, and the two then drift apart the first time someone changes the status and forgets the code.
  `CodeNotSet` keeps one source of truth (ADR 0x020).
- **`CodeNotSet` is also the progress marker.**
  Its occurrence count is how many call sites still lack a specific code, so `grep -c` measures what remains of ADR 0x020's phase 2.

## Testing

- `fileattachment_test.go` — multipart and edge cases.
- `fileattachment_ingest_test.go` — `ProcessUpload` without HTTP (type, size, normalization, provider failure) and `StoreChatAttachment` (key routing, thumbnail, rollback, cleanup).
- `fileattachment_metrics_test.go` — upload size/kind, the normalize stage, that each upload is counted exactly once, and provider-file cleanup for abandoned uploads.
- `httpresponse_test.go` — that raw errors never reach the body, and that every error response carries a code (including unmapped statuses).

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — HTTP layer.
