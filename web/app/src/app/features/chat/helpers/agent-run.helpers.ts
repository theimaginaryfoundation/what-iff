import { CHAT_TURN_PLAN_STATUSES, ChatTurnPlanItem, ChatTurnPlanStatus } from '../../../core/models/job.model';
import { AgentRun, AgentRunEndReason, AgentRunEvent } from '../../../core/models/message.model';
import { ToolCall } from '../../../core/models/toolcall.model';
import { toolCallDisplayName, toolCallFailed } from './tool-call-format.helpers';

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

/**
 * One event of a saved Advanced step, as shown: a note (reasoning or text), or a reference to one
 * of the message's tool calls.
 */
export type AgentRunEventView =
  { kind: 'reasoning' | 'text'; text: string } | { kind: 'tool_call'; toolCall: ToolCall; name: string; failed: boolean };

export interface AgentRunStepView {
  step: number;
  events: AgentRunEventView[];
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
 * One saved event as shown, or null to skip it: an empty note, a tool call the message does not
 * carry, or a kind this client does not know (a newer format may add kinds).
 */
function eventView(event: AgentRunEvent | null | undefined, toolCalls: ReadonlyMap<string, ToolCall>): AgentRunEventView | null {
  if (!event || typeof event !== 'object') return null;
  switch (event.kind) {
    case 'reasoning':
    case 'text': {
      const text = typeof event.text === 'string' ? event.text.trim() : '';
      return text ? { kind: event.kind === 'reasoning' ? 'reasoning' : 'text', text } : null;
    }
    case 'tool_call': {
      const toolCall = typeof event.tool_call_id === 'string' ? toolCalls.get(event.tool_call_id) : undefined;
      return toolCall
        ? { kind: 'tool_call', toolCall, name: toolCallDisplayName(toolCall.tool_name), failed: toolCallFailed(toolCall) }
        : null;
    }
    default:
      return null;
  }
}

/**
 * What to show for a saved Advanced run: each step's events in order (skipped events dropped, and
 * steps left empty with them), the plan, and a stop label. Tool-call events resolve against the
 * message's `toolCalls`. A newer `version` is rendered as far as its known parts go. Null when there
 * is nothing to show, so the bubble can skip the disclosure.
 */
export function agentRunView(run: AgentRun | null | undefined, toolCalls: readonly ToolCall[] = []): AgentRunView | null {
  if (!run || typeof run !== 'object') return null;
  const callsById = new Map(toolCalls.map(call => [call.id, call]));
  const steps: AgentRunStepView[] = [];
  for (const step of Array.isArray(run.steps) ? run.steps : []) {
    if (!step || typeof step.step !== 'number') continue;
    const events = (Array.isArray(step.events) ? step.events : [])
      .map(event => eventView(event, callsById))
      .filter((event): event is AgentRunEventView => event !== null);
    if (events.length > 0) steps.push({ step: step.step, events });
  }
  const plan = normalizePlanItems(run.plan, 'pending');
  const stopLabel = agentRunStopLabel(run.end_reason);
  if (steps.length === 0 && plan.length === 0 && !stopLabel) return null;
  return { steps, plan, stopLabel };
}
