import { test, expect } from '../../fixtures';

/**
 * @functional-coverage tests/functional/memory/compaction-log.spec.ts
 *
 * Audit entries being written on save, the toggle gating them, and restore
 * appending rather than rewriting are covered by the functional compaction-log
 * spec.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

test(
  'personality prompt change history',
  { tag: ['@visual', '@mock-only'] },
  async ({ authenticatedPage: page, personalitiesPage, personalityDetailPage, compactionLogPage, shell }) => {
    const personalityName = 'E2E Prompt History Persona';
    const initialPrompt = 'You are a calm assistant. Keep answers concise.';
    const updatedPrompt = 'You are a precise assistant. Explain your reasoning in short bullet points.';

    await shell.dismissAnnouncementIfPresent();
    await personalitiesPage.navigateTo();
    await shell.dismissAnnouncementIfPresent();
    await personalitiesPage.openCreateManually();
    await personalitiesPage.createManually(personalityName, initialPrompt);
    await expect(page).toHaveURL(/\/personality\/[^/]+$/);

    await personalityDetailPage.editPrompt({ systemPrompt: updatedPrompt });
    const saved = page.waitForResponse(response =>
      response.request().method() === 'PUT' && /\/api\/personality\/[^/]+$/.test(response.url()),
    );
    await personalityDetailPage.savePrompt();
    await saved;

    await compactionLogPage.navigateTo();
    await shell.dismissAnnouncementIfPresent();
    await expect(compactionLogPage.heading).toBeVisible();
    await expect(compactionLogPage.promptChangesToggle).toBeVisible();
    await expect(compactionLogPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'false');
    await expect(compactionLogPage.promptChangesList).toHaveCount(0);

    // The visual contract for #76 is geometric rather than pixel-identical: when prompt history is
    // collapsed, the existing compaction feed must remain immediately below it instead of being
    // displaced by the prompt diff card that #65 added above the feed.
    const feedStatus = page.getByText(/^No compactions logged yet\./);
    await expect(feedStatus).toBeVisible();
    const toggleBox = await compactionLogPage.promptChangesToggle.boundingBox();
    const feedBox = await feedStatus.boundingBox();
    expect(toggleBox).not.toBeNull();
    expect(feedBox).not.toBeNull();
    expect(feedBox!.y).toBeGreaterThan(toggleBox!.y + toggleBox!.height);
    expect(feedBox!.y - (toggleBox!.y + toggleBox!.height)).toBeLessThan(100);

    await compactionLogPage.expandPromptChanges();
    await expect(compactionLogPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'true');
    const changeCard = compactionLogPage.promptChangeCard(personalityName);
    await expect(changeCard).toBeVisible();
    await expect(changeCard).toContainText(initialPrompt);
    await expect(changeCard).toContainText(updatedPrompt);

    // The collapsed contract above is geometric because the *page* around the
    // card is not pixel-stable. The card's body is: both prompts are fixed
    // strings, so the before/after diff panes are worth a real baseline — the
    // two-column diff grid is the part most likely to regress silently into a
    // stacked single column.
    //
    // Scoped to the body rather than the whole card, and unmasked. The card's
    // header row carries the entry's own date and time, and masking that
    // sub-element leaves a sliver of unmasked text at the mask's edge whenever
    // the rendered time changes width between runs. Dropping the header from
    // the shot removes the variance instead of tolerating it; the badges it
    // holds are asserted by the functional spec.
    await expect(compactionLogPage.promptChangeBody(personalityName)).toHaveScreenshot(
      'prompt-change-card.png',
      { animations: 'disabled' },
    );
  },
);
