import { AgentJob, AgentJobStatus } from '../../../core/models/agent-job.model';
import { JobStatusTone, jobStatusDescription, jobStatusLabel, jobStatusTone } from './job-status.helpers';

export interface JobCardVm {
  id: string;
  title: string;
  promptExcerpt: string;
  scheduleTypeLabel: string;
  scheduleValue: string;
  status: AgentJobStatus;
  statusLabel: string;
  statusDescription: string;
  statusTone: JobStatusTone;
  timezone: string;
  nextRunLabel: string;
  lastRunLabel: string;
  runCount: number;
  canRunNow: boolean;
  canPauseResume: boolean;
}

export function toJobCardVm(job: AgentJob): JobCardVm {
  const scheduleTypeLabel = job.schedule_type === 'cron' ? 'Recurring' : 'One-off';
  const scheduleValue = job.schedule_type === 'cron' ? (job.schedule || job.schedule_input) : (job.run_at || job.schedule_input);

  return {
    id: job.id,
    title: (job.title || 'Untitled job').trim(),
    promptExcerpt: excerpt(job.prompt, 120),
    scheduleTypeLabel,
    scheduleValue,
    status: job.status,
    statusLabel: jobStatusLabel(job),
    statusDescription: jobStatusDescription(job),
    statusTone: jobStatusTone(job),
    timezone: job.timezone,
    nextRunLabel: formatDate(job.next_run_at),
    lastRunLabel: formatDate(job.last_run_at),
    runCount: job.run_count,
    canRunNow: canRunJobNow(job),
    canPauseResume: job.status === 'active' || (job.status === 'paused' && !hasMissingParameters(job)),
  };
}

/** A job with a parameter that has no value can't run, be resumed or be run by hand (ADR 0x044). */
export function hasMissingParameters(job: Pick<AgentJob, 'missing_parameters'>): boolean {
  return (job.missing_parameters?.length ?? 0) > 0;
}

/** Run now is offered for active and paused jobs whose parameters all have values. */
export function canRunJobNow(job: Pick<AgentJob, 'status' | 'missing_parameters'>): boolean {
  return (job.status === 'active' || job.status === 'paused') && !hasMissingParameters(job);
}

export function formatDate(raw?: string | null): string {
  if (!raw) return '—';
  const date = new Date(raw);
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleString();
}

export function excerpt(value: string, maxLen: number): string {
  const normalized = (value || '').trim();
  if (normalized.length <= maxLen) {
    return normalized;
  }
  return `${normalized.slice(0, maxLen - 1).trimEnd()}…`;
}
