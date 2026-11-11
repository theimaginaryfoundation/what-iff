import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, BehaviorSubject } from 'rxjs';
import { switchMap, tap } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { UserPreferences, UserPreferencesUpdateRequest } from '../models/user.model';

@Injectable({
  providedIn: 'root'
})
export class UserPreferencesService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/user/preferences`;

  private preferencesSubject = new BehaviorSubject<UserPreferences | null>(null);
  public preferences$ = this.preferencesSubject.asObservable();

  getUserPreferences(): Observable<UserPreferences> {
    return this.http.get<UserPreferences>(this.apiUrl).pipe(
      tap(preferences => {
        this.preferencesSubject.next(preferences);
      })
    );
  }

  updateUserPreferences(preferences: UserPreferencesUpdateRequest): Observable<UserPreferences> {
    return this.http.put<UserPreferences>(this.apiUrl, preferences).pipe(
      tap(updatedPreferences => {
        this.preferencesSubject.next(updatedPreferences);
      })
    );
  }

  /**
   * Change some preferences. PUT replaces the whole record, so this merges the patch onto a fresh
   * read rather than a copy loaded earlier, which could undo a change made elsewhere since.
   * Two writes racing between that read and the PUT are still last-writer-wins (the API has no
   * PATCH or version check). Errors reach the caller.
   */
  patchUserPreferences(patch: Partial<UserPreferencesUpdateRequest>): Observable<UserPreferences> {
    return this.getUserPreferences().pipe(switchMap(current => this.updateUserPreferences({ ...current, ...patch })));
  }

  /**
   * Clear all cached preference data
   * Should be called on logout to prevent showing stale data to different users
   */
  clearCache(): void {
    this.preferencesSubject.next(null);
  }
}
