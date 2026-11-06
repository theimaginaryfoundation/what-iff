# Package: `internal/filesave`

## Role

Replaces the content of one of a user's files from the file workspace (ADR 0x043 §3), the way `internal/filedelete` deletes one: the rules live here and `PUT /file-attachment/{id}/content` only maps errors to statuses.

## Responsibilities

- **`Service.Replace`** uploads the body as a new object, swaps the row to it with `datastore.SwapFileObject` (every chat copy sharing the old object moves with it; context-only rows keep what the reply saw), releases the replaced object unless a row still references it, regenerates an image's thumbnail, re-indexes a file that was chunked for search, and announces the change through `internal/filechange`.
- **`Request.IfMatch`** is the `content_version` the editor loaded; a mismatch is a `*ConflictError` carrying the current version, and the uploaded object is dropped.
  Nil overwrites unconditionally (the "Overwrite" choice after a conflict).
- **Refusals:** `ErrNotFound` (also for context-only rows), `ErrSkillFile` (skill bundle files belong to the skill editor), `ErrTypeMismatch` (a save keeps the file's type, so the body's media type must match it when given), `ErrInvalidImage`, `ErrTooLarge` (`MaxBytes`, the upload limit by default), `ErrNoStore`.

## Dependencies

- **Inbound:** `internal/handlers/fileattachment`.
- **Outbound:** `internal/datastore` (`SwapFileObject`, `ClearFileChunks`, `GetFileAttachment`, key references), `internal/storage` (object store, key builders, `ReleaseReplacedObject`), `internal/imageutil` (thumbnails), `internal/agent/filechunker` (re-indexing), `internal/filechange`.

## Non-obvious decisions

- **Where the new version lives follows the file's kind.**
  Agent files stay under `users/{uid}/agent/` because agent reads refuse any other prefix; images stay in the image store the gallery serves from; everything else goes under `users/{uid}/files/`.
  The key carries a fresh object id, never the attachment id, so a reader never sees a half-written object and reference rows keep the version they were made from.
- **A swap error keeps the uploaded object.**
  The update may have committed before the error surfaced; an orphaned object beats a row pointing at a deleted one (the same rule as agent file edits).
- **Re-indexing is background and only for files that were indexed** (`ChunkStatus` set) and under `MaxChunkBytes`; it clears the old chunks first because `CreateFileChunks` is idempotent on `chunk_status`.
  Agent files are never chunked, so a save leaves them alone.
- **The file's type and name never change on a save.** An image is saved in its own format; a `.md` stays Markdown. Renames are `PATCH /image-gallery/{id}`.

## Testing

- `filesave_test.go` — the happy path (key prefix, hash, size, version, released object, re-index, change event), the conflict path, every refusal, agent files, images.
