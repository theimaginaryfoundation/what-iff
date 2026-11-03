import { ChangeDetectionStrategy, Component, input } from '@angular/core';
import type { MarketplaceEntityKind } from './entity-marketplace-outlet.component';

/**
 * Mount point for a marketplace extension's "browse the marketplace" link on
 * the list pages (swap-point file). Empty in the open-source build; a private
 * build replaces this file (via the overlay) to render a link into its
 * marketplace, filtered to `kind`, so people discover they can install
 * personalities, skills, modes, connectors and jobs instead of building them.
 *
 * Hosts render it unconditionally next to the page title of the personalities,
 * skills, modes, connectors and jobs pages (`variant="button"`, a secondary button)
 * and inside the first-run welcome card on the personalities page
 * (`variant="welcome"`, a button in that card's own row of start actions).
 *
 * The stub hides its host so the empty slot adds no gap to a flex row. A
 * replacement must set its own :host display.
 */
@Component({
  selector: 'app-marketplace-browse-link',
  standalone: true,
  template: '',
  styles: [':host { display: none; }'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class MarketplaceBrowseLinkComponent {
  readonly kind = input.required<MarketplaceEntityKind>();
  readonly variant = input<'button' | 'welcome'>('button');
}
