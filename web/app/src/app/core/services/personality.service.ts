import { Injectable, inject, signal } from '@angular/core';
import { HttpClient, HttpParams, HttpResponse } from '@angular/common/http';
import { Observable, of, tap, map } from 'rxjs';
import { environment } from '@environments/environment';
import {
  Personality,
  PersonalityPromptChange,
  PromptDefaults,
  PersonalityFilters,
  CreatePersonalityRequest,
  UpdatePersonalityRequest,
  PaginatedPersonalityResponse,
  PersonalityExpression,
  PersonalityCardImportResult,
  UpdatePersonalityExpressionRequest
} from '../models/personality.model';

@Injectable({
  providedIn: 'root'
})
export class PersonalityService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/personality`;

  personalities = signal<Personality[]>([]);
  private promptDefaultsSignal = signal<PromptDefaults | null>(null);

  listPersonalities(page: number = 1, limit: number = 10, filters?: PersonalityFilters): Observable<PaginatedPersonalityResponse> {
    let params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());

    if (filters?.name) params = params.set('name', filters.name);
    if (filters?.query) params = params.set('query', filters.query);
    if (filters?.min_date) params = params.set('min_date', filters.min_date);
    if (filters?.max_date) params = params.set('max_date', filters.max_date);
    if (filters?.personality_ids?.length) params = params.set('personality_ids', filters.personality_ids.join(','));

    return this.http.get<PaginatedPersonalityResponse>(this.apiUrl, { params }).pipe(
      tap(response => this.personalities.set(response.results))
    );
  }

  getPromptDefaults(): Observable<PromptDefaults> {
    const cached = this.promptDefaultsSignal();
    if (cached) return of(cached);
    return this.http.get<PromptDefaults>(`${this.apiUrl}/prompt-defaults`).pipe(
      tap(defaults => this.promptDefaultsSignal.set(defaults))
    );
  }

  promptDefaults(): PromptDefaults | null {
    return this.promptDefaultsSignal();
  }

  getPersonality(id: string): Observable<Personality> {
    return this.http.get<Personality>(`${this.apiUrl}/${id}`);
  }

  listPromptChanges(personalityId: string): Observable<PersonalityPromptChange[]> {
    return this.http.get<PersonalityPromptChange[]>(`${this.apiUrl}/${personalityId}/prompt-changes`);
  }

  revertPromptChange(personalityId: string, changeId: string): Observable<PersonalityPromptChange> {
    return this.http.post<PersonalityPromptChange>(`${this.apiUrl}/${personalityId}/prompt-changes/${changeId}/revert`, {});
  }

  listExpressions(personalityId: string): Observable<PersonalityExpression[]> {
    return this.http.get<PersonalityExpression[]>(`${this.apiUrl}/${personalityId}/expressions`);
  }

  upsertExpression(personalityId: string, expressionKey: string, request: UpdatePersonalityExpressionRequest): Observable<PersonalityExpression> {
    return this.http.put<PersonalityExpression>(`${this.apiUrl}/${personalityId}/expressions/${expressionKey}`, request);
  }

  deleteExpression(personalityId: string, expressionKey: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${personalityId}/expressions/${expressionKey}`);
  }

  generateDefaultExpressionGrid(personalityId: string, opts?: { force?: boolean }): Observable<PersonalityExpression[]> {
    let params = new HttpParams();
    if (opts?.force) params = params.set('force', 'true');
    return this.http.post<PersonalityExpression[]>(`${this.apiUrl}/${personalityId}/expressions/generate-default-grid`, {}, { params, observe: 'response' }).pipe(
      map(res => {
        if (res.status === 202) throw new Error('Expected synchronous expression list; job was enqueued instead');
        return res.body ?? [];
      }),
    );
  }

  createPersonality(personality: CreatePersonalityRequest): Observable<Personality> {
    return this.http.post<Personality>(this.apiUrl, personality).pipe(
      tap(() => this.listPersonalities().subscribe())
    );
  }

  /** Creates a personality from a SillyTavern character card: its JSON text, or the bytes of a PNG card. */
  importSillyTavernCard(card: string | ArrayBuffer, contentType: 'application/json' | 'image/png' = 'application/json'): Observable<PersonalityCardImportResult> {
    return this.http
      .post<PersonalityCardImportResult>(`${this.apiUrl}/import/sillytavern`, card, {
        headers: { 'Content-Type': contentType },
      })
      .pipe(tap(() => this.listPersonalities().subscribe()));
  }

  /** Downloads the personality as a SillyTavern card: a chara_card_v2 JSON document, or the cover image with the card embedded. */
  exportSillyTavernCard(id: string, format: 'json' | 'png' = 'json'): Observable<HttpResponse<Blob>> {
    return this.http.get(`${this.apiUrl}/${id}/export/sillytavern`, {
      params: format === 'png' ? new HttpParams().set('format', 'png') : undefined,
      responseType: 'blob',
      observe: 'response',
    });
  }

  updatePersonality(id: string, personality: UpdatePersonalityRequest): Observable<Personality> {
    return this.http.put<Personality>(`${this.apiUrl}/${id}`, personality).pipe(
      tap(() => this.listPersonalities().subscribe())
    );
  }

  deletePersonality(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`).pipe(
      tap(() => this.listPersonalities().subscribe())
    );
  }
}
