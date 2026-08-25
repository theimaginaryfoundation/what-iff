import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { of } from 'rxjs';

import { ChatService } from '../../../core/services/chat.service';
import { ScratchpadService } from './scratchpad.service';

type ChatServiceMock = Pick<MockedObject<ChatService>, 'getChatContext' | 'patchChatContext'>;

describe('ScratchpadService', () => {
    let service: ScratchpadService;
    let chatService: ChatServiceMock;

    beforeEach(() => {
        chatService = {
            getChatContext: vi.fn().mockName("ChatService.getChatContext"),
            patchChatContext: vi.fn().mockName("ChatService.patchChatContext")
        } as unknown as ChatServiceMock;
        chatService.getChatContext.mockReturnValue(of({
            chat_id: 'chat-1',
            active_scratchpad: 'initial note',
            summary: 'summary',
        }));
        chatService.patchChatContext.mockReturnValue(of({
            chat_id: 'chat-1',
            active_scratchpad: 'saved',
            summary: 'summary',
        }));

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                ScratchpadService,
                { provide: ChatService, useValue: chatService },
            ],
        });
        service = TestBed.inject(ScratchpadService);
    });

    it('loads context for active chat', async () => {
        await service.load('chat-1', true);

        expect(chatService.getChatContext).toHaveBeenCalledWith('chat-1');
        expect(service.value()).toBe('initial note');
        expect(service.summary()).toBe('summary');
    });

    it('debounces save and patches context', async () => {
        await service.load('chat-1', true);
        service.updateDraft('new draft');
        await new Promise(resolve => setTimeout(resolve, 560));

        expect(chatService.patchChatContext).toHaveBeenCalledWith('chat-1', { active_scratchpad: 'new draft' });
        expect(service.status()).toBe('saved');
    });
});
