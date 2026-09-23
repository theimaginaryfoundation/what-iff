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
import { CHAT_JOB_POLL_INTERVAL_MS } from './chat.constants';
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

    it('runs a focus sync that arrived mid-turn once this tab\'s reply has landed', async () => {
        const messages = TestBed.inject(MessageService);
        const flushMessageFetches = () =>
            http.match(r => r.url.startsWith(`${api}/chat/chat-message/`)).forEach(r =>
                r.flush(r.request.url.endsWith('asst-1')
                    ? assistantMsg('asst-1', 'chat-B', '2026-09-23T10:00:05Z')
                    : userMsg('user-1', 'chat-B', '2026-09-23T10:00:00Z')));
        service.setActive('chat-B');
        flushList('chat-B', []);
        noActiveJob('chat-B');

        const send = service.sendMessage('first');
        http.expectOne(`${api}/chat/chat-B/chat-message`).flush({ id: 'user-1', job_id: 'job-1', type: 'chat_message' });
        await send;
        await vi.advanceTimersByTimeAsync(0);
        http.expectOne(`${api}/job/job-1`).flush({ id: 'job-1', status: 'processing', job_type: 'chat_message', reference: 'user-1' });

        // Another tab sends "user-2" while this tab is still generating: hold the sync.
        service.syncActiveThread(true);
        http.expectNone(r => r.url === `${api}/chat/chat-B/chat-message`);

        // Reply lands; post-inference phases (expression, summarization) keep job-1 pending.
        await vi.advanceTimersByTimeAsync(CHAT_JOB_POLL_INTERVAL_MS);
        http.expectOne(`${api}/job/job-1`).flush({
            id: 'job-1', status: 'inference_complete', job_type: 'chat_message', reference: 'user-1', result_id: 'asst-1',
        });
        flushMessageFetches();
        TestBed.tick();
        // Still animating the reply in: keep holding.
        expect(service.isStreaming()).toBe(true);
        http.expectNone(r => r.url === `${api}/chat/chat-B/chat-message`);

        const streaming = TestBed.inject(ChatStreamingService) as unknown as { setCompletionCallback: ReturnType<typeof vi.fn> };
        const onStreamComplete = streaming.setCompletionCallback.mock.calls[0][0] as (id: string) => void;
        onStreamComplete(service.streamingMessageId()!);
        TestBed.tick();

        // job-1 is still in post-inference, which must not hold the sync or the resume.
        expect(service.assistantJobPending()).toBe(true);
        http.expectOne(r => r.url === `${api}/chat/chat-B/chat-message`).flush({
            results: [userMsg('user-2', 'chat-B', '2026-09-23T10:01:00Z'), assistantMsg('asst-1', 'chat-B', '2026-09-23T10:00:05Z'),
                userMsg('user-1', 'chat-B', '2026-09-23T10:00:00Z')],
            page: 1, total_count: 3,
        });
        activeJobLookup('chat-B').flush({ job_id: 'job-2', status: 'processing', message_id: 'user-2' });

        expect(messages.getMessages().map(m => m.id)).toContain('user-2');
        await vi.advanceTimersByTimeAsync(0);
        http.expectOne(`${api}/job/job-2`).flush({
            id: 'job-2', status: 'processing', job_type: 'chat_message', reference: 'user-2', draft_deltas: ['Hel'],
        });
        expect(service.isGenerating()).toBe(true);
        expect(service.pendingAssistantDraftText()).toBe('Hel');

        // job-1 finishing its post-inference phases must not wipe job-2's in-progress draft.
        await vi.advanceTimersByTimeAsync(CHAT_JOB_POLL_INTERVAL_MS);
        http.expectOne(`${api}/job/job-1`).flush({
            id: 'job-1', status: 'complete', job_type: 'chat_message', reference: 'user-1', result_id: 'asst-1',
        });
        flushMessageFetches();
        http.expectOne(`${api}/job/job-2`).flush({
            id: 'job-2', status: 'processing', job_type: 'chat_message', reference: 'user-2', draft_deltas: ['Hel', 'lo'],
        });
        expect(service.pendingAssistantDraftText()).toBe('Hello');
        expect(service.isGenerating()).toBe(true);
    });

    it('surfaces the running turn\'s tool timeline from job progress, and drops it on switch', async () => {
        service.setActive('chat-B');
        flushList('chat-B', []);
        noActiveJob('chat-B');

        const send = service.sendMessage('look things up');
        http.expectOne(`${api}/chat/chat-B/chat-message`).flush({ id: 'user-1', job_id: 'job-1', type: 'chat_message' });
        await send;
        await vi.advanceTimersByTimeAsync(0);
        const progress = (calls: object[]) => JSON.stringify({ tool_calls: calls });
        http.expectOne(`${api}/job/job-1`).flush({
            id: 'job-1', status: 'processing', job_type: 'chat_message', reference: 'user-1',
            progress: progress([{ id: 'c1', name: 'recall_memories', status: 'running', round: 0, started_at: 't0' }]),
        });
        expect(service.liveToolCalls().map(c => [c.name, c.status])).toEqual([['recall_memories', 'running']]);

        // Only the progress changed: the poller must still deliver it.
        await vi.advanceTimersByTimeAsync(CHAT_JOB_POLL_INTERVAL_MS);
        http.expectOne(`${api}/job/job-1`).flush({
            id: 'job-1', status: 'processing', job_type: 'chat_message', reference: 'user-1',
            progress: progress([
                { id: 'c1', name: 'recall_memories', status: 'complete', output: '[]', round: 0, started_at: 't0', finished_at: 't1' },
                { id: 'c2', name: 'web_search', status: 'running', round: 1, started_at: 't2' },
            ]),
        });
        expect(service.liveToolCalls().map(c => [c.name, c.status])).toEqual([
            ['recall_memories', 'complete'],
            ['web_search', 'running'],
        ]);

        service.setActive('chat-A');
        flushList('chat-A', []);
        noActiveJob('chat-A');
        expect(service.liveToolCalls()).toEqual([]);
    });

    it('does not show the previous turn\'s tool rows while the next send is in flight', async () => {
        service.setActive('chat-B');
        flushList('chat-B', []);
        noActiveJob('chat-B');

        const first = service.sendMessage('first');
        http.expectOne(`${api}/chat/chat-B/chat-message`).flush({ id: 'user-1', job_id: 'job-1', type: 'chat_message' });
        await first;
        await vi.advanceTimersByTimeAsync(0);
        http.expectOne(`${api}/job/job-1`).flush({
            id: 'job-1', status: 'complete', job_type: 'chat_message', reference: 'user-1', result_id: 'asst-1',
            progress: JSON.stringify({ tool_calls: [{ id: 'c1', name: 'recall', status: 'complete', round: 0, started_at: 't0' }] }),
        });
        http.match(r => r.url.startsWith(`${api}/chat/chat-message/`)).forEach(r =>
            r.flush(r.request.url.endsWith('asst-1') ? assistantMsg('asst-1', 'chat-B', '2026-09-23T10:00:05Z') : userMsg('user-1', 'chat-B', '2026-09-23T10:00:00Z')));
        expect(service.liveToolCalls()).toEqual([]);

        // Second send: its POST is still in flight, so the placeholder shows with no job yet.
        const second = service.sendMessage('second');
        expect(service.assistantJobPending()).toBe(true);
        expect(service.liveToolCalls()).toEqual([]);
        http.expectOne(`${api}/chat/chat-B/chat-message`).flush({ id: 'user-2', job_id: 'job-2', type: 'chat_message' });
        await second;
    });

    it('refills the tool timeline from job progress when returning to a thread mid-turn', async () => {
        service.setActive('chat-B');
        flushList('chat-B', []);
        noActiveJob('chat-B');
        const send = service.sendMessage('look things up');
        http.expectOne(`${api}/chat/chat-B/chat-message`).flush({ id: 'user-1', job_id: 'job-1', type: 'chat_message' });
        await send;
        await vi.advanceTimersByTimeAsync(0);
        http.expectOne(`${api}/job/job-1`).flush({ id: 'job-1', status: 'processing', job_type: 'chat_message', reference: 'user-1' });

        service.setActive('chat-A');
        flushList('chat-A', []);
        noActiveJob('chat-A');
        expect(service.liveToolCalls()).toEqual([]);

        service.setActive('chat-B');
        flushList('chat-B', [userMsg('user-1', 'chat-B', '2026-09-23T10:00:00Z')]);
        activeJobLookup('chat-B').flush({ job_id: 'job-1', status: 'processing', message_id: 'user-1' });
        await vi.advanceTimersByTimeAsync(0);
        http.expectOne(`${api}/job/job-1`).flush({
            id: 'job-1', status: 'processing', job_type: 'chat_message', reference: 'user-1',
            progress: JSON.stringify({ tool_calls: [
                { id: 'c1', name: 'recall', status: 'complete', round: 0, started_at: 't0', finished_at: 't1' },
                { id: 'c2', name: 'list', status: 'running', round: 0, started_at: 't2' },
            ] }),
        });
        expect(service.liveToolCalls().map(c => [c.name, c.status])).toEqual([['recall', 'complete'], ['list', 'running']]);
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
