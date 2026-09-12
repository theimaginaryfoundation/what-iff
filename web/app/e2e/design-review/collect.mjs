/**
 * Assembles the design review model: every screen the visual suite covers,
 * as it looked at the merge base and as it looks now, plus what the change
 * touched.
 *
 * The output is a plain object — no HTML, no file writing. That split is
 * what lets the same model be rendered as the report, dumped as JSON for a
 * CI job to assert on, or summarised as a PR comment, without any of those
 * re-deriving "which screens changed" for themselves and drifting.
 */

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import * as git from './lib/git.mjs';
import { readPngSize } from './lib/png.mjs';
import { listBaselinePaths, parseBaselinePath, readSpecAnnotations, titleize, PROJECTS } from './lib/screens.mjs';
import { summarizeImpact, relatedFiles } from './lib/impact.mjs';

const execFileAsync = promisify(execFile);

/**
 * Pull request context, when this is running somewhere `gh` is authenticated.
 *
 * Entirely optional. The report's whole value proposition is that it works
 * on a local branch before anything is pushed, so an unauthenticated `gh`, a
 * missing `gh`, or a branch with no PR yet all return null and cost the
 * reader nothing but a header line.
 */
async function readPullRequest(root, headRef) {
  if (headRef !== git.WORKTREE && !/^[\w./-]+$/.test(String(headRef))) return null;
  try {
    const { stdout } = await execFileAsync(
      'gh',
      ['pr', 'view', '--json', 'number,title,url,author,headRefName,baseRefName,isDraft,body'],
      { cwd: root, encoding: 'utf8', timeout: 15_000 },
    );
    const pr = JSON.parse(stdout);
    return { number: pr.number, title: pr.title, url: pr.url, author: pr.author?.login, head: pr.headRefName, base: pr.baseRefName, draft: pr.isDraft };
  } catch {
    return null;
  }
}

/** One rendered image on one side of a comparison, or null if absent there. */
function toImage(buffer, relPath) {
  if (!buffer) return null;
  const size = readPngSize(buffer);
  return { path: relPath, bytes: buffer.length, width: size?.width ?? null, height: size?.height ?? null, buffer };
}

/**
 * Status of a single baseline across the two sides.
 *
 * Byte equality is the test for "unchanged" rather than a pixel comparison.
 * Playwright writes baselines deterministically, so identical bytes mean an
 * identical image; and the inverse — different bytes, visually identical —
 * is not a false positive worth engineering away, because a re-encoded
 * baseline that renders the same is still something a reviewer should see
 * in the changed list and ask about.
 */
function variantStatus(before, after) {
  if (before && !after) return 'removed';
  if (!before && after) return 'added';
  return before.buffer.equals(after.buffer) ? 'unchanged' : 'changed';
}

/** Worst status across a screen's viewports — what the card badge shows. */
const STATUS_RANK = { changed: 3, added: 2, removed: 2, unchanged: 0 };
function rollUpStatus(variants) {
  return variants.reduce((worst, variant) => (STATUS_RANK[variant.status] > STATUS_RANK[worst] ? variant.status : worst), 'unchanged');
}

export async function collect({ cwd = process.cwd(), baseRef = 'origin/main', headRef = git.WORKTREE } = {}) {
  const root = await git.repoRoot(cwd);
  const base = await git.resolveBase(root, baseRef, headRef);
  const head = await git.describe(root, headRef);

  const baseSha = base.sha;
  // With no merge base there is nothing to compare against, but the report is
  // still worth producing: it degrades to a plain gallery of what the app
  // looks like now, which is what a fresh clone or an orphan branch can
  // honestly offer.
  const beforePaths = baseSha ? await listBaselinePaths(root, baseSha) : [];
  const afterPaths = await listBaselinePaths(root, headRef);
  const allPaths = [...new Set([...beforePaths, ...afterPaths])].sort();

  const files = await git.changedFiles(root, baseSha, headRef);
  const impact = summarizeImpact(files);

  // Annotations are read once per spec, from the head side — the current
  // wording of a test is what describes the current screen.
  const annotationCache = new Map();
  const annotationsFor = async specPath => {
    if (!annotationCache.has(specPath)) annotationCache.set(specPath, await readSpecAnnotations(root, headRef, specPath));
    return annotationCache.get(specPath);
  };

  const screensByShot = new Map();
  for (const relPath of allPaths) {
    const parsed = parseBaselinePath(relPath);
    if (!parsed) continue;

    const before = toImage(baseSha ? await git.readBlob(root, baseSha, relPath) : null, relPath);
    const after = toImage(await git.readBlob(root, headRef, relPath), relPath);
    if (!before && !after) continue;

    const key = `${parsed.specPath}::${parsed.shot}`;
    if (!screensByShot.has(key)) {
      const annotation = (await annotationsFor(parsed.specPath)).get(parsed.shot) ?? {};
      screensByShot.set(key, {
        id: key.replace(/[^\w.-]+/g, '_'),
        shot: parsed.shot,
        title: annotation.testTitle ? titleize(annotation.testTitle) : titleize(parsed.shot),
        group: annotation.describeTitle ?? null,
        note: annotation.note ?? null,
        specPath: parsed.specPath,
        variants: [],
      });
    }

    const project = PROJECTS[parsed.project] ?? { label: parsed.project, form: 'unknown', order: 99 };
    screensByShot.get(key).variants.push({
      project: parsed.project,
      label: project.label,
      device: project.device ?? null,
      form: project.form,
      order: project.order,
      path: relPath,
      status: variantStatus(before, after),
      before,
      after,
    });
  }

  const screens = [...screensByShot.values()]
    .map(screen => {
      screen.variants.sort((a, b) => a.order - b.order);
      screen.status = rollUpStatus(screen.variants);
      screen.related = relatedFiles(screen, impact.files);
      return screen;
    })
    // Changed screens first — the report opens on what a reviewer came for,
    // with the unchanged gallery below as context rather than as a wall to
    // scroll past.
    .sort((a, b) => STATUS_RANK[b.status] - STATUS_RANK[a.status] || a.specPath.localeCompare(b.specPath) || a.shot.localeCompare(b.shot));

  /**
   * Design-surface files this change touched that no screen in the report
   * points at.
   *
   * The most expensive failure of a visual regression suite is the quiet
   * one: a designer restyles a component, every baseline still passes, the
   * report is green, and nobody notices that the screen in question has no
   * baseline at all. Listing the uncovered files turns that silence into a
   * line the reviewer can read — and, often, into the next spec someone
   * writes.
   *
   * It leans on the same name-match heuristic as the per-screen "related
   * files", so it inherits the same caveat and states it in the report: a
   * file can be genuinely covered and still land here because nothing in
   * its path resembles the screen's name.
   */
  const mentioned = new Set(screens.flatMap(screen => screen.related));
  const uncovered = impact.files
    .filter(file => ['template', 'style', 'asset'].includes(file.category))
    .filter(file => !mentioned.has(file.path))
    .map(file => ({ path: file.path, area: file.area, category: file.category, added: file.added, deleted: file.deleted }));

  return {
    generatedAt: new Date().toISOString(),
    repoRoot: root,
    base,
    head,
    pr: await readPullRequest(root, headRef),
    screens,
    impact: { ...impact, uncovered },
    summary: {
      screens: screens.length,
      changed: screens.filter(screen => screen.status === 'changed').length,
      added: screens.filter(screen => screen.status === 'added').length,
      removed: screens.filter(screen => screen.status === 'removed').length,
      unchanged: screens.filter(screen => screen.status === 'unchanged').length,
    },
  };
}
