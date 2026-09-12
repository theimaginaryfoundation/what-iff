/**
 * Enforces: a visual spec may not be the only coverage of an area.
 *
 * A `toHaveScreenshot()` baseline answers "does this still look the way it
 * looked?" — nothing more. It cannot tell you the screen still works, and it
 * passes happily against a screen that renders identically while doing
 * nothing. Standing a baseline up on an area with no functional spec behind it
 * buys a false sense of coverage: the pixels are pinned, the behaviour is not,
 * and the first person to read the suite sees a green visual row and assumes
 * the feature is tested.
 *
 * The rule is therefore: every file in `tests/visual/` names the functional
 * spec(s) that cover the same area, in a machine-readable header:
 *
 *   @functional-coverage tests/functional/chat/emoji-shortcodes.spec.ts
 *
 * Multiple paths may be comma-separated or given on repeated lines. Paths are
 * relative to `e2e/`.
 *
 * This check verifies, for each visual spec:
 *   1. at least one `@functional-coverage` path is declared;
 *   2. every declared path exists;
 *   3. every declared path is under `tests/functional/` or `tests/journeys/`
 *      — a visual spec cannot satisfy the rule by pointing at another visual
 *      spec, which is the obvious way to defeat it;
 *   4. every declared file actually contains at least one test;
 *   5. the declared file is not skipped in its entirety. A blanket
 *      `test.skip(true, ...)` on every test is how this rule was broken the
 *      first time: the Context X-ray's functional test was skipped in every
 *      environment while its visual spec kept running, so the area had a
 *      baseline and no behavioural coverage at all.
 *
 * Comments are stripped before any of the pattern matching below, so prose
 * *about* a skip (like the paragraph above) does not read as one.
 *
 * What it deliberately does NOT do is judge whether the referenced spec
 * covers the *right* behaviour. That is a review question, not a script
 * question. The declaration's value is that it forces the author to name
 * something, and the reviewer to look at what was named.
 *
 * Usage: node e2e/scripts/check-visual-coverage.mjs
 */
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

// Resolved from this file, not from the cwd: the npm script runs from
// web/app and CI may not, and a path-relative check that silently finds
// nothing would pass by default.
const E2E_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const VISUAL_DIR = join(E2E_DIR, 'tests', 'visual');
const ALLOWED_PREFIXES = [join('tests', 'functional'), join('tests', 'journeys')];

/** `@functional-coverage <path>[, <path>...]`, anywhere in the file's text. */
const DECLARATION = /@functional-coverage\s+([^\n*]+)/g;

/** A `test(...)` / `test.describe(...)` call, however it's indented. */
const HAS_TEST = /(^|[^.\w])test(\.describe)?\s*\(/g;

/** An unconditional `test.skip(true, ...)` — a skip in every environment. */
const BLANKET_SKIP = /test\.skip\(\s*true\b/g;

/**
 * Strips block and line comments. Crude on purpose — it does not parse
 * strings or regex literals — but the only thing it feeds is a count of
 * `test(` and `test.skip(true` occurrences, where a comment is the one source
 * of false positives that actually shows up (a doc comment explaining why
 * something used to be skipped).
 */
function stripComments(source) {
  return source.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
}

function count(source, pattern) {
  return [...source.matchAll(pattern)].length;
}

const problems = [];

const visualSpecs = readdirSync(VISUAL_DIR)
  .filter(name => name.endsWith('.spec.ts'))
  .sort();

if (visualSpecs.length === 0) {
  console.error(`No visual specs found in ${VISUAL_DIR}.`);
  process.exit(1);
}

for (const name of visualSpecs) {
  const specPath = join('e2e', relative(E2E_DIR, join(VISUAL_DIR, name)));
  const source = readFileSync(join(VISUAL_DIR, name), 'utf8');

  const declared = [];
  for (const match of source.matchAll(DECLARATION)) {
    for (const raw of match[1].split(',')) {
      const path = raw.trim();
      if (path) declared.push(path);
    }
  }

  if (declared.length === 0) {
    problems.push(
      `${specPath}: no @functional-coverage declaration.\n` +
        `    Add one naming the functional spec that covers this area, e.g.\n` +
        `    ' * @functional-coverage tests/functional/<area>/<file>.spec.ts'\n` +
        `    If no such spec exists, write it — a baseline is not coverage on its own.`,
    );
    continue;
  }

  for (const declaredPath of declared) {
    if (!ALLOWED_PREFIXES.some(prefix => declaredPath.startsWith(prefix))) {
      problems.push(
        `${specPath}: @functional-coverage '${declaredPath}' is not under ` +
          `${ALLOWED_PREFIXES.join(' or ')}. A visual spec cannot cover another visual spec.`,
      );
      continue;
    }

    const target = join(E2E_DIR, declaredPath);
    if (!existsSync(target)) {
      problems.push(`${specPath}: @functional-coverage '${declaredPath}' does not exist.`);
      continue;
    }

    const targetSource = stripComments(readFileSync(target, 'utf8'));
    const tests = count(targetSource, HAS_TEST);
    if (tests === 0) {
      problems.push(`${specPath}: @functional-coverage '${declaredPath}' contains no tests.`);
      continue;
    }

    // Every test carrying an unconditional skip means the file runs nowhere.
    // A file where only *some* tests are skipped still provides coverage, so
    // it passes — judging which of those skips are justified is a review
    // question, not one a regex should answer.
    if (count(targetSource, BLANKET_SKIP) >= tests) {
      problems.push(
        `${specPath}: @functional-coverage '${declaredPath}' is skipped in its ` +
          `entirety by unconditional test.skip(true, ...), so it does not run anywhere.\n` +
          `    Narrow it with a tag (@mock-only) instead of skipping it outright, ` +
          `or point at a spec that runs.`,
      );
    }
  }
}

console.log(`Visual specs checked: ${visualSpecs.length}`);

if (problems.length > 0) {
  console.error(`\n${problems.length} visual spec(s) without functional coverage:\n`);
  for (const problem of problems) console.error(`  ✗ ${problem}\n`);
  console.error('Rule: a screenshot may not be the only coverage of an area.');
  console.error('See e2e/README.md, "Visual regression".');
  process.exit(1);
}

console.log('✅ every visual spec declares a functional spec that runs');
