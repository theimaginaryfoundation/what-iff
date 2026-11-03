import { jobRunHref, jobRunRef } from './job-run.helpers';

describe('job-run helpers', () => {
  const JOB_ID = '33333333-3333-4333-8333-333333333333';
  const input = JSON.stringify({ agent_job_id: JOB_ID, title: ' Weekly meal planning ' });

  it("reads the job and its prompt from a saved reply's run_job call", () => {
    expect(
      jobRunRef([
        { tool_name: 'find_context', tool_input: '{}' },
        { tool_name: 'run_job', tool_input: input, tool_output: " Plan this week's meals. " },
      ]),
    ).toEqual({ agentJobId: JOB_ID, title: 'Weekly meal planning', prompt: "Plan this week's meals." });
  });

  it('reads the job and its prompt from a live run_job row', () => {
    expect(jobRunRef([{ name: 'run_job', input, output: 'Plan.' }])).toEqual({
      agentJobId: JOB_ID,
      title: 'Weekly meal planning',
      prompt: 'Plan.',
    });
  });

  it('is null without a run_job call, and tolerates a call that names no job', () => {
    expect(jobRunRef(undefined)).toBeNull();
    expect(jobRunRef([{ tool_name: 'load_memory' }])).toBeNull();
    expect(jobRunRef([{ tool_name: 'run_job', tool_input: 'not json' }])).toEqual({ agentJobId: null, title: '', prompt: '' });
  });

  it('links to the job', () => {
    expect(jobRunHref({ agentJobId: JOB_ID, title: 'x', prompt: '' })).toBe(`/agent-jobs/${JOB_ID}`);
    expect(jobRunHref({ agentJobId: null, title: 'x', prompt: '' })).toBeNull();
  });
});
