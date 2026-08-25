import { ChangeDetectionStrategy, Component, input } from '@angular/core';

@Component({
  selector: 'ui-spinner',
  standalone: true,
  templateUrl: './spinner.component.html',
  styleUrl: './spinner.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SpinnerComponent {
  readonly size = input(16);
  readonly ariaLabel = input('Loading');
}
