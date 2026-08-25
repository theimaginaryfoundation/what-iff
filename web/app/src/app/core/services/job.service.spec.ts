import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of } from 'rxjs';
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
});
