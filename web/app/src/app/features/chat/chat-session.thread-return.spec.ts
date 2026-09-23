import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting, TestRequest } from '@angular/common/http/testing';
import { of } from 'rxjs';

import { ChatSessionService } from './chat-session.service';
import { ChatService } from '../../core/services/chat.service';
import { ThreadListService } from '../../core/services/thread-list.service';
import { ChatStreamingService } from '../../core/services/chat-streaming.service';
import { DraftMessageService } from '../../core/services/draft-message.service';
import { JobService } from '../../core/services/job.service';
import { MessageService } from '../../core/services/message.service';
import { ChatSendGate } from './services/chat-send-gate';
import { Chat } from '../../core/models/chat.model';
import { ChatMessage } from '../../core/models/message.model';
import { environment } from '../../../environments/environment';

/**
 * Returning to a thread whose turn is still running, with the real JobService and
 * MessageService over HttpTestingController — the polling bookkeeping and message list are
 * the parts the unit spec mocks away.
 */
describe('ChatSessionService — returning to a thread with a running turn', () => {
    const api = environment.apiUrl;
    let service: ChatSessionService;
    let http: HttpTestingController;

    const chatFor = (id: string): Chat => ({ id, user_id: 'u', name: id, created_at: '', updated_at: '' });
    const userMsg = (id: string, chatId: string, sentAt: string): ChatMessage =>
        ({ id, chat_id: chatId, origin: 'User', message: 'hi', sent_at: sentAt } as ChatMessage);
    const assistantMsg = (id: string, chatId: string, sentAt: string): ChatMessage =>
        ({ id, chat_id: chatId, origin: 'Assistant', message: 'reply', sent_at: sentAt } as ChatMessage);

    beforeEach(() => {
        vi.useFakeTimers();
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(),
                provideHttpClientTesting(),
                ChatSessionService,
                {
                    provide: ChatService,
                    useValue: {
                        getChat: vi.fn((id: string) => of(chatFor(id))),
                        setLastChatId: vi.fn(),
                        markChatRead: vi.fn(() => of({ updated_count: 0 })),
                        patchChat: vi.fn(),
                    },
                },
                { provide: ThreadListService, useValue: { clearUnreadForThread: vi.fn() } },
                { provide: DraftMessageService, useValue: { getDraft: vi.fn(() => null), saveDraft: vi.fn(), clearDraft: vi.fn() } },
                { provide: ChatSendGate, useValue: { refresh: vi.fn() } },
                {
                    provide: ChatStreamingService,
                    useValue: {
                        appendServerChunks: vi.fn(), clearMessageState: vi.fn(), completeStreaming: vi.fn(),
                        configure: vi.fn(), destroy: vi.fn(), stopStreaming: vi.fn(), startStreaming: vi.fn(),
                        setCompletionCallback: vi.fn(), getDisplayMessage: vi.fn(), getDisplayRevision: vi.fn(() => 0),
                    },
                },
            ],
        });
        service = TestBed.inject(ChatSessionService);
        http = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        vi.useRealTimers();
    });

    function flushList(chatId: string, messages: ChatMessage[]): void {
        const req = http.expectOne(r => r.url === `${api}/chat/${chatId}/chat-message`);
        // API returns newest first.
        req.flush({ results: [...messages].reverse(), page: 1, total_count: messages.length });
    }

    /** The thread-level active-job lookup every (re)entry into a thread makes. */
    function activeJobLookup(chatId: string): TestRequest {
        return http.expectOne(`${api}/chat/${chatId}/active-job`);
    }

    function noActiveJob(chatId: string): void {
        activeJobLookup(chatId).flush(null, { status: 204, statusText: 'No Content' });
    }

    it('shows the pending turn again and resumes streaming after switching back', async () => {
        service.setActive('chat-B');
        flushList('chat-B', []);
        noActiveJob('chat-B');

        const send = service.sendMessage('slow question');
        http.expectOne(`${api}/chat/chat-B/chat-message`).flush({ id: 'user-B', job_id: 'job-B', type: 'chat_message' });
        await send;
        await vi.advanceTimersByTimeAsync(0);
        http.expectOne(`${api}/job/job-B`).flush({ id: 'job-B', status: 'processing', job_type: 'chat_message', reference: 'user-B' });
        expect(service.assistantJobPending()).toBe(true);

        service.setActive('chat-A');
        flushList('chat-A', []);
        noActiveJob('chat-A');
        expect(service.assistantJobPending()).toBe(false);

        service.setActive('chat-B');
        flushList('chat-B', [userMsg('user-B', 'chat-B', '2026-09-23T10:00:00Z')]);
        activeJobLookup('chat-B').flush({ job_id: 'job-B', status: 'processing', message_id: 'user-B' });

        expect(service.assistantJobPending()).toBe(true);
        await vi.advanceTimersByTimeAsync(0);
        http.expectOne(`${api}/job/job-B`).flush({
            id: 'job-B', status: 'processing', job_type: 'chat_message', reference: 'user-B', draft_deltas: ['Hel', 'lo'],
        });
        expect(service.pendingAssistantDraftText()).toBe('Hello');
    });

    it('picks up a turn sent from another tab when this one regains focus', async () => {
        const messages = TestBed.inject(MessageService);
        service.setActive('chat-B');
        flushList('chat-B', [
            userMsg('user-1', 'chat-B', '2026-09-23T10:00:00Z'),
            assistantMsg('asst-1', 'chat-B', '2026-09-23T10:00:05Z'),
        ]);
        noActiveJob('chat-B');

        // Another tab sends "user-2"; its turn is still running when the user comes back here.
        service.syncActiveThread(true);
        http.expectOne(r => r.url === `${api}/chat/chat-B/chat-message`).flush({
            results: [userMsg('user-2', 'chat-B', '2026-09-23T10:01:00Z'), assistantMsg('asst-1', 'chat-B', '2026-09-23T10:00:05Z'),
                userMsg('user-1', 'chat-B', '2026-09-23T10:00:00Z')],
            page: 1, total_count: 3,
        });
        activeJobLookup('chat-B').flush({ job_id: 'job-2', status: 'processing', message_id: 'user-2' });

        expect(messages.getMessages().map(m => m.id)).toContain('user-2');
        expect(service.assistantJobPending()).toBe(true);
        await vi.advanceTimersByTimeAsync(0);
        http.expectOne(`${api}/job/job-2`).flush({ id: 'job-2', status: 'processing', job_type: 'chat_message', reference: 'user-2' });
    });

    it('does not resume when nothing is running for the thread', () => {
        service.setActive('chat-B');
        flushList('chat-B', [
            userMsg('user-B', 'chat-B', '2026-09-23T10:00:00Z'),
            assistantMsg('asst-B', 'chat-B', '2026-09-23T10:00:05Z'),
        ]);
        noActiveJob('chat-B');
        expect(service.assistantJobPending()).toBe(false);
        http.expectNone(r => r.url.includes('/job/'));
    });
});
