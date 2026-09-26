import { isTerminalStatus, statusDescription, statusLabel, statusTone } from './job-status.helpers';

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
});
