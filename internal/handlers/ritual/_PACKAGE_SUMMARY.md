# Package: `internal/handlers/ritual`

## Role

HTTP API for **rituals** (reusable prompt/action templates) and **bindings** — list/create/update/delete at `/api/ritual/...`.

## Responsibilities

- **`Handler`:** Ritual CRUD (`handler.go`, `ritual.go`).
- **Agent Skills import and export** (`import_export.go`, ADR 0x035 §7): `POST /ritual/import` takes a multipart `file` (a skill zip or a lone SKILL.md) or `files` parts named by path, parses SKILL.md through `internal/skills`, and answers an `ImportResult` with warnings, the count of bundle files this version does not store, and which exporter connector names matched this account's connectors.
  `GET /ritual/{id}/export` renders `<slug>/SKILL.md` into a zip with the bundle's files at their paths.
  With `ritual_id`, import replaces an existing skill's text and files (the slug, personality and connectors stay).
- **Bundle files** (`skill_files.go`, ADR 0x035 §3): `GET /ritual/{id}/files`, `POST /ritual/{id}/files` (multipart `file` + `path`, replaces a file at the same path, any content type, 10 MB a file, 200 a skill) and `DELETE /ritual/{id}/files/{fileId}`.
  Objects live under `users/{uid}/skills/{skill_id}/` (`storage.FileKeyForSkillFile`), one per version; replaced and removed files release their objects through `storage.ReleaseAttachmentObjects`, and `DeleteRitual` reads the bundle's rows before the cascade for the same reason.
  The handler takes the object store (`NewHandler(ds, files, logger)`); without one, uploads answer 501 and import stores the text only, with a warning.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/models`, `internal/skills`, `mux`.

## Non-obvious decisions

- **Every create and update goes through `skills.Normalize`:** text is trimmed and stripped of smuggled Unicode, bounds are checked (description 1,024 chars, body 64 KB), tags are refused in name and description, and a create without a `slug` derives one from the name.
  A `skills.ErrInvalid` is a 400 whose message is the detail; `datastore.ErrRitualSlugTaken` is a 409.
- **Omitted skill fields are left alone on update** (`slug`, `license`, `compatibility`, `metadata`, `allowed_tools`, the invocation flags): they are pointers on `models.Ritual`, so an older client that sends only name, description and content cannot clear them.
- **Bundle import replaces files wholesale** (delete the skill's rows, store the bundle's), so a re-import is the bundle, not a merge; a bad file is reported in `warnings` and skipped rather than failing the import.
- **Import never fails on a taken slug:** it takes the next free numeric suffix and says so in `warnings`, since the user asked for the bundle, not the name.
- **`mcp_server_ids` on update:** `models.Ritual.MCPServerIDs` is a **pointer slice** — omit the JSON field to leave MCP edges unchanged; send an array (including `[]`) to replace the set (`internal/datastore/ritual.go` `UpdateRitual`).
  IDs must belong to the authenticated user; invalid sets return **400** (`ErrInvalidRequestBody` from datastore).

## Testing

- `import_export_test.go` — import of a zip with files and of a lone SKILL.md, export round-trip with the files, the suffixed slug on re-import, export round-trip, rejections, and create/update slug validation and conflicts, over an in-memory sqlite datastore.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — rituals in product overview.
