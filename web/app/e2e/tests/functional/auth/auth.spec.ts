import { test, expect, signInAs, uniqueId } from '../../../fixtures';
import { AUTH_REDIRECT_TIMEOUT } from '../../../timeouts';

/**
 * These specs exercise authentication itself, so they must start signed OUT.
 * The deployed configs preload a shared session into every project via
 * `storageState`; without this override the app would already be authenticated
 * and redirect straight off /auth/login. Harmless on mock/local, which have no
 * stored state to begin with.
 */
test.use({ storageState: { cookies: [], origins: [] } });

/**
 * Register/login through the real UI (not the API helper in fixtures/).
 * The register case is a direct regression test for the removal of the
 * hardcoded dev-signup domain restriction — an arbitrary email must succeed
 * under ENV=development with no ALLOWED_EMAILS set.
 *
 * Registration here is local username/password with no terms-of-service or
 * email-verification step.
 */
test.describe('registration', () => {
  // Creates a real account via POST /user/register that nothing here deletes
  // (no e2e-* sweep exists yet), so a run against a persistent backend leaves it
  // behind — hence the @needs-cognito-admin cleanup tag.
  test(
    'registering with an arbitrary email succeeds (no dev-domain restriction)',
    { tag: '@needs-cognito-admin' },
    async ({ page, registerPage }) => {
      const unique = uniqueId();

      await registerPage.navigateTo();

      await registerPage.fillCredentials({
        username: `e2e-ui-${unique}`,
        email: `e2e-ui-${unique}@anydomain.test`,
        password: 'E2ePlaywright123!',
      });

      await registerPage.submit();

      // A 403 from the old dev-domain gate would leave the user stuck on
      // /auth/register with an error banner; success navigates away.
      await expect(page).not.toHaveURL(/\/auth\/register/, {
        timeout: AUTH_REDIRECT_TIMEOUT,
      });
    },
  );
});

test.describe('login', () => {
  // The assertion lives inside the shared signInAs() helper (fixtures/index.ts),
  // not inline here; the rule can't see through the function call.
  // eslint-disable-next-line playwright/expect-expect
  test('valid credentials succeed', async ({ page, testUser }) => {
    await signInAs(page, testUser);
  });

  test('invalid credentials show an error and stay on the login page', async ({ loginPage, page }) => {
    await loginPage.signIn({
      email: 'nonexistent-user@example.test',
      password: 'wrong-password-123',
    });

    // Backend-agnostic: the local auth path renders "Invalid …", while a
    // Cognito-backed build surfaces "Incorrect username or password" or "User
    // not found …" (auth.service.ts's error mapping). Asserting only on
    // /invalid/i passed against mock/local and failed against dev for a
    // correctly-behaving app. What matters to this test is that a rejection is
    // shown at all, not which vendor's phrasing it uses.
    await expect(loginPage.errorAlert).toContainText(/invalid|incorrect|not found/i);
    await expect(page).toHaveURL(/\/auth\/login/);
  });
});
