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
  - `move_files` — files gallery files (images, documents, any upload) in a folder or back at the top level (`move_files.go`; tests in `move_files_test.go`).
    A personality's documents stay attached to it; only the folder label changes.
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
  `conversation` pages from the start of a thread toward its end with a conversation-scoped `(sent_at, message_id)` keyset cursor; relative time scopes are frozen in that cursor so later pages cannot drift.
- **`list` conversations:** Empty shells (no messages / nil `last_message_time`) are excluded via `ChatFilters.HasMessages` so they do not crowd out real conversations under Postgres `DESC NULLS FIRST` sort.
  Unlike the HTTP sidebar list, agent discovery includes archived threads so imported history can be read with `find_context` without rehydrating it.
- **`list` jobs:** Emits `next_runtime` for non-terminal jobs only; does not echo raw `schedule_input` (e.g. "in 5 minutes" next to `complete` is noise).
- **`list` files by folder:** `folder` filters to a gallery folder and everything beneath it (`FileAttachmentFilters.FolderPrefix`), normalized with `models.NormalizeFolder` so case and slashes do not matter, and each row shows its `folder`.
  An invalid folder is reported to the model without querying.
- **Folders for generated images:** `generate_image` takes an optional `folder`, validated before any image is generated (and paid for), and echoes where the images went.
  `move_files` takes up to 100 ids from `list`, reports how many moved, and notes when some were skipped.
- **`list` pagination:** Files/conversations/jobs (and personalities/skills) accept `page` + `limit`; results include `page`/`total_count`/`has_more` and a note that suggests `page=N+1` when more remain.
- **Human vs agent descriptions:** Built-in user-toggleable tools provide `FunctionToolDefinition.HumanDescription` for presentation.
  `internal/agent.GetAvailableTools` trims and prefers that field, but falls back to `FunctionToolSpec.Description` when a runtime/external tool omits it or supplies only whitespace.
  Provider-facing tool prompts remain unchanged on `FunctionToolSpec.Description`.
- **Spec parity:** `toolconstants_test.go` asserts OpenAI function tool projection stays aligned with the shared catalog; Claude schema sanitization is tested in `internal/agent/provider`.
- **Execution location:** Some tools are defined here only as shared schema/registration (`run_subagent`) while execution lives in `internal/agent` to reuse chat/user/provider context safely.
- **Sandbox policy:** every catalog entry declares a `SandboxPolicy` (`sandbox_policy.go`): `SandboxNever` (the zero value, so an undeclared tool fails closed) is never offered in a sandbox, `SandboxDefaultOff` is offered but starts in a new sandbox's `disabled_tools`, and `SandboxAllowed` is offered as in any chat.
  Every tool that is not `SandboxAllowed` is registered as a sandbox default-off tool from `init()` (`models.RegisterSandboxDefaultDisabledTool`; a build adding tools registers its own), so the Tools tab of a new sandbox simply shows them unticked.
- **Sandboxed chats:** `sandbox.go` holds the one rule set for a chat with `Chat.IsSandboxed()`.
  `memoryReadableBy` (a fetch by id) allows only a Chat-scoped memory or checkpoint summary of this conversation; `conversationReadable` allows only the chat itself; `fileInChatScope` allows only files uploaded to this conversation.
  Search retrieval is scoped in SQL (`GetRelatedMemories` takes the sandbox flag, `GetRelatedSummaryMemories` the one chat), and `find_context` lifecycle events list only folds of this chat's own memories.
  `list` refuses jobs, skills and personalities, lists conversations as just this one (so the model can reach its own id for `find_context`), and lists only this conversation's own files (`listSandboxFiles`); `update_scratchpad` and `move_files` refuse.
  `create_memory` always writes a Chat-scoped memory in a sandboxed chat, whatever scope was asked.
  A hidden memory, an unknown id and an ambiguous id prefix give a sandboxed chat the same `not found` error (`resolveMemory`).
  Refusals share `sandboxedNote`, so the model gets one consistent message.

## Testing

- `catalog_human_description_test.go` — every built-in user-toggleable catalog entry has non-empty human-facing copy distinct from its agent-facing prompt; runtime/external definitions may omit it and rely on the API fallback contract.
- `list_test.go` — unified list kinds (filters, scopes, empty-conversation / terminal-job omission).
- `tool_helpers_test.go` — marshaling and validation utilities.
- `toolconstants_test.go` — schema parity with shared specs.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — agent layer and tools; **Model context** for how tools attach to chat turns.
