import { test, expect } from '@playwright/test';
import { createApiClient } from '../sdk/client';

/**
 * GET /version, straight against the API. Unauthenticated by design: a
 * deployed-run preflight has to be able to ask "what build is this?" before
 * it has logged anything in.
 *
 * A local `go build` (no -ldflags) serves the compiled-in defaults
 * ("dev"/"unknown"), so these assertions are about shape — the fields exist
 * and are non-empty — not about specific values. Release builds stamp real
 * values; a downstream overlay build may additionally stamp overlay_commit.
 */

test.describe('version', () => {
  test('reports build provenance without authentication', async () => {
    const client = createApiClient();
    const { data, error, response } = await client.GET('/version', {});

    expect(error).toBeFalsy();
    expect(response.status).toBe(200);
    expect(response.headers.get('content-type')).toContain('application/json');

    expect(data?.version).toStrictEqual(expect.any(String));
    expect(data?.version).not.toBe('');
    expect(data?.commit).toStrictEqual(expect.any(String));
    expect(data?.commit).not.toBe('');
    expect(data?.built_at).toStrictEqual(expect.any(String));
    expect(data?.built_at).not.toBe('');
  });
});
