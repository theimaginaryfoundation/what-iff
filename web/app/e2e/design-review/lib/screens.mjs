/**
 * Turns committed baseline PNGs into the "screens" the report is organised
 * around.
 *
 * Playwright's on-disk layout already encodes everything needed:
 *
 *   tests/visual/<spec>.spec.ts-snapshots/<shot>-<project>-<platform>.png
 *
 * so a screen is recovered from the path rather than from a hand-maintained
 * registry. That matters more than it looks: a registry is a second list to
 * update, and the one thing guaranteed about a second list is that the day
 * someone adds a screen they will update only the first. Deriving from the
 * filesystem means a new baseline appears in the report the moment it is
 * written, with no other edit anywhere.
 */

import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import { WORKTREE, readBlob } from './git.mjs';
import { readPngSize } from './png.mjs';

const execFileAsync = promisify(execFile);

/** Matches `<shot>-<project>-<platform>.png`, e.g. `login-page-chromium-desktop-linux.png`. */
const BASELINE_FILE = /^(?<shot>.+)-(?<project>chromium-desktop|chromium-mobile|webkit-mobile)-(?<platform>[a-z0-9]+)\.png$/;

/**
 * How each Playwright project is presented to a designer.
 *
 * Project names are an implementation detail of the config; "Desktop" and
 * "Mobile" are what someone reviewing a layout actually thinks in. The
 * `order` field is what keeps desktop on the left of every pair, rather than
 * whatever order the filesystem happened to hand back.
 */
export const PROJECTS = {
  'chromium-desktop': { label: 'Desktop', device: 'Desktop Chrome', form: 'desktop', order: 0 },
  'chromium-mobile': { label: 'Mobile', device: 'Pixel 7', form: 'mobile', order: 1 },
  'webkit-mobile': { label: 'Mobile (WebKit)', device: 'iPhone 15', form: 'mobile', order: 2 },
};

/** True for any path Playwright would have written a baseline to. */
export function isBaselinePath(relPath) {
  return relPath.includes('.spec.ts-snapshots/') && relPath.endsWith('.png');
}

/**
 * Splits a baseline path into its parts, or returns null if it doesn't look
 * like one.
 *
 * An unrecognised filename is skipped rather than guessed at. A stray PNG in
 * a snapshots directory is far more likely to be somebody's scratch file
 * than a screen, and inventing a project name for it would put a fake
 * viewport into the report.
 */
export function parseBaselinePath(relPath) {
  if (!isBaselinePath(relPath)) return null;
  const dir = path.posix.dirname(relPath);
  const file = path.posix.basename(relPath);
  const groups = BASELINE_FILE.exec(file)?.groups;
  if (!groups) return null;

  // `<spec>.spec.ts-snapshots` → `<spec>.spec.ts`, which is the spec that
  // owns this baseline and the file a reviewer would open.
  const specPath = path.posix.join(path.posix.dirname(dir), path.posix.basename(dir).replace(/-snapshots$/, ''));

  return { path: relPath, specPath, shot: groups.shot, project: groups.project, platform: groups.platform };
}

/** Every baseline path present at a ref (or on disk, for the working tree). */
export async function listBaselinePaths(root, ref) {
  if (ref === WORKTREE) {
    // `git ls-files` rather than a directory walk so the result respects
    // .gitignore, and `--others` so a baseline written but not yet staged —
    // the normal state mid-iteration — is still found.
    const { stdout } = await execFileAsync('git', ['-C', root, 'ls-files', '--cached', '--others', '--exclude-standard'], {
      encoding: 'utf8',
      maxBuffer: 32 * 1024 * 1024,
    });
    return stdout.split('\n').filter(isBaselinePath);
  }
  const { stdout } = await execFileAsync('git', ['-C', root, 'ls-tree', '-r', '--name-only', ref], {
    encoding: 'utf8',
    maxBuffer: 32 * 1024 * 1024,
  });
  return stdout.split('\n').filter(isBaselinePath);
}

/**
 * Human title for a screenshot name: `chat-composer-empty` → `Chat composer
 * empty`. Only the first word is capitalised, so an acronym or product name
 * that a designer wrote into the shot name survives as they wrote it.
 */
export function titleize(shot) {
  const words = shot.replace(/[-_]+/g, ' ').trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * Pulls the enclosing `test(...)` title — and the comment above it — out of a
 * spec, keyed by screenshot name.
 *
 * This is a regex pass over the source, not a parse. A real parse would need
 * a TypeScript AST for what is, at best, a nicety: the report is perfectly
 * usable showing `personality-detail` alone, and everything this adds is
 * extra context on a card. So every failure mode here is "return less",
 * never "throw" — a spec written in a shape this doesn't recognise simply
 * contributes no annotation.
 */
/**
 * Ceiling on the spec source this will scan.
 *
 * The extraction below is a handful of unanchored regexes run repeatedly
 * over slices of the file, which is fine for a spec (a few kilobytes) and
 * quadratic-ish on something pathological. No real spec comes close, so a
 * file past this size is a generated or vendored artifact that happens to
 * end in `.spec.ts` — and the correct response to it is to contribute no
 * annotations rather than to spend a minute proving it has none.
 */
const MAX_SPEC_BYTES = 512 * 1024;

export async function readSpecAnnotations(root, ref, specPath) {
  const blob = await readBlob(root, ref, specPath);
  if (!blob || blob.length > MAX_SPEC_BYTES) return new Map();
  const source = blob.toString('utf8');
  if (!source) return new Map();

  // Every block comment in the file, with the offset just past its `*/`.
  const comments = [...source.matchAll(/\/\*\*(?<body>[\s\S]*?)\*\//g)].map(match => ({
    end: match.index + match[0].length,
    text: match.groups.body
      .split('\n')
      .map(line => line.replace(/^\s*\*\s?/, '').trimEnd())
      .join('\n')
      .trim(),
  }));

  /**
   * The doc comment for the declaration starting at `declStart`, if there is
   * one. "Is one" means the comment ends within a couple of lines of the
   * declaration — far enough to tolerate a blank line or a decorator, close
   * enough that an unrelated comment from earlier in the file can't be
   * mistaken for documentation of this test.
   */
  const docFor = declStart => {
    if (declStart === undefined) return undefined;
    const candidate = comments.filter(comment => comment.end <= declStart).at(-1);
    if (!candidate) return undefined;
    return source.slice(candidate.end, declStart).trim() === '' ? candidate.text : undefined;
  };

  const annotations = new Map();
  const shotRe = /toHaveScreenshot\(\s*['"`](?<shot>[^'"`]+)\.png['"`]/g;
  for (const match of source.matchAll(shotRe)) {
    const before = source.slice(0, match.index);
    // The nearest preceding `test(` and `test.describe(` are the ones this
    // screenshot sits inside. `test.describe` is matched first and excluded
    // from the `test(` pattern so a describe title can't be read as a test
    // title in a file that has only describes.
    const testMatch = [...before.matchAll(/\btest(?!\.describe)(?:\.\w+)*\(\s*\n?\s*['"`](?<title>[^'"`]+)['"`]/g)].at(-1);
    const describeMatch = [...before.matchAll(/\btest\.describe\(\s*['"`](?<title>[^'"`]+)['"`]/g)].at(-1);

    annotations.set(match.groups.shot, {
      testTitle: testMatch?.groups?.title,
      describeTitle: describeMatch?.groups?.title,
      // A test's own comment wins; a describe's comment covers the whole
      // group and is the right fallback when the individual test has none.
      note: docFor(testMatch?.index) ?? docFor(describeMatch?.index),
    });
  }
  return annotations;
}
