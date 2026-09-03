# Package: `internal/models`

## Role

**Shared API and domain DTOs** used by HTTP handlers, the agent, and datastore adapters — JSON tags and field shapes aligned with `openapi.yaml` and Ent-backed persistence where applicable.

## Responsibilities

- Request/response structs for chats, messages, memories, personalities, rituals, models, jobs, agent jobs (including schedule types), billing-related payloads, file attachments, MCP servers, users, quotas, etc.
- **Defaults and validation helpers** where present (e.g. `model_defaults.go` for model configuration).
- Keeps handler payloads decoupled from Ent generated types (handlers map to/from Ent in datastore or thin adapters).

## Key types and entry points

- Split across files by domain: `chat.go`, `chatmessage.go`, `memory.go`, `personality.go`, `ritual.go`, `agentjob.go`, `agentjob_schedule.go`, `fileattachment.go`, `user.go`, `mcpserver.go`, `quotabucket.go`, etc.
- **Conversation import (`chat_import.go`):** `ImportConversation` (parsed thread incl. `Source`) carries optional account-export continuation fields (checkpoint state, remapped personality, and per-chat UI/tool state) while regular OpenAI/Anthropic imports retain the archived/lazy-rehydration defaults; `ImportResult` and `ImportProgress` report `chat_import` work (the JSON written to `Job.progress`: phase/source/total/imported/skipped). Source constants (`ChatSourceOpenAI`/`ChatSourceAnthropic`) and rehydration-state constants (`RehydrationState*`) also live here; `Chat.RehydrationState` and `Job.Progress` surface them.
- **`model_defaults_test.go`** — documents expected defaults for model DTOs.
- **Context X-ray (`contextbreakdown.go`):** `ContextBreakdown` + `ContextSegmentStat` describe the per-turn model-context composition surfaced on `ChatMessage.ContextBreakdown` (assistant rows). `ContextBudgetTokens` (30k) is the display denominator, mirroring the agent's `checkpointMaxLastInputTokens` compaction ceiling. The total uses vendor-reported input usage when available; named context/tool estimates are cl100k and `vendor_prompt_other` reconciles vendor framing, image input, and other un-attributable usage.
- **Error envelope (`error.go`):** `ErrorResponse` plus the error-code registry — `ErrCode*` constants and `GenericErrorCode(status)`. This is the single source of truth for the taxonomy; the `ErrorCode` enum in `openapi.yaml` mirrors it and must be updated alongside.

## Dependencies

- **Inbound:** All handlers, `internal/agent`, `internal/datastore`, tests.
- **Outbound:** Standard library only in most files; no Ent import in typical DTO files.

## Non-obvious decisions

- When adding fields, update **OpenAPI** and any **frontend** contracts in sync; this package is the backend source of truth for JSON shapes.
- **Error codes have two tiers with different guarantees (ADR 0x020).** Specific codes are a published contract and are never renamed or repurposed. Generic codes are status-derived defaults and explicitly *not* stable branch targets, so a call site may narrow from a generic to a specific code freely. `ErrorResponse.Code` deliberately omits `omitempty`, so a missing code shows up as `""` rather than disappearing.
- **`ErrorResponse.Error` is deprecated** and duplicates `Message`. It exists only until the frontend reads `Message`/`Code`; it should not gain new meaning.

## Testing

- `model_defaults_test.go` — default model configuration.
- `error_test.go` — status-to-code mapping, class fallbacks, and generic-code uniqueness.
- `mcpserver_test.go` — MCP server model validation or serialization.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Models and Shared Types**; repo root `openapi.yaml`.
