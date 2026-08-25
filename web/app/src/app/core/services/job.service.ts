import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpResponse } from '@angular/common/http';
import { Observable, BehaviorSubject, throwError, timer, switchMap, takeWhile, finalize, distinctUntilChanged } from 'rxjs';
import { catchError, tap, map } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { Job, JobFilters, JobStatus, ActiveChatMessageJob } from '../models/job.model';
import { ChatMessage } from '../models/message.model';
import { MessageService } from './message.service';
import { PaginatedResponse } from '../models/common.model';
import { apiErrorMessage } from '../utils/api-error.helpers';

@Injectable({
  providedIn: 'root'
})
export class JobService {
  private http: HttpClient = inject(HttpClient);
  private messageService: MessageService = inject(MessageService);
  private apiUrl = `${environment.apiUrl}/job`;
  private chatApiUrl = `${environment.apiUrl}/chat`;

  // Active polling jobs
  private activeJobsSubject = new BehaviorSubject<Set<string>>(new Set());
  public activeJobs$ = this.activeJobsSubject.asObservable();

  listJobs(page: number = 1, limit: number = 20, filters?: JobFilters): Observable<PaginatedResponse<Job>> {
    let params: any = { page: page.toString(), limit: limit.toString() };

    if (filters) {
      if (filters.job_type) params.job_type = filters.job_type;
      if (filters.reference) params.reference = filters.reference;
      if (filters.status) params.status = filters.status;
    }

    return this.http.get<PaginatedResponse<Job>>(this.apiUrl, { params })
      .pipe(catchError(this.handleError));
  }

  getJob(jobId: string): Observable<Job> {
    return this.http.get<Job>(`${this.apiUrl}/${jobId}`)
      .pipe(catchError(this.handleError));
  }

  updateJobStatus(jobId: string, status: JobStatus, error?: string): Observable<Job> {
    const updateData: any = { status };
    if (error) {
      updateData.error = error;
    }

    return this.http.put<Job>(`${this.apiUrl}/${jobId}/status`, updateData)
      .pipe(catchError(this.handleError));
  }

  setJobResult(jobId: string, resultId: string): Observable<Job> {
    return this.http.put<Job>(`${this.apiUrl}/${jobId}/result`, { result_id: resultId })
      .pipe(catchError(this.handleError));
  }

  cancelJob(jobId: string): Observable<Job> {
    return this.http.post<Job>(`${this.apiUrl}/${jobId}/cancel`, {})
      .pipe(catchError(this.handleError));
  }

  /**
   * Poll a job until completion or failure
   * @param jobId The job ID to poll
   * @param chatId The chat ID for message handling
   * @param pollingInterval Polling interval in milliseconds (default 2000ms)
   */
  pollJob(jobId: string, chatId: string, pollingInterval: number = 2000): Observable<Job> {
    // Add job to active polling set
    const activeJobs = this.activeJobsSubject.getValue();
    activeJobs.add(jobId);
    this.activeJobsSubject.next(activeJobs);

    /** Avoid refetching the same assistant row when job snapshots repeat with identical result + phase. */
    let lastFetchedMessageKey = '';
    let lastUserSyncKey = '';

    return timer(0, pollingInterval)
      .pipe(
        switchMap(() => this.getJob(jobId)),
        distinctUntilChanged(
          (a, b) => {
            if (!a || !b) return a === b;
            return (
              a.status === b.status &&
              (a.result_id ?? '') === (b.result_id ?? '') &&
              (a.error ?? '') === (b.error ?? '') &&
              serializeDraftDeltas(a.draft_deltas) === serializeDraftDeltas(b.draft_deltas)
            );
          },
        ),
        takeWhile(job => !!job && job.status !== 'complete' && job.status !== 'cancelled' && job.status !== 'failed', true),
        tap(job => {
          if (!job) return;
          const phasesWithMessage = [
            'inference_complete',
            'expression_complete',
            'compaction_complete',
            'complete',
            'cancelled',
          ];
          if (job.result_id && phasesWithMessage.includes(job.status)) {
            const fetchKey = `${job.result_id}:${job.status}`;
            if (fetchKey === lastFetchedMessageKey) {
              return;
            }
            lastFetchedMessageKey = fetchKey;
            this.messageService.getMessage(job.result_id).subscribe({
              next: (message: ChatMessage) => {
                this.messageService.addAssistantMessage(message);
              },
              error: error => {
                console.error('Failed to fetch result message:', error);
                if (job.status === 'complete') {
                  this.messageService.addErrorMessage(chatId, 'Failed to retrieve assistant response');
                }
              }
            });
            if (job.job_type === 'chat_message' && job.reference && job.reference !== job.result_id) {
              const ukey = `${job.reference}:${job.status}`;
              if (ukey !== lastUserSyncKey) {
                lastUserSyncKey = ukey;
                this.messageService.getMessage(job.reference).subscribe({
                  next: (m: ChatMessage) => this.messageService.addMessageToList(m),
                  error: () => {},
                });
              }
            }
          }
          if (job.status === 'failed') {
            this.handleJobFailed(job, chatId);
          }
          if (job.status === 'complete' && !job.result_id) {
            this.messageService.addErrorMessage(chatId, 'Job completed but no result was provided');
          }
        }),
        finalize(() => {
          // Remove job from active polling set
          const activeJobs = this.activeJobsSubject.getValue();
          activeJobs.delete(jobId);
          this.activeJobsSubject.next(activeJobs);
        }),
        catchError(error => {
          this.messageService.addErrorMessage(chatId, 'Failed to process message');
          return throwError(() => error);
        })
      );
  }

  /**
   * Handle failed job
   */
  private handleJobFailed(job: Job, chatId: string): void {
    this.messageService.setAssistantTyping(false);
    if (job.job_type === 'chat_message' && job.reference?.trim()) {
      this.messageService.getMessage(job.reference).subscribe({
        next: (m: ChatMessage) => this.messageService.addMessageToList(m),
        error: () => {
          const errorMessage = job.error || 'The assistant failed to process your message';
          this.messageService.addErrorMessage(chatId, errorMessage);
        },
      });
      return;
    }
    const errorMessage = job.error || 'The assistant failed to process your message';
    this.messageService.addErrorMessage(chatId, errorMessage);
  }

  /**
   * Latest non-terminal chat_message job for a user turn, if any (e.g. resume after refresh).
   */
  getActiveChatMessageJob(chatId: string, messageId: string): Observable<ActiveChatMessageJob | null> {
    const url = `${this.chatApiUrl}/${chatId}/chat-message/${messageId}/active-job`;
    return this.http.get<ActiveChatMessageJob>(url, { observe: 'response' }).pipe(
      map((res: HttpResponse<ActiveChatMessageJob>) => (res.status === 204 ? null : res.body)),
      catchError(this.handleError),
    );
  }

  /**
   * Check if a job is currently being polled
   */
  isJobBeingPolled(jobId: string): boolean {
    return this.activeJobsSubject.getValue().has(jobId);
  }

  /**
   * Get the count of active polling jobs
   */
  getActiveJobCount(): number {
    return this.activeJobsSubject.getValue().size;
  }

  /**
   * Clear all cached job data
   * Should be called on logout to prevent showing stale data to different users
   */
  clearCache(): void {
    this.activeJobsSubject.next(new Set());
  }

  private handleError(error: HttpErrorResponse): Observable<never> {
    const errorMessage = apiErrorMessage(error, 'An error occurred');

    console.error('Job Service Error:', errorMessage);
    return throwError(() => new Error(errorMessage));
  }
}

function serializeDraftDeltas(deltas?: string[]): string {
  if (!deltas || deltas.length === 0) return '';
  return deltas.join('\u0000');
}
