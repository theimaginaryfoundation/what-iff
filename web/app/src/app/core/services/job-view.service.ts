import { Injectable, signal, computed } from '@angular/core';
import { finalize, forkJoin, map, Observable, of } from 'rxjs';

import { AgentJob, AgentJobFilters, AgentJobStatus, AgentJobScheduleType } from '../models/agent-job.model';
import { AgentJobService } from './agent-job.service';
import { inject } from '@angular/core';
import { isTerminalStatus } from '../../features/agent-job/helpers/job-status.helpers';

export interface JobViewFilters {
  search: string;
  status: AgentJobStatus | '';
  scheduleType: AgentJobScheduleType | '';
}

const DEFAULT_FILTERS: JobViewFilters = {
  search: '',
  status: '',
  scheduleType: '',
};

@Injectable({ providedIn: 'root' })
export class JobViewService {
  private readonly agentJobService = inject(AgentJobService);
  private pollingTimer: ReturnType<typeof setInterval> | null = null;

  readonly pageSize = 12;
  readonly jobs = signal<AgentJob[]>([]);
  readonly totalCount = signal(0);
  readonly currentPage = signal(1);
  readonly loading = signal(false);
  readonly error = signal<string | null>(null);
  readonly deleting = signal(false);
  readonly runningNowIds = signal<Record<string, boolean>>({});
  readonly filters = signal<JobViewFilters>({ ...DEFAULT_FILTERS });

  readonly totalPages = computed(() => Math.max(1, Math.ceil(this.totalCount() / this.pageSize)));

  load(page: number = 1): void {
    this.loading.set(true);
    this.error.set(null);
    this.currentPage.set(page);

    this.agentJobService
      .listAgentJobs(page, this.pageSize, toApiFilters(this.filters()))
      .pipe(finalize(() => this.loading.set(false)))
      .subscribe({
        next: response => {
          this.jobs.set(response.results ?? []);
          this.totalCount.set(response.total_count ?? 0);
        },
        error: error => {
          this.jobs.set([]);
          this.totalCount.set(0);
          this.error.set(error instanceof Error ? error.message : 'Failed to load jobs');
        },
      });
  }

  setFilters(partial: Partial<JobViewFilters>): void {
    this.filters.update(current => ({ ...current, ...partial }));
    this.load(1);
  }

  clearFilters(): void {
    this.filters.set({ ...DEFAULT_FILTERS });
    this.load(1);
  }

  runNow(jobId: string): Observable<void> {
    this.runningNowIds.update(ids => ({ ...ids, [jobId]: true }));
    return this.agentJobService.runNow(jobId).pipe(
      map(() => void 0),
      finalize(() => {
        this.runningNowIds.update(ids => {
          const next = { ...ids };
          delete next[jobId];
          return next;
        });
      }),
    );
  }

  updateStatus(jobId: string, status: 'active' | 'paused'): Observable<AgentJob> {
    return this.agentJobService.updateAgentJobStatus(jobId, { status });
  }

  deleteOne(jobId: string): Observable<void> {
    this.deleting.set(true);
    return this.agentJobService.deleteAgentJob(jobId).pipe(finalize(() => this.deleting.set(false)));
  }

  startPollingActiveJobs(intervalMs: number = 5000): void {
    this.stopPolling();
    this.pollingTimer = setInterval(() => {
      const activeIds = this.jobs()
        .filter(job => !isTerminalStatus(job.status))
        .map(job => job.id);
      if (activeIds.length === 0) {
        this.stopPolling();
        return;
      }

      forkJoin(activeIds.map(id => this.agentJobService.getAgentJob(id))).subscribe({
        next: latestJobs => {
          const byId = new Map(latestJobs.map(job => [job.id, job]));
          this.jobs.update(current => current.map(job => byId.get(job.id) ?? job));
        },
      });
    }, intervalMs);
  }

  stopPolling(): void {
    if (!this.pollingTimer) return;
    clearInterval(this.pollingTimer);
    this.pollingTimer = null;
  }
}

function toApiFilters(filters: JobViewFilters): AgentJobFilters {
  const api: AgentJobFilters = {};
  if (filters.search.trim()) api.search = filters.search.trim();
  if (filters.status) api.status = filters.status;
  if (filters.scheduleType) api.schedule_type = filters.scheduleType;
  return api;
}
