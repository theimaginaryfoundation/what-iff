/**
 * PR-comment tests.
 *
 * The comment is the part most people will ever see, and it is posted
 * automatically — so a wrong headline is a wrong headline on every PR until
 * someone notices. These pin the claims it makes.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { renderMarkdown } from '../markdown.mjs';

function variant(project, label, order, status, ratio) {
  return { project, label, order, form: label.toLowerCase(), status, diff: ratio == null ? null : { ratio, changed: 1, total: 100 } };
}

function model({ screens = [], uncovered = [], base = { ref: 'origin/main', sha: 'a'.repeat(40) } } = {}) {
  const count = status => screens.filter(screen => screen.status === status).length;
  return {
    base,
    head: { ref: 'HEAD', sha: 'b'.repeat(40), worktree: false },
    screens,
    impact: { uncovered },
    summary: {
      screens: screens.length,
      changed: count('changed'),
      added: count('added'),
      removed: count('removed'),
      unchanged: count('unchanged'),
    },
  };
}

const changedScreen = {
  title: 'Personality detail',
  status: 'changed',
  variants: [variant('chromium-desktop', 'Desktop', 0, 'changed', 0.112), variant('chromium-mobile', 'Mobile', 1, 'unchanged')],
};

test('the headline states the verdict, because it is often all that is read', () => {
  assert.match(renderMarkdown(model({ screens: [changedScreen] })), /^### Design review · 1 screen changed$/m);
  assert.match(
    renderMarkdown(model({ screens: [{ title: 'Login', status: 'unchanged', variants: [variant('chromium-desktop', 'Desktop', 0, 'unchanged')] }] })),
    /^### Design review · no screen changed$/m,
  );
});

test('the table shows a percentage per changed viewport and a marker otherwise', () => {
  const markdown = renderMarkdown(model({ screens: [changedScreen] }));

  assert.match(markdown, /\| Screen \| Desktop \| Mobile \|/);
  assert.match(markdown, /\| Personality detail \| \*\*11\.2%\*\* \| · \|/);
});

test('a very small change keeps enough precision to not read as zero', () => {
  const tiny = { title: 'Modal', status: 'changed', variants: [variant('chromium-desktop', 'Desktop', 0, 'changed', 0.00088)] };

  // Rounded to one decimal this is "0.1%", and at two it is "0.09%" — either
  // is fine, but "0%" next to the word "changed" reads as a bug.
  assert.match(renderMarkdown(model({ screens: [tiny] })), /0\.088%|0\.09%/);
});

test('unchanged screens never reach the table', () => {
  const markdown = renderMarkdown(
    model({ screens: [changedScreen, { title: 'Login', status: 'unchanged', variants: [variant('chromium-desktop', 'Desktop', 0, 'unchanged')] }] }),
  );

  assert.ok(!markdown.includes('| Login |'));
  assert.match(markdown, /1 other screen unchanged/);
});

test('uncovered design files are called out rather than folded away', () => {
  const markdown = renderMarkdown(
    model({ screens: [changedScreen], uncovered: [{ path: 'web/app/src/app/features/billing/billing-page.component.html', area: 'billing' }] }),
  );

  assert.match(markdown, /⚠️ \*\*1 design file changed that no covered screen renders/);
  assert.match(markdown, /`features\/billing\/billing-page\.component\.html`/);
  // Not inside a <details>: this is the most actionable line in the comment
  // and the least likely to be opened if hidden.
  assert.ok(!/<details>[\s\S]*billing/.test(markdown));
});

test('a long uncovered list is capped so the comment stays readable', () => {
  const uncovered = Array.from({ length: 14 }, (unused, index) => ({ path: `web/app/src/app/features/f${index}/page.component.html`, area: `f${index}` }));

  const markdown = renderMarkdown(model({ screens: [changedScreen], uncovered }));

  assert.equal(markdown.match(/^- `/gm).length, 10);
  assert.match(markdown, /…and 4 more/);
});

test('a missing merge base is stated, not silently rendered as "everything is new"', () => {
  const markdown = renderMarkdown(model({ screens: [changedScreen], base: { ref: 'origin/main', sha: null } }));

  assert.match(markdown, /No merge base with `origin\/main`/);
});

test('the report link is included only when there is somewhere to point', () => {
  assert.ok(!renderMarkdown(model({ screens: [changedScreen] })).includes('Open the before/after report'));
  assert.match(
    renderMarkdown(model({ screens: [changedScreen] }), { reportLink: 'https://example.invalid/run/1' }),
    /\[Open the before\/after report →\]\(https:\/\/example\.invalid\/run\/1\)/,
  );
});
