import {
  createApiClient,
  deleteUser as sdkDeleteUser,
  getProviderAccess,
  loginUser as sdkLoginUser,
  registerUser as sdkRegisterUser,
  setProviderKey,
} from '../sdk/client';
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

/**
 * A stand-in OpenAI key for test accounts. The mock LLM serves every model
 * call in process and provider egress is denied, so it is never sent anywhere;
 * it only stands for "this account has finished key setup".
 */
export const TEST_PROVIDER_KEY = 'sk-e2e-not-a-real-key-0000';

/**
 * Where accounts bring their own provider keys, gives the account a stand-in
 * OpenAI key so it starts past the first-run key prompt, as an account that
 * finished setup would. Does nothing where the deployment supplies keys.
 */
export async function giveProviderKey(accessToken: string, password: string): Promise<void> {
  const client = createApiClient(accessToken);
  const access = await getProviderAccess(client);
  if (access.keys_from !== 'account' || access.providers.length > 0) {
    return;
  }
  await setProviderKey(client, 'openai', TEST_PROVIDER_KEY, password);
}
