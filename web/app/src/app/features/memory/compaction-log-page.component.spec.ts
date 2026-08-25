import type { MockedObject } from "vitest";
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideRouter, Router } from '@angular/router';
import { of } from 'rxjs';

import { ChatService } from '../../core/services/chat.service';
import { ConfirmationService } from '../../core/services/confirmation.service';
import { MemoryService } from '../../core/services/memory.service';
import { PersonalityService } from '../../core/services/personality.service';
import { CompactionEvent } from '../../core/models/memory.model';
import { CompactionLogPageComponent } from './compaction-log-page.component';

describe('CompactionLogPageComponent', () => {
    let component: CompactionLogPageComponent;
    let fixture: ComponentFixture<CompactionLogPageComponent>;

    const event: CompactionEvent = {
        id: 'checkpoint-1',
        chat_id: 'chat-1',
        chat_name: 'A long-running project conversation',
        reason: 'assistant_messages_since_checkpoint (20) >= 20',
        loaded_memories: [{ memory_id: 'memory-1', content: 'User prefers concise updates.', scope: 'global' }],
        created_at: '2026-08-11T12:00:00Z',
        updated_at: '2026-08-11T12:00:00Z',
    };

    beforeEach(async () => {
        const memoryService = {
            listCompactionEvents: vi.fn().mockName("MemoryService.listCompactionEvents"),
            revertSnapshot: vi.fn().mockName("MemoryService.revertSnapshot")
        };
        memoryService.listCompactionEvents.mockReturnValue(of({ results: [event], total_count: 1, page: 1 }));

        const chatService = {
            listChats: vi.fn().mockName("ChatService.listChats")
        };
        chatService.listChats.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

        const personalityService = {
            listPersonalities: vi.fn().mockName("PersonalityService.listPersonalities")
        };
        personalityService.listPersonalities.mockReturnValue(of({ results: [], total_count: 0, page: 1 }));

        await TestBed.configureTestingModule({
            imports: [CompactionLogPageComponent],
            providers: [
                provideZonelessChangeDetection(),
                provideRouter([]),
                { provide: MemoryService, useValue: memoryService },
                { provide: ChatService, useValue: chatService },
                { provide: PersonalityService, useValue: personalityService },
                { provide: ConfirmationService, useValue: {
                        confirm: vi.fn().mockName("ConfirmationService.confirm")
                    } },
            ],
        }).compileComponents();

        fixture = TestBed.createComponent(CompactionLogPageComponent);
        component = fixture.componentInstance;
        fixture.detectChanges();
        component.toggleExpanded(event);
        fixture.detectChanges();
    });

    it('shows labeled, human-readable status chips', () => {
        const text = fixture.nativeElement.textContent;

        expect(text).toContain('Thread');
        expect(text).toContain('A long-running project conversation');
        expect(text).toContain('Checkpoint');
        expect(text).toContain('Turn limit reached');
        expect(text).toContain('Memory changes');
        expect(text).not.toContain('(20) >= 20');
    });

    it('collapses and expands the loaded memories segment', () => {
        const toggle = fixture.nativeElement.querySelector('.loaded-memories-toggle') as HTMLButtonElement | null;
        expect(toggle, 'loaded memories toggle').not.toBeNull();
        expect(toggle?.getAttribute('aria-expanded')).toBe('true');
        expect(toggle?.querySelector('.compaction-toggle__chevron')).not.toBeNull();
        expect(component.isLoadedMemoriesCollapsed(event)).toBe(false);
        expect(fixture.nativeElement.textContent).toContain('User prefers concise updates.');

        component.toggleLoadedMemories(event);
        fixture.detectChanges();

        expect(fixture.nativeElement.querySelector('.loaded-memories-toggle')?.getAttribute('aria-expanded')).toBe('false');
        expect(component.isLoadedMemoriesCollapsed(event)).toBe(true);
        expect(fixture.nativeElement.textContent).not.toContain('User prefers concise updates.');

        component.toggleLoadedMemories(event);

        expect(component.isLoadedMemoriesCollapsed(event)).toBe(false);
    });

    it('renders created memories from a checkpoint', () => {
        const withCreated: CompactionEvent = {
            ...event,
            id: 'checkpoint-2',
            created_memories: [
                { memory_id: 'memory-2', content: 'User is allergic to shellfish.', scope: 'global' },
            ],
        };
        component.events.set([withCreated]);
        fixture.detectChanges();
        component.toggleExpanded(withCreated);
        fixture.detectChanges();

        const text = fixture.nativeElement.textContent;
        expect(text).toContain('New memories (1)');
        expect(text).toContain('User is allergic to shellfish.');
    });

    it('maps checkpoint reason codes to human-readable labels', () => {
        expect(component.checkpointReasonLabel('last_input_tokens (12000) >= 12000')).toBe('Input size limit reached');
        expect(component.checkpointReasonLabel('estimated_context_tokens (900000) >= 900000')).toBe('Conversation size limit reached');
        expect(component.checkpointReasonLabel(null)).toBe('Checkpoint created');
    });

    it('narrows results by thread and personality filters', () => {
        const memoryService = TestBed.inject(MemoryService) as MockedObject<MemoryService>;
        memoryService.listCompactionEvents.mockClear();

        component.updateFilters('chat-42', 'personality-9');

        expect(component.selectedChatID()).toBe('chat-42');
        expect(component.selectedPersonalityID()).toBe('personality-9');
        expect(memoryService.listCompactionEvents).toHaveBeenCalledWith(1, 20, {
            chat_id: 'chat-42',
            personality_id: 'personality-9',
        });
    });

    it('collapses and expands the summaries section', () => {
        expect(component.isSectionCollapsed(event, 'summary')).toBe(false);
        expect(fixture.nativeElement.textContent).toContain('Conversation summary');

        component.toggleSection(event, 'summary');
        fixture.detectChanges();

        expect(component.isSectionCollapsed(event, 'summary')).toBe(true);
        expect(fixture.nativeElement.textContent).not.toContain('Conversation summary');

        component.toggleSection(event, 'summary');
        fixture.detectChanges();

        expect(component.isSectionCollapsed(event, 'summary')).toBe(false);
        expect(fixture.nativeElement.textContent).toContain('Conversation summary');
    });

    it('opens the originating thread at the checkpoint message', () => {
        const router = TestBed.inject(Router);
        const navigateSpy = vi.spyOn(router, 'navigate').mockResolvedValue(true);

        const withMessage: CompactionEvent = { ...event, assistant_message_id: 'msg-77' };
        component.openThread(withMessage);

        expect(navigateSpy).toHaveBeenCalledWith(['/chat', 'chat-1'], { queryParams: { checkpoint: 'msg-77' } });

        navigateSpy.mockClear();
        component.openThread(event);

        expect(navigateSpy).toHaveBeenCalledWith(['/chat', 'chat-1'], { queryParams: undefined });
    });
});
