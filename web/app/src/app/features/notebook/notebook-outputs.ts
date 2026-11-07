import DOMPurify from 'dompurify';

import { joinSource, NotebookOutput } from './nbformat';

/**
 * What a cell's outputs show (ADR 0x043 §8), chosen by MIME type from the richest representation we
 * can display safely. Notebooks come from anywhere: scripts never run here, HTML is sanitized, and
 * SVG is shown through an image, where it cannot script.
 */
export type OutputView =
  | { kind: 'text'; text: string; stderr?: boolean }
  | { kind: 'ansi'; html: string; stderr?: boolean }
  | { kind: 'html'; html: string }
  | { kind: 'image'; src: string; width?: number; height?: number }
  | { kind: 'markdown'; text: string }
  | { kind: 'json'; text: string }
  | { kind: 'unsupported'; mime: string };

/** The order Jupyter prefers, among what we can show; scripted HTML steps aside when there is a fallback. */
const IMAGE_MIMES = ['image/png', 'image/jpeg', 'image/gif', 'image/webp', 'image/bmp'];
const SVG = 'image/svg+xml';
/** Long streams are cut in the view, not in the file. */
export const MAX_OUTPUT_CHARS = 200_000;

export function outputViews(outputs: readonly NotebookOutput[]): OutputView[] {
  const views: OutputView[] = [];
  for (const output of mergeStreams(outputs)) {
    const view = outputView(output);
    if (view) views.push(view);
  }
  return views;
}

/** Jupyter shows consecutive writes to one stream as one block; carriage returns overwrite within it. */
function mergeStreams(outputs: readonly NotebookOutput[]): NotebookOutput[] {
  const merged: NotebookOutput[] = [];
  for (const output of outputs) {
    const last = merged[merged.length - 1];
    if (output.output_type === 'stream' && last?.output_type === 'stream' && last['name'] === output['name']) {
      merged[merged.length - 1] = { ...last, text: joinSource(last['text']) + joinSource(output['text']) };
    } else merged.push(output);
  }
  return merged;
}

function outputView(output: NotebookOutput): OutputView | null {
  switch (output.output_type) {
    case 'stream':
      return textView(applyCarriageReturns(joinSource(output['text'])), output['name'] === 'stderr');
    case 'error':
      return textView(Array.isArray(output['traceback']) ? output['traceback'].join('\n') : `${output['ename']}: ${output['evalue']}`, true);
    case 'display_data':
    case 'execute_result':
      return dataView((output['data'] ?? {}) as Record<string, unknown>, (output['metadata'] ?? {}) as Record<string, unknown>);
    default:
      return null;
  }
}

function textView(text: string, stderr = false): OutputView {
  // eslint-disable-next-line no-control-regex
  if (/\u001b\[/.test(text)) return { kind: 'ansi', html: ansiToHtml(text), stderr };
  return { kind: 'text', text, stderr };
}

function dataView(data: Record<string, unknown>, metadata: Record<string, unknown>): OutputView | null {
  const mimes = Object.keys(data);
  if (!mimes.length) return null;
  const hasFallback = mimes.some(m => m === 'text/plain' || m.startsWith('image/'));
  const html = data['text/html'] !== undefined ? joinSource(data['text/html']) : null;
  // A plotting library's HTML is a script and an empty div; its picture or repr says more.
  if (html !== null && !(hasFallback && /<script\b/i.test(html))) return { kind: 'html', html: sanitizeOutputHtml(html) };
  for (const mime of IMAGE_MIMES) {
    if (data[mime] !== undefined) return imageView(mime, joinSource(data[mime]).replace(/\s+/g, ''), metadata[mime]);
  }
  if (data['text/markdown'] !== undefined) return { kind: 'markdown', text: joinSource(data['text/markdown']) };
  if (data['text/latex'] !== undefined) return { kind: 'markdown', text: joinSource(data['text/latex']) };
  if (data[SVG] !== undefined) {
    return { kind: 'image', src: `data:${SVG};charset=utf-8,${encodeURIComponent(joinSource(data[SVG]))}` };
  }
  if (data['application/json'] !== undefined) return { kind: 'json', text: JSON.stringify(data['application/json'], null, 2) };
  if (data['text/plain'] !== undefined) return textView(joinSource(data['text/plain']));
  return { kind: 'unsupported', mime: mimes[0] };
}

function imageView(mime: string, base64: string, metadata: unknown): OutputView {
  const size = (metadata ?? {}) as { width?: unknown; height?: unknown };
  const view: OutputView = { kind: 'image', src: `data:${mime};base64,${base64}` };
  if (typeof size.width === 'number') view.width = size.width;
  if (typeof size.height === 'number') view.height = size.height;
  return view;
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

function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, ch => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[ch] as string);
}

// Its own instance: hooks on the shared one would reach the chat's clipboard code.
let purifier: ReturnType<typeof DOMPurify> | null = null;

function outputPurifier(): ReturnType<typeof DOMPurify> {
  if (purifier) return purifier;
  const instance = DOMPurify(window);
  instance.addHook('afterSanitizeAttributes', node => {
    // Output must not phone home when a notebook is opened: only embedded pictures load.
    if (node.tagName === 'IMG' && !/^data:image\//i.test(node.getAttribute('src') ?? '')) node.removeAttribute('src');
    if (node.tagName === 'IMG') node.removeAttribute('srcset');
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
  return outputPurifier().sanitize(html, {
    FORBID_TAGS: ['style', 'form', 'input', 'button', 'textarea', 'select', 'iframe', 'object', 'embed', 'base', 'meta', 'link', 'video', 'audio', 'source'],
    FORBID_ATTR: ['formaction', 'background'],
  }) as string;
}
