import DOMPurify from 'dompurify';

import { escapeHtml } from '../../core/utils/escape-html';
import { NotebookOutput, isDataOutput, isErrorOutput, isStreamOutput, joinSource, recordField } from './nbformat';

/**
 * What a cell's outputs show (ADR 0x043 §11), chosen by MIME type from the richest representation we
 * can display safely. Notebooks come from anywhere: HTML is sanitized, and SVG is shown through an
 * image, where it cannot script. Script-driven output runs only in a sandboxed frame with an opaque
 * origin, and only after the reader asks (see notebook-interactive-frame.component.ts).
 */
export type OutputView =
  | { kind: 'text'; text: string; stderr?: boolean }
  | { kind: 'ansi'; html: string; stderr?: boolean }
  | { kind: 'html'; html: string }
  | { kind: 'image'; src: string; width?: number; height?: number }
  | { kind: 'markdown'; text: string }
  | { kind: 'json'; text: string }
  | { kind: 'interactive'; document: string; fallback: OutputView | null }
  | { kind: 'widget' }
  | { kind: 'unsupported'; mime: string };

/** The order Jupyter prefers, among what we can show; scripted HTML steps aside when there is a fallback. */
const IMAGE_MIMES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/bmp'];
const SVG = 'image/svg+xml';
const WIDGET_VIEW = 'application/vnd.jupyter.widget-view+json';
const PLOTLY = 'application/vnd.plotly.v1+json';
const BOKEH_LOAD = 'application/vnd.bokehjs_load.v0+json';
const JAVASCRIPT = 'application/javascript';
const CDN = 'https://cdn.jsdelivr.net/npm';
const PLOTLY_JS = `${CDN}/plotly.js-dist-min@3.7.0/plotly.min.js`;
const REQUIRE_JS = `${CDN}/requirejs@2.3.8/require.js`;
const VEGA_STACKS: Record<string, readonly string[]> = {
  '5': [`${CDN}/vega@5.33.1`, `${CDN}/vega-lite@5.23.0`, `${CDN}/vega-embed@6.29.0`],
  '6': [`${CDN}/vega@6.4.0`, `${CDN}/vega-lite@6.5.0`, `${CDN}/vega-embed@7.3.0`],
};
/** Long streams are cut in the view, not in the file. */
export const MAX_OUTPUT_CHARS = 200_000;

/** `loaders`: the notebook's library-loading outputs (notebookLoaders), which a plot's frame needs too. */
export function outputViews(outputs: readonly NotebookOutput[], loaders = ''): OutputView[] {
  const views: OutputView[] = [];
  for (const output of mergeStreams(outputs)) {
    const view = outputView(output, loaders);
    if (view) views.push(view);
  }
  return views;
}

/** Jupyter shows consecutive writes to one stream as one block; carriage returns overwrite within it. */
function mergeStreams(outputs: readonly NotebookOutput[]): NotebookOutput[] {
  const merged: NotebookOutput[] = [];
  for (const output of outputs) {
    const last = merged[merged.length - 1];
    if (isStreamOutput(output) && last && isStreamOutput(last) && last.name === output.name) {
      merged[merged.length - 1] = { ...last, text: joinSource(last.text) + joinSource(output.text) };
    } else merged.push(output);
  }
  return merged;
}

function outputView(output: NotebookOutput, loaders: string): OutputView | null {
  if (isStreamOutput(output)) return textView(applyCarriageReturns(joinSource(output.text)), output.name === 'stderr');
  if (isErrorOutput(output)) {
    const trace = Array.isArray(output.traceback)
      ? output.traceback.map(String).join('\n')
      : `${String(output.ename ?? 'Error')}: ${String(output.evalue ?? '')}`;
    return textView(trace, true);
  }
  if (isDataOutput(output)) return dataView(recordField(output.data), recordField(output.metadata), loaders);
  return null;
}

function textView(text: string, stderr = false): OutputView {
  // eslint-disable-next-line no-control-regex
  if (/\u001b\[/.test(text)) return { kind: 'ansi', html: ansiToHtml(text), stderr };
  return { kind: 'text', text, stderr };
}

function dataView(data: Record<string, unknown>, metadata: Record<string, unknown>, loaders: string): OutputView | null {
  const mimes = Object.keys(data);
  if (!mimes.length || loaderHtml(data) !== null) return null;
  // A widget is a view of objects in a live kernel; its repr says nothing useful without one.
  if (data[WIDGET_VIEW] !== undefined) return { kind: 'widget' };
  const scripted = interactiveBody(data);
  if (scripted !== null)
    return { kind: 'interactive', document: frameDocument(scripted, loaders), fallback: staticView(data, metadata, true) };
  return staticView(data, metadata, false);
}

/** The richest representation that runs nothing; scripted HTML is left out when asked. */
function staticView(data: Record<string, unknown>, metadata: Record<string, unknown>, skipHtml: boolean): OutputView | null {
  if (!skipHtml && data['text/html'] !== undefined) return { kind: 'html', html: sanitizeOutputHtml(joinSource(data['text/html'])) };
  for (const mime of IMAGE_MIMES) {
    if (data[mime] !== undefined) return imageView(mime, joinSource(data[mime]).replace(/\s+/g, ''), metadata[mime]);
  }
  if (data['text/markdown'] !== undefined) return { kind: 'markdown', text: withoutRemoteImages(joinSource(data['text/markdown'])) };
  if (data['text/latex'] !== undefined) return { kind: 'markdown', text: joinSource(data['text/latex']) };
  if (data[SVG] !== undefined) {
    return { kind: 'image', src: `data:${SVG};charset=utf-8,${encodeURIComponent(joinSource(data[SVG]))}` };
  }
  if (data['application/json'] !== undefined) return { kind: 'json', text: JSON.stringify(data['application/json'], null, 2) };
  if (data['text/plain'] !== undefined) return textView(joinSource(data['text/plain']));
  return skipHtml ? null : { kind: 'unsupported', mime: Object.keys(data)[0] };
}

function imageView(mime: string, base64: string, metadata: unknown): OutputView {
  const size = (metadata ?? {}) as { width?: unknown; height?: unknown };
  const view: OutputView = { kind: 'image', src: `data:${mime};base64,${base64}` };
  if (typeof size.width === 'number') view.width = size.width;
  if (typeof size.height === 'number') view.height = size.height;
  return view;
}

// --- script-driven output --------------------------------------------------------------------

/** Text safe inside a script element. */
function inScript(code: string): string {
  return code.replace(/<\/(script)/gi, '<\\/$1').replace(/<!--/g, '<\\!--');
}

/** JSON safe inside a script element. */
function jsonInScript(value: unknown): string {
  return JSON.stringify(value).replace(/</g, '\\u003c');
}

/** Whether HTML holds no visible content beyond scripts and styles. */
function onlyScripts(html: string): boolean {
  return (
    /<script\b/i.test(html) &&
    !html
      .replace(/<(script|style)\b[\s\S]*?<\/\1>/gi, '')
      .replace(/<!--[\s\S]*?-->/g, '')
      .trim()
  );
}

/**
 * An output that only loads a library for later outputs (Plotly's init_notebook_mode, Bokeh's
 * output_notebook) has nothing to show, and the plots after it need what it loads.
 */
function loaderHtml(data: Record<string, unknown>): string | null {
  const js = data[JAVASCRIPT] !== undefined ? `<script>${inScript(joinSource(data[JAVASCRIPT]))}</script>` : '';
  if (data[BOKEH_LOAD] !== undefined) return js;
  const rest = Object.keys(data).filter(m => m !== 'text/plain' && m !== JAVASCRIPT);
  // A bare application/javascript output is not treated as a loader: nothing says it is one, and
  // it would run in every other frame. It gets a frame of its own, like any other script output.
  if (rest.length === 1 && rest[0] === 'text/html') {
    const html = joinSource(data['text/html']);
    if (onlyScripts(html)) return html + js;
  }
  return null;
}

/** Every loader in the notebook, in order, to include in each interactive frame. */
export function notebookLoaders(cells: readonly { outputs: readonly NotebookOutput[] }[]): string {
  let out = '';
  for (const cell of cells) {
    for (const output of cell.outputs) {
      if (isDataOutput(output)) out += loaderHtml(recordField(output.data)) ?? '';
    }
  }
  return out;
}

/** The markup that draws an output with script, or null when it runs nothing. */
function interactiveBody(data: Record<string, unknown>): string | null {
  if (data[PLOTLY] !== undefined) {
    const figure = data[PLOTLY] as { data?: unknown; layout?: unknown; config?: unknown };
    return (
      `<div id="plot" style="width:100%;height:100vh;min-height:420px"></div><script src="${PLOTLY_JS}"></script>` +
      `<script>Plotly.newPlot('plot',${jsonInScript(figure.data ?? [])},${jsonInScript(figure.layout ?? {})},` +
      `Object.assign({responsive:true},${jsonInScript(figure.config ?? {})}));</script>`
    );
  }
  for (const mime of Object.keys(data)) {
    const vega = /^application\/vnd\.vega(?:lite)?\.v(\d+)(?:\.\d+)?\+json$/.exec(mime);
    if (!vega) continue;
    const stack = VEGA_STACKS[vega[1]] ?? VEGA_STACKS['5'];
    return (
      `<div id="vis"></div>${stack.map(src => `<script src="${src}"></script>`).join('')}` +
      `<script>vegaEmbed('#vis',${jsonInScript(data[mime])},{actions:false});</script>`
    );
  }
  const html = data['text/html'] !== undefined ? joinSource(data['text/html']) : '';
  const js = data[JAVASCRIPT] !== undefined ? `<script>${inScript(joinSource(data[JAVASCRIPT]))}</script>` : '';
  return /<script\b/i.test(html) || js ? html + js : null;
}

/**
 * The whole document for one sandboxed frame. The sandbox gives it an opaque origin, so it reaches
 * neither this page nor its storage; the policy keeps it to https and stops forms and <base>. It
 * tells the host its height, the only thing the host listens for.
 *
 * The script sources are deliberately broad. Inline script is how every output library starts, and
 * Plotly and Vega compile functions at run time, so they need 'unsafe-inline' and 'unsafe-eval'.
 * Notebooks load libraries from whichever CDN their author used, so a host list would break output
 * without protecting anything: the boundary is the opaque origin, and this only runs on request.
 *
 * connect-src https: is the accepted trade-off of ADR 0x043 §11: a library can fetch its own data
 * and tiles, and so a hostile notebook's script can send what it was given (its own output data)
 * to an https host. It cannot read anything else: the frame has no cookies, no storage of ours and
 * no access to the page. Narrow this to a list of hosts if notebook output ever carries more.
 *
 * The height message goes to '*' because the frame's own origin is opaque and it cannot name the
 * parent's; it carries only a number, and the host accepts it only from this frame's window.
 */
function frameDocument(body: string, loaders: string): string {
  const policy = [
    "default-src 'none'",
    "script-src 'unsafe-inline' 'unsafe-eval' https:",
    "style-src 'unsafe-inline' https:",
    'img-src data: blob: https:',
    'font-src data: https:',
    'connect-src https:',
    'worker-src blob:',
    "base-uri 'none'",
    "form-action 'none'",
  ].join('; ');
  const needsRequire = /\brequire(?:js)?\s*[.(]/.test(loaders + body);
  return (
    `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="${policy}">` +
    `<style>html,body{margin:0;background:#fff;color:#111;font:13px sans-serif}</style>` +
    (needsRequire ? `<script src="${REQUIRE_JS}"></script>` : '') +
    `</head><body>` +
    `<script>var element=document.body;(function(){var post=function(){parent.postMessage({type:'nb-output-height',height:Math.ceil(document.body.getBoundingClientRect().height)},'*')};` +
    `addEventListener('load',function(){post();new ResizeObserver(post).observe(document.body)});})();</script>` +
    `${loaders}${body}</body></html>`
  );
}

/** A carriage return rewrites its line, which is how progress bars print. */
export function applyCarriageReturns(text: string): string {
  if (!text.includes('\r')) return text;
  return text
    .split('\n')
    .map(line => {
      const parts = line.replace(/\r$/, '').split('\r');
      let out = '';
      for (const part of parts) out = part + out.slice(part.length);
      return out;
    })
    .join('\n');
}

const ANSI_COLOURS = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white'];

/** SGR colours and bold to spans with classes; everything else is escaped text. */
export function ansiToHtml(text: string): string {
  let fg: string | null = null;
  let bg: string | null = null;
  let bold = false;
  let out = '';
  // eslint-disable-next-line no-control-regex
  const parts = text.split(/\u001b\[([0-9;]*)([A-Za-z])/);
  for (let i = 0; i < parts.length; i += 3) {
    const chunk = escapeHtml(parts[i]);
    if (chunk) {
      const classes = [fg && `nb-ansi-${fg}`, bg && `nb-ansi-bg-${bg}`, bold && 'nb-ansi-bold'].filter(Boolean).join(' ');
      out += classes ? `<span class="${classes}">${chunk}</span>` : chunk;
    }
    if (parts[i + 2] !== 'm') continue;
    const codes = (parts[i + 1] || '0').split(';').map(Number);
    for (let c = 0; c < codes.length; c++) {
      const code = codes[c];
      if (code === 0) [fg, bg, bold] = [null, null, false];
      else if (code === 1) bold = true;
      else if (code === 22) bold = false;
      else if (code === 39) fg = null;
      else if (code === 49) bg = null;
      else if (code >= 30 && code <= 37) fg = ANSI_COLOURS[code - 30];
      else if (code >= 90 && code <= 97) fg = ANSI_COLOURS[code - 90];
      else if (code >= 40 && code <= 47) bg = ANSI_COLOURS[code - 40];
      else if (code === 38 || code === 48) c += codes[c + 1] === 5 ? 2 : codes[c + 1] === 2 ? 4 : 0; // 256 and true colour: skipped
    }
  }
  return out;
}

// Its own instance: hooks on the shared one would reach the chat's clipboard code.
let purifier: ReturnType<typeof DOMPurify> | null = null;

function outputPurifier(view: Window & typeof globalThis): ReturnType<typeof DOMPurify> {
  if (purifier) return purifier;
  const instance = DOMPurify(view);
  instance.addHook('afterSanitizeAttributes', node => {
    // DOMPurify already removes event handlers; output stays script-free even if its defaults move.
    for (const name of node.getAttributeNames?.() ?? []) {
      if (/^on/i.test(name)) node.removeAttribute(name);
    }
    // Output must not phone home when a notebook is opened: only embedded pictures load, and
    // nothing else may name a remote resource (CSS url(), SVG image and filter hrefs, paint servers).
    for (const name of node.getAttributeNames?.() ?? []) {
      const value = node.getAttribute(name) ?? '';
      const lower = name.toLowerCase();
      if (lower === 'srcset') node.removeAttribute(name);
      else if (lower === 'src' || (lower.endsWith('href') && node.tagName !== 'A')) {
        if (!/^data:image\//i.test(value.trim()) && !value.trim().startsWith('#')) node.removeAttribute(name);
      } else if (lower === 'style') {
        // Style is only partly trusted, so it is kept narrow: a declaration may use colour and
        // size functions, nothing else. Any other function (url, image-set, element, vendor
        // variants) or a backslash escape, which can spell one, drops the whole attribute.
        const plain = value.replace(/\b(?:rgba?|hsla?|calc|var)\(/gi, '');
        if (/[(\\@]/.test(plain)) node.removeAttribute(name);
      } else if (
        (/url\s*\(|image-set\s*\(|@import/i.test(value) || /\\[\s\S]*\(/.test(value)) &&
        !/^\s*url\(\s*['"]?#[\w-]+['"]?\s*\)\s*$/i.test(value)
      ) {
        node.removeAttribute(name);
      }
    }
    if (node.tagName === 'A') {
      node.setAttribute('target', '_blank');
      node.setAttribute('rel', 'noopener noreferrer');
    }
  });
  purifier = instance;
  return instance;
}

/** Tables and text survive; scripts, handlers, forms, frames and stylesheets do not. */
export function sanitizeOutputHtml(html: string): string {
  // Without a DOM (server rendering, a worker) nothing can be sanitized, so the HTML shows as text.
  const view = typeof window === 'undefined' ? undefined : window;
  if (!view?.document) return escapeHtml(html);
  return outputPurifier(view).sanitize(html, {
    FORBID_TAGS: [
      'style',
      'form',
      'input',
      'button',
      'textarea',
      'select',
      'iframe',
      'object',
      'embed',
      'base',
      'meta',
      'link',
      'video',
      'audio',
      'source',
    ],
    FORBID_ATTR: ['formaction', 'background'],
  }) as string;
}

/**
 * Markdown without pictures that would load from elsewhere when the notebook opens. A remote image
 * becomes a link to it, which loads nothing until clicked; embedded (data:) images stay.
 */
export function withoutRemoteImages(markdown: string): string {
  return markdown
    .replace(/!\[([^\]]*)\]\(\s*<?(?!data:)([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)/gi, '[$1]($2)')
    .replace(/!\[([^\]]*)\]\[([^\]]*)\]/g, '[$1][$2]')
    .replace(/(<img\b[^>]*?)\s(?:src|srcset)\s*=\s*("(?!data:)[^"]*"|'(?!data:)[^']*'|(?!data:)[^\s>"']+)/gi, '$1');
}
