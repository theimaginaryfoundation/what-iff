import { DOCUMENT } from '@angular/common';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { bytesToBase64 } from '../../core/utils/base64';
import { ImageGalleryService } from '../../core/services/image-gallery.service';
import { cssReferences, isStylesheet, pageReferences, rewriteCss, rewritePage, serializePage, servedType } from './page-assets';

/** How many of the folder's files are listed to find the page's (pages of LIST_PAGE). */
const LIST_PAGE = 200;
const MAX_LIST_PAGES = 5;
/** What a lone page may pull from its folder: beyond this the rest stay unresolved. */
export const MAX_PAGE_FILES = 200;
export const MAX_PAGE_BYTES = 30 * 1024 * 1024;
/** How long a folder's listing is reused: a page rebuilt while typing lists its folder once. */
const LISTING_TTL_MS = 5_000;
/** How many downloaded files are kept for rebuilds, by file version. */
const MAX_CACHED_FILES = 400;

/** A page ready for its frame. */
export interface BuiltPage {
  /** The page, its sibling references swapped for `data:` URLs. */
  html: string;
  /** Names the page or its stylesheets reference that are not in its folder. */
  missing: string[];
  /** Names left unresolved because the page asked for more than a lone page may load. */
  skipped: string[];
}

/**
 * Builds a lone `.html` file's page (ADR 0x050 §1): the files it names beside it in its folder are
 * fetched, stylesheets are rewritten to point at their own files, and every reference is swapped for
 * a `data:` URL. Not a `blob:` URL: the frame's origin is opaque, and a browser refuses it another
 * origin's blobs. A page that names no file is returned as it is, without a request.
 */
@Injectable({ providedIn: 'root' })
export class PageFiles {
  private readonly document = inject(DOCUMENT);
  private readonly files = inject(FileAttachmentService);
  private readonly gallery = inject(ImageGalleryService);
  /** Recent folder listings, by folder. */
  private readonly listings = new Map<string, { at: number; files: Promise<Map<string, FileAttachment>> }>();
  /** Downloaded files by id and content version, least recently used first. */
  private readonly downloads = new Map<string, Promise<Blob>>();
  /** The clock, replaced in tests. */
  now = () => Date.now();

  async build(page: FileAttachment, html: string): Promise<BuiltPage> {
    const view = this.document.defaultView;
    const doc = new (view?.DOMParser ?? DOMParser)().parseFromString(html, 'text/html');
    const wanted = [...pageReferences(doc)];
    if (!wanted.length) return { html, missing: [], skipped: [] };

    const folder = await this.folderFiles(page);
    const fetched = new Map<string, { file: FileAttachment; blob: Blob; css?: string }>();
    const missing = new Set<string>();
    const skipped: string[] = [];
    let bytes = 0;
    // Breadth first: the page's names, then those its stylesheets name, and so on.
    const queue = [...wanted];
    const seen = new Set(queue);
    while (queue.length) {
      const name = queue.shift()!;
      const file = folder.get(name);
      if (!file) {
        missing.add(name);
        continue;
      }
      if (fetched.size >= MAX_PAGE_FILES || bytes >= MAX_PAGE_BYTES) {
        skipped.push(name);
        continue;
      }
      const blob = await this.download(file);
      bytes += blob.size;
      const entry: { file: FileAttachment; blob: Blob; css?: string } = { file, blob };
      if (isStylesheet(name)) {
        entry.css = await blob.text();
        for (const ref of cssReferences(entry.css)) {
          if (!seen.has(ref)) {
            seen.add(ref);
            queue.push(ref);
          }
        }
      }
      fetched.set(name, entry);
    }

    const urls = new Map<string, string>();
    for (const [name, entry] of fetched) {
      if (entry.css === undefined)
        urls.set(name, dataUrl(servedType(name, entry.file.file_type), new Uint8Array(await entry.blob.arrayBuffer())));
    }
    // Stylesheets last, deepest first, so one that imports another points at its finished URL.
    for (const [name, entry] of [...fetched].reverse()) {
      if (entry.css !== undefined) urls.set(name, dataUrl('text/css', new TextEncoder().encode(rewriteCss(entry.css, urls))));
    }
    rewritePage(doc, urls);
    return { html: serializePage(doc), missing: [...missing], skipped };
  }

  /** A file's bytes, fetched once per version while it stays among the recent ones. */
  private download(file: FileAttachment): Promise<Blob> {
    const key = `${file.id}@${file.content_version ?? '?'}`;
    let blob = this.downloads.get(key);
    if (blob) {
      this.downloads.delete(key);
    } else {
      blob = firstValueFrom(this.files.downloadFileAttachment(file.id));
      // A failed download is not kept, so the next build tries again.
      blob.catch(() => this.downloads.get(key) === blob && this.downloads.delete(key));
    }
    this.downloads.set(key, blob);
    for (const oldest of this.downloads.keys()) {
      if (this.downloads.size <= MAX_CACHED_FILES) break;
      this.downloads.delete(oldest);
    }
    return blob;
  }

  /** The page's folder's files, listed at most once every few seconds. */
  private folderFiles(page: FileAttachment): Promise<Map<string, FileAttachment>> {
    const folder = page.folder ?? '';
    const kept = this.listings.get(folder);
    if (kept && this.now() - kept.at < LISTING_TTL_MS) return kept.files;
    const files = this.listFolder(folder);
    files.catch(() => this.listings.get(folder)?.files === files && this.listings.delete(folder));
    this.listings.set(folder, { at: this.now(), files });
    return files;
  }

  /** The files in a folder, by name: the newest of a name wins. */
  private async listFolder(folder: string): Promise<Map<string, FileAttachment>> {
    const byName = new Map<string, FileAttachment>();
    for (let n = 1; n <= MAX_LIST_PAGES; n++) {
      const list = await firstValueFrom(this.gallery.listImages(n, LIST_PAGE, { folder, kind: 'all' }));
      for (const file of list.results) {
        const kept = byName.get(file.name);
        if (!kept || (file.created_at ?? '') > (kept.created_at ?? '')) byName.set(file.name, file);
      }
      if (n * LIST_PAGE >= list.total_count || !list.results.length) break;
    }
    return byName;
  }
}

function dataUrl(type: string, bytes: Uint8Array): string {
  return `data:${type};base64,${bytesToBase64(bytes)}`;
}
