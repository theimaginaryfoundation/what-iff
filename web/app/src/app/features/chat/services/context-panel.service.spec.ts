import type { MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';

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
        service.setActiveChat({
            id: 'chat-1',
            user_id: 'user-1',
            name: 'Thread',
            created_at: '',
            updated_at: '',
        });

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

    it('forwards desktop visibility to right panel service', () => {
        service.setDesktopVisible(true);
        expect(rightPanel.setVisible).toHaveBeenCalledWith(true);
    });

    it('shows the latest breakdown by default and a pinned one when selected', () => {
        const latest = breakdown(5000);
        service.setLatestBreakdown(latest, 'msg-latest');
        expect(service.shownBreakdown()).toBe(latest);

        const pinned = breakdown(1200);
        service.selectBreakdown(pinned);
        expect(service.shownBreakdown()).toBe(pinned);
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
});
