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
