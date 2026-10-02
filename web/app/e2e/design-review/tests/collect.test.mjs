/**
 * Collector tests. Run with `npm run design:review:test` (plain `node --test`
 * — this tool has no dependencies and its tests keep it that way).
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { collect } from '../collect.mjs';
import { FixtureRepo, SNAPSHOT_DIR } from './fixture-repo.mjs';
import { makePng } from './png-fixture.mjs';

const SPEC = `web/app/e2e/tests/visual/demo.visual.spec.ts`;

/** A repo with one committed screen on main, and a branch checked out. */
async function repoOnBranch() {
  const repo = await FixtureRepo.create();
  await repo.write(
    SPEC,
    [
      `import { test, expect } from '../../fixtures';`,
      ``,
      `/**`,
      ` * Static screens only — nothing downstream of a model reply.`,
      ` */`,
      `test.describe('demo screens', () => {`,
      `  test('the landing page', { tag: ['@visual'] }, async ({ page }) => {`,
      `    await expect(page).toHaveScreenshot('landing.png');`,
      `  });`,
      `});`,
    ].join('\n'),
  );
  await repo.write(`${SNAPSHOT_DIR}/landing-chromium-desktop-linux.png`, makePng(1280, 720, [20, 20, 20]));
  await repo.write(`${SNAPSHOT_DIR}/landing-chromium-mobile-linux.png`, makePng(412, 915, [20, 20, 20]));
  await repo.commit('seed');
  await repo.git('checkout', '-b', 'redesign');
  return repo;
}

test('an untouched branch reports every screen as unchanged', async t => {
  const repo = await repoOnBranch();
  t.after(() => repo.dispose());

  const model = await collect({ cwd: repo.root, baseRef: 'main' });

  assert.equal(model.summary.screens, 1);
  assert.equal(model.summary.unchanged, 1);
  assert.equal(model.summary.changed, 0);
  assert.deepEqual(
    model.screens[0].variants.map(variant => variant.label),
    ['Desktop', 'Mobile'],
    'desktop must sort before mobile so every pair reads the same way',
  );
});

test('a regenerated baseline is reported as changed, with the old image as "before"', async t => {
  const repo = await repoOnBranch();
  t.after(() => repo.dispose());

  await repo.write(`${SNAPSHOT_DIR}/landing-chromium-desktop-linux.png`, makePng(1280, 900, [240, 240, 240]));
  await repo.commit('restyle the landing page');

  const model = await collect({ cwd: repo.root, baseRef: 'main', headRef: 'HEAD' });

  assert.equal(model.summary.changed, 1);
  const [desktop, mobile] = model.screens[0].variants;
  assert.equal(desktop.status, 'changed');
  assert.equal(mobile.status, 'unchanged', 'a viewport whose baseline did not move must not be dragged along');
  // The before side has to come out of history, not off disk — this is the
  // whole reason the tool exists.
  assert.equal(desktop.before.height, 720);
  assert.equal(desktop.after.height, 900);
  assert.equal(model.screens[0].status, 'changed', 'a screen is as changed as its worst viewport');
});

test('uncommitted and untracked baselines are picked up by default', async t => {
  const repo = await repoOnBranch();
  t.after(() => repo.dispose());

  // Neither staged nor committed: the state a designer is actually in while
  // iterating, and the default the tool has to handle.
  await repo.write(`${SNAPSHOT_DIR}/landing-chromium-desktop-linux.png`, makePng(1280, 720, [99, 99, 99]));
  await repo.write(`${SNAPSHOT_DIR}/settings-chromium-desktop-linux.png`, makePng(1280, 720, [5, 5, 5]));

  const model = await collect({ cwd: repo.root, baseRef: 'main' });

  assert.equal(model.summary.changed, 1);
  assert.equal(model.summary.added, 1);
  const added = model.screens.find(screen => screen.shot === 'settings');
  assert.equal(added.status, 'added');
  assert.equal(added.variants[0].before, null, 'a new screen has no before side');
});

test('a deleted baseline is reported as removed, keeping the image that was lost', async t => {
  const repo = await repoOnBranch();
  t.after(() => repo.dispose());

  await repo.remove(`${SNAPSHOT_DIR}/landing-chromium-mobile-linux.png`);
  await repo.commit('drop the mobile baseline');

  const model = await collect({ cwd: repo.root, baseRef: 'main', headRef: 'HEAD' });

  assert.equal(model.summary.removed, 1);
  const mobile = model.screens[0].variants.find(variant => variant.form === 'mobile');
  assert.equal(mobile.status, 'removed');
  assert.ok(mobile.before, 'the removed image must still be shown — it is the only record of it left');
  assert.equal(mobile.after, null);
});

test('the spec supplies the screen title, group and doc comment', async t => {
  const repo = await repoOnBranch();
  t.after(() => repo.dispose());

  const model = await collect({ cwd: repo.root, baseRef: 'main' });
  const screen = model.screens[0];

  assert.equal(screen.title, 'The landing page');
  assert.equal(screen.group, 'demo screens');
  assert.match(screen.note, /Static screens only/);
});

test('changes on the base branch are not attributed to this one', async t => {
  const repo = await repoOnBranch();
  t.after(() => repo.dispose());

  // Someone else lands an unrelated screen on main after the fork point.
  await repo.git('checkout', 'main');
  await repo.write(`${SNAPSHOT_DIR}/unrelated-chromium-desktop-linux.png`, makePng(1280, 720, [7, 7, 7]));
  await repo.commit('someone else ships a screen');
  await repo.git('checkout', 'redesign');

  const model = await collect({ cwd: repo.root, baseRef: 'main', headRef: 'HEAD' });

  // Diffing against the tip of main would call `unrelated` "removed" by this
  // branch. Against the merge base — the correct comparison — this branch
  // changed nothing.
  assert.equal(model.summary.removed, 0);
  assert.equal(model.summary.changed, 0);
  assert.equal(model.summary.screens, 1);
});

test('design files no screen renders are listed as uncovered', async t => {
  const repo = await repoOnBranch();
  t.after(() => repo.dispose());

  await repo.write('web/app/src/app/features/billing/billing-page.component.html', '<section>invoices</section>');
  await repo.write('web/app/src/app/features/billing/billing-page.component.ts', 'export class BillingPage {}');
  await repo.commit('add a billing page');

  const model = await collect({ cwd: repo.root, baseRef: 'main', headRef: 'HEAD' });

  assert.deepEqual(
    model.impact.uncovered.map(file => file.path),
    ['web/app/src/app/features/billing/billing-page.component.html'],
    'only design surface is listed — a component .ts is not something a screenshot would have shown',
  );
  assert.equal(model.impact.uncovered[0].area, 'billing');
});

test('a missing merge base degrades to a gallery rather than failing', async t => {
  const repo = await repoOnBranch();
  t.after(() => repo.dispose());

  const model = await collect({ cwd: repo.root, baseRef: 'no-such-branch' });

  assert.equal(model.base.sha, null);
  assert.equal(model.summary.screens, 1);
  assert.equal(model.summary.added, 1, 'with nothing to compare against, every screen is new');
});
