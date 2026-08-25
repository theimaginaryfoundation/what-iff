import type { Page } from '@playwright/test';
import {
  test,
  expect,
  signInAs,
  authenticateAsNewUser,
  shortId,
  attachCoverage,
} from '../../../fixtures';
import { ProfileSettingsModal } from '../../../poms';

/**
 * Profile & Settings modal — see e2e/TEST_PLAN.md item 3. Opened via the
 * sidebar's "Open profile for <username>" button; a real modal (ui-modal),
 * not a route change.
 */
async function openProfileSettings(modal: ProfileSettingsModal): Promise<ProfileSettingsModal> {
  await modal.open();
  await expect(modal.heading).toBeVisible();
  return modal;
}

/**
 * Selects a theme mode and waits for `ThemeService.setTheme()`'s backend
 * sync (`PUT /user/preferences`, fired as soon as the `<select>` changes —
 * see `onThemeModeChange()` in profile-settings-modal.component.ts) to
 * complete, so a caller can safely reload immediately after.
 */
async function selectThemeAndWaitForSync(
  page: Page,
  modal: ProfileSettingsModal,
  mode: 'light' | 'dark' | 'system',
): Promise<void> {
  const themeSynced = page.waitForResponse(
    (res) => res.url().includes('/user/preferences') && res.request().method() === 'PUT',
  );
  await modal.selectTheme(mode);
  await themeSynced;
}

/**
 * @mutates-account — rewrites the account's own profile fields rather than a
 * seeded, self-cleaning entity. Not destructive the way the password test is,
 * but it permanently renames the shared deployed account and leaves the theme
 * flipped, so the tag keeps deployed runs read-only with respect to the
 * account itself.
 */
test('updating first/last name and theme persists', { tag: '@mutates-account' }, async ({ page, profileSettingsModal, testUser }) => {
  await signInAs(page, testUser);
  let modal = await openProfileSettings(profileSettingsModal);

  const unique = shortId();
  await modal.fillProfile({
    firstName: `Updated${unique}`,
    lastName: 'E2E',
    theme: 'light',
  });

  await modal.saveChanges();
  await expect(page.getByText('Profile updated.')).toBeVisible();

  // Reload and reopen to confirm the change actually persisted server-side,
  // not just in the form's local state.
  await page.reload();
  modal = await openProfileSettings(profileSettingsModal);
  await expect(modal.firstNameInput).toHaveValue(`Updated${unique}`);
  await expect(modal.lastNameInput).toHaveValue('E2E');
  await expect(modal.themeSelect).toHaveValue('light');
});

/**
 * Regression test for PR #291: theme mode grew a third `'dark'` state, and
 * selecting it (via `onThemeModeChange()` in
 * profile-settings-modal.component.ts) applies and persists the mode as soon
 * as the `<select>` fires `change` — no "Save Changes" click required, unlike
 * name/email. Only `light` mode had e2e coverage before this.
 *
 * @mutates-account — same reasoning as the theme case above: this rewrites
 * the account's own stored preference rather than a seeded, self-cleaning
 * entity.
 */
test('switching to dark theme applies it and persists across reload', { tag: '@mutates-account' }, async ({ page, profileSettingsModal, testUser }) => {
  await signInAs(page, testUser);
  let modal = await openProfileSettings(profileSettingsModal);

  // A brand new account's default preference is already 'dark' (ent schema
  // default on UserPreference.theme), so select 'light' first — a native
  // <select> only fires 'change' on an actual value transition — to
  // guarantee the later switch to 'dark' is a real, observable change.
  await selectThemeAndWaitForSync(page, modal, 'light');
  await expect(modal.htmlElement).toHaveAttribute('data-theme', 'light');

  await selectThemeAndWaitForSync(page, modal, 'dark');
  await expect(modal.htmlElement).toHaveAttribute('data-theme', 'dark');

  // Reload and reopen to confirm the mode persisted server-side, not just in
  // the form's local state.
  await page.reload();
  modal = await openProfileSettings(profileSettingsModal);
  await expect(modal.themeSelect).toHaveValue('dark');
  await expect(modal.htmlElement).toHaveAttribute('data-theme', 'dark');
});

/**
 * Regression test for PR #291: before this fix, choosing "system" resolved
 * it once against the current OS preference and persisted *that concrete
 * value* ('light'/'dark') instead of the literal mode 'system' — so the
 * preference got baked in at toggle time instead of tracking the OS setting
 * going forward. This test proves the resolved theme keeps tracking the OS
 * preference live (no save/reload needed) and that a reload under a
 * *different* OS preference than whatever was last resolved still re-derives
 * from the current preference rather than replaying the stale baked-in
 * value.
 *
 * @mutates-account — same reasoning as the other theme-mutating tests above.
 */
test(
  'switching to system theme keeps re-resolving live instead of freezing at the resolved value',
  { tag: '@mutates-account' },
  async ({ page, profileSettingsModal, testUser }) => {
    await page.emulateMedia({ colorScheme: 'light' });
    await signInAs(page, testUser);
    let modal = await openProfileSettings(profileSettingsModal);

    // A brand new account's default preference is already 'dark' (ent schema
    // default on UserPreference.theme). Move to 'light' first, then to
    // 'system', so the later switch is a real value transition — a native
    // <select> fires no 'change' event when re-selecting its current value.
    await selectThemeAndWaitForSync(page, modal, 'light');
    await selectThemeAndWaitForSync(page, modal, 'system');

    // Under an emulated light OS preference, 'system' resolves to 'light'.
    await expect(modal.htmlElement).toHaveAttribute('data-theme', 'light');

    // Live re-resolution: flipping the OS preference with no save/reload at
    // all must immediately flip the resolved theme, because the stored mode
    // is the literal string 'system', not a snapshot of what it resolved to.
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect(modal.htmlElement).toHaveAttribute('data-theme', 'dark');

    // Persistence: reload under yet another OS preference. A correct fix
    // re-resolves against the *current* preference at load time; the bug
    // this test guards against would instead have persisted and replayed
    // whichever concrete theme was resolved back when 'system' was chosen.
    await page.emulateMedia({ colorScheme: 'light' });
    await page.reload();
    modal = await openProfileSettings(profileSettingsModal);
    await expect(modal.themeSelect).toHaveValue('system');
    await expect(modal.htmlElement).toHaveAttribute('data-theme', 'light');
  },
);

/**
 * @mutates-account — changes the account's real password and never changes it
 * back. Harmless when `testUser` is a fresh throwaway account (mock/local),
 * destructive against the shared, pre-existing account the deployed configs
 * use: the credentials in `.env` stop working from this point on, every later
 * login in the run fails, and Cognito locks the account after enough failures.
 * That is not hypothetical — it happened on the first real dev run.
 */
test(
  'changing password with the correct current password works end to end',
  { tag: '@mutates-account' },
  async ({ browser, page, profileSettingsModal, testUser }) => {
    const newPassword = 'E2eNewPassword456!';
    await signInAs(page, testUser);
    const modal = await openProfileSettings(profileSettingsModal);

    await modal.changePassword(testUser.password, newPassword);

    await expect(page.getByText('Password updated.')).toBeVisible();

    /**
     * Verify with a fresh, unauthenticated browser context rather than
     * clearing storage on the same page — avoids any lingering in-memory
     * app state and matches how a real second session would behave.
     */
    // `storageState: undefined` is what makes it unauthenticated: a hand-built
    // context still inherits the config's `use` options, and the deployed
    // configs put a saved session there.
    const freshContext = await browser.newContext({ storageState: undefined });
    try {
      const freshPage = await freshContext.newPage();
      // This context is built by hand, so nothing has enrolled it in coverage.
      await attachCoverage(freshPage);
      await signInAs(freshPage, { email: testUser.email, password: newPassword });
    } finally {
      // `browser` is worker-scoped, so an unclosed context outlives the test and
      // stays alive for the rest of the worker.
      await freshContext.close();
    }
  },
);

/**
 * @needs-cognito-admin — creates its own account via `POST /user/register`. That works against
 * a local stack, but a deployed build authenticates against Cognito, so the
 * account would exist in the database and *not* in the user pool: the UI login
 * below could never succeed, and the row would be left behind with nothing to
 * clean it up. Currently fixme'd, so the body never runs — the tag is what
 * keeps that true the day someone flips the fixme, which is the explicit plan
 * once the wrong-current-password force-logout bug is fixed.
 */
test(
  'changing password with the wrong current password logs the user out instead of showing an inline error',
  { tag: '@needs-cognito-admin' },
  async ({ page, profileSettingsModal }) => {
    // Real bug found via exploration: the backend correctly returns 401 for
    // a wrong current password (internal/handlers/user/user.go), but the
    // frontend's global auth.interceptor.ts treats ANY 401 (other than on
    // /user/login, /user/register, /user/refresh) as session expiry — it
    // calls authService.refreshToken(), retries the original request, and
    // when that retry also 401s (still the wrong password, refreshing the
    // token doesn't fix that) the shared catchError logs the user out. A
    // wrong password on this one specific form should show an inline error,
    // not end the session. See e2e/TEST_PLAN.md item 3.
    test.fixme(true, 'Wrong current password force-logs-out via the global 401 interceptor — see #38');

    /**
     * Registered inline rather than via the `testUser` fixture so a fixme'd
     * test doesn't create an account it never uses.
     */
    const { user } = await authenticateAsNewUser();
    await signInAs(page, user);
    const modal = await openProfileSettings(profileSettingsModal);

    await modal.changePassword('definitely-the-wrong-password', 'WouldBeNewPassword789!');

    await expect(page.getByText(/current password is incorrect/i)).toBeVisible();
    await expect(page).not.toHaveURL(/\/auth\/login/);
  },
);
