import { AgentJobStatus } from '../../../core/models/agent-job.model';

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
