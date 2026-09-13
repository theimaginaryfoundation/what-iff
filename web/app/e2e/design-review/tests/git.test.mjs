/**
 * Tests for reading the change set out of git.
 *
 * These are all about paths that git *displays* differently from how they
 * exist: renames abbreviated with braces, and names that git backslash-quotes
 * because they contain a space or a non-ASCII byte. Getting one wrong
 * produces a path that does not exist, attached to a plausible-looking file
 * entry, which then flows into the impact list and the "no screen covers
 * this" list as a file nobody can find.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import path from 'node:path';
import { changedFiles, resolveBase } from '../lib/git.mjs';
import { FixtureRepo } from './fixture-repo.mjs';
import { makePng } from './png-fixture.mjs';

async function seeded(files) {
  const repo = await FixtureRepo.create();
  for (const [file, contents] of Object.entries(files)) await repo.write(file, contents);
  await repo.commit('seed');
  const base = (await repo.git('rev-parse', 'HEAD')).stdout.trim();
  return { repo, base };
}

test('a rename inside a nested directory keeps its real path and its status', async t => {
  const { repo, base } = await seeded({ 'web/app/src/app/features/billing/invoice.component.html': '<p>a</p>\n'.repeat(40) });
  t.after(() => repo.dispose());

  // git abbreviates this to `web/app/src/app/features/{billing =>
  // payments}/invoice.component.html` in its human-readable output.
  await mkdir(path.join(repo.root, 'web/app/src/app/features/payments'), { recursive: true });
  await repo.git('mv', 'web/app/src/app/features/billing/invoice.component.html', 'web/app/src/app/features/payments/invoice.component.html');
  await repo.commit('move billing to payments');

  const files = await changedFiles(repo.root, base, 'HEAD');

  assert.deepEqual(
    files.map(file => [file.path, file.status]),
    [['web/app/src/app/features/payments/invoice.component.html', 'renamed']],
  );
});

test('a rename with no shared path structure is also reported whole', async t => {
  const { repo, base } = await seeded({ 'plain.txt': 'x\n'.repeat(40) });
  t.after(() => repo.dispose());

  await repo.git('mv', 'plain.txt', 'renamed.txt');
  await repo.commit('rename');

  const files = await changedFiles(repo.root, base, 'HEAD');
  assert.deepEqual(files.map(file => [file.path, file.status]), [['renamed.txt', 'renamed']]);
});

test('paths git would quote survive verbatim', async t => {
  // Without `-z`, git renders this as `"web/app/src/a file \303\251.css"` —
  // quoted, octal-escaped, and not the name of anything on disk.
  const quoted = 'web/app/src/a file é.css';
  const { repo, base } = await seeded({ 'web/app/src/placeholder.txt': 'x\n' });
  t.after(() => repo.dispose());

  await repo.write(quoted, '.a { color: red }\n');
  await repo.commit('add an awkwardly named stylesheet');

  const files = await changedFiles(repo.root, base, 'HEAD');
  assert.deepEqual(files.map(file => file.path), [quoted]);
});

test('binary files report no line counts rather than a fabricated zero', async t => {
  // A real PNG, not a few handpicked bytes: git decides "binary" by looking
  // for a NUL in the first 8000 bytes, so a short header-shaped buffer is
  // still text to it and reports line counts like any other file.
  const { repo, base } = await seeded({ 'web/app/src/logo.png': makePng(8, 8, [10, 20, 30]) });
  t.after(() => repo.dispose());

  await repo.write('web/app/src/logo.png', makePng(8, 8, [200, 30, 40]));
  await repo.commit('change the logo');

  const [file] = await changedFiles(repo.root, base, 'HEAD');
  assert.equal(file.added, null);
  assert.equal(file.deleted, null);
});

test('an unresolvable base ref is distinguished from a missing common ancestor', async t => {
  const { repo } = await seeded({ 'a.txt': 'x\n' });
  t.after(() => repo.dispose());

  // The two produce an identical empty report, and only one is the user's
  // typo — so the reason has to say which.
  const typo = await resolveBase(repo.root, 'orgin/man', 'HEAD');
  assert.equal(typo.sha, null);
  assert.match(typo.reason, /does not resolve to a commit/);

  const orphan = (await repo.git('commit-tree', (await repo.git('hash-object', '-t', 'tree', '/dev/null')).stdout.trim(), '-m', 'unrelated')).stdout.trim();
  const unrelated = await resolveBase(repo.root, orphan, 'HEAD');
  assert.equal(unrelated.sha, null);
  assert.match(unrelated.reason, /no common ancestor/);
});
