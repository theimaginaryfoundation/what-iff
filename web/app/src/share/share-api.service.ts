import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';

/** The share site reaches the API on its own origin (ADR 0x051 §5), so its unlock cookie is first-party. */
export const SHARE_API = '/api/shared';

/** What a share's key looks like (22 base64url characters); anything else names no share. */
const KEY = /^[A-Za-z0-9_-]{22}$/;

/** Whether a key can be a share's: one that cannot is not asked about. */
export function isShareKey(key: string): boolean {
  return KEY.test(key);
}

/** A share's API path; the key is checked first, so it can only ever name a share. */
function shareUrl(key: string): string {
  if (!isShareKey(key)) throw new Error('Not a share key');
  return `${SHARE_API}/${key}`;
}

/** A file of a shared web app, for a link the browser downloads (the share's cookie goes with it). */
export function sharedFileUrl(key: string, path: string): string {
  return `${shareUrl(key)}/files/${path.split('/').map(encodeURIComponent).join('/')}`;
}

/** A shared file's bytes, for a link the browser downloads. */
export function sharedContentUrl(key: string): string {
  return `${shareUrl(key)}/content`;
}

/** What a share is, once it is open to the visitor. */
export interface SharedView {
  name: string;
  file_type: string;
  size_bytes?: number;
  content_version: number;
  web_app: boolean;
  /** A web app's page, by its path in the project. */
  page?: string;
  project_name?: string;
}

/** One file of a shared web app, by its path in the project. */
export interface SharedFile {
  path: string;
  file_type: string;
  size_bytes?: number;
  content_version: number;
}

/** What opening a share found. */
export type OpenResult = { state: 'open'; view: SharedView } | { state: 'locked' } | { state: 'missing' } | { state: 'error' };

/** What a passcode did. */
export type UnlockResult =
  | { state: 'unlocked' }
  | { state: 'wrong'; attemptsLeft: number | null }
  | { state: 'banned'; retryAfterSeconds: number }
  | { state: 'missing' }
  | { state: 'error' };

/** The public share API (ADR 0x051 §5): the share, its passcode, and its files' bytes. */
@Injectable({ providedIn: 'root' })
export class ShareApiService {
  private readonly http = inject(HttpClient);

  async open(key: string): Promise<OpenResult> {
    if (!isShareKey(key)) return { state: 'missing' };
    try {
      return { state: 'open', view: await firstValueFrom(this.http.get<SharedView>(shareUrl(key))) };
    } catch (e) {
      const status = (e as HttpErrorResponse).status;
      return status === 401 ? { state: 'locked' } : status === 404 ? { state: 'missing' } : { state: 'error' };
    }
  }

  async unlock(key: string, passcode: string): Promise<UnlockResult> {
    if (!isShareKey(key)) return { state: 'missing' };
    try {
      await firstValueFrom(this.http.post(`${shareUrl(key)}/unlock`, { passcode }));
      return { state: 'unlocked' };
    } catch (e) {
      const error = e as HttpErrorResponse;
      const body = (error.error ?? {}) as { attempts_left?: number; retry_after?: number };
      if (error.status === 401) return { state: 'wrong', attemptsLeft: typeof body.attempts_left === 'number' ? body.attempts_left : null };
      if (error.status === 429) {
        const header = Number(error.headers?.get('Retry-After'));
        return { state: 'banned', retryAfterSeconds: body.retry_after ?? (Number.isFinite(header) ? header : 60) };
      }
      return error.status === 404 ? { state: 'missing' } : { state: 'error' };
    }
  }

  /** A single file's bytes (a web app's page). */
  content(key: string): Promise<Blob> {
    return firstValueFrom(this.http.get(sharedContentUrl(key), { responseType: 'blob' }));
  }

  /** A web app's files. */
  async files(key: string): Promise<SharedFile[]> {
    const r = await firstValueFrom(this.http.get<{ files: SharedFile[] }>(`${shareUrl(key)}/files`));
    return r.files ?? [];
  }

  /** One of a web app's files, by its path in the project. */
  file(key: string, path: string): Promise<Blob> {
    return firstValueFrom(this.http.get(sharedFileUrl(key, path), { responseType: 'blob' }));
  }
}
