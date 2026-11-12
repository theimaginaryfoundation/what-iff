import { DOCUMENT } from '@angular/common';
import { Injectable, inject } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { setMermaidEditor } from '../../core/utils/mermaid.helpers';
import { DIAGRAMS_URL } from './diagram-site.config';
import { drawioUrl, readDrawioMessage } from './drawio-frame';

/** How long the editor may take to start and convert a diagram. */
const CONVERT_TIMEOUT_MS = 45_000;
/** How many times the converted diagram is asked for before it is given up on. */
const MAX_EXPORTS = 10;

/**
 * The diagram in draw.io's XML: Mermaid `source` loaded into a hidden editor from `site`, which
 * draws it as native shapes (its `load` takes a Mermaid descriptor, ADR 0x049 §4), then exported.
 */
export function convertMermaidToDrawio(document: Document, site: string, source: string, timeoutMs = CONVERT_TIMEOUT_MS): Promise<string> {
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
    const send = (message: unknown) => frame.contentWindow?.postMessage(JSON.stringify(message), origin);
    const onMessage = (event: MessageEvent) => {
      const message = readDrawioMessage(event, origin, frame.contentWindow);
      if (!message) return;
      if (message.event === 'init') send({ action: 'load', descriptor: { format: 'mermaid', data: source }, autosave: 1 });
      else if (message.event === 'load') send({ action: 'export', format: 'xml' });
      else if (message.event === 'export') {
        const xml = (message as { xml?: string }).xml ?? '';
        // The shapes may still be being drawn just after the load: asked again, a few times.
        if (/<mxCell [^>]*vertex="1"/.test(xml)) finish(null, xml);
        else if (++exports < MAX_EXPORTS) setTimeout(() => send({ action: 'export', format: 'xml' }), 300);
        else finish(new Error('The editor could not draw this diagram.'));
      }
    };
    const timer = setTimeout(() => finish(new Error('The diagram editor did not answer.')), timeoutMs);
    view?.addEventListener('message', onMessage);
    document.body.appendChild(frame);
  });
}

/** A file name for a diagram converted from Mermaid: its title, or its kind. */
export function mermaidFileName(source: string): string {
  const title = /^---\s*\n(?:[\s\S]*?\n)?title:\s*(.+?)\s*\n[\s\S]*?---/.exec(source.trimStart())?.[1];
  const kind = source
    .replace(/^---[\s\S]*?---\s*/, '')
    .replace(/^%%.*$/gm, '')
    .trim()
    .split(/\s/)[0];
  const base =
    (title || kind || 'diagram')
      .replace(/["/\\<>:|?*]/g, '')
      .trim()
      .slice(0, 80) || 'diagram';
  return `${base}.drawio`;
}

/**
 * "Edit" on every drawn Mermaid diagram (ADR 0x049 §4): converts it in the diagram editor, saves
 * the result as a new .drawio file in the gallery, and opens it there. Only where this server has
 * a diagram editor.
 */
@Injectable({ providedIn: 'root' })
export class MermaidDiagramOpener {
  private readonly document = inject(DOCUMENT);
  private readonly site = inject(DIAGRAMS_URL);
  private readonly gallery = inject(ImageGalleryService);
  private readonly router = inject(Router);

  /** Offers Edit on Mermaid diagrams, when there is an editor to open them in. */
  start(): void {
    setMermaidEditor(this.site ? source => this.open(source) : null);
  }

  async open(source: string): Promise<void> {
    if (!this.site) return;
    const xml = await convertMermaidToDrawio(this.document, this.site, source);
    const name = mermaidFileName(source);
    const file = await firstValueFrom(this.gallery.importImage(new File([xml], name, { type: 'application/vnd.jgraph.mxfile' })));
    await this.router.navigate(['/gallery'], { queryParams: { file: file.id } });
  }
}
