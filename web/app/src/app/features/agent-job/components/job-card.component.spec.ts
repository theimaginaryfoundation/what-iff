import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { JobCardComponent } from './job-card.component';

describe('JobCardComponent', () => {
    let fixture: ComponentFixture<JobCardComponent>;

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [JobCardComponent],
            providers: [provideZonelessChangeDetection()],
        }).compileComponents();

        fixture = TestBed.createComponent(JobCardComponent);
        fixture.componentRef.setInput('job', {
            id: 'job-1',
            title: 'Daily summary',
            promptExcerpt: 'Summarize tasks',
            scheduleTypeLabel: 'Recurring',
            scheduleValue: '0 9 * * *',
            status: 'active',
            statusLabel: 'Active',
            statusTone: 'success',
            timezone: 'UTC',
            nextRunLabel: 'Tomorrow',
            lastRunLabel: 'Today',
            runCount: 2,
            canRunNow: true,
            canPauseResume: true,
        });
        fixture.detectChanges();
    });

    it('renders status label text', () => {
        const host = fixture.nativeElement as HTMLElement;
        expect(host.textContent).toContain('Active');
    });
});
