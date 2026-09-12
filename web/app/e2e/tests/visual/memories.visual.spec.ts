import { test, expect } from '../../fixtures';

/**
 * @functional-coverage tests/functional/memory/memories.spec.ts
 *
 * Listing, filtering, inline edit, delete, sort and pagination of the cards this
 * baseline pictures are covered by the functional memories spec.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

/**
 * The memory list card. The memory feature moved a lot over the last few
 * weeks (merge history, the compaction log link, the import flow) without any
 * pixel contract on the card itself.
 *
 * Element-scoped rather than full-page: the page header carries a
 * "<n> memories" count and the toolbar's filter tabs carry per-filter counts,
 * both of which depend on what else a run has seeded. One card, with fixed
 * content, is the part that is genuinely stable.
 */
test(
  'memory card in the memories list',
  { tag: ['@visual', '@mock-only'] },
  async ({ memoriesPage, seed, userWithPersonality }) => {
    // Fixed content, not the seeded `memory-<id>-0` default: the excerpt is
    // rendered on the card, so a random suffix would change its glyphs — and
    // its wrap point — on every run.
    const content = 'E2E visual memory: the user prefers concise answers.';
    await seed.memories(1, { content });

    await memoriesPage.navigateTo();
    await expect(memoriesPage.heading).toBeVisible();

    // Exactly one, not merely visible: a `toBeVisible` alone would also pass if
    // seeding had silently failed and a card with this fixed content survived
    // from an earlier run.
    const card = memoriesPage.card(content);
    await expect(card).toHaveCount(1);
    await expect(card).toBeVisible();

    await expect(card).toHaveScreenshot('memory-card.png', {
      animations: 'disabled',
      // `updatedAt` renders as a date and a clock time on every card.
      mask: [memoriesPage.cardMetadata(content)],
    });
  },
);
