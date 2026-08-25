import { createApiClient, deleteUser as sdkDeleteUser, loginUser as sdkLoginUser, registerUser as sdkRegisterUser } from '../sdk/client';
import { uniqueId } from './unique';

/**
 * The only place in the suite that talks HTTP to the backend directly — via
 * the generated SDK in e2e/sdk (see e2e/sdk/README.md).
 */

export interface TestUser {
  username: string;
  email: string;
  password: string;
}

/**
 * `e2e-` prefixed so a stray is identifiable as test data at a glance.
 *
 * NOTE: the prefix is a label, not a safety net. No sweep deletes `e2e-`
 * records — cleanup is the fixtures' own teardown, so anything registered
 * outside a fixture leaks permanently. See e2e/README.md, "Cleanup".
 */
export function newTestUserDetails(): TestUser {
  const unique = uniqueId();
  return {
    username: `e2e-${unique}`,
    email: `e2e-${unique}@example.test`,
    password: 'E2ePlaywright123!',
  };
}

/**
 * Registers a fresh user directly against the backend API (bypassing the
 * register UI, which is covered separately) so each test starts from a
 * clean, isolated account. Requires the API to already be running — see
 * e2e/README.md. Returns the access token issued at registration, so
 * callers that need to clean the account up don't have to log in again.
 */
export async function registerUser(user: TestUser): Promise<string> {
  const client = createApiClient();
  const { accessToken } = await sdkRegisterUser(client, user);
  return accessToken;
}

/** Logs in via the API and returns the access token, for fixture teardown/setup that needs auth. */
export async function loginUser(user: Pick<TestUser, 'username' | 'password'>): Promise<string> {
  const client = createApiClient();
  const { accessToken } = await sdkLoginUser(client, user);
  return accessToken;
}

/** Deletes the account belonging to `accessToken`. Used for test-user self-cleanup. */
export async function deleteUser(accessToken: string): Promise<void> {
  const client = createApiClient(accessToken);
  await sdkDeleteUser(client);
}
