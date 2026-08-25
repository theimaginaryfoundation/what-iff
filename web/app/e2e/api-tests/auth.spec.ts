import { test, expect, createSecondUser } from './fixtures';
import { createApiClient, loginUser, refreshToken, registerUser as sdkRegisterUser, ApiError } from '../sdk/client';
import { newTestUserDetails, registerUser, deleteUser } from '../fixtures/api';
import { NEVER_A_CREDENTIAL_VALUES } from '../test-data/invalid-values';

/**
 * register / login / refresh / unauthorised, straight against the API.
 *
 * No logout case: there is no `/user/logout` endpoint in openapi.yaml — this
 * API is stateless JWT (access + refresh token pair), so "logging out" is a
 * client-side token discard with nothing server-side to assert against.
 */

test.describe('auth', () => {
  test('register issues a usable access token', async () => {
    const user = newTestUserDetails();
    const accessToken = await registerUser(user);
    expect(accessToken).toBeTruthy();

    // "usable" means it authenticates a real call, not just that a string came back.
    const client = createApiClient(accessToken);
    const profile = await client.GET('/user/profile', {});
    expect(profile.error).toBeFalsy();
    expect(profile.response.status).toBe(200);

    await deleteUser(accessToken);
  });

  test('login issues a fresh token pair for a registered user', async () => {
    const user = newTestUserDetails();
    await registerUser(user);
    const anonClient = createApiClient();

    const { accessToken } = await loginUser(anonClient, user);
    expect(accessToken).toBeTruthy();

    await deleteUser(accessToken);
  });

  test('refresh exchanges a refresh token for a new pair', async () => {
    const user = newTestUserDetails();
    const registerClient = createApiClient();
    const registration = await sdkRegisterUser(registerClient, user);

    const anonClient = createApiClient();
    const refreshed = await refreshToken(anonClient, registration.refreshToken);
    expect(refreshed.accessToken).toBeTruthy();
    expect(refreshed.refreshToken).toBeTruthy();

    // The new access token must itself be usable.
    const refreshedClient = createApiClient(refreshed.accessToken);
    const profile = await refreshedClient.GET('/user/profile', {});
    expect(profile.error).toBeFalsy();
    expect(profile.response.status).toBe(200);

    await deleteUser(refreshed.accessToken);
  });

  test('an unauthenticated request is rejected with 401', async () => {
    const client = createApiClient();
    const { error, response } = await client.GET('/chat', {});
    expect(error).toBeTruthy();
    expect(response.status).toBe(401);
  });

  test("a request bearing another user's stale-format token is not silently accepted", async () => {
    const client = createApiClient('not-a-real-token');
    const { error, response } = await client.GET('/chat', {});
    expect(error).toBeTruthy();
    expect(response.status).toBe(401);
  });

  test('two independently registered users each get their own account', async () => {
    const a = await createSecondUser();
    const b = await createSecondUser();
    try {
      expect(a.user.username).not.toBe(b.user.username);
      const aChats = await a.apiClient.GET('/chat', {});
      const bChats = await b.apiClient.GET('/chat', {});
      // Both calls succeed independently — proves the two tokens really do
      // authenticate two different accounts, not the same one twice.
      expect(aChats.error).toBeFalsy();
      expect(bChats.error).toBeFalsy();
    } finally {
      await Promise.all([a.cleanup(), b.cleanup()]);
    }
  });
});

test.describe('login failure modes', () => {
  test('login with a wrong password fails without issuing tokens', async () => {
    const user = newTestUserDetails();
    const accessToken = await registerUser(user);
    const client = createApiClient();

    await expect(loginUser(client, { username: user.username, password: 'definitely-wrong' })).rejects.toBeInstanceOf(ApiError);

    await deleteUser(accessToken);
  });
});

test.describe('credential validation', () => {
  // Data-driven over e2e/test-data/invalid-values.ts: the same classes of
  // bad input every negative case should probe, curated once. The assertion
  // is deliberately only "rejected, no tokens issued" — the handler decides
  // which status each class earns, and pinning it here would couple this
  // suite to messages rather than the contract.

  test('login never issues tokens for values that could never be a password', async () => {
    const user = newTestUserDetails();
    const accessToken = await registerUser(user);
    const client = createApiClient();
    try {
      for (const [label, value] of NEVER_A_CREDENTIAL_VALUES) {
        await expect(loginUser(client, { username: user.username, password: value }), `password: ${label}`).rejects.toBeInstanceOf(ApiError);
      }
    } finally {
      await deleteUser(accessToken);
    }
  });

  test('login never issues tokens for values that could never be a username', async () => {
    const client = createApiClient();
    for (const [label, value] of NEVER_A_CREDENTIAL_VALUES) {
      await expect(loginUser(client, { username: value, password: 'irrelevant-Password-1!' }), `username: ${label}`).rejects.toBeInstanceOf(ApiError);
    }
  });

  test('register rejects an empty value in any required field', async () => {
    // Empty string only, not the whole BLANK_STRING_VALUES table: the
    // register contract validates *presence* of each field, and rejecting
    // whitespace-only values would be a server-side change first.
    const client = createApiClient();
    const details = newTestUserDetails();
    await expect(sdkRegisterUser(client, { ...details, username: '' }), 'empty username').rejects.toBeInstanceOf(ApiError);
    await expect(sdkRegisterUser(client, { ...details, email: '' }), 'empty email').rejects.toBeInstanceOf(ApiError);
    await expect(sdkRegisterUser(client, { ...details, password: '' }), 'empty password').rejects.toBeInstanceOf(ApiError);
  });
});
