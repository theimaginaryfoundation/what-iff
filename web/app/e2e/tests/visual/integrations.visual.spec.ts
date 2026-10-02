import { test, expect } from '../../fixtures';
import { commonMasks } from './visual.helpers';

/**
 * @functional-coverage tests/functional/integrations/integrations.spec.ts
 *
 * The subscription gate, tab switching, and the webhook token lifecycle are
 * covered behaviourally by the functional integrations spec.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

/**
 * The connectors tab's three-column grid (`lg:grid-cols-3` with a spanning
 * left column) was restored by hand after a regression and had no pixel
 * contract. A fresh account has no connectors, so the tab is its own toolbar
 * plus the empty card — deterministic end to end.
 *
 * Safe to screenshot unconditionally here even though the tabs sit behind
 * `hasSubscriptionAccess()`: this spec is `@mock-only`, and the local
 * environment sets `requireBilling: false`, so the gate is open by
 * construction. The functional spec is the one that has to branch.
 */
test(
  'integrations connectors tab, no connectors configured',
  { tag: ['@visual', '@mock-only'] },
  async ({ page, integrationsPage, shell, userWithPersonality }) => {
    await integrationsPage.navigateTo();

    await expect(integrationsPage.heading).toBeVisible();
    await expect(integrationsPage.connectorsTab).toBeVisible();
    await integrationsPage.openConnectors();

    await expect(integrationsPage.connectorsLoading).toHaveCount(0);
    await expect(integrationsPage.emptyConnectorsMessage).toBeVisible();

    await expect(page).toHaveScreenshot('integrations-connectors-empty.png', {
      animations: 'disabled',
      mask: [...commonMasks(page), shell.recentThreadsSection],
    });
  },
);
