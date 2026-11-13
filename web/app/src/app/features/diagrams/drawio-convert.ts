import { DrawioAction, DrawioEvent, drawioUrl, readDrawioMessage } from './drawio-frame';

/** How long the editor may take to start and convert a diagram. */
const CONVERT_TIMEOUT_MS = 45_000;
/** How many times the converted diagram is asked for before it is given up on. */
const MAX_EXPORTS = 10;

/**
 * The diagram in draw.io's plain XML: Mermaid `source` loaded into a hidden editor from `site`,
 * which draws it as native shapes (its `load` takes a Mermaid descriptor, ADR 0x049 §4), then
 * exported.
 */
export function convertMermaidToDrawio(document: Document, site: string, source: string, timeoutMs = CONVERT_TIMEOUT_MS): Promise<string> {
  return convertInEditor(document, site, timeoutMs, {
    start: send => send({ action: 'load', descriptor: { format: 'mermaid', data: source }, autosave: 1 }),
  });
}

/**
 * A Visio drawing (.vsdx) as a draw.io diagram in plain XML (ADR 0x049 §7): draw.io's own importer
 * converts it in a hidden editor (our plugin runs it), and the result is loaded and exported again,
 * so it is saved uncompressed.
 */
export async function convertVisioToDrawio(
  document: Document,
  site: string,
  file: Blob,
  name: string,
  timeoutMs = CONVERT_TIMEOUT_MS,
): Promise<string> {
  const data = await blobToBase64(file);
  return convertInEditor(document, site, timeoutMs, {
    start: send => send({ action: 'whatiff-import-visio', data, name }),
    onMessage: (message, send, fail) => {
      if (message.event === 'whatiff-imported') send({ action: 'load', xml: message.xml, autosave: 1 });
      else if (message.event === 'whatiff-import-failed') fail(new Error(`The Visio file could not be converted: ${message.message}`));
    },
  });
}

/**
 * A PNG of a diagram (draw.io XML), drawn on white in a hidden editor, as a `data:` URL: a diagram's
 * preview where no editor has shown it yet (ADR 0x049 §8).
 */
export function renderDiagramPng(document: Document, site: string, xml: string, timeoutMs = CONVERT_TIMEOUT_MS): Promise<string> {
  return convertInEditor(document, site, timeoutMs, {
    start: send => send({ action: 'load', xml, autosave: 1 }),
    export: { action: 'export', format: 'png', scale: 1, background: '#ffffff' },
    result: message => (message.data?.startsWith('data:image/png') ? message.data : null),
  });
}

type ExportEvent = Extract<DrawioEvent, { event: 'export' }>;

interface EditorScript {
  /** What is sent once the editor is ready; a `load` follows, then the diagram is exported. */
  start: (send: (action: DrawioAction) => void) => void;
  /** Any other message, before the `load`. */
  onMessage?: (message: DrawioEvent, send: (action: DrawioAction) => void, fail: (error: Error) => void) => void;
  /** What is asked for once the diagram is loaded; its plain XML by default. */
  export?: DrawioAction;
  /** The result in the export's answer, or null to ask again; by default the XML, once it has shapes. */
  result?: (message: ExportEvent) => string | null;
}

const xmlWithShapes = (message: ExportEvent) => {
  const xml = message.xml ?? '';
  return /<mxCell [^>]*vertex="1"/.test(xml) ? xml : null;
};

/**
 * Runs `script` in a hidden editor and resolves with what the export gives once the diagram is
 * drawn (by default its plain XML, once it has shapes). The editor is removed once it is done, or
 * has failed or timed out.
 */
function convertInEditor(document: Document, site: string, timeoutMs: number, script: EditorScript): Promise<string> {
  const origin = new URL(site).origin;
  const frame = document.createElement('iframe');
  frame.setAttribute('sandbox', 'allow-scripts allow-same-origin');
  frame.setAttribute('aria-hidden', 'true');
  frame.tabIndex = -1;
  // Off screen rather than hidden: the editor measures its labels as it lays the diagram out.
  frame.style.cssText = 'position:fixed;left:-10000px;top:0;width:1200px;height:800px;border:0;';
  frame.src = drawioUrl(site, { readOnly: false, dark: false });
  const view = document.defaultView;
  return new Promise<string>((resolve, reject) => {
    let done = false;
    let exports = 0;
    const finish = (error: Error | null, xml?: string) => {
      if (done) return;
      done = true;
      clearTimeout(timer);
      view?.removeEventListener('message', onMessage);
      frame.remove();
      if (error) reject(error);
      else resolve(xml ?? '');
    };
    const send = (action: DrawioAction) => frame.contentWindow?.postMessage(JSON.stringify(action), origin);
    const exportDiagram = () => send(script.export ?? { action: 'export', format: 'xml' });
    const result = script.result ?? xmlWithShapes;
    const onMessage = (event: MessageEvent) => {
      const message = readDrawioMessage(event, origin, frame.contentWindow);
      if (!message) return;
      if (message.event === 'init') script.start(send);
      else if (message.event === 'load') exportDiagram();
      else if (message.event === 'export') {
        const got = result(message);
        // The shapes may still be being drawn just after the load: asked again, a few times.
        if (got) finish(null, got);
        else if (++exports < MAX_EXPORTS) setTimeout(exportDiagram, 300);
        else finish(new Error('The editor could not draw this diagram.'));
      } else script.onMessage?.(message, send, error => finish(error));
    };
    const timer = setTimeout(() => finish(new Error('The diagram editor did not answer.')), timeoutMs);
    view?.addEventListener('message', onMessage);
    document.body.appendChild(frame);
  });
}

async function blobToBase64(blob: Blob): Promise<string> {
  const bytes = new Uint8Array(await blob.arrayBuffer());
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(binary);
}
