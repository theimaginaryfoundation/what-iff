import { DecimalPipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, input } from '@angular/core';

import { InputAPICostEstimate } from '../features/chat/helpers/api-pricing.helpers';

/**
 * Mount point for the Context X-ray cost display. The default build renders
 * the estimated input API cost; a private build replaces this file (via the
 * overlay) to render its credit-cost UI instead. The Context X-ray renders
 * this outlet unconditionally whenever a cost estimate is available.
 */
@Component({
  selector: 'app-context-cost-outlet',
  standalone: true,
  imports: [DecimalPipe],
  template: `
    <span
      class="gauge__cost"
      [title]="
        'Standard input-token rate checked ' +
        cost().pricingCheckedAt +
        '. Excludes output tokens, cached-input discounts, tool fees, batch/priority tiers, regional uplifts, and account-specific pricing.'
      "
      >Est. &#36;{{ cost().amountUsd | number: '1.3-3' }}</span
    >
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
  readonly cost = input.required<InputAPICostEstimate>();
}
