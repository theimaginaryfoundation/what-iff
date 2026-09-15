import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import { ProviderKeyStatus } from '../models/provider-key.model';

/**
 * The signed-in account's model-provider API keys.
 *
 * Keys belong to accounts, so everything here is implicitly scoped to whoever
 * is signed in — there are no ids to pass. The key itself is write-only: it
 * goes up on save and never comes back, so the UI shows `key_hint` instead.
 */
@Injectable({ providedIn: 'root' })
export class ProviderKeyService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/provider-keys`;

  /** Every provider in the catalog, with this account's status for each. */
  listStatuses(): Observable<ProviderKeyStatus[]> {
    return this.http.get<ProviderKeyStatus[]>(this.apiUrl);
  }

  /** Stores or replaces this account's key for one provider. */
  setKey(provider: string, key: string): Observable<ProviderKeyStatus> {
    return this.http.put<ProviderKeyStatus>(`${this.apiUrl}/${provider}`, { key });
  }

  /**
   * Removes this account's key. The account may still reach the provider
   * through the deployment fallback, which the returned status reports.
   */
  removeKey(provider: string): Observable<ProviderKeyStatus> {
    return this.http.delete<ProviderKeyStatus>(`${this.apiUrl}/${provider}`);
  }
}
