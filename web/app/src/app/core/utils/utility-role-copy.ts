import { UtilityRole } from '../models/utility-model.model';

/**
 * What each utility role is called and covers, in the words a person would use, and what stops
 * when it is off or has no model.
 */
export const UTILITY_ROLE_COPY: Record<UtilityRole, { label: string; covers: string; stops: string }> = {
  memory: {
    label: 'Memory and summaries',
    covers: 'Saves memories, keeps the scratchpad and summarizes long threads',
    stops: "New chats don't add to memory or the scratchpad, and long threads aren't summarized. Existing memories are still used.",
  },
  memory_merge: {
    label: 'Memory tidying',
    covers: 'Folds duplicate memories together',
    stops: 'Only memories that match exactly are folded together.',
  },
  simple: {
    label: 'Simple tasks',
    covers: 'Names threads, picks expressions and moods, finds memories for replies and reads job schedules',
    stops: "Threads aren't named, expressions and moods don't change on their own, replies don't draw on memories, and job schedules can't be read.",
  },
  personality: {
    label: 'Personality generation',
    covers: 'Writes a new personality from your answers',
    stops: "Personalities can't be generated; you can still write one yourself.",
  },
};

/**
 * What to tell a person whose action needed background work that its role keeps from running
 * (codes `utility_model_not_set` and `utility_role_off`), or null for any other code. Where the
 * account chooses, the fix is in Settings; elsewhere the server chose, and there is nothing to fix.
 */
export function utilityRoleUnavailableMessage(code: string | null | undefined, role: UtilityRole, accountsChoose: boolean): string | null {
  const label = UTILITY_ROLE_COPY[role].label;
  if (code === 'utility_model_not_set') {
    return accountsChoose
      ? `No model is chosen for ${label}. Choose one in Settings, under Background models.`
      : `This server hasn't chosen a model for ${label}.`;
  }
  if (code === 'utility_role_off') {
    return accountsChoose ? `${label} is switched off. Choose a model for it in Settings, under Background models.` : `${label} is switched off on this server.`;
  }
  return null;
}
