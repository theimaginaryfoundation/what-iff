import { test, expect } from '../../fixtures';

/**
 * @functional-coverage tests/functional/personality/prompt-history.spec.ts
 *
 * Audit entries being written on save, the toggle gating them, and restore
 * appending rather than rewriting are covered by the functional prompt-history
 * spec.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

test(
  'personality prompt change history',
  { tag: ['@visual', '@mock-only'] },
  async ({ authenticatedPage: page, personalitiesPage, personalityDetailPage }) => {
    const personalityName = 'E2E Prompt History Persona';
    const initialPrompt = 'You are a calm assistant. Keep answers concise.';
    const updatedPrompt = 'You are a precise assistant. Explain your reasoning in short bullet points.';

    await personalitiesPage.navigateTo();
    await personalitiesPage.openCreateManually();
    await personalitiesPage.createManually(personalityName, initialPrompt);
    await expect(page).toHaveURL(/\/personality\/[^/]+$/);

    await personalityDetailPage.editPrompt({ systemPrompt: updatedPrompt });
    const saved = page.waitForResponse(response =>
      response.request().method() === 'PUT' && /\/api\/personality\/[^/]+$/.test(response.url()),
    );
    await personalityDetailPage.savePrompt();
    await saved;

    await expect(personalityDetailPage.promptChangesToggle).toBeVisible();
    await expect(personalityDetailPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'false');
    await expect(personalityDetailPage.promptChangesList).toHaveCount(0);

    // The visual contract for #76 is geometric rather than pixel-identical: while prompt history is
    // collapsed it must stay a one-line section, so the scratchpad below it is not displaced by the
    // prompt diff cards. Since #205 the history sits between the system prompt editor and the
    // scratchpad on the personality detail page.
    await expect(personalityDetailPage.scratchpadTextarea).toBeVisible();
    const historyBox = await personalityDetailPage.promptHistory.boundingBox();
    const scratchpadBox = await personalityDetailPage.scratchpadTextarea.boundingBox();
    expect(historyBox).not.toBeNull();
    expect(scratchpadBox).not.toBeNull();
    expect(scratchpadBox!.y).toBeGreaterThan(historyBox!.y + historyBox!.height);
    expect(scratchpadBox!.y - (historyBox!.y + historyBox!.height)).toBeLessThan(100);

    await personalityDetailPage.expandPromptChanges();
    await expect(personalityDetailPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'true');
    const changeCard = personalityDetailPage.promptChangeCards().first();
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
    // the shot removes the variance instead of tolerating it; the action label
    // it holds is asserted by the functional spec.
    await expect(personalityDetailPage.promptChangeBody(changeCard)).toHaveScreenshot(
      'prompt-change-card.png',
      { animations: 'disabled' },
    );
  },
);
