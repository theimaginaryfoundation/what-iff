import { ChangeDetectionStrategy, Component, input } from '@angular/core';

/**
 * The kinds of user-owned entity a marketplace extension can publish or keep
 * linked to a published copy. Values are the API's `entity_type` strings.
 */
export type MarketplaceEntityKind = 'personality' | 'skill' | 'mode' | 'connector';

/**
 * Mount point for a marketplace / publishing extension on an entity's own page
 * (swap-point file). Empty in the open-source build; a private build replaces
 * this file (via the overlay) to render publish, update and provenance controls
 * for the entity the host page is showing (e.g. a Nexus listing badge).
 *
 * Hosts render it unconditionally with the entity's kind, id and display name,
 * so the overlay needs no edits to the personality, skill, mode or connector
 * pages. `layout` tells a replacement how it is embedded: `actions` sits in a
 * row of inline buttons; `card` is its own bordered section in a sidebar.
 *
 * The stub hides its host so the empty slot adds no gap to a flex row. A
 * replacement must set its own :host display.
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
  /** Display name of the entity, used to prefill a listing title. */
  readonly entityName = input<string>('');
  readonly layout = input<'actions' | 'card'>('actions');
}
