import { test, expect } from '../../../fixtures';
import type { paths } from '../../../sdk/schema.d.ts';

// Same field set as the API's GET /version — reusing its generated type
// keeps the two provenance contracts from drifting apart silently.
type VersionInfo = paths['/version']['get']['responses'][200]['content']['application/json'];

// RFC 3339 / ISO 8601, UTC, second precision — what write-version.mjs emits
// (see its BUILT_AT handling) and what the API's schema documents.
const ISO_8601_UTC = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$/;

/**
 * The frontend's build provenance, `/version.json`, written by
 * scripts/write-version.mjs before every build and `npm start` and served as
 * a static asset — the counterpart of the API's `GET /api/version`
 * (api-tests/version.spec.ts). Shape only: a local build stamps git HEAD and
 * "dev", a release build stamps real values, and a downstream overlay build
 * may add `overlay_commit`.
 *
 * `@prod-safe`: one unauthenticated GET of a static file. The `content-type`
 * check is load-bearing, not incidental: nginx's SPA fallback
 * (`try_files $uri $uri/ /index.html`) would otherwise return `index.html`
 * with a 200 for a missing `version.json`, and only the content-type tells
 * the two apart.
 */
test('the frontend serves its build provenance at /version.json', { tag: '@prod-safe' }, async ({ request }) => {
  const response = await request.get('/version.json');

  expect(response.status()).toBe(200);
  expect(response.headers()['content-type']?.toLowerCase()).toContain('application/json');

  const info = (await response.json()) as VersionInfo;
  expect(info.version).toStrictEqual(expect.any(String));
  expect(info.version).not.toBe('');
  expect(info.commit).toStrictEqual(expect.any(String));
  expect(info.commit).not.toBe('');
  expect(info.built_at).toStrictEqual(expect.any(String));
  expect(info.built_at).not.toBe('');
  // Catches the BUILT_AT default degrading to a literal "unknown" (not a
  // timestamp) — a non-empty string alone wouldn't catch that. The regex is
  // the tighter of the two checks: Date.parse also accepts non-ISO formats
  // this script never emits, so the regex catches a format regression that
  // Date.parse alone would let through.
  expect(Number.isNaN(Date.parse(info.built_at))).toBe(false);
  expect(info.built_at).toMatch(ISO_8601_UTC);
});
