import { test, expect } from '../../fixtures';

/**
 * Unauthenticated screens — no fixture data, no LLM involvement, fully
 * static. Safest possible visual baselines.
 */
test.describe('auth screens', () => {
  test('login page', { tag: ['@visual', '@mock-only'] }, async ({ loginPage, page }) => {
    await loginPage.navigateTo();

    await expect(page).toHaveScreenshot('login-page.png', {
      animations: 'disabled',
    });
  });

  test('register page', { tag: ['@visual', '@mock-only'] }, async ({ page, registerPage }) => {
    await registerPage.navigateTo();

    await expect(page).toHaveScreenshot('register-page.png', {
      animations: 'disabled',
    });
  });
});
