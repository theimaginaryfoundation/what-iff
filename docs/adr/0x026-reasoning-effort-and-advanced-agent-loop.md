# ADR 0x026: Reasoning effort and the Advanced (ReAct) agent loop

- **Status:** Proposed (implemented on the feature branch, in review)
- **Date:** 2026-10-18
- **Deciders:** What Iff maintainers
- **Builds on:** the shared tool loop (`internal/agent/agentloop.go`), the provider `AgentAdapter` seam, live reasoning streaming (`Job.draft_reasoning` → `ChatMessage.model_reasoning`), and `internal/metering`

## Context

Two capabilities are wanted for longer, harder tasks:

1. **Reasoning effort.** People want to choose how hard a model thinks.
   Today no request carries a user choice: z.ai GLM always reasons at a fixed `high` effort, Xiaomi MiMo always reasons (thinking can only be switched off), Claude Opus 5 and Sonnet 5 think by default because no `thinking` parameter is sent, and OpenAI reasoning models run at the API's default effort with no reasoning text captured.
2. **A long-horizon agent loop.** The current loop (`handleAgentLoop`) calls the model until it stops asking for tools, capped at `maxToolCallRounds` (10), and then forces a final answer.
   That suits a chat turn, but not a task that needs dozens of tool calls, a plan, and progress checks along the way, the way coding agents work.

Both must be opt-in additions: nothing changes for anyone who does not select them.

## Decision

### 1. Two settings, opt-in

| Setting | Values | Unset means |
|---|---|---|
| `reasoning_effort` | `off`, `low`, `medium`, `high` | The model's native default, i.e. today's behaviour |
| `agent_loop` | `standard`, `advanced` | `standard`, today's loop |

Only four effort levels are offered.
Providers expose more (`minimal`, `xhigh`, `max`, budgets), but which of those a model accepts varies per model, not just per provider; four levels keep a small, predictable support matrix.

"Unset" is distinct from `off`: unset sends exactly what is sent today, while `off` explicitly asks for no reasoning where the model allows it.

### 2. Where the settings live, and which one wins

Each level stores only its own explicit value; nothing is copied between levels. A turn resolves each setting at the moment it runs (`models.ResolveTurnSettings`):

| Level | Stored on | When it applies |
|---|---|---|
| Agent job | `AgentJob.reasoning_effort` / `.agent_loop` | That job's runs only; overrides everything, never written to the thread |
| Mode | `Mood.reasoning_effort` / `.agent_loop` | Every turn while that mode is active; overrides the thread, **not persisted** onto it, so leaving the mode restores the thread's value |
| Thread | `Chat.reasoning_effort` / `.agent_loop` | Every turn in the thread; set per thread from the composer |
| Personality | `Personality.reasoning_effort` / `.agent_loop` | Turns in its threads that set none of their own |
| Account default | `UserPreference.reasoning_effort` / `.agent_loop` | Turns whose personality and thread set none |

So the effective value of a turn is the first set of: job → mode → thread → personality → account, per setting.
Every column is nullable; `NULL` means "not set at this level", and an unset thread inherits the current personality and account values (a change to them reaches existing threads).

A **sandboxed** thread (`context_scope: sandbox`, which is what the Discord relay creates) takes only its own and a job's values: other people start turns there on the owner's account, so the owner's modes, personality and account default must not start Advanced or a high effort for them. It runs Standard at the model's default effort unless the thread itself is given a setting.

When a mode overrides the thread's own setting, the composer shows the effective value and says the mode set it (with a tooltip explaining that the mode's setting wins while it is active).

### 3. Reasoning effort: one capability check, one mapping per provider

Whether a model can take an effort, and how, is decided in code from its provider and name (`models.ReasoningSupportFor`), not stored on the `Model` row, so admin-added models are covered without a migration.
The API exposes it on each model as `reasoning_support`:

| `reasoning_support` | Meaning | UI |
|---|---|---|
| `none` | No effort control | Selector hidden; the setting is ignored |
| `optional` | `off` and the three levels | All four choices |
| `toggle` | Thinking can only be on or off | Off / On (`On` is stored as `medium`) |
| `always` | Always reasons; levels, no `off` | Low / Medium / High; `off` behaves as `low` |

Mapping (`provider` package):

- **OpenAI Responses (GPT-5 family):** `reasoning.effort` = the level; `off` → `none` (`minimal` on the original `gpt-5`/`gpt-5-mini`/`gpt-5-nano`, which do not accept `none`). When the level is not `off`, `reasoning.summary = auto` is requested and the summaries are captured as reasoning text. The o-series cannot turn reasoning off (`always`); chat-tuned aliases (`-chat`), the `-pro` models and GPT-4-family models are `none`.
- **Anthropic:** `low`/`medium`/`high` → adaptive thinking with summarized display plus `output_config.effort`; the 4.5 generation and older (Haiku/Sonnet/Opus 4.5, Opus 4.1, the 4.0 models, Sonnet 3.7), which predate adaptive thinking, get a thinking budget instead. At `high` a streamed turn's output cap is raised further (`ClaudeHighEffortMaxOutputTokens`); non-streamed sub-agent calls keep the lower cap, which the SDK requires. `off` → thinking disabled on models that accept it (Opus 5, Sonnet 5 and the 5.5 family's equivalents), nothing on models that do not think by default. Models that cannot disable thinking (Opus 5.5, Fable) are `always`. Thinking text is captured, and thinking blocks are replayed across tool rounds when an effort was chosen, as the API requires.
- **z.ai GLM (`always`):** `low` → `low`, `medium` → `high` (today's fixed value), `high` → `max`. The truncation fallback still drops to `low`.
- **Xiaomi MiMo (`toggle`):** `off` disables thinking for the turn; any level leaves it on.
- **Gemini (OpenAI-compatible `reasoning_effort`):** Gemini 3+ and 2.5 Pro always think (`always`); 2.5 Flash can switch it off (`optional`, off → `none`). Thought text is not captured: the compatible endpoint documents no response field for it, and requesting it (`include_thoughts`) cannot be combined with `reasoning_effort`. Thought signatures were already round-tripped.
- **Qwen (DashScope):** Qwen 3.x hybrids and `qwen-plus`/`qwen-flash` are `optional`: off → `enable_thinking: false`; a level turns thinking on with a `thinking_budget` (2k/6k/12k), or on Qwen 3.8 with `reasoning_effort` (low/medium/xhigh). A few thinking-only IDs are `always`.
- **DeepSeek:** `deepseek-flash` / `deepseek-v4-pro` are `optional` via `reasoning_effort`: off → `none` (and `thinking: disabled`), low → `low`, medium → `high`, high → `max`. Thinking is on by default, and DeepSeek requires a tool-calling turn's `reasoning_content` echoed back, so reasoning is captured and replayed on every DeepSeek turn (this fixes a 400 on the second tool round).
- **Mistral:** hybrid models (Small 4, Medium 3.5, Large 4) take only `reasoning_effort` `none`/`high` (`toggle`). While reasoning, Mistral returns content as typed chunks, which the adapter parses (thinking → reasoning, text → reply) and replays as chunks. Aliases (`mistral-small-latest`, `mistral-large-latest`) stay `none`: their target changes without notice.
- **The rules fail closed.** Each provider's table (`internal/models/reasoning_models.go`) lists the model families it supports; a model the tables do not list is `none`, so nothing is sent and its request is what it was before this ADR. Sending a control a model rejects fails the turn; leaving a new model out only hides the setting until it is added.
- DeepSeek, Qwen and Mistral share one adapter core (`reasoningChatAdapter`) that captures, streams and replays reasoning; MiMo keeps its own (truncation fallback).

Capturing OpenAI and Anthropic reasoning follows the z.ai pattern exactly: the adapter implements `ReasoningStreamer`, streams through `reasoningRelay`, and accumulates a `reasoningLog` that becomes `GenerateResponse.Reasoning` and finally `model_reasoning`.
It happens only when the effective effort is not `off`, so turns without an explicit effort are unchanged.

When reasoning is on, the output cap is raised to `provider.ReasoningMaxOutputTokens`, as it already is for GLM and MiMo.
Utility and archival calls (naming, expressions, checkpoints, memory extraction) never take an effort.
`run_subagent` inherits the turn's effort when the turn's own model runs with one.

### 4. The Advanced loop

The Advanced loop is a **superset** of the Standard loop: an outer loop of *steps*, each of which runs the Standard tool loop with its round counter reset to zero.

```
for step := 0; step < maxSteps; step++ {
    run the inner tool loop (up to maxToolCallRounds rounds)
        └─ inner cap reached → the step simply ends (no forced final answer); the next step continues
    model stopped calling tools:
        no open plan items → this text is the final answer; stop
        open plan items    → append a continuation message listing them; next step
    before every model call after the first: cancellation and quota re-check (with the calls not yet metered)
    a call refused for an overflowing context: compact once and continue
    between steps: step record, draft save, cancellation check, context compaction
}
outer cap reached → the existing ForceFinalResponse
```

- **Termination is driven by a plan tool.** Advanced turns are offered `update_plan`, a checklist (`pending`, `in_progress`, `completed`, `cancelled`).
  A run ends when the model replies without calling a tool and no item is `pending` or `in_progress`.
  A simple question therefore ends after one step, exactly like Standard; a multi-step task keeps going while it has open work.
  No extra "is it done?" model call is made.
- **Stall guard:** two continuation nudges in a row that produce no tool call and no plan change end the run with the latest text.
- **Limits:** `AGENT_ADVANCED_MAX_STEPS` (default 50, at most 500) and `AGENT_ADVANCED_MAX_DURATION` (default 60 minutes, at most 6 hours) are the deployment defaults. An invalid value falls back to the default and a value above its bound is clamped, each with a warning. A deployment's meter can set them per account or plan (`metering.AdvancedLimiter`); its values replace the defaults they set and are clamped to the same bounds.
- **Stopping at a limit.** A run stopped by the step or time limit is asked, with tools off, for a reply that says what it completed and what is still open, instead of keeping a mid-work note ("next I'll check X") as the answer. With tool results pending, the existing forced final answer is used. A run the quota stops makes no further call (it would spend past the quota): if it stopped right after an answer, that answer is the reply; mid-step, the reply is a fixed note saying the account reached its usage limit, with the notes and plan saved alongside. Stop (a cancelled context) is never mistaken for a quota refusal, and a meter that cannot decide (`Decision.Err`, a database error; retried once) fails the run rather than ending it as out of quota.
- **Carrying unfinished work.** When the thread's latest reply is a run that stopped before its work was done (any end other than `done`) at most a week ago, the next Advanced turn is told its open plan items, so "continue" picks them up instead of starting over. They are inserted as a user-role note quoting the earlier run, not as developer instructions: the items were written by the model, possibly steered by tool output.
- **Prompting:** an Advanced turn gets a developer instruction (inserted before the final user message) explaining that it may work over many steps, should keep a plan for multi-step work, and that its final reply must stand on its own.

**What is saved.**
Everything the model writes before its final reply (the text between tool calls, earlier steps' replies, and any native reasoning) streams live into `Job.draft_reasoning` with step headers.
The saved message is the final step's reply, with the run as structured data in `ChatMessage.agent_run` (`version: 1`):

- `steps`: per step, its events in the order they happened: `reasoning` (native) and `text` (what the model wrote) notes, and `tool_call` events naming the message's ToolCall row by id. Notes are capped (20k characters each, 400k per run) with a marker when cut; tool-call events are always kept. Readers skip event kinds they do not know.
- `plan`: the plan as it stood at the end.
- `end_reason`: `done`, `stalled`, `step_limit`, `duration_limit`, `quota`, `cancelled` or `failed`.

An Advanced message has no `model_reasoning`; Standard messages are unchanged.
Tool calls are saved with the step they ran in (`ToolCall.step`), and the live timeline (`ChatTurnProgress`) carries the step number and the current plan.

**Runs that do not finish.**
Each tool call is saved as a ToolCall row as soon as it finishes, linked to the run's job (`ToolCall.draft_job`) until a message takes it; the run's steps, plan and generation metadata are saved on the job (`Job.draft_agent_run`) at every step boundary and when the run ends in an error. The draft refers to the tool-call rows rather than copying them, so a long run's draft writes do not grow with every tool output.
When the run fails, is stopped, or the server dies mid-run, a partial reply is built from the draft: a short "stopped before it finished" text, the run (ending `failed` or `cancelled`), its tool calls (moved onto the message) and its provider-native web searches. That covers a failure or Stop in the running process (also when Stop is handled by another instance: the draft is kept for the worker), a graceful shutdown (`FailInFlightTurns`), a crash or OOM (the startup reaper finalizes interrupted Advanced drafts first), and agent-job runs (their draft lives on the tracking job, or a scheduled run's queue ticket). Turns without an Advanced draft end as before. Draft writes use a context detached from Stop and Shutdown, bounded by a timeout.
`GET /job/{id}`, which the client polls, does not read `draft_agent_run`.

**Context management.**
The Standard loop never needs to manage context inside a turn; a long run does.
Between steps the run is compacted when its context has grown enough: the current adapter's last call has grown past its first call by more than `AGENT_ADVANCED_COMPACT_TOKENS` (default 120k), or the last call uses more than 60% of the model's context window (`models.ContextWindowFor`) and the run has added at least 16k tokens.
Measuring the run's growth, not the absolute size, keeps a long thread on a large-context model from compacting at every step; the window rule protects small-context models.
An unlisted model is assumed to have a 128k window (`models.DefaultContextWindow`), and a family's entry is its smallest member's, so the window rule errs toward compacting early.
A call the provider refuses because its input no longer fits (`provider.IsContextOverflowError`), which can happen inside a step, compacts the run and continues once; a second overflow straight after fails the run.
To compact, a fresh adapter is built from the turn's original `ModelContext` plus a developer "work so far" segment (the goal, the plan, the latest notes, and the tool calls with truncated inputs and outputs, newest kept first), and continues from there.
Compaction is mechanical, not an extra summarization call, so it is deterministic and costs nothing.
For OpenAI this also starts a new `previous_response_id` chain.
Provider-native web searches from every adapter the turn built are saved and counted.

**Adapters.**
Two optional interfaces are added, implemented by every adapter `runGeneration` drives:

- `Continuer.ContinueWith(text)`: records the model's last reply in the adapter's own history and appends a user message, so the next `Call` continues the conversation.
- `UsageReporter.CallUsage()`: per-call input, cached input, cache-write, output and reasoning token counts for every call the adapter made, including a call discarded by a truncation retry (it was still billed by the provider).

The local backend (`LLM_BACKEND=local`) keeps its own pipeline and always runs Standard.

### 5. Metering

`metering.Usage` gains public fields that a deployment's meter may price:

- `ReasoningEffort`: the effort when one was explicitly selected; empty otherwise (including always-on models with no selection).
- `AgentLoop`: `standard` or `advanced`.
- `Calls`: the usage of each model call the record covers (`CallUsage`: the call's model, input including cached and cache-written tokens, cached input, cache writes, output including reasoning, and reasoning where the provider reports it separately). Standard turns and sub-agent runs report every call they made.
- `CallsReported`: `Calls` is the complete account of what the record covers, including when it is empty (an Advanced turn's final record after every call was in a step record: nothing left to charge). A meter must then not price `Tokens` on top. When it is false, per-call usage is unknown and `Tokens` is the whole basis.
- `Tokens` is the input of the turn's final model call on every path.
- `Step` and `TurnID`: an Advanced run is recorded **per step**, as each step finishes (`Step` set), and the turn's final record covers any calls made after the last step (a limit summary, a forced answer, the estimated call a Stop cut short). All of a turn's records, and its sub-agents' records, carry its `TurnID`; the final one also carries the message id.

Every Record a turn makes passes the turn's gate Decision (from its `Check` at turn start), even an hour into a run, so a meter must read anything that can change during the run (balances, caps) at Record time. Step records are made on a context that Stop does not cancel, so a step is never left unbilled. A sub-agent that fails after making calls is recorded too, and a failed or stopped run's web searches are metered.

The quota is re-checked (`Meter.Check`) before every model call of a run after the first, not only between steps, with the calls made since the last step record on the context (`metering.WithPendingUsage`, with the same fields a record carries).
A meter can then refuse once recording what is pending would leave nothing to pay for another call, so a run overshoots its quota by at most the call that crossed it, the same as a Standard turn, rather than by a whole step of up to ten calls. Those Checks' Decisions are read only for `Allowed` and `Err`, never recorded.
A refusal ends the run as described in §4 (end reason `quota`).
An Advanced turn's checkpoint decision uses its first call's input (the context the thread will carry), not the final call's, which also holds the run's own work.
`NoopMeter` ignores all of this.

### 6. API and UI

- `GET/PUT /user/preferences`, `PUT /personality/{id}`, `PUT /mood/{id}`, `PATCH /chat/{id}` and the agent-job routes accept and return `reasoning_effort` and `agent_loop`. On every update route an omitted field keeps its value (so a client that predates the settings cannot clear them by saving) and an empty string clears it (back to "not set").
- `POST /chat` accepts both as the thread's own values; omitted, the thread inherits as in §2.
- Models carry `reasoning_support`, derived from the provider and name, or an admin's `reasoning_support_override` on the model row (to fix a misclassified model without a deploy).
- Chat messages carry `agent_run` (§4). Its string fields, like the setting enums, are documented as open-ended: clients render unknown values generically.
- The UI exposes both settings in account settings, the personality editor, the mode editor, the agent-job editor and the composer.

## Consequences

- No behaviour change on release for anyone who does not select a setting.
- Advanced runs can be long and expensive; the step, duration, quota and compaction limits bound them, and a deployment's meter sees every call.
- A thread without its own value follows later changes to its personality and the account default; nothing records an inherited value as the thread's, so a later "apply to all threads", a plan gate on Advanced, or turning a personality's Advanced off reach every thread that did not choose for itself.
- A queued message behind a long Advanced run still fails after `chatTurnWaitTimeout` (10 minutes); the composer already blocks sending while a turn runs.
- Advanced runs are priced per call by a deployment's meter; the public side reports the facts (§5), not prices.

## Alternatives considered

- **Copying the settings onto a thread when it is created, as the model is.** The first version did, for consistency with the model. Reverted before release: unlike the model, these are optional per-turn knobs where "unset" means something, and once copied an inherited value cannot be told from a deliberate per-thread choice, which rules out applying a new default or a plan gate later. Resolving at each turn costs two small reads per turn.
- **Saving an Advanced run's notes as one text blob in `model_reasoning`.** Simpler, but it cannot be split into steps later, and a partial run could not be rebuilt from it; the structured `agent_run` replaced it before release.
- **A `finish` tool, or a separate judge call, to end Advanced runs.** A `finish` tool would carry the final answer as tool arguments, which the draft pipeline cannot stream; a judge call adds cost to every step. The plan checklist gives the loop the state it needs at no extra cost.
- **Summarizing the work log with a model when compacting.** Better summaries, but it adds a provider dependency and a cost per compaction; the mechanical work log is the first step, and a model summary can replace it behind the same seam.
- **Exposing provider-specific effort values.** Rejected for the support-matrix reason in §1.
