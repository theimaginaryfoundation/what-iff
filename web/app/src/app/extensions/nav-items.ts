import type { NavItem } from '../layout/sidebar/nav.helpers';

/**
 * Extra config-mode sidebar entries (swap-point file).
 *
 * The open-source build contributes none. A private build replaces this file
 * to append its own sections (e.g. a marketplace page) after the base config
 * items; `layout/sidebar/nav.helpers.ts` reads this list in `configNavItems()`,
 * so the sidebar itself needs no edits. Routes named here must also exist in
 * `extensions/private-routes.ts`.
 *
 * The import is type-only so this file and nav.helpers.ts form no runtime cycle.
 */
export const extraConfigNavItems: ReadonlyArray<NavItem> = [];
