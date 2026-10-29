import { HttpClient } from '@angular/common/http';
import { Injectable, computed, inject, signal } from '@angular/core';
import { Observable, catchError, of, tap } from 'rxjs';

import { environment } from '../../../environments/environment';
import { ModelProvider, ProviderAccess, ProviderKeySummary } from '../models/provider-key.model';

/**
 * The account's provider keys, and the server's answer to where keys come
 * from. `access` is shared: the settings tab, the first-run prompt and error
 * links all read it, and it is refreshed whenever a key changes.
 *
 * Errors from set() and remove() are passed through as HttpErrorResponse so
 * callers can branch on the error code (reauthentication_required).
 */
@Injectable({ providedIn: 'root' })
export class ProviderKeysService {
  private readonly http = inject(HttpClient);
  private readonly baseUrl = environment.apiUrl;

  /** The last known access, or null before the first load. */
  readonly access = signal<ProviderAccess | null>(null);

  /** Keys come from accounts, so the key screen and first-run prompt apply. */
  readonly accountsSupplyKeys = computed(() => this.access()?.keys_from === 'account');

  /** The account has no key yet, in a deployment where it needs one. */
  readonly needsFirstKey = computed(() => {
    const access = this.access();
    return access?.keys_from === 'account' && access.providers.length === 0;
  });

  /**
   * Reloads access. A failure leaves the last answer in place: a network blip
   * should not flash the first-run prompt at someone who has keys.
   */
  refreshAccess(): Observable<ProviderAccess | null> {
    return this.http.get<ProviderAccess>(`${this.baseUrl}/provider-access`).pipe(
      tap(access => this.access.set(access)),
      catchError(() => of(this.access())),
    );
  }

  list(): Observable<ProviderKeySummary[]> {
    return this.http.get<ProviderKeySummary[]>(`${this.baseUrl}/provider-keys`);
  }

  /** Stores a key. currentPassword confirms it is the account's owner. */
  set(provider: ModelProvider, key: string, currentPassword: string): Observable<ProviderKeySummary> {
    return this.http.put<ProviderKeySummary>(`${this.baseUrl}/provider-keys/${provider}`, {
      key,
      current_password: currentPassword,
    });
  }

  remove(provider: ModelProvider): Observable<void> {
    return this.http.delete<void>(`${this.baseUrl}/provider-keys/${provider}`);
  }
}
