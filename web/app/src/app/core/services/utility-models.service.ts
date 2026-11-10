import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable } from 'rxjs';

import { environment } from '../../../environments/environment';
import { UtilityModelChoice, UtilityModels, UtilityRole } from '../models/utility-model.model';

/**
 * The models the account's background work runs on, one per utility role. Each
 * write answers with every role after it. A choice is replaced, never cleared:
 * where accounts choose, nothing stands behind it.
 */
@Injectable({ providedIn: 'root' })
export class UtilityModelsService {
  private readonly http = inject(HttpClient);
  private readonly baseUrl = `${environment.apiUrl}/utility-models`;

  get(): Observable<UtilityModels> {
    return this.http.get<UtilityModels>(this.baseUrl);
  }

  /** Chooses the account's model for a role, with how it's asked to work. */
  choose(role: UtilityRole, choice: UtilityModelChoice): Observable<UtilityModels> {
    return this.http.put<UtilityModels>(`${this.baseUrl}/${role}`, choice);
  }

  /** Switches a role off: its work stops until a model is chosen again. */
  switchOff(role: UtilityRole): Observable<UtilityModels> {
    return this.http.put<UtilityModels>(`${this.baseUrl}/${role}`, { off: true });
  }
}
