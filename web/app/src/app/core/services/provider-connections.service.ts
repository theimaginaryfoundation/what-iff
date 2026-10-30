import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import {
  ConnectionModel,
  ConnectionModelChoice,
  CreateProviderConnection,
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

  /** The provider's live listing, with which models the account enabled. */
  models(id: string): Observable<ConnectionModel[]> {
    return this.http.get<ConnectionModel[]>(`${this.baseUrl}/${id}/models`);
  }

  /**
   * Enables and disables models, as one change; answers with the connection's
   * enabled models. Disabling the account's default model answers 409
   * default_model_in_use.
   */
  setModels(id: string, models: ConnectionModelChoice[]): Observable<ConnectionModel[]> {
    return this.http.put<ConnectionModel[]>(`${this.baseUrl}/${id}/models`, { models });
  }
}
