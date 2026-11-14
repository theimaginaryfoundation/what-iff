import { DOCUMENT } from '@angular/common';
import { Injectable, inject } from '@angular/core';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { bytesToBase64 } from '../../core/utils/base64';
import { FolderFiles } from './folder-files.service';
import { PAGE_BRIDGE_SCRIPT } from './page-bridge';
import { withSourcePositions } from './source-positions';
import { cssReferences, isStylesheet, pageReferences, rewriteCss, rewritePage, serializePage, servedType } from './page-assets';

/** What a lone page may pull from its folder: beyond this the rest stay unresolved. */
export const MAX_PAGE_FILES = 200;
export const MAX_PAGE_BYTES = 30 * 1024 * 1024;

/**
 * The policy a lone page runs under (ADR 0x050 §1), as the preview site's pages do under the site's:
 * scripts only inline (its own, and ours), from `data:` and `blob:` URLs (its folder's files) and the
 * two package CDNs; styles, images, fonts, media and fetch from anywhere https; nothing else. The
 * frame's origin is opaque, so there is no 'self'.
 */
export const LONE_PAGE_POLICY = [
  "default-src 'none'",
  "script-src 'unsafe-inline' 'unsafe-eval' data: blob: https://esm.sh https://cdn.jsdelivr.net",
  "style-src 'unsafe-inline' data: blob: https:",
  'img-src data: blob: https:',
  'font-src data: https:',
  'media-src data: blob: https:',
  'connect-src data: blob: https:',
  'worker-src data: blob:',
  'frame-src data: blob:',
  "object-src 'none'",
  "base-uri 'none'",
  "form-action 'none'",
].join('; ');

/** The page with LONE_PAGE_POLICY first in its head, ahead of every script (ours is first). */
function withPolicy(html: string): string {
  const at = html.indexOf(`<script>${PAGE_BRIDGE_SCRIPT.slice(0, 40)}`);
  const meta = `<meta http-equiv="Content-Security-Policy" content="${LONE_PAGE_POLICY}">`;
  return at < 0 ? meta + html : html.slice(0, at) + meta + html.slice(at);
}

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
 * origin's blobs. Each element's source range and our bridge script are added first (ADR 0x050 §5);
 * a page that names no file gets only those, without a request.
 */
@Injectable({ providedIn: 'root' })
export class PageFiles {
  private readonly document = inject(DOCUMENT);
  private readonly folders = inject(FolderFiles);

  async build(page: FileAttachment, source: string): Promise<BuiltPage> {
    // Each element's source range and our bridge script first (ADR 0x050 §5).
    const html = withPolicy(withSourcePositions(source, page.id, PAGE_BRIDGE_SCRIPT));
    const view = this.document.defaultView;
    const doc = new (view?.DOMParser ?? DOMParser)().parseFromString(html, 'text/html');
    const wanted = [...pageReferences(doc)];
    if (!wanted.length) return { html, missing: [], skipped: [] };

    const folder = await this.folders.list(page.folder ?? '');
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
      // Its size from the listing, before downloading it: one huge file is never fetched.
      if (fetched.size >= MAX_PAGE_FILES || bytes + (file.size_bytes ?? 0) > MAX_PAGE_BYTES) {
        skipped.push(name);
        continue;
      }
      const blob = await this.folders.download(file);
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
}

function dataUrl(type: string, bytes: Uint8Array): string {
  return `data:${type};base64,${bytesToBase64(bytes)}`;
}
