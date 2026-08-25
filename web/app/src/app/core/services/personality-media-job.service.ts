import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpResponse } from '@angular/common/http';
import {
  Observable,
  BehaviorSubject,
  throwError,
  timer,
  switchMap,
  takeWhile,
  distinctUntilChanged,
  tap,
  map,
  catchError,
  of,
  firstValueFrom,
} from 'rxjs';
import { environment } from '../../../environments/environment';
import { Job, JobStatus } from '../models/job.model';
import { JobService } from './job.service';
import { apiErrorMessage } from '../utils/api-error.helpers';
import {
  ActivePersonalityMediaJob,
  PersonalityMediaJobConflict,
  PersonalityMediaJobResponse,
} from '../models/personality-media-job.model';

const TERMINAL: JobStatus[] = ['complete', 'failed'];

@Injectable({ providedIn: 'root' })
export class PersonalityMediaJobService {
  private http = inject(HttpClient);
  private jobService = inject(JobService);

  private personalityApi = `${environment.apiUrl}/personality`;
  private activeJobSubject = new BehaviorSubject<ActivePersonalityMediaJob | null>(null);
  readonly activeJob$ = this.activeJobSubject.asObservable();

  refreshActiveJob(): Observable<ActivePersonalityMediaJob | null> {
    return this.http
      .get<ActivePersonalityMediaJob>(`${this.personalityApi}/active-media-job`, {
        observe: 'response',
      })
      .pipe(
        map((res: HttpResponse<ActivePersonalityMediaJob>) =>
          res.status === 204 ? null : res.body,
        ),
        tap(job => this.activeJobSubject.next(job)),
        catchError(err => this.handleError(err)),
      );
  }

  startExpressionGrid(
    personalityId: string,
    opts?: { force?: boolean },
  ): Observable<PersonalityMediaJobResponse> {
    let params: Record<string, string> = {};
    if (opts?.force) {
      params = { force: 'true' };
    }
    return this.http
      .post<PersonalityMediaJobResponse>(
        `${this.personalityApi}/${personalityId}/expressions/generate-default-grid`,
        {},
        { params },
      )
      .pipe(
        switchMap(res =>
          this.refreshActiveJob().pipe(
            map(() => res),
            catchError(() => of(res)),
          ),
        ),
        catchError(err => this.handleConflict(err)),
      );
  }

  startFlowPortrait(flowId: string): Observable<PersonalityMediaJobResponse> {
    return this.http
      .post<PersonalityMediaJobResponse>(
        `${this.personalityApi}/generate/${flowId}/portrait`,
        {},
      )
      .pipe(
        switchMap(res =>
          this.refreshActiveJob().pipe(
            map(() => res),
            catchError(() => of(res)),
          ),
        ),
        catchError(err => this.handleConflict(err)),
      );
  }

  pollUntilTerminal(
    jobId: string,
    pollingInterval = 2000,
  ): Observable<Job> {
    return timer(0, pollingInterval).pipe(
      switchMap(() => this.jobService.getJob(jobId)),
      distinctUntilChanged(
        (a, b) =>
          a.status === b.status &&
          (a.result_id ?? '') === (b.result_id ?? '') &&
          (a.error ?? '') === (b.error ?? ''),
      ),
      takeWhile(job => !TERMINAL.includes(job.status), true),
      switchMap(job => {
        if (!TERMINAL.includes(job.status)) {
          return of(job);
        }
        return this.refreshActiveJob().pipe(
          map(() => job),
          catchError(() => of(job)),
        );
      }),
    );
  }

  clearCache(): void {
    this.activeJobSubject.next(null);
  }

  /** One-shot refresh without leaving a dangling subscription (for callers that need fire-and-forget). */
  refreshActiveJobSnapshot(): void {
    void firstValueFrom(this.refreshActiveJob()).catch(() => {});
  }

  private handleConflict(err: HttpErrorResponse): Observable<never> {
    if (err.status === 409 && err.error?.active) {
      const conflict = err.error as PersonalityMediaJobConflict;
      this.activeJobSubject.next(conflict.active);
    }
    return throwError(() => err);
  }

  private handleError(error: HttpErrorResponse): Observable<never> {
    const message = apiErrorMessage(error, 'An error occurred');
    console.error('PersonalityMediaJobService:', message);
    return throwError(() => error);
  }
}
