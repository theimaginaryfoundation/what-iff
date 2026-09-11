# Package: `internal/handlers/accountexport`

## Role
Authenticated user account portability: ZIP export by email and additive ZIP import.

## Responsibilities
- Enqueue and track asynchronous, email-only account exports and staged account imports.
- Build an id-stripped ZIP containing conversations, personalities, memories, and a file inventory.
- Validate bounded account-import ZIPs and restore the supported sections.

## Key types and entry points
- `Handler` — registers `/api/account/export` and `/api/account/import`.
- `EnqueueExport`, `GetExport`, `ImportAccount`, `GetImport` — HTTP entry points.

## Dependencies
- **Inbound:** `internal/server`.
- **Outbound:** `internal/datastore`, `internal/exporter`, `internal/storage`, `internal/email`, `internal/agent/embedding`.

## Non-obvious decisions
- Export download URLs are emailed only and never stored on the `Job` response.
- Local development uses the filesystem store plus `email.NoopSender`, preserving the production flow without AWS.
- Files are an inventory only; their bytes are deliberately outside this export format.
- `conversations.json` remains Anthropic-compatible but carries `whatiff_*` state for account round-trips. Personality IDs are source references only; import maps them to fresh destination IDs before restoring chats.
- Import order is personalities → chats → memories. The nested memory ZIP is rewritten to destination chat/personality IDs before the normal memory importer regenerates embeddings in bounded OpenAI batches.
- **Round-trip is idempotent against native data.** Importing an export back into the account it came from does not duplicate threads or memories: each conversation carries its source chat id (`ImportConversation.SourceID`), and the chat dedup skips when a chat with that id already exists for the user even though the native chat has no `import_hash`. Memories the target user already owns keep their original id (via `MemoryIDsOwnedByUser`) so the importer's id-dedup skips them; memories owned elsewhere are still namespaced to `uuid.NewSHA1(targetUser, sourceID)` to keep cross-account restores free of primary-key collisions.
- Duplicate names in the nested memory ZIP (including names that collide after personality-ID remapping) are rejected instead of being merged.
- Memory-import progress includes content-free invalid-record reasons; detailed logs identify ZIP entry, line, reason, and a parsed ID when available.
- Exported checkpoint summaries make threads immediately resumable (`ready` and unarchived); summary-less threads stay archived for lazy rehydration.
- Account-import uploads are staged to temporary files, restored by a bounded detached worker, and deleted regardless of terminal outcome; `AccountImportProgress` carries phases and the terminal result for polling.

## Testing
- `import_test.go` covers bounded ZIP reads, archive-entry limits, duplicate nested-memory entries, and reference remapping.
- `internal/handlers/chat/accountexport_roundtrip_test.go` verifies exported conversations round-trip through the existing importer.

## Related
- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md)
- [ADR 0x019](../../../docs/adr/0x019-account-export.md)
