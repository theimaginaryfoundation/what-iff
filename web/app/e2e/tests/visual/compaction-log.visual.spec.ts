import { test, expect } from '../../fixtures';
import { commonMasks } from './visual.helpers';

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

    // The visual contract for #76 is the compact default state: prompt history is discoverable
    // without pushing the existing checkpoint feed down the page.
    await expect(page).toHaveScreenshot('compaction-log-personality-prompt-history.png', {
      animations: 'disabled',
      mask: commonMasks(page),
      maxDiffPixels: 50,
    });

    await compactionLogPage.expandPromptChanges();
    await expect(compactionLogPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'true');
    const changeCard = compactionLogPage.promptChangeCard(personalityName);
    await expect(changeCard).toBeVisible();
    await expect(changeCard).toContainText(initialPrompt);
    await expect(changeCard).toContainText(updatedPrompt);
  },
);
