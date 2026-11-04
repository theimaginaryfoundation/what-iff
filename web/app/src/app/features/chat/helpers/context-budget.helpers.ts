import { ContextBreakdown } from '../../../core/models/message.model';

/**
 * Which limit a Context X-ray gauge is read against (#397):
 * - `checkpoint`: the thread's checkpoint budget. Past it, older turns are summarized into the
 *   thread summary at a later checkpoint. Standard turns, and snapshots without run data.
 * - `run`: an Advanced run's own limit. Past it, the run compacts its earlier steps into a work
 *   log before its next step. The run's final call carries the run's own work, so its total is
 *   not comparable to the checkpoint budget.
 */
export type ContextLimitKind = 'checkpoint' | 'run';

/** Usage against one limit. */
export interface ContextLimitUsage {
  /** Tokens compared to the ceiling, the same figure the server compares. */
  used: number;
  /** The ceiling in tokens; 0 when the snapshot has none. */
  ceiling: number;
  over: boolean;
  /** Tokens past the ceiling (0 when within it). */
  overBy: number;
  /** used / ceiling as a rounded percentage, not clamped (0 without a ceiling). */
  usedPct: number;
}

/** What the X-ray gauge shows for one snapshot: its main limit and, on an Advanced run, the checkpoint too. */
export interface ContextBudgetView {
  kind: ContextLimitKind;
  /** The gauge's limit, read against the snapshot's total. */
  main: ContextLimitUsage;
  /** On an Advanced run, the thread's checkpoint: the run's starting context against the checkpoint budget. */
  checkpoint: ContextLimitUsage | null;
  /** How many times an Advanced run compacted its context (0 otherwise). */
  compactions: number;
}

function positive(n: number | null | undefined): number {
  return typeof n === 'number' && Number.isFinite(n) && n > 0 ? n : 0;
}

function limitUsage(used: number, ceiling: number): ContextLimitUsage {
  const over = ceiling > 0 && used > ceiling;
  return {
    used,
    ceiling,
    over,
    overBy: over ? used - ceiling : 0,
    usedPct: ceiling > 0 ? Math.round((used / ceiling) * 100) : 0,
  };
}

/**
 * The limits a snapshot's gauge is read against, from the budgets the server recorded with it
 * (ContextBreakdown.budget_tokens, checkpoint_input_tokens, run_budget_tokens), so the X-ray reads
 * what enforcement reads rather than one budget for every loop. `total` is the gauge's total
 * (vendor-reported input, or the summed estimates).
 *
 * An Advanced turn's total is never compared to the checkpoint budget: the checkpoint measures the
 * run's first call, and the total is its last. Without a recorded run limit the main gauge then has
 * no ceiling. Snapshots captured before these fields existed read as Standard ones.
 */
export function contextBudgetView(breakdown: ContextBreakdown, total: number): ContextBudgetView {
  const checkpointBudget = positive(breakdown.budget_tokens);
  const compactions = positive(breakdown.run_compactions);
  if (breakdown.agent_loop !== 'advanced') {
    return { kind: 'checkpoint', main: limitUsage(total, checkpointBudget), checkpoint: null, compactions };
  }
  const checkpointInput = positive(breakdown.checkpoint_input_tokens);
  return {
    kind: 'run',
    main: limitUsage(total, positive(breakdown.run_budget_tokens)),
    checkpoint: checkpointInput > 0 && checkpointBudget > 0 ? limitUsage(checkpointInput, checkpointBudget) : null,
    compactions,
  };
}
