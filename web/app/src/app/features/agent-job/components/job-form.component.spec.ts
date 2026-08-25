import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { AgentJob } from '../../../core/models/agent-job.model';
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
            providers: [provideZonelessChangeDetection()],
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
});
