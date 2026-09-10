import { DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, input } from '@angular/core';

import { InputAPICostEstimate } from '../features/chat/helpers/api-pricing.helpers';

/**
 * Mount point for the Context X-ray cost display. The default build renders
 * the estimated input API cost; a private build replaces this file (via the
 * overlay) to render its credit-cost UI instead.
 *
 * The X-ray mounts this outlet whenever it has either a cost estimate or the
 * owning message id, so inputs are optional: the default build renders nothing
 * without a `cost` and ignores `messageId`; the private build keys its
 * per-turn credit lookup off `messageId`.
 */
@Component({
  selector: 'app-context-cost-outlet',
  standalone: true,
  imports: [DecimalPipe],
  template: `
    @if (cost(); as c) {
      <span
        class="gauge__cost"
        [title]="
          'Standard input-token rate checked ' +
          c.pricingCheckedAt +
          '. Excludes output tokens, cached-input discounts, tool fees, batch/priority tiers, regional uplifts, and account-specific pricing.'
        "
        >Est. &#36;{{ c.amountUsd | number: '1.3-3' }}</span
      >
    }
  `,
  styles: [
    `
      .gauge__cost {
        color: var(--color-text-secondary);
        font-size: 0.8rem;
        font-weight: 600;
        margin-left: auto;
      }
    `,
  ],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ContextCostOutletComponent {
  readonly cost = input<InputAPICostEstimate | null>(null);
  /** Assistant message that owns this X-ray. Unused by the default build; the private build looks up its charged credits by it. */
  readonly messageId = input<string | null>(null);
}
