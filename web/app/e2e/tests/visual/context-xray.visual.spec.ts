/* eslint-disable playwright/no-conditional-in-test --
 * The early return below branches on whether the turn's model is in the
 * reviewed BYOK input-pricing catalog, which is a property of the backend
 * under test rather than test-controlled state — the same shape, and the same
 * justification, as the subscription gate in
 * tests/functional/integrations/integrations.spec.ts. Asserting a cost
 * unconditionally would make this spec a pricing-catalog test that fails the
 * day a default model is repriced; asserting nothing would drop the placement
 * coverage it exists for. */
import { test, expect } from '../../fixtures';
import { LLM_REPLY_TIMEOUT } from '../../timeouts';

/**
 * @functional-coverage tests/functional/chat/thread-workspace.spec.ts
 *
 * That the Context action opens the X-ray, that the breakdown and gauge render,
 * and that the cost outlet is mounted for an assistant turn are covered by the
 * thread-workspace spec's token-breakdown test.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

/**
 * The Context X-ray's cost display, moved into `app-context-cost-outlet` by
 * #49 so the private build can swap it for credit costs.
 *
 * Geometric rather than pixel-identical, for the same reason as
 * `compaction-log.visual.spec.ts`: every number on this screen — token total,
 * budget fill, capture time, and the cost itself — is derived from a live
 * turn, so a committed PNG would encode one run's token count and fail on the
 * next. What the outlet extraction can actually regress is *placement*: the
 * cost has to stay on the gauge's top row, trailing the token total, instead
 * of wrapping onto its own line or being pushed below the track.
 */
test(
  'context x-ray cost sits on the gauge top row',
  { tag: ['@visual', '@mock-only'] },
  async ({ chatPage, seed, userWithPersonality }) => {
    const thread = await seed.thread(undefined, {
      personalityId: userWithPersonality.personality.id,
    });
    await chatPage.navigateTo(thread.id as string);

    await chatPage.sendMessage('Show the token breakdown for this reply.');
    await expect(chatPage.lastAssistantBody).not.toBeEmpty({ timeout: LLM_REPLY_TIMEOUT });
    await expect(chatPage.lastAssistantContextAction).toBeVisible({ timeout: LLM_REPLY_TIMEOUT });

    await chatPage.openLastAssistantContext();
    await expect(chatPage.contextBreakdown).toBeVisible();
    await expect(chatPage.contextGaugeTotal).toBeVisible();

    // The outlet is mounted for every assistant turn (the X-ray passes it the
    // owning message id even when there is no priced estimate), so its absence
    // means the extraction itself regressed.
    const outlet = chatPage.contextBreakdown.locator('app-context-cost-outlet');
    await expect(outlet).toHaveCount(1);

    // Whether a *cost* renders depends on the turn's model being in the
    // reviewed BYOK pricing catalog, which is a property of the backend under
    // test rather than of the layout — so the geometry below is asserted only
    // when the public build actually painted one. Absence is covered by the
    // component spec; this spec owns placement.
    if ((await chatPage.contextCostEstimate.count()) === 0) {
      return;
    }

    await expect(chatPage.contextCostEstimate).toHaveText(/^Est\. \$\d+\.\d{3}$/);

    const topRow = await chatPage.contextGaugeTop.boundingBox();
    const total = await chatPage.contextGaugeTotal.boundingBox();
    const budget = await chatPage.contextGaugeBudget.boundingBox();
    const cost = await chatPage.contextCostEstimate.boundingBox();
    for (const box of [topRow, total, budget, cost]) expect(box).not.toBeNull();

    // Same line as the token total, not wrapped beneath it.
    expect(Math.abs(cost!.y - total!.y)).toBeLessThan(total!.height);
    // Trailing the total and its budget, not sitting between them.
    expect(cost!.x).toBeGreaterThan(budget!.x + budget!.width);
    // Inside the top row, so the track below it is not displaced.
    expect(cost!.y + cost!.height).toBeLessThanOrEqual(topRow!.y + topRow!.height + 1);

    // Pushed to the row's right edge, not sitting in natural flow right after
    // the budget label. This is the assertion that caught the regression the
    // outlet extraction introduced: `margin-left: auto` was left on the
    // `.gauge__cost` span, which is no longer the flex child, so the cost sat
    // 5px after the budget with ~100px of empty row to its right. The rule now
    // lives on the outlet host in context-breakdown-tab.component.ts.
    expect(topRow!.x + topRow!.width - (cost!.x + cost!.width)).toBeLessThan(4);
  },
);
