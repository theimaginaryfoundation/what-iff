import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';

import { MessageService } from './message.service';
import { ChatMessage } from '../models/message.model';

describe('MessageService', () => {
    let service: MessageService;
    let httpMock: HttpTestingController;

    const makeMessage = (overrides: Partial<ChatMessage> = {}): ChatMessage => ({
        id: 'msg-1',
        chat_id: 'chat-1',
        message: 'Hello',
        origin: 'User',
        read_status: 'read',
        sent_at: '2026-06-23T00:00:00Z',
        ...overrides,
    });

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                MessageService,
            ],
        });
        service = TestBed.inject(MessageService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('starts with an empty message list', () => {
        expect(service.getMessages()).toEqual([]);
    });

    it('appends a new message to the list', () => {
        const message = makeMessage();
        service.addMessageToList(message);

        expect(service.getMessages()).toEqual([message]);
    });

    it('appends a second, distinct message rather than replacing the first', () => {
        const first = makeMessage({ id: 'msg-1', message: 'First' });
        const second = makeMessage({ id: 'msg-2', message: 'Second' });

        service.addMessageToList(first);
        service.addMessageToList(second);

        expect(service.getMessages()).toEqual([first, second]);
    });

    it('updates an existing message in place when adding by an id already in the list', () => {
        const original = makeMessage({ message: 'Original text' });
        const updated = makeMessage({ message: 'Updated text' });

        service.addMessageToList(original);
        service.addMessageToList(updated);

        const messages = service.getMessages();
        expect(messages.length).toBe(1);
        expect(messages[0].message).toBe('Updated text');
    });

    // Regression test for PR #236: two concurrent fetchers (e.g. a poller and a
    // streaming/socket update) delivering the same message id used to both push
    // onto the list with a naive append, producing a duplicate rendered message.
    // addMessageToList now looks up the id first and replaces in place instead
    // of always appending, so a "race" of two updates for the same id must
    // collapse to a single list entry with the content of the later update.
    it('dedupes by id when two updates for the same message race each other', () => {
        const chatId = 'chat-1';
        service.setCurrentChatId(chatId);

        const fetchA = makeMessage({ id: 'msg-race', chat_id: chatId, message: 'From fetch A' });
        const fetchB = makeMessage({ id: 'msg-race', chat_id: chatId, message: 'From fetch B (latest)' });

        // Simulate both fetchers delivering the same message id, one after another.
        service.addMessageToList(fetchA);
        service.addMessageToList(fetchB);

        const messages = service.getMessages();
        const matching = messages.filter(m => m.id === 'msg-race');

        expect(matching.length).toBe(1);
        expect(matching[0].message).toBe('From fetch B (latest)');
    });

    it('ignores messages for a chat other than the current active chat', () => {
        service.setCurrentChatId('chat-1');
        const otherChatMessage = makeMessage({ id: 'msg-other', chat_id: 'chat-2' });

        service.addMessageToList(otherChatMessage);

        expect(service.getMessages()).toEqual([]);
    });

    it('clears messages and resets typing state', () => {
        service.addMessageToList(makeMessage());
        service.setAssistantTyping(true);

        service.clearMessages();

        expect(service.getMessages()).toEqual([]);
        let isTyping = true;
        service.isAssistantTyping$.subscribe(v => (isTyping = v));
        expect(isTyping).toBe(false);
    });

    it('marks unread assistant messages in the chat as read', () => {
        const chatId = 'chat-1';
        const unreadAssistantMessage = makeMessage({
            id: 'msg-assistant',
            chat_id: chatId,
            origin: 'Assistant',
            read_status: 'unread',
        });
        service.addMessageToList(unreadAssistantMessage);

        service.markAssistantMessagesRead(chatId);

        const messages = service.getMessages();
        expect(messages[0].read_status).toBe('read');
    });
});
