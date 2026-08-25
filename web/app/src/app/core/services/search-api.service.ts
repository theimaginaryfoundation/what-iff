import { HttpClient, HttpErrorResponse, HttpParams } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, throwError } from 'rxjs';
import { catchError } from 'rxjs/operators';

import { environment } from '../../../environments/environment';
import { apiErrorMessage } from '../utils/api-error.helpers';
import {
  SearchOptions,
  SearchResponse,
  SearchSectionType,
} from '../models/search.model';

/**
 * Thin wrapper over `GET /search` (Gap 06). The handler returns the canonical
 * five sections with empty `results` for non-matching types; this service
 * surfaces that contract verbatim and lets `CommandPaletteService` decide what
 * to render.
 */
@Injectable({ providedIn: 'root' })
export class SearchApiService {
  private readonly http = inject(HttpClient);
  private readonly endpoint = `${environment.apiUrl}/search`;

  search(query: string, options: SearchOptions = {}): Observable<SearchResponse> {
    let params = new HttpParams().set('query', query);
    if (options.types && options.types.length > 0) {
      params = params.set('types', joinTypes(options.types));
    }
    if (options.limitPerType !== undefined) {
      params = params.set('limit_per_type', String(options.limitPerType));
    }
    return this.http
      .get<SearchResponse>(this.endpoint, { params })
      .pipe(catchError(this.handleError));
  }

  private handleError(error: HttpErrorResponse): Observable<never> {
    const message = apiErrorMessage(error, 'Search request failed');
    console.error('SearchApiService error:', message);
    return throwError(() => new Error(message));
  }
}

function joinTypes(types: ReadonlyArray<SearchSectionType>): string {
  return Array.from(new Set(types)).join(',');
}
