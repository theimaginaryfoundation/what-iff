import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { AgentJob } from '../../../core/models/agent-job.model';
import { provideHttpClient } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';

import { CurrentTimezoneService } from '../../../core/services/current-timezone.service';
import { JobFormComponent } from './job-form.component';

describe('JobFormComponent', () => {
    let fixture: ComponentFixture<JobFormComponent>;
    let component: JobFormComponent;

    const job: AgentJob = {
        id: 'job-1',
        user_id: 'user-1',
        title: 'Daily summary',
        prompt: 'Summarize tasks',
        schedule_input: 'every day at 9 AM',
        schedule_type: 'cron',
        schedule: '0 9 * * *',
        run_at: null,
        timezone: 'UTC',
        status: 'active',
        next_run_at: null,
        last_run_at: null,
        last_error: null,
        run_count: 0,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
    };

    beforeEach(async () => {
        await TestBed.configureTestingModule({
            imports: [JobFormComponent],
            providers: [provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()],
        }).compileComponents();

        fixture = TestBed.createComponent(JobFormComponent);
        component = fixture.componentInstance;
        fixture.componentRef.setInput('job', job);
        fixture.detectChanges();
    });

    it('emits preview request when preview button clicked', () => {
        const emitSpy = vi.spyOn(component.previewRequested, 'emit').mockReturnValue(undefined);
        component.requestPreview();
        expect(emitSpy).toHaveBeenCalledWith({
            schedule_input: 'every day at 9 AM',
            timezone: 'UTC',
        });
    });

    it('emits schedule edits for debounce parsing', () => {
        const emitSpy = vi.spyOn(component.scheduleEdited, 'emit').mockReturnValue(undefined);
        component.onScheduleChange('every monday at 8');
        expect(emitSpy).toHaveBeenCalled();
    });

    it('includes aria-describedby for schedule parse feedback', () => {
        const host = fixture.nativeElement as HTMLElement;
        const input = host.querySelector('#schedule-input');
        expect(input?.getAttribute('aria-describedby')).toContain('schedule-parse-help');
        expect(input?.getAttribute('aria-describedby')).toContain('schedule-parse-preview');
    });

    it('does not overwrite local prompt edits when same job refreshes', () => {
        component.prompt.set('Draft prompt edit');
        fixture.componentRef.setInput('job', {
            ...job,
            prompt: 'Server-side refresh value',
            updated_at: '2025-01-01T00:01:00Z',
        });
        fixture.detectChanges();

        expect(component.prompt()).toBe('Draft prompt edit');
    });

    it('rehydrates when server update changes and form is clean', () => {
        fixture.componentRef.setInput('job', {
            ...job,
            prompt: 'Server updated prompt',
            updated_at: '2025-01-01T00:03:00Z',
        });
        fixture.detectChanges();

        expect(component.prompt()).toBe('Server updated prompt');
    });

    it('tracks unsaved edits against latest job input', () => {
        expect(component.hasUnsavedEdits()).toBe(false);

        component.prompt.set('Updated prompt');
        expect(component.hasUnsavedEdits()).toBe(true);

        fixture.componentRef.setInput('job', {
            ...job,
            prompt: 'Updated prompt',
            updated_at: '2025-01-01T00:02:00Z',
        });
        fixture.detectChanges();

        expect(component.hasUnsavedEdits()).toBe(false);
    });
    it('leaves following the user off by default and sends the toggle on save', () => {
        const emitSpy = vi.spyOn(component.save, 'emit').mockReturnValue(undefined);
        expect(component.followUserTimezone()).toBe(false);

        const box = (fixture.nativeElement as HTMLElement).querySelector<HTMLInputElement>('[data-testid="job-follow-user-timezone"]');
        expect(box).not.toBeNull();
        component.followUserTimezone.set(true);
        expect(component.hasUnsavedEdits()).toBe(true);

        component.submit();
        expect(emitSpy).toHaveBeenCalledWith(expect.objectContaining({ follow_user_timezone: true }));
    });

    it('shows the timezone a following job runs in while it differs from its own', async () => {
        fixture.componentRef.setInput('job', {
            ...job,
            updated_at: '2025-01-02T00:00:00Z',
            timezone: 'America/New_York',
            follow_user_timezone: true,
            effective_timezone: 'America/Los_Angeles',
        });
        fixture.detectChanges();
        await fixture.whenStable();

        const host = fixture.nativeElement as HTMLElement;
        expect(component.followUserTimezone()).toBe(true);
        expect(host.querySelector('[data-testid="job-travel-timezone"]')?.textContent).toContain('America/Los_Angeles');

        fixture.componentRef.setInput('job', { ...job, follow_user_timezone: true, effective_timezone: 'UTC', updated_at: '2025-01-03T00:00:00Z' });
        fixture.detectChanges();
        await fixture.whenStable();
        expect(host.querySelector('[data-testid="job-travel-timezone"]')).toBeNull();
    });
    it('offers following only for recurring schedules', async () => {
        const host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('[data-testid="job-follow-user-timezone"]')).not.toBeNull();

        fixture.componentRef.setInput('job', { ...job, schedule_type: 'at', updated_at: '2025-01-04T00:00:00Z' });
        fixture.detectChanges();
        await fixture.whenStable();
        expect(host.querySelector('[data-testid="job-follow-user-timezone"]')).toBeNull();
    });

    it('says when following is paused instead of naming a travel timezone', async () => {
        const timezone = TestBed.inject(CurrentTimezoneService);
        timezone.start({ id: 'u', username: 'u', email: 'u@example.com', created_at: '', updated_at: '', timezone_following_paused_at: '2026-10-28T12:00:00Z', current_timezone: '' });
        fixture.componentRef.setInput('job', {
            ...job,
            updated_at: '2025-01-05T00:00:00Z',
            timezone: 'America/New_York',
            follow_user_timezone: true,
            effective_timezone: 'America/New_York',
        });
        fixture.detectChanges();
        await fixture.whenStable();

        const host = fixture.nativeElement as HTMLElement;
        expect(host.querySelector('[data-testid="job-following-paused"]')).not.toBeNull();
        expect(host.querySelector('[data-testid="job-travel-timezone"]')).toBeNull();
        timezone.stop();
    });
});
