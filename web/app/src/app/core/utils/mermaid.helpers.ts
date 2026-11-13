import type { MarkedExtension, Tokens } from 'marked';

import { saveBlobAsFile } from './download.helpers';
import { escapeHtml } from './escape-html';
import { retryingLoader } from './retrying-loader';

/**
 * Mermaid diagrams in rendered markdown, drawn the way LaTeX math is (latex-math.helpers.ts).
 *
 * 1. `mermaidExtension()` renders a fenced code block whose language is `mermaid` as a
 *    `.mermaid-diagram` placeholder holding the escaped source in a `pre`. Divs, `pre` and `class`
 *    survive Angular's HTML sanitizer, so the sanitizer stays as strict as it is. Only a fence that
 *    is closed becomes a placeholder: a reply still streaming in shows the code until the closing
 *    fence arrives, rather than a diagram that fails with every chunk.
 * 2. `renderMermaid(root)` runs once the sanitized HTML is in the DOM. It loads Mermaid on demand
 *    (it is large, and loads each diagram type on demand too), draws the source with
 *    `securityLevel: 'strict'` and HTML labels off (so the SVG has no `foreignObject`), and passes
 *    the SVG through DOMPurify's SVG profile before inserting it. Source Mermaid cannot draw stays
 *    as code, with a note. Drawings are cached by source and theme, so a message re-rendered as it
 *    streams does not draw its finished diagrams again; a failure to load Mermaid is not cached.
 *
 * Each diagram gets a small toolbar: show the source instead, copy the source, download SVG or PNG,
 * and, where the app has a diagram editor, open it as an editable diagram (setMermaidEditor).
 */

export const MERMAID_CLASS = 'mermaid-diagram';
const SOURCE_CLASS = 'mermaid-diagram__source';
const STATE = 'mermaidState';

/** Whether a fenced code token's closing fence has arrived (marked treats an unclosed fence as code to the end). */
export function isClosedFence(raw: string): boolean {
  const lines = raw.replace(/\n+$/, '').split('\n');
  if (lines.length < 2) return false;
  const open = /^ {0,3}(`{3,}|~{3,})/.exec(lines[0])?.[1];
  if (!open) return false;
  const close = lines[lines.length - 1].trim();
  return close.length >= open.length && close === open[0].repeat(close.length);
}

function isMermaid(token: Tokens.Code): boolean {
  return (token.lang ?? '').trim().split(/\s+/)[0]?.toLowerCase() === 'mermaid';
}

/**
 * Mermaid source as markdown that draws it: the whole text in a ```mermaid fence, longer than any
 * run of backticks in it. A `.mmd` file is shown and previewed this way.
 */
export function asMermaidMarkdown(source: string): string {
  const longest = Math.max(2, ...Array.from(source.matchAll(/`+/g), m => m[0].length));
  const fence = '`'.repeat(longest + 1);
  return `${fence}mermaid\n${source.replace(/\n$/, '')}\n${fence}\n`;
}

/** The marked extension; register it through ngx-markdown's `MARKED_EXTENSIONS`. */
export function mermaidExtension(): MarkedExtension {
  return {
    renderer: {
      code(token: Tokens.Code) {
        if (!isMermaid(token) || !token.text.trim() || !isClosedFence(token.raw)) return false;
        return `<div class="${MERMAID_CLASS}"><pre class="${SOURCE_CLASS}">${escapeHtml(token.text)}</pre></div>\n`;
      },
    },
  };
}

type Mermaid = typeof import('mermaid').default;
type Purify = typeof import('dompurify').default;
type Theme = 'light' | 'dark';
/** A drawing; `skipped` when no placeholder still wanted it by the time its turn came. */
type Drawing = { svg: string } | { error: string } | { skipped: true };

let initializedFor: Theme | null = null;
/** Mermaid keeps global state while it draws, so drawings run one at a time. */
let queue: Promise<unknown> = Promise.resolve();
let nextId = 0;
const MAX_CACHED = 50;
/** A drawing by theme and source; `wanted` holds who waits for it until its turn comes. */
type Entry = { drawing: Promise<Drawing>; wanted: Array<() => boolean> | null };
const drawings = new Map<string, Entry>();

/** A theme token as resolved on <html>, or `fallback` (its value in styles/themes.scss) where none is (tests). */
function token(document: Document, name: string, fallback: string): string {
  const value = document.defaultView?.getComputedStyle(document.documentElement).getPropertyValue(name).trim();
  return value || fallback;
}

/**
 * The colours of a diagram's series, in turn: pie slices (pie1…) and the branches of a mindmap or
 * the sections of a timeline (cScale0…), with the text on them.
 */
function series(colors: string[], text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (let i = 0; i < 12; i++) {
    const color = colors[i % colors.length];
    out[`pie${i + 1}`] = color;
    out[`cScale${i}`] = color;
    out[`cScaleLabel${i}`] = text;
  }
  return out;
}

/**
 * Mermaid's theme variables, from the app's theme tokens (styles/themes.scss) as the page resolves
 * them. The accent tints for node fills and the extra series colours have no token of their own.
 */
function themeVariables(document: Document, theme: Theme): Record<string, string | boolean> {
  const dark = theme === 'dark';
  const t = (name: string, light: string, darkValue: string) => token(document, name, dark ? darkValue : light);
  const background = t('--bg-card', '#FFFFFF', '#1F2937');
  const text = t('--text-primary', '#111827', '#F9FAFB');
  const surface = t('--bg-surface', '#E5E7EB', '#374151');
  const accent = t('--accent', '#8C87FF', '#8C87FF');
  const muted = t('--text-muted', '#6B7280', '#6B7280');
  return {
    darkMode: dark,
    background,
    fontFamily: token(document, '--font-sans', 'Outfit, ui-sans-serif, system-ui, sans-serif'),
    primaryColor: dark ? '#2E2B5F' : '#EEEDFF',
    primaryTextColor: text,
    primaryBorderColor: accent,
    secondaryColor: surface,
    tertiaryColor: t('--bg-base', '#F3F4F6', '#111827'),
    lineColor: t('--text-secondary', '#4B5563', '#9CA3AF'),
    textColor: text,
    noteBkgColor: dark ? surface : '#FEF9C3',
    noteTextColor: text,
    edgeLabelBackground: background,
    ...series(
      dark
        ? [accent, '#4B4594', muted, t('--border', '#D1D5DB', '#374151'), '#B8B5FF', '#9CA3AF']
        : [accent, '#C7C4FF', muted, t('--border', '#D1D5DB', '#374151'), '#5B55D6', '#9CA3AF'],
      text,
    ),
  };
}

const loadMermaid = retryingLoader<Mermaid>(() => import('mermaid').then(module => module.default));
const loadPurify = retryingLoader<Purify>(() => import('dompurify').then(module => module.default));

/** Whether an error is a chunk that did not download, not something wrong with the source. */
function isLoadFailure(error: unknown): boolean {
  const text = error instanceof Error ? `${error.name} ${error.message}` : String(error);
  return /dynamically imported module|importing a module script failed|ChunkLoadError|Loading chunk/i.test(text);
}

function initialize(mermaid: Mermaid, theme: Theme): void {
  if (initializedFor === theme) return;
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    // A failed render throws, rather than leaving Mermaid's "Syntax error" picture in the page.
    suppressErrorRendering: true,
    // Mermaid's `base` theme is the one whose colours can be set: these are the app's own
    // (styles/themes.scss), so a diagram looks like part of the page in either theme.
    theme: 'base',
    themeVariables: themeVariables(document, theme),
    // SVG text, not HTML in foreignObject: the SVG profile below would drop that anyway.
    htmlLabels: false,
    flowchart: { htmlLabels: false },
    // A diagram's `%%{init}%%` directive may not change these: CSS of its own could load a URL.
    secure: [
      'secure',
      'securityLevel',
      'startOnLoad',
      'suppressErrorRendering',
      'maxTextSize',
      'maxEdges',
      'themeCSS',
      'fontFamily',
      'altFontFamily',
      'htmlLabels',
      'themeVariables',
    ],
  });
  initializedFor = theme;
}

/** The app's resolved theme, as theme.helpers.ts writes it on <html data-theme>. */
function themeOf(document: Document): Theme {
  return document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
}

/** CSS with its escapes (`\75 rl(`, `@\69mport`) decoded, and any backslash left over dropped. */
function decodeCssEscapes(css: string): string {
  return css
    .replace(/\\(?:([0-9a-f]{1,6})[ \t\n\r\f]?|(\r\n|[\n\r\f])|([\s\S]))/gi, (_match, hex: string, _newline, char: string) => {
      if (hex) {
        const code = Number.parseInt(hex, 16);
        return code === 0 || code > 0x10ffff || (code >= 0xd800 && code <= 0xdfff) ? '�' : String.fromCodePoint(code);
      }
      return char ?? '';
    })
    .replace(/\\/g, '');
}

/**
 * Drops anything in the SVG's own CSS that could fetch: `@import`, and `url()`, `image-set()`,
 * `image()` and `src()` to anything but a fragment of the SVG itself (`url(#arrow)`, quoted or not).
 * Escapes are decoded first, so the browser reads exactly the text that was checked.
 */
export function withoutFetchingCss(css: string): string {
  return (
    decodeCssEscapes(css)
      .replace(/@import\b[^;]*;?/gi, '')
      .replace(/(?<![\w-])url\(\s*(?:"([^"]*)"|'([^']*)'|([^"')]*?))\s*\)/gi, (_match, dq?: string, sq?: string, bare?: string) => {
        const target = (dq ?? sq ?? bare ?? '').trim();
        return /^#[\w-]+$/.test(target) ? `url(#${target.slice(1)})` : 'none';
      })
      // Whatever is left that could still take a URL: a function the browser would not parse.
      .replace(/(?<![\w-])(?:url\((?!#[\w-]+\))|(?:-webkit-)?image-set\(|image\(|src\()/gi, 'none(')
  );
}

function sanitizeSvg(purify: Purify, svg: string): string {
  const clean = purify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true },
    ADD_TAGS: ['style'],
    RETURN_DOM: true,
  }) as unknown as HTMLElement;
  for (const style of Array.from(clean.querySelectorAll('style'))) style.textContent = withoutFetchingCss(style.textContent ?? '');
  for (const el of Array.from(clean.querySelectorAll('[style]')))
    el.setAttribute('style', withoutFetchingCss(el.getAttribute('style') ?? ''));
  return clean.innerHTML;
}

function forget(key: string, entry: Entry): void {
  if (drawings.get(key) === entry) drawings.delete(key);
}

/** Mermaid's work area for a drawing, should a failed render leave it in the page. */
function removeLeftovers(id: string): void {
  for (const leftover of [`d${id}`, `i${id}`, id]) document.getElementById(leftover)?.remove();
}

function errorText(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * Draws source (cached by source and theme); never rejects. `wanted` says whether the drawing is
 * still needed: once its turn comes, a drawing nobody waiting for it still wants is skipped, so a
 * preview typed into does not draw every earlier version first. A failure to load Mermaid (a chunk
 * that did not download) is not cached: the next request tries again.
 */
export function drawMermaid(source: string, theme: Theme, wanted: () => boolean = () => true): Promise<Drawing> {
  const key = `${theme}\n${source}`;
  const cached = drawings.get(key);
  if (cached) {
    cached.wanted?.push(wanted);
    return cached.drawing;
  }
  const entry: Entry = { drawing: Promise.resolve({ skipped: true }), wanted: [wanted] };
  entry.drawing = (queue = queue.then(async (): Promise<Drawing> => {
    const waiting = entry.wanted ?? [];
    entry.wanted = null;
    if (!waiting.some(isWanted => isWanted())) {
      forget(key, entry);
      return { skipped: true };
    }
    const id = `mermaid-diagram-${++nextId}`;
    try {
      const [mermaid, purify] = await Promise.all([loadMermaid(), loadPurify()]);
      initialize(mermaid, theme);
      const { svg } = await mermaid.render(id, source);
      return { svg: sanitizeSvg(purify, svg) };
    } catch (error) {
      removeLeftovers(id);
      if (isLoadFailure(error)) forget(key, entry);
      return { error: errorText(error) };
    }
  })) as Promise<Drawing>;
  drawings.set(key, entry);
  if (drawings.size > MAX_CACHED) drawings.delete(drawings.keys().next().value!);
  return entry.drawing;
}

/**
 * Draws every `.mermaid-diagram` placeholder under `root` not drawn yet. A placeholder replaced
 * before or while it was being drawn (a streaming re-render) is left alone.
 */
export async function renderMermaid(root: HTMLElement): Promise<void> {
  const placeholders = Array.from(root.querySelectorAll<HTMLElement>(`.${MERMAID_CLASS}`)).filter(el => !el.dataset[STATE]);
  if (!placeholders.length) return;
  const theme = themeOf(root.ownerDocument);
  for (const element of placeholders) {
    if (!element.isConnected) continue;
    element.dataset[STATE] = 'drawing';
    const source = element.querySelector(`.${SOURCE_CLASS}`)?.textContent ?? '';
    const drawing = await drawMermaid(source, theme, () => element.isConnected);
    if (!element.isConnected) continue;
    if ('svg' in drawing) showDiagram(element, drawing.svg, source);
    else if ('error' in drawing) showError(element);
    else delete element.dataset[STATE];
  }
}

/** Opens a diagram's source as an editable draw.io diagram (ADR 0x049), where the app has an editor. */
type MermaidEditor = (source: string) => Promise<void>;
let mermaidEditor: MermaidEditor | null = null;

/** Offers Edit on drawn diagrams (null takes it away); diagrams drawn before keep what they had. */
export function setMermaidEditor(editor: MermaidEditor | null): void {
  mermaidEditor = editor;
}

/** The Edit button: opens the diagram in the editor, saying so while it does and if it fails. */
function editButton(document: Document, editor: MermaidEditor, source: string): HTMLButtonElement {
  const title = 'Open as an editable diagram (saved as a new file)';
  const b = button(document, 'Edit', title, () => {
    if (b.disabled) return;
    b.disabled = true;
    b.textContent = 'Opening…';
    editor(source)
      .then(() => {
        b.textContent = 'Edit';
        b.title = title;
      })
      .catch(() => {
        b.textContent = "Couldn't open";
        b.title = 'The diagram could not be opened in the editor; try again';
      })
      .finally(() => (b.disabled = false));
  });
  return b;
}

function button(document: Document, label: string, title: string, onClick: () => void): HTMLButtonElement {
  const b = document.createElement('button');
  b.type = 'button';
  b.className = 'mermaid-diagram__button';
  b.textContent = label;
  b.title = title;
  b.addEventListener('click', event => {
    event.preventDefault();
    event.stopPropagation();
    onClick();
  });
  return b;
}

function showDiagram(element: HTMLElement, svg: string, source: string): void {
  const document = element.ownerDocument;
  const pre = element.querySelector<HTMLElement>(`.${SOURCE_CLASS}`);
  const figure = document.createElement('div');
  figure.className = 'mermaid-diagram__figure';
  figure.setAttribute('role', 'img');
  figure.setAttribute('aria-label', 'Diagram');
  // The SVG was sanitized by DOMPurify's SVG profile above.
  figure.innerHTML = svg;
  const drawn = figure.querySelector('svg');
  if (drawn) withUniqueIds(drawn);
  sizeDiagram(figure);
  const toolbar = document.createElement('div');
  toolbar.className = 'mermaid-diagram__toolbar';
  const toggle = button(document, 'Source', 'Show the Mermaid source', () => {
    const showSource = figure.hidden === false;
    figure.hidden = showSource;
    if (pre) pre.hidden = !showSource;
    toggle.textContent = showSource ? 'Diagram' : 'Source';
    toggle.title = showSource ? 'Show the diagram' : 'Show the Mermaid source';
    toggle.setAttribute('aria-pressed', String(showSource));
  });
  toggle.setAttribute('aria-pressed', 'false');
  toolbar.append(
    toggle,
    button(document, 'Expand', 'Show the diagram full screen', () => {
      const el = figure.querySelector('svg');
      if (el) expand(document, el);
    }),
    button(document, 'Copy', 'Copy the Mermaid source', () => void document.defaultView?.navigator.clipboard?.writeText(source)),
    button(document, 'SVG', 'Download as SVG', () => {
      const el = figure.querySelector('svg');
      if (el) saveBlobAsFile(new Blob([serializeSvg(el)], { type: 'image/svg+xml' }), 'diagram.svg');
    }),
    button(document, 'PNG', 'Download as PNG', () => {
      const el = figure.querySelector('svg');
      if (el) void svgToPng(document, el).then(blob => blob && saveBlobAsFile(blob, 'diagram.png'));
    }),
  );
  if (mermaidEditor) toolbar.append(editButton(document, mermaidEditor, source));
  if (pre) pre.hidden = true;
  element.prepend(toolbar, figure);
  element.dataset[STATE] = 'drawn';
}

/**
 * Gives the SVG its own size: Mermaid writes `width="100%"` with a `max-width` of the drawing's
 * width, and in a box that shrinks to its content (a chat bubble) `100%` falls back to the
 * browser's default 300px. The drawing's width goes in `--mermaid-width`; the stylesheet scales it
 * up on wide screens (`--mermaid-scale`) and keeps it within its box and the screen's height.
 */
function sizeDiagram(figure: HTMLElement): void {
  const svg = figure.querySelector('svg');
  if (!svg) return;
  const width = svg.viewBox?.baseVal?.width || Number.parseFloat(svg.style.maxWidth) || 0;
  svg.removeAttribute('width');
  svg.removeAttribute('height');
  svg.style.maxWidth = '';
  if (width > 0) figure.style.setProperty('--mermaid-width', `${Math.ceil(width)}px`);
}

let nextCopy = 0;

/**
 * Gives every id in an inserted SVG a suffix of its own, and every reference to one (`url(#…)`,
 * `href="#…"`, `aria-labelledby`, `#…` in its CSS) the same. A cached drawing is inserted again
 * wherever its source is shown, and the full-screen view is a copy: with the ids shared, `url(#…)`
 * resolves to the first copy in the page, and hiding that one takes the arrowheads off the others.
 */
function withUniqueIds(svg: SVGSVGElement): void {
  const suffix = `-${++nextCopy}`;
  const ids = new Map<string, string>();
  for (const el of [svg, ...Array.from(svg.querySelectorAll('[id]'))]) {
    if (!el.id) continue;
    ids.set(el.id, el.id + suffix);
    el.id += suffix;
  }
  if (!ids.size) return;
  const ref = (id: string) => ids.get(id) ?? id;
  const refsIn = (text: string) => text.replace(/#([\w-]+)/g, (match, id: string) => (ids.has(id) ? `#${ref(id)}` : match));
  for (const el of [svg, ...Array.from(svg.querySelectorAll('*'))]) {
    for (const attr of Array.from(el.attributes)) {
      const name = attr.localName;
      if (name === 'id') continue;
      const value =
        name === 'aria-labelledby' || name === 'aria-describedby' ? attr.value.split(/\s+/).map(ref).join(' ') : refsIn(attr.value);
      if (value !== attr.value) attr.value = value;
    }
  }
  for (const style of Array.from(svg.querySelectorAll('style'))) style.textContent = refsIn(style.textContent ?? '');
}

/** The diagram in a full-screen dialog, scaled to fit it; Escape or a click closes it. */
function expand(document: Document, svg: SVGSVGElement): void {
  const dialog = document.createElement('dialog');
  dialog.className = 'mermaid-diagram__dialog';
  dialog.setAttribute('aria-label', 'Diagram');
  const close = document.createElement('button');
  close.type = 'button';
  close.className = 'mermaid-diagram__button mermaid-diagram__close';
  close.textContent = 'Close';
  const copy = svg.cloneNode(true) as SVGSVGElement;
  withUniqueIds(copy);
  dialog.append(close, copy);
  const done = () => {
    if (typeof dialog.close === 'function') dialog.close();
    dialog.remove();
  };
  close.addEventListener('click', done);
  dialog.addEventListener('click', event => {
    if (event.target === dialog) done();
  });
  dialog.addEventListener('close', () => dialog.remove());
  document.body.append(dialog);
  if (typeof dialog.showModal === 'function') dialog.showModal();
  else dialog.setAttribute('open', '');
}

function showError(element: HTMLElement): void {
  const note = element.ownerDocument.createElement('p');
  note.className = 'mermaid-diagram__error';
  note.textContent = "Couldn't draw this diagram.";
  element.append(note);
  element.dataset[STATE] = 'error';
}

function serializeSvg(svg: SVGSVGElement): string {
  const clone = svg.cloneNode(true) as SVGSVGElement;
  clone.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
  return new XMLSerializer().serializeToString(clone);
}

/** The SVG drawn on a canvas at twice its size, on the page's background. */
async function svgToPng(document: Document, svg: SVGSVGElement): Promise<Blob | null> {
  const box = svg.viewBox?.baseVal;
  const rect = svg.getBoundingClientRect();
  const width = Math.ceil(box?.width || rect.width || 800);
  const height = Math.ceil(box?.height || rect.height || 600);
  const clone = svg.cloneNode(true) as SVGSVGElement;
  clone.setAttribute('width', String(width));
  clone.setAttribute('height', String(height));
  clone.style.maxWidth = '';
  const image = new Image();
  image.src = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(serializeSvg(clone))}`;
  await image.decode();
  const canvas = document.createElement('canvas');
  const scale = 2;
  canvas.width = width * scale;
  canvas.height = height * scale;
  const context = canvas.getContext('2d');
  if (!context) return null;
  const view = document.defaultView;
  const background = view?.getComputedStyle(svg.closest(`.${MERMAID_CLASS}`) ?? document.body).backgroundColor;
  context.fillStyle =
    background && background !== 'rgba(0, 0, 0, 0)' && background !== 'transparent'
      ? background
      : token(document, '--bg-card', themeOf(document) === 'dark' ? '#1F2937' : '#FFFFFF');
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.drawImage(image, 0, 0, canvas.width, canvas.height);
  return new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
}

/**
 * Replaces each drawn diagram in a copied selection with its source as a ```mermaid fence, in a
 * `pre` so the copy keeps its lines, as replaceRenderedMathWithTex does for formulas: the clipboard
 * otherwise gets the SVG's labels.
 */
export function replaceRenderedDiagramsWithSource(container: HTMLElement): void {
  for (const diagram of Array.from(container.querySelectorAll<HTMLElement>(`.${MERMAID_CLASS}`))) {
    const source = diagram.querySelector(`.${SOURCE_CLASS}`)?.textContent;
    if (source == null) continue;
    const pre = container.ownerDocument.createElement('pre');
    pre.textContent = asMermaidMarkdown(source).replace(/\n$/, '');
    diagram.replaceWith(pre);
  }
}
