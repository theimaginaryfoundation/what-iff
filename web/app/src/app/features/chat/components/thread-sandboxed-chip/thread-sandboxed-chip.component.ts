import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { ShieldIconComponent } from '../../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';

/**
 * Header indicator for a thread's privacy state, shown on every open thread so it is never
 * hidden: "Sandboxed" (warning tone) or "Not sandboxed" (muted). Click opens the sandbox setting.
 * `null` / `undefined` means the state is not known yet, so nothing is shown.
 */
@Component({
  selector: 'app-thread-sandboxed-chip',
  standalone: true,
  imports: [ShieldIconComponent, TooltipDirective],
  template: `
    @if (sandboxed() === true) {
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
    } @else if (sandboxed() === false) {
      <button
        type="button"
        class="chip chip--open"
        aria-label="Not sandboxed. This thread can use your memories, conversations and files. Open sandbox settings."
        uiTooltip="Not sandboxed: this thread can use your memories, other conversations and account files."
        placement="bottom"
        (click)="openSettings.emit()"
      >
        <ui-shield-icon [size]="12" />
        <span>Not sandboxed</span>
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

      .chip--open {
        background: transparent;
        border-color: var(--color-border-base);
        color: var(--color-text-secondary);
      }

      .chip:focus-visible {
        outline: 2px solid var(--color-accent);
        outline-offset: 2px;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ThreadSandboxedChipComponent {
  readonly sandboxed = input<boolean | null | undefined>(null);
  readonly openSettings = output<void>();
}
