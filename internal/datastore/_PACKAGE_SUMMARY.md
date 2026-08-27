# Package: `internal/datastore`

## Role

Application **repository layer** over Ent: CRUD, ownership checks, pagination, vector search, quotas, and encrypted token storage for MCP and other secrets.

## Responsibilities

- **`Datastore`** (`datastore.go`): holds `*ent.Client`, `*sql.DB` for raw queries, logger, optional **`telemetry.Metrics`** (for datastore counters such as context-item persist failures), and **`tokenCrypto`** for encrypting OAuth/API tokens at rest (`token_crypto.go`).
- **Per-domain files** (each adds methods on `Datastore`):
  - Chat & messages: `chat.go`, `chatmessage.go`, `chat_checkpoint.go` (checkpoint semantics), `chatexport.go`, `mark_read` / quota helpers. **`ListChats`** defaults to `archived=false`; set `ChatFilters.Archived` to `true` for the archive view or `IncludeArchived` for agent discovery spanning both states; **`ChatFilters.HasMessages`** excludes empty shells (`last_message_time IS NOT NULL`); **`UpdateChat`** persists `archived` from the loaded model (including PATCH).
  - Conversation import & rehydration: `chat_import.go` (**`ImportChats`** — one tx per conversation, dedups on `(owner_id, import_hash)`, sets `source`, optional progress callback) and `chat_rehydration.go` (**`GetChatMessagesForSummary`** lean chronological transcript; **`GetChatRehydrationState`**; **`SetChatRehydrationState`**; **`SetImportedThreadRehydrated`** — writes checkpoint summary + count + window pointer `last_checkpoint_at` and flips `rehydration_state=ready` in one update).
- Account backup: `accountbackup.go` imports versioned admin ZIP archives into an existing target user with UUID-based idempotency.
- **Audit trail:** `auditlog.go` appends rows to Ent `AuditLog` for admin model CRUD, quota bucket **consume** / lazy **refill**, account backup import, user account export/import, and profile **memory ZIP** export/import (best-effort writes; failures only log).
  - Memory & embeddings: `memory.go`, vector queries, ZIP export, and UUID-deduped import with embedding regeneration. **`GetMemoryByIDPrefix`** resolves a unique active memory by an 8–32 digit hex UUID prefix (used by `find_context` `related`/`origin` short `memory:df3e519d` hops), translating it into an indexed UUID range rather than formatting every row. `GetRelatedMemories` excludes `Scope=Summary` rows (checkpoint state, not facts); `GetRelatedSummaryMemories` is the Summary-only counterpart used by `find_context`'s `source_type=summaries`. `memory_merge.go`'s `ListMemoryMergeEvents` takes a `models.MemoryMergeEventFilters` (query/survivor-memory/date-range/exclude-reverted) for both the merge-audit HTTP endpoint and `find_context`'s `mode=lifecycle_events`.
  - Files: `fileattachment.go`, `filechunk.go` (chunk storage + search).
  - Personalities & rituals: `personality.go`, `personality_gen_flow.go`, `ritual.go`, `system_ritual_binding.go`.
  - Jobs: `job.go` (CRUD + **`UpdateJobProgress`** — a single scoped UPDATE for the opaque `progress` JSON, safe to call frequently from long-running jobs), `agentjob.go`, `scheduler_lock.go` (distributed scheduler lock).
  - Usage: `usagestats.go`. Quota-bucket types live in `internal/models` (`billing.go`, `quotabucket.go`); the quota-enforcement logic itself (free-tier limits, trial grants, subscription reconciliation) is in a private extension, not this tree.
  - Users & access: `user.go`, `userpreferences.go`, `role.go`, `admin.go`.
  - MCP: `mcpserver.go`.
  - Models: `model.go`.
  - Safety violations: `safety_violation_event.go`.
  - Tool call persistence: `toolcall.go`.
- **`errors.go`:** Sentinel errors for handlers and agent.

## Key types and entry points

| Symbol | Notes |
|--------|--------|
| `NewDatastore` | Requires Ent client, `sql.DB`, logger, token encryption secret, and optional metrics (`nil` in tests). |
| Methods on `Datastore` | Domain-specific; handlers depend on narrow interfaces where possible (`handlers/*/provider.go`). |
| `AdminImportAccountBackup` | Admin-only restore of an account packet containing chats, personalities, memories, moods, and agent jobs. |

## Dependencies

- **Inbound:** `internal/server` (constructs datastore), all `internal/handlers/*`, `internal/agent`, `internal/agentjobs/scheduler`.
- **Outbound:** `ent` generated code, PostgreSQL/pgvector, `zap`, optional `internal/telemetry` for metrics.

## Non-obvious decisions

- **Chat message context items:** `createContextItemsBulk` returns errors to callers; failed inserts **roll back** the surrounding transaction and increment `telemetry.ChatMessageContextItemsPersistFailures` when metrics are configured.
- **Message pagination:** `ListChatMessages` remains newest-first and offset-paginated for the chat UI. `ListChatMessagesAfter` is the forward-only retrieval path for agent thread walks; it orders by `(sent_at, id)` and accepts that same keyset cursor to avoid unstable ties and offset drift.
- **Context X-ray column:** `chat_message.context_breakdown` is a typed **`jsonb`** column (`field.JSON` over `*models.ContextBreakdown`); ent handles (un)marshalling, so `toChatMessageModel` just surfaces it (guarding empty snapshots) and `SetChatMessageContextBreakdown` sets the pointer (assistant rows only; best-effort). It is a scalar column rather than a `ChatMessageContextItem` row on purpose — context items are re-fed into the model context, and this snapshot must not be. It's a value object (write-once, read-with-parent, never queried by field), so no child table; `jsonb` (not text) keeps DB-side JSON queries open and the embedded `version` guards shape evolution.
- **Scheduler lock:** `scheduler_lock.go` supports single active scheduler instance in multi-replica deployments (see `internal/server` config flags).
- **Quota buckets:** `models.QuotaBucket` (`internal/models/quotabucket.go`) is the shared type for free-tier/billing quota state. The enforcement logic that reads and reconciles it — free-tier limits, trial grants, subscription-driven renewal — lives in a private extension, not this tree; see `internal/metering` for the public seam it plugs into.
- **Chat list search/favorites/tags:** `chat.go` persists chat `tags` and `is_favorite`; tags are normalized/validated via shared model helpers. `ListChats` search matches chat **name**, **checkpoint_summary**, and individual **tags** (JSON array) with case-insensitive substring predicates. Favorite-count checks are a **best-effort UI guard** (non-locking count check, not a strict datastore invariant under races).
- **Agent jobs:** `UpdateAgentJobSchedule` (`agentjob.go`) sets status back to **active** and clears **last_error** when the job was **complete** or **failed** and the new schedule still has a **next_run_at**, so the in-process scheduler (which only schedules **active** jobs) picks it up again.
- **Streamed chat failures:** `FinalizeCancelledChatJobWithPartial` and `FinalizeFailedChatJobWithPartial` atomically consume `draft_deltas` into an assistant message when text was streamed before termination, set the terminal job status/result, and clear the draft buffer. A failed (rather than cancelled) chat job retains its error for the user-turn failure banner.
- **`SetAgentJobOverrides`:** `personality_id` must belong to the job owner; `model_id` must exist in the global model catalog. Partial updates use `models.SetAgentJobOverridesPatch` so omitted JSON fields are not overwritten. Invalid IDs return `ErrInvalidRequestBody`.
- **Model soft-delete query contract:** model reads should default to active rows (`deleted=false`). Include deleted models only for explicit admin/audit use cases. Validation paths that accept `model_id` from user-facing writes must treat soft-deleted models as invalid.
- **Account backup import:** preserves source UUIDs for idempotency, imports only into an existing user, clears provider response IDs, requires a target default model for chat restore fallback, and rebuilds memory embeddings. It is **not one transaction**; partial restores are intentional, and operators can rerun the same archive after fixing the failure. JSONL sections are loaded one at a time under the admin upload cap; broader/non-internal use should switch this to streaming batch processing. **Audit:** one `account_backup` / `import` row summarizes all sections (including memory counts); the inner memory ZIP import does **not** also write `memory_pack` rows (avoids duplicate audit entries).
- **User account export:** `ExportConversationInputs` walks each chat's messages by the ordered `(sent_at, id)` cursor, rather than `OFFSET`, so very long conversations preserve stable ordering without progressively slower page scans.
- **Batched embedding imports:** imported embeddings use the deterministic memory UUID as their primary key and conflict on that primary key. This preserves retry safety for legacy PostgreSQL databases that lack the Ent-declared unique constraint on `embedding_memory`.
- **Actor on audit rows:** `actor_user_id` is filled from request context via `internal/apicontext` (middleware dual-writes the authenticated user id next to `middleware.UserIDKey` so datastore avoids an import cycle). Background jobs that propagate user id should use the same dual-write when building exec contexts.
- **`UpdateUserPreferences` writes per field, not wholesale:** the DTO has no pointers, so each field picks its own rule and they are not consistent — `default_model` is written unconditionally, `default_personality` is **cleared** when the incoming value is `uuid.Nil`, and `theme` and `last_seen_announcement` are skipped when empty. Adding a scalar field means choosing one of those and living with the ambiguity that a zero value cannot be distinguished from an omitted one. **`favorite_model_ids` sidesteps it**: a nil slice means "field omitted, leave the stored list alone" and an empty non-nil slice means "clear it", a distinction JSON gives slices for free. Prefer slice/map-shaped fields here when the semantics allow. `toUserPreferencesModel` normalises the column to a non-nil slice so the response always carries an array rather than `null`.

### Model Query Checklist

- Add `model.Deleted(false)` to new runtime model queries unless the use case explicitly needs deleted rows.
- For "exists" checks used by writes (chat/job/preferences/overrides), validate against active models only.
- Do not hard-delete models in normal flows; use soft delete.
- Seed code should insert missing defaults but must not overwrite metadata on existing active models.

## Testing

- `chat_checkpoint_test.go`, `chatmessage_test.go`, `chatmessage_mark_read_test.go` — message and checkpoint behavior.
- `memory_test.go`, `filechunk_test.go` — retrieval, ZIP export/import helpers, full import count/persist coverage, and chunks.
- `compaction_event_test.go`, `memory_merge_test.go` — SQLite harness mirrors ent FK semantics (`memory_merge_events.compaction_event_id` → `compaction_events` ON DELETE SET NULL); compaction tests cover content-addressed snapshots, merge grouping, page-size cap, and FK null-on-delete.
- `accountbackup_test.go` — backup JSONL parsing edge cases such as large records and optional sections.
- `accountexport_test.go` — conversation export’s timestamp/ID cursor covers a batch boundary where all messages share a timestamp.
- `token_crypto_test.go` — round-trip encryption.
- `user_test.go`, `quotabucket_test.go`, `scheduler_lock_test.go`, `free_tier_quota_test.go`, `trial_quota_test.go` — quotas, locking, and trial-credit grant/backfill (sqlite harness must include the unique `(owner_type, owner_id, resource_type)` index).
- `agentjob_schedule_reactivate_test.go` — terminal-state → active when rescheduling with a next run.
- `userpreferences_test.go` — favorites mapping plus the nil-vs-empty write rule. The write assertions capture the SQL Ent emits and filter to `UPDATE` statements, because "was this column written at all" is the behaviour under test and the re-read that follows names every column regardless.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Data layer**, **Data Access & Ent**.
