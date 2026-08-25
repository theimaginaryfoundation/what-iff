import { provideZonelessChangeDetection, signal } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';

import { AgentJob } from '../../core/models/agent-job.model';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { JobViewFilters, JobViewService } from '../../core/services/job-view.service';
import { JobsPageComponent } from './jobs-page.component';

describe('JobsPageComponent', () => {
    let fixture: ComponentFixture<JobsPageComponent>;
    let view: any;

    const filters: JobViewFilters = { search: '', status: '', scheduleType: '' };

    beforeEach(async () => {
        view = {
            jobs: signal<AgentJob[]>([]),
            loading: signal(false),
            error: signal<string | null>(null),
            filters: signal(filters),
            currentPage: signal(1),
            totalPages: signal(1),
            totalCount: signal(0),
            runningNowIds: signal<Record<string, boolean>>({}),
            load: vi.fn().mockName('load'),
            startPollingActiveJobs: vi.fn().mockName('startPollingActiveJobs'),
            stopPolling: vi.fn().mockName('stopPolling'),
            setFilters: vi.fn().mockName('setFilters'),
            clearFilters: vi.fn().mockName('clearFilters'),
            updateStatus: vi.fn().mockName('updateStatus'),
            runNow: vi.fn().mockName('runNow'),
            deleteOne: vi.fn().mockName('deleteOne'),
        };
        const confirmation = {
            confirm: vi.fn().mockName("ConfirmationService.confirm")
        };
        const router = {
            navigate: vi.fn().mockName("Router.navigate")
        };

        await TestBed.configureTestingModule({
            imports: [JobsPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                { provide: JobViewService, useValue: view },
                { provide: ConfirmationService, useValue: confirmation },
                { provide: Router, useValue: router },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(JobsPageComponent);
    });

    it('creates', () => {
        expect(fixture.componentInstance).toBeTruthy();
    });

    it('renders loading, error, and empty branches', () => {
        view.loading.set(true);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('[role="status"]')?.textContent).toContain('Loading jobs');

        view.loading.set(false);
        view.error.set('Unable to load jobs');
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('[role="alert"]')?.textContent).toContain('Unable to load jobs');

        view.error.set(null);
        fixture.detectChanges();
        expect(fixture.nativeElement.textContent).toContain('No jobs match your current filters.');
    });

    it('renders one card per job in the populated branch', () => {
        view.jobs.set([job('job-1', 'Morning summary'), job('job-2', 'Evening review')]);
        view.totalCount.set(2);
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelectorAll('app-job-card').length).toBe(2);
        expect(fixture.nativeElement.textContent).toContain('Morning summary');
        expect(fixture.nativeElement.textContent).toContain('Evening review');
        expect(fixture.nativeElement.textContent).toContain('2 total jobs');
    });

    it('shows pagination only when there is more than one page and reflects boundary states', () => {
        view.totalPages.set(3);
        view.currentPage.set(1);
        fixture.detectChanges();
        let footer = fixture.nativeElement.querySelector('footer') as HTMLElement;
        let buttons = footer.querySelectorAll('button');

        expect(footer.textContent).toContain('Page 1 of 3');
        expect(buttons[0].disabled).toBe(true);
        expect(buttons[1].disabled).toBe(false);

        view.currentPage.set(3);
        fixture.detectChanges();
        footer = fixture.nativeElement.querySelector('footer');
        buttons = footer.querySelectorAll('button');
        expect(buttons[1].disabled).toBe(true);

        view.totalPages.set(1);
        fixture.detectChanges();
        expect(fixture.nativeElement.querySelector('footer')).toBeNull();
    });

    function job(id: string, title: string): AgentJob {
        return {
            id,
            user_id: 'user-1',
            title,
            prompt: `${title} prompt`,
            schedule_input: 'Every day at 8am',
            schedule_type: 'cron',
            schedule: '0 8 * * *',
            timezone: 'UTC',
            status: 'active',
            run_count: 1,
            created_at: '2026-08-01T12:00:00Z',
            updated_at: '2026-08-01T12:00:00Z',
        };
    }
});
