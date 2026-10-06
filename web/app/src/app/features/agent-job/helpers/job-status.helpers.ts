import { AgentJob, AgentJobStatus } from '../../../core/models/agent-job.model';

export type JobStatusTone = 'success' | 'warning' | 'danger' | 'neutral';

export function statusLabel(status: AgentJobStatus): string {
  switch (status) {
    case 'active':
      return 'Active';
    case 'paused':
      return 'Paused';
    case 'failed':
      return 'Failed';
    case 'complete':
      return 'Complete';
    default:
      return status;
  }
}

/** Short explanation of what a status means for scheduling, for the status badge tooltip. */
export function statusDescription(status: AgentJobStatus): string {
  switch (status) {
    case 'active':
      return 'Runs on its schedule';
    case 'paused':
      return 'Scheduled runs are skipped until you resume it';
    case 'failed':
      return 'This one-off job ran but hit an error';
    case 'complete':
      return 'This one-off job has run and won\'t run again';
    default:
      return '';
  }
}

export function statusTone(status: AgentJobStatus): JobStatusTone {
  switch (status) {
    case 'active':
      return 'success';
    case 'paused':
      return 'warning';
    case 'failed':
      return 'danger';
    case 'complete':
    default:
      return 'neutral';
  }
}

export function isTerminalStatus(status: AgentJobStatus): boolean {
  return status === 'complete' || status === 'failed';
}

/**
 * Prefix the scheduler writes into `last_error` when it pauses a recurring job because it
 * couldn't compute the next run (`ScheduleErrorMarker` in internal/agentjobs/scheduler/job.go).
 * A user pause always clears `last_error`, so this is what tells the two kinds of pause apart.
 */
const SCHEDULE_ERROR_MARKER = 'failed to compute next_run_at:';

export type JobStatusFields = Pick<AgentJob, 'status' | 'schedule_type' | 'last_error'>;

/**
 * Why the scheduler paused a recurring job, or null when the job isn't in that state (including
 * jobs the user paused).
 */
export function scheduleFailureReason(job: JobStatusFields): string | null {
  if (job.status !== 'paused' || job.schedule_type !== 'cron' || !job.last_error) return null;
  const at = job.last_error.indexOf(SCHEDULE_ERROR_MARKER);
  if (at < 0) return null;
  return job.last_error.slice(at + SCHEDULE_ERROR_MARKER.length).trim() || 'unknown error';
}

/** Badge text for a job; a scheduler-paused recurring job reads differently from a user pause. */
export function jobStatusLabel(job: JobStatusFields): string {
  return scheduleFailureReason(job) === null ? statusLabel(job.status) : 'Paused: schedule error';
}

/** Tooltip for the status badge, including the reason when the scheduler paused the job. */
export function jobStatusDescription(job: JobStatusFields): string {
  const reason = scheduleFailureReason(job);
  return reason === null
    ? statusDescription(job.status)
    : `Paused: couldn't schedule the next run (${reason}). Fix the schedule, then resume it`;
}

export function jobStatusTone(job: JobStatusFields): JobStatusTone {
  return scheduleFailureReason(job) === null ? statusTone(job.status) : 'danger';
}
