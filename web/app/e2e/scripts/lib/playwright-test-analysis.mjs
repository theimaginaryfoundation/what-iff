/**
 * Answers one question about a Playwright spec file: does it declare at least
 * one test that can actually run somewhere?
 *
 * This exists because the question cannot be answered with regexes, which is
 * how it was answered first and why it was wrong. Three disabling shapes got
 * through the regex version, all verified:
 *
 *   - a test disabled by `test.skip(true, ...)` *inside a describe block*
 *     passed, because the same pattern that matched `test(` also matched
 *     `test.describe(`, inflating the test count past the skip count;
 *   - `test.describe.skip(...)` was invisible entirely, since only the
 *     literal `test.skip(true` was recognised as disabling;
 *   - `test.fixme` in either position was likewise invisible.
 *
 * Disabling in Playwright is inherited and positional, which is structure, not
 * text. So this walks the TypeScript AST instead.
 *
 * Deliberately NOT handled, because it is not decidable statically and
 * pretending otherwise is what caused the original bug:
 *
 *   - `test.skip(someCondition)` — a *conditional* skip is environment
 *     dependent. A test skipped on one project and run on another is real
 *     coverage, so these count as runnable.
 *   - `grep`/`grepInvert` config filters and project selection. A tag can
 *     exclude a test from one config while another runs it; only a real run
 *     knows. Out of scope, and the caller must not claim otherwise.
 */
import ts from 'typescript';

/** `test` / `test.skip` / `test.describe.skip` -> ['test','describe','skip']. */
function calleeParts(node) {
  const parts = [];
  let cur = node.expression;
  while (ts.isPropertyAccessExpression(cur)) {
    parts.unshift(cur.name.text);
    cur = cur.expression;
  }
  if (!ts.isIdentifier(cur)) return null;
  parts.unshift(cur.text);
  return parts;
}

const DISABLING = new Set(['skip', 'fixme']);

/**
 * A call *declares* something when its first argument is a string —
 * `test('...')`, `test.describe('...')`. That is what separates
 * `test.skip('name', fn)` (a declared-but-disabled test) from
 * `test.skip(true, 'reason')` (a runtime skip of the enclosing test). The two
 * look identical to a regex, which is exactly why the regex version miscounted.
 */
function firstArgIsString(node) {
  const [first] = node.arguments;
  return !!first && (ts.isStringLiteral(first) || ts.isNoSubstitutionTemplateLiteral(first));
}

function firstArgIsLiteralTrue(node) {
  const [first] = node.arguments;
  return !!first && first.kind === ts.SyntaxKind.TrueKeyword;
}

/**
 * An unconditional `test.skip(true, ...)` / `test.fixme(true, ...)` in a
 * test's own body takes it out of every environment. Scoped to that test's
 * arguments so a sibling's skip is never attributed to it.
 */
function bodyAlwaysSkips(testCall) {
  let found = false;
  const scan = node => {
    if (found) return;
    if (ts.isCallExpression(node)) {
      const parts = calleeParts(node);
      if (parts && parts[0] === 'test' && DISABLING.has(parts[parts.length - 1]) && firstArgIsLiteralTrue(node)) {
        found = true;
        return;
      }
    }
    node.forEachChild(scan);
  };
  for (const arg of testCall.arguments) scan(arg);
  return found;
}

/**
 * @returns {{declared: number, runnable: number}} — `declared` counts every
 * test in the file, `runnable` only those not disabled in every environment.
 */
export function analyzeSpec(fileName, sourceText) {
  const source = ts.createSourceFile(fileName, sourceText, ts.ScriptTarget.Latest, true);
  let declared = 0;
  let runnable = 0;

  // `disabledDepth > 0` means we are inside a skipped or fixme'd describe, so
  // everything below it is disabled regardless of what it says about itself.
  const visit = (node, disabledDepth) => {
    let nextDepth = disabledDepth;

    if (ts.isCallExpression(node)) {
      const parts = calleeParts(node);
      if (parts && parts[0] === 'test') {
        const tail = parts.slice(1);
        const modifier = tail[tail.length - 1];
        const disabling = DISABLING.has(modifier);

        if (tail.includes('describe')) {
          if (disabling && firstArgIsString(node)) nextDepth = disabledDepth + 1;
        } else if (firstArgIsString(node)) {
          declared++;
          if (!disabling && disabledDepth === 0 && !bodyAlwaysSkips(node)) runnable++;
        }
      }
    }

    node.forEachChild(child => visit(child, nextDepth));
  };

  visit(source, 0);
  return { declared, runnable };
}
