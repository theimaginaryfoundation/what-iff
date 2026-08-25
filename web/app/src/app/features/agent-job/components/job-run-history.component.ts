import { ChangeDetectionStrategy, Component, input } from '@angular/core';

import { AgentJob } from '../../../core/models/agent-job.model';
import { formatDate } from '../helpers/job-vm.helpers';

@Component({
  selector: 'app-job-run-history',
  standalone: true,
  templateUrl: './job-run-history.component.html',
  styleUrl: './job-run-history.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobRunHistoryComponent {
  readonly job = input.required<AgentJob>();

  fmt(value?: string | null): string {
    return formatDate(value);
  }
}
