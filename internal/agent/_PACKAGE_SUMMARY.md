# Package: `internal/agent`

## Role

Orchestrates assistant behavior: user turns, OpenAI/Anthropic calls, tool execution loops, memories, rituals, scratchpads, checkpoints, file/MCP context, and background agent jobs.

## Responsibilities

- **`Agent`** (`NewAgent`): central service constructed with datastore, telemetry, OpenAI key, optional Anthropic key, and S3 file store.
- **Chat turns:** `HandleUserMessage` and related paths build `provider.ModelContext` via `messageContextBuilder` / `buildModelContextForChatMessage`, attach tools, run the model (`openAIResponseParamsForChat` → `ModelContext.BuildOpenAIResponseParams`), and execute tool rounds (`agentloop`).
- **Context:** History, carry-over after checkpoints, token budgeting, memories (including **rehydrated** `ChatMessage.additional_context` from prior user turns plus this turn’s prefetch), attachments, personality files, MCP tools, timezone-aware user lines (`[sys:Mon 2006-01-02 15:04:05 -07:00]` via `formatUserMessageWithTime` / `agentMessageTimestampLayout` — weekday + local wall time + offset, not ISO-8601).
  **`messageContextBuilder`** attaches image **`file_id`**s to the final user segment for OpenAI; **`loadImageBytesForClaude`** fills **`RawBytes`** for Claude multimodal turns when bytes are available (see `internal/agent/provider` package summary).
- **Tools:** Build a shared per-turn tool policy (`tools.go`), derive provider tool lists from the `internal/agent/tools` catalog, dispatch tool calls through a registered handler map, and support delegated subagent calls (`list_models`, `list_personalities`, `run_subagent`).
- **Mode tools/context:** agent-facing tools `list_modes` + `change_mode` map to internal mood records (`active_mood_id` / `is_auto_mood`), with active-mood resolution/reconciliation and mood prompt text as **`SegmentKindMood`** appended inside **`messageContextBuilder.build`** after merged additional context and before expression continuity and the final user turn (same segment order for OpenAI and Claude).
- **Remote MCP parity:** Chat/ritual MCP server selection is shared across providers; OpenAI uses Responses MCP tools and Claude uses Anthropic Beta MCP client mood when MCP servers are present.
- **Maintenance prompts:** Chat naming, personality generation, conversation summaries, memory extraction, scratchpad summarize/update (often **manual** `ResponseNewParams` — see architecture doc).
  **Archival** checkpoint scratchpad update and memory extraction use fixed small models (`archivalOpenAIModel` / `archivalClaudeModel` in `archival_models.go`); **checkpoint conversation summary always uses `archivalOpenAIModel` (`gpt-5-mini`)** for both OpenAI and Claude chats via unified `summarizeConversationForCheckpoint` (OpenAI threads `PreviousResponseID`; Claude renders explicit input items from inference `ModelContext`).
  Persona `archival_model` and custom memory read/write prompts are deprecated and ignored; optional `scratchpad_update_prompt` is still used for checkpoint scratchpad updates.
- **Background backfills** (started from `internal/server`): `StartSummaryMemoryBackfill` (`message.go`) copies legacy checkpoint summaries into Summary memories once at boot.
  `StartMemoryEmbeddingBackfill` (`memory_embedding_backfill.go`) embeds active non-Summary memories that have no Embedding row, via `datastore.BackfillMemoryEmbeddings` and the memory tool's batch embeddings call.
  It runs once, at startup, on the server lifecycle context, and never on a timer: memories are embedded when they are saved, so it only catches up older rows and any save-time failure since the last restart.
  The pass runs only on the instance that wins the Postgres advisory lock `MemoryEmbeddingBackfillLockKey` (default 80920033); other instances, and databases without advisory locks, skip it quietly.
  It does not start at all under mock/local LLM backends (`nonVendorLLM`), where embeddings have no provider.
  Both backfills are idempotent and only log failures; a pass stops early when the provider is unreachable (e.g. the deny-network client under mock/local backends).
- **Rituals:** User and system rituals, image ritual flow, registry of built-in system rituals.
- **Jobs:** Running scheduled/async agent work (`agentjob_run.go`, `agentjob_schedule.go`) and tools that create jobs.
- **Thread rehydration (`thread_rehydration.go`):** Lazy summarization of **imported** threads.
  `EnqueueThreadRehydration` (called from the chat handler when an imported thread is unarchived) marks `Chat.rehydration_state=pending`, creates a `JobTypeThreadRehydration` job, and runs `summarizeImportedThread` in a detached goroutine: it loads the full transcript, keeps the last `rehydrationKeepTurns` (5) user turns live, and summarizes everything before that — single-pass for normal threads, **map-reduce** chunked (`chunkMessagesByChars`) for very long (100+ turn) ones — via the fixed `archivalOpenAIModel` + `checkpointConversationSummaryInstructions`.
  It persists summary + window pointer atomically (`SetImportedThreadRehydrated`, `last_checkpoint_at` = sent_at of the n-5 turn) and flips state to `ready`.
  After the state is `ready` (so the inference gate is already released), it also **seeds long-term memories** via `extractAndStoreImportedMemories`: mines up to `importMemoryMaxPerThread` (20) durable memories from the full transcript using the live memory-extraction prompt **minus** the scratchpad delta (`importMemoryExtractionInstructions` + `memoryExtractionSchema`), chunked for long threads, and stores each as an embedded `Memory` tied to the chat (best-effort; never fails the job).
  This runs for both the summarized and short-thread paths so an import leaves the user with ~10-20 memories per rehydrated thread.
  **`WaitForThreadRehydration`** is the inference gate: `handleUserMessage`/`handleEphemeralPrompt` call it to stall a turn (bounded by `rehydrationWaitTimeout`, graceful degrade on timeout) until an in-flight summary settles.
- **Per-chat turn serialization (`chat_turn_gate.go`, #254):** turns in one chat run one at a time, in job order, across API instances.
  `handleUserMessage` and `handleEphemeralPrompt` call the gate (`awaitUserChatTurn` / `beginEphemeralChatTurn`) after the job is marked processing and before the rehydration gate and `prepareChatContext`, so a queued turn builds its context on the earlier turn's reply.
  It is job-ordered single-flight: a turn waits until no older `chat_message`/`agent_job_run` job in its chat is still pending or processing (`ListPendingTurnJobsForChat`, ordered by `created_at` then id at microsecond precision).
  The gate opens at `inference_complete`, when the reply and response chain are saved (`persistInferencePhase` writes the chat before the job) and when the web client unlocks its composer; the earlier turn's expression and checkpoint overlap the next turn, whose scratchpad write is conditional.
  Waiting polls with backoff (250ms to 2s); a turn in this process reaching `inference_complete` (`noteTurnJobStatus`, from the job phase helpers) wakes that chat's waiters at once (`chatTurnTracker`, keyed per chat).
  Nothing is held while a turn runs: no advisory lock or pinned connection.
  Every queued or running turn heartbeats its job (`TouchJob`, every 30s), and a pending/processing job without a write for `chatTurnStaleAfter` (2m) is treated as dead and does not block.
  A turn that queues longer than `chatTurnWaitTimeout` (10m; queued turns heartbeat too, so it need not stay below the stale bound) fails with `ErrChatTurnWaitTimeout` instead of running concurrently.
  A queued turn whose job was cancelled meanwhile (Stop on another instance) does not run (`errQueuedTurnCancelled`), and a cancelled queued chat turn is marked cancelled.
  A scheduled run has no job row, so `beginEphemeralChatTurn` creates an `agent_job_run` ticket job (reference = chat id) to hold its place and finishes it with the run's outcome.
  `handleUserMessage` owns its job's status, so its release also finishes a job the turn left non-terminal (a lost final status write): complete if it saved a reply, else failed.
  On shutdown, `FailInFlightTurns` (called by `server.Shutdown`) finishes this process's in-flight turn jobs: complete if past their reply, else failed.
  `finalizeChat` renames a new chat without writing `response_id`, which the next turn may already have moved on.
- **Scratchpad optimistic concurrency (`scratchpad_commit.go`, #254):** a checkpoint's scratchpad write is conditional on the revision its turn read (`Chat.ScratchpadRevision`).
  On `datastore.ErrScratchpadConflict`, `commitScratchpadUpdate` reloads the personality and regenerates the update once against the latest scratchpad, writing conditionally on that revision; a second conflict skips the update (logged) rather than overwrite it.
  On that retry the latest scratchpad becomes the turn's previous scratchpad (`adoptScratchpadUpdate`; on the Claude path the context's scratchpad segment is swapped, on the OpenAI path `concurrentScratchpadNote` says it replaces the earlier one), so the memory delta holds only this conversation's changes.
  The `update_scratchpad` tool and user edits write unconditionally but bump the revision; the tool also records the new scratchpad and revision on the turn's chat, so the turn's own checkpoint does not conflict with it.
- **Post-processing:** Checkpoint policy, message sync helpers.

## Key types and entry points

| Symbol | Notes |
|--------|--------|
| `Agent` | Main struct; datastore, clients, file store, logger, telemetry, shared **`tokenCounter`** (segment token estimates). Optional **`testHooks`** (`agent_hooks.go`) is for unit tests only; production paths assert hooks are unset. |
| `HandleUserMessage` | Primary path from HTTP into a full model turn + persistence. |
| `messageContextBuilder` | Builds `ModelContext` segments (history, memories, user message, **`userMessageImagesFromAttachments`** for OpenAI `file_id` vision, etc.). |
| `buildModelContextForChatMessage` | Shared path for chat turns so user-segment handling stays consistent. |
| `openAIResponseParamsForChat` | Tools + personality file enrichment + `BuildOpenAIResponseParams`. |
| `HandleAgentLoop` / `ExecuteToolUseWithRecovery` | Multi-round tool execution with panic recovery (`agentloop.go`). |
| `webSearchTool` / `fetchPageTool` / `applyWebSearchPolicy` / `turnWebSearchCount` | Web search (ADR 0x021, `web_search_tool.go`, `tools.go`). With a `websearch.Service` (Parallel) configured, every model gets `web_search`/`fetch_page`, and nothing vendor-native runs: the native tool is not sent, the `native_web_search*.go` extraction is not wired into `runGeneration`, and metering counts successful `web_search` and `fetch_page` calls, flagged `WebSearchFirstParty` so they can be priced apart from native searches. Without one, vendor-native search is the fallback and metering uses the provider's count. The user's `web_search` toggle governs both, and its description (`WebSearchToggleDescription`) follows the mode. |
| `jobToolProgress` | Live tool timeline for an in-flight chat turn: records each call's start/finish into `Job.progress` (`models.ChatTurnProgress`, truncated previews), flushing buffered draft text and marking a paragraph boundary first (`tool_progress.go`). Writes happen on a background goroutine that always saves the newest snapshot (latest-wins, so order is kept and bursts coalesce), so a slow datastore never blocks the loop; `Close()` flushes the final state when the loop returns, with a bounded wait. The writer recovers its own panics so a cosmetic write can't crash the server. Nil-safe; best-effort. |
| `memoryLoadProgress` | Shows memory retrieval in a chat job's live tool timeline (`Job.progress`, `memory_progress.go`, via `loadTurnMemories`): a `Load Memory` row that is running during retrieval and then completes with the retrieved memories (or errors), the same row the saved reply carries. Retrieval that finds nothing removes the row, as the saved reply has none. The row is written only when retrieval really runs (`memoryEnrichmentRuns`: not in mock/local mode). The tool recorder is seeded with it (`jobToolProgress.Seed`) so its snapshots keep the row, and the saved reply lists it before the turn's other tool calls. Writes are bounded, best-effort and detached from turn cancellation. Nil-safe; agent-job runs pass nil. |
| `buildTurnToolPolicy` / `getChatTools` / `dispatchToolUse` | Shared tool policy, provider-specific tool assembly, and handler routing (`tools.go`, `processtoolcall.go`). |

Subpackages: `provider/` (model context & SDK mapping), `tools/` (per-tool implementations), `embedding/`, `filechunker/`.

## Dependencies

- **Inbound:** `internal/handlers/chat` (and other handlers that invoke the agent), `internal/agentjobs/scheduler`.
- **Outbound:** `internal/datastore`, `internal/models`, `internal/metering`, `internal/agent/provider`, `internal/storage`, `internal/telemetry`, OpenAI and Anthropic SDKs.

## Non-obvious decisions

- **LLM backends (ADR 0x018):** `AgentConfig.LLMBackend` selects assistant generation.
  `mock` builds a per-request `provider.MockAdapter`; `local` uses `generateAssistantForMessageLocal` with a real local OpenAI-compatible `provider.LocalAdapter`, always targeting `LocalLLMModel` rather than a per-chat vendor model.
  **`runGeneration`** is the shared draft-buffer → `handleAgentLoop` → tool-call-merge → `saveAgentResponse` pipeline for OpenAI, Claude, Gemini, OpenAI-compatible, mock, and local paths, so their save/stream invariants cannot drift.
  **`assertGenerationProducedOutput`** gates the save: a turn whose model call returned no assistant text and no generated attachments fails instead of persisting a blank assistant message.
  A provider call can succeed at the transport level and still carry no text (stream closed after `message_start`, truncation before any text, a content block shape the extractor does not recognise); persisting that produced a turn that looked answered but was empty, with no error and no retry offered — and because `AppendHistoryTurn` skips empty content, the blank row then vanished from the rebuilt context on the next turn.
  Attachment-only turns (image rituals) are explicitly allowed.
  The failure is logged with `stop_reason` and `output_tokens`, which distinguish "text was generated and dropped in extraction" from "nothing came back".
  An **output-length truncation** (`stop_reason` `max_tokens` / `max_output_tokens`, via `isTruncationStopReason`) — common on tool-use-heavy GLM/Claude turns that spend the whole `DefaultMaxContentLength` budget on reasoning or a partial tool call before emitting any text — gets its own clearer, actionable user-facing message ("…cut off at the length limit…please try again") instead of the raw diagnostic dump; there is nothing to clip because no text block was produced.
  `LocalProvider` deliberately uses its own real HTTP client while `AgentConfig.HTTPClient` (deny transport) covers every other provider.
  That local client gets `AgentConfig.LLMCallTimeouts` through `provider.WithCallTimeouts`; the server has already applied the same limits to `HTTPClient`.
  Image rituals persist an embedded fixture PNG (`mock_fixture.go`) through `saveImageRitualResult` under both non-vendor backends.
  Downstream consumers key off `Agent.nonVendorLLM()` (mock **or** local) for deliberate skip/fake behavior — memory enrichment no-ops (`getMemoriesForEnrichment`), chat naming uses `mockChatName`, and checkpoint evaluation (`postMessageProcessing`) plus expression classification (`applyExpressionPhase`) are skipped — so nothing unexpectedly reaches the deny transport.
- **`saveImageRitualResult`** attaches the created PNG attachment to the returned assistant message (previously discarded, leaving `Attachments` empty on the return value); shared by real and mock ritual paths.
- **Streamed generation failures:** `runGeneration` persists text deltas on the job as they arrive.
  If the agent loop subsequently fails, `setJobStatusFailedWithPartial` atomically promotes those deltas to an assistant message through `datastore.FinalizeFailedChatJobWithPartial`, then keeps the failure on the triggering user message for the existing UI banner.
  Cancellation follows the analogous `FinalizeCancelledChatJobWithPartial` path.
- **Context X-ray capture:** `generateAssistantForMessage` (`message.go`) is a thin wrapper: it runs telemetry + `dispatchAssistantGeneration` (the provider branch), then on success calls **`persistContextBreakdown`** — one funnel so every provider path (mock/local/image-ritual/OpenAI/Claude/Gemini/Chat-Completions) captures the same per-turn snapshot.
  **`buildContextBreakdown`** maps `ModelContext.SegmentBreakdown` into `models.ContextBreakdown` (segment/token rows, the checkpoint policy's `checkpointMaxLastInputTokens` display budget, model+provider), and the value is persisted on the assistant message via `datastore.SetChatMessageContextBreakdown` (best-effort: logged, never fails the turn) **and** set in-memory.
  It also attaches **`Inputs`** (`context_inputs.go`): the turn's input manifest of memory IDs with their stage (`prefetch` vs `tool`) and relevance, memories replayed from earlier turns, the active mood, and short SHA-256 prefixes of the scratchpad and summary, references only and never content.
  Read back by the frontend "Context" panel tab.
  Estimates are cl100k text-token estimates, exclude image-token usage, and are not billed usage.
  It is deliberately **not** stored as a `ChatMessageContextItem` — `appendMergedAdditionalContext` re-injects every non-MEMORY item type back into the model context, which would feed the breakdown JSON back to the model; a dedicated `chat_message.context_breakdown` column avoids that.
- **`HandleAgentJobPrompt`** (`agentjob_run.go`) applies model/personality overrides only in-memory on `chatContext`; `UpdateChat` receives a copy with the **persisted** chat `ModelID` / `PersonalityID` so scheduled-job overrides never overwrite the chat row.
  `handleEphemeralPrompt` runs on the context its caller passes: the async `agent_job_run` worker passes its cancellable run context, so cancelling that job stops the turn, while the sync entry points (`HandleAgentJobPrompt`, `HandleEphemeralPromptSync`) detach first (`detachedUserContext`) and run to completion.
- **Reply hooks** (`reply_hook.go`): once a reply is saved and its job is `complete`, `runUserChatPostInferencePhases` and `handleEphemeralPrompt` call `fireReplyHook`, which hands pointers to any hooks registered in `internal/replyhook`.
  It is a no-op when none is linked, runs detached on the lifecycle context, and fires on success only.
- **Model context rules** (segment ordering, caching prefix, when **not** to hand-build `ResponseNewParams`) are documented in [architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) under **Model context and providers** and **When to build `ResponseNewParams` by hand**.
- **`messageContextBuilder.build`** requires non-nil telemetry with a non-nil logger (see architecture doc).
- **Prefetched memories** for a user turn are persisted on that user message (`additional_context` with type `MEMORY`) in **`persistInferencePhase`** (`job_phase.go`) so later turns can merge them with history; **`mergeAdditionalContextItems`** dedupes by type+scope+identity and **`appendMergedAdditionalContext`** emits `SegmentKindMemoryContext` (and `SegmentKindDeveloperContext` for other types).
  For MEMORY items the identity is the **metadata-stripped, normalized content** (`additionalContextDedupeIdentity`), not the rendered line: `FormatMemoryForContext` appends `age_days`/`relevance`/`reconfirmed` computed at render time, so a memory persisted on an earlier turn and the same memory rendered this turn are different strings.
  Keying on the raw line let one memory accumulate a near-duplicate copy per turn — identical `stored_at`, a different `age_days` on each — bounded only by the 50-message history window.
  On a duplicate the **later** (fresher) rendering replaces the earlier one in place, so the surviving copy's `age_days` describes now rather than the first turn that retrieved it.
- **Prior-turn expression continuity:** When history/carry-over loads **`generation_expression`** on the latest assistant message, **`appendPriorTurnExpressionContinuity`** (`message_context_builder.go`) adds developer-oriented user/context segments (`The previous *assistant* message was classified with the expression: "<key>"` plus optional **`(usage hint: …)`** and **` Rationale: …`** when classifier reasoning was saved) **after** memories and mood in the segment list so the main model aligns with the last portrait choice while keeping a durable cache prefix for Claude.
- **Expression images are filed in a folder:** `uploadExpressionCellAttachment` saves each generated expression cell (the default grid and the custom candidates) in `models.ExpressionFolder(personality name)`, so they land in `expressions/<name>` rather than the top level of the gallery.
  Personalities that share a name share a folder, and the folder is only a label; assigning an existing gallery image to an expression does not move it.
- **Tool-generated attachments** are saved with their message in `message.go`, and carry the `Folder` the tool chose (`generate_image`'s optional `folder`), so generated images can land in a gallery folder.
- **Default expression grid:** `GenerateDefaultExpressionGrid` (`expression_grid_generation.go`) — nano likeness from `SystemPrompt` + one medium `gpt-image-2.5-flare` 3×3 grid, splice **nine** cells (`SlicePNGGrid3x3`: after outer trim, width/height need not be multiples of 3; remainder pixels go on the last column/row so cells may differ by 1px), gallery upload + `UpsertPersonalityExpression` for `ExpressionGridKeys`.
  When the personality has a cover image, it is the direct reference for both the likeness pass and the image model (edit endpoint), as on the candidates path; without one the grid is prompt-only.
  Not quota-metered.
  Partial runs can leave a mix of set keys; safe to retry (upsert per key).
  Exposed via personality HTTP + expressions UI; create/accept flows do not auto-call.
- **Always-on reasoning models & truncated turns:** On the zai path `generateAssistantForMessageClaude` applies `provider.ApplyZAIReasoningEffort` (`high`) and installs a `SetTruncationFallback` that logs and drops to `low`; `XiaomiAdapter` handles its own thinking-off retry.
  If a turn still ends with no text, `assertGenerationProducedOutput` maps `max_tokens` / `max_output_tokens` / `length` (`isTruncationStopReason`) to the clearer "cut off at the length limit" error.
  `saveAgentResponse` persists `GenerateResponse.Reasoning` to `ChatMessage.ModelReasoning` (`model_reasoning` column) for the chat UI's collapsed "Thought process" disclosure; the context builder never reads it.
  **Live reasoning:** when the adapter implements `provider.ReasoningStreamer`, `runGeneration` wires a second `jobDraftDeltaBuffer` (`newJobDraftReasoningBuffer`) into `Job.draft_reasoning`; its `ResetReasoning` is the stream's `OnReset`, and each text delta flushes the reasoning tail first (buffers flush on the next delta, not a timer).
- **Stop (thread-wide, multi-instance):** `CancelJob` on a chat job stops every non-terminal chat job in that thread.
  A job whose worker is in this process is cancelled in-process; any other is marked cancelled in the DB, because the API runs more than one instance and a job may be orphaned by a restart.
  Each chat worker runs `watchChatJobCancel`, polling its status every `chatJobCancelPollInterval` (2s), so a Stop handled by another instance still stops it.
  Other job types keep the in-process-only cancel.
- **Custom expression candidates:** `EnqueueExpressionCandidatesJob` / `GenerateExpressionCandidates` (`expression_candidates.go`) — same pipeline (`generateExpressionGridCells`) for nine caller-chosen row-major keys (the canvas prompt is built from the keys), but cells are only uploaded as personality-pinned gallery images, **never assigned**; the completed `expression_grid` job's `Progress` (`ExpressionCandidatesProgress`, `mode: "candidates"`) lists `{expression_key, image_id}` and the client assigns keepers via the regular upsert.
  An optional reference image grounds the likeness pass **and** is sent to the image model via `OpenAIProvider.EditImagePNGBase64WithQuality` (images edit endpoint, no mask); a rejected edit falls back to prompt-only generation.
  Failed runs delete already-uploaded candidates.
  Shares the per-user media-job slot.
- **Expression portrait picker:** **`PickGenerationExpression`** forks the inference **`ModelContext`**, appends the assistant reply and task **user** turn, and uses **strict JSON schema** output (`expression_key` + `reasoning`) with **`GenerateSchema`**, **`Text.Format`**, and **`ProcessResponseOutput`**.
  On failure or unknown key, **no** portrait is selected (no default first slot).
  **`generation_expression_reasoning`** is persisted when set; continuity echoes it.
  Minimal standalone prompt when context is nil.
- **Claude + user images:** Chat turns call **`loadImageBytesForClaude`** before building `ModelContext` so **`UserMessageImage.RawBytes`** can be set; **`renderClaudeContext`** then emits image blocks.
  OpenAI still uses **`file_id`** for vision.
  Details in `provider/_PACKAGE_SUMMARY.md`.
- **Claude checkpoint context isolation:** `runCheckpointClaude` (`message.go`) runs scratchpad → memory on a **clone** of inference context because `updateScratchpadClaude` / `extractMemoriesWithScratchpadDeltaClaude` **mutate** the context they receive (scratchpad-update / memory-extraction prompt turns).
  **`checkpointArchivalContext`** appends the just-completed assistant reply before cloning (inference `ModelContext` predates that turn; OpenAI gets it via `PreviousResponseID`).
  The summarizer uses the **pristine** original `ModelContext` plus explicit `AssistantReply` through unified `summarizeConversationForCheckpoint` (gpt-5-mini, same prompt as OpenAI); it must not reuse the scratchpad clone.
  OpenAI summarizer threads off assistant `ResponseID`; Claude has no OpenAI thread ID.
- **Checkpoint memory merge embeddings:** `planFoldGroup` (`memory_merge_infer.go`) sets `NeedsEmbedding` when there is no survivor or when the canonical content differs from the survivor's, and `applyMemoryCompactionPlan` (`memory.go`) embeds the canonical content before `PersistMemoryMergeGroup`.
  The datastore then decides whether the survivor is rewritten (never when starred).
  Candidates carry `Starred` from the turn's loaded memories, so the planner skips that embedding for a survivor it can see is starred.
  If that embedding fails, a survivor fold still runs and keeps the survivor's wording; only a new-row fold is skipped.
- **Compaction throttle:** `decideCheckpoint` (`postprocessing_policy.go`) gates the **token-based** triggers behind `MinTurnsBetweenCheckpoints` (`checkpointMinTurnsBetweenCheckpoints` = 5) so a burst of tool-heavy turns (agent job runs with large web-search/tool results) cannot force compaction every turn.
  The scheduled turn-count trigger (`MinAssistantMessagesSinceCheckpoint`) is exempt.
- **Inference `call_path`:** New top-level flows that call the model should use **`Agent.withCallPath(ctx, path)`** (or ensure nested calls set `telemetry.WithCallPath`) so provider token metrics are labeled; see architecture doc.
  Per-turn side calls label themselves so they don't inherit the turn's path: mood auto-selection uses `mode_select` and the expression picker uses `expression_pick`.
- **Job and turn metrics** (`job_telemetry.go`): every async job worker (chat send/retry, the sync webhook path, `agent_job_run`, the three personality media jobs, thread rehydration) records `whatiff.job.queue.wait` and runs under `telemetry.TrackJob`.
  Outcomes come from the worker's result: `quota` for `ErrQuotaExceeded`, `cancelled`/`timeout` for context errors, `panic` when the worker panics (the tracker's defer runs after the worker's own recover), else `success`/`failed`.
  A new send and a retry share one goroutine body, `runAsyncChatMessageJob` (`message.go`), so both get the same panic guard (`recoverAsyncMessageJob` marks the job failed instead of crashing the process), cancel cleanup and outcome recording.
  Chat turns record `whatiff.chat.turn.stage.duration` for a fixed stage set (`rehydration_wait`, `prepare_context` including `memory_enrichment`, `mood`, `build_context`, `inference`, `expression`, `post_process` including `chat_name` and `checkpoint_scratchpad`/`memory`/`summary`/`persist`), labeled by the turn's `call_path` (`user_chat` or `agent_job`).
  `rehydration_wait` and `expression` are recorded only when the turn actually waits or runs the picker, so skipped turns don't add zero samples.
  `turn_queue_wait` (per-chat turn gate) is likewise recorded only when a turn queued behind an earlier one.
  Quota-gate rejections count `whatiff.quota.rejections` by `call_path`.
  Each tool call is timed on `whatiff.agent.tool.duration`; `toolMetricName` keeps the `tool` label bounded (catalog function tools by name, `mcp__*` as `mcp`, anything else, including made-up names, as `other`).
- **Delegated subagent path:** `run_subagent` uses a minimal context builder (`base+personality system prompt`, optional scratchpad, provided message only), explicitly excludes history/checkpoint/memory segments, and calls providers directly to avoid post-turn side effects.
- **Metering boundary:** The agent gates each billable turn through
  `metering.Meter.Check` and returns its opaque `Decision` to `Record` after
  completion. It owns neither quota math nor billing behavior: a private
  metering implementation may be linked to enforce usage limits and record
  usage, while builds without one fall back to `metering.NoopMeter` (allow
  all, record nothing). See `internal/metering`.

## Testing

- `agent_hooks.go` — `agentTestHooks` groups test-only seams (memory/history overrides, image ritual fakes).
  `assertNoTestHooksInProduction` runs under `NewAgent`, `handleUserMessage`, and `HandleAgentJobPrompt` when not inside `go test`.
- `agentloop_test.go` — tool loop and adapter append behavior.
- `job_telemetry_test.go` — job outcome mapping (success, quota, cancelled, timeout, failed, panic), media job tracking, bounded tool labels, quota rejections and turn stage labels.
- `context_rebuild_test.go` — model switch / async job input shapes; persisted additional context from history.
- `context_breakdown_test.go` — `buildContextBreakdown` totals/budget/model stamping and nil-input guards for the Context X-ray.
- `message_context_builder_test.go` — builder ordering; `mergeAdditionalContextItems` dedupe.
- `expression_generation_test.go` — expression picker JSON / markdown-fence parsing helpers.
- `expression_grid_generation_test.go` — 3×3 PNG grid splice (`SlicePNGGrid3x3`), key-driven canvas prompt.
- `message_context_builder_expression_test.go` — prior-turn expression snapshot selection for continuity text.
- `conversation_summary_test.go`, `scratchpad_test.go`, `memory_test.go`, `postprocessing_policy_test.go` — maintenance prompts and checkpoints.
- `thread_rehydration_test.go` — imported-thread split at n-5 turns, assistant counting, char-budget chunking, and transcript rendering.
- `chat_turn_gate_test.go` — turn gate on an in-memory `chatTurnStore` (`agentTestHooks.ChatTurnStore`): waits until the older turn reaches `inference_complete`, replied/terminal/stale/newer/other-chat jobs don't block, heartbeats, timeout, cancel (context and job cancelled while queued), per-chat wake, job-order serialization, shutdown finishing, scheduled-run tickets.
- `scratchpad_commit_test.go` — conditional scratchpad write: stale revision regenerates against the latest, a second conflict skips, and a forced interleave of two checkpoints keeps both updates.
- `message_test.go`, `message_timezone_test.go` — attachment labels, memories, tool-call context, human-readable `[sys:…]` timezone stamps (weekday + local offset).
- `mcp_tools_test.go` — MCP tool wiring (OpenAI + Claude MCP config mapping).
- `processtoolcall_test.go` — catalog-derived tool list and dispatch handler registration (including `list_models`, `list_personalities`, `run_subagent`).
- `subagent_tools_test.go` — minimal subagent context composition and argument validation behavior.
- `testsupport_test.go` — shared datastore and agent constructors for unit tests.
- `rituals_test.go`, `system_rituals_test.go` — ritual registry and system IDs.
- `tools_test.go` — native web-search registration and logical-toggle coverage.

## Related documentation

- [Architecture summary](../../docs/ARCHITECTURE_SUMMARY.md) — **Agent layer**, **Model context and providers**, **When to build `ResponseNewParams` by hand**.
