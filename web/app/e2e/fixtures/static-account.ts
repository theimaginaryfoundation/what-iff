import type { TestUser } from './api';

/**
 * Swap-point: the account a test runs as when the suite cannot self-register
 * one.
 *
 * In this build there is no such account — every config (mock/local) registers
 * a fresh, isolated user per test via the API, so
 * `staticTestAccountConfigured()` is always false and `signInAsStaticAccount()`
 * is unreachable. A downstream consumer pointing the suite at a deployment
 * whose login is backed by an external identity provider (where the suite has
 * no way to provision a throwaway identity) replaces this module with one that
 * signs a single pre-provisioned account in once per worker. The two fixture
 * behaviors that hinge on it live in `fixtures/index.ts`
 * (`authenticatedTestUser`, `authenticatedPage`).
 */
export function staticTestAccountConfigured(): boolean {
  return false;
}

// Not `async` (nothing here awaits), but the return type is part of the
// swap-point contract: a replacement implementation is expected to be async.
export function signInAsStaticAccount(): Promise<{ user: TestUser; accessToken: string }> {
  return Promise.reject(
    new Error(
      'No static test account exists in this build — staticTestAccountConfigured() is always false here, so this call should be unreachable.',
    ),
  );
}
