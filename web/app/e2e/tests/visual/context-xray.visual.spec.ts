/* eslint-disable playwright/no-conditional-in-test --
 * The early return below branches on whether the turn's model is in the
 * reviewed BYOK input-pricing catalog, which is a property of the backend
 * under test rather than test-controlled state — the same shape, and the same
 * justification, as the subscription gate in
 * tests/functional/integrations/integrations.spec.ts. Asserting a cost
 * unconditionally would make this spec a pricing-catalog test that fails the
 * day a default model is repriced; asserting nothing would drop the placement
 * coverage it exists for. */
import type { Locator } from '@playwright/test';
import { test, expect } from '../../fixtures';
import { LLM_REPLY_TIMEOUT } from '../../timeouts';

type BoundingBox = { x: number; y: number; width: number; height: number };

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
/** Tolerance for "flush against an edge", in CSS pixels. */
const EDGE_TOLERANCE = 4;

async function box(locator: Locator, name: string) {
  const rect = await locator.boundingBox();
  expect(rect, `${name} should be laid out and visible`).not.toBeNull();
  return rect!;
}

/**
 * The layout contract, expressed once so a tolerance change is a single edit
 * and a failure names which relationship broke rather than which subtraction
 * produced the wrong number.
 */
function expectOnSameRow(reference: BoundingBox, subject: BoundingBox, what: string) {
  expect(Math.abs(subject.y - reference.y), `${what} should sit on the same line`).toBeLessThan(reference.height);
}

function expectTrails(reference: BoundingBox, subject: BoundingBox, what: string) {
  expect(subject.x, `${what} should start after it`).toBeGreaterThan(reference.x + reference.width);
}

function expectFlushRight(container: BoundingBox, subject: BoundingBox, what: string) {
  const slack = container.x + container.width - (subject.x + subject.width);
  expect(slack, `${what} should be pushed to the row's right edge`).toBeLessThan(EDGE_TOLERANCE);
}

function expectVerticallyInside(container: BoundingBox, subject: BoundingBox, what: string) {
  // +1 absorbs subpixel rounding on the container's own bottom edge.
  expect(subject.y + subject.height, `${what} should not overflow the row`).toBeLessThanOrEqual(
    container.y + container.height + 1,
  );
}

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
    await expect(chatPage.contextCostOutlet).toHaveCount(1);

    // Gate the geometry on the outlet having rendered *something*, not on the
    // public build's `.gauge__cost` class matching. Whether a cost renders at
    // all depends on the turn's model being in the reviewed BYOK pricing
    // catalog, which is a property of the backend under test — but keying the
    // skip off a class name would also silently drop this coverage the day
    // that class is renamed, or when the private overlay's credit-cost UI is
    // mounted here instead. An outlet with content must be laid out correctly
    // whichever build produced it.
    const rendered = ((await chatPage.contextCostOutlet.textContent()) ?? '').trim();
    if (rendered === '') return;

    // Shape, not exact copy: this spec owns placement, and pinning the
    // formatting would make it fail on a pricing-precision change that left
    // the layout untouched.
    await expect(chatPage.contextCostEstimate).toHaveText(/^Est\. \$\d/);

    const topRow = await box(chatPage.contextGaugeTop, 'gauge top row');
    const total = await box(chatPage.contextGaugeTotal, 'token total');
    const budget = await box(chatPage.contextGaugeBudget, 'budget label');
    const cost = await box(chatPage.contextCostEstimate, 'cost estimate');

    expectOnSameRow(total, cost, 'the cost and the token total');
    expectTrails(budget, cost, 'the cost, relative to the budget label');
    expectVerticallyInside(topRow, cost, 'the cost');

    // The assertion that caught the regression the outlet extraction
    // introduced: `margin-left: auto` was left on the `.gauge__cost` span,
    // which is no longer the flex child, so the cost sat 5px after the budget
    // with ~100px of empty row to its right.
    //
    // The rule that makes this pass is
    //   `.gauge__top app-context-cost-outlet { margin-left: auto; }`
    // in context-breakdown-tab.component.ts. Those two are a pair — change one
    // and this fails, which is the point.
    expectFlushRight(topRow, cost, 'the cost');
  },
);
