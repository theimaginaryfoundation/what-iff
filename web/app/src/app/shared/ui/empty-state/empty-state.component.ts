import { ChangeDetectionStrategy, Component, input } from '@angular/core';

export type EmptyStateHeadingLevel = 1 | 2 | 3;

@Component({
  selector: 'ui-empty-state',
  standalone: true,
  templateUrl: './empty-state.component.html',
  styleUrl: './empty-state.component.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class EmptyStateComponent {
  readonly heading = input.required<string>();
  readonly body = input<string | null | undefined>(undefined);
  readonly level = input<EmptyStateHeadingLevel>(2);
}
