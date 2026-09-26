import { test, expect } from '../../fixtures';

/**
 * @functional-coverage tests/functional/auth/auth.spec.ts
 *
 * The login and register forms these baselines picture are driven end to end
 * (sign in, sign out, validation errors, the registration flow) by the
 * functional auth spec.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

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
