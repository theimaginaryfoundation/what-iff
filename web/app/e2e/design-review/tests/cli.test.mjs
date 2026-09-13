/**
 * CLI argument handling.
 *
 * Every case here is one where the wrong behaviour is not a crash but a
 * *plausible report about the wrong thing*, which is the failure mode this
 * tool can least afford: a reviewer has no way to tell a confidently wrong
 * before/after from a correct one.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const execFileAsync = promisify(execFile);
const CLI = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'cli.mjs');

/** Runs the CLI and returns `{ code, stdout, stderr }` without throwing. */
async function run(...args) {
  try {
    const { stdout, stderr } = await execFileAsync('node', [CLI, ...args], { encoding: 'utf8', timeout: 60_000 });
    return { code: 0, stdout, stderr };
  } catch (error) {
    return { code: error.code ?? 1, stdout: error.stdout ?? '', stderr: error.stderr ?? '' };
  }
}

test('an empty inline value is rejected, not passed through as a ref', async () => {
  // `--base=` parses as an inline value of "", which reached git as an empty
  // ref. git does not error on that — it resolves to nothing — so the report
  // came back claiming every screen was new, with exit code 0 and no
  // warning. This is the single worst outcome the tool has.
  const result = await run('--base=', '--out', '/dev/null');

  assert.equal(result.code, 1);
  assert.match(result.stderr, /--base needs a value/);
  assert.ok(!/\d+ new/.test(result.stdout), 'it must not produce a report at all');
});

test('a flag where a value belongs is not swallowed as one', async () => {
  // Otherwise this compares against a ref literally named "--open".
  const result = await run('--base', '--open');

  assert.equal(result.code, 1);
  assert.match(result.stderr, /--base needs a value/);
});

test('a lone -- is skipped, because npm leaves one behind', async () => {
  const result = await run('--', '--help');

  assert.equal(result.code, 0);
  assert.match(result.stdout, /Design review report/);
});

test('an unknown flag fails with the message and the usage, not a stack trace', async () => {
  const result = await run('--nope');

  assert.equal(result.code, 1);
  assert.match(result.stderr, /Unknown option: --nope/);
  assert.ok(!result.stderr.includes('at parseArgs'), 'no stack trace');
});

test('--open does nothing in a non-interactive context', async () => {
  // stdout is a pipe here, so this is the CI shape: the flag must be
  // refused rather than spawning a platform opener nobody asked for.
  const result = await run('--open', '--out', '/dev/null', '--base', 'HEAD');

  assert.match(result.stderr, /--open is for interactive use/);
});
