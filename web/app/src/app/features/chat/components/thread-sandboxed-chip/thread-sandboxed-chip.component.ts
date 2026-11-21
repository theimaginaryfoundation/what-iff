import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

import { ShieldIconComponent } from '../../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';

/**
 * Header shield (icon only, no label) shown only on a sandboxed thread (warning tone); click asks to remove the
 * thread from the sandbox. An ordinary thread shows nothing, and so does an unknown state (`null` / `undefined`).
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
        aria-label="Sandboxed thread. Remove from sandbox."
        uiTooltip="Sandboxed: this thread can't read memories, conversations or files from outside itself. Click to remove it from the sandbox."
        placement="bottom"
        (click)="remove.emit()"
      >
        <ui-shield-icon [size]="14" />
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
        height: 1.75rem;
        justify-content: center;
        padding: 0;
        width: 1.75rem;
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
  /** Asks to take the thread out of its sandbox; the parent confirms. */
  readonly remove = output<void>();
}
