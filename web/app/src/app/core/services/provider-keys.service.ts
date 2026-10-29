import { HttpClient } from '@angular/common/http';
import { Injectable, computed, inject, signal } from '@angular/core';
import { Observable, catchError, of, tap } from 'rxjs';

import { environment } from '../../../environments/environment';
import { ProviderAccess } from '../models/provider-key.model';

/**
 * The server's answer to where the account's provider keys come from, and
 * which providers it can reach. `access` is shared: the Providers tab, the
 * first-run prompt and error links all read it, and it is refreshed whenever a
 * connection changes. Connections themselves are ProviderConnectionsService.
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
}
