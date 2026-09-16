import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import { ProviderUsage } from '../models/provider-usage.model';

/**
 * What each provider key is spent on, as reported by the server.
 *
 * Deliberately fetched rather than written into the page: the models come from
 * the same constants the calls use, so the list cannot drift from what the
 * server actually does the way a hand-maintained copy would.
 */
@Injectable({ providedIn: 'root' })
export class ProviderUsageService {
  private http = inject(HttpClient);

  list(): Observable<ProviderUsage[]> {
    return this.http.get<ProviderUsage[]>(`${environment.apiUrl}/provider-usage`);
  }
}
