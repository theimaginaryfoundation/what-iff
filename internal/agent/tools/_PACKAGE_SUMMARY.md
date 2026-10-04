# Package: `internal/agent/tools`

## Role

Provider-neutral function tool catalog plus concrete tool implementations, JSON specs, and shared helpers for marshaling results and arguments.

## Responsibilities

- **Tool catalog/specs:** `FunctionToolSpec`, `FunctionToolDefinition`, and catalog helpers (`catalog.go`, `toolconstants.go`) define the static function-tool surface and user-toggleable metadata.
  `FunctionToolDefinition.HumanDescription` is presentation copy for human-facing surfaces and is intentionally separate from the agent-facing `FunctionToolSpec.Description` prompt.
- **Provider projection:** `OpenAIToolUnionParam` / `OpenAIFunctionTools` build OpenAI Responses function tools; Claude projection is built by `internal/agent` via `provider.ClaudeFunctionTool`.
- **Implementations:** Per-tool structs with `Execute`-style handlers, e.g.:
  - `VectorStoreMemoryTool` — memory search (`memory.go`).
  - `ScratchpadTool` — update scratchpad (`scratchpad.go`, `update_scratchpad.go`); read-only scratchpad tool was removed because scratchpad is already injected into context.
  - `list` — unified resource listing (`list.go`, `list_kinds.go`; tests in `list_test.go`).
  - `create_memory`, `generate_image`, `create_agent_job` — named files for those flows.
  - `RecallTool` — unified context retrieval (`recall.go`, `recall_modes.go`, `recall_memory_id.go`, `recall_time.go`; see ADR 0x017).
    Modes: `investigate`/`search`/`fetch`/`related`/`origin`/`conversation`/`lifecycle_events` (alias `merge_history`).
    `source_type=summaries` searches per-chat checkpoint Summary memories (via `datastore.GetRelatedSummaryMemories`); `lifecycle_events` lists the memory lifecycle audit trail (`datastore.ListMemoryMergeEvents`).
    Paginated modes emit `page`/`total_count`/`has_more`/`next_page_token`.
  - `list_models`, `list_personalities`, `run_subagent` — shared function specs consumed by `internal/agent` dispatch.
- **Shared helpers:** `tool_helpers.go` — marshal/unmarshal tool results, validation, truncation for logs (`tool_helpers_test.go`).
- **`prompts.go`:** Tool-specific system strings where needed.

## Key types and entry points

| Symbol | Notes |
|--------|--------|
| `FunctionToolSpec` | Name, agent-facing description, JSON schema map for provider tools. |
| `FunctionToolDefinition` | Catalog metadata, including optional human-facing description and user-toggleable/default flags. |
| `FunctionToolCatalog` / `AgentFunctionToolSpecs` / `UserToggleableFunctionToolSpecs` | Ordered static catalog projections used by agent/tool metadata assembly. |
| `RecallTool`, `NewVectorStoreMemoryTool`, `NewScratchpadTool` | Wired from `internal/agent` when building tool lists. |
| `OpenAIToolUnionParam` | Builds `responses.ToolUnionParam` from a spec. |

## Dependencies

- **Inbound:** `internal/agent` (`tools.go`, `processtoolcall.go`) constructs and dispatches these tools.
- **Outbound:** `internal/datastore`, OpenAI client (search/embeddings), `zap`.

## Non-obvious decisions

- **Find context:** `RecallTool` implements the agent-facing `find_context` surface, consolidating memory and attachment retrieval.
- **File reads:** `FileReadTool` implements `read_file` (line ranges, cursor, outline) and `grep_files` (host-side literal/RE2 search) over uploaded text files and workspace files; `file_read_outline.go` builds outlines and `file_text_cache.go` is the shared byte-bounded LRU.
- **Entities:** `EntityTool` (`entities.go`) implements `recall_entity`, `remember_entity` (create / update with `base_revision` / forget) and `list kind=entities`, and `Spot`, which matches 1-4 word phrases of a message against entity names and aliases (cached per user and personality for a minute, invalidated by this process's writes).
  A one-word name that is also a common English word only matches when capitalized, and `RenderEntityCards` formats the matched cards for the turn's context.
- **Workspace:** `WorkspaceTool` (`workspace.go`) implements `write_file` (write / append / edit / delete) over revisioned text files under `agent/` (the personality's notebook) and `chat/` (this conversation), and backs `list kind=workspace`.
  Overwrites, edits and deletes must pass the `base_revision` the agent read, so a stale write is refused with a typed conflict instead of replacing newer content.
  Each revision is an immutable object under `users/{uid}/workspace/` (`WorkspaceObjectPrefix`), which attachment sweeps must skip.
  `conversation` pages from the start of a thread toward its end with a conversation-scoped `(sent_at, message_id)` keyset cursor; relative time scopes are frozen in that cursor so later pages cannot drift.
- **`list` conversations:** Empty shells (no messages / nil `last_message_time`) are excluded via `ChatFilters.HasMessages` so they do not crowd out real conversations under Postgres `DESC NULLS FIRST` sort.
  Unlike the HTTP sidebar list, agent discovery includes archived threads so imported history can be read with `find_context` without rehydrating it.
- **`list` jobs:** Emits `next_runtime` for non-terminal jobs only; does not echo raw `schedule_input` (e.g. "in 5 minutes" next to `complete` is noise).
- **`list` pagination:** Files/conversations/jobs (and personalities/skills) accept `page` + `limit`; results include `page`/`total_count`/`has_more` and a note that suggests `page=N+1` when more remain.
- **Human vs agent descriptions:** Built-in user-toggleable tools provide `FunctionToolDefinition.HumanDescription` for presentation.
  `internal/agent.GetAvailableTools` trims and prefers that field, but falls back to `FunctionToolSpec.Description` when a runtime/external tool omits it or supplies only whitespace.
  Provider-facing tool prompts remain unchanged on `FunctionToolSpec.Description`.
- **Spec parity:** `toolconstants_test.go` asserts OpenAI function tool projection stays aligned with the shared catalog; Claude schema sanitization is tested in `internal/agent/provider`.
- **Execution location:** Some tools are defined here only as shared schema/registration (`run_subagent`) while execution lives in `internal/agent` to reuse chat/user/provider context safely.
- **Restricted chats (memory sensitivity):** `sandbox.go` holds the one rule set for a chat whose `memory_sensitivity_limit` is below `sensitive`.
  `memoryReadableBy` (limit, plus no other conversation's summary), `otherConversationBlocked`, `fileInChatScope` and `cappedMemorySensitivity` are the predicates every tool asks; `restrictedNote` is the shared refusal text.
  `find_context` passes the limit into `GetRelatedMemories` (SQL), resolves a hidden memory like a missing one (`resolveMemory`), refuses other conversations in `conversation`, `bookmarks`, `origin`, `fetch summary:/bookmark:` and `source_type=summaries`, scopes file lookups to the chat and personality, and filters `lifecycle_events` to fold events within the limit without their pre-merge member previews.
  `list` refuses conversations, jobs and account-wide files; `parseWorkspacePath` and `listForChat` close the `agent/` notebook; `update_scratchpad` refuses.
  `create_memory` takes an optional `sensitivity` of `personal` or `sensitive` only (an agent can never mark a memory `public`); the default level is capped by the chat's limit and an explicit `sensitive` is kept, and a restricted chat's memory is always Chat scope (the result carries a note).
  `remember_entity` in a restricted chat can only create (at the chat's limit): updating or forgetting an existing entity is refused, and the refusal for a taken name is identical whether the holder is readable or above the limit.
  Every entity read takes the limit (the spotting alias index is cached per limit).
  A hidden memory, an unknown id and an ambiguous id prefix give a restricted chat the same `not found` error (`resolveMemory`).

## Testing

- `catalog_human_description_test.go` — every built-in user-toggleable catalog entry has non-empty human-facing copy distinct from its agent-facing prompt; runtime/external definitions may omit it and rely on the API fallback contract.
- `list_test.go` — unified list kinds (filters, scopes, empty-conversation / terminal-job omission).
- `tool_helpers_test.go` — marshaling and validation utilities.
- `toolconstants_test.go` — schema parity with shared specs.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — agent layer and tools; **Model context** for how tools attach to chat turns.
