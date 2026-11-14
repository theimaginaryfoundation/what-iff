/**
 * A lone page's files (ADR 0x050 §1): the stylesheets, scripts, images and media an `.html` file
 * names beside it in its folder, found in the page and its stylesheets and swapped for `data:` URLs
 * of their bytes, so the page renders in a sandboxed frame with no site behind it.
 *
 * Only plain names in the same folder are resolved (`styles.css`, `./app.js`): a path into another
 * folder, an absolute URL, a data or blob URL, or a fragment is left as written. Relative imports
 * inside a module script and requests the page makes at run time are not seen; projects (§2) are
 * served whole by the preview site instead.
 */

/** Attributes that name one file, by element. */
const URL_ATTRIBUTES: readonly [selector: string, attribute: string][] = [
  ['link[href]', 'href'],
  ['script[src]', 'src'],
  ['img[src]', 'src'],
  ['source[src]', 'src'],
  ['video[src]', 'src'],
  ['video[poster]', 'poster'],
  ['audio[src]', 'src'],
  ['track[src]', 'src'],
  ['input[type=image][src]', 'src'],
  ['object[data]', 'data'],
  ['embed[src]', 'src'],
];

/** Attributes that list files with sizes (`a.png 1x, b.png 2x`). */
const SRCSET_ATTRIBUTES: readonly [selector: string, attribute: string][] = [
  ['img[srcset]', 'srcset'],
  ['source[srcset]', 'srcset'],
];

const CSS_URL = /url\(\s*(['"]?)([^'")]+?)\1\s*\)/g;
const CSS_IMPORT = /@import\s+(['"])([^'"]+)\1/g;

/**
 * The file name a reference names in the page's own folder, or null when it names anything else.
 * `./` is dropped, and a query or fragment is ignored (`app.js?v=2` is `app.js`).
 */
export function siblingName(ref: string | null | undefined): string | null {
  let value = (ref ?? '').trim();
  if (!value || value.startsWith('#') || value.startsWith('/') || value.startsWith('\\')) return null;
  // A scheme (https:, data:, blob:, mailto:, javascript:) names something that is not a sibling.
  if (/^[a-z][a-z0-9+.-]*:/i.test(value)) return null;
  value = value.replace(/[?#].*$/, '');
  while (value.startsWith('./')) value = value.slice(2);
  if (!value || value.includes('/') || value.includes('\\') || value === '.' || value === '..') return null;
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function srcsetEntries(value: string): string[] {
  return value
    .split(',')
    .map(part => part.trim().split(/\s+/)[0] ?? '')
    .filter(Boolean);
}

/** The names a stylesheet's `url()` and `@import` reference in its folder. */
export function cssReferences(css: string): string[] {
  const names: string[] = [];
  for (const pattern of [CSS_URL, CSS_IMPORT]) {
    for (const match of css.matchAll(pattern)) {
      const name = siblingName(match[2]);
      if (name) names.push(name);
    }
  }
  return names;
}

/** The css with every sibling reference that has a URL in `urls` swapped for it. */
export function rewriteCss(css: string, urls: ReadonlyMap<string, string>): string {
  const swap = (ref: string) => {
    const name = siblingName(ref);
    return name ? urls.get(name) : undefined;
  };
  return css
    .replace(CSS_URL, (whole, quote: string, ref: string) => {
      const url = swap(ref);
      return url ? `url(${quote}${url}${quote})` : whole;
    })
    .replace(CSS_IMPORT, (whole, quote: string, ref: string) => {
      const url = swap(ref);
      return url ? `@import ${quote}${url}${quote}` : whole;
    });
}

/** Every sibling name the page references: in its elements, its `<style>`s and its `style` attributes. */
export function pageReferences(doc: Document): Set<string> {
  const names = new Set<string>();
  const add = (ref: string | null) => {
    const name = siblingName(ref);
    if (name) names.add(name);
  };
  for (const [selector, attribute] of URL_ATTRIBUTES) {
    for (const el of Array.from(doc.querySelectorAll(selector))) add(el.getAttribute(attribute));
  }
  for (const [selector, attribute] of SRCSET_ATTRIBUTES) {
    for (const el of Array.from(doc.querySelectorAll(selector))) srcsetEntries(el.getAttribute(attribute) ?? '').forEach(add);
  }
  for (const el of Array.from(doc.querySelectorAll('style'))) cssReferences(el.textContent ?? '').forEach(name => names.add(name));
  for (const el of Array.from(doc.querySelectorAll('[style]')))
    cssReferences(el.getAttribute('style') ?? '').forEach(name => names.add(name));
  return names;
}

/** Swaps the page's sibling references for the URLs in `urls`, in place; a name without one is left. */
export function rewritePage(doc: Document, urls: ReadonlyMap<string, string>): void {
  const swap = (ref: string | null) => {
    const name = siblingName(ref);
    return name ? urls.get(name) : undefined;
  };
  for (const [selector, attribute] of URL_ATTRIBUTES) {
    for (const el of Array.from(doc.querySelectorAll(selector))) {
      const url = swap(el.getAttribute(attribute));
      if (url) el.setAttribute(attribute, url);
    }
  }
  for (const [selector, attribute] of SRCSET_ATTRIBUTES) {
    for (const el of Array.from(doc.querySelectorAll(selector))) {
      const value = el.getAttribute(attribute) ?? '';
      const rewritten = value
        .split(',')
        .map(part => {
          const [ref, ...rest] = part.trim().split(/\s+/);
          const url = swap(ref ?? null);
          return url ? [url, ...rest].join(' ') : part.trim();
        })
        .join(', ');
      el.setAttribute(attribute, rewritten);
    }
  }
  for (const el of Array.from(doc.querySelectorAll('style'))) el.textContent = rewriteCss(el.textContent ?? '', urls);
  for (const el of Array.from(doc.querySelectorAll('[style]'))) el.setAttribute('style', rewriteCss(el.getAttribute('style') ?? '', urls));
}

/** The document as HTML again, with its doctype. */
export function serializePage(doc: Document): string {
  const doctype = doc.doctype ? `<!DOCTYPE ${doc.doctype.name}>\n` : '';
  return doctype + doc.documentElement.outerHTML;
}

const TYPES_BY_EXTENSION: Record<string, string> = {
  '.css': 'text/css',
  '.js': 'text/javascript',
  '.mjs': 'text/javascript',
  '.json': 'application/json',
  '.svg': 'image/svg+xml',
  '.html': 'text/html',
  '.htm': 'text/html',
  '.wasm': 'application/wasm',
  '.woff': 'font/woff',
  '.woff2': 'font/woff2',
  '.ttf': 'font/ttf',
  '.otf': 'font/otf',
};

/**
 * The type a browser needs a file served with: stylesheets and scripts by their extension (a
 * stylesheet with an upload's generic type is refused), anything else as it was stored.
 */
export function servedType(name: string, storedType: string | null | undefined): string {
  const dot = name.lastIndexOf('.');
  const byExtension = dot >= 0 ? TYPES_BY_EXTENSION[name.slice(dot).toLowerCase()] : undefined;
  return byExtension ?? (storedType || 'application/octet-stream');
}

/** Whether the name is a stylesheet, whose own references are resolved too. */
export function isStylesheet(name: string): boolean {
  return name.toLowerCase().endsWith('.css');
}
