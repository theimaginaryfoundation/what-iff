import { of, throwError } from 'rxjs';

import { AgentJob } from '../../../core/models/agent-job.model';
import { Chat } from '../../../core/models/chat.model';
import { ThreadAutomation } from '../../../core/services/thread-automation-source';
import {
  THREAD_JOBS_MAX_PAGES,
  jobDisplayName,
  loadThreadsWithJobs,
  summarizeJobsByChat,
} from './thread-jobs.helpers';

function job(overrides: Partial<AgentJob>): AgentJob {
  return {
    id: 'job',
    user_id: 'user-1',
    prompt: 'Summarize',
    schedule_input: 'every morning',
    schedule_type: 'cron',
    timezone: 'UTC',
    status: 'active',
    run_count: 0,
    created_at: '2026-04-01T00:00:00Z',
    updated_at: '2026-04-01T00:00:00Z',
    ...overrides,
  };
}

function chat(id: string): Chat {
  return { id, user_id: 'user-1', name: id, created_at: '', updated_at: '' };
}

describe('summarizeJobsByChat', () => {
  it('skips jobs without a thread and picks failed, then soonest active, then paused, then complete', () => {
    const summaries = summarizeJobsByChat([
      job({ id: 'complete', chat_id: 'c1', status: 'complete' }),
      job({ id: 'paused', chat_id: 'c1', status: 'paused' }),
      job({ id: 'later', chat_id: 'c1', next_run_at: '2026-10-02T00:00:00Z' }),
      job({ id: 'sooner', chat_id: 'c1', next_run_at: '2026-10-01T00:00:00Z' }),
      job({ id: 'failed', chat_id: 'c1', status: 'failed' }),
      job({ id: 'orphan' }),
    ]);

    expect([...summaries.keys()]).toEqual(['c1']);
    const summary = summaries.get('c1')!;
    expect(summary.primary.id).toBe('failed');
    expect(summary.others.map(j => j.id)).toEqual(['sooner', 'later', 'paused', 'complete']);
  });

  it('names untitled jobs by their schedule', () => {
    expect(jobDisplayName(job({ title: '  ' }))).toBe('every morning');
    expect(jobDisplayName(job({ title: 'Digest' }))).toBe('Digest');
  });
});

describe('loadThreadsWithJobs', () => {
  it('pages through jobs and fetches threads in id chunks of 200', async () => {
    const jobs = Array.from({ length: 250 }, (_, i) => job({ id: `j${i}`, chat_id: `c${i}` }));
    const listAgentJobs = vi.fn((page: number, limit: number) =>
      of({ results: jobs.slice((page - 1) * limit, page * limit), total_count: jobs.length, page }),
    );
    const listAllChats = vi.fn((_limit: number, filters?: { ids?: string }) =>
      of({ chats: (filters?.ids ?? '').split(',').map(chat), truncated: false }),
    );

    const result = await loadThreadsWithJobs({ listAgentJobs, listAllChats }, { search: 'x' });

    expect(listAgentJobs).toHaveBeenCalledTimes(3);
    expect(listAllChats).toHaveBeenCalledTimes(2);
    expect(listAllChats.mock.calls[0][1]).toEqual(expect.objectContaining({ search: 'x' }));
    expect(listAllChats.mock.calls[0][1]!.ids!.split(',').length).toBe(200);
    expect(result.chats.length).toBe(250);
    expect(result.jobsByChatId.size).toBe(250);
    expect(result.truncated).toBe(false);
  });

  it('flags truncation when the job page cap is hit', async () => {
    const listAgentJobs = vi.fn((page: number) =>
      of({ results: [job({ id: `j${page}`, chat_id: `c${page}` })], total_count: 10_000, page }),
    );
    const listAllChats = vi.fn(() => of({ chats: [], truncated: false }));

    const result = await loadThreadsWithJobs({ listAgentJobs, listAllChats }, {});

    expect(listAgentJobs).toHaveBeenCalledTimes(THREAD_JOBS_MAX_PAGES);
    expect(result.truncated).toBe(true);
  });

  it('makes no chat request when no job has a thread', async () => {
    const listAllChats = vi.fn();
    const result = await loadThreadsWithJobs(
      { listAgentJobs: () => of({ results: [job({})], total_count: 1, page: 1 }), listAllChats },
      {},
    );
    expect(listAllChats).not.toHaveBeenCalled();
    expect(result.chats).toEqual([]);
  });

  it('adds threads that only an automation drives, once each, with their automations', async () => {
    const relay = (chatId: string, name: string): ThreadAutomation => ({
      chatId, name, statusText: 'Live', tone: 'success', link: ['/integrations'],
    });
    const listAllChats = vi.fn((_limit: number, filters?: { ids?: string }) =>
      of({ chats: (filters?.ids ?? '').split(',').map(chat), truncated: false }),
    );

    const result = await loadThreadsWithJobs(
      {
        listAgentJobs: () => of({ results: [job({ id: 'j1', chat_id: 'c1' })], total_count: 1, page: 1 }),
        listAllChats,
        listAutomations: () => of([relay('c1', 'a'), relay('c2', 'b'), relay('c2', 'c')]),
      },
      {},
    );

    expect(listAllChats.mock.calls[0][1]!.ids!.split(',').sort()).toEqual(['c1', 'c2']);
    expect(result.chats.map((c) => c.id).sort()).toEqual(['c1', 'c2']);
    expect(result.jobsByChatId.has('c2')).toBe(false);
    expect(result.automationsByChatId.get('c1')!.map((a) => a.name)).toEqual(['a']);
    expect(result.automationsByChatId.get('c2')!.map((a) => a.name)).toEqual(['b', 'c']);
  });

  it('keeps the agent-job threads when the automation source fails', async () => {
    const result = await loadThreadsWithJobs(
      {
        listAgentJobs: () => of({ results: [job({ id: 'j1', chat_id: 'c1' })], total_count: 1, page: 1 }),
        listAllChats: (_limit, filters) => of({ chats: (filters?.ids ?? '').split(',').map(chat), truncated: false }),
        listAutomations: () => throwError(() => new Error('down')),
      },
      {},
    );

    expect(result.chats.map((c) => c.id)).toEqual(['c1']);
    expect(result.automationsByChatId.size).toBe(0);
  });
});
