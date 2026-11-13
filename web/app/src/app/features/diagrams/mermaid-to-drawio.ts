import { DOCUMENT } from '@angular/common';
import { Injectable, inject } from '@angular/core';
import { Router } from '@angular/router';
import { firstValueFrom } from 'rxjs';

import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { setMermaidEditor } from '../../core/utils/mermaid.helpers';
import { ContextPanelService } from '../chat/services/context-panel.service';
import { DIAGRAMS_URL } from './diagram-site.config';
import { DRAWIO_MIME_TYPE } from './diagram-uploads';
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
 * the result as a new .drawio file, and opens it: in a conversation, in its workspace and on the
 * message being written; anywhere else, in the gallery. Only where this server has a diagram editor.
 */
@Injectable({ providedIn: 'root' })
export class MermaidDiagramOpener {
  private readonly document = inject(DOCUMENT);
  private readonly site = inject(DIAGRAMS_URL);
  private readonly gallery = inject(ImageGalleryService);
  private readonly files = inject(FileAttachmentService);
  private readonly contextPanel = inject(ContextPanelService);
  private readonly router = inject(Router);
  /** Converts the Mermaid; replaced in tests. */
  convert = convertMermaidToDrawio;

  /** Offers Edit on Mermaid diagrams, when there is an editor to open them in. */
  start(): void {
    setMermaidEditor(this.site ? source => this.open(source) : null);
  }

  async open(source: string): Promise<void> {
    if (!this.site) return;
    const xml = await this.convert(this.document, this.site, source);
    const diagram = new File([xml], mermaidFileName(source), { type: DRAWIO_MIME_TYPE });
    const chatId = this.conversationShown();
    if (chatId) {
      const file = await firstValueFrom(this.files.uploadChatFileAttachment(chatId, diagram));
      // As Save As in a conversation: on the message being written, so the agent sees it (ADR 0x043 §4).
      this.contextPanel.requestComposerAttachment(file);
      await this.router.navigate(['/chat', chatId], { queryParams: { file: file.id } });
      return;
    }
    const file = await firstValueFrom(this.gallery.importImage(diagram));
    await this.router.navigate(['/gallery'], { queryParams: { file: file.id } });
  }

  /** The conversation on screen (`/chat/:id`), whose message or file the diagram is in, or null. */
  private conversationShown(): string | null {
    const segments = this.router.parseUrl(this.router.url).root.children['primary']?.segments ?? [];
    return segments[0]?.path === 'chat' && segments[1]?.path ? segments[1].path : null;
  }
}
