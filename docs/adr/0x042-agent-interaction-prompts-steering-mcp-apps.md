# ADR 0x042: Agent questions, mid-run follow-ups, MCP elicitation and MCP Apps

- **Status:** Proposed
- **Date:** 2026-11-07
- **Deciders:** What Iff maintainers
- **Issue:** none (exploratory feature work requested in conversation)
- **PR:** see the pull request that adds this file

## Context

The Advanced (ReAct) loop (ADR 0x026) lets an agent work for many steps, but the person could only watch it.
Four things were missing:

- **Questions and approvals.** The agent had no way to ask the person something mid-run ("which format?", "can I delete these?"). It either guessed or ended the turn to ask.
- **Follow-ups.** A message sent while a turn ran was held by the composer; the only way to add information was to stop the run and start over.
- **MCP elicitation.** MCP servers can ask the user for input while a tool runs (`elicitation/create`, spec 2025-06-18 and 2025-11-25). Our client advertised no capabilities, spoke `2024-11-05`, and read a whole streamed reply before parsing it, so a server that asked would deadlock.
- **MCP Apps.** The MCP Apps extension (SEP-1865, `io.modelcontextprotocol/ui`, stable 2026-01-26) lets a tool name a `ui://` HTML view that the host renders. We dropped every tool's `_meta`.

Two constraints shape everything below:

- **No push channel.** The web client follows a turn by polling `GET /job/{id}` every second (ADR 0x039). Anything live has to ride on the job.
- **Several API instances.** The answer to a question can arrive on a different instance from the worker that asked it.

## Decision

### 1. Agent prompts: one mechanism for "the turn waits on the person"

A running turn that needs the person writes an **`agent_prompt` row** (job, chat, source, payload, status, response, expiry).
It then **blocks**, re-reading the row every second, and is woken at once when the answer lands on the same instance (`promptWaiters`).
The pending prompt is published on the job's progress (`ChatTurnProgress.prompt`), which the client already polls, so no new channel is needed.
`POST /chat/{chatId}/prompts/{promptId}/answer` validates the answer against the prompt (only offered options, form values typed to the schema, bounded text) and stores it with a conditional update.
That way two tabs cannot both answer, and an answer after expiry gets a 409.

- **Who can be asked:** only an interactive web turn (a `chat_message` job outside a relay thread; `interactivePromptJob`).
  Scheduled runs, webhooks, relays and sub-agents have nobody to ask: `ask_user` is not offered there, and MCP elicitations are cancelled.
- **Timeout (Gori's call):** a prompt waits `AGENT_PROMPT_TIMEOUT` (default 10 min, max 1 h).
  An unanswered `ask_user` tells the model to stop and ask in its reply. The tool round ends there, and an Advanced run ends with the new end reason **`needs_input`**, which is unfinished, so its open plan carries into the next turn.
  The person answers later as a normal message, and no worker is held indefinitely.
- **Stop** cancels the turn's context, which cancels the pending prompt.
- **Time spent waiting** does not count against the Advanced run's duration limit (`chatContext.promptWaited`).
  The turn's job keeps its heartbeat, so the gate's two-minute staleness rule never fires while it waits.

### 2. `ask_user`

A turn-only tool (offered next to `update_plan` through `turnOnlyToolSpecs`), shaped like Claude Code's question tool:

- either 1–4 questions, each with 2–4 options (single or multi-select) or none (free text); the person can always write their own answer;
- or an `approval` (`action`, `details`), answered Approve / Deny with an optional note.

The result the model reads is JSON: the answers, `approved` with the note, `declined`, or `no_answer` with the instruction to stop.

**Approval gating is agent-asked only for now (Gori's call).**
Enforced gating, where the app asks before certain tools run (MCP tools by annotation, built-in side-effect tools such as Discord/Slack posts and agent jobs), is the stretch goal and is not built here.
It would reuse this mechanism with a third `source`.

### 3. Follow-ups while a turn runs (steering)

A message sent while the person's own turn is generating is saved like any message, and its job is created with **`steer = true`**.
The job waits in the per-chat turn gate as usual.
Before each model call, the running turn **claims** queued steer jobs in its chat (`ClaimSteerMessages`).
A claim is a conditional update that moves the job from pending/processing to cancelled with error code `delivered_to_running_turn`.
The claimed job's worker sees the cancel (`watchChatJobCancel`) and exits without running a turn.

- **Where the text goes:** mid-step, on the last of the round's tool results.
  Every provider accepts that, whereas a user message between tool results and the next call is refused by some (Mistral, for one).
  When an Advanced step ends with an answer, the follow-up becomes a continuation and the run goes on, even if its plan was done.
  The text is framed as coming from the user, not from a tool.
  It is also kept in `chatContext.steerNotes` so a compacted run's work log still has it.
- **Nothing is lost:** a message the running turn never reaches (a Standard turn's final answer, a limit, the run ending first) is still a queued turn and runs next. The claim is conditional, so a message is either taken in or answered by its own turn, never both.
- **Client:** while the person's own turn generates, Enter and Send still work, but only for text. Attachments and slash commands wait, because the running turn cannot take files.
  The message is posted with `steer: true` and the running turn stays the one followed.
  A scheduled job's run is not steerable.

### 4. MCP elicitation, and protocol 2025-11-25

- `initialize` asks for **`2025-11-25`** and advertises `elicitation: {form, url}` and the MCP Apps extension.
  The version the server picks is stored with the turn's session and sent back as `MCP-Protocol-Version`.
- A streamed reply is now **read event by event**.
  Server requests on the stream are answered with a POST of their own: `elicitation/create` goes to the handler on the context, `ping` gets an empty result, and anything else gets -32601.
  Notifications are skipped as before.
- The handler (`mcpElicitor`) shows the request as a prompt with `source = mcp_elicitation` and the connector's name.
  A form is generated from the requested schema, validated on both ends.
  URL mode shows the full link and host, and is only accepted after the person opens it.
  Up to 5 elicitations are allowed per tool call and 10 per turn; beyond that, or for anything the card cannot show, the request is cancelled or declined without asking.
- A `-32042` URLElicitationRequiredError asks for each listed URL elicitation and, if all are accepted, retries the call once.
- **`tools/call` loses its 15 s overall timeout.**
  It runs on a client without one, bounded by 60 s of silence (the idle timer is paused while the person answers) and by the turn's context.
  A slow tool that streams progress no longer fails at 15 s.

### 5. MCP Apps

- **Discovery** keeps each tool's `_meta.ui` (`resourceUri`, and the deprecated flat `ui/resourceUri`) and `visibility`.
  Tools whose visibility leaves out `model` are never offered to the model and are refused if it guesses their name.
- **After a call**, a tool with a view saves `ToolCall.app` with the connector id, the `ui://` URI, the tool's own name and its raw `CallToolResult` (capped at 256 KB).
- **`GET /chat/{chatId}/tool-calls/{toolCallId}/app`** reads the view (`resources/read`, HTML only, at most 2 MB) and returns it with a **Content-Security-Policy meta** inserted after its doctype, plus the tool's input and result.
  The policy is built server-side from the view's declared domains, keeping only `https`/`wss` origins (with an optional `*.` wildcard).
  When nothing is declared, the view gets no network at all.
- **`POST …/app/tools/call`** proxies the view's `tools/call` to its own connector, only for tools visible to apps.
- **Rendering (Gori's call): `srcdoc` iframe with `sandbox="allow-scripts allow-forms"` and no `allow-same-origin`.**
  The view gets an opaque origin, with no access to the app's DOM, storage or cookies, and talks to the chat only through `postMessage`.
  The spec's sandbox-proxy page on a separate origin is the hardened follow-up.
  Views that need `localStorage` will not work until then.
- **The host bridge** (`McpAppBridge`) implements:
  - `ui/initialize` (theme, inline display, max height, locale);
  - after `ui/notifications/initialized`: `tool-input` then `tool-result`;
  - `tools/call`;
  - `ui/open-link` (http/https only, after a confirm);
  - `ui/message`, which goes **into the composer for the person to send**, never straight to the agent;
  - `size-changed`, logging and `ping`.
  `ui/update-model-context` is accepted and ignored. Only `inline` display is offered.
- Views render under the saved reply and load when they scroll near the screen.

### 6. Mock LLM directives (development)

In mock mode (`LLM_BACKEND=mock`, echo), lines like `/mock-tool <name> <json>` and `/mock-wait <duration>` in a message make the mock call those tools round by round.
It then answers with the rest of the message and a summary of the results.
Mock turns now also offer the turn-only tools and the chat's MCP tools (a chat without connectors discovers nothing, so mock e2e stays hermetic).
`cmd/mcp-demo-server` is a small unauthenticated MCP server with an elicitation form, a URL elicitation and an MCP App counter, for trying all of this locally.

## Alternatives considered

- **End the turn to ask (no blocking).** It is durable and holds no worker, but mid-run approvals become impossible, and MCP elicitation must be answered inside the tool call anyway. The timeout keeps the blocking bounded.
- **Store answers on the job row.** A JSON column on `job` would save a table, but prompts need a conditional answer, an expiry and a history across a turn's several prompts; a row each is simpler to reason about.
- **Steering as a separate endpoint and table.** A queue of notes on the running job would need its own fallback for notes the turn never reaches. Reusing the message and turn-gate queue makes that fallback free, and the follow-up shows in the thread as the message it is.
- **A real user message for steering.** Appending a user turn after tool results is cleaner for Anthropic and OpenAI, but some Chat Completions providers refuse `tool → user`. A per-adapter `AppendUserNote` can replace the tool-result fold later.
- **Render MCP Apps on a separate sandbox origin now.** It is the spec's recommendation and the right end state, but it needs a second origin with its own deploy (overlay infra). The opaque-origin sandbox is safe for the host and good enough to start.

## Consequences

- New table `agent_prompts`; new columns `jobs.steer` and `tool_calls.app` (auto-migrated).
- An interactive turn can now hold a worker for up to `AGENT_PROMPT_TIMEOUT` per question.
- Every MCP connector is now initialized as `2025-11-25` with capabilities. A server that only supports an older version answers with it and is handled as before.
- MCP tool calls can run longer than 15 s.
- The thread history shows `ask_user` as an ordinary tool call (its input and the answer JSON).

## Not done yet

- **Enforced tool approvals** (§2), the stretch goal.
- **Live MCP Apps.** Views render on the saved reply only, not while the turn is still running (live timeline rows have no saved tool-call id).
- **MCP Apps:** the separate-origin sandbox proxy; `ui/update-model-context` into the next turn; fullscreen/pip; `resources/read` from the view; the `permissions` (camera, microphone…) allow list; the view in Advanced runs' collapsed notes.
- **Elicitation** in sub-agents and agent jobs (cancelled today), and `notifications/elicitation/complete`, which is ignored; the person confirms instead.
- **Steering** for Standard turns that end without another tool round. The message runs as the next turn, which is correct but slower.
- The account backup does not carry `tool_calls.app`.
- OpenAPI covers the new endpoints. The overlay's metering does not need to change, since waiting makes no model calls.
