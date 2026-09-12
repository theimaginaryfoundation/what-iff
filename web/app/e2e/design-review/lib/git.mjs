/**
 * Git access for the design review report.
 *
 * Two things shape this file:
 *
 * 1. **The "before" side of every comparison comes out of git, not out of a
 *    test run.** When a designer updates a baseline they commit the new PNG,
 *    so by the time anyone reviews the PR `toHaveScreenshot()` passes and the
 *    Playwright report shows nothing at all — the intended design change is
 *    invisible precisely because it was done properly. Reading the old blob
 *    at the merge base is what makes that change visible again, and it costs
 *    no browser, no backend and no Docker.
 *
 * 2. **The head side defaults to the working tree, not to HEAD.** A designer
 *    iterating on a change wants to see it before committing, so an
 *    uncommitted or even untracked baseline has to show up in the report.
 */

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { readFile } from 'node:fs/promises';
import path from 'node:path';

const execFileAsync = promisify(execFile);

/** Sentinel `head` ref meaning "whatever is on disk right now". */
export const WORKTREE = Symbol.for('design-review.worktree');

/**
 * Runs git and returns stdout.
 *
 * `execFile` rather than `exec`: every caller below interpolates a ref or a
 * pathspec that came from a command line, and argv-passing means a branch
 * named something shell-hostile is just a string.
 *
 * `maxBuffer` is raised because `git show <ref>:<png>` streams a whole
 * baseline image through stdout, and the default 1MB cap truncates a
 * full-page desktop screenshot into a corrupt buffer rather than failing
 * loudly.
 */
async function git(repoRoot, args, { encoding = 'utf8' } = {}) {
  const { stdout } = await execFileAsync('git', ['-C', repoRoot, ...args], {
    encoding,
    maxBuffer: 64 * 1024 * 1024,
  });
  return stdout;
}

/** Absolute path of the repository containing `cwd`. */
export async function repoRoot(cwd) {
  const out = await execFileAsync('git', ['-C', cwd, 'rev-parse', '--show-toplevel'], { encoding: 'utf8' });
  return out.stdout.trim();
}

/**
 * Resolves the commit a comparison should treat as "before".
 *
 * This is the merge base, never the tip of the base branch. Diffing against
 * the tip attributes every unrelated screen someone else changed on main to
 * *this* PR, which is the fastest way to make a review report untrustworthy.
 */
export async function resolveBase(root, baseRef, headRef) {
  const head = headRef === WORKTREE ? 'HEAD' : headRef;
  try {
    const sha = (await git(root, ['merge-base', baseRef, head])).trim();
    return { ref: baseRef, sha };
  } catch {
    // No common ancestor: an orphan branch, a shallow clone whose history
    // was cut above the fork point, or a base ref that does not exist
    // locally. Report it instead of silently comparing against nothing.
    return { ref: baseRef, sha: null };
  }
}

/** `{ ref, sha, subject, author }` for a ref, or the working tree. */
export async function describe(root, ref) {
  if (ref === WORKTREE) {
    const sha = (await git(root, ['rev-parse', 'HEAD'])).trim();
    const branch = (await git(root, ['rev-parse', '--abbrev-ref', 'HEAD'])).trim();
    return { ref: 'working tree', sha, branch, subject: 'uncommitted working tree', worktree: true };
  }
  const sha = (await git(root, ['rev-parse', ref])).trim();
  const subject = (await git(root, ['log', '-1', '--format=%s', sha])).trim();
  const author = (await git(root, ['log', '-1', '--format=%an', sha])).trim();
  return { ref, sha, subject, author, worktree: false };
}

/**
 * Reads a file's bytes at a ref, or null when it does not exist there.
 *
 * Null is the load-bearing case, not an error path: it is how an added
 * screen ("no before") and a deleted screen ("no after") are told apart
 * from a changed one.
 */
export async function readBlob(root, ref, relPath) {
  if (ref === WORKTREE) {
    try {
      return await readFile(path.join(root, relPath));
    } catch {
      return null;
    }
  }
  try {
    return await git(root, ['show', `${ref}:${relPath}`], { encoding: 'buffer' });
  } catch {
    return null;
  }
}

/**
 * Files that differ between `baseSha` and the head, as
 * `{ path, status, added, deleted }`.
 *
 * `-M` so a renamed component reads as a rename rather than as a delete plus
 * an unrelated add. Binary files report `added`/`deleted` as null, which is
 * every baseline PNG — the report shows a pixel delta for those instead, and
 * a fake line count would be worse than an honest absence.
 */
export async function changedFiles(root, baseSha, headRef) {
  if (!baseSha) return [];

  const numstatArgs =
    headRef === WORKTREE
      ? ['diff', '--numstat', '-M', baseSha, '--']
      : ['diff', '--numstat', '-M', `${baseSha}..${headRef}`, '--'];
  const statusArgs =
    headRef === WORKTREE
      ? ['diff', '--name-status', '-M', baseSha, '--']
      : ['diff', '--name-status', '-M', `${baseSha}..${headRef}`, '--'];

  const statusByPath = new Map();
  for (const line of (await git(root, statusArgs)).split('\n')) {
    if (!line) continue;
    const parts = line.split('\t');
    const code = parts[0][0];
    // Rename/copy entries carry both the old and the new path; the new one
    // is what the report links to.
    const file = parts.length > 2 ? parts[2] : parts[1];
    statusByPath.set(file, { R: 'renamed', A: 'added', D: 'deleted', M: 'modified' }[code] ?? 'modified');
  }

  const files = [];
  for (const line of (await git(root, numstatArgs)).split('\n')) {
    if (!line) continue;
    const [added, deleted, ...rest] = line.split('\t');
    // A rename's numstat path field is "old => new" (or an elided form with
    // braces); the name-status pass above already recorded the real new
    // path, so prefer a key it knows.
    const raw = rest.join('\t');
    const file = statusByPath.has(raw) ? raw : (raw.match(/\{.*? => (.*?)\}|.* => (.*)/)?.slice(1).find(Boolean) ?? raw);
    files.push({
      path: file,
      status: statusByPath.get(file) ?? 'modified',
      added: added === '-' ? null : Number(added),
      deleted: deleted === '-' ? null : Number(deleted),
    });
  }

  // Untracked files exist only when comparing against the working tree, and
  // they are how a brand-new screen's first baseline shows up before anyone
  // has staged it.
  if (headRef === WORKTREE) {
    const untracked = (await git(root, ['ls-files', '--others', '--exclude-standard'])).split('\n').filter(Boolean);
    for (const file of untracked) {
      files.push({ path: file, status: 'added', added: null, deleted: null, untracked: true });
    }
  }

  return files;
}
