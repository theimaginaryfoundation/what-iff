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
 * WHAT THIS CHECKS, precisely — the wording matters, because an earlier
 * version of this file claimed more than it delivered and the claim itself was
 * the bug. For each visual spec:
 *
 *   1. at least one `@functional-coverage` path is declared;
 *   2. every declared path resolves to a real, regular `.spec.ts` file
 *      (no symlinks, no directories);
 *   3. every resolved path is genuinely *inside* `tests/functional/` or
 *      `tests/journeys/` — compared after normalisation, so `../` and
 *      lookalike directory names cannot escape;
 *   4. every declared file declares at least one test that is not disabled
 *      in every environment, determined by parsing it (see
 *      ./lib/playwright-test-analysis.mjs), not by matching text.
 *
 * WHAT IT DOES NOT CHECK, and must not be described as checking:
 *
 *   - whether the named spec covers the *right* behaviour. That is a review
 *     question. The value here is that an author must name something and a
 *     reviewer must look at what was named.
 *   - whether the named spec actually ran in a given CI job. Config `grep`
 *     filters and project selection decide that, and only a real run knows.
 *
 * Usage: node e2e/scripts/check-visual-coverage.mjs
 */
import { existsSync, lstatSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';
import { analyzeSpec } from './lib/playwright-test-analysis.mjs';

// Resolved from this file, not from the cwd: the npm script runs from
// web/app and CI may not, and a path-relative check that silently finds
// nothing would pass by default.
const E2E_DIR = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const VISUAL_DIR = join(E2E_DIR, 'tests', 'visual');
const ALLOWED_ROOTS = [join(E2E_DIR, 'tests', 'functional'), join(E2E_DIR, 'tests', 'journeys')];

/** `@functional-coverage <path>[, <path>...]`, anywhere in the file's text. */
const DECLARATION = /@functional-coverage\s+([^\n*]+)/g;

/**
 * True when `target` is inside `root`. Compares normalised paths rather than
 * the raw string: a `startsWith` on the declared text admitted both
 * `tests/functional/../visual/x.visual.spec.ts` and a lookalike directory
 * named `tests/functional-whatever/`, which let a visual spec cite another
 * visual spec — the one thing this containment check exists to prevent.
 */
function isInside(root, target) {
  const rel = relative(root, target);
  return rel !== '' && !rel.startsWith('..' + sep) && rel !== '..' && !isAbsolute(rel);
}

const problems = [];
const note = (specPath, message) => problems.push(`${specPath}: ${message}`);

const visualSpecs = readdirSync(VISUAL_DIR)
  .filter(name => name.endsWith('.spec.ts'))
  .sort();

if (visualSpecs.length === 0) {
  console.error(`No visual specs found in ${VISUAL_DIR}.`);
  process.exit(1);
}

for (const name of visualSpecs) {
  const specPath = join('e2e', 'tests', 'visual', name);
  const source = readFileSync(join(VISUAL_DIR, name), 'utf8');

  const declared = [];
  for (const match of source.matchAll(DECLARATION)) {
    for (const raw of match[1].split(',')) {
      const path = raw.trim();
      if (path) declared.push(path);
    }
  }

  if (declared.length === 0) {
    note(
      specPath,
      'no @functional-coverage declaration.\n' +
        "    Add one naming the functional spec that covers this area, e.g.\n" +
        "    ' * @functional-coverage tests/functional/<area>/<file>.spec.ts'\n" +
        '    If no such spec exists, write it — a baseline is not coverage on its own.',
    );
    continue;
  }

  for (const declaredPath of declared) {
    if (isAbsolute(declaredPath)) {
      note(specPath, `@functional-coverage '${declaredPath}' must be relative to e2e/, not absolute.`);
      continue;
    }

    const target = resolve(E2E_DIR, declaredPath);

    if (!ALLOWED_ROOTS.some(root => isInside(root, target))) {
      note(
        specPath,
        `@functional-coverage '${declaredPath}' resolves outside tests/functional/ and ` +
          'tests/journeys/. A visual spec cannot cover another visual spec.',
      );
      continue;
    }

    if (!declaredPath.endsWith('.spec.ts')) {
      note(specPath, `@functional-coverage '${declaredPath}' is not a .spec.ts file.`);
      continue;
    }

    if (!existsSync(target)) {
      note(specPath, `@functional-coverage '${declaredPath}' does not exist.`);
      continue;
    }

    // lstat, not stat: a symlink could otherwise point anywhere while the
    // containment check above only saw the link's own tidy path.
    const stat = lstatSync(target);
    if (!stat.isFile()) {
      note(specPath, `@functional-coverage '${declaredPath}' is not a regular file (symlinks are not accepted).`);
      continue;
    }

    let analysis;
    try {
      analysis = analyzeSpec(target, readFileSync(target, 'utf8'));
    } catch (err) {
      // Fail closed. An unparseable target is not evidence of coverage.
      note(specPath, `@functional-coverage '${declaredPath}' could not be parsed: ${err.message}`);
      continue;
    }

    if (analysis.declared === 0) {
      note(specPath, `@functional-coverage '${declaredPath}' declares no tests.`);
      continue;
    }

    if (analysis.runnable === 0) {
      note(
        specPath,
        `@functional-coverage '${declaredPath}' declares ${analysis.declared} test(s), all disabled in\n` +
          '    every environment (test.skip(true, ...), test.describe.skip, or fixme), so it runs nowhere.\n' +
          '    Narrow a flaky test with a tag (@mock-only) instead of disabling it outright,\n' +
          '    or point at a spec that runs.',
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
