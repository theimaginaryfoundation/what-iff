# ADR 0x024: Swappable context-budget policy (extended context and capacity tiers)

- **Status:** Accepted (mechanism) / Proposed (policy)
- **Date:** 2026-10-02
- **Deciders:** What Iff maintainers

The swap seam in "What was actually built" ships here as a behavioral no-op
(`internal/agent/context_budget_seam.go`). The tier *policy* that will drive it is still a proposal
pending calibration. Preset values in this document are placeholders.

> Provenance: adapted from the `chat-app` ADR `0x019 — Extended Context Window & Capacity Tiers`.
> The adaptation is not a straight copy: it splits the design along the public/private
> (OSS/overlay) seam and re-ranks the levers for the tiny-model, tool-heavy world `what-iff` now runs
> in. Divergences from the source are called out inline as **[what-iff]**.

## Context

`what-iff` keeps a long thread inside a bounded window by compacting (checkpointing) it on a
schedule. Today every user and every conversation runs with **one fixed set of budget constants**.
That was a deliberate choice — small, cheap, predictable context — and it served the first year
well. Two things have changed since:

1. **Tiny, cheap models.** Running on e.g. GPT-5.6-luna at ~$0.20/M input tokens, the old static
   "compact at ~30k" budget is far more aggressive than the economics require. At Opus ($5/M) or
   Sonnet ($3/M) a tight window is prudent; at $0.20/M it is leaving continuity on the table for no
   real cost saving. **[what-iff]** — this is the primary motivation and it inverts the source
   ADR's framing: a *generous* budget is now the affordable default, and the cost-bounded budget is
   the exception (expensive models, or protecting a free-unlimited tier), not the baseline.
2. **Tool- and MCP-heavy flows.** Code review, MCP tooling, and subagent orchestration routinely
   bump the ceiling. A single tool-heavy turn can blow past 30k on results alone, forcing a
   compaction mid-flow — exactly when losing context hurts most.

We want three things: offer expanded context where it's affordable or paid for; keep a
cost-bounded experience for overage/free/expensive-model conversations; and never break continuity
when a conversation moves between budgets.

Before proposing knobs, this ADR pins down **what the current system actually does**, because the
choice of levers depends on it.

### What the code actually does today

All symbols verified on `main` at the time of writing (2026-10-02).

| Common assumption | Codebase reality | Source |
|---|---|---|
| Fixed **~30k token budget** enforced everywhere | 30k/32k are compaction **triggers**, not a hard cap. `checkpointMaxLastInputTokens = 30_000`, `checkpointMaxEstimatedContextTokens = 32_000`. A throttle (`checkpointMinTurnsBetweenCheckpoints = 5`) deliberately lets context run *over* briefly rather than compact every turn. | `internal/agent/message.go:59-65` |
| **Carry-over turns = 6** | `carryOverMaxTurns = 3`, `carryOverMaxTokens = 500`. Carry-over is only the **post-checkpoint bridge** — a tiny window of turns replayed right after a compaction. | `internal/agent/message_context_builder.go:22-23` |
| History depth is the "carry-over" knob | The real history lever was a flat **`pageSize = 50`** messages per turn, scoped to `sent_at >= LastCheckpointAt`. This is what "how much conversation the model sees" actually means. Now named `defaultHistoryPageSize` and routed through the seam. | `internal/agent/message_context_builder.go:27,~283` |
| Memory fetch: **up to 100/turn, escalating** | `GetRelatedMemories` fetches a hardcoded **`Limit(5)`** per turn (no escalation). The "100" is an *accumulation ceiling*: each turn's 5 memories persist on the message row and reload from history on later turns until the next checkpoint resets the window. | `internal/datastore/memory.go:2380` |
| Summary size ≈ 2k | `checkpointSummaryMaxTokens = 1200`. | `internal/agent/conversation_summary.go:15` |
| Scratchpad ≈ 1k | Already **2048** content budget. And it is **per-personality** (`Personality.scratchpad`), shared across every chat on that personality — `chat.Scratchpad` is a read-through copy. | `internal/agent/provider/constants.go:7` |
| Tool-call rounds are a per-mode knob | `maxToolCallRounds = 10` is a single global constant. | `internal/agent/message.go:69` |
| Per-conversation tool gating needs building | It **already exists**: `Chat.ToolsEnabled` + `Chat.DisabledTools`. | `internal/models/chat.go:30-31` |
| "mode" is a free name | **Collision.** `Chat.ActiveMoodID` and the agent-facing mood tools already own "mode" in the UI/tool surface. A second "mode" concept will confuse. Use "capacity tier." | `internal/models/chat.go:44` |

Two consequences shape the whole design:

1. **The compaction policy is already parameterized.** `decideCheckpoint(policy, inputs)` reads a
   `checkpointPolicy` struct every turn (`postprocessing_policy.go`). Today it is fed constants.
   Making context capacity configurable is, at its core, **feeding that struct tier-dependent
   values instead of constants** — the machinery already exists.
2. **The scratchpad cannot be sized per-conversation.** It is a per-personality object; two chats
   on the same personality in different tiers would fight over one scratchpad. This kills any
   "scratchpad sized per tier" idea on architectural grounds.

## Decision

Split the work along the repo seam, because this feature straddles the OSS boundary:

- **Public `what-iff` owns the *mechanism*** — a swappable context-budget resolver. The default
  resolves to today's exact constants (a no-op). Any consumer, including an OSS self-hoster, can
  install a resolver to vary the budget per conversation. **This ADR + the landed seam.**
- **The private overlay owns the *policy*** — the concrete capacity tiers, their calibrated values,
  billing gating (which tier a user may hold, free-overage slab allowance, overage fallback). This
  lives in `what-iff-private` and is captured in a dated decision doc there, **not** in the public
  repo, mirroring how billing/quota already evacuated to the overlay.

**[what-iff] — this is the biggest divergence from `0x019`.** The source ADR wove billing gating
and slab-allowance math straight into the design. Post-split, billing lives in the overlay, so the
public seam is a **pure `chat -> budget` mapping** with no billing awareness. Benefits: the public
repo stays free of SaaS-billing concerns; an OSS self-hoster gets "always Extended, no billing" for
free (just don't install a resolver, or install a trivial one); and the hot path stays clean —
budget resolution never touches the quota system.

### Working tier names: `Compact` / `Standard` / `Extended`

Placeholders (see *Open questions*). The small/overage tier is where cost is bounded, the large tier
is where context is bought or afforded, and **neither is called "mode."** `Standard` is pinned to
today's constants so migrating every thread to it is a zero-behavior-change no-op.

### The transition mechanism *is* the compaction system (the core idea)

Tier changes are **prospective, not retrospective.** There is no collapse event, no forced
compaction, no separate transition algorithm.

- The budget fed to `decideCheckpoint` (and to the history/carry-over builder) is resolved from the
  conversation's active tier each turn.
- When the tier changes, nothing runs. The flag flips. The **next** turn already reads the new
  budget; the next natural checkpoint compacts to the new shape.
- Under the new limits → nothing happens, silently. Over them → the conversation keeps running on
  current context until the next natural checkpoint, which uses the new budget.

This is the strongest part of the proposal and should anchor the pitch: we are wiring a value
source into an existing seam, not building a subsystem.

## Which knobs actually move — a ranked opinion

Mapped to real code, ranked by value **for `what-iff`'s actual goals** (more tool context, more
continuity, affordable free-unlimited chat). **[what-iff]** — this ranking deliberately differs
from `0x019`, which under-weighted continuity levers.

**Tier 1 — build these; they carry the feature:**

1. **Compaction token thresholds** (`checkpointMaxLastInputTokens`, `checkpointMaxEstimatedContextTokens`).
   The primary lever, and where the cheap-model win lives: 30k → 100k+ is the whole point. Clean per
   tier via the policy struct. **In the seam.**
2. **History window** (`defaultHistoryPageSize`). The true "how many turns the model sees" knob.
   Pairs directly with #1. **In the seam.**
3. **`MinTurnsBetweenCheckpoints` throttle.** *Promoted from the source ADR.* On cheap models you'd
   rather carry a large window than compact constantly — and every compaction is both a summarizer
   LLM call *and* a "jarring" seam in the conversation. Raising this cuts summarizer invocations and
   smooths continuity at once. It directly serves Gori's "less jarring" goal and is nearly free.
   **In the seam.**
4. **Carry-over bridge** (`carryOverMaxTurns` / `carryOverMaxTokens`). *Promoted.* The source ADR
   called this "small." It is the single most direct anti-jarring knob we have: more turns replayed
   right after a checkpoint = a cleaner seam across the compaction the user just hit. For the
   continuity goal, scale this **up** for `Extended`; keep it tight for `Compact`. **In the seam.**
5. **Tool-call rounds** (`maxToolCallRounds`, global 10 today). High value for the MCP/tool use case:
   complex flows want *more* rounds, cost-bounded turns want fewer. Zero retroactive cost; safe to
   change mid-conversation. **Not yet in the seam** — reserved (see §Scope).

**Tier 2 — worth doing, more work or narrower:**

6. **Memory accumulation ceiling** (*reframed, needs new logic*). The per-turn fetch is already a
   flat 5; the bloat is *accumulation* — ~20 turns × 5 memories × ~100 tokens ≈ 10k, a third of the
   Compact budget. The tier should cap how many accumulated items are re-emitted, **dropping
   oldest**, not throttle the per-turn fetch. This is the one area needing genuinely new code.
7. **Max input message size** (*new*). A guardrail more than a context-quality lever — hard-cap
   pasted input in the cost-bounded tier. Belongs to the overage/billing story, so its gating is an
   overlay concern; the cap mechanism itself can be public.
8. **MCP / subagent availability per tier.** Reuse the existing `ToolsEnabled` / `DisabledTools`
   plumbing. On/off per tier.

**Tier 3 — my opinion: do NOT pull these per tier:**

9. **Scratchpad size.** Architecturally impossible per-conversation (per-personality) *and* low
   value — per `what-iff`'s history the scratchpad is not where the budget blows out. If we want
   more working memory, bump `ScratchpadMaxContentLength` **uniformly** (2048 → 3072). Tier-independent.
10. **Summary size, per tier.** Shrinking it in `Compact` saves ~800 tokens and risks losing the
    thread — a bad trade. If anything, bump it **globally** (1200 is smaller than the 2048 scratchpad,
    which is surprising). Tier-independent.
11. **Per-turn memory fetch `Limit(5)` as a cost lever.** Raising it *increases* load — the opposite
    of what a cost-bounded tier wants. Only nudge it up for `Extended` for denser recall, never as a
    Compact saving.

The through-line: **spend the design effort on history depth, compaction cadence, and the carry-over
bridge.** Those move both cost and continuity. The anchors (scratchpad, summary) should move
uniformly or not at all.

## What was actually built in this pass (the seam)

`internal/agent/context_budget_seam.go` introduces:

- `contextBudget` — a struct bundling the six "when to summarize / what context" knobs currently in
  scope (the three checkpoint values + history page size + carry-over turns/tokens).
- `defaultContextBudget()` — returns the exact prior constants. A locked test
  (`context_budget_seam_test.go`) asserts it byte-for-byte matches them; this is the "Standard ==
  today" invariant.
- `var contextBudgetForChat func(chat *models.Chat) contextBudget` — the swap-point, nil-guarded,
  mirroring `external_tools_seam.go`. The overlay installs it from an `init()` in a composed file in
  package `agent`. Nil ⇒ default ⇒ today's behavior.
- `resolveContextBudget(chat)` — used at the two call sites: the `checkpointPolicy` literal in
  `message.go` and the carry-over / history-page-size calls in `message_context_builder.go`.

No behavior change: with no resolver installed, every value is identical to before. The scheduled
turn-count trigger (`maxTurnsBeforeCheckpoint`) and `maxToolCallRounds` are intentionally **left
out** of this first seam — reserved levers, added when the tier policy actually needs them.

## Continuity anchors — explicitly out of scope for tiering

Restating for emphasis: **scratchpad and summary sizes stay global.** See levers 9–10 above and the
Rationale. Any bump to either is a uniform, tier-independent change that can ship separately.

## Deferred to the overlay policy layer (not this repo)

Captured here only so the public reader knows where they went:

- **Tier storage & defaults** (`Chat.capacity_tier`, `Personality.default_capacity_tier`) — mirror
  the mood pattern (per-conversation value + personality default + auto/pinned).
- **Billing gating** — which tiers a user may hold, overage → prospective `Compact` fallback, rate
  limiting. Uses the overlay's existing quota signals.
- **Free-overage slab allowance** — bill `max(0, N − F)` slabs with `F` pinned to the `Compact`
  context cap, so free models are genuinely free in `Compact` and the slab cliff disappears. This is
  the source ADR's §9 and is **entirely an overlay/billing concern** now.
- **Preset value calibration** — via the mock-LLM framework; `Standard` is the fixed reference.

## Rationale

- **Why the seam is `chat -> budget` and billing-free.** Post-split, billing lives in the overlay.
  Keeping the public resolver pure keeps the OSS repo free of SaaS concerns, keeps budget resolution
  off the quota hot path, and makes self-hosted "no billing, always generous" the trivial default.
- **Why the compaction policy is the transition mechanism.** `decideCheckpoint` already consumes a
  policy struct per turn; parameterizing it means a tier change needs no collapse algorithm and no
  retrospective rewrite.
- **Why continuity levers rank higher here than in `0x019`.** Gori's stated goals are more tool
  context *and* less jarring summarizer seams. `MinTurnsBetweenCheckpoints` and the carry-over
  bridge target the seam-smoothness goal directly and cheaply; the source ADR treated them as
  secondary. On cheap models, "compact less often, replay more across the seam" is close to free.
- **Why anchors don't scale with tier.** Scratchpad is per-personality and cannot be owned by a
  per-conversation setting; the summary is a continuity anchor whose shrinkage buys almost nothing.
- **Why `Standard` == today.** Migration becomes a no-op and calibration gets a fixed reference:
  `Standard` is "what we validated over a year of production," `Compact`/`Extended` are the deltas.

## Revised preset table

All values are **placeholders pending calibration.** `Standard` is pinned to current production
constants. Billing-gated columns are shown for context but live in the overlay.

| Knob (code symbol) | Compact | Standard (= today) | Extended |
|---|---|---|---|
| Est-context trigger (`checkpointMaxEstimatedContextTokens`) | 32k | **32k** | 120k |
| Last-input trigger (`checkpointMaxLastInputTokens`) | 28k | **30k** | 110k |
| History window (`defaultHistoryPageSize`, messages) | 20 | **50** | 90 |
| Min turns between checkpoints (`checkpointMinTurnsBetweenCheckpoints`) | 5 | **5** | 8 |
| Carry-over bridge (`carryOverMaxTurns` / `carryOverMaxTokens`) | 3 / 500 | **3 / 500** | 8 / 1600 |
| Tool-call rounds (`maxToolCallRounds`) | 3 | 7\* | 10 |
| Accumulated-memory ceiling, drop-oldest (**new**) | 30–50 | ~60 | ~100 |
| Max input message size (**new**) | 4k (hard) | 12k | 32k+ |
| Scratchpad (`ScratchpadMaxContentLength`) | 2048 | 2048 | 2048 |
| Summary (`checkpointSummaryMaxTokens`) | 1200 | 1200 | 1500 |
| MCP tools / complex subagents (`ToolsEnabled`/`DisabledTools`) | off | on | on |

\* `maxToolCallRounds` is globally 10 today; `Standard` = 7 is a mild tightening — the one place
`Standard` isn't pixel-identical. Decide during calibration whether to keep 10 for a true no-op.

## Implementation notes

- **Landed:** `context_budget_seam.go` + test; call sites in `message.go` and
  `message_context_builder.go`; `defaultHistoryPageSize` constant.
- **Next (public):** fold `maxToolCallRounds` into `contextBudget` (reserved); memory accumulation
  ceiling in `mergeAdditionalContextItems`; max-input-size cap mechanism.
- **Next (overlay):** tier enum + storage (ent schema on chat/personality), the
  `contextBudgetForChat` resolver installed via a composed `init()`, billing gating, slab allowance.
- **Docs:** update `docs/ARCHITECTURE_SUMMARY.md` (context section) once tiers land.

## Open questions / future work

- **Tier names.** `Compact` / `Standard` / `Extended` are placeholders. Anything but "mode" or "Pro."
- **`Standard` tool-round value** — 7 vs 10. Prefer 10 if we want a strict no-op; 7 if the mild
  tightening is acceptable.
- **Memory accumulation semantics** — hard ceiling with oldest-drop vs. leaning on the dedupe pass.
- **`MinAssistantMessagesSinceCheckpoint` per tier** — the scheduled turn-count trigger could also
  vary; left out of the first seam. Secondary; the token thresholds do the heavy lifting.
- **Global bumps** — summary (1200 → larger) and scratchpad (2048 → 3072); tier-independent, decide
  whether they ride with this work.
- **Preset calibration** — every value above is a placeholder; run the mock-LLM framework per tier,
  especially the untested `Extended` thresholds at 100k+.
- **Cross-personality UX** — because scratchpad is shared per-personality, document that tier is
  per-conversation but the working scratchpad is personality-wide.
