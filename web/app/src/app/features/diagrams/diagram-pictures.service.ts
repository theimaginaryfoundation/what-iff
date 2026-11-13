import { DOCUMENT } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, defer, firstValueFrom, map, of, shareReplay } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { DIAGRAMS_URL } from './diagram-site.config';
import { renderDiagramPng } from './drawio-convert';
import { dataUrlToBlob } from './drawio-frame';

/**
 * A picture of a diagram to show where a file's picture goes, as images have (a message's
 * attachments): its stored preview (ADR 0x049 §8), or, for one no editor has shown yet (just
 * uploaded, made by an agent), one drawn now in a hidden editor and stored as its preview. Drawing
 * runs one diagram at a time, and each file version is fetched or drawn once.
 */
@Injectable({ providedIn: 'root' })
export class DiagramPictures {
  private readonly document = inject(DOCUMENT);
  private readonly site = inject(DIAGRAMS_URL);
  private readonly http = inject(HttpClient);
  private readonly files = inject(FileAttachmentService);
  private readonly gallery = inject(ImageGalleryService);
  private readonly pictures = new Map<string, Observable<string | null>>();
  /** The drawing in progress; the next waits for it. */
  private drawing: Promise<unknown> = Promise.resolve();
  /** Draws a diagram as a PNG data URL; replaced in tests. */
  render = renderDiagramPng;

  /** The diagram's picture as a URL an `img` can show, or null when there is none and none can be drawn. */
  picture(file: FileAttachment): Observable<string | null> {
    const key = `${file.id}@${file.content_version ?? '?'}`;
    let picture = this.pictures.get(key);
    if (!picture) {
      picture = defer(() => this.find(file)).pipe(
        catchError(() => of(null)),
        shareReplay(1),
      );
      this.pictures.set(key, picture);
    }
    return picture;
  }

  private async find(file: FileAttachment): Promise<string | null> {
    const stored = await firstValueFrom(
      this.http.get(this.files.previewUrl(file.id, 'full'), { responseType: 'blob' }).pipe(
        map(blob => URL.createObjectURL(blob) as string | null),
        catchError(() => of(null)),
      ),
    );
    if (stored || !this.site) return stored;
    const site = this.site;
    const turn = this.drawing.then(() => this.draw(file, site));
    this.drawing = turn.catch(() => undefined);
    return turn;
  }

  private async draw(file: FileAttachment, site: string): Promise<string | null> {
    const version = file.content_version ?? (await firstValueFrom(this.gallery.getFileInfo(file.id))).content_version;
    const xml = await (await firstValueFrom(this.files.downloadFileAttachment(file.id))).text();
    const png = await this.render(this.document, site, xml);
    const blob = dataUrlToBlob(png);
    // Stored as the preview, so the gallery and agents have it too; best effort.
    if (blob && version) this.files.putPreview(file.id, version, blob).subscribe({ error: () => undefined });
    return png;
  }
}
