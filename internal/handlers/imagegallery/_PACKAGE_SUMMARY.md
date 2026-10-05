# Package: `internal/handlers/imagegallery`

## Role

HTTP API for the **image gallery** at `/api/image-gallery/...`: listing a user's images, streaming their bytes, importing, renaming, deleting, reusing one in a chat, and filing them in folders.

## Responsibilities

- **`Handler` (`handler.go`, `gallery.go`, `import.go`, `manage.go`):** list (`GET /image-gallery`, filtered by name, personality, `global_only` and `folder`), content (`GET /{id}?size=thumbnail|full`), import, rename, delete, and reference (`POST /{id}/reference`, a chat-reuse copy that shares the stored object).
- **Folders (`folders.go`):** `GET /folders` lists the folders that hold images with the count directly in each, `POST /move` files images in a folder, and `POST /folders/move` renames a folder and everything beneath it.
  These routes are registered before `/{id}` so `folders` is not read as an image id.
- **`Store` (`provider.go`):** the narrow datastore surface the handlers need, which `*datastore.Datastore` satisfies.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/storage`, `internal/models`, `internal/agent` (import only), `handlerutils`, `mux`.

## Non-obvious decisions

- **A folder is a label, not a place.**
  It is a lower-case path string on `file_attachments.folder` (`models.NormalizeFolder`: segments trimmed, `/` or `\` separate, empty segments dropped, no `.` or `..`, at most 8 levels and 255 characters).
  Moving an image or a folder changes the label only; the `s3_key` and the stored bytes never move.
  A folder exists while it holds an image, so there is no folder table and an empty folder lives only in the client until something is moved into it.
- **`folder` on the list is a presence check.** Absent lists every image (the pre-folders behavior), and an empty value is the top level.
- **Reference copies never carry a folder and are never counted.** Moving by a copy's id moves the original it shares a stored object with.
- **Moves are scoped in the datastore** to the caller's image rows, so an id that is someone else's, or not an image, is skipped and the response says how many moved.
- **Import takes an optional `folder` form field**, so an image imported while viewing a folder lands in it.

## Testing

- `folders_test.go` — the folder routes (normalization, validation, route precedence over `/{id}`, auth) and the list `folder` presence check, against a fake store.
- `gallery_test.go`, `list_test.go` — content streaming and list filter parsing.
- Datastore behavior (grouping, moves, nested renames, ownership, reference copies) is tested in `internal/datastore/fileattachment_folders_test.go`.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — attachments and S3.
- `internal/agent/tools/_PACKAGE_SUMMARY.md` — the agent's `list` folder filter, `generate_image` folder and `move_files`.
