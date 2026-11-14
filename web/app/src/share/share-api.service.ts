import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { firstValueFrom } from 'rxjs';

/** The share site reaches the API on its own origin (ADR 0x051 §5), so its unlock cookie is first-party. */
export const SHARE_API = '/api/shared';

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
    try {
      return { state: 'open', view: await firstValueFrom(this.http.get<SharedView>(`${SHARE_API}/${key}`)) };
    } catch (e) {
      const status = (e as HttpErrorResponse).status;
      return status === 401 ? { state: 'locked' } : status === 404 ? { state: 'missing' } : { state: 'error' };
    }
  }

  async unlock(key: string, passcode: string): Promise<UnlockResult> {
    try {
      await firstValueFrom(this.http.post(`${SHARE_API}/${key}/unlock`, { passcode }));
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
    return firstValueFrom(this.http.get(`${SHARE_API}/${key}/content`, { responseType: 'blob' }));
  }

  /** A web app's files. */
  async files(key: string): Promise<SharedFile[]> {
    const r = await firstValueFrom(this.http.get<{ files: SharedFile[] }>(`${SHARE_API}/${key}/files`));
    return r.files ?? [];
  }

  /** One of a web app's files, by its path in the project. */
  file(key: string, path: string): Promise<Blob> {
    const encoded = path.split('/').map(encodeURIComponent).join('/');
    return firstValueFrom(this.http.get(`${SHARE_API}/${key}/files/${encoded}`, { responseType: 'blob' }));
  }
}
