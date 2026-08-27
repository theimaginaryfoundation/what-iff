import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { BehaviorSubject, Subject, of, throwError } from 'rxjs';

import { ChatSessionService } from './chat-session.service';
import { isChatSendFailed, isChatSendSucceeded } from './chat-send-result';
import { ChatService } from '../../core/services/chat.service';
import { ThreadListService } from '../../core/services/thread-list.service';
import { ChatStreamingService } from '../../core/services/chat-streaming.service';
import { DraftMessageService } from '../../core/services/draft-message.service';
import { JobService } from '../../core/services/job.service';
import { MessageService } from '../../core/services/message.service';
import { Chat } from '../../core/models/chat.model';
import { ChatMessage } from '../../core/models/message.model';
import { ChatSendGate } from './services/chat-send-gate';
import { CHAT_PENDING_ASSISTANT_MESSAGE_ID } from './chat.constants';

describe('ChatSessionService', () => {
    type ChatServiceMock = Pick<MockedObject<ChatService>, 'createChat' | 'getChat' | 'patchChat' | 'setLastChatId' | 'markChatRead'>;
    type ThreadListServiceMock = Pick<MockedObject<ThreadListService>, 'clearUnreadForThread'>;
    type MessageServiceMock = Pick<MockedObject<MessageService>, 'clearMessages' | 'listMessages' | 'reconcileLatestPage' | 'sendMessage' | 'retryUserMessage' | 'setCurrentChatId' | 'markAssistantMessagesRead' | 'messages$'>;
    type ChatStreamingServiceMock = Pick<MockedObject<ChatStreamingService>, 'appendServerChunks' | 'clearMessageState' | 'completeStreaming' | 'configure' | 'destroy' | 'getDisplayMessage' | 'getDisplayRevision' | 'setCompletionCallback' | 'startStreaming' | 'stopStreaming'>;
    type DraftMessageServiceMock = Pick<MockedObject<DraftMessageService>, 'clearDraft' | 'getDraft' | 'saveDraft'>;
    type JobServiceMock = Pick<MockedObject<JobService>, 'pollJob' | 'getActiveChatMessageJob' | 'isJobBeingPolled' | 'cancelJob'>;
    type ChatSendGateMock = Pick<MockedObject<ChatSendGate>, 'refresh'>;

    let service: ChatSessionService;
    let chatService: ChatServiceMock;
    let threadList: ThreadListServiceMock;
    let messageService: MessageServiceMock;
    let streamingService: ChatStreamingServiceMock;
    let draftService: DraftMessageServiceMock;
    let jobService: JobServiceMock;
    let sendGate: ChatSendGateMock;
    let messages$: BehaviorSubject<ChatMessage[]>;

    const chat: Chat = {
        id: 'chat-1',
        user_id: 'user-1',
        name: 'Test Chat',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
    };
    const secondChat: Chat = {
        id: 'chat-2',
        user_id: 'user-1',
        name: 'Second Chat',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-01T00:00:00Z',
    };

    beforeEach(() => {
        messages$ = new BehaviorSubject<ChatMessage[]>([]);
        chatService = {
            createChat: vi.fn().mockName("ChatService.createChat"),
            getChat: vi.fn().mockName("ChatService.getChat"),
            patchChat: vi.fn().mockName("ChatService.patchChat"),
            setLastChatId: vi.fn().mockName("ChatService.setLastChatId"),
            markChatRead: vi.fn().mockName("ChatService.markChatRead")
        } as unknown as ChatServiceMock;
        threadList = {
            clearUnreadForThread: vi.fn().mockName("ThreadListService.clearUnreadForThread")
        } as unknown as ThreadListServiceMock;
        messageService = {
            clearMessages: vi.fn().mockName("MessageService.clearMessages"),
            listMessages: vi.fn().mockName("MessageService.listMessages"),
            reconcileLatestPage: vi.fn().mockName("MessageService.reconcileLatestPage"),
            sendMessage: vi.fn().mockName("MessageService.sendMessage"),
            retryUserMessage: vi.fn().mockName("MessageService.retryUserMessage"),
            setCurrentChatId: vi.fn().mockName("MessageService.setCurrentChatId"),
            markAssistantMessagesRead: vi.fn().mockName("MessageService.markAssistantMessagesRead"),
            messages$: messages$.asObservable()
        } as unknown as MessageServiceMock;
        streamingService = {
            appendServerChunks: vi.fn().mockName("ChatStreamingService.appendServerChunks"),
            clearMessageState: vi.fn().mockName("ChatStreamingService.clearMessageState"),
            completeStreaming: vi.fn().mockName("ChatStreamingService.completeStreaming"),
            configure: vi.fn().mockName("ChatStreamingService.configure"),
            destroy: vi.fn().mockName("ChatStreamingService.destroy"),
            getDisplayMessage: vi.fn().mockName("ChatStreamingService.getDisplayMessage"),
            getDisplayRevision: vi.fn().mockName("ChatStreamingService.getDisplayRevision"),
            setCompletionCallback: vi.fn().mockName("ChatStreamingService.setCompletionCallback"),
            startStreaming: vi.fn().mockName("ChatStreamingService.startStreaming"),
            stopStreaming: vi.fn().mockName("ChatStreamingService.stopStreaming")
        } as unknown as ChatStreamingServiceMock;
        draftService = {
            clearDraft: vi.fn().mockName("DraftMessageService.clearDraft"),
            getDraft: vi.fn().mockName("DraftMessageService.getDraft"),
            saveDraft: vi.fn().mockName("DraftMessageService.saveDraft")
        } as unknown as DraftMessageServiceMock;
        jobService = {
            pollJob: vi.fn().mockName("JobService.pollJob"),
            getActiveChatMessageJob: vi.fn().mockName("JobService.getActiveChatMessageJob"),
            isJobBeingPolled: vi.fn().mockName("JobService.isJobBeingPolled"),
            cancelJob: vi.fn().mockName("JobService.cancelJob")
        } as unknown as JobServiceMock;
        jobService.isJobBeingPolled.mockReturnValue(false);
        jobService.getActiveChatMessageJob.mockReturnValue(of(null));
        jobService.cancelJob.mockReturnValue(of({
            id: 'job-1',
            user_id: 'user-1',
            status: 'processing',
            job_type: 'chat_message',
            reference: 'user-msg',
            created_at: '',
            updated_at: '',
        }));
        sendGate = {
            refresh: vi.fn().mockName("ChatSendGate.refresh")
        } as unknown as ChatSendGateMock;

        chatService.getChat.mockReturnValue(of(chat));
        chatService.patchChat.mockReturnValue(of(chat));
        chatService.createChat.mockReturnValue(of(chat));
        chatService.markChatRead.mockReturnValue(of({ updated_count: 0 }));
        messageService.listMessages.mockReturnValue(of({ results: [], page: 1, total_count: 0 }));
        messageService.reconcileLatestPage.mockReturnValue(of({ appended: 0, total: 0, gap: false }));
        messageService.sendMessage.mockReturnValue(of({ id: 'user-msg', job_id: 'job-1', type: 'chat_message' }));
        messageService.retryUserMessage.mockReturnValue(of({ id: 'user-msg', job_id: 'job-2', type: 'chat_message' }));
        draftService.getDraft.mockReturnValue({ chatId: 'chat-1', message: 'saved draft', timestamp: Date.now() });
        jobService.pollJob.mockReturnValue(of({
            id: 'job-1',
            user_id: 'user-1',
            status: 'complete',
            job_type: 'chat',
            reference: 'chat-1',
            created_at: '',
            updated_at: '',
        }));
        streamingService.getDisplayMessage.mockImplementation((message: ChatMessage) => message.message);
        streamingService.getDisplayRevision.mockReturnValue(0);

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                ChatSessionService,
                { provide: ChatService, useValue: chatService },
                { provide: ThreadListService, useValue: threadList },
                { provide: MessageService, useValue: messageService },
                { provide: ChatStreamingService, useValue: streamingService },
                { provide: DraftMessageService, useValue: draftService },
                { provide: JobService, useValue: jobService },
                { provide: ChatSendGate, useValue: sendGate },
            ],
        });
        service = TestBed.inject(ChatSessionService);
    });

    it('loads chat, messages and draft when active thread changes', () => {
        service.setActive('chat-1');

        expect(messageService.setCurrentChatId).toHaveBeenCalledWith('chat-1');
        expect(chatService.getChat).toHaveBeenCalledWith('chat-1');
        expect(messageService.listMessages).toHaveBeenCalledWith('chat-1', 1, expect.any(Number));
        expect(chatService.markChatRead).toHaveBeenCalledWith('chat-1');
        expect(messageService.markAssistantMessagesRead).toHaveBeenCalledWith('chat-1');
        expect(threadList.clearUnreadForThread).toHaveBeenCalledWith('chat-1');
        expect(service.thread()).toEqual(chat);
        expect(service.draft()).toBe('saved draft');
    });

    it('refreshes the send gate when a chat job finishes', async () => {
        service.setActive('chat-1');
        await service.sendMessage('hello');
        expect(sendGate.refresh).toHaveBeenCalled();
    });

    it('clears expectingAssistantResponse when job polling finalizes without assistant message', async () => {
        service.setActive('chat-1');
        await service.sendMessage('hello');
        expect(service['expectingAssistantResponse']).toBe(false);
    });

    it('sends a message and starts job polling', async () => {
        service.setActive('chat-1');
        const result = await service.sendMessage(' hello ');

        expect(isChatSendSucceeded(result)).toBe(true);
        expect(messageService.sendMessage).toHaveBeenCalledWith('chat-1', expect.objectContaining({
            message: 'hello',
            origin: 'User',
        }));
        expect(jobService.pollJob).toHaveBeenCalledWith('job-1', 'chat-1');
    });

    it('streams the next assistant message after a send', async () => {
        const activeJob$ = new Subject<unknown>();
        jobService.pollJob.mockReturnValue(activeJob$ as any);
        service.setActive('chat-1');
        const result = await service.sendMessage('hello');
        expect(isChatSendSucceeded(result)).toBe(true);
        const user = message('user-msg', 'User');
        const assistant = message('assistant-1', 'Assistant');

        messages$.next([user, assistant]);

        expect(streamingService.startStreaming).toHaveBeenCalledWith(assistant);
        expect(service.streamingMessageId()).toBe('assistant-1');
    });

    it('does not re-stream prior assistant message while waiting for new user turn message insertion', async () => {
        const activeJob$ = new Subject<unknown>();
        jobService.pollJob.mockReturnValue(activeJob$ as any);
        service.setActive('chat-1');

        const priorAssistant = message('assistant-old', 'Assistant');
        messages$.next([priorAssistant]);

        const result = await service.sendMessage('hello');
        expect(isChatSendSucceeded(result)).toBe(true);
        expect(streamingService.startStreaming).not.toHaveBeenCalled();

        const user = message('user-msg', 'User');
        const nextAssistant = message('assistant-new', 'Assistant');
        messages$.next([priorAssistant, user, nextAssistant]);

        expect(streamingService.startStreaming).toHaveBeenCalledWith(nextAssistant);
    });

    it('does not force animate option when appending server draft deltas', async () => {
        const activeJob$ = new Subject<any>();
        jobService.pollJob.mockReturnValue(activeJob$ as any);
        service.setActive('chat-1');

        const result = await service.sendMessage('hello');
        expect(isChatSendSucceeded(result)).toBe(true);

        activeJob$.next({
            id: 'job-1',
            user_id: 'user-1',
            status: 'processing',
            job_type: 'chat_message',
            reference: 'user-msg',
            draft_deltas: ['Part 1'],
            created_at: '',
            updated_at: '',
        });

        expect(streamingService.appendServerChunks).toHaveBeenCalledWith(CHAT_PENDING_ASSISTANT_MESSAGE_ID, ['Part 1']);
    });

    it('stops reporting generating once core inference completes, before post-inference phases', async () => {
        const activeJob$ = new Subject<any>();
        jobService.pollJob.mockReturnValue(activeJob$ as any);
        service.setActive('chat-1');

        const result = await service.sendMessage('hello');
        expect(isChatSendSucceeded(result)).toBe(true);

        activeJob$.next({
            id: 'job-1',
            user_id: 'user-1',
            status: 'processing',
            job_type: 'chat_message',
            reference: 'user-msg',
            created_at: '',
            updated_at: '',
        });
        expect(service.isGenerating()).toBe(true);

        // Core inference done: expression + summarization still run in the background,
        // but the stop button / composer lock must release now.
        activeJob$.next({
            id: 'job-1',
            user_id: 'user-1',
            status: 'inference_complete',
            job_type: 'chat_message',
            reference: 'user-msg',
            result_id: 'assistant-1',
            created_at: '',
            updated_at: '',
        });

        expect(service.assistantJobPending()).toBe(true);
        expect(service.isGenerating()).toBe(false);
        activeJob$.complete();
    });

    it('clears pending placeholder stream cache when starting a new polled job', () => {
        service.setActive('chat-1');
        service.startAssistantJobPolling('job-1', 'chat-1');

        expect(streamingService.clearMessageState).toHaveBeenCalledWith(CHAT_PENDING_ASSISTANT_MESSAGE_ID);
    });

    it('cancels active streaming', () => {
        service.setActive('chat-1');
        service['expectingAssistantResponse'] = true;
        messages$.next([message('assistant-1', 'Assistant')]);

        service.cancelStreaming();

        expect(streamingService.stopStreaming).toHaveBeenCalledWith('assistant-1', undefined, false);
        expect(service.streamingMessageId()).toBeNull();
    });

    it('requests backend cancellation for an active job', () => {
        const activeJob$ = new Subject<any>();
        jobService.pollJob.mockReturnValue(activeJob$ as any);
        service.setActive('chat-1');
        service.startAssistantJobPolling('job-1', 'chat-1');

        service.cancelGeneration();

        expect(jobService.cancelJob).toHaveBeenCalledWith('job-1');
        expect(service.assistantJobPending()).toBe(true);
        expect(service.isGenerating()).toBe(false);
        activeJob$.complete();
    });

    it('keeps pending draft text when cancelling placeholder streaming', () => {
        const activeJob$ = new Subject<any>();
        jobService.pollJob.mockReturnValue(activeJob$ as any);
        service.setActive('chat-1');
        service.startAssistantJobPolling('job-1', 'chat-1');

        activeJob$.next({
            id: 'job-1',
            user_id: 'user-1',
            status: 'processing',
            job_type: 'chat_message',
            reference: 'user-msg',
            draft_deltas: ['Part 1'],
            created_at: '',
            updated_at: '',
        });
        expect(service.pendingAssistantDraftText()).toBe('Part 1');

        service.cancelGeneration();

        expect(streamingService.stopStreaming).toHaveBeenCalledWith(CHAT_PENDING_ASSISTANT_MESSAGE_ID, 'Part 1', false);
        expect(service.pendingAssistantDraftText()).toBe('Part 1');
        activeJob$.complete();
    });

    it('requests cancel after pending-send resolves to a real job id', async () => {
        const activeJob$ = new Subject<any>();
        jobService.pollJob.mockReturnValue(activeJob$ as any);
        messageService.sendMessage.mockReturnValue(of({ id: 'user-msg', job_id: 'job-9', type: 'chat_message' }));
        service.setActive('chat-1');

        const sendPromise = service.sendMessage('hello');
        service.cancelGeneration();
        await sendPromise;

        expect(jobService.cancelJob).toHaveBeenCalledWith('job-9');
        activeJob$.complete();
    });

    it('falls back to stopping local streaming when no active job id exists', () => {
        service.setActive('chat-1');
        service['_streamingMessageId'].set('assistant-1');

        service.cancelGeneration();

        expect(streamingService.stopStreaming).toHaveBeenCalledWith('assistant-1', undefined, false);
        expect(jobService.cancelJob).not.toHaveBeenCalled();
    });

    it('does not resume polling for terminal active-job snapshots', () => {
        const user = message('user-1', 'User');
        messages$.next([user]);
        jobService.getActiveChatMessageJob.mockReturnValue(of({ job_id: 'job-1', status: 'cancelled' }));
        service.setActive('chat-1');

        expect(jobService.getActiveChatMessageJob).toHaveBeenCalledWith('chat-1', 'user-1');
        expect(jobService.pollJob).not.toHaveBeenCalled();
        expect(service.assistantJobPending()).toBe(false);
    });

    it('does not resume polling when assistant reply exists but messages are transiently misordered', () => {
        const assistant = message('assistant-1', 'Assistant', { sent_at: '2024-01-01T00:00:02Z' });
        const user = message('user-1', 'User', { sent_at: '2024-01-01T00:00:01Z' });
        messages$.next([assistant, user]);
        jobService.getActiveChatMessageJob.mockReturnValue(of({ job_id: 'job-1', status: 'processing' }));

        service.setActive('chat-1');

        expect(jobService.getActiveChatMessageJob).not.toHaveBeenCalled();
        expect(jobService.pollJob).not.toHaveBeenCalled();
        expect(service.assistantJobPending()).toBe(false);
    });

    it('does not resume polling when newest user turn already has assistant reply despite list-order mismatch', () => {
        const olderUser = message('user-older', 'User', { sent_at: '2024-01-01T00:00:01Z' });
        const newestUser = message('user-new', 'User', { sent_at: '2024-01-01T00:00:05Z' });
        const newestAssistant = message('assistant-new', 'Assistant', { sent_at: '2024-01-01T00:00:06Z' });
        // Deliberately misordered payload where newest user is not the last user in array order.
        messages$.next([newestAssistant, newestUser, olderUser]);
        jobService.getActiveChatMessageJob.mockReturnValue(of({ job_id: 'job-1', status: 'processing' }));

        service.setActive('chat-1');

        expect(jobService.getActiveChatMessageJob).not.toHaveBeenCalled();
        expect(jobService.pollJob).not.toHaveBeenCalled();
        expect(service.assistantJobPending()).toBe(false);
    });

    it('bumps the context checkpoint token when a checkpoint lands after load', () => {
        service.setActive('chat-1');
        expect(service.contextCheckpointToken()).toBe(0);

        messages$.next([message('assistant-1', 'Assistant', { checkpoint_completed_at: '2024-01-02T00:00:00Z' })]);

        expect(service.contextCheckpointToken()).toBe(1);
    });

    it('does not bump the checkpoint token for checkpoints already present at load', () => {
        messages$.next([message('assistant-1', 'Assistant', { checkpoint_completed_at: '2024-01-02T00:00:00Z' })]);
        service.setActive('chat-1');
        expect(service.contextCheckpointToken()).toBe(0);

        // Only a newer checkpoint should trigger a refresh.
        messages$.next([message('assistant-2', 'Assistant', { checkpoint_completed_at: '2024-01-03T00:00:00Z' })]);
        expect(service.contextCheckpointToken()).toBe(1);
    });

    it('reconciles the active thread messages on return to the app', () => {
        service.setActive('chat-1');
        messageService.reconcileLatestPage.mockClear();

        service.syncActiveThread(true);

        expect(messageService.reconcileLatestPage).toHaveBeenCalledWith('chat-1', expect.any(Number));
    });

    it('does not reconcile the active thread while a job is in flight', () => {
        const activeJob$ = new Subject<any>();
        jobService.pollJob.mockReturnValue(activeJob$ as any);
        service.setActive('chat-1');
        service.startAssistantJobPolling('job-1', 'chat-1');
        messageService.reconcileLatestPage.mockClear();

        service.syncActiveThread(true);

        expect(messageService.reconcileLatestPage).not.toHaveBeenCalled();
        activeJob$.complete();
    });

    it('patches model changes onto the active chat', () => {
        service.setActive('chat-1');
        service.setModel({ id: 'model-1', name: 'm', display_name: 'Model', description: '', tool_support: true });

        expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { model_id: 'model-1' });
        expect(service.model()?.id).toBe('model-1');
        expect(service.thread()?.model_id).toBe('model-1');
    });

    it('resets the optimistic model selection when switching threads', () => {
        service.setActive('chat-1');
        service.setModel({ id: 'model-1', name: 'm', display_name: 'Model', description: '', tool_support: true });
        expect(service.model()?.id).toBe('model-1');

        // Switching to another thread must not carry model-1 over as the displayed pick.
        service.setActive('chat-2');
        expect(service.model()).toBeNull();
    });

    it('rolls back model selection when patch fails', () => {
        service.setActive('chat-1');
        service.setModel({ id: 'model-1', name: 'm1', display_name: 'Model 1', description: '', tool_support: true });
        chatService.patchChat.mockReturnValue(throwError(() => new Error('model patch failed')));

        service.setModel({ id: 'model-2', name: 'glm-5.2', display_name: 'GLM-5.2', description: '', tool_support: true });

        expect(service.error()).toBe('Failed to update model. Please try again.');
        expect(service.model()?.id).toBe('model-1');
        expect(service.thread()?.model_id).toBe('model-1');
    });

    it('patches active mode onto the active chat', () => {
        service.setActive('chat-1');
        service.setActiveMood('mood-1');

        expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', {
            active_mood_id: 'mood-1',
            is_auto_mood: false,
        });
    });

    it('clears active mode when set to null', () => {
        service.setActive('chat-1');
        service.setActiveMood(null);

        expect(chatService.patchChat).toHaveBeenCalledWith('chat-1', { clear_active_mood: true });
    });

    it('surfaces an error when active mode patch fails', () => {
        service.setActive('chat-1');
        chatService.patchChat.mockReturnValue(throwError(() => new Error('mode patch failed')));

        service.setActiveMood('mood-1');

        expect(service.error()).toBe('mode patch failed');
        expect(service.thread()).toEqual(chat);
    });

    it('ignores stale load errors after switching threads', () => {
        const firstChat$ = new Subject<Chat>();
        const firstMessages$ = new Subject<{
            results: ChatMessage[];
            page: number;
            total_count: number;
        }>();
        const secondMessages$ = new Subject<{
            results: ChatMessage[];
            page: number;
            total_count: number;
        }>();
        chatService.getChat.mockImplementation(id => id === 'chat-1' ? firstChat$ : of(secondChat));
        messageService.listMessages.mockImplementation(id => id === 'chat-1' ? firstMessages$ : secondMessages$);
        draftService.getDraft.mockReturnValue(null);

        service.setActive('chat-1');
        service.setActive('chat-2');
        firstChat$.error(new Error('old chat failed'));
        firstMessages$.next({ results: [], page: 1, total_count: 0 });

        expect(service.thread()).toEqual(secondChat);
        expect(service.error()).toBeNull();
        expect(service.loading()).toBe(true);
    });

    it('restores draft and error state when send fails', async () => {
        service.setActive('chat-1');
        service.draft.set('hello');
        messageService.sendMessage.mockReturnValue(throwError(() => new Error('network down')));

        const result = await service.sendMessage('hello');
        expect(isChatSendFailed(result)).toBe(true);
        if (isChatSendFailed(result)) {
            expect(result.error).toEqual(expect.any(Error));
        }
        messages$.next([message('assistant-1', 'Assistant')]);

        expect(service.draft()).toBe('hello');
        expect(service.error()).toBe('network down');
        expect(draftService.saveDraft).toHaveBeenCalledWith('chat-1', 'hello');
        expect(jobService.pollJob).not.toHaveBeenCalled();
        expect(streamingService.startStreaming).not.toHaveBeenCalled();
    });

    it('surfaces an error and resets polling state when job polling errors', async () => {
        jobService.pollJob.mockReturnValue(throwError(() => new Error('poll failed')));
        service.setActive('chat-1');

        const result = await service.sendMessage('hello');
        expect(isChatSendSucceeded(result)).toBe(true);
        expect(service.error()).toBe('poll failed');
        expect(service['expectingAssistantResponse']).toBe(false);
        expect(service.assistantJobPending()).toBe(false);
    });
});

function message(id: string, origin: ChatMessage['origin'], overrides: Partial<ChatMessage> = {}): ChatMessage {
    return {
        id,
        chat_id: 'chat-1',
        message: `Message ${id}`,
        origin,
        sent_at: '2024-01-01T00:00:00Z',
        ...overrides,
    };
}
