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
    await expect(compactionLogPage.promptChangesHeading).toBeVisible();
    const changeCard = compactionLogPage.promptChangeCard(personalityName);
    await expect(changeCard).toContainText(initialPrompt);
    await expect(changeCard).toContainText(updatedPrompt);

    await expect(page).toHaveScreenshot('compaction-log-personality-prompt-history.png', {
      animations: 'disabled',
      mask: [...commonMasks(page), compactionLogPage.promptChangeMetadata(personalityName)],
      // A second CI-image render varied by at most 36 pixels across desktop
      // and mobile after masking the timestamp; keep that renderer noise from
      // flaking the visual contract while leaving the threshold effectively zero.
      maxDiffPixels: 50,
    });
  },
);
