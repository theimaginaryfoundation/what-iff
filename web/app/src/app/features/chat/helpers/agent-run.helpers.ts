import { CHAT_TURN_PLAN_STATUSES, ChatTurnPlanItem, ChatTurnPlanStatus } from '../../../core/models/job.model';
import { AgentRun, AgentRunEndReason } from '../../../core/models/message.model';

const PLAN_STATUSES = new Set<string>(CHAT_TURN_PLAN_STATUSES);

/**
 * An Advanced run's plan items (ADR 0x026), from either a live progress payload or a saved
 * `agent_run`. Items without content are dropped. An item with an unknown status is dropped too,
 * unless `unknownStatusAs` is given, in which case it is shown with that status instead.
 */
export function normalizePlanItems(raw: unknown, unknownStatusAs?: ChatTurnPlanStatus): ChatTurnPlanItem[] {
  if (!Array.isArray(raw)) return [];
  const items: ChatTurnPlanItem[] = [];
  for (const item of raw as unknown[]) {
    if (!item || typeof item !== 'object') continue;
    const { content, status } = item as { content?: unknown; status?: unknown };
    if (typeof content !== 'string' || content.trim() === '') continue;
    if (typeof status === 'string' && PLAN_STATUSES.has(status)) {
      items.push({ content, status: status as ChatTurnPlanStatus });
    } else if (unknownStatusAs) {
      items.push({ content, status: unknownStatusAs });
    }
  }
  return items;
}

/** One note of a saved Advanced step, as shown: unknown kinds read as plain text. */
export interface AgentRunNoteView {
  kind: 'reasoning' | 'text';
  text: string;
}

export interface AgentRunStepView {
  step: number;
  notes: AgentRunNoteView[];
}

/** A saved Advanced run reduced to what the message bubble shows. */
export interface AgentRunView {
  steps: AgentRunStepView[];
  plan: ChatTurnPlanItem[];
  /** Why the run stopped early; null when it finished normally (or the reason is absent). */
  stopLabel: string | null;
}

const STOP_LABELS: Readonly<Record<string, string>> = {
  step_limit: 'Stopped: step limit reached',
  duration_limit: 'Stopped: time limit reached',
  quota: 'Stopped: credit limit reached',
  cancelled: 'Stopped: cancelled',
  failed: 'Stopped: failed',
  stalled: 'Stopped: no further progress',
};

/** A short label for why a run stopped early; null for `done` or no reason. Unknown reasons get a generic label. */
export function agentRunStopLabel(reason: AgentRunEndReason | null | undefined): string | null {
  const key = reason?.trim();
  if (!key || key === 'done') return null;
  return Object.hasOwn(STOP_LABELS, key) ? STOP_LABELS[key] : 'Stopped before finishing';
}

/**
 * What to show for a saved Advanced run: steps that have notes (empty notes dropped), the plan, and
 * a stop label. Null when there is nothing to show, so the bubble can skip the disclosure.
 */
export function agentRunView(run: AgentRun | null | undefined): AgentRunView | null {
  if (!run) return null;
  const steps: AgentRunStepView[] = [];
  for (const step of Array.isArray(run.steps) ? run.steps : []) {
    if (!step || typeof step.step !== 'number') continue;
    const notes = (Array.isArray(step.notes) ? step.notes : [])
      .filter(note => !!note && typeof note.text === 'string' && note.text.trim() !== '')
      .map((note): AgentRunNoteView => ({ kind: note.kind === 'reasoning' ? 'reasoning' : 'text', text: note.text.trim() }));
    if (notes.length > 0) steps.push({ step: step.step, notes });
  }
  const plan = normalizePlanItems(run.plan, 'pending');
  const stopLabel = agentRunStopLabel(run.end_reason);
  if (steps.length === 0 && plan.length === 0 && !stopLabel) return null;
  return { steps, plan, stopLabel };
}
