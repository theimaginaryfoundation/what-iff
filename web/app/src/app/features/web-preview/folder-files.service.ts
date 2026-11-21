import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';

import { FileAttachment } from '../../core/models/file-attachment.model';
import { FileAttachmentService } from '../../core/services/file-attachment.service';
import { ImageGalleryService } from '../../core/services/image-gallery.service';

/** How many of a folder's files are listed (pages of LIST_PAGE). */
const LIST_PAGE = 200;
const MAX_LIST_PAGES = 5;
/** How long a folder's listing is reused: a page rebuilt while typing lists its folder once. */
const LISTING_TTL_MS = 5_000;
/** How many downloaded files are kept, by file version. */
const MAX_CACHED_FILES = 400;

/**
 * The gallery's folders as a page sees them (ADR 0x050 §1, §2): a folder's files by name, listed
 * at most once every few seconds, and their bytes, fetched once per version while they are among
 * the recent ones. Shared by a lone page and the preview site.
 */
@Injectable({ providedIn: 'root' })
export class FolderFiles {
  private readonly files = inject(FileAttachmentService);
  private readonly gallery = inject(ImageGalleryService);
  /** Recent folder listings, by folder; a conversation's under a key no folder can have. */
  private readonly listings = new Map<string, { at: number; files: Promise<Map<string, FileAttachment>> }>();
  /** Downloaded files by id and content version, least recently used first. */
  private readonly downloads = new Map<string, Promise<Blob>>();
  /** The clock, replaced in tests. */
  now = () => Date.now();

  /** The folder's files by name (the newest of a name wins); `fresh` lists it again now. */
  list(folder: string, fresh = false): Promise<Map<string, FileAttachment>> {
    return this.listed(folder, fresh, () => this.listFolder(folder));
  }

  /**
   * A conversation's files by workspace path (its context, ADR 0x043 §4, at the paths of ADR 0x050
   * §7), the one that takes each path: files shadowed by a newer one are left out.
   */
  conversation(chatId: string, fresh = false): Promise<Map<string, FileAttachment>> {
    return this.listed(`\u0000chat:${chatId}`, fresh, async () => {
      // Without the container's status, which costs a call to the container service per poll.
      const response = await firstValueFrom(this.files.listChatContextFiles(chatId, { container: false }));
      const byPath = new Map<string, FileAttachment>();
      for (const f of response.files) if (!f.shadowed_by) byPath.set(f.workspace_path, f.file);
      return byPath;
    });
  }

  private listed(key: string, fresh: boolean, list: () => Promise<Map<string, FileAttachment>>): Promise<Map<string, FileAttachment>> {
    const kept = this.listings.get(key);
    if (kept && !fresh && this.now() - kept.at < LISTING_TTL_MS) return kept.files;
    const files = list();
    files.catch(() => this.listings.get(key)?.files === files && this.listings.delete(key));
    this.listings.set(key, { at: this.now(), files });
    return files;
  }

  /** A file's bytes. */
  download(file: FileAttachment): Promise<Blob> {
    const key = `${file.id}@${file.content_version ?? '?'}`;
    let blob = this.downloads.get(key);
    if (blob) {
      this.downloads.delete(key);
    } else {
      blob = firstValueFrom(this.files.downloadFileAttachment(file.id));
      // A failed download is not kept, so the next request tries again.
      blob.catch(() => this.downloads.get(key) === blob && this.downloads.delete(key));
    }
    this.downloads.set(key, blob);
    for (const oldest of this.downloads.keys()) {
      if (this.downloads.size <= MAX_CACHED_FILES) break;
      this.downloads.delete(oldest);
    }
    return blob;
  }

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
