/**
 * The draw.io embed protocol (ADR 0x049), as far as the file workspace uses it: the editor runs in
 * an iframe on its own origin (embed=1, proto=json) and the two sides exchange JSON messages.
 *
 * - The editor says `init` once it is ready; we answer `load` with the file's XML.
 * - While editing, every change comes back as `autosave` with the whole diagram's XML.
 * - `export` asks for an image (png, svg, xmlpng, xmlsvg: the last two carry the diagram inside).
 * - `load` also takes Mermaid (`descriptor: { format: 'mermaid', data }`), which draw.io turns into
 *   native shapes.
 */

/** An element of the diagram as the bridge plugin reports it: its cell id, and its label as plain text. */
export interface BridgeCell {
  id: string;
  label: string;
}

/**
 * What our plugin in the editor (web/diagrams/whatiff-bridge.js) reports, for comments on elements:
 * the page shown, the selection, an element clicked, a right-click menu item chosen; and a Visio
 * file converted.
 */
export type BridgeEvent =
  | { event: 'whatiff-page'; page: string | null }
  | { event: 'whatiff-selection'; page: string | null; cells: BridgeCell[] }
  | { event: 'whatiff-click'; page: string | null; cell: string; label?: string }
  | { event: 'whatiff-menu'; page: string | null; item: 'new' | 'reply' | 'delete'; cell: string; label?: string }
  // A Visio file converted (draw.io's XML, possibly compressed), or why not.
  | { event: 'whatiff-imported'; xml: string }
  | { event: 'whatiff-import-failed'; message: string };

export type DrawioEvent =
  | BridgeEvent
  | { event: 'init' }
  | { event: 'load'; xml: string }
  | { event: 'autosave'; xml: string }
  | { event: 'save'; xml: string }
  | { event: 'export'; data: string; format: string; xml?: string; message?: { requestId?: string } }
  | { event: 'exit' }
  | { event: 'configure' };

export type DrawioAction =
  | { action: 'load'; xml: string; autosave?: 1; title?: string; noExitBtn?: 1 }
  | { action: 'load'; descriptor: { format: 'mermaid'; data: string }; autosave?: 1; title?: string }
  | { action: 'export'; format: 'png' | 'svg' | 'xmlpng' | 'xmlsvg' | 'xml'; scale?: number; background?: string; requestId?: string }
  | { action: 'configure'; config: Record<string, unknown> }
  // Our plugin: which elements on a page have threads (and which is active), and showing one.
  | { action: 'whatiff-marks'; page: string | null; marks: Record<string, 'open' | 'active'> }
  | { action: 'whatiff-reveal'; cell: string; page?: string }
  // Our plugin: convert a Visio file (base64 .vsdx) with draw.io's importer.
  | { action: 'whatiff-import-visio'; data: string; name: string };

/** The editor's URL: embed mode, our JSON protocol, the app's theme, and read-only when viewing. */
export function drawioUrl(site: string, opts: { readOnly: boolean; dark: boolean }): string {
  const url = new URL(site);
  const params: Record<string, string> = {
    embed: '1',
    proto: 'json',
    spin: '1',
    lang: 'en',
    // No save or exit buttons: the workspace saves the file and closes the editor.
    noSaveBtn: '1',
    noExitBtn: '1',
    saveAndExit: '0',
    // Libraries panel on, with the general shapes.
    libraries: '1',
    ui: 'kennedy',
    dark: opts.dark ? '1' : '0',
  };
  // Viewing: no menus, toolbars or panels, and nothing can be changed.
  if (opts.readOnly) Object.assign(params, { chrome: '0', toolbar: '0' });
  for (const [k, v] of Object.entries(params)) url.searchParams.set(k, v);
  return url.href;
}

/** A message from the editor, or null for anything else (another frame's, or not ours). */
export function readDrawioMessage(event: MessageEvent, origin: string, frame: Window | null | undefined): DrawioEvent | null {
  if (event.origin !== origin || !frame || event.source !== frame || typeof event.data !== 'string') return null;
  try {
    const message = JSON.parse(event.data) as { event?: unknown };
    return typeof message?.event === 'string' ? (message as DrawioEvent) : null;
  } catch {
    return null;
  }
}

/** An empty diagram, for a new or empty .drawio file. */
export const EMPTY_DIAGRAM = '';

/** The bytes of an export's `data:` URL (base64 or percent-encoded), or null if it is not one. */
export function dataUrlToBlob(url: string): Blob | null {
  const match = /^data:([^;,]+)((?:;[^;,]+)*),(.*)$/s.exec(url);
  if (!match) return null;
  const [, type, params, payload] = match;
  if (!params.split(';').includes('base64')) return new Blob([decodeURIComponent(payload)], { type });
  const binary = atob(payload);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return new Blob([bytes], { type });
}

/** A download's name: the diagram's file name with the image's extension in place of .drawio. */
export function exportName(fileName: string, format: 'png' | 'svg'): string {
  const base = fileName.replace(/\.drawio(\.xml)?$/i, '').replace(/\.xml$/i, '') || 'diagram';
  return `${base}.${format}`;
}
