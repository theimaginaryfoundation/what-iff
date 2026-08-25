import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { map } from 'rxjs/operators';
import { environment } from '@environments/environment';
import { Ritual, CreateRitualRequest, UpdateRitualRequest, RitualFilters, PaginatedRitualResponse } from '../models/ritual.model';

@Injectable({
  providedIn: 'root'
})
export class RitualService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/ritual`;

  listSystemRituals(): Observable<Ritual[]> {
    // Backend currently returns a plain array; tolerate both array and paginated shapes.
    return this.http
      .get<Ritual[] | PaginatedRitualResponse>(`${this.apiUrl}/system`)
      .pipe(map((r) => (Array.isArray(r) ? r : r?.results ?? [])));
  }

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

  assignSystemRitualHotkey(ritualId: string, hotkeys: string): Observable<any> {
    return this.http.put<any>(`${this.apiUrl}/${ritualId}/binding`, { hotkeys });
  }

  deleteSystemRitualHotkey(ritualId: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${ritualId}/binding`);
  }

  getSystemRitualBinding(ritualId: string): Observable<any> {
    return this.http.get<any>(`${this.apiUrl}/${ritualId}/binding`);
  }
}
