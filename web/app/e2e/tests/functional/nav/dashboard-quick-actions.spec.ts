import type { Page } from '@playwright/test';

import { test, expect } from '../../../fixtures';
import { dashboardTiles, type DashboardTile } from '../../../poms';

/**
 * The three Quick Actions tiles on `/dashboard` must route client-side.
 *
 * They shipped as bare `href`s, which made each one a full document reload of a
 * single-page app: the router, the app state and a second of load time thrown
 * away on the main navigation path. The unit spec pins the `routerLink`
 * directive and the cancelled click; this pins the thing only a real browser can
 * show, which is that the document survives the navigation.
 */

const tiles = Object.keys(dashboardTiles) as DashboardTile[];

/** Marks the live document so a replacement is detectable after the click. */
const stampDocument = async (page: Page): Promise<void> => {
  await page.evaluate(() => {
    (window as unknown as { __navSentinel?: number }).__navSentinel = 1;
  });
};

const sentinelSurvived = (page: Page): Promise<boolean> =>
  page.evaluate(() => (window as unknown as { __navSentinel?: number }).__navSentinel === 1);

for (const tile of tiles) {
  const { name, route } = dashboardTiles[tile];

  test(`the ${name} tile routes to ${route} without reloading`, async ({
    dashboardPage,
    userWithPersonality,
  }) => {
    const page = userWithPersonality.page;
    await dashboardPage.navigateTo();
    await expect(dashboardPage.heading).toBeVisible();

    await stampDocument(page);
    await dashboardPage.tile(tile).click();

    await expect(page).toHaveURL(new RegExp(`${route}(/|\\?|$)`));
    expect(await sentinelSurvived(page), 'document survived the tile click').toBe(true);
  });

  test(`the ${name} tile exposes ${route} as a real link`, async ({
    dashboardPage,
    userWithPersonality,
  }) => {
    await dashboardPage.navigateTo();

    // `routerLink` writes the href as well as handling the click. Without it,
    // middle-click and open-in-new-tab would silently do nothing — a failure no
    // click-driven assertion above can see.
    await expect(dashboardPage.tile(tile)).toHaveAttribute('href', route);
  });
}
