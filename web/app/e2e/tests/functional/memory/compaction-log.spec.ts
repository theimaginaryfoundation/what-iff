import { test, expect } from '../../../fixtures';
import type { Page } from '@playwright/test';
import type { AppShell, PersonalitiesPage, PersonalityDetailPage } from '../../../poms';

/**
 * The compaction log's personality prompt audit section (#65, collapsed by
 * default since #76). `tests/visual/compaction-log.visual.spec.ts` pins how
 * the change card looks and where the collapsed toggle sits; this spec owns
 * the behaviour behind it — that entries are written on save, that the toggle
 * gates them, and that "Restore previous" appends rather than rewrites.
 */

/**
 * Creates a personality through the UI and edits its prompt once, which is
 * what produces an audit entry. Returns the fixed strings the assertions use.
 *
 * Fixed names, not `seedName()`: the card is located by the personality name
 * and the diff panes are compared against the prompts verbatim.
 */
async function editPromptOnce(
  page: Page,
  personalitiesPage: PersonalitiesPage,
  personalityDetailPage: PersonalityDetailPage,
  shell: AppShell,
  name: string,
): Promise<{ initialPrompt: string; updatedPrompt: string }> {
  const initialPrompt = 'You are a calm assistant. Keep answers concise.';
  const updatedPrompt = 'You are a precise assistant. Explain your reasoning in short bullet points.';

  await shell.dismissAnnouncementIfPresent();
  await personalitiesPage.navigateTo();
  await shell.dismissAnnouncementIfPresent();
  await personalitiesPage.openCreateManually();
  await personalitiesPage.createManually(name, initialPrompt);

  await personalityDetailPage.editPrompt({ systemPrompt: updatedPrompt });
  // The audit entry is written by the same request that saves the prompt, so
  // navigating to the log before it lands races an empty list.
  const saved = page.waitForResponse(
    response => response.request().method() === 'PUT' && /\/api\/personality\/[^/]+$/.test(response.url()),
  );
  await personalityDetailPage.savePrompt();
  await saved;

  return { initialPrompt, updatedPrompt };
}

test('records a prompt edit as an audit entry behind the collapsed toggle', async ({
  authenticatedPage: page,
  compactionLogPage,
  personalitiesPage,
  personalityDetailPage,
  shell,
}) => {
  const name = 'E2E Audit Persona';
  const { initialPrompt, updatedPrompt } = await editPromptOnce(
    page,
    personalitiesPage,
    personalityDetailPage,
    shell,
    name,
  );
  await expect(page).toHaveURL(/\/personality\/[^/]+$/);

  await compactionLogPage.navigateTo();
  await shell.dismissAnnouncementIfPresent();
  await expect(compactionLogPage.heading).toBeVisible();

  // Collapsed by default: the entry exists but is not rendered until asked for.
  await expect(compactionLogPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'false');
  await expect(compactionLogPage.promptChangesList).toHaveCount(0);

  await compactionLogPage.expandPromptChanges();
  await expect(compactionLogPage.promptChangesToggle).toHaveAttribute('aria-expanded', 'true');
  await expect(compactionLogPage.noPromptChangesMessage).toHaveCount(0);

  // Both sides of the diff, and the action that produced the entry.
  await expect(compactionLogPage.promptChangePane(name, 'Before')).toHaveText(initialPrompt);
  await expect(compactionLogPage.promptChangePane(name, 'After')).toHaveText(updatedPrompt);
  await expect(compactionLogPage.promptChangeAction(name)).toHaveText('Edited');
});

test('restoring a previous prompt appends a second, reversed entry', async ({
  authenticatedPage: page,
  compactionLogPage,
  personalitiesPage,
  personalityDetailPage,
  shell,
}) => {
  const name = 'E2E Restore Persona';
  const { initialPrompt, updatedPrompt } = await editPromptOnce(
    page,
    personalitiesPage,
    personalityDetailPage,
    shell,
    name,
  );

  await compactionLogPage.navigateTo();
  await shell.dismissAnnouncementIfPresent();
  await compactionLogPage.expandPromptChanges();
  await expect(compactionLogPage.promptChangeCard(name)).toHaveCount(1);

  await compactionLogPage.restorePrevious(name);

  // Append-only: the edit entry stays and a restore entry joins it, rather
  // than the original being rewritten or removed.
  await expect(compactionLogPage.promptChangeCard(name)).toHaveCount(2);
  const newest = compactionLogPage.promptChangeCard(name).first();
  await expect(newest.locator('.diff-pane--old .diff-pane__content')).toHaveText(updatedPrompt);
  await expect(newest.locator('.diff-pane--new .diff-pane__content')).toHaveText(initialPrompt);
  await expect(newest.locator('.compaction-card__badge-value').filter({ hasText: 'Restored' })).toBeVisible();
});
