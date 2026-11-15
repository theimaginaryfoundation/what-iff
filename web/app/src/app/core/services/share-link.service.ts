import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, map } from 'rxjs';

import { environment } from '../../../environments/environment';
import { ShareLink, ShareState } from '../models/share-link.model';

/**
 * Share links (ADR 0x051): share a file, read and change its link, stop sharing it, and list every
 * link of the user's. Sharing is offered only where the build names a share site (`shareUrl`).
 */
@Injectable({ providedIn: 'root' })
export class ShareLinkService {
  private readonly http = inject(HttpClient);
  private readonly apiUrl = environment.apiUrl;

  /** The share site, without a trailing slash; empty when the build has none. */
  readonly siteUrl = shareSiteUrl((environment as { shareUrl?: string }).shareUrl);

  /** Whether sharing is offered in this build. */
  get enabled(): boolean {
    return this.siteUrl !== '';
  }

  /** The link a visitor opens. */
  urlOf(link: Pick<ShareLink, 'key'>): string {
    return `${this.siteUrl}/s/${link.key}`;
  }

  /** What sharing the file shares, and its link (null when it is not shared). */
  get(fileId: string): Observable<ShareState> {
    return this.http.get<ShareState>(this.fileUrl(fileId));
  }

  /** Shares the file (with a passcode unless told otherwise); a shared file keeps its link. */
  share(fileId: string, passcodeRequired = true): Observable<ShareLink> {
    return this.http.put<ShareLink>(this.fileUrl(fileId), { passcode_required: passcodeRequired });
  }

  /** Turns the passcode on (a new one) or off, or makes a new one: everyone who unlocked it is signed out. */
  update(fileId: string, change: { passcode_required?: boolean; new_passcode?: boolean }): Observable<ShareLink> {
    return this.http.patch<ShareLink>(this.fileUrl(fileId), change);
  }

  /** Stops sharing the file: its link stops working, and sharing again makes a new one. */
  stop(fileId: string): Observable<void> {
    return this.http.delete<void>(this.fileUrl(fileId));
  }

  /** Every link of the user's, newest first. */
  list(): Observable<ShareLink[]> {
    return this.http.get<{ links: ShareLink[] }>(`${this.apiUrl}/share-links`).pipe(map(r => r.links ?? []));
  }

  private fileUrl(fileId: string): string {
    return `${this.apiUrl}/share-links/files/${encodeURIComponent(fileId)}`;
  }
}

/** The share site's address as the build names it, or '' when it names none (or a placeholder left unset). */
export function shareSiteUrl(raw: string | undefined): string {
  const url = (raw ?? '').trim().replace(/\/+$/, '');
  return /^https?:\/\//.test(url) ? url : '';
}
