import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpParams } from '@angular/common/http';
import { Observable, catchError, throwError } from 'rxjs';
import { environment } from '@environments/environment';
import { apiErrorMessage } from '../utils/api-error.helpers';
import {
  BatchCreateMemoryInput,
  BatchCreateMemoryResponse,
  CreateMemoryInput,
  Memory,
  MemoryFilters,
  MemoryImportResult,
  CheckpointSnapshot,
  CompactionEvent,
  MemoryMergeEvent,
  MemoryPatch,
  PaginatedCompactionEventResponse,
  PaginatedMemoryMergeEventResponse,
  PaginatedMemoryResponse,
} from '../models/memory.model';

@Injectable({
  providedIn: 'root'
})
export class MemoryService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/memory`;

  /**
   * Get paginated list of memories with optional filters
   */
  getMemories(page: number = 1, limit: number = 10, filters?: MemoryFilters): Observable<PaginatedMemoryResponse> {
    let params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());

    if (filters?.chat_id) {
      params = params.set('chat_id', filters.chat_id);
    }
    if (filters?.level) {
      params = params.set('level', filters.level);
    }
    if (filters?.type) {
      params = params.set('type', filters.type);
    }
    if (filters?.starred !== undefined) {
      params = params.set('starred', String(filters.starred));
    }
    if (filters?.pinned_personality_id) {
      params = params.set('pinned_personality_id', filters.pinned_personality_id);
    }
    if (filters?.pinned_personality_ids?.length) {
      for (const personalityId of filters.pinned_personality_ids) {
        if (personalityId?.trim()) {
          params = params.append('personality_ids', personalityId.trim());
        }
      }
    }
    if (filters?.global_only) {
      params = params.set('global_only', 'true');
    }
    if (filters?.query) {
      params = params.set('query', filters.query);
    }
    if (filters?.status) {
      params = params.set('status', filters.status);
    }
    if (filters?.sort) {
      params = params.set('sort', filters.sort);
    }
    if (filters?.min_date) {
      params = params.set('min_date', filters.min_date);
    }
    if (filters?.max_date) {
      params = params.set('max_date', filters.max_date);
    }

    return this.http.get<PaginatedMemoryResponse>(this.apiUrl, { params })
      .pipe(
        catchError(this.handleError)
      );
  }

  /**
   * Get a specific memory by ID
   */
  getMemoryById(id: string): Observable<Memory> {
    return this.http.get<Memory>(`${this.apiUrl}/${id}`)
      .pipe(
        catchError(this.handleError)
      );
  }

  createMemory(payload: CreateMemoryInput): Observable<Memory> {
    return this.http.post<Memory>(this.apiUrl, payload)
      .pipe(catchError(this.handleError));
  }

  createMemoriesBatch(payload: BatchCreateMemoryInput): Observable<BatchCreateMemoryResponse> {
    return this.http.post<BatchCreateMemoryResponse>(`${this.apiUrl}/batch`, payload)
      .pipe(catchError(this.handleError));
  }

  patchMemory(id: string, payload: MemoryPatch): Observable<Memory> {
    return this.http.patch<Memory>(`${this.apiUrl}/${id}`, payload)
      .pipe(catchError(this.handleError));
  }

  /**
   * Delete a memory by ID
   */
  deleteMemory(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`)
      .pipe(
        catchError(this.handleError)
      );
  }

//Update the pinned personality for a memory
  updateMemoryPin(id: string, pinnedPersonalityId: string | null): Observable<Memory> {
    return this.http.put<Memory>(`${this.apiUrl}/${id}/pin`, {
      pinned_personality_id: pinnedPersonalityId
    }).pipe(
      catchError(this.handleError)
    );
  }

  /**
   * Export all memories as a ZIP archive download.
   * Returns the raw blob; the caller is responsible for triggering the download.
   */
  exportMemories(): Observable<Blob> {
    return this.http.get(`${environment.apiUrl}/memory/export`, {
      responseType: 'blob'
    }).pipe(
      catchError(this.handleError)
    );
  }

  /**
   * Import memories from an exported ZIP archive.
   */
  importMemories(file: File): Observable<MemoryImportResult> {
    const formData = new FormData();
    formData.append('file', file);
    return this.http.post<MemoryImportResult>(`${environment.apiUrl}/memory/import`, formData).pipe(
      catchError(this.handleError)
    );
  }

  listMergeEvents(page: number = 1, limit: number = 20): Observable<PaginatedMemoryMergeEventResponse> {
    const params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());
    return this.http.get<PaginatedMemoryMergeEventResponse>(`${this.apiUrl}/merge-events`, { params })
      .pipe(catchError(this.handleError));
  }

  undoMergeEvent(eventId: string): Observable<MemoryMergeEvent> {
    return this.http.post<MemoryMergeEvent>(`${this.apiUrl}/merge-events/${eventId}/undo`, {})
      .pipe(catchError(this.handleError));
  }

  listCompactionEvents(
    page: number = 1,
    limit: number = 20,
    filters?: { chat_id?: string; personality_id?: string },
  ): Observable<PaginatedCompactionEventResponse> {
    let params = new HttpParams()
      .set('page', page.toString())
      .set('limit', limit.toString());
    if (filters?.chat_id) {
      params = params.set('chat_id', filters.chat_id);
    }
    if (filters?.personality_id) {
      params = params.set('personality_id', filters.personality_id);
    }
    return this.http.get<PaginatedCompactionEventResponse>(`${this.apiUrl}/compaction-events`, { params })
      .pipe(catchError(this.handleError));
  }

  getCompactionEvent(eventId: string): Observable<CompactionEvent> {
    return this.http.get<CompactionEvent>(`${this.apiUrl}/compaction-events/${eventId}`)
      .pipe(catchError(this.handleError));
  }

  /** Revert a summary or scratchpad snapshot to its live source (Chat.summary / Personality.scratchpad). */
  revertSnapshot(snapshotId: string): Observable<CheckpointSnapshot> {
    return this.http.post<CheckpointSnapshot>(`${this.apiUrl}/snapshots/${snapshotId}/revert`, {})
      .pipe(catchError(this.handleError));
  }

  /**
   * Handle HTTP errors
   */
  private handleError(error: unknown): Observable<never> {
    const errorMessage = apiErrorMessage(error, 'An unexpected error occurred');

    console.error('Memory Service Error:', error);
    return throwError(() => new Error(errorMessage));
  }
}
