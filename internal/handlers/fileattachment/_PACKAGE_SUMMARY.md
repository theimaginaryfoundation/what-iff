# Package: `internal/handlers/fileattachment`

## Role

HTTP API for **file attachments** metadata and download flows at `/api/file-attachment/...` (global attachment operations not scoped under a single chat path when applicable).

## Responsibilities

- **`Handler`:** CRUD/list for attachments per OpenAPI; coordinates with datastore and storage presigned URLs as implemented.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/storage`, `internal/models`, `mux`.

## Non-obvious decisions

- Chat-scoped uploads may also exist under `handlers/chat` — avoid duplicating business rules; datastore is canonical.
- **Delete is reference counted.**
  `CreateFileAttachmentReference` copies share the source's `s3_key` and provider `FileID`.
  `DELETE /file-attachment/{id}` deletes the provider file only when no other row carries the `FileID`, then the row, then releases the stored object and thumbnail via `storage.ReleaseAttachmentObjects` (skipped while another row references the key).
  The object step is best effort and never fails the request; an object it cannot remove is left orphaned.
- **Download fallback uses the chat id.**
  For rows without `s3_key` the derived key comes from `FileKeyForAttachment(..., ChatID, PersonalityID)`; `ChatID` is resolved by the datastore through the chat message (issue #253).

## Testing

- *(See chat handler tests for upload quotas; add here if standalone behaviors grow.)*
- `cleanup_test.go` — delete removes an unreferenced object and thumbnail, keeps one a reference copy shares (and its provider file), survives an object-store failure; legacy chat document download without `s3_key`.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — attachments and S3.
