import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable, catchError, throwError } from 'rxjs';
import { environment } from '@environments/environment';

import { apiErrorMessage } from '../utils/api-error.helpers';
import { AgentEntity, AgentEntityFilters, PaginatedAgentEntityResponse } from '../models/agent-entity.model';

/** The entity wiki under /memory/entities (ADR 0x034): agents write it, the user reviews it here. */
@Injectable({ providedIn: 'root' })
export class AgentEntityService {
  private readonly http = inject(HttpClient);
  private readonly apiUrl = `${environment.apiUrl}/memory/entities`;

  list(page: number, limit: number, filters: AgentEntityFilters = {}): Observable<PaginatedAgentEntityResponse> {
    let params = new HttpParams().set('page', String(page)).set('limit', String(limit));
    if (filters.query?.trim()) params = params.set('query', filters.query.trim());
    if (filters.global_only) {
      params = params.set('global_only', 'true');
    } else if (filters.personality_id) {
      params = params.set('personality_id', filters.personality_id);
    }
    return this.http.get<PaginatedAgentEntityResponse>(this.apiUrl, { params }).pipe(catchError(handleError));
  }

  updateBody(id: string, body: string): Observable<AgentEntity> {
    return this.http.patch<AgentEntity>(`${this.apiUrl}/${id}`, { body }).pipe(catchError(handleError));
  }

  delete(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`).pipe(catchError(handleError));
  }
}

function handleError(error: unknown): Observable<never> {
  return throwError(() => new Error(apiErrorMessage(error, 'An unexpected error occurred')));
}
