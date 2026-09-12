/**
 * Unit tests for the pure pieces: path parsing, file classification and the
 * name-match heuristic.
 *
 * These pin behaviour that is easy to change by accident and impossible to
 * notice — a report with a slightly worse heuristic still renders, still
 * looks right, and quietly stops pointing at the correct files.
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import { parseBaselinePath, titleize, isBaselinePath } from '../lib/screens.mjs';
import { categorize, featureArea, relatedFiles, summarizeImpact } from '../lib/impact.mjs';
import { readPngSize } from '../lib/png.mjs';
import { makePng } from './png-fixture.mjs';

test('baseline paths decompose into spec, shot and project', () => {
  const parsed = parseBaselinePath('web/app/e2e/tests/visual/auth.visual.spec.ts-snapshots/login-page-chromium-desktop-linux.png');

  assert.equal(parsed.specPath, 'web/app/e2e/tests/visual/auth.visual.spec.ts');
  assert.equal(parsed.shot, 'login-page');
  assert.equal(parsed.project, 'chromium-desktop');
  assert.equal(parsed.platform, 'linux');
});

test('a shot name containing a project-like word is still split at the real boundary', () => {
  // The shot below ends in "-mobile", which a greedy split would hand to the
  // project field and leave the report with a project called "mobile".
  const parsed = parseBaselinePath('e2e/tests/visual/nav.visual.spec.ts-snapshots/sidebar-mobile-chromium-mobile-linux.png');

  assert.equal(parsed.shot, 'sidebar-mobile');
  assert.equal(parsed.project, 'chromium-mobile');
});

test('non-baseline files in a snapshots directory are skipped, not guessed at', () => {
  assert.equal(parseBaselinePath('e2e/tests/visual/x.spec.ts-snapshots/scratch.png'), null);
  assert.equal(parseBaselinePath('web/app/src/assets/logo.png'), null);
  assert.equal(isBaselinePath('web/app/src/assets/logo.png'), false);
});

test('titles capitalise only the first word, leaving names as written', () => {
  assert.equal(titleize('chat-composer-empty'), 'Chat composer empty');
  assert.equal(titleize('oauth-SSO-prompt'), 'Oauth SSO prompt');
});

test('file categories follow precedence, not just extension', () => {
  assert.equal(categorize('web/app/e2e/tests/visual/a.spec.ts-snapshots/x-chromium-desktop-linux.png').id, 'baseline');
  // Same extension, different meaning: an app asset is design surface, a
  // baseline is evidence about it.
  assert.equal(categorize('web/app/src/assets/logo.png').id, 'asset');
  assert.equal(categorize('web/app/src/app/features/chat/chat.component.html').id, 'template');
  assert.equal(categorize('web/app/src/app/features/chat/chat.component.scss').id, 'style');
  assert.equal(categorize('web/app/src/app/features/chat/chat.component.ts').id, 'component');
  // A component's own unit test is a test first, even though it is app source.
  assert.equal(categorize('web/app/src/app/features/chat/chat.component.spec.ts').id, 'spec');
  assert.equal(categorize('internal/handlers/chat.go').id, 'other');
});

test('feature areas descend past container directories', () => {
  assert.equal(featureArea('web/app/src/app/features/personality/detail/x.ts'), 'personality');
  assert.equal(featureArea('web/app/src/app/shared/ui/modal/modal.component.ts'), 'shared/ui');
  assert.equal(featureArea('web/app/src/app/app.ts'), 'app root');
  assert.equal(featureArea('internal/handlers/chat.go'), null, 'backend files have no frontend area');
});

test('related files match on meaningful words only', () => {
  const { files } = summarizeImpact([
    { path: 'web/app/src/app/features/personality/detail/personality-detail-page.component.html', status: 'modified', added: 3, deleted: 1 },
    { path: 'web/app/src/app/features/chat/chat-composer.component.ts', status: 'modified', added: 2, deleted: 0 },
    { path: 'web/app/src/app/shared/ui/button/button.component.ts', status: 'modified', added: 1, deleted: 0 },
  ]);

  const related = relatedFiles({ shot: 'personality-detail', specPath: 'web/app/e2e/tests/visual/personalities.visual.spec.ts' }, files);

  assert.deepEqual(related, ['web/app/src/app/features/personality/detail/personality-detail-page.component.html']);
  // `button.component.ts` shares only stop words with every screen name. If
  // it started matching, the hint would fire on everything and mean nothing.
  assert.ok(!related.some(file => file.includes('button')));
});

test('design share counts frontend files only', () => {
  const impact = summarizeImpact([
    { path: 'web/app/src/app/features/chat/chat.component.html', status: 'modified', added: 10, deleted: 2 },
    { path: 'web/app/src/app/features/chat/chat.component.scss', status: 'modified', added: 4, deleted: 0 },
    { path: 'internal/handlers/chat.go', status: 'modified', added: 120, deleted: 30 },
  ]);

  // Two of two frontend files are pure surface. The Go file is excluded
  // rather than counted as zero — otherwise a PR that happens to touch the
  // backend would read as barely a design change.
  assert.equal(impact.designShare, 1);
  assert.equal(impact.frontendFileCount, 2);
});

test('PNG dimensions come from the header; anything else reads as unknown', () => {
  assert.deepEqual(readPngSize(makePng(412, 915)), { width: 412, height: 915 });
  assert.equal(readPngSize(Buffer.from('not a png at all')), null);
  // A truncated file — the shape a mangled binary merge leaves behind.
  assert.equal(readPngSize(makePng(8, 8).subarray(0, 16)), null);
});
