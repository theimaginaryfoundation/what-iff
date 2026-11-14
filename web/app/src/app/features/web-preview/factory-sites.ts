import { init, parse } from 'es-module-lexer';

/**
 * Where a library's element factory was called (ADR 0x050 §5). Preact and React build elements
 * from `h(...)` / `createElement(...)` calls, but the library makes the DOM element later, in its
 * own code, after the call has returned: the page's record of who made an element (a stack of the
 * DOM call) then names the library, or the line that rendered the whole app. A project module's
 * import of the library is pointed, as the module is served, at a module of ours that is the
 * library with its factory wrapped: each call keeps where it was made, and the element the library
 * makes from it is given that place.
 *
 * Only the library's main entry is wrapped (bare `preact` or `react`, or that entry on a CDN), and
 * only in static imports; everything else it exports is the library's own, from the same module, so
 * the page has one copy of it.
 */

/** The libraries whose factory is wrapped. */
export type FactoryLibrary = 'preact' | 'react';

/** Where the wrappers are served, under the project's root. */
export const FACTORY_PATH = '__whatiff/factory/';

/**
 * The library a specifier is the main entry of: `preact`, `react`, or that entry on a CDN
 * (`https://esm.sh/preact@10`, `https://cdn.jsdelivr.net/npm/react@19/+esm`); null otherwise,
 * including a path inside the package (`preact/hooks`).
 */
export function factoryLibraryOf(specifier: string): FactoryLibrary | null {
  const bare = /^(preact|react)$/.exec(specifier);
  if (bare) return bare[1] as FactoryLibrary;
  let url: URL;
  try {
    url = new URL(specifier);
  } catch {
    return null;
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return null;
  const segments = url.pathname.split('/').filter(s => s && s !== '+esm');
  if (segments[0] === 'npm') segments.shift();
  if (segments.length !== 1) return null;
  const m = /^(preact|react)(@[^/]*)?$/.exec(segments[0]!);
  return m ? (m[1] as FactoryLibrary) : null;
}

/** The path, under the project's root, of the wrapper of `library` imported as `specifier`. */
export function factoryPath(library: FactoryLibrary, specifier: string): string {
  return `${FACTORY_PATH}${library}/${base64Url(specifier)}.js`;
}

/** The library and specifier a wrapper's path stands for; null when it is not one. */
export function factoryAt(path: string): { library: FactoryLibrary; specifier: string } | null {
  const m = /^__whatiff\/factory\/(preact|react)\/([A-Za-z0-9_-]+)\.js$/.exec(path);
  if (!m) return null;
  const specifier = fromBase64Url(m[2]!);
  const library = m[1] as FactoryLibrary;
  return specifier !== null && factoryLibraryOf(specifier) === library ? { library, specifier } : null;
}

let ready: Promise<void> | null = null;

/**
 * The module at `path` (in the project) with its static imports of a library's main entry pointed
 * at the wrapper; as it is when it has none or cannot be read. Only the specifier's text changes,
 * so no line moves.
 */
export async function rewriteFactoryImports(source: string, path: string): Promise<string> {
  ready ??= init;
  await ready;
  let imports: readonly { n: string | undefined; s: number; e: number; d: number }[];
  try {
    [imports] = parse(source);
  } catch {
    return source;
  }
  const up = '../'.repeat(path.split('/').length - 1) || './';
  let out = '';
  let at = 0;
  for (const imp of imports) {
    // Static imports only (d === -1): a dynamic import may run after the page has rendered.
    if (imp.d !== -1 || !imp.n) continue;
    const library = factoryLibraryOf(imp.n);
    if (!library) continue;
    out += source.slice(at, imp.s) + up + factoryPath(library, imp.n);
    at = imp.e;
  }
  return at === 0 ? source : out + source.slice(at);
}

/**
 * The wrapper module: the library, with `h` / `createElement` keeping where they were called for
 * each element they describe (by its props object, which is the element's own). The page's bridge
 * (`window.__whatiffSite`, page-bridge.ts) takes it from there. Without the bridge, the library is
 * as it is.
 */
export function factoryModule(library: FactoryLibrary, specifier: string): string {
  const from = JSON.stringify(specifier);
  const record = `const wiSite = typeof window !== 'undefined' ? window.__whatiffSite : undefined;
function wiWrap(factory) {
  if (!wiSite || typeof factory !== 'function') return factory;
  return function (type) {
    const element = factory.apply(this, arguments);
    if (typeof type === 'string' && element && element.props && typeof element.props === 'object') wiSite.props(element.props, new Error());
    return element;
  };
}
`;
  if (library === 'preact') {
    return `import { h as wiH, createElement as wiCreate, options as wiOptions } from ${from};
export * from ${from};
${record}export const h = wiWrap(wiH);
export const createElement = wiWrap(wiCreate);
// The element Preact made for a vnode is known once it has been diffed (vnode.__e, its _dom).
if (wiSite && wiOptions) {
  const wiDiffed = wiOptions.diffed;
  wiOptions.diffed = function (vnode) {
    const dom = vnode && (vnode.__e || vnode._dom);
    if (dom && vnode.props && typeof vnode.type === 'string') wiSite.element(dom, vnode.props);
    if (wiDiffed) wiDiffed.apply(this, arguments);
  };
}
`;
  }
  // React keeps an element's current props on its DOM node (__reactProps$…), so the bridge finds them there.
  return `import * as wiReact from ${from};
import { createElement as wiCreate } from ${from};
export * from ${from};
${record}export const createElement = wiWrap(wiCreate);
const wiDefault = wiReact.default;
export default wiDefault && typeof wiDefault === 'object' ? Object.assign({}, wiDefault, { createElement }) : wiDefault;
`;
}

function base64Url(text: string): string {
  const bytes = new TextEncoder().encode(text);
  let binary = '';
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function fromBase64Url(encoded: string): string | null {
  try {
    const binary = atob(encoded.replace(/-/g, '+').replace(/_/g, '/'));
    return new TextDecoder('utf-8', { fatal: true }).decode(Uint8Array.from(binary, c => c.charCodeAt(0)));
  } catch {
    return null;
  }
}
