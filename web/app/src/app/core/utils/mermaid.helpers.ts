import type { MarkedExtension, Tokens } from 'marked';
import { saveBlobAsFile } from './download.helpers';

/**
 * Mermaid diagrams in rendered markdown, drawn the way LaTeX math is (latex-math.helpers.ts).
 *
 * 1. `mermaidExtension()` renders a fenced code block whose language is `mermaid` as a
 *    `.mermaid-diagram` placeholder holding the escaped source in a `pre`. Divs, `pre` and `class`
 *    survive Angular's HTML sanitizer, so the sanitizer stays as strict as it is. Only a fence that
 *    is closed becomes a placeholder: a reply still streaming in shows the code until the closing
 *    fence arrives, rather than a diagram that fails with every chunk.
 * 2. `renderMermaid(root)` runs once the sanitized HTML is in the DOM. It loads Mermaid on demand
 *    (it is large, and loads each diagram type on demand too), checks the source with
 *    `mermaid.parse`, draws it with `securityLevel: 'strict'` and HTML labels off (so the SVG has no
 *    `foreignObject`), and passes the SVG through DOMPurify's SVG profile before inserting it.
 *    Source Mermaid cannot draw stays as code, with a note. Drawings are cached by source and
 *    theme, so a message re-rendered as it streams does not draw its finished diagrams again.
 *
 * Each diagram gets a small toolbar: show the source instead, copy the source, download SVG or PNG,
 * and, where the app has a diagram editor, open it as an editable diagram (setMermaidEditor).
 */

export const MERMAID_CLASS = 'mermaid-diagram';
const SOURCE_CLASS = 'mermaid-diagram__source';
const STATE = 'mermaidState';

function escapeHtml(text: string): string {
  return text.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

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
type Theme = 'light' | 'dark';
type Drawing = { svg: string } | { error: string };

let mermaidLoader: Promise<Mermaid> | null = null;
let initializedFor: Theme | null = null;
/** Mermaid keeps global state while it draws, so drawings run one at a time. */
let queue: Promise<unknown> = Promise.resolve();
let nextId = 0;
const MAX_CACHED = 50;
const drawings = new Map<string, Promise<Drawing>>();

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

/** Mermaid's theme variables, from the app's theme tokens (styles/themes.scss). */
const THEME_VARIABLES: Record<Theme, Record<string, string | boolean>> = {
  light: {
    darkMode: false,
    background: '#FFFFFF',
    fontFamily: 'Outfit, ui-sans-serif, system-ui, sans-serif',
    primaryColor: '#EEEDFF',
    primaryTextColor: '#111827',
    primaryBorderColor: '#8C87FF',
    secondaryColor: '#E5E7EB',
    tertiaryColor: '#F3F4F6',
    lineColor: '#4B5563',
    textColor: '#111827',
    noteBkgColor: '#FEF9C3',
    noteTextColor: '#111827',
    edgeLabelBackground: '#FFFFFF',
    ...series(['#8C87FF', '#C7C4FF', '#6B7280', '#D1D5DB', '#5B55D6', '#9CA3AF'], '#111827'),
  },
  dark: {
    darkMode: true,
    background: '#1F2937',
    fontFamily: 'Outfit, ui-sans-serif, system-ui, sans-serif',
    primaryColor: '#2E2B5F',
    primaryTextColor: '#F9FAFB',
    primaryBorderColor: '#8C87FF',
    secondaryColor: '#374151',
    tertiaryColor: '#111827',
    lineColor: '#9CA3AF',
    textColor: '#F9FAFB',
    noteBkgColor: '#374151',
    noteTextColor: '#F9FAFB',
    edgeLabelBackground: '#1F2937',
    ...series(['#8C87FF', '#4B4594', '#6B7280', '#374151', '#B8B5FF', '#9CA3AF'], '#F9FAFB'),
  },
};

function loadMermaid(): Promise<Mermaid> {
  mermaidLoader ??= import('mermaid').then(module => module.default);
  return mermaidLoader;
}

function initialize(mermaid: Mermaid, theme: Theme): void {
  if (initializedFor === theme) return;
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    // Mermaid's `base` theme is the one whose colours can be set: these are the app's own
    // (styles/themes.scss), so a diagram looks like part of the page in either theme.
    theme: 'base',
    themeVariables: THEME_VARIABLES[theme],
    // SVG text, not HTML in foreignObject: the SVG profile below would drop that anyway.
    htmlLabels: false,
    flowchart: { htmlLabels: false },
    // A diagram's `%%{init}%%` directive may not change these: CSS of its own could load a URL.
    secure: [
      'secure',
      'securityLevel',
      'startOnLoad',
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

/** Drops anything in the SVG's own CSS that could fetch: `@import` and `url()` to anything but a fragment. */
function withoutFetchingCss(css: string): string {
  return css.replace(/@import[^;]*;?/gi, '').replace(/url\(\s*(['"]?)(?!#)[^)]*\1\s*\)/gi, 'none');
}

async function sanitizeSvg(svg: string): Promise<string> {
  const DOMPurify = (await import('dompurify')).default;
  const clean = DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true },
    ADD_TAGS: ['style'],
    RETURN_DOM: true,
  }) as unknown as HTMLElement;
  for (const style of Array.from(clean.querySelectorAll('style'))) style.textContent = withoutFetchingCss(style.textContent ?? '');
  for (const el of Array.from(clean.querySelectorAll('[style]')))
    el.setAttribute('style', withoutFetchingCss(el.getAttribute('style') ?? ''));
  return clean.innerHTML;
}

/** Draws source (cached by source and theme); never rejects. */
export function drawMermaid(source: string, theme: Theme): Promise<Drawing> {
  const key = `${theme}\n${source}`;
  const cached = drawings.get(key);
  if (cached) return cached;
  const drawing = (queue = queue.then(async (): Promise<Drawing> => {
    try {
      const mermaid = await loadMermaid();
      initialize(mermaid, theme);
      await mermaid.parse(source);
      const { svg } = await mermaid.render(`mermaid-diagram-${++nextId}`, source);
      return { svg: await sanitizeSvg(svg) };
    } catch (error) {
      return { error: error instanceof Error ? error.message : String(error) };
    }
  })) as Promise<Drawing>;
  drawings.set(key, drawing);
  if (drawings.size > MAX_CACHED) drawings.delete(drawings.keys().next().value!);
  return drawing;
}

/**
 * Draws every `.mermaid-diagram` placeholder under `root` not drawn yet. A placeholder replaced
 * while it was being drawn (a streaming re-render) is left alone.
 */
export async function renderMermaid(root: HTMLElement): Promise<void> {
  const placeholders = Array.from(root.querySelectorAll<HTMLElement>(`.${MERMAID_CLASS}`)).filter(el => !el.dataset[STATE]);
  if (!placeholders.length) return;
  const theme = themeOf(root.ownerDocument);
  for (const element of placeholders) {
    element.dataset[STATE] = 'drawing';
    const source = element.querySelector(`.${SOURCE_CLASS}`)?.textContent ?? '';
    const drawing = await drawMermaid(source, theme);
    if (!element.isConnected) continue;
    if ('svg' in drawing) showDiagram(element, drawing.svg, source);
    else showError(element);
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
  context.fillStyle = (view && view.getComputedStyle(svg.closest(`.${MERMAID_CLASS}`) ?? document.body).backgroundColor) || '#ffffff';
  if (context.fillStyle === 'rgba(0, 0, 0, 0)') context.fillStyle = themeOf(document) === 'dark' ? '#111827' : '#ffffff';
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.drawImage(image, 0, 0, canvas.width, canvas.height);
  return new Promise(resolve => canvas.toBlob(resolve, 'image/png'));
}

/**
 * Replaces each drawn diagram in a copied selection with its source as a ```mermaid fence, as
 * replaceRenderedMathWithTex does for formulas: the clipboard otherwise gets the SVG's labels.
 */
export function replaceRenderedDiagramsWithSource(container: HTMLElement): void {
  for (const diagram of Array.from(container.querySelectorAll<HTMLElement>(`.${MERMAID_CLASS}`))) {
    const source = diagram.querySelector(`.${SOURCE_CLASS}`)?.textContent;
    if (source == null) continue;
    diagram.replaceWith(container.ownerDocument.createTextNode(`\`\`\`mermaid\n${source.replace(/\n$/, '')}\n\`\`\`\n`));
  }
}
