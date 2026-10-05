import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';

import { environment } from '@environments/environment';
import { ChatService } from './chat.service';

describe('ChatService', () => {
    let service: ChatService;
    let httpMock: HttpTestingController;

    beforeEach(() => {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                ChatService,
            ],
        });
        service = TestBed.inject(ChatService);
        httpMock = TestBed.inject(HttpTestingController);
    });

    afterEach(() => {
        httpMock.verify();
    });

    it('creates chats without auto-starring', () => {
        let responseChatId: string | null = null;
        service.createChat({ name: 'New Chat' }).subscribe(chat => {
            responseChatId = chat.id;
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/chat`);
        expect(req.request.method).toBe('POST');
        expect(req.request.body).toEqual(expect.objectContaining({ name: 'New Chat' }));
        expect(req.request.body.is_favorite).toBeUndefined();
        req.flush({
            id: 'chat-1',
            user_id: 'user-1',
            name: 'New Chat',
            is_favorite: false,
            created_at: '2026-06-23T00:00:00Z',
            updated_at: '2026-06-23T00:00:00Z',
        });

        expect(responseChatId as string | null).toBe('chat-1');
        expect(service.getActiveChat()?.id).toBe('chat-1');
    });

    it('preserves an explicit favorite choice on create', () => {
        service.createChat({ name: 'Starred chat', is_favorite: true }).subscribe();

        const req = httpMock.expectOne(`${environment.apiUrl}/chat`);
        expect(req.request.method).toBe('POST');
        expect(req.request.body.is_favorite).toBe(true);
        req.flush({
            id: 'chat-2',
            user_id: 'user-1',
            name: 'Starred chat',
            is_favorite: true,
            created_at: '2026-06-23T00:00:00Z',
            updated_at: '2026-06-23T00:00:00Z',
        });
    });

    it('starts an imported thread\'s summary with a POST to the rehydrate endpoint', () => {
        let state: string | undefined;
        service.rehydrateChat('chat-1').subscribe(chat => {
            state = chat.rehydration_state;
        });

        const req = httpMock.expectOne(`${environment.apiUrl}/chat/chat-1/rehydrate`);
        expect(req.request.method).toBe('POST');
        req.flush({
            id: 'chat-1',
            user_id: 'user-1',
            name: 'Imported',
            rehydration_state: 'pending',
            created_at: '2026-06-23T00:00:00Z',
            updated_at: '2026-06-23T00:00:00Z',
        });

        expect(state).toBe('pending');
    });

    describe('markAllChatsRead', () => {
        function seedActiveChatWithUnread(): void {
            service.createChat({ name: 'Unread chat' }).subscribe();
            httpMock.expectOne(`${environment.apiUrl}/chat`).flush({
                id: 'chat-3',
                user_id: 'user-1',
                name: 'Unread chat',
                unread_count: 4,
                created_at: '2026-06-23T00:00:00Z',
                updated_at: '2026-06-23T00:00:00Z',
            });
        }

        it('posts once and clears cached badges on success', () => {
            seedActiveChatWithUnread();
            let updated: number | undefined;
            service.markAllChatsRead().subscribe(res => (updated = res.updated_count));

            const req = httpMock.expectOne(`${environment.apiUrl}/chat/mark-all-read`);
            expect(req.request.method).toBe('POST');
            req.flush({ updated_count: 4 });

            expect(updated).toBe(4);
            expect(service.getActiveChat()?.unread_count).toBe(0);
        });

        it('keeps cached badges and errors when the server fails', () => {
            seedActiveChatWithUnread();
            let failed = false;
            service.markAllChatsRead().subscribe({ error: () => (failed = true) });

            httpMock
                .expectOne(`${environment.apiUrl}/chat/mark-all-read`)
                .flush({ error: 'boom' }, { status: 500, statusText: 'Server Error' });

            expect(failed).toBe(true);
            expect(service.getActiveChat()?.unread_count).toBe(4);
        });
    });
});
