import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * The kinds of user-owned entity a marketplace extension can publish or keep
 * linked to a published copy. Values are the API's `entity_type` strings.
 */
export type MarketplaceEntityKind = 'personality' | 'skill' | 'mode' | 'connector' | 'job';

/**
 * Mount point for a marketplace extension's status badge on an entity
 * (swap-point file). Empty in the open-source build; a private build replaces
 * this file (via the overlay) to render a compact badge saying how the entity
 * relates to the marketplace (published there, installed from there), with
 * nothing else: managing a listing or an install happens on the extension's
 * own settings tab or in the marketplace itself, not next to the entity.
 *
 * Hosts render it unconditionally, inline with the entity's other badges, on
 * cards, rows, editor headers and detail pages, so the overlay needs no edits
 * to those pages. Each placement is sized like a badge; a replacement must fit
 * that (one badge, no buttons).
 *
 * The stub hides its host so the empty slot adds no gap to a flex row. A
 * replacement sets its own :host display, and should hide the host again when
 * it has nothing to show.
 */
@Component({
  selector: 'app-entity-marketplace-outlet',
  standalone: true,
  template: '',
  styles: [':host { display: none; }'],
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class EntityMarketplaceOutletComponent {
  readonly kind = input.required<MarketplaceEntityKind>();
  readonly entityId = input.required<string>();
}
