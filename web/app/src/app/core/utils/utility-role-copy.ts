import { UtilityRole } from '../models/utility-model.model';

/** What each utility role is called and covers, in the words a person would use. */
export const UTILITY_ROLE_COPY: Record<UtilityRole, { label: string; covers: string }> = {
  memory: { label: 'Memory and summaries', covers: 'Saves memories, keeps the scratchpad and summarizes long threads' },
  quick: { label: 'Quick tasks', covers: 'Names threads, picks expressions and moods, and reads job schedules' },
  personality: { label: 'Personality generation', covers: 'Writes a new personality from your answers' },
};
