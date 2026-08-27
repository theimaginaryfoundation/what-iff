import { Injectable, inject } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, catchError, throwError } from 'rxjs';

import { environment } from '@environments/environment';
import { Job } from '../models/job.model';

export interface AccountImportResult {
  conversations?: { imported: number; skipped: number; errors?: string[] };
  memories?: {
    imported_count: number;
    duplicate_count: number;
    invalid_record_count?: number;
    invalid_reasons?: {
      malformed_json?: number;
      missing_id?: number;
      empty_content?: number;
      missing_created_at?: number;
      missing_chat_id?: number;
    };
    skipped_missing_chat_count?: number;
    skipped_missing_personality_count?: number;
  };
  personalities?: { created: number; skipped: number };
  warnings?: string[];
}

export interface AccountImportProgress extends AccountImportResult {
  phase?: string;
  message?: string;
  counts?: Record<string, number>;
  result?: AccountImportResult;
}

@Injectable({
  providedIn: 'root',
})
export class AccountExportService {
  private readonly http = inject(HttpClient);
  private readonly apiUrl = `${environment.apiUrl}/account/export`;

  enqueue(): Observable<Job> {
    return this.http.post<Job>(this.apiUrl, {}).pipe(catchError(this.handleError));
  }

  getStatus(id: string): Observable<Job> {
    return this.http.get<Job>(`${this.apiUrl}/${id}`).pipe(catchError(this.handleError));
  }

  importAccount(file: File): Observable<Job> {
    const form = new FormData();
    form.append('file', file);
    return this.http.post<Job>(`${environment.apiUrl}/account/import`, form).pipe(catchError(this.handleError));
  }

  getImportStatus(id: string): Observable<Job> {
    return this.http.get<Job>(`${environment.apiUrl}/account/import/${id}`).pipe(catchError(this.handleError));
  }

  private handleError(error: any): Observable<never> {
    const message = error.error?.error || error.message || 'Unable to request an account export.';
    return throwError(() => new Error(message));
  }
}
