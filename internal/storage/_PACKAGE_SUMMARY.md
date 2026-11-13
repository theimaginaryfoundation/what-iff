# Package: `internal/storage`

## Role

**S3 object storage** abstraction for user uploads and file archival: put/get/delete against a configured bucket and region.

## Responsibilities

- **`FileStore`** (`filestore.go`): constructor from bucket name, region, logger; methods used by `internal/agent` and handlers for attachment pipelines.
- **Attachment object cleanup** (`attachment_cleanup.go`):
  ent cascades delete attachment rows without touching the store, so callers read the affected rows first and release them after the delete.
  - `ReleaseAttachmentObjects` deletes a deleted row's object (its `s3_key`, or the legacy derived keys when empty) only when `AttachmentKeyRefs` reports no remaining row references it, plus the row's own thumbnail.
  - Both release paths also delete a deleted object's preview (`DeletePreviews`) when the object may have one: its key, or a row pointing at it, is a diagram's (`drawio.IsDiagramFile`).
    Asking both catches a file renamed to or from `.drawio`, or stored as a diagram by its media type, without two extra deletes for every object released.
    `ReleaseReplacedObject` takes the row whose object was replaced for this (nil for one that cannot be a diagram).
- **Previews** (`preview.go`, ADR 0x049 §8): a picture of a diagram the browser drew, keyed by the stored object it shows (`FileKeyForPreview`, `FileKeyForPreviewThumbnail`: `users/{id}/previews/{hash of the object key}.png` and `.thumb.jpg`).
  Keying by object means a preview matches the content it was drawn from, rows sharing the object share it, and a save (always a new object) leaves the file with none until it is drawn again.
  `PutPreview` stores the picture, its thumbnail and its tag (a hash of the picture, `.tag`); `PreviewTag` reads the tag, so a reader can tell whether a cached copy is current without downloading the picture.
  The tag is removed before the picture is replaced and written after, so it never names an older picture.
    It first confirms the rows are actually gone (`ExistingFileAttachmentIDs`), so a cascade that did not fire never strips a surviving row.
    Best effort: failures are logged and counted, never returned.
  - `PurgeUserObjects` removes the whole `users/{id}/` prefix and the account's export bundles under `exports/{id}/` (`UserExportPrefix`; `accountexport` writes there via `ExportBundleRoot`) after account deletion.
    It falls back to releasing the known rows when the store cannot list `users/{id}/`.
- **Streaming upload** (`stream.go`): `StreamUploader` and the `UploadStream` helper store an object read from an `io.Reader` without holding it whole.
  S3 uploads it in 8 MiB multipart parts (one part's buffer in memory; a single-part object is a plain `PutObject`) and aborts the multipart upload on any failure, on its own context so a timed-out job still cleans up.
  The local store writes a temp file and renames it into place.
  Like `ObjectLister`, it is an optional interface so `FileStore` mocks stay unchanged; `UploadStream` falls back to `io.ReadAll` + `UploadFile` for them.
- **Attachment image reads** (`attachment_image_bytes.go`): `ResolveAttachmentImage` tries an attachment's keys in order (thumbnail, `s3_key`, canonical gallery key, legacy chat key for a thumbnail; canonical, `s3_key`, chat for a full image), never fetching the same key twice, and reports which key answered.
  Every read counts on `whatiff.file.image_resolve` (`variant`, `source`); a thumbnail read answered by any other key served the full image, which callers hand to `imageutil.BackfillThumbnail`.
  `ResolveAttachmentImageBytes` is the same read without the source.
- **`Instrument`** (`instrumented.go`): decorator recording `whatiff.dependency.duration` (dependency `s3` or `local_fs`, operation `put_object`/`get_object`/`delete_object`/`list_objects`) through `telemetry.Global()`; `internal/server` wraps the store at construction.

## Dependencies

- **Inbound:** `internal/server` constructs the store; `internal/agent` and file handlers use it.
- **Outbound:** AWS SDK v2 S3 client.

## Non-obvious decisions

- Bucket and region come from `internal/server` `Config` (`S3FileBucket`, `AWSRegion`).
- **The object store is the only copy of an uploaded file.**
  Uploads are not mirrored to any provider (OpenAI's Files API was dropped), so every vision provider gets its image bytes from here at inference time, as well as the image gallery.
  Image uploads only need to write to `FileKeyForImage`; a chat-scoped `FileKeyForChat` copy is not required.
- **`ReadAttachment`** is a file attachment's bytes: its `s3_key`, else (rows stored before keys were recorded) the key `FileKeyForAttachment` derives; the download route and `internal/filecomments` read files through it.
- **Key layout:** `users/{id}/images/…` for gallery/Claude; `users/{id}/images/thumbs/…` for thumbnails; `users/{id}/chats/{chatId}/…` for non-image file-search assets; `users/{id}/personalities/…` for personality attachments; `users/{id}/agent/…` for agent-authored files; `users/{id}/files/…` (`FileKeyForUserFile`) for gallery imports of non-images and for every new version a file workspace save writes (ADR 0x043 §3).
  Version keys carry a fresh object id, never the attachment id, so a row's `s3_key` moves between objects and a reference row keeps the version it was made from.
- **Instrumented store keeps the optional interfaces:** `Instrument` returns a type that implements `ObjectLister`/`Presigner` exactly when the wrapped store does, so the account export's type assertions still work.
  It always implements `StreamUploader` (timed as `put_object`), streaming through when the wrapped store can.
  Presigning is local signing with no network call, so it is not timed; a missing object is still `(nil, nil)` and counts as a successful `get_object`.
- **Local fallback:** when `S3_FILE_BUCKET` is unset, `NewFileStore` returns a `localFileStore` backed by `$TMPDIR/chat-app-files/` (or `LOCAL_FILE_STORE_DIR`).
  Not suitable for multi-instance deployments.

## Testing

- `filestore_test.go` — behavior with mocks or localstack-style setups (see file).
- `stream_test.go` — multipart part splitting, abort on reader/part failure (even after the caller's context ends), and the local and fallback paths.
- `instrumented_test.go` — dependency metrics, the not-found contract, and optional-interface preservation.
- `attachment_image_bytes_test.go` — key order, the reported source, no repeated fetch of a key, and the read counter.
- `attachment_cleanup_test.go` — release reference counting and legacy keys, account purge, against the local store.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Infra** (S3).
