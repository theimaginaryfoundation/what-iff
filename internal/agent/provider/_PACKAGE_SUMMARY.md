# Package: `internal/agent/provider`

## Role

Maps **`ModelContext`** (ordered prompt segments) to OpenAI Responses and Anthropic Messages APIs, plus shared tool JSON, adapters, and token counting.

## Responsibilities

- **`ModelContext` / `ModelContextSegment`:** Segment kinds (including dedicated mode prompt segment) and append/insert helpers (`InsertBeforeLastUserMessage` for optional developer lines immediately before the final user turn).
  **`StripUserMessageImages`** clears multimodal payloads when reusing a context for APIs that only accept text (e.g. expression picker + strict JSON schema on Responses API).
- **OpenAI:** `RenderOpenAIInputItems`, `ModelContext.BuildOpenAIResponseParams`, `OpenAIProvider`, `OpenAIAdapter`, `ProcessResponseOutput` (assistant text extraction / dedup), `BuildOpenAITools`, image generation helpers, file-attachment upload helpers.
  **`SegmentKindUserMessage` with `RoleDeveloper` and images** (expression continuity portrait) is rendered as **two** input items—developer text plus a **user** message carrying `input_image`, because the Responses API allows images only under the user role for message parts.
- **Claude:** `BuildClaudeParams`, `ClaudeProvider`, `ClaudeAdapter`, `ClaudeFunctionTool`, `ExtractClaudeText`, `ExtractClaudeBetaText`, `UnmarshalClaudeTextJSON`.
  **`renderClaudeContext`** splits **developer continuity + portrait** like OpenAI: text turn includes `ExpressionPortraitContinuityPointerNote`; the next user message carries caption + image blocks.
  **Prompt caching:** the last block of the leading **contiguous `Cacheable` segment prefix** gets Anthropic **`cache_control` `ephemeral`** with **5m TTL** (see `model_context_claude.go`).
- **Claude remote MCP:** `ClaudeAdapter` switches to Anthropic Beta Messages MCP client mode when MCP server config is supplied (`mcp_servers` + MCP toolsets).
- **z.ai GLM (Anthropic-compatible):** GLM models (`provider = "zai"`) reuse the **entire Claude path** via `NewClaudeProviderWithBaseURL` pointed at z.ai's Anthropic-compatible endpoint (`DefaultZAIBaseURL`).
  Routing uses `models.UsesAnthropicMessagesAPI` (true for anthropic + zai).
  Anthropic-native features (native web search, beta MCP) are gated off for z.ai via `models.IsNativeAnthropicModel`; prompt caching (`cache_control`) is left on (z.ai supports it).
  **Reasoning effort:** GLM always thinks (disabling is rejected) and **ignores `thinking.budget_tokens`** (verified live 2026-09-22), so unbounded reasoning could consume the whole output cap and truncate the turn before any answer.
  `ApplyZAIReasoningEffort` (called on the zai path in `internal/agent`) raises `MaxTokens` to `ReasoningMaxOutputTokens` (2× `DefaultMaxContentLength`) and sets **`output_config.effort`** — the one lever z.ai honors.
  Unset behaves like `max`; `ZAIReasoningEffort` = `"high"` cut reasoning ~5× in testing (z.ai accepts only `low`/`high`/`max`; `medium` is rejected).
  `ClaudeAdapter.SetTruncationFallback` installs a one-shot retry: a non-beta call cut off at `max_tokens` with no reply text is discarded and re-issued once after the fallback mutates params (the zai path drops to `ZAIFallbackReasoningEffort` = `"low"`).
  Native Anthropic sets no thinking/effort param, so reasoning stays off there.
- **Gemini (OpenAI Chat Completions compatible):** Gemini models (`provider = "google"`) use a **separate** `GeminiProvider` + `GeminiAdapter` + `BuildGeminiParams`/`renderGeminiMessages` built on the openai-go **Chat Completions** API (not the Responses API), pointed at `DefaultGeminiBaseURL`.
  **`BuildOpenAIChatCompletionParams`** / `renderGeminiMessages` are also shared by Mistral, DeepSeek, Qwen, and Xiaomi.
  The agent renders from a **`PrepareForTextOnly`** clone on every provider path when the model row's **`vision_support`** is false (see `visionRenderContext` in `internal/agent`).
  `renderGeminiMessages` maps developer-ish segments (`SegmentKindToolResult`, `SegmentKindAttachmentContext`, mood/developer context) to **user** messages; expression portrait and final user turns support multimodal `RawBytes` when vision is enabled.
  When a text-delta handler is attached, Gemini and the other Chat Completions providers stream via `streamChatCompletion`, which accumulates content, tool calls, and final usage; function tools use `OpenAIChatCompletionFunctionTool` / `GeminiFunctionTool`.
  **Streamed tool calls carry no raw JSON** (the accumulator rebuilds them from deltas), so `geminiToolCallToParam` reconstructs the request param from the decoded fields (`geminiToolCallParamFromFields`) rather than the SDK's `ToParam()`, whose empty raw-JSON override otherwise fails to marshal ("unexpected end of JSON input") on the next round — the failure image-gen turns reliably hit, since every Gemini turn streams and image-gen is a guaranteed single tool call.
  The accumulator also drops Google's non-standard `extra_content.google.thought_signature`, which Google **requires** echoed back on the assistant tool-call turn (else a 400 "Function call is missing a thought_signature").
  `GeminiProvider.CallStreaming` recovers it per tool-call index from the raw deltas (via `streamChatCompletionCapturing`'s `onToolCallDelta` hook) and the adapter re-attaches it in `geminiAssistantToolCallMessage`.
  Non-streaming responses keep it in their own raw JSON, so the raw path preserves it there.
  **Tool-call placeholder echo (#142):** Gemini rejects an assistant tool-call turn with empty content, so `geminiAssistantToolCallMessage` fills it with the internal `geminiToolCallContentPlaceholder` (`"[tool call]"`).
  That placeholder lives only in the in-turn outbound `params.Messages` (never persisted), but the model sees it as its own prior output and can imitate it, streaming it back as response text — which `runGeneration` would concatenate into the draft stream and saved message.
  Whenever an outbound **assistant** turn carries the placeholder (`geminiMessagesCarryToolCallPlaceholder`), the adapter wraps the text-delta handler in `geminiToolCallEchoFilter` and strips the final `Text` via `stripGeminiToolCallEcho`; both remove only **leading** echoes of the placeholder.
  User-authored text never arms the filter and is never rewritten.
- **User-attached images (chat):** `ModelContextSegment.UserImages` + `AppendUserMessage`.
  OpenAI Responses gets `input_image` with the OpenAI **`file_id`** from upload.
  **Claude** uses **`UserMessageImage.RawBytes`** when set (see **`loadImageBytesForClaude`**); otherwise **`renderClaudeContext`** falls back to **text-only** for that turn.
  **`HydrateUserMessageImages`** can prefetch bytes from storage for reuse.
- **Xiaomi MiMo (Chat Completions):** `XiaomiAdapter` always reasons, streams it as the non-standard `reasoning_content`, and honors **neither** `budget_tokens` nor `reasoning_effort` — only `thinking: {"type":"disabled"}`.
  The adapter raises `MaxCompletionTokens` to `ReasoningMaxOutputTokens`, and when a call finishes with `finish_reason: "length"` and no reply text it is discarded and re-issued once with thinking disabled (which then stays off for the rest of the turn).
  `GenerateResponse.StopReason` carries `finish_reason`.
- **Model reasoning capture:** `GenerateResponse.Reasoning` carries the turn's reasoning text for display — joined across **every** tool round via the embedded `reasoningLog`, not just the final call.
  Sources: GLM `thinking` blocks (`ExtractClaudeThinking`, non-beta path) and MiMo `reasoning_content` (non-streamed via `ChatCompletionReasoning`; streamed via the `onReasoningDelta` hook on `streamChatCompletionCapturing`, since `ChatCompletionAccumulator` drops non-standard fields).
  A truncated-and-retried attempt's reasoning is dropped.
  Other providers leave it empty.
  Reasoning is never replayed into model context.
  **Live streaming:** `ClaudeAdapter` and `XiaomiAdapter` implement the optional `ReasoningStreamer` (`SetReasoningStream(ReasoningStream{OnDelta, OnReset})`) on streaming calls; `reasoningRelay` keeps the live draft identical to the saved text (round separators) and, on reset, clears it and re-sends the kept rounds.
  Resets fire on the truncation fallback and — Claude path only, via `retryAwareThinking` in `CallWithRetryStreamingReasoning` — on a transport retry after thinking streamed (the retry loop only refuses once *text* has streamed).
  Chat Completions streams are never replayed by the SDK, so MiMo needs no transport reset.
  Observed live: z.ai streams thinking incrementally at `max` but at `high` it is short enough to arrive in one burst right before text.
- **Unified iteration:** `AgentAdapter` + `ToolUse` / `ToolResult` for the multi-round agent loop.
- **`GenerateResponse`:** Normalized completion type from either provider.
  Carries **`StopReason`** — the provider's own verbatim account of why generation ended (`end_turn`, `max_tokens`, `refusal`, `incomplete`, …), sourced from `Message.StopReason` on Anthropic and from `IncompleteDetails.Reason` (falling back to `Status`) on the Responses API, and from `finish_reason` on the Xiaomi Chat Completions adapter (`length` = truncated).
  The agent's empty-turn guard reads it to surface a clearer truncation message.
  Also carries **`Reasoning`** (see "Model reasoning capture").
- **`TokenCounter` + carry-over selection:** Token budget and `SelectCarryOverTurns` for history trimming.
- **Inference metrics (`genai_metrics.go`):** every vendor call goes through one wrapper, and each wrapper records through a `genAICall` (`startGenAICall`, then `end(err)`) on `tel.Metrics`.
  Wrappers: `responsesNew`/`responsesNewStreaming` (timed once per logical call in `callWithRetry`), `messagesNew`/`betaMessagesNew`, the Claude streaming calls (timed in `callClaudeWithRetry`), `chatCompletionsNew`/`chatCompletionsStream` for every Chat Completions provider, the Images API calls and the Files/Containers calls.
  `gen_ai.client.operation.duration` has provider, model, operation (`chat`, `generate_image`, `edit_image`, `file`), `call_path` and, on failure, `error.type`; the duration includes app-level retries and their waits, and a stream is timed to its terminal event.
  `whatiff.gen_ai.time_to_first_token` is the first text or reasoning delta of the attempt that succeeded (each retry attempt calls `beginAttempt`).
  Tokens go to `gen_ai.client.token.usage` (no model label) and `whatiff.gen_ai.tokens` (with model) as `input`, `output`, `cached_input` and `reasoning`, recorded only when positive.
  Anthropic `input` keeps the full total (uncached + cache reads + cache writes); `cached_input` is cache reads only, because cache writes are not hits.
  DeepSeek's `prompt_cache_hit_tokens` counts as `cached_input`; returned usage values (used for metering) are unchanged.
  `whatiff.gen_ai.retries` counts `callWithRetry`/`callClaudeWithRetry` retries (`rate_limited`, `server_error`), the Claude truncation fallback (`truncated`) and the Xiaomi thinking-off retry (`length`).
  `whatiff.gen_ai.safety_blocks` counts calls that failed with a safety violation, or finished with a `content_filter` / `refusal` stop reason.
  The provider name is a `telemetry.Dependency*` constant; `ClaudeProvider` reports `zai` when built with a custom base URL (only z.ai uses one).
  An `init` registers `*openai.Error` and `*anthropic.Error` with `telemetry.RegisterStatusCodeFunc`, so `error.type` is status based wherever those errors are classified.
  `ModelContext.EstimatedTokensBySegment` supports segment-level token estimates for telemetry.
  Providers take `*telemetry.Telemetry`; `OpenAIProvider.zapLog()` uses `tel.Logger` for attachment/image helpers (falls back to zap Nop when nil).
- **`GenerateSchema`:** JSON-schema helper for structured model outputs.

## Key types and entry points

| Symbol | Notes |
|--------|--------|
| `ModelContext` | Segment list driving both providers; cacheable **contiguous prefix** convention (esp. Claude). |
| `ModelContext.SegmentBreakdown` | Rolls segments up by kind (tokens summed, segment/image counts, cacheable OR-ed) in **first-appearance order** for the per-turn "Context X-ray"; provider-neutral `SegmentKindStat` so `internal/agent` can map it to a DTO without this package importing `internal/models`. Sibling of `EstimatedTokensBySegment` (which the telemetry histograms use). |
| `RenderOpenAIInputItems` / `BuildOpenAIResponseParams` | OpenAI Responses API input + params. |
| `BuildClaudeParams` | Anthropic `MessageNewParams`. |
| `OpenAIAdapter` / `ClaudeAdapter` | Implements `AgentAdapter` for `agentloop`. |
| `ClaudeFunctionTool` | Converts provider-neutral function specs into Anthropic tool params; selection stays in `internal/agent`. |
| `BuildOpenAITools` | Merges function tools; image tool when model supports it. |
| `NewOpenAIProvider` / `NewClaudeProvider` | SDK clients; optional `*telemetry.Telemetry` (metrics + logger). All provider constructors take an optional `*http.Client` (nil = SDK default) so mock mode can inject the deny transport. |
| `MockAdapter` / `NewMockAdapter` | In-process `AgentAdapter` fake for `LLM_BACKEND=mock` (echo/fixed/scripted modes, whitespace-preserving word-delta streaming, ctx cancellation). Built per request. |
| `LocalProvider` / `LocalAdapter` | Real `AgentAdapter` for `LLM_BACKEND=local`: an OpenAI-compatible Chat Completions client pointed at a local server (Ollama default via `DefaultLocalBaseURL`; any compatible server via `LOCAL_LLM_BASE_URL`). Placeholder API key; non-streaming, mirrors the Mistral/DeepSeek adapter shape. |
| `DenyNetworkHTTPClient` / `ErrNetworkDenied` | `http.Client` whose transport fails every request before egress; injected into all provider clients under mock mode ("no provider egress" guarantee). |

## Dependencies

- **Inbound:** `internal/agent` (message handling, loops), `internal/agentjobs/schedule` (schedule parsing via `CallWithRetry`).
- **Outbound:** `internal/datastore` (OpenAI provider for some ops), OpenAI and Anthropic SDKs, `go.uber.org/zap`.

## Non-obvious decisions

- **Claude vs OpenAI for user images:** OpenAI uses **`file_id`** from upload.
  Claude needs **`RawBytes`** on each `UserMessageImage`; the chat path fills them via **`loadImageBytesForClaude`** before **`buildModelContextForChatMessage`**.
  Without bytes, `renderClaudeContext` keeps the user message text-only for that attachment.
- **Claude rendering:** Developer/system vs user ordering is covered in tests (`RenderClaudeContext_DeveloperBeforeUser`).
- **Consecutive same-role Claude turns are normal.**
  Segments render independently and nothing merges adjacent messages that share a role, so a request routinely carries several `user` messages in a row (history turn, `[CONTEXT]` memory turn, developer context, then the current user message).
  Anthropic accepts this.
  Do not "fix" it by collapsing adjacent roles without re-deriving the cache anchor: `lastContiguousCacheableSegmentIndex` marks `cache_control` on a specific segment's block, and the render tests pin the resulting message count.
- **Claude output cap is per-call.**
  `BuildClaudeParams` delegates to **`BuildClaudeParamsWithMaxTokens`**, which treats `<= 0` as `DefaultMaxContentLength`.
  This matches `OpenAIResponseParamsOptions.MaxOutputTokens` on the Responses path.
  A response truncated at the cap is otherwise indistinguishable from a complete one — `GenerateResponse.StopReason` (`max_tokens`) is the only signal.
- **Tool JSON:** `ClaudeFunctionTool` strips unsupported JSON-schema features so shared function specs work on Anthropic; provider tests use local schema fixtures to avoid importing the agent tool catalog.
- **Claude input-token totals include cached tokens:** Anthropic's `Usage.InputTokens` is the **uncached** portion only; the cached prompt prefix is reported separately in `CacheReadInputTokens` / `CacheCreationInputTokens`.
  `claudeTotalInputTokens` / `claudeBetaTotalInputTokens` sum all three so `GenerateResponse.InputTokens` and `recordProviderTokenUsage` match the true prompt size (OpenAI already reports a single combined `InputTokens`).
  Skipping this under-reports cached chats by the whole prefix (was ~3k constant) and starves the checkpoint token heuristics.
- **OpenAI tool continuations:** After an OpenAI Responses call, `OpenAIAdapter` sends only the newly produced function-call outputs with `previous_response_id`; re-sending the initial `ModelContext` would duplicate system/history/scratchpad content on every tool round.
  Instructions and the configured tool list remain on each request because the provider does not inherit them through the response chain.
  **`Call` sets `previous_response_id` at request entry and advances `previousResponseID` only *after* the call returns, so `params.PreviousResponseID` lags one round behind — it stays aligned with the outputs `AppendToolResults` staged.**
  `ForceFinalResponse` must therefore re-thread `previous_response_id` to the latest `previousResponseID` before it fires: it sends the *last* round's function-call outputs, and pairing them against the stale (prior-round) id makes OpenAI reject the whole turn with `400 No tool output found for function call <id>`.
  Pinned by `TestOpenAIAdapter_ForceFinalResponse_ThreadsLatestResponseID`.
- **Claude native web-search replay:** Intermediate text blocks are replayed without their provider-owned citations because Anthropic can return a citation with an empty `web_search_result_location.url`, then reject that same citation on the next request.
  The native `web_search_tool_result` block is still replayed unchanged, preserving its encrypted content and server-side search continuity.
- **Vendor-native web search is the no-key fallback (ADR 0x021):** its helpers live in `claude_native_web_search.go` and `openai_native_web_search.go`, not in the adapters.
  With first-party web search configured the agent never sends the native tool, so none of that code sees a block.
  `AgentAdapter.WebSearchCompletedCount` stays on every adapter because metering reads it in native mode.
- **MCP defer/search caveat:** OpenAI `defer_loading` + `tool_search` behavior is provider-specific and is not simulated for Claude when unavailable in Anthropic MCP mode.
- **Cross-cutting model-context rules** live in the root [architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — do not duplicate long prose here; link to it.
- **Call path:** Before `CallWithRetry` / `ClaudeProvider.Call`, attach a path on `context` (`telemetry.WithCallPath`, or `Agent.withCallPath` from agent entry points).
  New inference call sites must do this or metrics show `call_path=unknown` (see architecture doc).
- **OpenAI Responses streaming has four terminal events, not one.**
  `responsesNewStreaming` captures the final `Response` from both `response.completed` and `response.incomplete` (an incomplete response — e.g. reasoning models like Sol spending the output cap on reasoning, or a content filter — is a real, truncated response; the non-streaming path already surfaces `IncompleteDetails.Reason` through `GenerateResponse.StopReason`, so failing the turn was wrong).
  `response.failed` and `error` return descriptive errors carrying the provider's code/message/response-id.
  Only a stream that ends with `stream.Err()==nil` and none of those events is treated as a truncated/dropped stream.
  Capturing only `response.completed` was the root cause of the opaque "stream finished without response.completed event" (issue #132).
- **Only two of the three streaming paths need a mid-stream retry guard, and that asymmetry is deliberate.**
  Claude (`callClaudeWithRetry`) and OpenAI Responses (`callWithRetry`) wrap their calls in application-level retry loops, so both carry a "delta already emitted" flag and refuse to re-issue a call whose output the user has already seen.
  `streamChatCompletion` has no such loop — its only retries come from openai-go's `WithMaxRetries`, which decides from the response status and headers before any SSE body is read, and `ssestream` has no reconnect logic.
  Once a chunk is delivered nothing can re-issue, so no guard is needed.
  Pinned by `TestStreamChatCompletion_DoesNotRetryAfterDeltasDelivered` plus a control that proves a pre-body failure *is* retried, so an SDK bump breaking the invariant fails the suite rather than passing quietly.
  Verified against openai-go v3.29.0.
  Full reasoning is on `streamChatCompletion`.
- **Chat Completions streaming usage is last-chunk, not summed.**
  `openai.ChatCompletionAccumulator` sums `chunk.Usage` across every chunk (`cc.Usage.X += chunk.Usage.X`), which is correct for OpenAI (usage only in the final chunk) but wrong for Gemini's OpenAI-compat streaming, which repeats a full **cumulative** usage block on **every** SSE chunk — summing multiplies prompt tokens by the chunk count (an 80k prompt reported as ~300k once the reply is long).
  `streamChatCompletion` therefore captures the usage from the **last chunk that carries any** (`chunkCarriesUsage`) and overwrites `acc.ChatCompletion.Usage` with it before returning, so `chatCompletionTokenUsage` (telemetry + `GenerateResponse.InputTokens`) reports the true totals for every OpenAI-compatible provider (Gemini, Mistral, DeepSeek, Qwen, Xiaomi).
  Pinned by `TestStreamChatCompletion_DoesNotSumRepeatedUsage`.
- **Subagent call path:** Delegated `run_subagent` calls are labeled `call_path=subagent` so telemetry can separate orchestrator traffic from normal chat turns and scheduled jobs.

## Testing

- `mock_adapter_test.go` — echo/fixed/scripted modes, delta concatenation (unicode, no-space, newline-heavy), mid-stream cancellation.
  `deny_transport_test.go` — proves an `httptest` server is never reached, including through a real openai-go client built on the deny client.
- `model_context_test.go`, `model_context_hydrate_test.go`, `model_context_openai_test.go` — segment assembly, hydration helpers, OpenAI/Claude rendering (including Claude text fallback when image bytes absent).
- `model_context_breakdown_test.go` — `SegmentBreakdown` aggregation, first-appearance ordering, cacheable-any / image counting, and nil-receiver/nil-counter guards.
- `openai_test.go`, `openai_tools_test.go`, `claude_test.go`, `claude_adapter_test.go`, `claude_adapter_more_test.go` — output shape and tool compatibility (including Beta MCP adapter mode).
- `token_counter_test.go` — counting and carry-over selection.
- `inference_metrics_test.go` — nil-safe usage recording.
- `fileattachment_test.go`, `images_test.go` — helpers.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **Model context and providers**, **When to build `ResponseNewParams` by hand**.
