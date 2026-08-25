import { Routes } from '@angular/router';

/**
 * Extension point for privately-maintained routes (swap-point file).
 *
 * The open-source build ships this empty array, so no private feature routes
 * exist. The private overlay replaces this file with one that lazy-loads its
 * own components (billing/subscription/usage, etc.). `app.routes.ts` spreads
 * these into the authenticated AppLayout children, so the public app never
 * imports a feature that isn't in its tree.
 */
export const privateChildRoutes: Routes = [];
