/**
 * The "what did this PR actually touch" half of the report.
 *
 * A before/after gallery answers *how it looks*; this answers *how much of
 * the product is involved*, which is the question a designer gets asked in
 * review and currently has to answer by reading a diff. Everything here is
 * derived from file paths — no build graph, no AST — because a path-based
 * answer is one a reader can check at a glance, and a wrong-but-confident
 * dependency graph is worse than an obviously approximate list.
 */

import path from 'node:path';
import { isBaselinePath } from './screens.mjs';

const APP_SRC = 'web/app/src/';

/**
 * File categories, in the order they are presented.
 *
 * `weight` drives the "design surface" score: how much of the change is
 * something a designer owns. Templates, styles and assets are pure surface;
 * a component `.ts` is partly surface and partly logic; specs and backend
 * code are neither.
 */
const CATEGORIES = [
  { id: 'baseline', label: 'Visual baselines', weight: 0, test: p => isBaselinePath(p) },
  { id: 'style', label: 'Styles', weight: 1, test: p => /\.(css|scss|sass|less)$/.test(p) },
  { id: 'template', label: 'Templates', weight: 1, test: p => p.startsWith(APP_SRC) && p.endsWith('.html') },
  {
    id: 'asset',
    label: 'Assets',
    weight: 1,
    test: p => /\.(svg|png|jpe?g|gif|webp|avif|woff2?|ttf|otf)$/.test(p) && !isBaselinePath(p),
  },
  { id: 'spec', label: 'Tests', weight: 0, test: p => /\.spec\.ts$/.test(p) || p.includes('/e2e/') },
  { id: 'component', label: 'Components & logic', weight: 0.5, test: p => p.startsWith(APP_SRC) && p.endsWith('.ts') },
  { id: 'config', label: 'Config & build', weight: 0, test: p => /(^|\/)(package(-lock)?\.json|angular\.json|tsconfig[^/]*\.json|.*\.ya?ml)$/.test(p) },
  { id: 'docs', label: 'Docs', weight: 0, test: p => /\.(md|mdx)$/.test(p) },
  { id: 'other', label: 'Other', weight: 0, test: () => true },
];

/** First matching category — order above is the precedence, deliberately. */
export function categorize(relPath) {
  return CATEGORIES.find(category => category.test(relPath)) ?? CATEGORIES.at(-1);
}

/**
 * Grouping buckets whose own name says nothing about the product.
 *
 * `features/personality` and `shared/ui` are meaningful areas; `features`
 * and `shared` are just where the router and the component library happen
 * to live, and collapsing to them would put most of the app in one bucket
 * called "features" — which is the same as having no grouping at all.
 */
const CONTAINER_SEGMENTS = new Set(['features', 'shared', 'core', 'modules', 'pages', 'components']);

/**
 * Feature area a file belongs to: the first meaningful path segment under
 * the app's `app/` directory (`chat`, `personality`, `shared/ui`…).
 */
export function featureArea(relPath) {
  if (!relPath.startsWith(APP_SRC)) return null;
  const rest = relPath.slice(APP_SRC.length).replace(/^app\//, '');
  const segments = path.posix.dirname(rest).split('/').filter(segment => segment && segment !== '.');
  if (!segments.length) return 'app root';
  // Descend past container segments, but keep the container's name in the
  // label when it disambiguates — `shared/ui` reads better than a bare `ui`,
  // which could be either the design system or a feature's own folder.
  if (CONTAINER_SEGMENTS.has(segments[0]) && segments[1]) {
    return segments[0] === 'features' ? segments[1] : `${segments[0]}/${segments[1]}`;
  }
  return segments[0];
}

/**
 * Tokens from a path that are worth matching a screen name against —
 * lowercase words of 4+ characters, minus the boilerplate every Angular
 * filename carries.
 *
 * The length floor and the stop list exist for the same reason: without
 * them every screen "relates to" every file, via `app`, `ts` or `component`,
 * and a relevance hint that fires on everything is just noise with extra
 * steps.
 */
const STOP_WORDS = new Set(['component', 'components', 'service', 'services', 'spec', 'html', 'scss', 'index', 'shared', 'page', 'pages', 'app', 'src', 'web']);

function tokens(text) {
  return new Set(
    text
      .toLowerCase()
      .split(/[^a-z0-9]+/)
      .filter(word => word.length >= 4 && !STOP_WORDS.has(word)),
  );
}

/**
 * Changed files whose path shares a meaningful word with a screen's name or
 * spec.
 *
 * This is a *hint*, and the report labels it as one. It is a name match, not
 * a dependency: it will miss a shared component that has no word in common
 * with the screen it renders, and it will occasionally suggest a file that
 * turns out to be unrelated. Both are acceptable for what it is used for —
 * putting "you probably want to look at these files" next to a screen that
 * changed — and neither would be fixed by a more elaborate heuristic that
 * merely fails less legibly.
 */
export function relatedFiles(screen, files) {
  const screenTokens = new Set([...tokens(screen.shot), ...tokens(path.posix.basename(screen.specPath))]);
  if (!screenTokens.size) return [];
  return files
    .filter(file => !isBaselinePath(file.path) && file.category !== 'spec')
    .filter(file => [...tokens(file.path)].some(word => screenTokens.has(word)))
    .map(file => file.path);
}

/**
 * Annotates changed files and rolls them up into the report's impact
 * section.
 */
export function summarizeImpact(files) {
  const annotated = files.map(file => {
    const category = categorize(file.path);
    return { ...file, category: category.id, categoryLabel: category.label, weight: category.weight, area: featureArea(file.path) };
  });

  const byCategory = CATEGORIES.map(category => {
    const matched = annotated.filter(file => file.category === category.id);
    return {
      id: category.id,
      label: category.label,
      count: matched.length,
      added: matched.reduce((sum, file) => sum + (file.added ?? 0), 0),
      deleted: matched.reduce((sum, file) => sum + (file.deleted ?? 0), 0),
    };
  }).filter(entry => entry.count > 0);

  const byArea = [...new Map(annotated.filter(file => file.area).map(file => [file.area, null])).keys()]
    .map(area => {
      const matched = annotated.filter(file => file.area === area);
      return {
        area,
        count: matched.length,
        churn: matched.reduce((sum, file) => sum + (file.added ?? 0) + (file.deleted ?? 0), 0),
        // An area is "design-heavy" when most of what changed in it is
        // template, style or asset rather than logic.
        designWeight: matched.reduce((sum, file) => sum + file.weight, 0) / matched.length,
      };
    })
    .sort((a, b) => b.churn - a.churn || b.count - a.count);

  // Share of the *frontend* change that is design surface. Backend files are
  // excluded from both sides rather than counted as zero: a PR that happens
  // to also touch Go should not read as "barely a design change" when every
  // frontend file in it is a stylesheet.
  const frontend = annotated.filter(file => file.path.startsWith(APP_SRC));
  const designShare = frontend.length ? frontend.reduce((sum, file) => sum + file.weight, 0) / frontend.length : 0;

  return { files: annotated, byCategory, byArea, designShare, frontendFileCount: frontend.length };
}
