import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import { UtilityModels, UtilityRole } from '../models/utility-model.model';

/**
 * The models the account's background work runs on, one per utility role. Each
 * write answers with every role after it.
 */
@Injectable({ providedIn: 'root' })
export class UtilityModelsService {
  private readonly http = inject(HttpClient);
  private readonly baseUrl = `${environment.apiUrl}/utility-models`;

  get(): Observable<UtilityModels> {
    return this.http.get<UtilityModels>(this.baseUrl);
  }

  /** Chooses the account's model for a role. */
  choose(role: UtilityRole, modelId: string): Observable<UtilityModels> {
    return this.http.put<UtilityModels>(`${this.baseUrl}/${role}`, { model_id: modelId });
  }

  /** Clears the account's choice, so the role runs on its default. */
  clear(role: UtilityRole): Observable<UtilityModels> {
    return this.http.delete<UtilityModels>(`${this.baseUrl}/${role}`);
  }
}
