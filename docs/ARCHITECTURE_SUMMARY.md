 # Architecture Summary

 ## Repository documentation

 Where documentation lives and how it fits together:

 | Location | Role |
 |----------|------|
 | **`docs/ARCHITECTURE_SUMMARY.md` (this file)** | **System** architecture: product purpose, layers, data flow, and cross-cutting decisions (e.g. agent/model-context rules, data layer). |
 | **`internal/<package>/_PACKAGE_SUMMARY.md`** | **Package** documentation: that directory’s responsibilities, main entry points, dependencies, non-obvious decisions, and testing notes. One file per Go package directory (same path as `package` in Go). |

 **Generated code:** `ent/` is described here at the architecture level only; do not duplicate Ent schema detail in package summaries unless a specific query pattern deserves a callout.

 **Excluded / special:** `testdata/` and asset-only embed trees may not have a `_PACKAGE_SUMMARY.md` if there is no Go `package` there. Small **`main`** packages under `internal/system_agents/cmd/...` each get their own summary next to `main.go` so binaries stay documented.

 ### `_PACKAGE_SUMMARY.md` template (each package)

 Use the same heading order everywhere so humans and tools can scan consistently:

 1. **Role** — one line: what this package is for.
 2. **Responsibilities** — short bullets of what it does (and what it deliberately does *not* do, if that matters).
 3. **Key types and entry points** — exported symbols or HTTP handlers worth reading first.
 4. **Dependencies** — **Inbound:** who calls this package. **Outbound:** main packages or areas it imports (datastore, models, SDKs, etc.).
 5. **Non-obvious decisions** — important rules from code comments or ADR-style notes; use `file.go:Symbol` pointers where helpful.
 6. **Testing** — which `*_test.go` files matter, what is mocked; known gaps. **When tests are renamed, split, or moved, update this section in the same PR** so file names and pointers stay accurate (same anti-drift idea as the rest of the summary).
 7. **Related** — links to the relevant sections in this architecture doc.

 Summaries should stay **short**; prefer pointers to code and tests over copying implementation detail that will rot.

 **Discoverability:** From the repo root, `find internal -name _PACKAGE_SUMMARY.md` lists every package summary (useful for humans and agents).

 **Naming:** The leading underscore keeps the filename **out of the middle** of an alphabetical listing next to `*.go` files; it typically sorts **first** (or with other `_`-prefixed files) in ASCII/IDE ordering.

 ## Documentation maintenance (anti-drift)

 **Expectation:** Any change under `internal/...` that affects behavior, public API, dependencies, or documented behavior should **update docs in the same PR** so the repo stays aligned as change velocity increases.

 For every change that touches a package:

 1. **`_PACKAGE_SUMMARY.md`** — Open that package’s `internal/<pkg>/_PACKAGE_SUMMARY.md` and update sections that are now wrong or incomplete (role, responsibilities, key symbols, dependencies, decisions, testing). If the change is tiny and doc-neutral, a quick skim is still enough to confirm nothing became false.
 2. **`docs/ARCHITECTURE_SUMMARY.md`** — Re-read sections that describe system boundaries or cross-cutting rules (Purpose, Module breakdown, Agent layer, Data layer, etc.). If the change affects those concerns—or would mislead a reader—update **this file** in the same PR.

 This is a **habit**, not a one-time cleanup: small doc edits alongside code prevent drift and keep agent/human review grounded in accurate structure. Agentic PR review can enforce consistency over time; authors should still proactively check both places before requesting review.

 **Testing docs:** Refactors that only touch tests (rename/move/split `*_test.go` files) should still update that package’s **`Testing`** section in `_PACKAGE_SUMMARY.md` when those notes name specific files—so the summary does not silently point at old paths.

 ## Go workflow (before push)

 After Go code changes, from the repository root, run:

 ```bash
 go fmt ./...
 go mod tidy
 ```

 This cuts down on CI noise from formatting drift and stale or missing `go.sum` / module metadata. Make it the last step before commit when you have touched Go files (same idea as updating docs when behavior changes).

 ## Purpose
 - **Personal AI assistant.** Authenticated users chat with an OpenAI-powered assistant for research, content creation, task management, etc.
 - **Rich chat experience.** Persistent conversations, semantic “memory,” reusable rituals, custom personalities, file attachments (30+ types), and background jobs for async AI work.
 - **Extendable & production ready.** Clean Go backend with Angular frontend, Postgres+pgvector, ships as a standard container so it runs on any container platform.

 ## High-Level System Architecture
 ### Frontend
- Angular SPA in `web/app/`.
- Talks to the REST API defined in `openapi.yaml`, handles auth, chat, memories, personalities, rituals, file uploads, and job tracking.
- **`openapi.yaml` is a frontend build input, not just documentation.** The e2e SDK's types are generated from it into `web/app/e2e/sdk/schema.d.ts`, and `frontend-pr-validation` fails when the committed file does not match what the spec would generate. So **any change to `openapi.yaml` — including a backend-only PR — must regenerate the SDK and commit the result**, or frontend CI goes red on a PR that touched no frontend code:

  ```bash
  cd web/app && npm run sdk:generate
  ```

  Run `npm install` first if `openapi-typescript` is missing; it arrived with the e2e restructure, so a `node_modules` from before then will not have it.
- Served by nginx (`web/app/nginx.conf`) with a deliberate split cache policy: `index.html` is `no-cache` and the content-hashed `*.js`/`*.css` are `immutable`. The entry point names the hashed bundles, so letting it be cached keeps browsers on a previous deploy's JavaScript — deploys and local rebuilds then appear to have no effect.

 ### Backend
 - Go API server rooted at `cmd/api-server/main.go` (HTTP server with graceful shutdown).
 - `internal/server/` configures the router, middleware (CORS/logging/auth), and route-to-handler wiring.
 - Clean architecture: HTTP handlers ➜ agent/business logic ➜ datastore/Ent ➜ PostgreSQL.
- OpenAI integrations for chat completions, tools (code interpreter, web search, image generation), and semantic memory; attachment retrieval uses internal pgvector-backed tools.

 ## Data Layer
 - PostgreSQL 16 + `pgvector`.
 - Schemas defined via Ent (`ent/schema`); generated ORM lives in `ent/`.
- Key entities: `User`, `Chat` (includes **`archived`**: default lists exclude archived threads; `GET /chat?archived=true` returns the archive only, `&source=openai|anthropic` narrows to imported threads; imported threads also carry **`source`**, **`import_hash`** for dedup, **`checkpoint_summary`**/**`last_checkpoint_at`** window pointer, and **`rehydration_state`** (`pending`/`processing`/`ready`/`failed`) driving lazy summarization), `ChatMessage` (includes JSON **`additional_context`** for per-turn context rehydration, e.g. prefetched memories; optional **`last_error_message`** on user rows when a `chat_message` job fails, cleared on successful delivery), `Memory`, `Embedding`, `Personality`, `Ritual`, `Model`, `FileAttachment`, `Job`, `ToolCall`, `UserPreference`, **`AuditLog`** (append-only operational trail: admin model changes, quota bucket consume/refill, account backup import, and user-facing memory/account portability).
- Integration/auth extensions include `MCPServer` (remote connectors) and `WebhookToken` (static bearer token metadata for webhook-authenticated routes).
 - `internal/datastore/` exposes provider interfaces (chat, memory, personality, etc.) that handlers and agents depend on.
 - `internal/database/` handles client setup, pgvector enablement, migrations, and seeding.
- `Model` uses soft delete (`deleted`): non-admin runtime queries should filter to active rows (`deleted=false`) unless a path explicitly needs deleted records (admin/audit use cases).

 ## Infra & Operations
 - Local development: Docker + `docker-compose` (root `Dockerfile`, `docker/` scripts), or the make-driven hybrid stack below.
 - Production: ships as a standard container (see the root `Dockerfile`); infrastructure-as-code for the maintainers' hosted deployment lives outside this repository and is not required to run the app.
 - CI/CD: GitHub Actions pipelines for backend and frontend (`.github/workflows/`).
 - Account-export delivery reuses the configured file store plus SES: `exports/{user_id}/…` holds short-lived ZIPs, and `EXPORT_FROM_EMAIL` selects the verified SES identity that mails the presigned link (unset → links are logged for local dev). Bucket lifecycle expiry for `exports/` is an operational requirement handled wherever the deployment's infrastructure is defined.

 ### LLM backends (vendor/mock/local) & hermetic local dev/CI (ADR 0x018)
- **`LLM_BACKEND`** selects assistant generation: `vendor` (default; real providers), `mock`, or `local`. **`LLM_BACKEND=mock`** routes all assistant generation through an in-process **`provider.MockAdapter`** (echo/fixed/scripted modes, simulated streaming) driven through the real `generateAssistantForMessage` → draft-buffer → `saveAgentResponse` pipeline; image rituals persist an embedded fixture PNG through the real save path. **`LLM_BACKEND=local`** routes assistant generation through **`provider.LocalProvider`/`LocalAdapter`** — a *real* HTTP round trip to a local OpenAI-compatible server (Ollama default; `LOCAL_LLM_BASE_URL`/`LOCAL_LLM_MODEL`) via the same pipeline. Under **both** non-vendor backends, every other LLM consumer has a **deliberate** mock behavior (`Agent.nonVendorLLM()`): memory enrichment and imported-memory mining no-op, chat naming uses a deterministic fake, checkpoint summarization / expression classification / auto-mood selection are intentionally skipped, thread rehydration uses a deterministic fake summary, file-chunk uploads are marked chunked without embeddings, and personality/expression-grid generation return a clear "disabled" error.
- **No provider egress is enforced at the transport**: under a non-vendor backend, `provider.DenyNetworkHTTPClient()` (a RoundTripper returning `ErrNetworkDenied`) is injected into every provider SDK client — the shared agent client, all per-provider constructors, the memory handler client, and the embedder the server hands to plugins (`plugins.Deps.CreateEmbedding`). The one deliberate exception: `LocalProvider` gets its own real HTTP client so `LLM_BACKEND=local` can reach the local server, while everything else stays egress-denied. All provider clients must be constructed through `agent.NewAgent`/handler constructors so a one-off `openai.NewClient(...)` never becomes an escape hatch. Scope is "no provider egress", not "no network" — Postgres still works.
- **Fail-closed gating:** non-vendor `LLM_BACKEND` values and `DESTRUCTIVE_MIGRATION` are honored only when `ENV` was **explicitly set** to `development`/`test`/`local` (`Config.IsExplicitLocalEnv`); the parsed value defaults to `development` when unset, which would be fail-open. `ENV` is canonical, `ENVIRONMENT` a legacy alias; conflicting values, `ENV=production LLM_BACKEND=mock/local`, an unrecognized `LLM_BACKEND`, `LLM_BACKEND=local` without `LOCAL_LLM_MODEL`, or a non-local explicit env all fail startup fatally (`cmd/api-server/main.go`). This gate is also what keeps hosted/production deployments structurally unable to point at a local model.
- **Make-driven hybrid stack:** `make check-env` → `make db-up` (Postgres+pgvector in Docker, healthcheck-polled) → `make run-mock` (native API, mock backend) or `make run-local` (real local model) → `make web` (Angular dev server). `make dev-up`/`dev-down` run the API as a background binary with a readiness poll and verified-PID teardown. `make local-superuser` (`cmd/create-superuser`) provisions admins interactively and is the **only** admin-provisioning path — the former `SUPERADMIN_*` boot provisioning was removed, so no environment variable can mint an admin in a running deployment; CI test users come from the public register endpoint.
- **Coverage:** `make test-ci` also writes `coverage/go-unit.out` (`-covermode=atomic`, required with `-race`; `-coverpkg=./cmd/...,./internal/...,./ent/schema/...` so a package is credited for lines exercised by *any* test, not just its own). `pr-validation.yml` uploads it to Codecov under flag `go-unit` using tokenless OIDC, and validates `codecov.yml` against Codecov's API. Both the PR and `main`-push paths upload, so PR comparisons have a base. Several more flags (e2e, mock-e2e, API, frontend) merge into one combined number. Codecov statuses are **informational**; they gate nothing today.
- **Hermetic CI:** `make test-ci` (mock backend, dummy keys, race detector) is the required PR gate; a `local-model-smoke` CI job (opt-in via the `run-local-model` label, `continue-on-error: true`) pulls a tiny Ollama model and runs `make test-ci-local` (`scripts/local-e2e.sh`, real inference, non-echo assertion); real-provider tests sit behind `//go:build integration` in an opt-in, secret-gated job; `scripts/check-no-local-models.sh` enforces the no-*embedded*-local-model-runtime constraint (external local servers are the sanctioned `local` backend); `scripts/mock-e2e.sh` is an isolated end-to-end run (own compose project, `env -i` allowlist, ephemeral per-run secrets, real register/login) asserting the mock echo reply, job success, persistence, and a genuine PNG from the image ritual — run locally/on demand via `make mock-e2e`, and as its own `mock-e2e` job in `pr-validation.yml` on every backend PR (parallel to, not a step in, the main `validate` job, so it doesn't lengthen that job's critical path); `MOCK_E2E_COVERAGE=1` in CI builds the API with `-cover` and uploads flag `go-mock-e2e` to Codecov (see the Coverage section of `README.md`); the browserless Playwright API-test suite (`web/app/e2e/api-tests/`, its own `api` job in `e2e-mock.yml`, `npm run e2e:api`) drives a separate `-cover` API instance and uploads flag `go-api`.

 ## Module Breakdown
 ### HTTP Layer
- `internal/handlers/`: organized per API resource (`chat`, `memory`, `accountexport`, `personality`, `ritual`, `model`, `job`, `fileattachment`, `user`, etc.). There is no admin HTTP surface: `cmd/create-superuser` is the operator path, and an operator who wants admin routes can add them as a plugin (`internal/plugins`). The datastore's admin methods — including account backup/restore for bulk migrations — stay here and are what such a plugin would build on.
- Webhook surfaces are split by trust model: authenticated user token management (`/api/webhook-tokens`) and static-token callback routes (`/api/webhooks/...`) handled by `internal/handlers/webhook/`.
 - Each handler injects datastore, agent, logger; `handlerutils/` holds reusable HTTP helpers.
- The agent depends only on **`internal/metering.Meter`** to gate and record turns. A production implementation may be linked privately (via a blank import in `cmd/api-server`) to enforce usage limits and record usage; builds without it fall back to **`metering.NoopMeter`**, which allows every turn and records nothing.
- `internal/middleware/` handles CORS, logging, panic recovery, and auth middleware (JWT — plus an optional external-auth provider — for app routes, plus static webhook bearer token validation for webhook routes); `internal/auth/` supplies JWT utilities, password hashing, invite codes.

 ### Agent Layer
 - `internal/agent/`: builds conversation context (history, memories, rituals, attachments), scores safety (risk, contradictions, beliefs), orchestrates tool calls, and persists results.
 - `static_beliefs.go` provides reusable What Iff-based belief templates.
 - Tool helpers, prompts, agent loops, memory extraction, and safety evaluations live inside this package.
 - Background **`jobs`** for **`chat_message`** and **`agent_job_run`** advance **`inference_complete` → `expression_complete` → `compaction_complete` → `complete`** so clients can render the assistant reply after inference while expression picking and checkpoint/post-processing finish (`job_phase.go`, `expression_generation.go`, `handleUserMessage`, `HandleAgentJobPrompt`).
- During streamed chat inference, deltas are held on `Job.draft_deltas`. Both cancellation and an ordinary post-stream failure atomically promote non-empty drafts to an assistant message before clearing them; failed jobs also retain the error on the triggering user message for the UI banner.

#### Conversation import & lazy rehydration (ChatGPT/Claude migration)
- **Import:** `POST /chat/import` (`internal/handlers/chat`) ingests an **OpenAI** or **Anthropic** `conversations.json` export (format auto-detected). The Angular client accepts the export `.zip` directly; it merges OpenAI’s thread-complete `conversations-NNN.json` shards client-side, then splits uploads larger than ~60 MB at conversation boundaries. The upload is staged to a temp file and parsed + persisted as **archived** threads in a detached background job (`JobTypeChatImport`), with `models.ImportProgress` persisted on `Job.progress` for client polling. The client uses an explicit, cancellation-guarded polling loop that returns only on a terminal job status (never the initial `processing` response); job reads are `Cache-Control: no-store`, and count-less parsing-phase payloads must never overwrite a last valid imported/skipped count. OpenAI parsing uses `internal/chatimport`; Anthropic parsing is in-package. Dedup by `sha256(conversationID)` → `import_hash`.
- **Lazy rehydration:** imported threads have no native checkpoint/summary, so they are summarized **on demand** when the user restores (unarchives) one. `PatchChat` enqueues `JobTypeThreadRehydration` (`internal/agent/thread_rehydration.go`), which keeps the last `rehydrationKeepTurns` (5) user turns live and summarizes everything before that — single-pass, or **map-reduce** chunked for very long (100+ turn) threads — via the fixed `archivalOpenAIModel`. It persists the checkpoint summary + window pointer (`last_checkpoint_at`) and flips `rehydration_state` to `ready`. **`WaitForThreadRehydration`** is the inference gate: `handleUserMessage`/`handleEphemeralPrompt` stall a turn (bounded, graceful degrade) until an in-flight summary settles, so a restored thread runs against the summary + recent window rather than its full history.
- **Memory seeding:** after a thread is `ready`, the rehydration job also mines up to ~20 durable long-term **memories** from the full transcript (`extractAndStoreImportedMemories`), reusing the live memory-extraction prompt/schema **minus the scratchpad delta**. Combined with the frontend's post-import picker (select up to 5 threads to rehydrate immediately), a new user leaves import with a few resumable threads **and** a seeded memory base — closing the "empty long-term memory on day one" migration gap.

#### User account portability (ADR 0x019, experimental)
- **Export:** `POST /account/export` creates an `account_export` job and builds an in-memory, id-stripped ZIP in a bounded background goroutine (`internal/exporter`). It contains round-trippable Anthropic-shape `conversations.json`, personality prompts/scratchpads, the existing memory ZIP, a file-object inventory, and a manifest; raw file bytes are deliberately excluded. The ZIP is stored at `exports/{user_id}/…`, then a 24-hour presigned link is sent only through email. The API and job progress never expose that URL.
- **Import:** `POST /account/import` stages a versioned export ZIP and returns an `account_import` Job (`202`); `GET /account/import/{id}` supplies phase/count/result progress while a bounded detached in-process worker restores it. Imports remain additive: chats dedupe by import hash, personalities by normalized name, and memory embeddings are regenerated when the embedding provider is available. Whatiff-only fields in the otherwise Anthropic-compatible `conversations.json` preserve checkpoint summary/window metadata, tool/tag/favorite state, and source personality IDs. Import resolves source personalities first, then chats, then rewrites chat and pinned-personality memory references to fresh destination IDs. Because memory primary keys are global, each source memory ID is deterministically rekeyed by target user before import, so a ZIP is repeatable in one account and importable into another. Memory imports use bounded embedding batches and bounded transactional bulk writes; completed batches stay durable if a later batch fails. Invalid memory counts are broken down by parse/required-field cause without recording memory text. A thread with an exported summary is restored active with `rehydration_state=ready`; a summary-less thread stays archived so the lazy-rehydration path remains authoritative. Account export/import actions are audit logged.
- **Local verification:** when `S3_FILE_BUCKET` and `EXPORT_FROM_EMAIL` are unset, the same flow uses `localFileStore` (`LOCAL_FILE_STORE_DIR`) and `email.NoopSender`; the logged `file://` URL points to the locally written ZIP. `cmd/clear-account` supports fast local export/import cycles and is dry-run by default.
- **Frontend:** the authenticated, deliberately unlinked `/experimental` route temporarily houses these pre-release controls behind an explicit breakage warning; it has no navigation entry point and is exempt from personality setup so a zero-personality user can restore an account.

 #### Model context and providers
- **`provider.ModelContext`** (`internal/agent/provider/model_context.go`) holds an ordered list of **segments** (system prompt, checkpoint summary, scratchpad, history turns, memories, attachment labels, mode prompt context, developer context, final user message). Provider-specific code maps segments into SDK types.
 - **`RenderOpenAIInputItems`** / **`BuildOpenAIResponseParams`** / **`BuildClaudeParams`** turn a `ModelContext` into OpenAI Responses params, input items, or Anthropic `MessageNewParams`. OpenAI tool continuations use `previous_response_id` plus only the newly generated function outputs—the initial ModelContext must not be replayed on every round—while request instructions and tool definitions remain configured. Cacheable segments are documented as a **contiguous prefix** (convention for prompt caching, especially on Claude). **`renderClaudeContext`** sets Anthropic **`cache_control` `ephemeral`** (5m TTL) on the **last content block** of that prefix so memories, mood, expression continuity, and the current user turn stay outside the cached prefix.
- **Remote MCP parity:** OpenAI uses Responses `mcp` tools; Claude uses Anthropic Beta Messages MCP client mode (`mcp_servers` + MCP toolsets) when chat MCP servers are configured. The same chat/ritual MCP server selection is reused for both providers.
 - **`messageContextBuilder`** (`internal/agent/message_context_builder.go`) loads history, carry-over after checkpoints, token-budget selection, **merged additional context** (typed snippets persisted on `ChatMessage` rows—e.g. prefetched memories with type `MEMORY`—plus this turn’s `prepareChatContext` memories, deduped), **active-mood snippet** (`SegmentKindMood`), **prior-turn expression continuity**, attachment labels when tools are enabled, and the final user turn. Construction requires **non-nil `telemetry` with a non-nil `Logger`**. **`Agent.buildModelContextForChatMessage`** centralizes **`prepareUserMessage`** + **`messageContextBuilder.build`** for chat turns (jobs, rituals, `handleUserMessage`) so user-segment handling stays consistent. **`Agent.openAIResponseParamsForChat`** wires tools and personality-file enrichment, then calls **`ModelContext.BuildOpenAIResponseParams`** for full chat turns.
 - **Image attachments (vision):** User image bytes for Claude are loaded in **`loadImageBytesForClaude`** (S3 keys and `FileContent` fallback where applicable) and sent as multimodal blocks; OpenAI uses uploaded **`file_id`** on the final user message. **`InsertBeforeLastUserMessage`** remains for inserting optional **developer** segments immediately before that final user turn when needed.
- **Mode semantics:** `chat.active_mode_id` stores the effective mode used for generation; `chat.is_auto_mode` stores policy (`true` = Auto, mode tools enabled). `change_mode` requires an explicit `mode_id` and flips policy to manual; stale/invalid active modes are reconciled server-side, and Auto policy re-runs selection when mode is missing.
- **Mode images:** API payloads still use `image_ids`, and handler validation enforces singleton semantics (0 or 1 image per mode). Historically, mode/mood save paths generated and persisted thumbnails from the attached image, but that generation path is currently disabled/deprecated (save should not depend on thumbnail/image-byte availability).
- **Attachment key stability:** `file_attachment.s3_key` is canonical and immutable for object retrieval; rename updates only display name, and reference-image records retain source key/file-id for OpenAI + Claude compatibility.
- **Tool catalog:** `internal/agent/tools` owns the provider-neutral static function-tool catalog and OpenAI projection helpers. `internal/agent` computes a shared per-turn tool policy (disabled tools, mood tools, file availability, MCP ritual IDs) and projects it into OpenAI tools or Claude function tools via `provider.ClaudeFunctionTool`; provider packages keep SDK-specific request/response formatting only.
 - **`internal/agent/provider/`** also contains OpenAI/Claude adapters, provider-specific tool composition (`BuildOpenAITools`, `ClaudeFunctionTool`), and token counting. **Telemetry:** `ModelContext.EstimatedTokensBySegment` drives per-segment **estimate** histograms (best-effort cl100k; **`Agent`** reuses a single **`TokenCounter`** for these estimates). The Context X-ray uses actual vendor input usage as its total when supplied, attributes serialized function schemas as `tool_definitions`, folds each persisted current-turn tool call's input/output/error into `tool_result`, and puts provider framing, image input, and any unattributable difference into `vendor_prompt_other`; UI presentation folds expression portraits into Developer notes. OpenAI/Claude providers take **`telemetry.Telemetry`**, record **actual** input/output usage once per successful HTTP call via internal `responsesNew` / `messagesNew`, and use **`tel.Logger`** for attachment/image helpers. **Call path (required for labeled metrics):** any code that invokes **`OpenAIProvider`** or **`ClaudeProvider`** must attach a path on **`context`** first — use **`Agent.withCallPath`** at **Agent** entry points (`handleUserMessage`, `HandleAgentJobPrompt`, etc.), and **`telemetry.WithCallPath`** directly in packages that call providers without an **`Agent`** (e.g. **`gate`**, **`agentjobs/schedule`**, scratchpad/memory helpers). Omitting this yields `call_path=unknown` on usage series.
- **Claude MCP defer/discovery caveat:** OpenAI-only `defer_loading` + `tool_search` behavior is not emulated for Claude when unavailable natively; Claude MCP requests include server definitions/toolsets only.
- **Discovery / retrieval tools:** Unified **`list`** (`internal/agent/tools/list.go`) enumerates resources by `kind` (models, personalities, skills, files, conversations, jobs, mcp_servers) with shared filters/`limit`/`page`; its conversation discovery spans archived imports as well as active threads, while `find_context` reads the selected thread under the normal ownership check without rehydrating it. Unified **`find_context`** retrieves context (investigate / fetch / conversation / origin; fetch of images attaches vision payloads via the tool-result image path). Conversation retrieval walks threads oldest-to-newest using a conversation-scoped `(sent_at, message_id)` keyset cursor, preserving stable import order without offset drift. **`run_subagent`** executes a synchronous provider call with isolated minimal context (base+personality system prompt, personality scratchpad, provided user message) and intentionally skips history/checkpoint/memory segments and post-turn maintenance side effects.

 #### When to build `ResponseNewParams` by hand
 - **Full chat with DB history and user tools:** build context with **`Agent.buildModelContextForChatMessage`** (or **`messageContextBuilder.build`** when the user text is already prepared), then **`ModelContext.BuildOpenAIResponseParams`** via **`openAIResponseParamsForChat`** (same `ModelContext` for instructions + input). Do not duplicate model/instructions/tool wiring for these paths.
- **Acceptable manual `ResponseNewParams`:** short, **no-conversation-history** jobs with a **fixed or narrow purpose** — e.g. **`chatname.go`**, **`generate_personality.go`**, **`gate/safety_helpers.go`**, scratchpad **summarize/update** branches, **`conversation_summary.go`** / **`memory.go`** maintenance prompts, **`agentjobs/schedule/parse.go`**, delegated `run_subagent` execution (`subagent_tools.go`), and similar. These typically use a **hard-coded or task-specific model** and a tiny input list. Checkpoint scratchpad update and memory extraction use fixed small models (`archival_models.go`: `gpt-5-mini` for OpenAI, `claude-haiku-4-5` for Claude). **Checkpoint conversation summary always uses `gpt-5-mini`** for both providers (OpenAI via `PreviousResponseID`, Claude via explicit `ModelContext` input items). Persona **`archival_model`** and custom **memory** prompts are deprecated and ignored; optional **`scratchpad_update_prompt`** on the personality is still applied for scratchpad updates.
 - **Hybrid:** flows that first build a **`ModelContext`** (e.g. **`image_ritual.go`**) then wrap rendered items in a small, task-specific `ResponseNewParams` (extra developer instruction, no tools) are fine; the **conversation content** still comes from the builder + renderer, not from hand-rolled history.

 ### Models and Shared Types
 - `internal/models/`: DTOs shared across handlers, agent, store.
 - Keeps HTTP payload schemas, Ent entities, and OpenAI request/response shapes in sync.

 ### Data Access & Ent
 - `internal/datastore/`: repository layer abstracting Ent queries with ownership, pagination, filters, and vector search.
 - Entities mirror the schema files and are augmented with provider interfaces for testing.
- Data access utilities include chat/message/memory/personality/ritual/file attachment helpers. User account portability projects selected user data into an id-stripped archive suitable for additive cross-account import; it intentionally inventories, rather than transfers, file bytes. Admin account backup restore remains a separate clone-oriented path: it imports a versioned ZIP of user-owned chats, personalities, memories, moods, and agent jobs into an existing target user, preserves source UUIDs for idempotency, clears provider response IDs, and rebuilds memory embeddings. Both restore paths are intentionally section-by-section rather than one large transaction, so partial imports can be retried.
