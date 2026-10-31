# Package: `internal/handlers/ritual`

## Role

HTTP API for **rituals** (reusable prompt/action templates) and **bindings** — list/create/update/delete at `/api/ritual/...`.

## Responsibilities

- **`Handler`:** Ritual CRUD (`handler.go`, `ritual.go`).
- **Agent Skills import and export** (`import_export.go`, ADR 0x035 §7): `POST /ritual/import` takes a multipart `file` (a skill zip or a lone SKILL.md) or `files` parts named by path, parses SKILL.md through `internal/skills`, and answers an `ImportResult` with warnings, the count of bundle files this version does not store, and which exporter connector names matched this account's connectors.
  `GET /ritual/{id}/export` renders `<slug>/SKILL.md` into a zip.

## Dependencies

- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/models`, `internal/skills`, `mux`.

## Non-obvious decisions

- **Every create and update goes through `skills.Normalize`:** text is trimmed and stripped of smuggled Unicode, bounds are checked (description 1,024 chars, body 64 KB), tags are refused in name and description, and a create without a `slug` derives one from the name.
  A `skills.ErrInvalid` is a 400 whose message is the detail; `datastore.ErrRitualSlugTaken` is a 409.
- **Omitted skill fields are left alone on update** (`slug`, `license`, `compatibility`, `metadata`, `allowed_tools`, the invocation flags): they are pointers on `models.Ritual`, so an older client that sends only name, description and content cannot clear them.
- **Import never fails on a taken slug:** it takes the next free numeric suffix and says so in `warnings`, since the user asked for the bundle, not the name.
- **`mcp_server_ids` on update:** `models.Ritual.MCPServerIDs` is a **pointer slice** — omit the JSON field to leave MCP edges unchanged; send an array (including `[]`) to replace the set (`internal/datastore/ritual.go` `UpdateRitual`).
  IDs must belong to the authenticated user; invalid sets return **400** (`ErrInvalidRequestBody` from datastore).

## Testing

- `import_export_test.go` — import of a zip and of a lone SKILL.md, the suffixed slug on re-import, export round-trip, rejections, and create/update slug validation and conflicts, over an in-memory sqlite datastore.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — rituals in product overview.
