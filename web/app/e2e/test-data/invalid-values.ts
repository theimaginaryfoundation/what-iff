/**
 * Universal invalid-value tables for negative API tests, as `as const`
 * label/value tuples so a data-driven loop can name the case it is running
 * without inventing labels at the call site. Curated here once instead of
 * ad-hoc literals per spec, so every endpoint's negative cases probe the
 * same classes of bad input and a new class added here reaches them all.
 *
 * These are inputs that can never be a *valid* value for the field under
 * test — a test using them asserts the request fails; which 4xx it fails
 * with is the endpoint's contract, not this file's.
 */

/** Present-but-blank strings: the required-field checks' whole job. */
export const BLANK_STRING_VALUES = [
  ['empty string', ''],
  ['single space', ' '],
  ['whitespace only', ' \t '],
] as const;

/** Structurally hostile strings a well-formed client never sends. */
export const MALFORMED_STRING_VALUES = [
  ['embedded null byte', 'abc\u0000def'],
  ['embedded control character', 'abc\u0007def'],
  ['ten thousand characters', 'x'.repeat(10_000)],
  ['emoji only', '\u{1F4A5}\u{1F525}\u{1F4AB}'],
] as const;

/**
 * Injection-shaped strings. The assertion is never "the server sanitises
 * these" — it is that they behave like any other wrong value: rejected,
 * no tokens issued, no error that echoes them back.
 */
export const INJECTION_STRING_VALUES = [
  ['SQL tautology', "' OR '1'='1"],
  ['SQL statement smuggling', '"; DROP TABLE users; --'],
  ['HTML script tag', '<script>alert(1)</script>'],
  ['template expression', '{{7*7}}'],
] as const;

/** Everything above in one table, for fields where any bad string will do. */
export const NEVER_A_CREDENTIAL_VALUES: readonly (readonly [string, string])[] = [
  ...BLANK_STRING_VALUES,
  ...MALFORMED_STRING_VALUES,
  ...INJECTION_STRING_VALUES,
];
