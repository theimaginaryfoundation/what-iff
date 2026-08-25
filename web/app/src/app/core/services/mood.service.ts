import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable } from 'rxjs';
import { environment } from '../../../environments/environment';
import { Mood, CreateMoodRequest, UpdateMoodRequest, AttachMoodToPersonalitiesRequest } from '../models/mood.model';
import { PaginatedResponse } from '../models/common.model';

@Injectable({
  providedIn: 'root'
})
export class MoodService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/mood`;

  listMoods(page = 1, limit = 20, name?: string): Observable<PaginatedResponse<Mood>> {
    let params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());
    if (name) params = params.set('name', name);
    return this.http.get<PaginatedResponse<Mood>>(this.apiUrl, { params });
  }

  getMood(id: string): Observable<Mood> {
    return this.http.get<Mood>(`${this.apiUrl}/${id}`);
  }

  createMood(req: CreateMoodRequest): Observable<Mood> {
    return this.http.post<Mood>(this.apiUrl, req);
  }

  updateMood(id: string, req: UpdateMoodRequest): Observable<Mood> {
    return this.http.put<Mood>(`${this.apiUrl}/${id}`, req);
  }

  deleteMood(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`);
  }

  /** Replace all personality associations for a mood. */
  attachToPersonalities(id: string, req: AttachMoodToPersonalitiesRequest): Observable<void> {
    return this.http.post<void>(`${this.apiUrl}/${id}/personalities`, req);
  }
}
