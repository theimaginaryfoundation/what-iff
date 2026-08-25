import { test as base } from '@playwright/test';
import { createApiClient, getJob, type ApiClient, type Job } from '../sdk/client';
import { deleteUser, newTestUserDetails, registerUser, type TestUser } from '../fixtures/api';

/**
 * Fixtures for e2e/api-tests/, deliberately separate from e2e/fixtures/index.ts.
 * That file's `Fixtures` interface includes page-backed fixtures
 * (`authenticatedPage`, every POM); Playwright only instantiates a fixture
 * a test actually names, so importing it wouldn't literally launch a browser
 * today — but this suite has no browser project to fall back on if a future
 * change there stopped being lazy, and pulling in fifteen POM classes this
 * suite will never construct is its own reason to keep this file thin.
 */

export type { TestUser };

interface Fixtures {
  /** A freshly registered account, unique to this test. Deletes itself via the API in teardown. */
  testUser: TestUser;
  /** An SDK client authenticated as `testUser`. */
  apiClient: ApiClient;
}

interface InternalFixtures {
  /** Internal: the registered user plus its access token, shared by the fixtures above. */
  registered: { user: TestUser; accessToken: string };
}

export const test = base.extend<Fixtures & InternalFixtures>({
  registered: async ({}, use) => {
    const user = newTestUserDetails();
    const accessToken = await registerUser(user);

    await use({ user, accessToken });

    try {
      await deleteUser(accessToken);
    } catch (err) {
      // Same policy as fixtures/index.ts's authenticatedTestUser: log
      // locally, fail in CI. There is no sweep that reclaims a leaked
      // account — see e2e/README.md, "Cleanup".
      console.error(`testUser fixture: failed to delete ${user.username}:`, err);
      if (process.env['CI']) {
        throw new Error(`testUser fixture: failed to delete account ${user.username} (see logged error above).`);
      }
    }
  },

  testUser: async ({ registered }, use) => {
    await use(registered.user);
  },

  apiClient: async ({ registered }, use) => {
    await use(createApiClient(registered.accessToken));
  },
});

export { expect } from '@playwright/test';

/**
 * Registers a *second*, independent user — for cross-user access tests,
 * where the fixture-provided `testUser` needs a peer whose resources it
 * tries (and fails) to reach. Not a fixture itself: only the handful of
 * cross-user specs need a second identity, and a test-scoped fixture would
 * register one for every test in the file whether it used it or not. Callers
 * own its teardown via the returned `cleanup()`.
 */
export async function createSecondUser(): Promise<{ user: TestUser; apiClient: ApiClient; cleanup: () => Promise<void> }> {
  const user = newTestUserDetails();
  const accessToken = await registerUser(user);
  return {
    user,
    apiClient: createApiClient(accessToken),
    cleanup: () => deleteUser(accessToken),
  };
}

/** How often to re-poll a job's status. Not a static per-test wait — see waitForJobStatus. */
const JOB_POLL_INTERVAL_MS = 500;

/**
 * Polls a background job (the one a chat message spawns to generate its
 * reply — see `sendChatMessage`) until it reaches a terminal state, and
 * returns it. Throws immediately on `failed`, rather than waiting out the
 * full timeout to report a state that already can't change; throws on
 * timeout if the job is still `pending`/`processing`/... when time runs out.
 *
 * Mirrors `poll_job()` in scripts/mock-e2e.sh — same shape of assertion,
 * same reason for existing (`origin: 'User'` messages generate a reply
 * asynchronously; the message endpoint returns as soon as the job is
 * accepted, not once it's done).
 */
export async function waitForJobComplete(client: ApiClient, jobId: string, timeoutMs: number): Promise<Job> {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const job = await getJob(client, jobId);
    if (job.status === 'complete') {
      return job;
    }
    if (job.status === 'failed') {
      throw new Error(`job ${jobId} failed: ${job.error ?? '(no error message)'}`);
    }
    if (Date.now() >= deadline) {
      throw new Error(`job ${jobId} did not complete within ${timeoutMs}ms (last status: ${job.status})`);
    }
    await new Promise(resolve => setTimeout(resolve, JOB_POLL_INTERVAL_MS));
  }
}
