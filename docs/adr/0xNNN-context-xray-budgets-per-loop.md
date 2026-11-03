# ADR 0xNNN: Context X-ray budgets follow the loop that ran

- **Status:** Proposed
- **Date:** 2026-11-03
- **Deciders:** What Iff maintainers
- **Issue:** [what-iff#397](https://github.com/theimaginaryfoundation/what-iff/issues/397), [what-iff#430](https://github.com/theimaginaryfoundation/what-iff/issues/430)
- **PR:** TBD

## Context

The Context X-ray compares a turn's input tokens with one budget, `budget_tokens`, which the agent filled with the constant `checkpointMaxLastInputTokens` (30k).
On an Advanced (ReAct) turn the total is the run's final call, which also carries every step the run took (tool results, notes, continuation prompts).
A long run therefore showed "60,000 / 30,000 tokens" and the over-budget warning, although nothing was over any limit.

Two limits actually apply to an Advanced turn, and neither matched the display:

- The thread's checkpoint compares the run's **first** call with the checkpoint budget (`turnContextTokens`), so the run's own growth never triggers it.
- The run compacts itself between steps once a call passes `first + AGENT_ADVANCED_COMPACT_TOKENS`, or 60% of the model's window with real growth (`shouldCompact`).
  Past that, earlier steps are replaced by a work log and the oldest tool calls are dropped.

The checkpoint budget is also resolved per chat (`resolveContextBudget`, the overlay seam from ADR 0x024), while the X-ray read the constant, so the two could drift as soon as a tier policy is installed.

## Decision

### 1. The server records the budgets with the snapshot

`buildContextBreakdown` takes every budget from what enforces it (`applyContextBudgets`), not from a constant:

- `budget_tokens` is `resolveContextBudget(chat).MaxLastInputTokens`, as `postMessageProcessing` uses.
- `checkpoint_input_tokens` is what checkpointing compares with it (`turnContextTokensFrom`: the first call on an Advanced turn, the final call otherwise).
- On a turn that ran the Advanced loop, `agent_loop`, `run_budget_tokens` and `run_compactions` say how the run bounded itself.

The fields are additive and optional, so `ContextBreakdownVersion` stays 1.

### 2. One function for the compaction rule

`shouldCompact`'s two rules are expressed as one input size, `advancedCompactCeiling`, which both the loop and the X-ray use.
The ceiling is the current adapter's, so after a compaction it is measured from the compacted context.

### 3. The gauge reads the total against the loop's own limit

A Standard turn is read against the checkpoint budget, as before.
An Advanced turn's total is read against the run limit, and a second row shows the checkpoint on the run's first call.
An Advanced total is never compared with the checkpoint budget, even when no run limit was recorded.

The frontend only reads the snapshot (`contextBudgetView`); it does not recompute the limits, so there is nothing to keep in sync with the server.

### 4. Over budget is visibly different

Past its limit, the gauge marks where the limit falls on the track, hatches the part past it, colors the total, shows the percentage of the limit and the tokens over, and says what happens next: older turns are summarized at the next checkpoint, or the run compacts its earlier steps.
The existing sentence "Heavy turns can exceed this budget until the next compaction." is kept; the private overlay's spec asserts it.

## Alternatives considered

- **Pick the budget in the frontend from the active turn settings** (the values `ChatSendGate.setActiveTurnSettings` receives).
  Rejected: the X-ray shows a past turn, whose loop may differ from the current one, and the run limit depends on per-account limits and the model's window that only the server knows.
- **Raise `budget_tokens` on Advanced turns.**
  Rejected: it would hide the checkpoint, which still applies to the run's first call, and change the meaning of an existing field.
- **Show the run's first call as the total.**
  Rejected: the segments describe everything the final call carried, and the total is the input the turn was billed for.

## Consequences

- An Advanced turn reads e.g. "60,000 / 120,000 tokens" with "Thread checkpoint 20,000 / 30,000 at turn start" below it.
- Snapshots captured before this change carry no `agent_loop` and still read as Standard, so old Advanced turns keep the old display.
- After a compaction, the tool-result row counts every call the run made, including ones compacted away, so the rows can add up to more than the total; the X-ray says so.

## Not done yet

- Old Advanced snapshots could be recognized from the message's `agent_run` and shown without the checkpoint comparison.
- The segment estimates for a compacted run could be rebuilt from what the final call carried instead of every tool call.
