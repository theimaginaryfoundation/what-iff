import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import {
  CreateProviderConnection,
  ListedModel,
  ProviderConnected,
  ProviderConnection,
  ProviderPreset,
  UpdateProviderConnection,
} from '../models/provider-connection.model';

/**
 * The account's provider connections (ADR 0x027). Errors are passed through as
 * HttpErrorResponse so callers can branch on the error code: a failed connect
 * answers 422 with provider_unreachable, provider_key_rejected,
 * provider_incompatible or provider_unavailable.
 */
@Injectable({ providedIn: 'root' })
export class ProviderConnectionsService {
  private readonly http = inject(HttpClient);
  private readonly baseUrl = `${environment.apiUrl}/provider-connections`;

  list(): Observable<ProviderConnection[]> {
    return this.http.get<ProviderConnection[]>(this.baseUrl);
  }

  presets(): Observable<ProviderPreset[]> {
    return this.http.get<ProviderPreset[]>(`${this.baseUrl}/presets`);
  }

  /** Lists the provider's models with these details, and stores the connection only if that works. */
  create(body: CreateProviderConnection): Observable<ProviderConnected> {
    return this.http.post<ProviderConnected>(this.baseUrl, body);
  }

  update(id: string, body: UpdateProviderConnection): Observable<ProviderConnected> {
    return this.http.patch<ProviderConnected>(`${this.baseUrl}/${id}`, body);
  }

  remove(id: string): Observable<void> {
    return this.http.delete<void>(`${this.baseUrl}/${id}`);
  }

  models(id: string): Observable<ListedModel[]> {
    return this.http.get<ListedModel[]>(`${this.baseUrl}/${id}/models`);
  }
}
