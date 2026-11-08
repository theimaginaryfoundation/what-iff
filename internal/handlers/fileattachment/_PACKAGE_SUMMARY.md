# Package: `internal/handlers/fileattachment`

## Role

HTTP API for **file attachments** metadata and download flows at `/api/file-attachment/...` (global attachment operations not scoped under a single chat path when applicable).

## Responsibilities

- **`Handler`:** CRUD/list for attachments per OpenAPI; coordinates with datastore and storage presigned URLs as implemented.
- **`PUT /file-attachment/{id}/content` (`content.go`):** replaces a file's content from the file workspace (ADR 0x043 §3).
  The body is the whole file; `If-Match` is the `content_version` the client loaded (bare or quoted, `*` or absent for an unconditional overwrite); `?chat_id=` names the conversation the save came from.
  It returns the updated attachment with the new version as the `ETag`; a stale version is 412 with `details.content_version`, a skill file 403, a mismatched `Content-Type` 415, an invalid image 400, an oversized body 413, no object store 503.
  The work is `filesave.Service.Replace`.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/filedelete`, `internal/filesave`, `internal/storage`, `internal/models`, `mux`.

## Non-obvious decisions

- Chat-scoped uploads may also exist under `handlers/chat` — avoid duplicating business rules; datastore is canonical.
- **Delete is reference counted.**
  `CreateFileAttachmentReference` copies share the source's `s3_key`.
  `DELETE /file-attachment/{id}` deletes the row, then releases the stored object and thumbnail via `storage.ReleaseAttachmentObjects` (skipped while another row references the key).
  The object step is best effort and never fails the request; an object it cannot remove is left orphaned.
  The steps live in `filedelete.Service.DeleteFile`, which the gallery's bulk delete shares; the handler only maps its errors (not found or unreadable is 404, anything else 500).
- **Downloads revalidate.**
  A save replaces a file's bytes under the same URL, so `GET` sends `Cache-Control: private, no-cache` with `content_version` as the ETag and answers a matching `If-None-Match` with 304, without reading the store.
- **Download fallback uses the chat id.**
  For rows without `s3_key` the derived key comes from `FileKeyForAttachment(..., ChatID, PersonalityID)`; `ChatID` is resolved by the datastore through the chat message (issue #253).

## Testing

- *(See chat handler tests for upload quotas; add here if standalone behaviors grow.)*
- `cleanup_test.go` — delete removes an unreferenced object and thumbnail, keeps one a reference copy shares, survives an object-store failure; legacy chat document download without `s3_key`.
- `content_test.go` — the content route's statuses (200 with ETag, 412 with the current version, 400 If-Match, 404, 415) against stubs; the save itself is tested in `internal/filesave`.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — attachments and S3.
