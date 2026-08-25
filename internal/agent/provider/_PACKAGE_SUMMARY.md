# Package: `internal/agent/provider`

## Role

Maps **`ModelContext`** (ordered prompt segments) to OpenAI Responses and Anthropic Messages APIs, plus shared tool JSON, adapters, and token counting.

## Responsibilities

- **`ModelContext` / `ModelContextSegment`:** Segment kinds (including dedicated mode prompt segment) and append/insert helpers (`InsertBeforeLastUserMessage` for optional developer lines immediately before the final user turn). **`StripUserMessageImages`** clears multimodal payloads when reusing a context for APIs that only accept text (e.g. expression picker + strict JSON schema on Responses API).
- **OpenAI:** `RenderOpenAIInputItems`, `ModelContext.BuildOpenAIResponseParams`, `OpenAIProvider`, `OpenAIAdapter`, `ProcessResponseOutput` (assistant text extraction / dedup), `BuildOpenAITools`, image generation helpers, file-attachment upload helpers. **`SegmentKindUserMessage` with `RoleDeveloper` and images** (expression continuity portrait) is rendered as **two** input items—developer text plus a **user** message carrying `input_image`, because the Responses API allows images only under the user role for message parts.
- **Claude:** `BuildClaudeParams`, `ClaudeProvider`, `ClaudeAdapter`, `ClaudeFunctionTool`, `ExtractClaudeText`, `ExtractClaudeBetaText`, `UnmarshalClaudeTextJSON`. **`renderClaudeContext`** splits **developer continuity + portrait** like OpenAI: text turn includes `ExpressionPortraitContinuityPointerNote`; the next user message carries caption + image blocks. **Prompt caching:** the last block of the leading **contiguous `Cacheable` segment prefix** gets Anthropic **`cache_control` `ephemeral`** with **5m TTL** (see `model_context_claude.go`).
- **Claude remote MCP:** `ClaudeAdapter` switches to Anthropic Beta Messages MCP client mode when MCP server config is supplied (`mcp_servers` + MCP toolsets).
- **z.ai GLM (Anthropic-compatible):** GLM models (`provider = "zai"`) reuse the **entire Claude path** via `NewClaudeProviderWithBaseURL` pointed at z.ai's Anthropic-compatible endpoint (`DefaultZAIBaseURL`). Routing uses `models.UsesAnthropicMessagesAPI` (true for anthropic + zai). Anthropic-native features (native web search, beta MCP) are gated off for z.ai via `models.IsNativeAnthropicModel`; prompt caching (`cache_control`) is left on (z.ai supports it).
- **Gemini (OpenAI Chat Completions compatible):** Gemini models (`provider = "google"`) use a **separate** `GeminiProvider` + `GeminiAdapter` + `BuildGeminiParams`/`renderGeminiMessages` built on the openai-go **Chat Completions** API (not the Responses API), pointed at `DefaultGeminiBaseURL`. **`BuildOpenAIChatCompletionParams`** / `renderGeminiMessages` are also shared by Mistral, DeepSeek, Qwen, and Xiaomi. The agent calls **`PrepareForTextOnlyChatCompletions`** only when **`models.ChatCompletionsSupportsVision`** is false (DeepSeek, MiMo; Qwen/Mistral matched by model-id heuristics until a DB vision flag exists). `renderGeminiMessages` maps developer-ish segments (`SegmentKindToolResult`, `SegmentKindAttachmentContext`, mood/developer context) to **user** messages; expression portrait and final user turns support multimodal `RawBytes` when vision is enabled. When a text-delta handler is attached, Gemini and the other Chat Completions providers stream via `streamChatCompletion`, which accumulates content, tool calls, and final usage; function tools use `OpenAIChatCompletionFunctionTool` / `GeminiFunctionTool`.
- **User-attached images (chat):** `ModelContextSegment.UserImages` + `AppendUserMessage`. OpenAI Responses gets `input_image` with the OpenAI **`file_id`** from upload. **Claude** uses **`UserMessageImage.RawBytes`** when set (see **`loadImageBytesForClaude`**); otherwise **`renderClaudeContext`** falls back to **text-only** for that turn. **`HydrateUserMessageImages`** can prefetch bytes from storage for reuse.
- **Unified iteration:** `AgentAdapter` + `ToolUse` / `ToolResult` for the multi-round agent loop.
- **`GenerateResponse`:** Normalized completion type from either provider.
- **`TokenCounter` + carry-over selection:** Token budget and `SelectCarryOverTurns` for history trimming.
- **Inference metrics:** `responsesNew` / `messagesNew` are the only SDK call sites for OpenAI Responses and Anthropic Messages; they call `recordProviderTokenUsage` (`tel.Metrics`, `call_path` from context). `ModelContext.EstimatedTokensBySegment` supports segment-level token estimates for telemetry. Providers take `*telemetry.Telemetry`; `OpenAIProvider.zapLog()` uses `tel.Logger` for attachment/image helpers (falls back to zap Nop when nil).
- **`GenerateSchema`:** JSON-schema helper for structured model outputs (also used from `internal/gate`).

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

- **Inbound:** `internal/agent` (message handling, loops), `internal/gate` (safety helpers use `OpenAIProvider` + schema).
- **Outbound:** `internal/datastore` (OpenAI provider for some ops), OpenAI and Anthropic SDKs, `go.uber.org/zap`.

## Non-obvious decisions

- **Claude vs OpenAI for user images:** OpenAI uses **`file_id`** from upload. Claude needs **`RawBytes`** on each `UserMessageImage`; the chat path fills them via **`loadImageBytesForClaude`** before **`buildModelContextForChatMessage`**. Without bytes, `renderClaudeContext` keeps the user message text-only for that attachment.
- **Claude rendering:** Developer/system vs user ordering is covered in tests (`RenderClaudeContext_DeveloperBeforeUser`).
- **Tool JSON:** `ClaudeFunctionTool` strips unsupported JSON-schema features so shared function specs work on Anthropic; provider tests use local schema fixtures to avoid importing the agent tool catalog.
- **Claude input-token totals include cached tokens:** Anthropic's `Usage.InputTokens` is the **uncached** portion only; the cached prompt prefix is reported separately in `CacheReadInputTokens` / `CacheCreationInputTokens`. `claudeTotalInputTokens` / `claudeBetaTotalInputTokens` sum all three so `GenerateResponse.InputTokens` and `recordProviderTokenUsage` match the true prompt size (OpenAI already reports a single combined `InputTokens`). Skipping this under-reports cached chats by the whole prefix (was ~3k constant) and starves the checkpoint token heuristics.
- **OpenAI tool continuations:** After an OpenAI Responses call, `OpenAIAdapter` sends only the newly produced function-call outputs with `previous_response_id`; re-sending the initial `ModelContext` would duplicate system/history/scratchpad content on every tool round. Instructions and the configured tool list remain on each request because the provider does not inherit them through the response chain.
- **Claude native web-search replay:** Intermediate text blocks are replayed without their provider-owned citations because Anthropic can return a citation with an empty `web_search_result_location.url`, then reject that same citation on the next request. The native `web_search_tool_result` block is still replayed unchanged, preserving its encrypted content and server-side search continuity.
- **MCP defer/search caveat:** OpenAI `defer_loading` + `tool_search` behavior is provider-specific and is not simulated for Claude when unavailable in Anthropic MCP mode.
- **Cross-cutting model-context rules** live in the root [architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — do not duplicate long prose here; link to it.
- **Call path:** Before `CallWithRetry` / `ClaudeProvider.Call`, attach a path on `context` (`telemetry.WithCallPath`, or `Agent.withCallPath` from agent entry points). New inference call sites must do this or metrics show `call_path=unknown` (see architecture doc).
- **Only two of the three streaming paths need a mid-stream retry guard, and that asymmetry is deliberate.** Claude (`callClaudeWithRetry`) and OpenAI Responses (`callWithRetry`) wrap their calls in application-level retry loops, so both carry a "delta already emitted" flag and refuse to re-issue a call whose output the user has already seen. `streamChatCompletion` has no such loop — its only retries come from openai-go's `WithMaxRetries`, which decides from the response status and headers before any SSE body is read, and `ssestream` has no reconnect logic. Once a chunk is delivered nothing can re-issue, so no guard is needed. Pinned by `TestStreamChatCompletion_DoesNotRetryAfterDeltasDelivered` plus a control that proves a pre-body failure *is* retried, so an SDK bump breaking the invariant fails the suite rather than passing quietly. Verified against openai-go v3.29.0. Full reasoning is on `streamChatCompletion`.
- **Subagent call path:** Delegated `run_subagent` calls are labeled `call_path=subagent` so telemetry can separate orchestrator traffic from normal chat turns and scheduled jobs.

## Testing

- `mock_adapter_test.go` — echo/fixed/scripted modes, delta concatenation (unicode, no-space, newline-heavy), mid-stream cancellation. `deny_transport_test.go` — proves an `httptest` server is never reached, including through a real openai-go client built on the deny client.
- `model_context_test.go`, `model_context_hydrate_test.go`, `model_context_openai_test.go` — segment assembly, hydration helpers, OpenAI/Claude rendering (including Claude text fallback when image bytes absent).
- `model_context_breakdown_test.go` — `SegmentBreakdown` aggregation, first-appearance ordering, cacheable-any / image counting, and nil-receiver/nil-counter guards.
- `openai_test.go`, `openai_tools_test.go`, `claude_test.go`, `claude_adapter_test.go`, `claude_adapter_more_test.go` — output shape and tool compatibility (including Beta MCP adapter mode).
- `token_counter_test.go` — counting and carry-over selection.
- `inference_metrics_test.go` — nil-safe usage recording.
- `fileattachment_test.go`, `images_test.go` — helpers.

## Related documentation

- [Architecture summary](../../../docs/ARCHITECTURE_SUMMARY.md) — **Model context and providers**, **When to build `ResponseNewParams` by hand**.
