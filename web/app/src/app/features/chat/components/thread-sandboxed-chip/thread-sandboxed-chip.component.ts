import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { ShieldIconComponent } from '../../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';

/** Header indicator shown only on a sandboxed thread. Click opens the sandbox setting. */
@Component({
  selector: 'app-thread-sandboxed-chip',
  standalone: true,
  imports: [ShieldIconComponent, TooltipDirective],
  template: `
    @if (sandboxed()) {
      <button
        type="button"
        class="chip"
        aria-label="Sandboxed thread. Open sandbox settings."
        uiTooltip="Sandboxed: this thread can't read memories, conversations or files from outside itself."
        placement="bottom"
        (click)="openSettings.emit()"
      >
        <ui-shield-icon [size]="12" />
        <span>Sandboxed</span>
      </button>
    }
  `,
  styles: [
    `
      :host {
        display: inline-flex;
      }

      .chip {
        align-items: center;
        background: color-mix(in srgb, var(--color-warning) 10%, transparent);
        border: 1px solid color-mix(in srgb, var(--color-warning) 40%, var(--color-border-base));
        border-radius: 999px;
        color: var(--color-warning);
        cursor: pointer;
        display: inline-flex;
        font-size: 0.6875rem;
        font-weight: 600;
        gap: 0.25rem;
        padding: 0.1875rem 0.5rem;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadSandboxedChipComponent {
  readonly sandboxed = input<boolean | null | undefined>(false);
  readonly openSettings = output<void>();
}
