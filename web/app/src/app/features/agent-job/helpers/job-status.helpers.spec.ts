import { AgentJob } from '../../../core/models/agent-job.model';
import {
    isTerminalStatus,
    jobStatusDescription,
    jobStatusLabel,
    jobStatusTone,
    scheduleFailureReason,
    statusDescription,
    statusLabel,
    statusTone,
} from './job-status.helpers';

describe('job-status.helpers', () => {
    it('maps statuses to labels and tones', () => {
        expect(statusLabel('active')).toBe('Active');
        expect(statusTone('active')).toBe('success');
        expect(statusLabel('paused')).toBe('Paused');
        expect(statusTone('paused')).toBe('warning');
        expect(statusLabel('failed')).toBe('Failed');
        expect(statusTone('failed')).toBe('danger');
        expect(statusLabel('complete')).toBe('Complete');
        expect(statusTone('complete')).toBe('neutral');
    });

    it('identifies terminal statuses', () => {
        expect(isTerminalStatus('complete')).toBe(true);
        expect(isTerminalStatus('failed')).toBe(true);
        expect(isTerminalStatus('active')).toBe(false);
        expect(isTerminalStatus('paused')).toBe(false);
    });

    it('describes what each status means for scheduling', () => {
        expect(statusDescription('active')).toBe('Runs on its schedule');
        expect(statusDescription('paused')).toContain('skipped until you resume');
        expect(statusDescription('complete')).toContain('one-off');
        expect(statusDescription('failed')).toContain('error');
    });

    describe('scheduler-paused recurring jobs', () => {
        const schedulerPaused: Pick<AgentJob, 'status' | 'schedule_type' | 'last_error'> = {
            status: 'paused',
            schedule_type: 'cron',
            last_error: 'failed to compute next_run_at: invalid cron schedule',
        };

        it('extracts the reason and flags the job as a schedule failure', () => {
            expect(scheduleFailureReason(schedulerPaused)).toBe('invalid cron schedule');
            expect(jobStatusLabel(schedulerPaused)).toBe('Paused: schedule error');
            expect(jobStatusTone(schedulerPaused)).toBe('danger');
            expect(jobStatusDescription(schedulerPaused)).toContain("couldn't schedule the next run");
            expect(jobStatusDescription(schedulerPaused)).toContain('invalid cron schedule');
        });

        it('finds the marker when a run error precedes it', () => {
            const job = { ...schedulerPaused, last_error: 'agent blew up\n\nfailed to compute next_run_at: bad tz' };
            expect(scheduleFailureReason(job)).toBe('bad tz');
        });

        it('leaves a user-paused job (no last_error) looking like a plain pause', () => {
            const job = { ...schedulerPaused, last_error: null };
            expect(scheduleFailureReason(job)).toBeNull();
            expect(jobStatusLabel(job)).toBe('Paused');
            expect(jobStatusTone(job)).toBe('warning');
            expect(jobStatusDescription(job)).toBe(statusDescription('paused'));
        });

        it('ignores unrelated errors and non-paused or one-off jobs', () => {
            expect(scheduleFailureReason({ ...schedulerPaused, last_error: 'run failed' })).toBeNull();
            expect(scheduleFailureReason({ ...schedulerPaused, status: 'active' })).toBeNull();
            expect(scheduleFailureReason({ ...schedulerPaused, schedule_type: 'at' })).toBeNull();
            expect(jobStatusLabel({ status: 'failed', schedule_type: 'at', last_error: 'boom' })).toBe('Failed');
        });
    });
});
