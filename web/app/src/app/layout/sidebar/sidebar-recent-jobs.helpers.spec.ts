import { AgentJob } from '../../core/models/agent-job.model';
import { SIDEBAR_RECENT_JOBS_LIMIT, pickRecentJobs, toSidebarJobRow } from './sidebar-recent-jobs.helpers';

function job(id: string, over: Partial<AgentJob> = {}): AgentJob {
  return {
    id, user_id: 'u', prompt: 'p', schedule_input: 'daily', schedule_type: 'cron', timezone: 'UTC',
    status: 'active', run_count: 0, created_at: '2026-01-01T00:00:00Z', updated_at: '2026-01-01T00:00:00Z', ...over,
  };
}

describe('sidebar recent jobs helpers', () => {
  it('orders by last run, never-run jobs last, and caps at ten', () => {
    const jobs = [
      job('never-old'),
      job('ran-old', { last_run_at: '2026-02-01T00:00:00Z' }),
      job('never-new', { created_at: '2026-03-01T00:00:00Z' }),
      job('ran-new', { last_run_at: '2026-04-01T00:00:00Z' }),
      ...Array.from({ length: 12 }, (_, i) => job(`x${i}`, { last_run_at: `2025-01-${String(i + 1).padStart(2, '0')}T00:00:00Z` })),
    ];
    const picked = pickRecentJobs(jobs);
    expect(picked).toHaveLength(SIDEBAR_RECENT_JOBS_LIMIT);
    expect(picked.slice(0, 2).map(j => j.id)).toEqual(['ran-new', 'ran-old']);
  });

  it('shows the title with a last-run line, or "Not run yet"', () => {
    const ran = toSidebarJobRow(job('a', { title: 'Weekly digest', last_run_at: new Date(Date.now() - 3 * 3600_000).toISOString() }));
    expect(ran).toMatchObject({ title: 'Weekly digest', lastRun: 'Last run 3h ago' });
    expect(toSidebarJobRow(job('b'))).toMatchObject({ title: 'Untitled job', lastRun: 'Not run yet' });
  });
});
