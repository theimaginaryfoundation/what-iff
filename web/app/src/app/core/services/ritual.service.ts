import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams, HttpResponse } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '@environments/environment';
import {
  Ritual,
  CreateRitualRequest,
  UpdateRitualRequest,
  RitualFilters,
  PaginatedRitualResponse,
  RitualImportResult,
  LoadedSkill,
} from '../models/ritual.model';

@Injectable({
  providedIn: 'root'
})
export class RitualService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/ritual`;

  listRituals(page: number = 1, limit: number = 10, filters: RitualFilters = {}): Observable<PaginatedRitualResponse> {
    let params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());

    if (filters.name) {
      params = params.set('name', filters.name);
    }
    if (filters.search) {
      params = params.set('search', filters.search);
    }
    if (filters.personality_id) {
      params = params.set('personality_id', filters.personality_id);
    }
    if (filters.personality_ids?.length) {
      filters.personality_ids.forEach(id => {
        params = params.append('personality_ids', id);
      });
    }
    if (filters.global_only !== undefined) {
      params = params.set('global_only', filters.global_only.toString());
    }
    if (filters.has_hotkeys !== undefined) {
      params = params.set('has_hotkeys', filters.has_hotkeys.toString());
    }
    if (filters.sort) {
      params = params.set('sort', filters.sort);
    }
    if (filters.min_date) {
      params = params.set('min_date', filters.min_date);
    }
    if (filters.max_date) {
      params = params.set('max_date', filters.max_date);
    }

    return this.http.get<PaginatedRitualResponse>(this.apiUrl, { params });
  }

  getRitual(id: string): Observable<Ritual> {
    return this.http.get<Ritual>(`${this.apiUrl}/${id}`);
  }

  createRitual(ritual: CreateRitualRequest): Observable<Ritual> {
    return this.http.post<Ritual>(this.apiUrl, ritual);
  }

  updateRitual(id: string, ritual: UpdateRitualRequest): Observable<Ritual> {
    return this.http.put<Ritual>(`${this.apiUrl}/${id}`, ritual);
  }

  deleteRitual(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`);
  }

  /**
   * Imports a skill from an Agent Skills bundle: a zip with SKILL.md at its root or in one top-level
   * folder, or a lone SKILL.md. A taken slug gets a numeric suffix; the result's warnings say so.
   */
  importBundle(file: File, personalityId?: string | null): Observable<RitualImportResult> {
    const form = new FormData();
    form.append('file', file, file.name);
    if (personalityId) {
      form.append('personality_id', personalityId);
    }
    return this.http.post<RitualImportResult>(`${this.apiUrl}/import`, form);
  }

  /** The skills the agent loaded in a thread with load_skill, oldest load first. */
  listLoadedSkills(chatId: string): Observable<LoadedSkill[]> {
    return this.http.get<LoadedSkill[]>(`${environment.apiUrl}/chat/${chatId}/loaded-skills`);
  }

  /** Forgets a skill the agent loaded in a thread; its instructions leave the context from the next turn. */
  unloadSkill(chatId: string, ritualId: string): Observable<void> {
    return this.http.delete<void>(`${environment.apiUrl}/chat/${chatId}/loaded-skills/${ritualId}`);
  }

  /** The skill as an Agent Skills zip; the Content-Disposition header carries `<slug>.zip`. */
  exportRitual(id: string): Observable<HttpResponse<Blob>> {
    return this.http.get(`${this.apiUrl}/${id}/export`, { responseType: 'blob', observe: 'response' });
  }

  getAvailableRituals(chatId: string, page: number = 1, limit: number = 10, filters: RitualFilters = {}): Observable<PaginatedRitualResponse> {
    let params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());

    if (filters.name) {
      params = params.set('name', filters.name);
    }
    if (filters.search) {
      params = params.set('search', filters.search);
    }
    if (filters.has_hotkeys !== undefined) {
      params = params.set('has_hotkeys', filters.has_hotkeys.toString());
    }
    if (filters.sort) {
      params = params.set('sort', filters.sort);
    }
    if (filters.min_date) {
      params = params.set('min_date', filters.min_date);
    }
    if (filters.max_date) {
      params = params.set('max_date', filters.max_date);
    }

    return this.http.get<PaginatedRitualResponse>(`${environment.apiUrl}/chat/${chatId}/available-rituals`, { params });
  }
}
