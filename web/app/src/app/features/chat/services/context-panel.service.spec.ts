import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

import { Chat } from '../../../core/models/chat.model';
import { ContextBreakdown } from '../../../core/models/message.model';
import { RightPanelService } from '../../../core/services/right-panel.service';
import { ContextPanelService } from './context-panel.service';

function breakdown(total: number): ContextBreakdown {
    return {
        segments: [{ kind: 'history_turn', segments: 1, tokens: total, cacheable: false }],
        total_tokens: total,
        budget_tokens: 30000,
        captured_at: '2026-08-17T12:00:00Z',
    };
}

function chat(id: string, name: string): Chat {
    return {
        id,
        user_id: 'user-1',
        name,
        created_at: '',
        updated_at: '',
    };
}

type RightPanelServiceMock = Pick<MockedObject<RightPanelService>, 'setVisible'>;

describe('ContextPanelService', () => {
    let service: ContextPanelService;
    let rightPanel: RightPanelServiceMock;

    beforeEach(() => {
        localStorage.clear();
        rightPanel = {
            setVisible: vi.fn().mockName("RightPanelService.setVisible")
        } as unknown as RightPanelServiceMock;

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                ContextPanelService,
                { provide: RightPanelService, useValue: rightPanel },
            ],
        });
        service = TestBed.inject(ContextPanelService);
    });

    it('persists active tab per chat', () => {
        service.setActiveChat(chat('chat-1', 'Thread'));

        service.setActiveTab('tools');

        expect(service.activeTab()).toBe('tools');
        const saved = localStorage.getItem('contextPanel.tabsByChat.v1');
        expect(saved).toContain('"chat-1":"tools"');
    });

    it('queues and consumes composer insert payload', () => {
        service.requestComposerInsert('note');

        expect(service.consumeComposerInsert()).toBe('note');
        expect(service.consumeComposerInsert()).toBeNull();
    });

    const ID_A = '11111111-1111-4111-8111-111111111111';
    const ID_B = '22222222-2222-4222-8222-222222222222';
    const ID_ACTIVE = '33333333-3333-4333-8333-333333333333';
    const ID_OTHER = '44444444-4444-4444-8444-444444444444';
    const refIds = () => service.composerThreadReferences().map(thread => thread.id);

    it('toggles composer thread references and never references the active chat', () => {
        service.setActiveChat(chat(ID_ACTIVE, 'Current chat'));
        const other = chat(ID_A, 'Old research');

        service.toggleComposerThreadReference(other);
        service.toggleComposerThreadReference(chat(ID_ACTIVE, 'Current chat'));
        expect(service.composerThreadReferences()).toEqual([other]);
        expect(service.isComposerThreadReferenced(ID_A)).toBe(true);

        service.toggleComposerThreadReference(other);
        expect(service.composerThreadReferences()).toEqual([]);
    });

    it('filters the active chat out of a wholesale replace', () => {
        service.setActiveChat(chat(ID_ACTIVE, 'Current chat'));
        service.setComposerThreadReferences([chat(ID_A, 'Alpha'), chat(ID_ACTIVE, 'Current chat')]);
        expect(refIds()).toEqual([ID_A]);
    });

    it('removes, clears and formats composer thread references', () => {
        service.toggleComposerThreadReference(chat(ID_A, 'Alpha'));
        service.toggleComposerThreadReference(chat(ID_B, 'Beta'));

        expect(service.composerThreadReferencesText()).toBe(
            `[Referenced thread "Alpha" — read it with find_context mode="conversation" target="${ID_A}"]\n` +
            `[Referenced thread "Beta" — read it with find_context mode="conversation" target="${ID_B}"]\n`,
        );

        service.removeComposerThreadReference(ID_A);
        expect(refIds()).toEqual([ID_B]);

        service.clearComposerThreadReferences();
        expect(service.composerThreadReferencesText()).toBe('');
    });

    describe('binding references to the chat they were attached for', () => {
        it('carries unbound references (added with no chat open) into the next chat, minus that chat', () => {
            service.setActiveChat(null);
            service.toggleComposerThreadReference(chat(ID_A, 'Alpha'));
            service.toggleComposerThreadReference(chat(ID_B, 'Beta'));

            service.setActiveChat(chat(ID_A, 'Alpha'));

            expect(refIds()).toEqual([ID_B]);
        });

        it('keeps references while the same chat stays active (e.g. the chat object is refreshed)', () => {
            service.setActiveChat(chat(ID_ACTIVE, 'Current chat'));
            service.toggleComposerThreadReference(chat(ID_A, 'Alpha'));

            service.setActiveChat(chat(ID_ACTIVE, 'Renamed chat'));

            expect(refIds()).toEqual([ID_A]);
        });

        it('clears references bound to a chat when another chat opens', () => {
            service.setActiveChat(chat(ID_ACTIVE, 'Current chat'));
            service.toggleComposerThreadReference(chat(ID_A, 'Alpha'));

            service.setActiveChat(chat(ID_OTHER, 'Other chat'));

            expect(refIds()).toEqual([]);
        });

        it('clears references bound to a chat when no chat is active', () => {
            service.setActiveChat(chat(ID_ACTIVE, 'Current chat'));
            service.toggleComposerThreadReference(chat(ID_A, 'Alpha'));

            service.setActiveChat(null);

            expect(refIds()).toEqual([]);
        });

        it('binds carried references to the chat they arrive in', () => {
            service.toggleComposerThreadReference(chat(ID_A, 'Alpha'));
            service.setActiveChat(chat(ID_ACTIVE, 'Current chat'));
            expect(refIds()).toEqual([ID_A]);

            service.setActiveChat(chat(ID_OTHER, 'Other chat'));

            expect(refIds()).toEqual([]);
        });
    });

    it('forwards desktop visibility to right panel service', () => {
        service.setDesktopVisible(true);
        expect(rightPanel.setVisible).toHaveBeenCalledWith(true);
    });

    it('shows the latest breakdown by default and a pinned one when selected', () => {
        const latest = breakdown(5000);
        service.setLatestBreakdown(latest, 'msg-latest');
        expect(service.shownBreakdown()).toBe(latest);
        expect(service.shownBreakdownId()).toBe('msg-latest');

        const pinned = breakdown(1200);
        service.selectBreakdown(pinned, 'msg-pinned');
        expect(service.shownBreakdown()).toBe(pinned);
        expect(service.shownBreakdownId()).toBe('msg-pinned');
    });

    it('clears a pinned past turn when a genuinely new turn lands', () => {
        service.setLatestBreakdown(breakdown(5000), 'msg-1');
        const pinned = breakdown(1200);
        service.selectBreakdown(pinned);
        expect(service.shownBreakdown()).toBe(pinned);

        // Same message id updating (e.g. re-emit) keeps the pin.
        service.setLatestBreakdown(breakdown(5100), 'msg-1');
        expect(service.shownBreakdown()).toBe(pinned);

        // A new turn (new id) replaces the pin with the newest.
        const newest = breakdown(6000);
        service.setLatestBreakdown(newest, 'msg-2');
        expect(service.shownBreakdown()).toBe(newest);
    });

    it('resolves shownBreakdownId to the pinned turn, else the latest', () => {
        service.setLatestBreakdown(breakdown(5000), 'msg-latest');
        expect(service.shownBreakdownId()).toBe('msg-latest');

        service.selectBreakdown(breakdown(1200), 'msg-pinned');
        expect(service.shownBreakdownId()).toBe('msg-pinned');

        // A new turn clears the pin, so the id falls back to the latest.
        service.setLatestBreakdown(breakdown(6000), 'msg-newest');
        expect(service.shownBreakdownId()).toBe('msg-newest');
    });

    describe('publishThreadUpdate', () => {
        it('makes the saved chat the active chat and exposes it for the session to adopt', () => {
            service.setActiveChat(chat('chat-1', 'Thread'));
            expect(service.threadUpdate()).toBeNull();

            const saved = { ...chat('chat-1', 'Thread'), context_scope: 'sandbox' as const };
            service.publishThreadUpdate(saved);

            expect(service.activeChat()).toBe(saved);
            expect(service.threadUpdate()).toBe(saved);
        });
    });
});
