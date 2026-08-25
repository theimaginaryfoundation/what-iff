import { test, expect } from '../../../fixtures';

/**
 * Expression slots on the personality detail page — the "Add expression"
 * custom-key flow, which had no coverage.
 *
 * Only the custom-key half is reachable here. "Generate default expressions"
 * is a real image-generation call, disabled under `LLM_BACKEND=mock`/`local`
 * (see TEST_PLAN.md), and assigning an image to a slot needs a gallery that a
 * hermetic run cannot fill.
 *
 * Nothing below asserts that a new key survives a reload, and that is not an
 * omission: `submitCustomKey()` emits a client-side placeholder that the page
 * hands to `PersonalityViewService.setExpressions()`, which updates the cached
 * list "without re-fetching". A slot is only persisted once an image is
 * assigned to it. Asserting persistence here would be asserting something the
 * app does not currently claim to do.
 */

test('adds a custom expression key and opens the image picker for it', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);

  await expect(personalityDetailPage.expressions).toBeVisible();
  await expect(personalityDetailPage.expressionsEmptyMessage).toBeVisible();

  await personalityDetailPage.openAddExpression();
  await expect(personalityDetailPage.expressionKeyDialog).toBeVisible();

  await personalityDetailPage.submitExpressionKey('mischievous');

  await expect(personalityDetailPage.expressionKeyDialog).toBeHidden();
  await expect(personalityDetailPage.expressionSlot('mischievous')).toBeVisible();
  // Accepting a key runs straight into picking an image for it.
  await expect(personalityDetailPage.expressionImagePicker('mischievous')).toBeVisible();
});

test('rejects an expression key that breaks the key rules', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);

  await personalityDetailPage.openAddExpression();
  await personalityDetailPage.submitExpressionKey('Not A Valid Key!');

  await expect(personalityDetailPage.expressionKeyError).toHaveText(
    'Use lowercase letters, digits, hyphens, or underscores. Max 64 chars.',
  );
  // The modal stays open on a rejected key, so the typed value can be fixed
  // rather than retyped.
  await expect(personalityDetailPage.expressionKeyDialog).toBeVisible();
});

test('rejects an expression key that already exists', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);

  await personalityDetailPage.openAddExpression();
  await personalityDetailPage.submitExpressionKey('curious');
  await expect(personalityDetailPage.expressionSlot('curious')).toBeVisible();
  // The image picker opens on the accepted key; dismiss it to get back to the
  // page before adding the second one.
  await personalityDetailPage.expressionImagePicker('curious').press('Escape');
  await expect(personalityDetailPage.expressionImagePicker('curious')).toBeHidden();

  await personalityDetailPage.openAddExpression();
  await personalityDetailPage.submitExpressionKey('curious');

  await expect(personalityDetailPage.expressionKeyError).toHaveText('That key already exists.');
});

test('the add-key button stays disabled until a key is typed', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);

  await personalityDetailPage.openAddExpression();

  // Why the component's own "Key is required." branch is unreachable from the
  // UI: the button is disabled until the draft is non-empty.
  await expect(personalityDetailPage.expressionKeySubmit).toBeDisabled();
  await personalityDetailPage.expressionKeyInput.fill('playful');
  await expect(personalityDetailPage.expressionKeySubmit).toBeEnabled();

  await personalityDetailPage.expressionKeyCancel.click();
  await expect(personalityDetailPage.expressionKeyDialog).toBeHidden();
  await expect(personalityDetailPage.expressionSlot('playful')).toBeHidden();
});
