import { DOCUMENT } from '@angular/common';
import { Injectable, inject } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { setMermaidEditor } from '../../core/utils/mermaid.helpers';
import { DIAGRAMS_URL } from './diagram-site.config';
import { convertMermaidToDrawio } from './drawio-convert';

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
