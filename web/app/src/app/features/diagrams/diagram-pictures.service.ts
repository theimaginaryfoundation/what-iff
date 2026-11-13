import { DOCUMENT } from '@angular/common';
import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, defer, firstValueFrom, map, of, shareReplay, tap } from 'rxjs';

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
 * runs one diagram at a time. The last MAX_PICTURES file versions' pictures are kept, so each is
 * fetched or drawn once while it is in use; an older one's object URL is revoked. One let go while
 * it was still being found is kept again when it arrives, as the latest, since it is on its way to
 * the screen. Having no picture is not kept: the next request tries again.
 */
/** How many file versions' pictures are kept. */
export const MAX_PICTURES = 40;

/** A file version's picture: what asking for it gives, and its URL once there is one. */
type Picture = { url$: Observable<string | null>; url?: string };

@Injectable({ providedIn: 'root' })
export class DiagramPictures {
  private readonly document = inject(DOCUMENT);
  private readonly site = inject(DIAGRAMS_URL);
  private readonly http = inject(HttpClient);
  private readonly files = inject(FileAttachmentService);
  private readonly gallery = inject(ImageGalleryService);
  /** The pictures by file version, least recently asked for first. */
  private readonly pictures = new Map<string, Picture>();
  /** The drawing in progress; the next waits for it. */
  private drawing: Promise<unknown> = Promise.resolve();
  /** Draws a diagram as a PNG data URL; replaced in tests. */
  render = renderDiagramPng;

  /** The diagram's picture as a URL an `img` can show, or null when there is none and none can be drawn. */
  picture(file: FileAttachment): Observable<string | null> {
    const key = `${file.id}@${file.content_version ?? '?'}`;
    const kept = this.pictures.get(key);
    if (kept) {
      this.pictures.delete(key);
      this.pictures.set(key, kept);
      return kept.url$;
    }
    const picture: Picture = { url$: of(null) };
    picture.url$ = defer(() => this.find(file)).pipe(
      catchError(() => of(null)),
      tap(url => {
        if (url === null) this.forget(key, picture);
        else this.arrived(key, picture, url);
      }),
      shareReplay(1),
    );
    this.pictures.set(key, picture);
    this.trim();
    return picture.url$;
  }

  /** Records a found picture's URL, keeping it again if it was let go while being found. */
  private arrived(key: string, picture: Picture, url: string): void {
    picture.url = url;
    if (this.pictures.get(key) === picture) return;
    if (this.pictures.has(key)) {
      // Another request for this version is kept instead; this one is a spare.
      release(url);
      return;
    }
    this.pictures.set(key, picture);
    this.trim();
  }

  /** Lets go of the least recently asked for pictures beyond MAX_PICTURES, revoking their URLs. */
  private trim(): void {
    for (const [oldest, old] of this.pictures) {
      if (this.pictures.size <= MAX_PICTURES) break;
      this.pictures.delete(oldest);
      if (old.url) release(old.url);
    }
  }

  private forget(key: string, picture: Picture): void {
    if (this.pictures.get(key) === picture) this.pictures.delete(key);
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

/** Revokes a picture's object URL; a drawn picture's data URL needs nothing. */
function release(url: string): void {
  if (url.startsWith('blob:')) URL.revokeObjectURL(url);
}
