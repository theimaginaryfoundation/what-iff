import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { signal } from '@angular/core';
import { of, Subject, throwError } from 'rxjs';

import { ChatService } from './chat.service';
import { ThreadListService } from './thread-list.service';
import { Chat, PatchChatRequest } from '../models/chat.model';
import { clearRecentOpenedThreadIds } from '../../features/chat/helpers/thread-list.helpers';

function makeChat(overrides: Partial<Chat> = {}): Chat {
    return {
        id: 'chat-id',
        user_id: 'user-1',
        name: 'Thread',
        created_at: '2026-04-01T00:00:00Z',
        updated_at: '2026-04-01T00:00:00Z',
        ...overrides,
    };
}

describe('ThreadListService', () => {
    let service: ThreadListService;
    let chatService: Pick<MockedObject<ChatService>, 'listChats' | 'listAllChats' | 'patchChat' | 'deleteChat'>;

    beforeEach(() => {
        clearRecentOpenedThreadIds();
        chatService = {
            listChats: vi.fn().mockName("ChatService.listChats"),
            listAllChats: vi.fn().mockName("ChatService.listAllChats"),
            patchChat: vi.fn().mockName("ChatService.patchChat"),
            deleteChat: vi.fn().mockName("ChatService.deleteChat")
        } as unknown as Pick<MockedObject<ChatService>, 'listChats' | 'listAllChats' | 'patchChat' | 'deleteChat'>;
        chatService.listAllChats.mockReturnValue(of({
            chats: [makeChat({ id: 'a', name: 'Alpha' }), makeChat({ id: 'b', name: 'Bravo' })],
            truncated: false,
        }));
        chatService.patchChat.mockImplementation((id: string, patch: PatchChatRequest) => of(makeChat({ id, ...patch })));
        chatService.deleteChat.mockReturnValue(of(void 0));

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                ThreadListService,
                { provide: ChatService, useValue: chatService },
            ],
        });
        service = TestBed.inject(ThreadListService);
    });

    it('debounces query-triggered refresh', async () => {
        service.setQuery('alp');
        expect(chatService.listAllChats).not.toHaveBeenCalled();
        await new Promise(resolve => setTimeout(resolve, 240));
        expect(chatService.listAllChats).toHaveBeenCalled();
    });

    it('keeps the newest refresh result when an earlier request resolves last', async () => {
        const older = new Subject<{ chats: Chat[]; truncated: boolean }>();
        const newer = new Subject<{ chats: Chat[]; truncated: boolean }>();
        chatService.listAllChats.mockReturnValueOnce(older).mockReturnValueOnce(newer);

        const olderRefresh = service.refresh();
        const newerRefresh = service.refresh();
        newer.next({ chats: [makeChat({ id: 'new', name: 'New result' })], truncated: false });
        newer.complete();
        await newerRefresh;

        older.next({ chats: [makeChat({ id: 'old', name: 'Stale result' })], truncated: false });
        older.complete();
        await olderRefresh;

        expect(service.filteredThreads().map(thread => thread.id)).toEqual(['new']);
        expect(service.loading()).toBe(false);
    });

    it('optimistically toggles pin and keeps server value', async () => {
        await service.refresh();
        const thread = service.filteredThreads()[0];

        await service.togglePinned(thread);

        expect(chatService.patchChat).toHaveBeenCalledWith(thread.id, { is_favorite: true });
        expect(service.filteredThreads()[0].is_favorite).toBe(true);
    });

    it('rolls back optimistic delete on failure', async () => {
        await service.refresh();
        chatService.deleteChat.mockReturnValue(throwError(() => new Error('boom')));
        const thread = service.filteredThreads()[0];
        vi.spyOn(console, 'error').mockReturnValue(undefined);

        const ok = await service.deleteThread(thread);

        expect(ok).toBe(false);
        expect(service.filteredThreads().length).toBe(2);
        expect(service.error()).toContain('boom');
        expect(console.error).toHaveBeenCalledWith(expect.stringMatching(/Failed to delete thread a/), expect.any(Error));
    });

    it('bulkDelete continues after a per-thread failure', async () => {
        await service.refresh();
        service.setAllSelected(['a', 'b'], true);
        chatService.deleteChat.mockImplementation((id: string) => {
            if (id === 'a')
                return throwError(() => new Error('delete a failed'));
            return of(void 0);
        });
        vi.spyOn(console, 'error').mockReturnValue(undefined);

        await service.bulkDelete(['a', 'b']);

        expect(chatService.deleteChat).toHaveBeenCalledWith('a');
        expect(chatService.deleteChat).toHaveBeenCalledWith('b');
        expect(service.filteredThreads().map(t => t.id)).toEqual(['a']);
        expect(service.selectedIds().size).toBe(0);
        expect(service.error()).toContain('delete a failed');
        expect(console.error).toHaveBeenCalledWith(expect.stringMatching(/Failed to delete thread a/), expect.any(Error));
    });

    it('bulkSetArchived continues after a per-thread failure', async () => {
        await service.refresh();
        service.setAllSelected(['a', 'b'], true);
        chatService.patchChat.mockImplementation((id: string, patch: PatchChatRequest) => {
            if (id === 'a')
                return throwError(() => new Error('archive a failed'));
            return of(makeChat({ id, ...patch }));
        });
        vi.spyOn(console, 'error').mockReturnValue(undefined);

        await service.bulkSetArchived(['a', 'b'], true);

        expect(chatService.patchChat).toHaveBeenCalledWith('a', { archived: true });
        expect(chatService.patchChat).toHaveBeenCalledWith('b', { archived: true });
        expect(service.filteredThreads().map(t => t.id)).toEqual(['a']);
        expect(service.selectedIds().size).toBe(0);
        expect(service.error()).toContain('archive a failed');
        expect(console.error).toHaveBeenCalledWith(expect.stringMatching(/Failed to update archive for thread a/), expect.any(Error));
    });

    it('bulkAssignPersonality continues after a per-thread failure', async () => {
        await service.refresh();
        service.setAllSelected(['a', 'b'], true);
        chatService.patchChat.mockImplementation((id: string, patch: PatchChatRequest) => {
            if (id === 'a')
                return throwError(() => new Error('personality a failed'));
            return of(makeChat({ id, ...patch }));
        });
        vi.spyOn(console, 'error').mockReturnValue(undefined);

        await service.bulkAssignPersonality(['a', 'b'], 'nova');

        expect(chatService.patchChat).toHaveBeenCalledWith('a', { personality_id: 'nova' });
        expect(chatService.patchChat).toHaveBeenCalledWith('b', { personality_id: 'nova' });
        expect(service.filteredThreads().find(t => t.id === 'b')?.personality_id).toBe('nova');
        expect(service.selectedIds().size).toBe(0);
        expect(service.error()).toContain('personality a failed');
        expect(console.error).toHaveBeenCalledWith(expect.stringMatching(/Failed to patch thread a/), expect.any(Error));
    });

    it('returns false from setTags when patch fails', async () => {
        await service.refresh();
        chatService.patchChat.mockReturnValue(throwError(() => new Error('tag save failed')));
        const thread = service.filteredThreads()[0];
        vi.spyOn(console, 'error').mockReturnValue(undefined);

        const ok = await service.setTags(thread, ['a', 'b']);

        expect(ok).toBe(false);
        expect(service.error()).toContain('tag save failed');
        expect(console.error).toHaveBeenCalled();

        chatService.patchChat.mockImplementation((id: string, patch: PatchChatRequest) => of(makeChat({ id, ...patch })));
    });

    it('records opened thread ids when active thread changes', () => {
        service.setActiveThreadId('a');
        expect(service.recentOpenedIds()).toEqual(['a']);
        service.setActiveThreadId('b');
        expect(service.recentOpenedIds()).toEqual(['b', 'a']);
    });

    it('clearUnreadForThread zeroes unread_count locally', async () => {
        chatService.listAllChats.mockReturnValue(of({
            chats: [
                makeChat({ id: 'a', name: 'Alpha', unread_count: 3 }),
                makeChat({ id: 'b', name: 'Bravo', unread_count: 0 }),
            ],
            truncated: false,
        }));
        await service.refresh();
        service.clearUnreadForThread('a');
        const alpha = service.filteredThreads().find(t => t.id === 'a');
        expect(alpha?.unread_count).toBe(0);
    });
});
