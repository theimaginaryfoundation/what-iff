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
    // Failure here has two very different causes that produce an identical
    // empty report, so they are separated before returning. A typo'd or
    // unfetched ref is the overwhelmingly common one and is fixable in
    // seconds — but told only "no merge base", a reader reasonably concludes
    // their branch is fine and the tool is broken.
    let reason = 'no common ancestor — an orphan branch, or a clone shallow enough to have cut the fork point';
    try {
      await git(root, ['rev-parse', '--verify', `${baseRef}^{commit}`]);
    } catch {
      reason = `\`${baseRef}\` does not resolve to a commit here — check the spelling, or fetch it first`;
    }
    return { ref: baseRef, sha: null, reason };
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
 * Splits `-z` output into its NUL-delimited fields.
 *
 * Every path-bearing git command here is asked for `-z`, which is the only
 * way to get paths back verbatim. Without it git abbreviates a rename to
 * `dir/{old => new}/file`, and separately backslash-quotes any path
 * containing a space, a quote or a non-ASCII byte — so the "path" in the
 * output is a display string, not a path, and reconstructing the real one
 * from it is guesswork. An earlier version of this file guessed with a
 * regex and turned `web/app/src/{billing => payments}/invoice.html` into
 * `payments}/invoice.html`, a path that does not exist, reported as
 * modified rather than renamed.
 */
function nulFields(output) {
  const fields = output.split('\0');
  // A trailing NUL terminates the last record rather than starting a new one.
  if (fields.at(-1) === '') fields.pop();
  return fields;
}

/** `R100`/`C75` carry a similarity score; everything else is a bare letter. */
const STATUS_NAMES = { R: 'renamed', C: 'renamed', A: 'added', D: 'deleted', M: 'modified', T: 'modified' };

/**
 * Status per path from `--name-status -z`.
 *
 * Rename and copy records span three fields (code, old path, new path);
 * every other status spans two. The new path is what the report links to.
 */
function parseNameStatus(output) {
  const fields = nulFields(output);
  const statuses = new Map();
  for (let i = 0; i < fields.length; ) {
    const code = fields[i][0];
    if (code === 'R' || code === 'C') {
      statuses.set(fields[i + 2], STATUS_NAMES[code]);
      i += 3;
    } else {
      statuses.set(fields[i + 1], STATUS_NAMES[code] ?? 'modified');
      i += 2;
    }
  }
  return statuses;
}

/**
 * Line counts per path from `--numstat -z`.
 *
 * A record is `<added>\t<deleted>\t<path>`, except for a rename, where the
 * path is empty in that field and the old and new paths follow as their own
 * two fields. `-` for a count means binary, which is every baseline PNG.
 */
function parseNumstat(output) {
  const fields = nulFields(output);
  const entries = [];
  for (let i = 0; i < fields.length; ) {
    const [added, deleted, inlinePath] = fields[i].split('\t');
    const renamed = inlinePath === '';
    const file = renamed ? fields[i + 2] : inlinePath;
    entries.push({ path: file, added: added === '-' ? null : Number(added), deleted: deleted === '-' ? null : Number(deleted) });
    i += renamed ? 3 : 1;
  }
  return entries;
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

  const range = headRef === WORKTREE ? [baseSha] : [`${baseSha}..${headRef}`];
  const statuses = parseNameStatus(await git(root, ['diff', '--name-status', '-z', '-M', ...range, '--']));
  const files = parseNumstat(await git(root, ['diff', '--numstat', '-z', '-M', ...range, '--'])).map(entry => ({
    ...entry,
    status: statuses.get(entry.path) ?? 'modified',
  }));

  // Untracked files exist only when comparing against the working tree, and
  // they are how a brand-new screen's first baseline shows up before anyone
  // has staged it.
  if (headRef === WORKTREE) {
    const untracked = nulFields(await git(root, ['ls-files', '--others', '--exclude-standard', '-z']));
    for (const file of untracked) {
      files.push({ path: file, status: 'added', added: null, deleted: null, untracked: true });
    }
  }

  return files;
}
