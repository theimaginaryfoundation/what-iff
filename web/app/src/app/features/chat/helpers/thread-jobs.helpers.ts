import { Observable, firstValueFrom } from 'rxjs';

import { AgentJob, AgentJobStatus } from '../../../core/models/agent-job.model';
import { Chat, ChatFilters } from '../../../core/models/chat.model';
import { PaginatedResponse } from '../../../core/models/common.model';

/** Page size for the agent-job list (the API's maximum). */
export const THREAD_JOBS_PAGE_SIZE = 100;
/** Upper bound on job pages fetched for the Jobs tab (1000 jobs). */
export const THREAD_JOBS_MAX_PAGES = 10;
/** `GET /chat?ids=` accepts at most this many ids per request. */
export const THREAD_JOBS_IDS_PER_REQUEST = 200;

/**
 * The scheduled jobs attached to one thread, with the one to show first.
 * `others` keeps the rest in the same priority order, for the "+N more" hint.
 */
export interface ThreadJobSummary {
  primary: AgentJob;
  others: AgentJob[];
}

export interface ThreadsWithJobs {
  chats: Chat[];
  jobsByChatId: ReadonlyMap<string, ThreadJobSummary>;
  /** True when the job or chat lists were cut off by the caps above. */
  truncated: boolean;
}

/** What the loader needs from AgentJobService and ChatService. */
export interface ThreadJobsSources {
  listAgentJobs(page: number, limit: number): Observable<PaginatedResponse<AgentJob>>;
  listAllChats(limitPerPage: number, filters?: ChatFilters): Observable<{ chats: Chat[]; truncated: boolean }>;
}

/**
 * Failed jobs need attention, so they lead; then jobs that will still run, then paused ones,
 * then finished one-offs.
 */
const STATUS_PRIORITY: Record<AgentJobStatus, number> = {
  failed: 0,
  active: 1,
  paused: 2,
  complete: 3,
};

function timeOrNull(value?: string | null): number | null {
  if (!value) return null;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? null : parsed;
}

/** Orders jobs on one thread: status priority, then soonest next run, then most recent last run. */
export function compareThreadJobs(a: AgentJob, b: AgentJob): number {
  const byStatus = (STATUS_PRIORITY[a.status] ?? 9) - (STATUS_PRIORITY[b.status] ?? 9);
  if (byStatus !== 0) return byStatus;
  const nextA = timeOrNull(a.next_run_at);
  const nextB = timeOrNull(b.next_run_at);
  if (nextA !== nextB) {
    if (nextA === null) return 1;
    if (nextB === null) return -1;
    return nextA - nextB;
  }
  return (timeOrNull(b.last_run_at) ?? 0) - (timeOrNull(a.last_run_at) ?? 0);
}

/**
 * Groups jobs by the thread they post into. Jobs with no thread yet (never run) are
 * skipped: the scheduler creates and attaches a thread on the first run.
 */
export function summarizeJobsByChat(jobs: readonly AgentJob[]): Map<string, ThreadJobSummary> {
  const grouped = new Map<string, AgentJob[]>();
  for (const job of jobs) {
    if (!job.chat_id) continue;
    const list = grouped.get(job.chat_id) ?? [];
    list.push(job);
    grouped.set(job.chat_id, list);
  }
  const summaries = new Map<string, ThreadJobSummary>();
  for (const [chatId, list] of grouped) {
    const [primary, ...others] = [...list].sort(compareThreadJobs);
    summaries.set(chatId, { primary, others });
  }
  return summaries;
}

export function jobDisplayName(job: AgentJob): string {
  return job.title?.trim() || job.schedule_input?.trim() || 'Untitled job';
}

/**
 * Loads every thread that has a scheduled job, active or archived, with its job summary.
 * Uses the existing APIs: the agent-job list gives the chat ids, then `GET /chat?ids=`
 * (which ignores archive state) fetches those threads with the Thread Manager's filters.
 */
export async function loadThreadsWithJobs(
  sources: ThreadJobsSources,
  filters: Omit<ChatFilters, 'ids' | 'archived'>,
): Promise<ThreadsWithJobs> {
  const jobs: AgentJob[] = [];
  let truncated = false;
  for (let page = 1; ; page++) {
    const response = await firstValueFrom(sources.listAgentJobs(page, THREAD_JOBS_PAGE_SIZE));
    jobs.push(...response.results);
    const done = response.results.length === 0 || jobs.length >= response.total_count;
    if (done) break;
    if (page >= THREAD_JOBS_MAX_PAGES) {
      truncated = true;
      break;
    }
  }

  const jobsByChatId = summarizeJobsByChat(jobs);
  const ids = [...jobsByChatId.keys()];
  const chats: Chat[] = [];
  for (let i = 0; i < ids.length; i += THREAD_JOBS_IDS_PER_REQUEST) {
    const chunk = ids.slice(i, i + THREAD_JOBS_IDS_PER_REQUEST);
    const result = await firstValueFrom(
      sources.listAllChats(THREAD_JOBS_IDS_PER_REQUEST, { ...filters, ids: chunk.join(',') }),
    );
    chats.push(...result.chats);
    truncated ||= result.truncated;
  }
  return { chats, jobsByChatId, truncated };
}
