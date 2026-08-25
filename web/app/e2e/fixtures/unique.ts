import { randomUUID } from 'node:crypto';

/**
 * The suite's single source of uniqueness for anything it creates — test
 * users, seeded entities, and names typed into the UI by specs.
 *
 * `randomUUID()` rather than `Date.now()` + `Math.random()`: under parallel
 * workers a timestamp+pseudo-random collision is unlikely but possible, and a
 * collision here doesn't fail cleanly — a reused email hits an
 * already-registered account, and a reused entity name makes teardown delete
 * the wrong row. Imported from `node:crypto` rather than the `crypto` global
 * so it type-checks without depending on DOM lib settings.
 */
export function uniqueId(): string {
  return randomUUID();
}

/** Short unique segment, for names that a human reads in a screenshot or the UI. */
export function shortId(): string {
  return randomUUID().slice(0, 8);
}

/**
 * `e2e-` prefixed so a stray is identifiable as test data at a glance. The
 * prefix is a label only — no sweep collects it; see fixtures/api.ts.
 */
export function seedName(kind: string): string {
  return `e2e-${kind}-${uniqueId()}`;
}
