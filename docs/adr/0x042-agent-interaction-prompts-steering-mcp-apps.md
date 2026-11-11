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

- **Who can be asked:** only an interactive web turn (a `chat_message` job; `interactivePromptJob`).
  Scheduled runs, webhooks, relays and sub-agents have nobody to ask: `ask_user` is not offered there, and MCP elicitations are cancelled.
  In a relay thread, a turn sent from the app (turn source `app`) is interactive like any other; only the turns the relay starts are not.
- **Questions or approval, not both.** The two halves of `ask_user` are optional, but OpenAI's strict function calling (the Responses API default) treats every property as required, so the model fills the unused half with empties.
  Only a half with content counts: an approval with a blank action, or questions with no text, are ignored.
- **Timeout (Gori's call):** a prompt waits `AGENT_PROMPT_TIMEOUT` (default 10 min, max 1 h).
  An unanswered `ask_user` tells the model to stop and ask in its reply. The tool round ends there, and an Advanced run ends with the new end reason **`needs_input`**, which is unfinished, so its open plan carries into the next turn.
  The person answers later as a normal message, and no worker is held indefinitely.
- **Stop** cancels the turn's context, which cancels the pending prompt.
- **Time spent waiting** does not count against the Advanced run's duration limit (`chatContext.promptWaited`).
  The turn's job keeps its heartbeat, so the gate's two-minute staleness rule never fires while it waits.

### 2. `ask_user`

A turn-only tool (offered next to `update_plan` through `turnOnlyToolSpecs`), shaped like Claude Code's question tool:

- either 1–4 questions, each with 2–4 options (single or multi-select) or none (free text); the person can always write their own answer;
  a question can be marked `optional` (e.g. "Anything else I should know?"), and a prompt whose questions are all optional may be sent with no answers;
- or an `approval` (`action`, `details`), answered Approve / Deny with an optional note.

The result the model reads is JSON: the answers, `approved` with the note, `declined`, or `no_answer` with the instruction to stop.

**The card** docks above the composer, at the composer's width, and is built like the question tools in Claude Code and Cursor:

- **One question at a time.** With more than one, compact tabs carry each question's `header` (a tick once answered) above "Question 2 of 4"; Back and Next move between them and the tabs jump.
  The send button appears on the last question, or as soon as everything is answered, and stays disabled (with "Still to answer: …") until every required question has an answer.
- **Answers are stacked rows** with a radio or checkbox, the option's description underneath, and the agent's " (Recommended)" shown as a tag.
  Clicking a single-select question's first answer moves on to the next question; arrow keys don't.
  Enter moves on, and sends from the last question.
- **Text boxes only when asked for.** "Other…" is the last row of every question with options and opens its box when picked.
  An optional open question offers "Nothing to add" / "Add something…" and only shows a box for the second; a required open question has nothing to pick, so its box is there from the start.
  An approval's note is behind "Add a note for the agent".
- The header's ✕ dismisses without answering: `decline` for the agent's questions, `cancel` for an MCP elicitation, whose own footer keeps an explicit Decline.
- Actions use the compact `xs` button size; the radio is the shared `ui-radio`, and the tabs are `ui-tabs` at `size="sm"`.

**Approval gating is agent-asked only for now (Gori's call).**
Enforced gating, where the app asks before certain tools run (MCP tools by annotation, built-in side-effect tools such as Discord/Slack posts and agent jobs), is the stretch goal and is not built here.
It would reuse this mechanism with a third `source`.

### 3. Follow-ups while a turn runs (steering)

A message sent while the person's own turn is generating is saved like any message, and its job is created with **`steer = true`**.
The job waits in the per-chat turn gate as usual.
Before each model call, the running turn **claims** queued steer jobs in its chat (`ClaimSteerMessages`).
A claim is a conditional update that moves the job from pending/processing to cancelled with error code `delivered_to_running_turn`.
The claimed job's worker sees the cancel (`watchChatJobCancel`) and exits without running a turn.

- **Where the text goes:** a **user turn of its own**, right after the round's tool results (`provider.UserNoteAppender`, implemented by every built-in adapter).
  The person's words stay theirs: not a system message, which would promote their text above the conversation, and not text inside a tool's output.
  A user message after tool results is already what the Chat Completions adapters send in `ForceFinalResponse`, and Anthropic merges it with the tool-result turn.
  An adapter without the hook gets the old fallback: the note is folded into the last tool result, framed as coming from the user.
  When an Advanced step ends with an answer, the follow-up becomes a continuation and the run goes on, even if its plan was done.
  It is also kept in `chatContext.steerNotes` so a compacted run's work log still has it.
- **Tagged as mid-run:** the turn starts with `[Sent mid-run, while you were still working on my request]`, so the model reads it as the user adding to (or changing) the request it is working on, not as a new request after a finished one.
- **A Standard turn's final answer:** when the model is already writing its final answer with no further tool call, the follow-up isn't forced in. The turn is functionally done, so the message runs as the next turn (decided with Gori).
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
  - `ui/initialize` (theme, inline display, max height, locale, and the chat's theme as the spec's `styles.variables`, resolved from the live theme tokens);
  - `ui/notifications/host-context-changed` with the new theme and variables when the chat's theme changes;
  - after `ui/notifications/initialized`: `tool-input` then `tool-result`;
  - `tools/call`;
  - `ui/open-link` (http/https only, after the person confirms in the app's dialog, which names the host);
  - `ui/message`, which goes **into the composer for the person to send**, never straight to the agent;
  - `size-changed`, logging and `ping`.
  `ui/update-model-context` is accepted and ignored. Only `inline` display is offered.
- Views render under the saved reply and load when they scroll near the screen.
  Each sits in a card with a header (the tool's name, the connector, and a button to fold it away), a placeholder until the view finishes its handshake (or 1.5 s after its document loads), and a retry on errors.
  The frame's `color-scheme` follows the chat's theme, so a view that follows the theme it is given is transparent over the card rather than painted an opaque page color.
  `prefersBorder: false` drops the card's border and background.

### 6. Mock LLM directives (development)

In mock mode (`LLM_BACKEND=mock`, echo), lines like `/mock-tool <name> <json>` and `/mock-wait <duration>` in a message make the mock call those tools round by round.
It then answers with the rest of the message and a summary of the results.
Mock turns now also offer the turn-only tools and the chat's MCP tools (a chat without connectors discovers nothing, so mock e2e stays hermetic).
`cmd/mcp-demo-server` is a small unauthenticated MCP server with an elicitation form, a URL elicitation and an MCP App counter, for trying all of this locally.

## Alternatives considered

- **End the turn to ask (no blocking).** It is durable and holds no worker, but mid-run approvals become impossible, and MCP elicitation must be answered inside the tool call anyway. The timeout keeps the blocking bounded.
- **Store answers on the job row.** A JSON column on `job` would save a table, but prompts need a conditional answer, an expiry and a history across a turn's several prompts; a row each is simpler to reason about.
- **Steering as a separate endpoint and table.** A queue of notes on the running job would need its own fallback for notes the turn never reaches. Reusing the message and turn-gate queue makes that fallback free, and the follow-up shows in the thread as the message it is.
- **Folding the follow-up into the last tool result.** It was the first version: accepted by every provider, but it put the person's words inside a tool's output. A user turn of its own reads right to the model and is already a shape every adapter sends, so the fold is only the fallback.
- **A system message for the follow-up.** It would raise the person's text above the conversation (a prompt-injection shape), and Anthropic takes no system messages mid-conversation.
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
