import { Injectable, inject } from '@angular/core';

import { FileAttachment } from '../app/core/models/file-attachment.model';
import { ShareApiService, SharedFile } from './share-api.service';

/**
 * The folder a share's files stand in, as the preview's project sees them (ADR 0x050 §2): any
 * gallery folder but the top level, as a page there would get only top-level files.
 */
export const SHARE_ROOT = 'shared';

/** How long a web app's listing is reused: a preview checks for changes every few seconds. */
const LISTING_TTL_MS = 30_000;

/** A shared file as the app's viewers take it, with an id that is its path, never the owner's. */
export function sharedAttachment(file: SharedFile): FileAttachment {
  const slash = file.path.lastIndexOf('/');
  return {
    id: `shared:${file.path}`,
    name: slash < 0 ? file.path : file.path.slice(slash + 1),
    folder: slash < 0 ? SHARE_ROOT : `${SHARE_ROOT}/${file.path.slice(0, slash)}`,
    file_type: file.file_type,
    content_version: file.content_version,
    size_bytes: file.size_bytes,
  } as FileAttachment;
}

/** A file's path in the share's project, from the attachment that stands for it. */
export function sharedPath(file: Pick<FileAttachment, 'id'>): string | null {
  return file.id.startsWith('shared:') ? file.id.slice('shared:'.length) : null;
}

/**
 * The share's files in place of the gallery's (FolderFiles, ADR 0x050 §2), so the app's viewers and
 * the preview read a share as they read the user's files. A single file is the one file of its
 * folder; a web app lists its project. Everything comes from the share API, which serves nothing
 * outside the share (ADR 0x051 §7).
 */
@Injectable({ providedIn: 'root' })
export class ShareFolderFiles {
  private readonly api = inject(ShareApiService);
  private key = '';
  private single: FileAttachment | null = null;
  private listing: { at: number; files: Promise<SharedFile[]> } | null = null;
  /** The clock, replaced in tests. */
  now = () => Date.now();

  /** The share whose files these are: a web app, or one file. */
  use(key: string, single: SharedFile | null): void {
    this.key = key;
    this.single = single ? sharedAttachment(single) : null;
    this.listing = null;
  }

  /** The share's files, by path in its project. */
  async all(fresh = false): Promise<SharedFile[]> {
    if (this.single) return [];
    if (!this.listing || (fresh && this.now() - this.listing.at >= LISTING_TTL_MS)) {
      const files = this.api.files(this.key);
      files.catch(() => this.listing?.files === files && (this.listing = null));
      this.listing = { at: this.now(), files };
    }
    return this.listing.files;
  }

  /** A folder's files by name, as FolderFiles lists them. */
  async list(folder: string, fresh = false): Promise<Map<string, FileAttachment>> {
    const byName = new Map<string, FileAttachment>();
    if (this.single) {
      if (folder === this.single.folder) byName.set(this.single.name, this.single);
      return byName;
    }
    for (const file of await this.all(fresh)) {
      const a = sharedAttachment(file);
      if (a.folder === folder) byName.set(a.name, a);
    }
    return byName;
  }

  /** A share has no conversation. */
  async conversation(): Promise<Map<string, FileAttachment>> {
    return new Map();
  }

  /** A file's bytes. */
  download(file: FileAttachment): Promise<Blob> {
    if (this.single && file.id === this.single.id) return this.api.content(this.key);
    const path = sharedPath(file);
    if (!path) return Promise.reject(new Error('Not a file of this share'));
    return this.api.file(this.key, path);
  }
}
