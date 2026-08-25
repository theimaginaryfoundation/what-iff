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

## Testing

- *(See chat handler tests for upload quotas; add here if standalone behaviors grow.)*

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — attachments and S3.
