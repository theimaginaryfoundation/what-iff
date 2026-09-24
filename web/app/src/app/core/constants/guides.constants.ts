import { environment } from '@environments/environment';

/**
 * Product guides on the docs site (whatiffchat-web), linked from in-app help hints and the
 * new-user welcome. Paths resolve against `environment.docsUrl`, so a deployment that points
 * docsUrl elsewhere (or blanks it) moves or hides every guide link at once.
 */
export type GuideKey = 'docs' | 'gettingStarted' | 'context' | 'continuity' | 'expressions' | 'advanced';

const GUIDE_PATHS: Record<Exclude<GuideKey, 'docs'>, string> = {
  gettingStarted: 'guides/getting-started.html',
  context: 'guides/how-context-works.html',
  continuity: 'guides/building-continuity.html',
  expressions: 'guides/expressions.html',
  advanced: 'guides/advanced-features.html',
};

/**
 * Absolute URL for a guide, or null when docs are not configured (empty, an unsubstituted
 * container placeholder like `__DOCS_URL__`, or not an http(s) URL). Callers hide the link on null.
 */
export function guideUrl(key: GuideKey, docsUrl: string = environment.docsUrl): string | null {
  const base = docsUrl?.trim();
  if (!base) return null;
  // Browser-only SPA (no SSR), so URL is always available; any parse failure just hides the link.
  try {
    const parsed = new URL(base);
    if (parsed.protocol !== 'https:' && parsed.protocol !== 'http:') return null;
    return key === 'docs' ? parsed.toString() : new URL(GUIDE_PATHS[key], parsed).toString();
  } catch {
    return null;
  }
}
