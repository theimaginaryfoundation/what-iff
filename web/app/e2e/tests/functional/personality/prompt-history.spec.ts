import { test, expect } from '../../../fixtures';
import type { Page } from '@playwright/test';
import type { AppShell, PersonalitiesPage, PersonalityDetailPage } from '../../../poms';
import type { Seed } from '../../../fixtures';
import { uniqueId } from '../../../fixtures/unique';

/**
 * The personality prompt audit history (#65, collapsed by default since #76),
 * which lives under the system prompt editor on the personality detail page
 * since #205 (it used to sit in the Memory Manager's compaction log).
 * `tests/visual/personality-prompt-history.visual.spec.ts` pins how the change
 * card looks and where the collapsed section sits; this spec owns the
 * behaviour behind it — that entries are written on save, that the toggle
 * gates them, that "Restore previous" appends rather than rewrites, and that
 * the compaction log no longer carries them.
 */

/**
 * Creates a personality through the UI and edits its prompt once, which is
 * what produces an audit entry. Returns the fixed prompts the assertions use.
 *
 * The card is located by the personality name, so the name carries a per-run
 * suffix (deployed runs share one account, and a fixed name matched every
 * earlier run's leftover card), and the personality is handed to `seed` so it
 * is deleted afterwards. The prompts stay fixed: the diff panes compare them
 * verbatim, and they are only ever matched inside this run's card.
 */
async function editPromptOnce(
  page: Page,
  personalitiesPage: PersonalitiesPage,
  personalityDetailPage: PersonalityDetailPage,
  shell: AppShell,
  seed: Seed,
  name: string,
): Promise<{ initialPrompt: string; updatedPrompt: string }> {
  const initialPrompt = 'You are a calm assistant. Keep answers concise.';
  const updatedPrompt = 'You are a precise assistant. Explain your reasoning in short bullet points.';

  await personalitiesPage.navigateTo();
  await personalitiesPage.openCreateManually();
  await personalitiesPage.createManually(name, initialPrompt);
  // createManually only clicks Create; the app then routes to the new personality.
  await page.waitForURL(url => /^\/personality\/[^/]+$/.test(url.pathname));
  const created = /^\/personality\/([^/]+)$/.exec(new URL(page.url()).pathname);
  expect(created, 'creating the personality should land on its detail page').not.toBeNull();
  seed.adoptPersonality(created![1]);

  await personalityDetailPage.editPrompt({ systemPrompt: updatedPrompt });
  // The audit entry is written by the same request that saves the prompt, so
  // expanding the history before it lands races an empty list.
  const saved = page.waitForResponse(
    response => response.request().method() === 'PUT' && /\/api\/personality\/[^/]+$/.test(response.url()),
  );
  await personalityDetailPage.savePrompt();
  // Assert the save succeeded rather than only that it responded. A 4xx still
  // settles `waitForResponse`, and the failure would then surface as a
  // confusing empty audit list.
  expect((await saved).ok(), 'saving the prompt should succeed').toBe(true);

  return { initialPrompt, updatedPrompt };
}

test('records a prompt edit as an audit entry behind the collapsed toggle', async ({
  authenticatedPage: page,
  personalitiesPage,
  personalityDetailPage,
  shell,
  seed,
}) => {
  const name = `E2E Audit Persona ${uniqueId()}`;
  const { initialPrompt, updatedPrompt } = await editPromptOnce(
    page,
    personalitiesPage,
    personalityDetailPage,
    shell,
    seed,
    name,
  );
  await expect(page).toHaveURL(/\/personality\/[^/]+$/);

  // Collapsed by default: the entry exists but is not rendered until asked for.
  await expect(personalityDetailPage.promptHistory).toBeVisible();
  await expect(personalityDetailPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'false');
  await expect(personalityDetailPage.promptChangesList).toHaveCount(0);

  await personalityDetailPage.expandPromptChanges();
  await expect(personalityDetailPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'true');
  await expect(personalityDetailPage.noPromptChangesMessage).toHaveCount(0);

  // Only this personality's history is listed, so the page holds exactly the one edit.
  const cards = personalityDetailPage.promptChangeCards();
  await expect(cards).toHaveCount(1);
  await expect(personalityDetailPage.promptChangePane(cards.first(), 'Before')).toHaveText(initialPrompt);
  await expect(personalityDetailPage.promptChangePane(cards.first(), 'After')).toHaveText(updatedPrompt);
  await expect(personalityDetailPage.promptChangeAction(cards.first())).toHaveText('Edited');
});

test('restoring a previous prompt appends a second, reversed entry and updates the editor', async ({
  authenticatedPage: page,
  personalitiesPage,
  personalityDetailPage,
  shell,
  seed,
}) => {
  const name = `E2E Restore Persona ${uniqueId()}`;
  const { initialPrompt, updatedPrompt } = await editPromptOnce(
    page,
    personalitiesPage,
    personalityDetailPage,
    shell,
    seed,
    name,
  );

  await personalityDetailPage.expandPromptChanges();
  const cards = personalityDetailPage.promptChangeCards();
  await expect(cards).toHaveCount(1);

  await personalityDetailPage.restorePrevious(cards.first());

  // Append-only: the edit entry stays and a restore entry joins it, rather
  // than the original being rewritten or removed.
  await expect(cards).toHaveCount(2);
  const newest = cards.first();
  await expect(personalityDetailPage.promptChangePane(newest, 'Before')).toHaveText(updatedPrompt);
  await expect(personalityDetailPage.promptChangePane(newest, 'After')).toHaveText(initialPrompt);
  await expect(personalityDetailPage.promptChangeAction(newest)).toHaveText('Restored');

  // The editor right above shows the restored prompt without a reload.
  await expect(personalityDetailPage.promptEditor).toContainText(initialPrompt);
});

test('the compaction log no longer lists personality prompt changes', async ({
  authenticatedPage: page,
  compactionLogPage,
}) => {
  await compactionLogPage.navigateTo();
  await expect(compactionLogPage.heading).toBeVisible();
  await expect(page.getByTestId('prompt-change-toggle')).toHaveCount(0);
  await expect(page.getByRole('list', { name: 'Personality prompt changes' })).toHaveCount(0);
});
