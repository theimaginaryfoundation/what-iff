# Package: `internal/storage`

## Role

**S3 object storage** abstraction for user uploads and file archival: put/get/delete against a configured bucket and region.

## Responsibilities

- **`FileStore`** (`filestore.go`): constructor from bucket name, region, logger; methods used by `internal/agent` and handlers for attachment pipelines.
- **`Instrument`** (`instrumented.go`): decorator recording `whatiff.dependency.duration` (dependency `s3` or `local_fs`, operation `put_object`/`get_object`/`delete_object`/`list_objects`) through `telemetry.Global()`; `internal/server` wraps the store at construction.

## Dependencies

- **Inbound:** `internal/server` constructs the store; `internal/agent` and file handlers use it.
- **Outbound:** AWS SDK v2 S3 client.

## Non-obvious decisions

- Bucket and region come from `internal/server` `Config` (`S3FileBucket`, `AWSRegion`).
- **S3 is never used by the OpenAI vision path.**
  OpenAI resolves images via its own Files API (the `FileID` stored on `FileAttachment`).
  Our S3 keys are used solely by Claude (raw bytes for vision) and the image gallery.
  This means image uploads only need to write to `FileKeyForImage`; a chat-scoped `FileKeyForChat` copy is not required.
- **Key layout:** `users/{id}/images/…` for gallery/Claude; `users/{id}/images/thumbs/…` for thumbnails; `users/{id}/chats/{chatId}/…` for non-image file-search assets; `users/{id}/personalities/…` for personality attachments.
- **Instrumented store keeps the optional interfaces:** `Instrument` returns a type that implements `ObjectLister`/`Presigner` exactly when the wrapped store does, so the account export's type assertions still work.
  Presigning is local signing with no network call, so it is not timed; a missing object is still `(nil, nil)` and counts as a successful `get_object`.
- **Local fallback:** when `S3_FILE_BUCKET` is unset, `NewFileStore` returns a `localFileStore` backed by `$TMPDIR/chat-app-files/` (or `LOCAL_FILE_STORE_DIR`).
  Not suitable for multi-instance deployments.

## Testing

- `filestore_test.go` — behavior with mocks or localstack-style setups (see file).
- `instrumented_test.go` — dependency metrics, the not-found contract, and optional-interface preservation.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Infra** (S3).
