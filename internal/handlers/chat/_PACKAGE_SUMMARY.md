# Package: `internal/handlers/chat`

## Role

HTTP API for **chats** and **chat messages** — the primary surface for sending user messages to the assistant, listing history, exports, file attachments on a chat, read receipts, rituals, and **MCP server** association.

## Responsibilities

- **`Handler`:** `RegisterRoutes` mounts `/api/chat/...` (under parent `/api` from server). Key routes: list/create/get/update/patch/delete chat, chat messages CRUD, `welcome-message`, `mark-read`, export, **`import`**, `available-rituals`, file attachments on a chat, `context` for debugging.
- List chats accepts `search`, which the datastore applies to chat names and checkpoint summaries. Query `archived=true` lists archived threads only; omit or `false` for active threads (default). Query `source=openai|anthropic` filters to imported threads (used by the post-import thread picker, sorted by recency via the default `last_message_time DESC` order).
- Chat create/update/patch accepts `tags` (max 10 items, max 10 chars each, no empty tags), `is_favorite`, and **`archived`** on PATCH to archive or restore.
- **Conversation import (`POST /chat/import`, `import.go`/`anthropic_import.go`):** Accepts a multipart `conversations.json` from an **OpenAI** or **Anthropic** export (format auto-detected by `detectImportFormat`). The upload is spooled to a temp file (capped ~60MB; larger exports are split client-side) and parsed + persisted as **archived** threads in a detached background job (`JobTypeChatImport`); the handler returns `202` with the Job and writes `models.ImportProgress` JSON to `Job.progress` (phase/source/total/imported/skipped) for the client to poll. OpenAI parsing uses `internal/chatimport`; Anthropic parsing is in-package. Per-conversation dedup via `sha256(conversationID)`. Expected non-chat roles (`system`/`tool` for OpenAI; same for Anthropic senders) are dropped silently — only truly unknown roles are reported as parse warnings. The Angular importer always lands the Thread Manager on the **Archived** tab after import. Client-side zip extraction merges ChatGPT shards matching `conversations.json` / `conversations-NNN.json` (thread-complete chunks) before upload.
- **Lazy rehydration trigger (`PatchChat`):** When an imported thread (`source` set, no checkpoint yet) is unarchived, the handler calls `agent.EnqueueThreadRehydration` to summarize it **and seed long-term memories** in the background (see `internal/agent`). The frontend's post-import picker reuses this path: selecting threads simply PATCHes `archived=false` on each, so no dedicated "prepare" endpoint exists.
- **`MessageAgent` interface:** Decouples `CreateChatMessage` from concrete `*agent.Agent` for tests.
- **`WelcomeMessageAgent` interface:** Decouples welcome-message async prompt enqueueing from concrete `*agent.Agent`.
- **`HandlerConfig`:** Billing requirement and free-tier message limit passed from server config.
- **Chat MCP routes:** `/mcp-servers` CRUD association routes (plus singular aliases for backward compatibility) are available to authenticated users.
- Supporting files: `chatmessage.go`, `fileattachment.go`, `provider.go` (store interface — includes `ImportChats` + job helpers), `mcpserver.go`, `export`, `import.go`, `anthropic_import.go`, quota tests.

## Dependencies

- **Inbound:** `internal/server` registers routes.
- **Outbound:** `internal/agent`, `internal/datastore` (via `Store` interface), `internal/middleware`, `internal/models`, `mux`.

## Non-obvious decisions

- Route order: specific paths registered before `/{id}` patterns (see `handler.go` comments in server if any).
- **`messageAgent`** defaults to real agent but can be swapped in tests via unexported setter pattern if present.
- **File storage routing in `CreateFileAttachment`:** The `FileID` stored on a `FileAttachment` record is an **OpenAI Files API** identifier — OpenAI resolves it from its own storage when running the model. Our S3 bucket is never read by the OpenAI vision path. Therefore image uploads go exclusively to `storage.FileKeyForImage` (used by Claude and the gallery); non-image files go to `storage.FileKeyForChat` (used by the pgvector chunk pipeline). No dual-write to both paths is needed for images.

## Testing

- `update_patch_test.go`, `mark_read_test.go`, `export_test.go`, `free_tier_quota_test.go`, `chatmessage_quota_test.go` — HTTP and quota behavior (including list `archived` query parsing and PATCH `archived`).
- `welcome_message_test.go` covers onboarding welcome endpoint eligibility and job enqueue behavior.
- `import_test.go` exercises the async import pipeline end-to-end (202 handoff, OpenAI + Anthropic happy paths, terminal job status on datastore failure); `import_unit_test.go` and `anthropic_import_test.go` cover the parsers and format detection.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **HTTP Layer**, **Agent layer**.
