import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { Subject, of, throwError } from 'rxjs';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting } from '@angular/common/http/testing';

import { JobService } from './job.service';
import { MessageService } from './message.service';
import { Job } from '../models/job.model';

describe('JobService', () => {
    let service: JobService;
    let messageService: Pick<MockedObject<MessageService>, 'addAssistantMessage' | 'addMessageToList' | 'addErrorMessage' | 'setAssistantTyping' | 'getMessage'>;

    beforeEach(() => {
        messageService = {
            addAssistantMessage: vi.fn().mockName("MessageService.addAssistantMessage"),
            addMessageToList: vi.fn().mockName("MessageService.addMessageToList"),
            addErrorMessage: vi.fn().mockName("MessageService.addErrorMessage"),
            setAssistantTyping: vi.fn().mockName("MessageService.setAssistantTyping"),
            getMessage: vi.fn().mockName("MessageService.getMessage")
        } as unknown as Pick<MockedObject<MessageService>, 'addAssistantMessage' | 'addMessageToList' | 'addErrorMessage' | 'setAssistantTyping' | 'getMessage'>;
        messageService.getMessage.mockReturnValue(of({
            id: 'm1',
            chat_id: 'chat-1',
            message: 'stub',
            origin: 'Assistant',
            sent_at: new Date().toISOString(),
        }));

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                JobService,
                { provide: MessageService, useValue: messageService },
            ],
        });
        service = TestBed.inject(JobService);
    });

    it('pollJob handles nullable getJob emissions without throwing', async () => {
        vi.spyOn(service, 'getJob').mockReturnValue(of(null as unknown as Job));
        const emissions: Array<Job | null> = [];

        await new Promise<void>((resolve, reject) => {
            service.pollJob('job-1', 'chat-1', 10).subscribe({
                next: (value) => emissions.push(value as Job | null),
                error: reject,
                complete: resolve,
            });
        });

        expect(emissions.length).toBe(1);
        expect(emissions[0]).toBeNull();
    });

    it('pollJob treats cancelled as terminal and does not emit failure error', async () => {
        vi.spyOn(service, 'getJob').mockReturnValue(of({
            id: 'job-1',
            user_id: 'user-1',
            status: 'cancelled',
            job_type: 'chat_message',
            reference: 'chat-1',
            created_at: '',
            updated_at: '',
        } as Job));

        const emissions: Job[] = [];
        await new Promise<void>((resolve, reject) => {
            service.pollJob('job-1', 'chat-1', 10).subscribe({
                next: (value) => emissions.push(value),
                error: reject,
                complete: resolve,
            });
        });

        expect(emissions[0]?.status).toBe('cancelled');
        expect(messageService.addErrorMessage).not.toHaveBeenCalledWith('chat-1', 'Failed to process message');
    });

    it('pollJob keeps polling after a transient transport error instead of showing a false job failure', async () => {
        vi.useFakeTimers();
        try {
            let polls = 0;
            vi.spyOn(service, 'getJob').mockImplementation(() => {
                polls += 1;
                if (polls === 1) {
                    return throwError(() => new Error('temporary network failure'));
                }
                return of({
                    id: 'job-1',
                    user_id: 'user-1',
                    status: 'complete',
                    job_type: 'chat_message',
                    reference: 'message-1',
                    result_id: 'm1',
                    created_at: '',
                    updated_at: '',
                } as Job);
            });

            const emissions: Job[] = [];
            let observedError: unknown;
            let completed = false;
            service.pollJob('job-1', 'chat-1', 10).subscribe({
                next: value => emissions.push(value),
                error: error => { observedError = error; },
                complete: () => { completed = true; },
            });

            await vi.advanceTimersByTimeAsync(0);
            expect(observedError).toBeUndefined();
            expect(messageService.addErrorMessage).not.toHaveBeenCalledWith('chat-1', 'Failed to process message');
            expect(service.isJobBeingPolled('job-1')).toBe(true);

            await vi.advanceTimersByTimeAsync(10);
            expect(emissions.at(-1)?.status).toBe('complete');
            expect(completed).toBe(true);
            expect(service.isJobBeingPolled('job-1')).toBe(false);
        } finally {
            vi.useRealTimers();
        }
    });
    it('pollJob does not let a late inference_complete row overwrite the completed one', async () => {
        // Regression: a job emits several phases that each carry a result_id, and
        // each dispatches its own getMessage(). Those are independent requests, so
        // the inference_complete one can resolve *after* the complete one. The row
        // gains its context_breakdown between those phases, so applying the older
        // response last erased the breakdown — which is what made the Context X-ray
        // e2e test flaky against a real-inference backend.
        vi.useFakeTimers();
        try {
            const withoutBreakdown = {
                id: 'm1',
                chat_id: 'chat-1',
                message: 'reply',
                origin: 'Assistant',
                sent_at: new Date().toISOString(),
            };
            const withBreakdown = { ...withoutBreakdown, context_breakdown: { model: 'test-model' } };

            // One subject per phase so the test decides the completion order.
            const inferenceFetch = new Subject<typeof withoutBreakdown>();
            const completeFetch = new Subject<typeof withBreakdown>();
            messageService.getMessage
                .mockReturnValueOnce(inferenceFetch as never)
                .mockReturnValueOnce(completeFetch as never);

            const phases = ['inference_complete', 'complete'];
            let poll = 0;
            vi.spyOn(service, 'getJob').mockImplementation(() =>
                of({
                    id: 'job-1',
                    user_id: 'user-1',
                    status: phases[Math.min(poll++, phases.length - 1)],
                    job_type: 'chat_message',
                    reference: 'm1',
                    result_id: 'm1',
                    created_at: '',
                    updated_at: '',
                } as Job),
            );

            service.pollJob('job-1', 'chat-1', 10).subscribe({ error: () => {} });

            await vi.advanceTimersByTimeAsync(0);   // inference_complete dispatches fetch 1
            await vi.advanceTimersByTimeAsync(10);  // complete dispatches fetch 2

            // The newer fetch lands first, then the older one arrives late.
            completeFetch.next(withBreakdown as never);
            completeFetch.complete();
            inferenceFetch.next(withoutBreakdown as never);
            inferenceFetch.complete();
            await vi.advanceTimersByTimeAsync(0);

            const applied = messageService.addAssistantMessage.mock.calls.map(call => call[0]);
            expect(applied).toHaveLength(1);
            expect(applied[0]).toHaveProperty('context_breakdown');
        } finally {
            vi.useRealTimers();
        }
    });

    it('pollJob still applies phase rows that arrive in order', async () => {
        // The guard drops only *stale* responses; normal ordering must be unaffected.
        vi.useFakeTimers();
        try {
            const row = (extra: Record<string, unknown>) => ({
                id: 'm1',
                chat_id: 'chat-1',
                message: 'reply',
                origin: 'Assistant',
                sent_at: new Date().toISOString(),
                ...extra,
            });
            const first = new Subject<ReturnType<typeof row>>();
            const second = new Subject<ReturnType<typeof row>>();
            messageService.getMessage
                .mockReturnValueOnce(first as never)
                .mockReturnValueOnce(second as never);

            const phases = ['inference_complete', 'complete'];
            let poll = 0;
            vi.spyOn(service, 'getJob').mockImplementation(() =>
                of({
                    id: 'job-1',
                    user_id: 'user-1',
                    status: phases[Math.min(poll++, phases.length - 1)],
                    job_type: 'chat_message',
                    reference: 'm1',
                    result_id: 'm1',
                    created_at: '',
                    updated_at: '',
                } as Job),
            );

            service.pollJob('job-1', 'chat-1', 10).subscribe({ error: () => {} });
            await vi.advanceTimersByTimeAsync(0);
            first.next(row({ phase: 'inference' }) as never);
            await vi.advanceTimersByTimeAsync(10);
            second.next(row({ phase: 'complete' }) as never);
            await vi.advanceTimersByTimeAsync(0);

            const applied = messageService.addAssistantMessage.mock.calls.map(call => call[0]);
            expect(applied).toHaveLength(2);
            expect(applied[1]).toMatchObject({ phase: 'complete' });
        } finally {
            vi.useRealTimers();
        }
    });
});
