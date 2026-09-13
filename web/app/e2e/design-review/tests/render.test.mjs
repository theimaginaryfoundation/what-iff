/**
 * Renderer tests.
 *
 * The report's value depends on it being one file that opens anywhere, and
 * on it staying small enough to send. Both are properties nothing else
 * checks, and both are one careless `<link>` or a lost deduplication away
 * from quietly going wrong.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { render, reportTitle } from '../render.mjs';
import { DEFAULT_THRESHOLD } from '../lib/diff.mjs';

const TOOL_DIR = path.join(path.dirname(fileURLToPath(import.meta.url)), '..');
import { makePng } from './png-fixture.mjs';

function image(buffer, overrides = {}) {
  return { path: 'x.png', bytes: buffer.length, width: 100, height: 80, buffer, ...overrides };
}

function modelWith(variants, extra = {}) {
  return {
    generatedAt: '2026-09-12T00:00:00.000Z',
    repoRoot: '/somewhere/private',
    base: { ref: 'origin/main', sha: 'a'.repeat(40) },
    head: { ref: 'HEAD', sha: 'b'.repeat(40), branch: 'redesign', subject: 'restyle', worktree: false },
    pr: null,
    screens: [{ id: 's', shot: 'landing', title: 'Landing', group: null, note: null, specPath: 'a.spec.ts', status: 'changed', related: [], variants }],
    impact: { files: [], byCategory: [], byArea: [], designShare: 0, frontendFileCount: 0, uncovered: [] },
    summary: { screens: 1, changed: 1, added: 0, removed: 0, unchanged: 0 },
    ...extra,
  };
}

test('the report references nothing outside itself', async () => {
  const png = makePng(100, 80, [10, 20, 30]);
  const html = await render(modelWith([{ project: 'chromium-desktop', label: 'Desktop', form: 'desktop', order: 0, status: 'changed', path: 'x.png', before: image(png), after: image(makePng(100, 80, [200, 0, 0])) }]));

  assert.ok(!/<link\b/i.test(html), 'no external stylesheet');
  assert.ok(!/src=["']https?:/i.test(html), 'no remotely hosted images');
  assert.ok(!/<script[^>]+\bsrc=/i.test(html), 'no external script');
  assert.match(html, /data:image\/png;base64,/, 'images are inlined');
});

test('identical images are embedded once', async () => {
  const png = makePng(100, 80, [10, 20, 30]);
  // An unchanged screen holds the same bytes on both sides; embedding them
  // twice would roughly double the size of a report that is mostly unchanged
  // screens, which is most reports.
  const html = await render(
    modelWith([{ project: 'chromium-desktop', label: 'Desktop', form: 'desktop', order: 0, status: 'unchanged', path: 'x.png', before: image(png), after: image(png) }]),
  );

  const occurrences = html.split('data:image/png;base64,').length - 1;
  assert.equal(occurrences, 1);
});

test('the embedded model survives characters that would end a script block', async () => {
  const html = await render(
    modelWith([{ project: 'chromium-desktop', label: 'Desktop', form: 'desktop', order: 0, status: 'changed', path: 'x.png', before: null, after: image(makePng(4, 4)) }], {
      head: { ref: 'HEAD', sha: 'b'.repeat(40), branch: 'fix/</script><img src=x>', subject: '<!-- hi -->', worktree: false },
    }),
  );

  const payload = html.match(/<script type="application\/json" id="design-data">([\s\S]*?)<\/script>/)[1];
  const parsed = JSON.parse(payload.replaceAll('\\u003c', '<'));
  assert.equal(parsed.head.branch, 'fix/</script><img src=x>');
  // The raw document must not contain the sequence at all, or the parser
  // would have closed the block before the JSON ended.
  assert.ok(!payload.includes('</script>'));
});

test('the repository path is not leaked into a shareable file', async () => {
  const html = await render(modelWith([{ project: 'chromium-desktop', label: 'Desktop', form: 'desktop', order: 0, status: 'changed', path: 'x.png', before: null, after: image(makePng(4, 4)) }]));

  assert.ok(!html.includes('/somewhere/private'), 'a report gets forwarded; local paths should not ride along');
});

test('a pull request title cannot break out of the document', async () => {
  // The title reaches the page twice, escaped two different ways: as text in
  // <title> via escapeHtml, and inside the JSON payload via embedJson. The
  // branch-name test above covers the payload; this covers the element,
  // because a PR title is attacker-influenced in a way a branch name on your
  // own machine is not.
  const hostile = '</title><script>alert(1)</script> & "quoted"';
  const html = await render(
    modelWith([{ project: 'chromium-desktop', label: 'Desktop', form: 'desktop', order: 0, status: 'changed', path: 'x.png', before: null, after: image(makePng(4, 4)) }], {
      pr: { number: 7, title: hostile, url: 'https://example.invalid/7' },
    }),
  );

  assert.ok(!html.includes('<script>alert(1)</script>'), 'the raw script tag must not appear anywhere');
  assert.match(html, /<title>Design review — PR #7: &lt;\/title&gt;/);
});

test('the browser and Node comparisons start from the same threshold', async () => {
  // The rule is implemented twice on purpose — Node so the PR comment can
  // quote a figure without rendering, the browser so the sensitivity slider
  // moves without a round trip. Nothing links them at runtime, so this is
  // what catches the two drifting apart and quietly disagreeing about how
  // much of a screen changed.
  const reportSource = await readFile(path.join(TOOL_DIR, 'assets', 'report.js'), 'utf8');
  const declared = reportSource.match(/const DEFAULT_THRESHOLD = ([\d.]+);/)?.[1];

  assert.ok(declared, 'report.js must declare DEFAULT_THRESHOLD for this to be checkable');
  assert.equal(Number(declared), DEFAULT_THRESHOLD);
});

test('the title names the pull request when there is one', () => {
  assert.equal(reportTitle(modelWith([], { pr: { number: 42, title: 'Refresh the nav' } })), 'Design review — PR #42: Refresh the nav');
  assert.equal(reportTitle(modelWith([])), 'Design review — redesign');
});
