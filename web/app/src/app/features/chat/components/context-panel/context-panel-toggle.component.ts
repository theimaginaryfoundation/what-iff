import { ChangeDetectionStrategy, Component, output } from '@angular/core';
import { AdjustmentsHorizontalIconComponent } from '../../../../shared/ui/icons/icons';

@Component({
  selector: 'app-context-panel-toggle',
  standalone: true,
  imports: [AdjustmentsHorizontalIconComponent],
  template: `
    <button
      type="button"
      class="context-toggle"
      aria-label="Open conversation context panel"
      (click)="open.emit()"
    >
      <ui-adjustments-horizontal-icon [size]="14" />
    </button>
  `,
  styles: [`
    .context-toggle {
      align-items: center;
      background: transparent;
      border: 1px solid var(--color-border-base);
      border-radius: 0.625rem;
      color: var(--color-text-muted);
      display: inline-flex;
      height: 1.875rem;
      justify-content: center;
      padding: 0;
      width: 1.875rem;
    }
  `],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextPanelToggleComponent {
  readonly open = output<void>();
}
