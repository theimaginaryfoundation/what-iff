import { AgentJob } from '../../../core/models/agent-job.model';
import { excerpt, formatDate, toJobCardVm } from './job-vm.helpers';

describe('job-vm.helpers', () => {
    const job: AgentJob = {
        id: 'job-1',
        user_id: 'user-1',
        title: 'Morning check-in',
        prompt: 'Summarize updates and blockers for the day.',
        schedule_input: 'every day at 8 AM',
        schedule_type: 'cron',
        schedule: '0 8 * * *',
        run_at: null,
        timezone: 'UTC',
        status: 'active',
        next_run_at: '2026-01-01T00:00:00Z',
        last_run_at: '2025-12-31T00:00:00Z',
        last_error: null,
        run_count: 4,
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-12-31T00:00:00Z',
    };

    it('maps API model into card view model', () => {
        const vm = toJobCardVm(job);
        expect(vm.id).toBe('job-1');
        expect(vm.title).toBe('Morning check-in');
        expect(vm.scheduleTypeLabel).toBe('Recurring');
        expect(vm.statusLabel).toBe('Active');
        expect(vm.canRunNow).toBe(true);
        expect(vm.canPauseResume).toBe(true);
    });

    it('formats missing dates and truncates prompt excerpt', () => {
        expect(formatDate(null)).toBe('—');
        expect(excerpt('abcdef', 4)).toBe('abc…');
    });
});
