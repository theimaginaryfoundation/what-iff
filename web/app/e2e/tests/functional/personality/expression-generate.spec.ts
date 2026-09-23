import { test, expect } from '../../../fixtures';

/**
 * The expressions panel's "Generate" modal: name nine expressions, optionally
 * pick a reference image, generate, keep/discard.
 *
 * Image generation itself is disabled under `LLM_BACKEND=mock`/`local` (see
 * TEST_PLAN.md), so the keep/discard review is covered by the component's unit
 * specs, not here. What a hermetic run *can* prove end to end is everything up
 * to the image call — the default names, the name rules, the gallery picker —
 * and that a real enqueue → background job → failure round trip surfaces in
 * the modal instead of spinning forever.
 */

const DEFAULT_NAMES = ['happy', 'content', 'sad', 'angry', 'surprised', 'confused', 'tired', 'in love', 'thinking'];

test('opens with the nine default expression names', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);

  await personalityDetailPage.openGenerate();
  await expect(personalityDetailPage.generateDialog).toBeVisible();
  await expect(personalityDetailPage.generateNames).toHaveCount(9);
  for (const [i, name] of DEFAULT_NAMES.entries()) {
    await expect(personalityDetailPage.generateNames.nth(i)).toHaveValue(name);
  }
  await expect(personalityDetailPage.generateSubmit).toBeEnabled();

  await personalityDetailPage.generateCancel.click();
  await expect(personalityDetailPage.generateDialog).toBeHidden();
});

test('blocks generating while a name is empty or duplicated', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);
  await personalityDetailPage.openGenerate();

  const names = personalityDetailPage.generateNames;

  await names.nth(0).fill('');
  await expect(personalityDetailPage.generateCell(0).getByText('Name required')).toBeVisible();
  await expect(names.nth(0)).toHaveAttribute('aria-invalid', 'true');
  await expect(personalityDetailPage.generateSubmit).toBeDisabled();

  // Names are normalized to keys, so "SAD" collides with cell 3's "sad".
  await names.nth(0).fill('SAD');
  await expect(personalityDetailPage.generateCell(0).getByText('Duplicate name')).toBeVisible();
  await expect(personalityDetailPage.generateSubmit).toBeDisabled();

  await names.nth(0).fill('Mischievous');
  await expect(names.nth(0)).toHaveAttribute('aria-invalid', 'false');
  await expect(personalityDetailPage.generateSubmit).toBeEnabled();
});

test('the reference picker shows an empty gallery for a fresh user', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);
  await personalityDetailPage.openGenerate();

  await expect(personalityDetailPage.generateDialog.getByText('No reference')).toBeVisible();
  await personalityDetailPage.generateGalleryToggle.click();
  await expect(personalityDetailPage.generateGalleryToggle).toHaveAttribute('aria-expanded', 'true');
  await expect(personalityDetailPage.generateDialog.getByText('No images yet — upload one instead.')).toBeVisible();

  // Widening the scope to every gallery image is still empty for a new account.
  await personalityDetailPage.generateDialog.getByRole('button', { name: 'Show all images' }).click();
  await expect(personalityDetailPage.generateDialog.getByText('All images')).toBeVisible();
  await expect(personalityDetailPage.generateDialog.getByText('No images yet — upload one instead.')).toBeVisible();
});

test('a failed generation job surfaces in the modal and leaves the names editable', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);
  await personalityDetailPage.openGenerate();
  await personalityDetailPage.generateNames.nth(8).fill('Smug');

  await personalityDetailPage.generateSubmit.click();

  // The enqueue succeeds; the background job then fails on the mock backend's
  // image-generation gate, and the modal's job poll reports that failure.
  await expect(personalityDetailPage.generateError).toContainText('disabled under LLM_BACKEND=mock/local', { timeout: 30_000 });
  await expect(personalityDetailPage.generateSubmit).toHaveText('Generate');
  await expect(personalityDetailPage.generateSubmit).toBeEnabled();
  await expect(personalityDetailPage.generateNames.nth(8)).toBeEditable();
  await expect(personalityDetailPage.generateNames.nth(8)).toHaveValue('Smug');
});
