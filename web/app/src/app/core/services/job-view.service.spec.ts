import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of, throwError } from 'rxjs';

import { AgentJob } from '../models/agent-job.model';
import { AgentJobService } from './agent-job.service';
import { JobViewService } from './job-view.service';

describe('JobViewService', () => {
    let service: JobViewService;
    let agentJobService: Pick<MockedObject<AgentJobService>, 'listAgentJobs' | 'runNow' | 'updateAgentJobStatus' | 'deleteAgentJob' | 'getAgentJob'>;

    const job: AgentJob = {
        id: 'job-1',
        user_id: 'user-1',
        title: 'Morning check-in',
        prompt: 'Prompt',
        schedule_input: 'daily',
        schedule_type: 'cron',
        schedule: '0 8 * * *',
        run_at: null,
        timezone: 'UTC',
        status: 'active',
        next_run_at: null,
        last_run_at: null,
        last_error: null,
        run_count: 2,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-02T00:00:00Z',
    };

    beforeEach(() => {
        agentJobService = {
            listAgentJobs: vi.fn().mockName("AgentJobService.listAgentJobs"),
            runNow: vi.fn().mockName("AgentJobService.runNow"),
            updateAgentJobStatus: vi.fn().mockName("AgentJobService.updateAgentJobStatus"),
            deleteAgentJob: vi.fn().mockName("AgentJobService.deleteAgentJob"),
            getAgentJob: vi.fn().mockName("AgentJobService.getAgentJob")
        } as unknown as Pick<MockedObject<AgentJobService>, 'listAgentJobs' | 'runNow' | 'updateAgentJobStatus' | 'deleteAgentJob' | 'getAgentJob'>;
        agentJobService.listAgentJobs.mockReturnValue(of({ results: [job], total_count: 1, page: 1 }));
        agentJobService.runNow.mockReturnValue(of({ status: 'triggered' }));
        agentJobService.getAgentJob.mockReturnValue(of(job));
        agentJobService.deleteAgentJob.mockReturnValue(of(void 0));
        agentJobService.updateAgentJobStatus.mockReturnValue(of(job));

        TestBed.configureTestingModule({
            providers: [provideZonelessChangeDetection(), JobViewService, { provide: AgentJobService, useValue: agentJobService }],
        });
        service = TestBed.inject(JobViewService);
    });

    it('loads jobs with filters', () => {
        service.setFilters({ search: 'daily' });
        expect(agentJobService.listAgentJobs).toHaveBeenCalled();
        expect(service.jobs().length).toBe(1);
        expect(service.filters().search).toBe('daily');
    });

    it('sets error on failed load', () => {
        agentJobService.listAgentJobs.mockReturnValue(throwError(() => new Error('boom')));
        service.load();
        expect(service.error()).toBe('boom');
    });

    it('tracks run-now in-flight ids', () => {
        service.runNow('job-1').subscribe();
        expect(agentJobService.runNow).toHaveBeenCalledWith('job-1');
        expect(service.runningNowIds()['job-1']).toBeUndefined();
    });

    it('polls active jobs and can stop polling', () => {
        vi.useFakeTimers();
        service.load();
        service.startPollingActiveJobs(20);
        vi.advanceTimersByTime(25);
        expect(agentJobService.getAgentJob).toHaveBeenCalledWith('job-1');
        service.stopPolling();
        vi.useRealTimers();
    });
});
