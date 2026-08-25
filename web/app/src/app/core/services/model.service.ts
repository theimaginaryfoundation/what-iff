import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, BehaviorSubject } from 'rxjs';
import { tap } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { Model } from '../models/model.model';

@Injectable({
  providedIn: 'root'
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
      })
    );
  }

  /**
   * Clear all cached model data
   * Should be called on logout to ensure fresh data on next login
   */
  clearCache(): void {
    this.modelsSubject.next([]);
  }
}

