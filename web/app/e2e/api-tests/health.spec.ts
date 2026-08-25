import { test, expect } from '@playwright/test';
import { createApiClient } from '../sdk/client';

/**
 * GET /health, straight against the API. Unauthenticated by design — this is
 * the endpoint `make dev-up` polls to decide the server is up, so it has to
 * answer before any account exists.
 *
 * Ported from the Newman regression suite's "00 Setup / Health" step (that
 * suite's scenario-contract.md, step 1), which asserted only the status
 * code. The body is `text/plain`, not JSON, hence `parseAs`.
 */

test.describe('health', () => {
  test('answers 200 without authentication', async () => {
    const client = createApiClient();
    const { data, error, response } = await client.GET('/health', { parseAs: 'text' });

    expect(error).toBeFalsy();
    expect(response.status).toBe(200);
    expect(data?.length ?? 0).toBeGreaterThan(0);
  });
});
