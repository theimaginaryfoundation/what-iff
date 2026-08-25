import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

@Component({
  selector: 'app-create-job-modal',
  standalone: true,
  templateUrl: './create-job-modal.component.html',
  styleUrl: './create-job-modal.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CreateJobModalComponent {
  readonly open = input(false);
  readonly close = output<void>();
}
