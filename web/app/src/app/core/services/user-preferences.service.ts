import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, BehaviorSubject } from 'rxjs';
import { tap } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { UserPreferences } from '../models/user.model';

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

  updateUserPreferences(preferences: UserPreferences): Observable<UserPreferences> {
    return this.http.put<UserPreferences>(this.apiUrl, preferences).pipe(
      tap(updatedPreferences => {
        this.preferencesSubject.next(updatedPreferences);
      })
    );
  }

  /**
   * Clear all cached preference data
   * Should be called on logout to prevent showing stale data to different users
   */
  clearCache(): void {
    this.preferencesSubject.next(null);
  }
}
