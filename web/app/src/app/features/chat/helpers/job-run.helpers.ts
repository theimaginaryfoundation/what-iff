/**
 * A scheduled job's reply opens with a display-only `run_job` tool call (ADR 0x040): its input
 * names the job (`agent_job_id`, `title`) and its output is the job's prompt. These helpers read
 * it, from a saved reply's tool calls or a live turn's timeline, so the chat can mark job turns
 * and link them to their job.
 */
import { parseToolPayload } from './tool-call-format.helpers';

export const JOB_RUN_TOOL_NAME = 'run_job';

/** The job a turn ran for. `agentJobId` is null when the call names none. */
export interface JobRunRef {
  agentJobId: string | null;
  title: string;
}

/** The job named by the first run_job call among `calls` (saved or live shape), or null. */
export function jobRunRef(
  calls: readonly { tool_name?: string; name?: string; tool_input?: string; input?: string }[] | null | undefined,
): JobRunRef | null {
  const call = calls?.find(c => (c.tool_name ?? c.name) === JOB_RUN_TOOL_NAME);
  if (!call) return null;
  const input = parseToolPayload(call.tool_input ?? call.input);
  const record = typeof input === 'object' && input !== null && !Array.isArray(input) ? (input as Record<string, unknown>) : {};
  const id = typeof record['agent_job_id'] === 'string' ? record['agent_job_id'].trim() : '';
  const title = typeof record['title'] === 'string' ? record['title'].trim() : '';
  return { agentJobId: id || null, title };
}

/** Whether a tool call is a job's run_job call. */
export function isJobRunToolCall(call: { tool_name?: string }): boolean {
  return call.tool_name === JOB_RUN_TOOL_NAME;
}

/** The job page for a ref, or null when it names no job. */
export function jobRunHref(ref: JobRunRef): string | null {
  return ref.agentJobId ? `/agent-jobs/${encodeURIComponent(ref.agentJobId)}` : null;
}

/** "the job “Weekly meal planning”", or "a scheduled job" when it has no title. */
export function jobRunName(ref: JobRunRef): string {
  return ref.title ? `the job “${ref.title}”` : 'a scheduled job';
}
