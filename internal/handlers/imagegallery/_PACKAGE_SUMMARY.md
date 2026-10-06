# Package: `internal/handlers/imagegallery`

## Role

HTTP API for the **gallery** at `/api/image-gallery/...`: listing a user's images and other files, streaming image bytes, importing images, renaming, deleting, reusing an image in a chat, and filing any file in folders.
The web app's gallery doubles as a read-only file view; other files' bytes come from `GET /file-attachment/{id}`.

## Responsibilities

- **`Handler` (`handler.go`, `gallery.go`, `import.go`, `manage.go`):** list (`GET /image-gallery`, filtered by `kind`, name, personality, `global_only` and `folder`), content (`GET /{id}?size=thumbnail|full`, images only), file info (`GET /{id}/info`, any kind, for opening a linked file), import, rename, delete, and reference (`POST /{id}/reference`, a chat-reuse copy that shares the stored object).
- **`kind`** on the list and on `/folders` is `images` (the default, so older clients see only images), `files` (everything that is not `image/*`) or `all`; anything else is a 400.
  Import, thumbnails and `DELETE /{id}` stay image-only; the web app deletes other files through `DELETE /file-attachment/{id}`, which also removes the provider file.
- **Folders (`folders.go`):** `GET /folders` lists the folders that hold files of the requested kind with the count directly in each, `POST /move` files any of the caller's files in a folder, and `POST /folders/move` renames a folder and everything beneath it (every kind).
  These routes are registered before `/{id}` so `folders` is not read as an image id.
- **`Store` (`provider.go`):** the narrow datastore surface the handlers need, which `*datastore.Datastore` satisfies.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/storage`, `internal/models`, `internal/agent` (import only), `handlerutils`, `mux`.

## Non-obvious decisions

- **A folder is a label, not a place, and images and other files share one tree.**
  It is a lower-case path string on `file_attachments.folder` (`models.NormalizeFolder`: segments trimmed, `/` or `\` separate, empty segments dropped, no `.` or `..`, at most 8 levels and 255 characters).
  Moving a file or a folder changes the label only; the `s3_key` and the stored bytes never move.
  A folder exists while it holds a file, so there is no folder table and an empty folder lives only in the client until something is moved into it.
- **`folder` on the list is a presence check.** Absent lists every file of the kind (the pre-folders behavior), and an empty value is the top level.
- **Reference copies never carry a folder and are never counted.** Moving by a copy's id moves the original it shares a stored object with.
- **Moves are scoped in the datastore** to the caller's own rows, so an id that is someone else's is skipped and the response says how many moved.
- **Import takes an optional `folder` form field**, so an image imported while viewing a folder lands in it.

## Testing

- `folders_test.go` — the folder routes (normalization, validation, route precedence over `/{id}`, auth) and the list `folder` presence check, against a fake store.
- `kind_test.go` — the `kind` parameter on the list and `/folders` (default, values, refusal) and `GET /{id}/info`.
- `gallery_test.go`, `list_test.go` — content streaming and list filter parsing.
- Datastore behavior (grouping, moves, nested renames, ownership, reference copies) is tested in `internal/datastore/fileattachment_folders_test.go`.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — attachments and S3.
- `internal/agent/tools/_PACKAGE_SUMMARY.md` — the agent's `list` folder filter, `generate_image` folder and `move_files`.
- Issue #43 — reading uploaded (RAG) documents; the personality page links each document to the gallery viewer.
