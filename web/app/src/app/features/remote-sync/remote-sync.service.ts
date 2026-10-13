import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, of, shareReplay } from 'rxjs';

import { environment } from '../../../environments/environment';

/** Remote sync (ADR 0x026). Shapes mirror internal/models/remote_sync.go. */

export type RemoteSyncStatus = 'pending' | 'ok' | 'error' | 'needs_attention';

export interface RemoteSyncRunStats {
  threads_checked: number;
  threads_created: number;
  threads_adopted: number;
  threads_updated: number;
  threads_forked: number;
  threads_failed: number;
  threads_deferred: number;
  messages_added: number;
}

export interface RemoteSyncTotals {
  threads: number;
  messages: number;
  forks: number;
}

export interface RemoteSyncSettings {
  configured: boolean;
  enabled?: boolean;
  base_url?: string;
  /** The stored token's last four characters. The token itself is never returned. */
  token_hint?: string;
  status?: RemoteSyncStatus;
  last_error?: string | null;
  last_run_at?: string | null;
  last_success_at?: string | null;
  last_run?: RemoteSyncRunStats;
  running: boolean;
  totals?: RemoteSyncTotals;
  interval_seconds?: number;
  default_base_url?: string;
  allow_local_network?: boolean;
}

export interface RemoteSyncSaveRequest {
  base_url: string;
  /** Required the first time; omit to keep the stored token. */
  token?: string;
  enabled?: boolean;
}

/** "3 new threads, 12 messages, 1 fork" for a run, or '' when it changed nothing. */
export function runSummary(r: RemoteSyncRunStats | undefined | null): string {
  if (!r) return '';
  const parts: string[] = [];
  const plural = (n: number, one: string, many: string) => `${n.toLocaleString()} ${n === 1 ? one : many}`;
  const added = r.threads_created + r.threads_adopted;
  if (added) parts.push(plural(added, 'new thread', 'new threads'));
  if (r.threads_updated) parts.push(plural(r.threads_updated, 'updated thread', 'updated threads'));
  if (r.threads_forked) parts.push(plural(r.threads_forked, 'fork', 'forks'));
  if (r.messages_added) parts.push(plural(r.messages_added, 'message', 'messages'));
  if (r.threads_deferred) parts.push(`${r.threads_deferred.toLocaleString()} more next time`);
  return parts.join(', ');
}

/** API client for /api/remote-sync (internal/remotesyncplugin). */
@Injectable({ providedIn: 'root' })
export class RemoteSyncService {
  private readonly http = inject(HttpClient);
  private readonly base = `${environment.apiUrl}/remote-sync`;
  private availability$: Observable<boolean> | null = null;

  /**
   * Whether the server runs remote sync. It is off unless the operator sets
   * REMOTE_SYNC_ENABLED=true, and then /api/remote-sync is not routed: a 404 here
   * means off. Any other outcome means on. Asked once per session.
   */
  available(): Observable<boolean> {
    if (!this.availability$) {
      this.availability$ = this.http.get<unknown>(this.base).pipe(
        map(() => true),
        catchError((e: unknown) => of(!(e instanceof HttpErrorResponse && e.status === 404))),
        shareReplay({ bufferSize: 1, refCount: false }),
      );
    }
    return this.availability$;
  }

  get(): Observable<RemoteSyncSettings> {
    return this.http.get<RemoteSyncSettings>(this.base);
  }

  save(body: RemoteSyncSaveRequest): Observable<RemoteSyncSettings> {
    return this.http.put<RemoteSyncSettings>(this.base, body);
  }

  remove(): Observable<void> {
    return this.http.delete<void>(this.base);
  }

  runNow(): Observable<RemoteSyncSettings> {
    return this.http.post<RemoteSyncSettings>(`${this.base}/run`, {});
  }
}
