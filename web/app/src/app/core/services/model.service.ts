import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, BehaviorSubject } from 'rxjs';
import { tap } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { Model } from '../models/model.model';

@Injectable({
  providedIn: 'root',
})
export class ModelService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/model`;

  private modelsSubject = new BehaviorSubject<Model[]>([]);
  public models$ = this.modelsSubject.asObservable();

  getModels(): Observable<Model[]> {
    return this.http.get<Model[]>(this.apiUrl).pipe(
      tap(models => {
        this.modelsSubject.next(models);
      }),
    );
  }

  /**
   * Every model this account's keys unlock, including ones it has hidden.
   *
   * Deliberately does not publish to `models$`: that stream feeds the picker,
   * and pushing hidden models into it would put them straight back in the list
   * the user just removed them from.
   */
  getAllModelsIncludingHidden(): Observable<Model[]> {
    return this.http.get<Model[]>(`${this.apiUrl}?include_hidden=true`);
  }

  /**
   * Clear all cached model data
   * Should be called on logout to ensure fresh data on next login
   */
  clearCache(): void {
    this.modelsSubject.next([]);
  }
}
