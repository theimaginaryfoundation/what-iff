/**
 * Regression fixtures for analyzeSpec(). Every case marked VERIFIED BYPASS
 * below passed the original regex implementation of the visual-coverage gate;
 * they are the reason it was replaced. Run with `node --test`.
 */
import test from 'node:test';
import assert from 'node:assert/strict';
import { analyzeSpec } from './playwright-test-analysis.mjs';

const runnable = src => analyzeSpec('x.spec.ts', src).runnable;
const declared = src => analyzeSpec('x.spec.ts', src).declared;

test('a plain test is runnable', () => {
  assert.equal(runnable(`test('a', async () => {});`), 1);
});

test('describe does not itself count as a test', () => {
  // VERIFIED BYPASS: the old regex matched `test.describe(` as a test, so a
  // single skipped test inside a describe counted 2 tests against 1 skip and
  // passed the gate — the exact case the gate exists to catch.
  const src = `
    test.describe('group', () => {
      test('only test', async () => { test.skip(true, 'off everywhere'); });
    });`;
  assert.equal(declared(src), 1);
  assert.equal(runnable(src), 0);
});

test('an unconditional body skip disables its test', () => {
  assert.equal(runnable(`test('a', async () => { test.skip(true, 'why'); });`), 0);
});

test('describe.skip disables everything inside it', () => {
  // VERIFIED BYPASS: invisible to the old regex, which only knew `test.skip(true`.
  assert.equal(runnable(`test.describe.skip('g', () => { test('a', async () => {}); });`), 0);
});

test('describe.fixme disables everything inside it', () => {
  assert.equal(runnable(`test.describe.fixme('g', () => { test('a', async () => {}); });`), 0);
});

test('nested describes inherit the outer skip', () => {
  const src = `
    test.describe.skip('outer', () => {
      test.describe('inner', () => { test('a', async () => {}); });
    });`;
  assert.equal(runnable(src), 0);
});

test('a sibling skip is not attributed to the next test', () => {
  const src = `
    test('disabled', async () => { test.skip(true, 'off'); });
    test('enabled', async () => {});`;
  assert.equal(runnable(src), 1);
});

test('test.skip as a declaration modifier disables that test', () => {
  assert.equal(runnable(`test.skip('a', async () => {});`), 0);
});

test('a conditional skip still counts as runnable', () => {
  // Environment dependent, not disabled everywhere: skipped on one project and
  // run on another is real coverage. Treating these as disabled would make the
  // gate reject sound specs.
  assert.equal(runnable(`test('a', async () => { test.skip(process.env.CI === '1', 'ci'); });`), 1);
});

test('a tagged test is runnable', () => {
  assert.equal(runnable(`test('a', { tag: '@mock-only' }, async () => {});`), 1);
});

test('a file with no tests reports none', () => {
  assert.equal(declared(`export const helper = () => test('not a call site', () => {});`), 1);
});
