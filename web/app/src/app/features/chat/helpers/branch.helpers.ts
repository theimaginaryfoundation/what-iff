import { ChatBranchSummary, ChatLineage, ForkChatRequest } from '../../../core/models/chat.model';
import { ChatMessage } from '../../../core/models/message.model';

/** Groups a thread's branches by the message they were taken from (newest branch first). */
export function groupBranchesByMessage(
  branches: readonly ChatBranchSummary[] | null | undefined,
): ReadonlyMap<string, readonly ChatBranchSummary[]> {
  const out = new Map<string, ChatBranchSummary[]>();
  for (const b of branches ?? []) {
    const key = b.forked_from_message_id;
    if (!key) continue;
    const list = out.get(key);
    if (list) list.push(b);
    else out.set(key, [b]);
  }
  for (const list of out.values()) {
    list.sort((a, b) => Date.parse(b.created_at) - Date.parse(a.created_at));
  }
  return out;
}

/** What a branch action on a message does. */
export interface BranchPlan {
  request: ForkChatRequest;
  /**
   * Text to pre-fill in the branch's composer. For a user message the branch ends just before it
   * and the original wording is offered back for rewriting ("what if I'd said…").
   */
  draft: string | null;
}

/**
 * User message → branch *before* it with its text as an editable draft (re-ask differently).
 * Assistant message → branch *through* it and keep going from that reply.
 */
export function branchPlanFor(message: Pick<ChatMessage, 'id' | 'origin' | 'message'>): BranchPlan {
  if (message.origin === 'User') {
    return {
      request: { message_id: message.id, include_message: false },
      draft: message.message?.trim() ? message.message : null,
    };
  }
  return { request: { message_id: message.id, include_message: true }, draft: null };
}

/** Banner copy for a branch thread, or null for an original thread. */
export function branchBannerText(lineage: ChatLineage | null | undefined): string | null {
  const parent = lineage?.parent;
  if (!parent) return null;
  if (parent.deleted || !parent.name?.trim()) return 'Branched from a thread that has since been deleted';
  return `Branched from “${parent.name.trim()}”`;
}
