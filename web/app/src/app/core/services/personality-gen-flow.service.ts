import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpResponse } from '@angular/common/http';
import { Observable } from 'rxjs';
import { map } from 'rxjs/operators';
import { environment } from '@environments/environment';
import { AcceptFlowRequest, PersonalityGenFlow, UpdateFlowRequest } from '../models/personality-gen-flow.model';
import { Personality } from '../models/personality.model';
import { ActivePersonalityMediaJob, PersonalityMediaJobResponse } from '../models/personality-media-job.model';

@Injectable({
  providedIn: 'root'
})
export class PersonalityGenFlowService {
  private http = inject(HttpClient);
  private apiUrl = `${environment.apiUrl}/personality/generate`;

  /** Returns the user's active flow (in_progress or generated), creating one if none exists. */
  getOrCreateFlow(): Observable<PersonalityGenFlow> {
    return this.http.get<PersonalityGenFlow>(this.apiUrl);
  }

  /** Saves partial wizard progress (answers + current step). */
  updateFlow(flowId: string, req: UpdateFlowRequest): Observable<PersonalityGenFlow> {
    return this.http.put<PersonalityGenFlow>(`${this.apiUrl}/${flowId}`, req);
  }

  /** Returns a specific flow by id. */
  getFlow(flowId: string): Observable<PersonalityGenFlow> {
    return this.http.get<PersonalityGenFlow>(`${this.apiUrl}/${flowId}`);
  }

  /** Abandons a draft flow and returns a new empty flow. */
  resetFlow(flowId: string): Observable<PersonalityGenFlow> {
    return this.http.post<PersonalityGenFlow>(`${this.apiUrl}/${flowId}/reset`, {});
  }

  /** Enqueues background generation from the collected answers. */
  completeFlow(flowId: string): Observable<PersonalityMediaJobResponse> {
    return this.http.post<PersonalityMediaJobResponse>(`${this.apiUrl}/${flowId}/complete`, {});
  }

  /** Re-enqueues generation with the same answers. */
  regenerateFlow(flowId: string): Observable<PersonalityMediaJobResponse> {
    return this.http.post<PersonalityMediaJobResponse>(`${this.apiUrl}/${flowId}/regenerate`, {});
  }

  /** Returns the flow's current active generation job, if any. */
  getActiveGenerationJob(flowId: string): Observable<ActivePersonalityMediaJob | null> {
    return this.http
      .get<ActivePersonalityMediaJob>(`${this.apiUrl}/${flowId}/active-job`, { observe: 'response' })
      .pipe(
        map((res: HttpResponse<ActivePersonalityMediaJob>) => {
          if (res.status === 204) return null;
          if (this.isActiveGenerationJobResponse(res.body)) return res.body;
          throw new Error('Invalid active generation job response');
        }),
      );
  }

  /** Creates a real personality from the flow and returns it. */
  acceptFlow(flowId: string, req: AcceptFlowRequest): Observable<Personality> {
    return this.http.post<Personality>(`${this.apiUrl}/${flowId}/accept`, req);
  }

  private isActiveGenerationJobResponse(body: unknown): body is ActivePersonalityMediaJob {
    if (!body || typeof body !== 'object') return false;
    const candidate = body as Partial<ActivePersonalityMediaJob>;
    return (
      typeof candidate.job_id === 'string' &&
      typeof candidate.job_type === 'string' &&
      typeof candidate.reference === 'string' &&
      typeof candidate.status === 'string'
    );
  }
}
