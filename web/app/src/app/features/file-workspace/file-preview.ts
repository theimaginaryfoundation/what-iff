import { firstValueFrom } from 'rxjs';

import { FileAttachment, PreviewSource } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { dataUrlToBlob } from '../diagrams/drawio-frame';

/**
 * A file's preview (ADR 0x049 §8, ADR 0x050 §8): the server cannot draw a diagram or a web page, so
 * the browser that shows one sends a picture of it, for the gallery's thumbnail and for agents to
 * look at. It is drawn when the file is shown and has none that is current (a new file, one an agent
 * changed, or a page one of whose files changed), and after every save. Best effort: a failure
 * leaves the file without one until it is next shown.
 *
 * A page's picture shows the files it loads too, so it is sent with them (`sources`), at their
 * versions: the server can then tell a picture a change to one of them left behind.
 */
export class FilePreview {
  /** What a picture was last sent for (or is being), so each is drawn once. */
  private sentFor: string | null = null;

  constructor(
    private readonly files: Pick<FileAttachmentService, 'hasPreview' | 'putPreview'>,
    /** A PNG of the file as it is shown now, as a data URL, or null. */
    private readonly drawPng: () => Promise<string | null>,
    /** The other files what is shown was drawn from, at their versions (a page's). */
    private readonly sources: () => readonly PreviewSource[] = () => [],
  ) {}

  /** `file` is shown as it is stored: draws its preview if it has none that is current. */
  async shown(file: FileAttachment): Promise<void> {
    const version = file.content_version;
    if (!version) return;
    const sources = this.sources();
    const sent = key(file.id, version, sources);
    if (this.sentFor === sent) return;
    this.sentFor = sent;
    if (await firstValueFrom(this.files.hasPreview(file.id, sources.length > 0))) return;
    await this.send(file.id, version, sources);
  }

  /** What is shown was saved as `file`: draws the new version's preview. */
  async saved(file: FileAttachment): Promise<void> {
    const version = file.content_version;
    if (!version) return;
    const sources = this.sources();
    this.sentFor = key(file.id, version, sources);
    await this.send(file.id, version, sources);
  }

  private async send(id: string, version: number, sources: readonly PreviewSource[]): Promise<void> {
    try {
      const data = await this.drawPng();
      const png = data ? dataUrlToBlob(data) : null;
      // Nothing drawn (the page did not answer in time, or could not draw itself): drawn when next shown.
      if (!png) this.sentFor = null;
      else await firstValueFrom(this.files.putPreview(id, version, png, sources));
    } catch {
      // The file changed meanwhile (409), or it could not be drawn: drawn when next shown.
      this.sentFor = null;
    }
  }
}

function key(id: string, version: number, sources: readonly PreviewSource[]): string {
  return [`${id}@${version}`, ...sources.map(s => `${s.id}@${s.content_version}`).sort()].join(' ');
}
