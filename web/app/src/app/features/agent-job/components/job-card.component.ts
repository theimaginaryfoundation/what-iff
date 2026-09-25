import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';
import { JobCardVm } from '../helpers/job-vm.helpers';

@Component({
  selector: 'app-job-card',
  standalone: true,
  imports: [TooltipDirective],
  templateUrl: './job-card.component.html',
  styleUrl: './job-card.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class JobCardComponent {
  readonly job = input.required<JobCardVm>();
  readonly runningNow = input(false);

  readonly open = output<string>();
  readonly runNow = output<string>();
  readonly togglePauseResume = output<{ id: string; nextStatus: 'active' | 'paused' }>();
  readonly delete = output<string>();
}
