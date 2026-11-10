# Package: `internal/handlers/chat`

## Role

HTTP API for **chats** and **chat messages** — the primary surface for sending user messages to the assistant, listing history, exports, file attachments on a chat, read receipts, rituals, and **MCP server** association.

## Responsibilities

- **Agent prompts** (`prompts.go`, ADR 0x042): `POST /chat/{chatId}/prompts/{promptId}/answer` records the person's answer to a question the running turn asked (ask_user or an MCP elicitation) through `PromptAgent`; 404 for another chat's or account's prompt, 409 when it no longer waits, 400 for an answer that does not fit.
- **Follow-ups** (ADR 0x042): `CreateChatMessage` takes `steer: true` for a message sent while a turn runs, which that turn takes in at its next step.
- **MCP Apps** (`mcp_apps.go`, ADR 0x042): `GET /chat/{chatId}/tool-calls/{toolCallId}/app` returns a tool call's view (HTML with its CSP, the tool's input and result, `Cache-Control: no-store`) and `POST .../app/tools/call` runs the view's tool calls on its own connector (403 for a tool not visible to apps), through `MCPAppAgent`.
- **Loaded skills** (`skills.go`, ADR 0x035): `GET /chat/{chatId}/loaded-skills` lists the skills the agent loaded in the thread with `load_skill` (mode and message skills are not loaded state) and `DELETE /chat/{chatId}/loaded-skills/{ritualId}` unloads one, for the thread's Tools panel.
- **Files in context** (`contextfiles.go`, ADR 0x043 §4): `GET /chat/{chatId}/context-files` returns `agent.ContextFilesForChat` for the thread's Files tab, plus the container status the private build reports; without an agent it returns an empty list.

- **`Handler`:** `RegisterRoutes` mounts `/api/chat/...` (under parent `/api` from server).
  Key routes: list/create/get/update/patch/delete chat, chat messages CRUD, `welcome-message`, `mark-read`, `mark-all-read` (all of the caller's chats, one all-or-nothing update), export, **`import`**, `available-rituals`, file attachments on a chat, `context` for debugging.
- List chats accepts `search`, which the datastore applies to chat names and checkpoint summaries.
  Query `archived=true` lists archived threads only; omit or `false` for active threads (default).
  Query `source=openai|anthropic` filters to imported threads (used by the post-import thread picker, sorted by recency via the default `last_message_time DESC` order).
- Chat create/update/patch accepts `tags` (max 10 items, max 10 chars each, no empty tags), `is_favorite`, and **`archived`** on PATCH to archive or restore.
- **Conversation import (`POST /chat/import`, `import.go`/`anthropic_import.go`):** Accepts a multipart `conversations.json` from an **OpenAI** or **Anthropic** export (format auto-detected by `detectImportFormat`).
  The upload is spooled to a temp file (capped ~60MB; larger exports are split client-side) and parsed + persisted as **archived** threads in a detached background job (`JobTypeChatImport`); the handler returns `202` with the Job and writes `models.ImportProgress` JSON to `Job.progress` (phase/source/total/imported/skipped) for the client to poll.
  The background run is bounded by `chatImportJobTimeout` (30 minutes, like account import); a run that times out is failed with a timeout message written on a fresh context, so the job never stays `processing`.
  OpenAI parsing uses `internal/chatimport`; Anthropic parsing is in-package.
  Per-conversation dedup via `sha256(conversationID)`.
  Terminal job status distinguishes three outcomes that all import zero threads: everything already present (`Skipped == total`) completes, since a re-import is a successful no-op; an empty archive completes; but a run where nothing was imported **and** nothing was deduplicated means every conversation failed individually — those failures are collected in `ImportResult.Errors` rather than returned as an error, so the job is marked `failed` instead of quietly completing with a zero count.
  Expected non-chat roles (`system`/`tool` for OpenAI; same for Anthropic senders) are dropped silently — only truly unknown roles are reported as parse warnings.
  A single unsupported *conversation* is skipped with a warning, but corruption of the JSON *stream* (a truncated or syntactically invalid archive) aborts the parse and fails the job: `encoding/json` keeps a stream-level decode error permanently while `Decoder.More` goes on reporting data, so skipping past one would loop forever rather than reach the next conversation.
  Both parsers pass the conversation title through `models.NormalizeImportedTitle` before persisting, because `Chat.name` is `NotEmpty` and `MaxLen(200)` counted in **bytes**: an untrimmed title made only of spaces or zero-width characters would land as a nameless thread, and a title of 200 CJK or emoji characters (600-800 bytes) would fail validation and drop that conversation from the import with nothing but a server-side log.
  The Angular importer always lands the Thread Manager on the **Archived** tab after import.
  Client-side zip extraction merges ChatGPT shards matching `conversations.json` / `conversations-NNN.json` (thread-complete chunks) before upload.
  Metrics (through `telemetry.Global()`): the staged payload size goes on `whatiff.file.size` (operation `chat_import`), the background run is tracked as a `chat_import` job (`TrackJob`, including a panic outcome), `parse` and `insert` are timed on `whatiff.file.operation.duration`, and conversation counts by outcome go on `whatiff.file.operation.items`.
- **File attachments (`fileattachment.go`):** upload metrics are recorded inside the shared `handlerutils` upload helpers, so this handler does not count uploads itself.
  An optional `folder` form field files the upload in a gallery folder (Save As from the chat-hosted file workspace, ADR 0x043 §3); an invalid or reserved folder is a 400 and the temp file is abandoned.
  A stored upload is announced through `internal/filechange` with the chat id, so the private build can seed it into the conversation's container.
- **Export (`export.go`):** the streamed ZIP's byte count is recorded on `whatiff.file.size` (operation `chat_export`); latency is left to the HTTP server metric.
- **Lazy rehydration trigger (`GetChat`):** When an unarchived imported thread (`source` set, no checkpoint yet) is opened, the handler calls `agent.EnqueueThreadRehydration` to summarize it **and seed long-term memories** in the background (see `internal/agent`).
  Unarchiving alone does not start it, so a bulk restore costs nothing until a thread is opened; archived threads open read-only and never start it.
  `POST /chat/{id}/rehydrate` (`RehydrateChat`) starts it explicitly for the post-import picker.
  The claim is atomic, so repeated opens start one job.
  The frontend's post-import picker reuses this path: selecting threads simply PATCHes `archived=false` on each, so no dedicated "prepare" endpoint exists.
- **Resuming a running turn:** `GET /chat/{chatId}/active-job` (`GetActiveChatJob`) returns the turn job to follow in the chat (`datastore.FindActiveTurnJobForChat`), with its `job_type`, the `message_id` it answers, and for a scheduled run its `job_run` (ADR 0x040; a webhook background run or the welcome has none) (204 when idle).
  That is a user's turn (`chat_message`) or a scheduled job's or webhook's run (`agent_job_run`); a run keyed by the chat id answers no saved message, so it has no `message_id` (ADR 0x039).
  The web client calls it whenever it (re)enters a thread and every few seconds while an idle thread is open, since job polls are scoped to the active thread and nothing pushes a new turn to the client.
  The older per-message `.../chat-message/{messageId}/active-job` remains for callers that already know the turn.
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
- **File storage routing in `CreateFileAttachment`:** The object store is the only copy of an uploaded file (nothing is uploaded to OpenAI's Files API any more), and every provider's vision input is built from it at inference time.
  Image uploads go to `storage.FileKeyForImage` (used by the gallery and read back into model context); non-image files go to `storage.FileKeyForChat` (used by the pgvector chunk pipeline).
  No dual-write to both paths is needed for images.
- **Deleting a chat releases its attachment objects.**
  The ent cascade removes the attachment rows on the chat's messages without touching the object store, so `DeleteChat` reads them first (`ListChatFileAttachmentObjectRefs`) and calls `storage.ReleaseAttachmentObjects` after the delete.
  Keys a reference copy in another chat still uses are kept; failures are logged, never returned.
- **Context scope:** chat create accepts `context_scope` (`account` or `sandbox`; 400 otherwise), and a sandbox gets none of the owner's default-enabled MCP connectors.
  `PUT` and `PATCH` accept it too, marked as an explicit change (`SetContextScope`), but `applyContextScopeChange` refuses `sandbox` on a chat that is not one already: a thread is sandboxed when it is created, never later.
- **`disabled_tools` is not part of create:** `CreateChat` drops it from the request (set it with PATCH); the datastore honours it only for threads the server creates itself, and gives a new sandbox its own defaults.

## Testing

- `update_patch_test.go`, `mark_read_test.go`, `export_test.go`, `free_tier_quota_test.go`, `chatmessage_quota_test.go` — HTTP and quota behavior (including list `archived` query parsing and PATCH `archived`).
- `chat_retry_test.go` covers `GetActiveChatJob`: a user turn with its message, a scheduled run with no message and its `job_run`, a chat-keyed run with no `job_run`, 204, 400 and 500.
- `welcome_message_test.go` covers onboarding welcome endpoint eligibility and job enqueue behavior.
- `import_metrics_test.go` checks the chat import job outcome (including a timed-out run), stage timings, payload size and item counts.
- `import_test.go` exercises the async import pipeline end-to-end (202 handoff, OpenAI + Anthropic happy paths, terminal job status on datastore failure); `import_unit_test.go` and `anthropic_import_test.go` cover the parsers and format detection.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **HTTP Layer**, **Agent layer**.
