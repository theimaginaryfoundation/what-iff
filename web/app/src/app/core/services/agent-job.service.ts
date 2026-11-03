import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Observable, throwError } from 'rxjs';
import { catchError } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { PaginatedResponse } from '../models/common.model';
import { apiErrorEnvelope, apiErrorMessage, hasErrorCode } from '../utils/api-error.helpers';
import {
  AgentJob,
  AgentJobFilters,
  AgentJobScheduleParseRequest,
  AgentJobSchedulePreview,
  CreateAgentJobRequest,
  UpdateAgentJobRequest,
  UpdateAgentJobStatusRequest
} from '../models/agent-job.model';

/**
 * The message for a failed "Run now". A run refused by the per-user job run limit
 * (429 `job_run_limit_reached`) names the reset time in the viewer's own clock,
 * read from `details.retry_at`; the server's message (which uses UTC) is the
 * fallback when it has no reset time. Every other failure uses the envelope's
 * message, as the other calls here do.
 */
export function runNowErrorMessage(error: unknown, locale?: string): string {
  if (hasErrorCode(error, 'job_run_limit_reached')) {
    const details = apiErrorEnvelope(error)?.details as { retry_at?: unknown } | null | undefined;
    const retryAt = typeof details?.retry_at === 'string' ? new Date(details.retry_at) : null;
    if (retryAt && !Number.isNaN(retryAt.getTime())) {
      const time = retryAt.toLocaleTimeString(locale ?? [], { hour: 'numeric', minute: '2-digit' });
      return `Job run limit reached — try again at ${time}.`;
    }
  }
  return apiErrorMessage(error, 'Failed to run job now');
}

@Injectable({
  providedIn: 'root'
})
export class AgentJobService {
  private http: HttpClient = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/agent-job`;

  listAgentJobs(page: number = 1, limit: number = 20, filters?: AgentJobFilters): Observable<PaginatedResponse<AgentJob>> {
    const params: any = { page: page.toString(), limit: limit.toString() };

    if (filters) {
      if (filters.status) params.status = filters.status;
      if (filters.schedule_type) params.schedule_type = filters.schedule_type;
      if (filters.search) params.search = filters.search;
    }

    return this.http.get<PaginatedResponse<AgentJob>>(this.apiUrl, { params })
      .pipe(catchError(this.handleError));
  }

  createAgentJob(request: CreateAgentJobRequest): Observable<AgentJob> {
    return this.http.post<AgentJob>(this.apiUrl, request)
      .pipe(catchError(this.handleError));
  }

  getAgentJob(id: string): Observable<AgentJob> {
    return this.http.get<AgentJob>(`${this.apiUrl}/${id}`)
      .pipe(catchError(this.handleError));
  }

  updateAgentJob(id: string, update: UpdateAgentJobRequest): Observable<AgentJob> {
    return this.http.put<AgentJob>(`${this.apiUrl}/${id}`, update)
      .pipe(catchError(this.handleError));
  }

  updateAgentJobStatus(id: string, update: UpdateAgentJobStatusRequest): Observable<AgentJob> {
    return this.http.put<AgentJob>(`${this.apiUrl}/${id}/status`, update)
      .pipe(catchError(this.handleError));
  }

  deleteAgentJob(id: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${id}`)
      .pipe(catchError(this.handleError));
  }

  parseSchedule(request: AgentJobScheduleParseRequest): Observable<AgentJobSchedulePreview> {
    return this.http.post<AgentJobSchedulePreview>(`${this.apiUrl}/schedule/parse`, request)
      .pipe(catchError(this.handleError));
  }

  runNow(id: string): Observable<{ status: string }> {
    return this.http.post<{ status: string }>(`${this.apiUrl}/${id}/run`, {})
      .pipe(catchError((error: HttpErrorResponse) => throwError(() => new Error(runNowErrorMessage(error)))));
  }

  addRitual(jobId: string, ritualId: string): Observable<void> {
    return this.http.post<void>(`${this.apiUrl}/${jobId}/rituals/${ritualId}`, {})
      .pipe(catchError(this.handleError));
  }

  removeRitual(jobId: string, ritualId: string): Observable<void> {
    return this.http.delete<void>(`${this.apiUrl}/${jobId}/rituals/${ritualId}`)
      .pipe(catchError(this.handleError));
  }

  private handleError(error: HttpErrorResponse): Observable<never> {
    const errorMessage = apiErrorMessage(error, 'An error occurred');

    console.error('AgentJob Service Error:', errorMessage);
    return throwError(() => new Error(errorMessage));
  }
}

