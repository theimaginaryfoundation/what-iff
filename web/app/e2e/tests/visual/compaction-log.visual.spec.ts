import { test, expect } from '../../fixtures';

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
  },
);
