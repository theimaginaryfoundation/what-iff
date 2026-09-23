import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection, signal } from '@angular/core';
import { Router } from '@angular/router';
import { of, throwError } from 'rxjs';

import { ChatBranchingService } from './chat-branching.service';
import { ChatSessionService } from '../chat-session.service';
import { ChatService } from '../../../core/services/chat.service';
import { DraftMessageService } from '../../../core/services/draft-message.service';
import { ThreadListService } from '../../../core/services/thread-list.service';
import { Chat, ChatLineage } from '../../../core/models/chat.model';
import { ChatMessage } from '../../../core/models/message.model';

function chat(overrides: Partial<Chat> = {}): Chat {
    return { id: 'c1', user_id: 'u1', name: 'Trip', created_at: '', updated_at: '', ...overrides };
}

function msg(origin: ChatMessage['origin'], id = 'm1', text = 'Should we go to Lisbon?'): ChatMessage {
    return { id, chat_id: 'c1', message: text, origin, sent_at: '2026-01-01T00:00:00Z' };
}

describe('ChatBranchingService', () => {
    let service: ChatBranchingService;
    let thread: ReturnType<typeof signal<Chat | null>>;
    let generating: ReturnType<typeof signal<boolean>>;
    let chatService: { getChatLineage: ReturnType<typeof vi.fn>; forkChat: ReturnType<typeof vi.fn> };
    let drafts: { saveDraft: ReturnType<typeof vi.fn> };
    let threadList: { refresh: ReturnType<typeof vi.fn> };
    let router: { navigate: ReturnType<typeof vi.fn> };
    const lineage: ChatLineage = {
        parent: null,
        branches: [{ id: 'b1', name: 'What if: Trip', forked_from_message_id: 'm2', created_at: '2026-01-02T00:00:00Z' }],
    };

    beforeEach(() => {
        thread = signal<Chat | null>(chat());
        generating = signal(false);
        chatService = {
            getChatLineage: vi.fn().mockReturnValue(of(lineage)),
            forkChat: vi.fn().mockReturnValue(of({ chat: chat({ id: 'branch-1' }), copied_messages: 2 })),
        };
        drafts = { saveDraft: vi.fn() };
        threadList = { refresh: vi.fn().mockResolvedValue(undefined) };
        router = { navigate: vi.fn().mockResolvedValue(true) };

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                ChatBranchingService,
                { provide: ChatSessionService, useValue: { thread, isGenerating: generating } },
                { provide: ChatService, useValue: chatService },
                { provide: DraftMessageService, useValue: drafts },
                { provide: ThreadListService, useValue: threadList },
                { provide: Router, useValue: router },
            ],
        });
        service = TestBed.inject(ChatBranchingService);
        TestBed.tick();
    });

    it('loads lineage for the active thread and indexes branches by message', () => {
        expect(chatService.getChatLineage).toHaveBeenCalledWith('c1');
        expect(service.branchesByMessage().get('m2')?.[0].id).toBe('b1');
        expect(service.bannerText()).toBeNull();
    });

    it('reloads lineage when the thread changes', () => {
        chatService.getChatLineage.mockReturnValue(of({ parent: { id: 'c1', name: 'Trip', deleted: false }, branches: [] }));
        thread.set(chat({ id: 'c2' }));
        TestBed.tick();
        expect(chatService.getChatLineage).toHaveBeenLastCalledWith('c2');
        expect(service.bannerText()).toBe('Branched from “Trip”');
    });

    it('branches before a user message, saves its text as the branch draft, and opens the branch', () => {
        service.branchFrom(msg('User'));

        expect(chatService.forkChat).toHaveBeenCalledWith('c1', { message_id: 'm1', include_message: false });
        expect(drafts.saveDraft).toHaveBeenCalledWith('branch-1', 'Should we go to Lisbon?');
        expect(threadList.refresh).toHaveBeenCalled();
        expect(router.navigate).toHaveBeenCalledWith(['/chat', 'branch-1']);
        expect(service.creating()).toBe(false);
    });

    it('branches through an assistant reply without a draft', () => {
        service.branchFrom(msg('Assistant', 'm2', 'Lisbon it is.'));
        expect(chatService.forkChat).toHaveBeenCalledWith('c1', { message_id: 'm2', include_message: true });
        expect(drafts.saveDraft).not.toHaveBeenCalled();
    });

    it('surfaces fork errors and stays on the thread', () => {
        chatService.forkChat.mockReturnValue(throwError(() => new Error('nope')));
        service.branchFrom(msg('Assistant'));
        expect(service.error()).toBe('nope');
        expect(router.navigate).not.toHaveBeenCalled();
    });

    it('is disabled while generating or on archived threads', () => {
        expect(service.enabled()).toBe(true);
        generating.set(true);
        expect(service.enabled()).toBe(false);
        service.branchFrom(msg('User'));
        expect(chatService.forkChat).not.toHaveBeenCalled();

        generating.set(false);
        thread.set(chat({ archived: true }));
        expect(service.enabled()).toBe(false);
    });

    it('opens the parent at the branch point, but not a deleted parent', () => {
        service.lineage.set({ parent: { id: 'p1', message_id: 'm9', name: 'Trip', deleted: false }, branches: [] });
        service.openParent();
        expect(router.navigate).toHaveBeenCalledWith(['/chat', 'p1'], { queryParams: { focus: 'm9' } });

        router.navigate.mockClear();
        service.lineage.set({ parent: { id: 'p1', deleted: true }, branches: [] });
        service.openParent();
        expect(router.navigate).not.toHaveBeenCalled();
    });

    it('flags a branch whose earlier history is still being summarized', () => {
        service.lineage.set({ parent: { id: 'p1', name: 'Trip', deleted: false }, branches: [] });
        thread.set(chat({ rehydration_state: 'pending' }));
        expect(service.catchingUp()).toBe(true);
        thread.set(chat({ rehydration_state: 'ready' }));
        expect(service.catchingUp()).toBe(false);
    });
});
