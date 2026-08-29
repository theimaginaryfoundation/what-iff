/* eslint-disable playwright/no-conditional-in-test, playwright/no-conditional-expect --
 * The subscription gate below is an environment property, not test-controlled
 * state: the same spec must be valid against a local build (gate open) and a
 * deployed one (gate closed for an unsubscribed account). Branching on the
 * rendered gate is the honest encoding of that; the alternative — tagging the
 * test out of deployed runs — would need config changes and silently drop the
 * coverage instead. */
import { test, expect, seedName } from '../../../fixtures';
import {
  DEFAULT_VIEWPORT_HEIGHT,
  RESPONSIVE_WIDTHS,
  expectBalancedHorizontalInsets,
  expectHorizontallyInside,
  expectInsideViewport,
  expectNoHorizontalScroll,
  rect,
} from '../../../helpers/responsive-layout';

/**
 * Integrations smoke coverage for the IntegrationsPage POM.
 *
 * The tabs are behind `hasSubscriptionAccess()`. Against the local dev server
 * (`environment.ts`, `requireBilling: false`) that is unconditionally true, so
 * a plain test user gets the full webhook flow. Against a dev/prod build it is
 * false for an unsubscribed account and the `role="status"` notice renders
 * instead — these tests branch on that rather than assuming either, so the same
 * spec is valid in every environment config.
 */

test('creates and revokes a webhook API token', async ({ integrationsPage, userWithPersonality }) => {
  await integrationsPage.navigateTo();
  await expect(integrationsPage.heading).toBeVisible();

  // Wait for the gate to resolve before branching on it. `hasSubscriptionAccess`
  // starts false and, where billing is required, only flips once
  // GET /billing/customer returns — so an instantaneous probe reads the
  // loading state, not the entitlement, and would take the "unavailable"
  // branch even for an account that turns out to have access.
  await expect(integrationsPage.unavailableNotice.or(integrationsPage.webhooksTab)).toBeVisible();

  if (await integrationsPage.unavailableNotice.isVisible()) {
    await expect(integrationsPage.webhooksTab).toBeHidden();
    return;
  }

  await integrationsPage.openWebhooks();
  await expect(integrationsPage.createTokenHeading).toBeVisible();

  const name = seedName('webhook');
  await integrationsPage.createToken(name);

  // The raw token is shown exactly once, in a dismissible banner.
  await expect(integrationsPage.newTokenBanner).toBeVisible();
  await integrationsPage.dismissTokenBanner();
  await expect(integrationsPage.newTokenBanner).toBeHidden();

  await expect(integrationsPage.tokenRow(name)).toContainText('Active');

  await integrationsPage.revokeToken(name);
  await expect(integrationsPage.tokenRow(name)).toContainText('Revoked');
});

test('copies the newly created token and shows an empty state once none remain', { tag: '@serial' }, async ({ integrationsPage, userWithPersonality }, testInfo) => {
  // navigator.clipboard.writeText rejects without this permission grant —
  // WebKit doesn't support programmatic grants at all, so this is skipped
  // there rather than asserting the "couldn't copy" fallback path instead.
  /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
  test.skip(testInfo.project.name === 'webkit-mobile', 'WebKit does not support the clipboard-write permission grant.');
  await userWithPersonality.page.context().grantPermissions(['clipboard-write']);

  await integrationsPage.navigateTo();
  await expect(integrationsPage.unavailableNotice.or(integrationsPage.webhooksTab)).toBeVisible();

  if (await integrationsPage.unavailableNotice.isVisible()) {
    return;
  }

  await integrationsPage.openWebhooks();
  // @serial: this is genuinely a "zero tokens on the whole account" claim —
  // on a deployed run tokens are only ever revoked, never deleted (see the
  // POM's own doc comment), so a shared account accumulates rows across every
  // past run and this would never be empty there next to any other test.
  await expect(integrationsPage.emptyTokensMessage).toBeVisible();

  const name = seedName('webhook');
  await integrationsPage.createToken(name);
  await expect(integrationsPage.newTokenBanner).toBeVisible();

  await integrationsPage.copyToken();

  // Copy success is reported through the shared ConfirmationService `alert()`
  // (integrations-webhooks-tab.component.ts), the same dialog Revoke's
  // confirmation uses — so it's asserted and dismissed through that POM
  // rather than the real OS clipboard, which isn't reliably readable across
  // browser projects/CI sandboxes.
  await expect(integrationsPage.confirmation.message).toHaveText('API token copied to clipboard.');
  await integrationsPage.confirmation.confirm('OK');

  await integrationsPage.dismissTokenBanner();
  await expect(integrationsPage.emptyTokensMessage).toBeHidden();

  await integrationsPage.revokeToken(name);
});

test('shows the connectors tab by default', async ({ integrationsPage, userWithPersonality }) => {
  await integrationsPage.navigateTo();

  // Same reasoning as above: let the billing gate settle before branching.
  await expect(integrationsPage.unavailableNotice.or(integrationsPage.connectorsTab)).toBeVisible();

  if (await integrationsPage.unavailableNotice.isVisible()) {
    await expect(integrationsPage.connectorsTab).toBeHidden();
    return;
  }

  await expect(integrationsPage.connectorsTab).toBeVisible();
  await expect(integrationsPage.webhooksTab).toBeVisible();
});

test('keeps the connector search controls contained and centered across responsive widths', async ({
  integrationsPage,
  page,
  userWithPersonality,
}) => {
  const widths = [...RESPONSIVE_WIDTHS, 1024] as const;

  for (const width of widths) {
    await test.step(`${width}px viewport`, async () => {
      await page.setViewportSize({ width, height: DEFAULT_VIEWPORT_HEIGHT });
      await integrationsPage.navigateTo();
      await expect(integrationsPage.unavailableNotice.or(integrationsPage.connectorsTab)).toBeVisible();

      if (await integrationsPage.unavailableNotice.isVisible()) {
        return;
      }

      const searchInput = page.getByPlaceholder('Search by name or description');
      const searchButton = page.getByRole('button', { name: 'Search', exact: true });
      const searchRow = searchInput.locator('..');

      await expect(searchInput).toBeVisible();
      await expect(searchButton).toBeVisible();

      const rowBox = await rect(searchRow, 'Connector search row');
      const inputBox = await rect(searchInput, 'Connector search input');
      const buttonBox = await rect(searchButton, 'Connector Search button');

      expectInsideViewport(rowBox, width, 'Connector search row');
      expectHorizontallyInside(rowBox, inputBox, 'Connector search input');
      expectHorizontallyInside(rowBox, buttonBox, 'Connector Search button');
      expectBalancedHorizontalInsets(rowBox, inputBox, buttonBox, 'Connector search controls');

      await searchInput.fill('connector search pressure test with a deliberately long query');
      await searchButton.click({ trial: true });
      await expectNoHorizontalScroll(page, width);
    });
  }
});
