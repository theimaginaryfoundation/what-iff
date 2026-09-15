import { Injectable, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { Observable, catchError, throwError } from 'rxjs';

import { environment } from '@environments/environment';
import { Job } from '../models/job.model';

export const ACTIVE_ACCOUNT_IMPORT_JOB_STORAGE_KEY = 'whatiff.active-account-import-job';

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

/**
 * Narrows an account import to chosen items. Sent as a JSON `selection` form field; omitting it
 * imports everything. IDs are the source ids from the export ZIP (personality ids, conversation
 * uuids). Memories are all-or-nothing via includeMemories (an account can carry thousands).
 */
export interface AccountImportSelection {
  personality_ids: string[];
  conversation_ids: string[];
  include_memories: boolean;
}

/** One row of the user's import/export activity log. */
export interface AccountActivityEntry {
  occurred_at: string;
  category: string;
  action: string;
  message: string;
}

@Injectable({
  providedIn: 'root',
})
export class AccountExportService {
  private readonly http = inject(HttpClient);
  private readonly apiUrl = `${environment.apiUrl}/account/export`;
  /** The active server-side restore job, retained for this browser tab across route changes. */
  readonly activeImportJobId = signal<string | null>(this.readActiveImportJob());

  enqueue(): Observable<Job> {
    return this.http.post<Job>(this.apiUrl, {}).pipe(catchError(this.handleError));
  }

  getStatus(id: string): Observable<Job> {
    return this.http.get<Job>(`${this.apiUrl}/${id}`).pipe(catchError(this.handleError));
  }

  importAccount(file: File, selection?: AccountImportSelection): Observable<Job> {
    const form = new FormData();
    form.append('file', file);
    if (selection) {
      form.append('selection', JSON.stringify(selection));
    }
    return this.http.post<Job>(`${environment.apiUrl}/account/import`, form).pipe(catchError(this.handleError));
  }

  getImportStatus(id: string): Observable<Job> {
    return this.http.get<Job>(`${environment.apiUrl}/account/import/${id}`).pipe(catchError(this.handleError));
  }

  trackActiveImport(jobID: string): void {
    this.activeImportJobId.set(jobID);
    try {
      sessionStorage.setItem(ACTIVE_ACCOUNT_IMPORT_JOB_STORAGE_KEY, jobID);
    } catch {
      // Storage can be unavailable in privacy-restricted browsers; the live service state still survives route changes.
    }
  }

  clearActiveImport(): void {
    this.activeImportJobId.set(null);
    try {
      sessionStorage.removeItem(ACTIVE_ACCOUNT_IMPORT_JOB_STORAGE_KEY);
    } catch {
      // Nothing else to clean up when browser storage is unavailable.
    }
  }

  /** Recent import/export activity (audit log), newest first. */
  getActivity(): Observable<AccountActivityEntry[]> {
    return this.http.get<AccountActivityEntry[]>(`${environment.apiUrl}/account/activity`).pipe(catchError(this.handleError));
  }

  private handleError(error: any): Observable<never> {
    const message = error.error?.error || error.message || 'Unable to request an account export.';
    return throwError(() => new Error(message));
  }

  private readActiveImportJob(): string | null {
    try {
      return sessionStorage.getItem(ACTIVE_ACCOUNT_IMPORT_JOB_STORAGE_KEY);
    } catch {
      return null;
    }
  }
}
