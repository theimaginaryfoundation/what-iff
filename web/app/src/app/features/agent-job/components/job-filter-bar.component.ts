
import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';
import { FormsModule } from '@angular/forms';

import { AgentJobScheduleType, AgentJobStatus } from '../../../core/models/agent-job.model';
import { JobViewFilters } from '../../../core/services/job-view.service';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';

@Component({
  selector: 'app-job-filter-bar',
  standalone: true,
  imports: [FormsModule, TooltipDirective],
  templateUrl: './job-filter-bar.component.html',
  styleUrl: './job-filter-bar.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobFilterBarComponent {
  readonly filters = input.required<JobViewFilters>();
  readonly loading = input(false);

  readonly filtersChanged = output<Partial<JobViewFilters>>();
  readonly clear = output<void>();
  readonly createRequested = output<void>();

  readonly statuses: ReadonlyArray<{ value: AgentJobStatus | ''; label: string }> = [
    { value: '', label: 'All statuses' },
    { value: 'active', label: 'Active' },
    { value: 'paused', label: 'Paused' },
    { value: 'complete', label: 'Complete' },
    { value: 'failed', label: 'Failed' },
  ];

  readonly scheduleTypes: ReadonlyArray<{ value: AgentJobScheduleType | ''; label: string }> = [
    { value: '', label: 'All schedule types' },
    { value: 'at', label: 'One-off' },
    { value: 'cron', label: 'Recurring' },
  ];
}
