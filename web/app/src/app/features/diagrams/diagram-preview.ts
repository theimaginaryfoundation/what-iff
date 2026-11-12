import { firstValueFrom } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { dataUrlToBlob } from './drawio-frame';

/**
 * A diagram's preview (ADR 0x049 §8): the server cannot draw a diagram, so the editor that shows
 * one sends a picture of it, for the gallery's thumbnail and for agents to look at. It is drawn
 * when a diagram is shown that has none of its current version (a new file, one an agent changed),
 * and after every save. Best effort: a failure leaves the file without one until it is next shown.
 */
export class DiagramPreview {
  /** The version a picture was last sent for (or is being), so each is drawn once. */
  private sentFor: string | null = null;

  constructor(
    private readonly files: Pick<FileAttachmentService, 'hasPreview' | 'putPreview'>,
    /** A PNG of the diagram as the editor shows it now, as a data URL, or null. */
    private readonly drawPng: () => Promise<string | null>,
  ) {}

  /** The editor shows `file` as it is stored: draws its preview if it has none. */
  async shown(file: FileAttachment): Promise<void> {
    const version = file.content_version;
    if (!version || this.sentFor === key(file.id, version)) return;
    this.sentFor = key(file.id, version);
    if (await firstValueFrom(this.files.hasPreview(file.id))) return;
    await this.send(file.id, version);
  }

  /** The editor's diagram was saved as `file`: draws the new version's preview. */
  async saved(file: FileAttachment): Promise<void> {
    const version = file.content_version;
    if (!version) return;
    this.sentFor = key(file.id, version);
    await this.send(file.id, version);
  }

  private async send(id: string, version: number): Promise<void> {
    try {
      const data = await this.drawPng();
      const png = data ? dataUrlToBlob(data) : null;
      if (png) await firstValueFrom(this.files.putPreview(id, version, png));
    } catch {
      // The file changed meanwhile (409), or the editor could not draw it: drawn when next shown.
      this.sentFor = null;
    }
  }
}

function key(id: string, version: number): string {
  return `${id}@${version}`;
}
