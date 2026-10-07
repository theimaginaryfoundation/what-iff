import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

import { MemoryProvenance } from '../../../core/models/memory.model';
import { TooltipDirective } from '../../../shared/ui/tooltip/tooltip.directive';

/**
 * "External" pill for a memory learned from other people in a Discord relay thread
 * (unverified), naming who it came from when known. Renders nothing for your own memories.
 */
@Component({
  selector: 'app-memory-provenance-badge',
  standalone: true,
  imports: [TooltipDirective],
  template: `
    @if (external()) {
      <span class="provenance-badge" data-provenance="external" [uiTooltip]="hint()">{{ label() }}</span>
    }
  `,
  styles: [
    `
      :host {
        display: inline-flex;
      }

      .provenance-badge {
        --provenance-color: var(--color-warning);
        background: color-mix(in srgb, var(--provenance-color) 10%, transparent);
        border: 1px dashed color-mix(in srgb, var(--provenance-color) 55%, var(--color-border-base));
        border-radius: 999px;
        color: var(--provenance-color);
        font-size: 0.6875rem;
        font-weight: 600;
        line-height: 1.2;
        max-width: 14rem;
        overflow: hidden;
        padding: 0.05rem 0.45rem;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MemoryProvenanceBadgeComponent {
  readonly provenance = input<MemoryProvenance | null | undefined>(null);
  readonly speaker = input<string | null | undefined>(null);

  readonly external = computed(() => this.provenance() === 'external');
  readonly label = computed(() => (this.speaker() ? `External · ${this.speaker()}` : 'External'));
  readonly hint = computed(() => {
    const who = this.speaker() ? `${this.speaker()}, ` : 'someone ';
    return `Learned from ${who}in a Discord thread, not from you. Unverified: the persona treats it that way too.`;
  });
}
