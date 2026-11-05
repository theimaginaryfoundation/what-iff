import { AgentJob } from '../../core/models/agent-job.model';
import { relativeTimeCompact } from '../../features/chat/components/chat-import-modal/chat-import-modal.utils';
import { excerpt } from '../../features/agent-job/helpers/job-vm.helpers';

/** How many jobs the sidebar's "Recent jobs" list shows. */
export const SIDEBAR_RECENT_JOBS_LIMIT = 10;

export interface SidebarJobRow {
  id: string;
  personalityId: string | null;
  title: string;
  /** e.g. "Last run 3h ago", or "Not run yet". */
  lastRun: string;
}

/** Jobs that ran most recently first; jobs that never ran go last (newest first), capped to `limit`. */
export function pickRecentJobs(jobs: readonly AgentJob[], limit: number = SIDEBAR_RECENT_JOBS_LIMIT): AgentJob[] {
  const stamp = (value?: string | null): number => {
    const time = value ? new Date(value).getTime() : NaN;
    return Number.isNaN(time) ? 0 : time;
  };
  return [...jobs]
    .sort((a, b) => {
      const ran = stamp(b.last_run_at) - stamp(a.last_run_at);
      return ran !== 0 ? ran : stamp(b.created_at) - stamp(a.created_at);
    })
    .slice(0, limit);
}

export function toSidebarJobRow(job: AgentJob): SidebarJobRow {
  const ago = relativeTimeCompact(job.last_run_at ?? undefined);
  return {
    id: job.id,
    personalityId: job.personality_id ?? null,
    title: excerpt((job.title || 'Untitled job').trim(), 60),
    lastRun: ago ? `Last run ${ago}` : 'Not run yet',
  };
}
