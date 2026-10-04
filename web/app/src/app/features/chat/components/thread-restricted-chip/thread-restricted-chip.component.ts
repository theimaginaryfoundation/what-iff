import { ChangeDetectionStrategy, Component, computed, input, output } from '@angular/core';

import { MemorySensitivity } from '../../../../core/models/memory.model';
import { ShieldIconComponent } from '../../../../shared/ui/icons/icons';
import { TooltipDirective } from '../../../../shared/ui/tooltip/tooltip.directive';
import { isMemoryRestricted, memoryAccessLabel, restrictedTooltip } from '../../helpers/memory-access.helpers';

/** Header indicator shown only on a restricted thread (memory limit below Everything). Click opens memory access. */
@Component({
  selector: 'app-thread-restricted-chip',
  standalone: true,
  imports: [ShieldIconComponent, TooltipDirective],
  template: `
    @if (restricted()) {
      <button
        type="button"
        class="chip"
        [attr.aria-label]="'Restricted thread. Memory access: ' + accessLabel() + '. Open memory access settings.'"
        [uiTooltip]="tooltip()"
        placement="bottom"
        (click)="openSettings.emit()"
      >
        <ui-shield-icon [size]="12" />
        <span>Restricted</span>
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
export class ThreadRestrictedChipComponent {
  readonly limit = input<MemorySensitivity | null | undefined>(null);
  readonly openSettings = output<void>();

  readonly restricted = computed(() => isMemoryRestricted(this.limit()));
  readonly accessLabel = computed(() => memoryAccessLabel(this.limit()));
  readonly tooltip = computed(() => restrictedTooltip(this.limit()));
}
